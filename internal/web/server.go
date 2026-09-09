// Package web serves the admin UI: plain Go templates plus htmx, no build
// step and no bundler.
//
// Handlers here are deliberately thin — read the form, call the service,
// render a template. All the logic lives in internal/service, so this
// package can be replaced by a JSON API and a single-page frontend
// without touching anything that matters.
package web

import (
	"embed"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kosje/skysbx-panel/internal/ratelimit"
	"github.com/kosje/skysbx-panel/internal/service"
	"github.com/kosje/skysbx-panel/internal/store"
)

//go:embed templates/*.html static/*
var assets embed.FS

const (
	settingSessionKey    = "web.session_key"
	settingCSRFKey       = "web.csrf_key"
	settingSessionGen    = "web.session_generation"
	settingSetupDeadline = "web.setup_deadline"
)

// setupWindow is how long the first-run setup form stays open after the
// panel is first started. A one-click install creates the admin before the
// server starts, so this window only matters when the binary is run by hand:
// there, the form must not stay open forever, or anyone who reaches the
// panel before the operator notices could take it over. Ten minutes is
// plenty to open a browser and fill in a form, and short enough that
// leaving the process running unattended stops being a race with a
// stranger.
const setupWindow = 10 * time.Minute

// sessionGeneration reads the current session generation counter, or 0 if it
// has never been bumped. The generation is what makes logout meaningful: a
// cookie signed under an older generation is rejected by user(), so bumping
// the counter on logout invalidates every cookie issued before it.
func (s *Server) sessionGeneration() (int64, error) {
	v, err := s.svc.Store().Setting(settingSessionGen)
	if err != nil {
		return 0, err
	}
	if v == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("stored session generation is corrupt: %w", err)
	}
	return n, nil
}

// bumpSessionGeneration increments the counter manometrically: read, add one,
// write. Only the admin logout path calls it, so there is no contention to
// worry about, and a lost update (two concurrent logouts) would at worst keep
// an already-logged-out cookie valid for another logout's worth of time.
func (s *Server) bumpSessionGeneration() (int64, error) {
	cur, err := s.sessionGeneration()
	if err != nil {
		return 0, err
	}
	next := cur + 1
	if err := s.svc.Store().SetSetting(settingSessionGen, strconv.FormatInt(next, 10)); err != nil {
		return 0, err
	}
	return next, nil
}

// setupOpen reports whether the first-run setup form is still available.
// The deadline is written on the first boot and never overwritten, so a
// one-click install that creates the admin before the server starts never
// opens the window; a hand-run binary has setupWindow from its first boot
// to create the admin before the form locks and refuses new claims.
func (s *Server) setupOpen(now time.Time) (bool, error) {
	raw, err := s.svc.Store().Setting(settingSetupDeadline)
	if err != nil {
		return false, err
	}
	if raw == "" {
		// No deadline has been claimed yet: this is the first boot (or the
		// database is new). Claim it atomically — the guard is the key's
		// own absence, so two processes booting together cannot both race
		// to write different deadlines.
		deadline := now.Add(setupWindow)
		wrote, err := s.svc.Store().SetSettingsIfAbsent(settingSetupDeadline, map[string]string{
			settingSetupDeadline: strconv.FormatInt(deadline.Unix(), 10),
		})
		if err != nil {
			return false, err
		}
		if !wrote {
			// Someone else claimed it first; read their deadline.
			return s.setupOpen(now)
		}
		return true, nil
	}
	deadline, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return false, fmt.Errorf("stored setup deadline is corrupt: %w", err)
	}
	return now.Unix() <= deadline, nil
}

type Server struct {
	subscriptionDomain string
	svc                *service.Service
	log                *slog.Logger
	tpl                *template.Template
	sess               *sessions
	csrf               *csrfToken
	nodes              NodeChannel
	logins             *ratelimit.Limiter
	secureCookies      bool
	sessionGen         int64 // current session generation, cached to avoid a DB read per request
}

