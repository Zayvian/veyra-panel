// handlers_auth.go - 完整替换文件
//
// 修改：postSetup / postLogin 成功后签发 CSRF token
//       postLogout 时清除 CSRF token
//
// 其他 handler 不变（GET 请求不需要 CSRF token）

package web

import (
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/zayvian-lee/veyra-panel/internal/ratelimit"
	"github.com/zayvian-lee/veyra-panel/internal/service"
	"github.com/zayvian-lee/veyra-panel/internal/store"
)

func (s *Server) getSetup(w http.ResponseWriter, r *http.Request) {
	exists, err := s.svc.AdminExists()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if exists {
		s.redirect(w, r, "/login")
		return
	}
	// The setup form is only available within a bounded window after the
	// panel's first boot. After that, /setup is locked even if no admin
	// has been created — a hand-run binary left unattended cannot stay
	// open forever, or anyone who reaches it first takes the account.
	open, err := s.setupOpen(time.Now())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !open {
		s.errorBanner(w, http.StatusForbidden,
			"setup window has expired; restart the panel with the -set-admin flag")
		return
	}
	s.page(w, r, "setup", nil)
}

func (s *Server) postSetup(w http.ResponseWriter, r *http.Request) {
	exists, err := s.svc.AdminExists()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if exists {
		s.redirect(w, r, "/login")
		return
	}
	open, err := s.setupOpen(time.Now())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !open {
		s.errorBanner(w, http.StatusForbidden,
			"setup window has expired; restart the panel with the -set-admin flag")
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")
	if password != r.FormValue("password2") {
		s.errorBanner(w, http.StatusBadRequest, "the two passwords do not match")
		return
	}
	// Claimed inside the write, not checked beforehand: two requests
	// arriving together would otherwise both pass the check above and
	// the later one would take the account.
	created, err := s.svc.CreateAdmin(username, password)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !created {
		s.redirect(w, r, "/login")
		return
	}
	// Now that there is a session, hand the browser the CSRF token it
	// will need to make any state-changing request from this point on.
	// The token is bound to the username: a cookie issued here only
	// verifies for forms posted by this user.
	s.sess.issue(w, username, s.secureCookies, s.sessionGen)
	s.csrf.issue(w, username, s.secureCookies)
	s.redirect(w, r, "/")
}

func (s *Server) getLogin(w http.ResponseWriter, r *http.Request) {
	exists, err := s.svc.AdminExists()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !exists {
		s.redirect(w, r, "/setup")
		return
	}
	if _, err := s.sess.user(r, s.sessionGen); err == nil {
		s.redirect(w, r, "/")
		return
	}
	s.page(w, r, "login", nil)
}

func (s *Server) postLogin(w http.ResponseWriter, r *http.Request) {
	// Before the password check, not after: the check is a bcrypt, so
	// an unthrottled one is both a guessing oracle and a way to
	// spend the panel's CPU without holding any credential.
	if !s.logins.Allow(ratelimit.ClientIP(r), time.Now()) {
		s.log.Warn("login rate limited", "remote", r.RemoteAddr)
		s.errorBanner(w, http.StatusTooManyRequests, "太多次尝试，请稍候再试")
		return
	}
	username := r.FormValue("username")
	if err := s.svc.CheckAdmin(username, r.FormValue("password")); err != nil {
		if errors.Is(err, service.ErrBadCredentials) {
			// One message for both wrong username and wrong password:
			// telling them apart is free reconnaissance.
			s.log.Warn("failed login", "username", username, "remote", r.RemoteAddr)
			s.errorBanner(w, http.StatusUnauthorized, "wrong username or password")
			return
		}
		s.fail(w, r, err)
		return
	}
	s.sess.issue(w, username, s.secureCookies, s.sessionGen)
	// Pair the session cookie with a CSRF cookie. They are issued
	// together because the verifier binds them: a token signed for
	// this user only verifies for this user, and there is no token
	// without a session to bind it to.
	s.csrf.issue(w, username, s.secureCookies)
	s.redirect(w, r, "/")
}

func (s *Server) postLogout(w http.ResponseWriter, r *http.Request) {
	s.sess.clear(w, s.secureCookies)
	// Drop the CSRF cookie too. A logged-out browser should not
	// still be carrying a token that the server would otherwise
	// accept (against a new, identical session) until the TTL runs
	// out.
	s.csrf.clear(w, s.secureCookies)
	// Bump the session generation so every cookie issued before this
	// logout is invalidated, whether the browser deleted it or not. A
	// copied or resurrected cookie can no longer authenticate.
	if next, err := s.bumpSessionGeneration(); err != nil {
		s.log.Warn("bump session generation", "error", err)
	} else {
		s.sessionGen = next
	}
	s.redirect(w, r, "/login")
}

func (s *Server) getDashboard(w http.ResponseWriter, r *http.Request) {
	users, err := s.svc.Users()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	nodes, err := s.svc.Nodes()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	inbounds, err := s.svc.Inbounds()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var totalTraffic int64
	active := 0
	for _, u := range users {
		totalTraffic += u.TrafficUsed
		if u.Active(nowFunc()) {
			active++
		}
	}
	history, err := s.svc.TrafficHistory(14)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	online := s.nodes.OnlineUsers()
	nodesUp := 0
	for _, n := range nodes {
		if s.nodes.Connected(n.ID) {
			nodesUp++
		}
	}
	// Per node, because a single total answers "how much" and
	// nothing else. It cannot say which node is carrying the load,
	// which one stopped carrying any, or which one is about to need
	// a bigger plan.
	byNode, err := s.svc.NodeTraffic(dashboardDays)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	rows := make([]nodeTrafficRow, 0, len(nodes))
	var ledgerTotal int64
	for _, n := range nodes {
		u := byNode[n.ID]
		total := u.Up + u.Down
		period := n.StatsUp + n.StatsDown
		ledgerTotal += total
		rows = append(rows, nodeTrafficRow{
			Node:      n,
			Up:        u.Up,
			Down:      u.Down,
			Total:     total,
			Recent:    u.Recent,
			Period:    period,
			Connected: s.nodes.Connected(n.ID),
			Inbounds:  countInbounds(inbounds, n.ID),
		})
	}
	// Busiest first: the interesting node is the one at the top, and
	// with more than a handful of them alphabetical order buries it.
	sort.Slice(rows, func(i, j int) bool { return rows[i].Period > rows[j].Period })
	s.page(w, r, "dashboard", map[string]any{
		"Users":        len(users),
		"ActiveUsers":  active,
		"OnlineUsers":  len(online),
		"Nodes":        len(nodes),
		"NodesUp":      nodesUp,
		"Inbounds":     len(inbounds),
		"Traffic":      totalTraffic,
		"Chart":        trafficChart(history),
		"NodeTraffic":  rows,
		"Days":         dashboardDays,
		// The per-node ledger and the per-user totals are counted
		// separately — deleting a node drops its rows, deleting a
		// user drops theirs — so they drift apart. Showing both and
		// letting the difference be visible beats picking one and
		// being quietly wrong.
		"LedgerTotal": ledgerTotal,
	})
}

// dashboardDays is the window for the "recent" column, matching the
// chart above it so the two are read together.
const dashboardDays = 14

type nodeTrafficRow struct {
	Node      *store.Node
	Up        int64
	Down      int64
	Total     int64
	Recent    int64
	Period    int64
	Connected bool
	Inbounds  int
}

func countInbounds(inbounds []*store.Inbound, nodeID int64) int {
	n := 0
	for _, in := range inbounds {
		if in.NodeID == nodeID {
			n++
		}
	}
	return n
}