// NodeChannel is the node control channel, mounted by the router. It is an
// interface so that the web package does not depend on the hub, and —
// more to the point — so that forgetting to pass one is a compile error
// rather than a route that quietly 404s every node that dials in.
type NodeChannel interface {
	Handler() http.HandlerFunc
	Connected(nodeID int64) bool
	OnlineUsers() map[string]bool
	// UserIPCounts is how many distinct source addresses each user is
	// connected from, summed across nodes.
	UserIPCounts() map[string]int
	// LiveInbounds is the set of inbound tags the node says it is serving.
	// known is false when it has not said — a disconnected node, or one
	// that has not reported yet — which must not be shown as "everything
	// is down".
	LiveInbounds(nodeID int64) (tags map[string]bool, known bool)
	// ApplyError is why the node is not running what it was last sent.
	ApplyError(nodeID int64) string
}

func New(svc *service.Service, nodes NodeChannel, log *slog.Logger, secureCookies bool) (*Server, error) {
	tpl, err := template.New("").Funcs(templateFuncs()).ParseFS(assets, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	// The session key lives in the database so restarts do not log everyone out.
	stored, err := svc.Store().Setting(settingSessionKey)
	if err != nil {
		return nil, err
	}
	var key []byte
	if stored == "" {
		key = newSessionKey()
		if err := svc.Store().SetSetting(settingSessionKey, base64.StdEncoding.EncodeToString(key)); err != nil {
			return nil, err
		}
	} else {
		key, err = base64.StdEncoding.DecodeString(stored)
		if err != nil {
			return nil, fmt.Errorf("stored session key is corrupt: %w", err)
		}
	}

	// Same dance for the CSRF key: it lives in the same `settings` row,
	// generated on first start, so panel restarts do not invalidate every
	// open form. Rotating it is the same operation as rotating the
	// session key — independent values stored side by side.
	csrfStored, err := svc.Store().Setting(settingCSRFKey)
	if err != nil {
		return nil, err
	}
	var csrfKey []byte
	if csrfStored == "" {
		csrfKey = newCSRFKey()
		if err := svc.Store().SetSetting(settingCSRFKey, base64.StdEncoding.EncodeToString(csrfKey)); err != nil {
			return nil, err
		}
	} else {
		csrfKey, err = base64.StdEncoding.DecodeString(csrfStored)
		if err != nil {
			return nil, fmt.Errorf("stored CSRF key is corrupt: %w", err)
		}
	}

	if nodes == nil {
		return nil, fmt.Errorf("a node channel is required")
	}
	srv := &Server{
		svc:           svc,
		nodes:         nodes,
		log:           log,
		tpl:           tpl,
		sess:          newSessions(key),
		csrf:          newCSRF(csrfKey),
		secureCookies: secureCookies,
		logins:        ratelimit.New(10, 5*time.Second),
	}
	// The session generation is cached for the lifetime of the Server; it
	// only changes on logout, which goes through this same Server.
	gen, err := srv.sessionGeneration()
	if err != nil {
		return nil, err
	}
	srv.sessionGen = gen
	return srv, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	static, err := fs.Sub(assets, "static")
	if err != nil {
		// Impossible: the directory is embedded at compile time.
		panic("skysbx: embedded static assets missing: " + err.Error())
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))

	// The token in the path is the credential; this is the one
	// unauthenticated route that returns anything.
	mux.HandleFunc("GET /sub/{token}", s.getSubscription)

	// The node control channel. Nodes authenticate with their own bearer
	// token, so this sits outside the session gate.
	mux.HandleFunc("GET /api/v1/node/connect", s.nodes.Handler())

	// Open routes.
	mux.HandleFunc("GET /setup", s.getSetup)
	mux.HandleFunc("POST /setup", s.postSetup)
	mux.HandleFunc("GET /login", s.getLogin)
	mux.HandleFunc("POST /login", s.postLogin)
	mux.HandleFunc("POST /logout", s.postLogout)

	// Everything else needs a session, and every state-changing route
	// needs a CSRF token. The two gates stack: auth() first (so we know
	// the user), requireCSRF second (so we know the form was rendered
	// for that user).
	mux.Handle("GET /{$}", s.auth(http.HandlerFunc(s.getDashboard)))

	mux.Handle("GET /users", s.auth(http.HandlerFunc(s.listUsers)))
	mux.Handle("POST /users", s.auth(s.requireCSRF(http.HandlerFunc(s.createUser))))
	mux.Handle("GET /users/{id}/edit", s.auth(http.HandlerFunc(s.editUser)))
	mux.Handle("POST /users/{id}", s.auth(s.requireCSRF(http.HandlerFunc(s.updateUser))))
	mux.Handle("POST /users/{id}/toggle", s.auth(s.requireCSRF(http.HandlerFunc(s.toggleUser))))
	mux.Handle("POST /users/{id}/reset", s.auth(s.requireCSRF(http.HandlerFunc(s.resetUser))))
	mux.Handle("DELETE /users/{id}", s.auth(s.requireCSRF(http.HandlerFunc(s.deleteUser))))
	mux.Handle("GET /users/{id}/activity", s.auth(http.HandlerFunc(s.getUserActivity)))
	mux.Handle("GET /users/{id}/access", s.auth(http.HandlerFunc(s.getUserAccess)))
	mux.Handle("POST /users/{id}/access", s.auth(s.requireCSRF(http.HandlerFunc(s.setUserAccess))))

	mux.Handle("GET /policy", s.auth(http.HandlerFunc(s.getPolicy)))
	mux.Handle("POST /policy", s.auth(s.requireCSRF(http.HandlerFunc(s.setPolicy))))

	mux.Handle("GET /nodes", s.auth(http.HandlerFunc(s.listNodes)))
	mux.Handle("POST /nodes", s.auth(s.requireCSRF(http.HandlerFunc(s.createNode))))
	mux.Handle("GET /nodes/{id}/edit", s.auth(http.HandlerFunc(s.editNode)))
	mux.Handle("POST /nodes/{id}", s.auth(s.requireCSRF(http.HandlerFunc(s.updateNode))))
	mux.Handle("POST /nodes/{id}/toggle", s.auth(s.requireCSRF(http.HandlerFunc(s.toggleNode))))
	mux.Handle("POST /nodes/{id}/rotate", s.auth(s.requireCSRF(http.HandlerFunc(s.rotateNode))))
	mux.Handle("DELETE /nodes/{id}", s.auth(s.requireCSRF(http.HandlerFunc(s.deleteNode))))

	mux.Handle("GET /nodes/{id}/inbounds", s.auth(http.HandlerFunc(s.listInbounds)))
	mux.Handle("POST /nodes/{id}/inbounds", s.auth(s.requireCSRF(http.HandlerFunc(s.createInbound))))
	mux.Handle("GET /inbounds/{id}/edit", s.auth(http.HandlerFunc(s.editInbound)))
	mux.Handle("POST /inbounds/{id}", s.auth(s.requireCSRF(http.HandlerFunc(s.updateInbound))))
	mux.Handle("POST /inbounds/{id}/toggle", s.auth(s.requireCSRF(http.HandlerFunc(s.toggleInbound))))
	mux.Handle("DELETE /inbounds/{id}", s.auth(s.requireCSRF(http.HandlerFunc(s.deleteInbound))))

	return harden(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if s.subscriptionDomain != "" {
			isSubscriptionHost := strings.EqualFold(host, s.subscriptionDomain)
			isSubscriptionPath := strings.HasPrefix(r.URL.Path, "/sub/")
			if isSubscriptionHost != isSubscriptionPath {
				http.NotFound(w, r)
				return
			}
		}
		mux.ServeHTTP(w, r)
	}))
}

// maxBody caps a request body. Every form here is a handful of short
// fields; net/http's own 10 MB default for urlencoded bodies is three
// orders of magnitude more than any of them need, and it is read into
// memory.
const maxBody = 256 << 10

// harden adds the response headers the browser needs in order to defend
// the admin UI, and bounds request bodies.
func harden(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		// Every destructive action in this UI is a single button.
		// Framing the panel and putting something else over those
		// buttons is the cheapest attack there is against a logged-in
		// administrator, and frame-ancestors is what refuses it. The
		// rest of the policy is narrow because the page genuinely
		// needs nothing else: one same-origin script, inline styles
		// and handlers written into the templates, no images, no
		// fonts, no XHR anywhere but here.
		h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self' 'unsafe-inline'; "+
			"style-src 'self' 'unsafe-inline'; img-src 'self' data:; "+
			"connect-src 'self'; form-action 'self'; base-uri 'none'; "+
			"frame-ancestors 'none'")
		h.Set("X-Frame-Options", "DENY") // for anything that predates CSP level 2
		h.Set("X-Content-Type-Options", "nosniff")
		// The subscription token is in the path, so it is in the
		// Referer of every link followed from the subscription page.
		h.Set("Referrer-Policy", "no-referrer")
		// Only over TLS, and only for a month. A year is the usual
		// advice, but this is software someone runs on their own
		// domain: a max-age they cannot revoke is a way to lose that
		// hostname for plain HTTP long after they have stopped running
		// the panel on it.
		if r.TLS != nil {
			h.Set("Strict-Transport-Security", "max-age=2592000")
		}
		// A subscription response is a bearer credential in a text
		// file. It must not be written to any cache between here and
		// the client.
		if strings.HasPrefix(r.URL.Path, "/sub/") {
			h.Set("Cache-Control", "no-store, private")
		}
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
			r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		}
		next.ServeHTTP(w, r)
	})
}

// requireCSRF gates every state-changing request on a valid token. The
// token is bound to the session user, so a logged-in form is required
// to have been rendered for the same user that is now submitting it.
//
// Returns 403 on failure rather than 400: a wrong token is an attack
// attempt, not a user mistake, and 403 is what the upstream browser
// will turn into a "form expired, please reload" message — the same
// thing the user would see if they had just sat on a form for a day.
func (s *Server) requireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, err := s.sess.user(r, s.sessionGen)
		if err != nil {
			s.errorBanner(w, http.StatusForbidden, "session expired; please sign in again")
			return
		}
		if !s.csrf.verify(r, username) {
			s.log.Warn("CSRF token rejected",
				"method", r.Method, "path", r.URL.Path, "remote", r.RemoteAddr)
			s.errorBanner(w, http.StatusForbidden, "form expired; please reload the page")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// auth gates a handler on a valid session. It also handles the
// first-run case: with no administrator configured yet, everything
// redirects to /setup.
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exists, err := s.svc.AdminExists()
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if !exists {
			s.redirect(w, r, "/setup")
			return
		}
		if _, err := s.sess.user(r, s.sessionGen); err != nil {
			s.redirect(w, r, "/login")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// redirect works for both a normal navigation and an htmx request.
// htmx swallows a 302 by following it with XHR and swapping the result
// into a fragment, which would nest a whole login page inside a table;
// HX-Redirect tells it to navigate instead.
func (s *Server) redirect(w http.ResponseWriter, r *http.Request, to string) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", to)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// fail logs the real error and shows the user a short one. Validation
// problems are the user's to fix, so those are shown verbatim.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrInvalid):
		s.errorBanner(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), "invalid: "))
	case errors.Is(err, store.ErrConflict):
		// A duplicate name is something the operator fixes by typing
		// another one, not an internal failure. Saying so beats
		// "something went wrong".
		s.errorBanner(w, http.StatusConflict, "that name or tag is already taken")
	case errors.Is(err, store.ErrNotFound):
		s.errorBanner(w, http.StatusNotFound, "not found")
	default:
		s.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
		s.errorBanner(w, http.StatusInternalServerError, "something went wrong")
	}
}

func (s *Server) errorBanner(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	s.render(w, "error-banner", msg)
}

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	if err := s.tpl.ExecuteTemplate(w, name, data); err != nil {
		// The response is already partly written, so there is nothing
		// useful to send. Log it so a broken template does not vanish
		// silently.
		s.log.Error("render template", "template", name, "error", err)
	}
}

// page renders a full page (with layout) and injects the CSRF token
// into the template data so forms can render `{{ csrfField }}` next to
// their other fields. The token is read from the cookie set by
// `csrf.issue` on login; if the user has no token yet (just after
// `requireCSRF` redirected them to re-login), the field renders empty
// and the next form submission will be rejected, prompting a reload
// that picks up a fresh token.
func (s *Server) page(w http.ResponseWriter, r *http.Request, name string, data map[string]any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if data == nil {
		data = map[string]any{}
	}
	data["Page"] = name
	data["CSRFToken"] = s.csrf.csrfValue(r)
	s.render(w, name, data)
}

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

// templateFuncs is registered in format.go alongside the rest of the
// template helpers; csrfField is one of them. The reason it lives in
// format.go and not here is that template.FuncMap registration has to
// happen once per process — the second declaration in this file would
// shadow the first, taking the csrfField with it.

// csrfField is the function registered with the template engine. It
// emits the hidden input that posts the token back to the server. The
// alternative — rendering {{ .CSRFToken }} directly — works for htmx
// (where the page reads it from a meta tag) but is one more thing to
// remember for every form.
func csrfField() (template.HTML, error) {
	return template.HTML(`<input type="hidden" name="csrf" value="{{ .CSRFToken }}">`), nil
}
