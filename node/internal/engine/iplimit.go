package engine

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/sagernet/sing-box/common/trafficcontrol"
	"github.com/sagernet/sing/service"

	"github.com/Zayvian/veyra-node/internal/proto"
)

// A shared subscription is worth nothing to the person paying for it if fifty
// strangers can use it at once, so a user can be capped at N distinct source
// addresses connected at a time.
//
// Distinct addresses, not connections: a browser opens dozens of connections
// from one machine, and counting those would cap the wrong thing.
//
// Enforcement lives here rather than in the panel because this is where the
// connections are. The panel learns about them up to thirty seconds late, and
// the only lever it has is to revoke the account — which takes the paying user
// offline along with everyone else.

// ipGrace is how long an address keeps its slot after its last connection
// closes.
//
// Without it, a user who idles for a moment loses their place to whoever
// connects next, and comes back to find themselves the one being cut off. With
// it, a genuinely shared account still converges on the first N addresses.
//
// One minute rather than the original five. The cost of a long grace falls on
// the honest user: a phone that moves from Wi-Fi to mobile data shows up from a
// new address while the old one still holds its slot, and on an account at its
// limit the new address — the same person — is refused until the old slot
// expires. Five minutes of that read as the node being down. A minute still
// covers a screen lock or a dropped connection reconnecting; a sharer has to
// stay away longer than that to lose their place.
const ipGrace = time.Minute

// limiter tracks which addresses are using which account, and closes the ones
// over the line.
type limiter struct {
	mu     sync.Mutex
	limits map[string]int             // user -> max distinct addresses
	seen   map[string]map[string]slot // user -> address -> when it was first and last seen

	// over is the last enforce's verdict: per user, the addresses beyond
	// the limit. Xray cannot close connections, so for its inbounds the node
	// blocks these addresses instead, and needs them by address.
	over map[string]map[string]bool
}

type slot struct {
	first time.Time
	last  time.Time
}

func newLimiter() *limiter {
	return &limiter{limits: map[string]int{}, seen: map[string]map[string]slot{}}
}

func (l *limiter) setLimits(limits map[string]int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.limits = map[string]int{}
	for name, n := range limits {
		if n > 0 {
			l.limits[name] = n
		}
	}
}

// conn is one live connection as the clash API reports it.
type conn struct {
	ID   string
	User string
	IP   string // source

	// Where it is going. Used only to count — see proto.Activity for why the
	// destination itself is not kept.
	DestIP   string
	DestPort string
}

// enforce decides which connections to close, and returns the number of
// distinct addresses per user for reporting.
//
// The addresses that keep their place are the ones seen earliest — stable
// across polls, so the decision does not flap between two devices each closing
// the other. A newcomer over the limit is simply cut off; the account keeps
// working for whoever was already using it.
func (l *limiter) enforce(conns []conn, now time.Time) (close []string, counts map[string]int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Refresh what is live before deciding anything.
	live := map[string]map[string]bool{}
	for _, c := range conns {
		if c.User == "" || c.IP == "" {
			continue
		}
		if live[c.User] == nil {
			live[c.User] = map[string]bool{}
		}
		live[c.User][c.IP] = true

		byIP := l.seen[c.User]
		if byIP == nil {
			byIP = map[string]slot{}
			l.seen[c.User] = byIP
		}
		s, ok := byIP[c.IP]
		if !ok {
			s.first = now
		}
		s.last = now
		byIP[c.IP] = s
	}

	// Drop addresses that have been gone longer than the grace period, and
	// users left with none.
	for user, byIP := range l.seen {
		for ip, s := range byIP {
			if now.Sub(s.last) > ipGrace {
				delete(byIP, ip)
			}
		}
		if len(byIP) == 0 {
			delete(l.seen, user)
		}
	}

	l.over = map[string]map[string]bool{}
	counts = make(map[string]int, len(live))
	for user, ips := range live {
		counts[user] = len(ips)
	}

	for user, limit := range l.limits {
		byIP := l.seen[user]
		if len(byIP) <= limit {
			continue
		}
		// Oldest first, ties broken by address so the order is total and the
		// same input always produces the same decision.
		type entry struct {
			ip    string
			first time.Time
		}
		entries := make([]entry, 0, len(byIP))
		for ip, s := range byIP {
			entries = append(entries, entry{ip, s.first})
		}
		sort.Slice(entries, func(i, j int) bool {
			if !entries[i].first.Equal(entries[j].first) {
				return entries[i].first.Before(entries[j].first)
			}
			return entries[i].ip < entries[j].ip
		})

		over := map[string]bool{}
		for _, e := range entries[limit:] {
			over[e.ip] = true
		}
		l.over[user] = over
		for _, c := range conns {
			if c.User == user && over[c.IP] {
				close = append(close, c.ID)
			}
		}
	}
	return close, counts
}

// overAddrs is a copy of the addresses the last enforce found over each user's
// limit — including ones with nothing open at that moment, which still hold
// their place in the order until their grace runs out.
func (l *limiter) overAddrs() map[string]map[string]bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[string]map[string]bool, len(l.over))
	for user, ips := range l.over {
		m := make(map[string]bool, len(ips))
		for ip := range ips {
			m[ip] = true
		}
		out[user] = m
	}
	return out
}

// forget drops everything remembered about users that no longer exist, so a
// deleted account does not hold addresses in memory forever.
func (l *limiter) forget(known map[string]bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for user := range l.seen {
		if !known[user] {
			delete(l.seen, user)
		}
	}
}

// tracker is sing-box's own table of live connections — the one its clash API
// serves over HTTP. It is registered in the instance's context, so the node
// reads it directly: no loopback request every few seconds, no JSON of every
// connection encoded and decoded again, which on a small machine with many
// connections was a real share of the CPU. Nil when nothing is running.
func (e *Engine) tracker() *trafficcontrol.Manager {
	e.mu.Lock()
	running, ctx := e.running, e.ctx
	e.mu.Unlock()
	if !running || ctx == nil {
		return nil
	}
	return service.PtrFromContext[trafficcontrol.Manager](ctx)
}

// connections lists the live connections.
func (e *Engine) connections(context.Context) ([]conn, error) {
	m := e.tracker()
	if m == nil {
		return nil, nil
	}
	live := m.Connections()
	out := make([]conn, 0, len(live))
	for _, t := range live {
		md := t.Metadata
		var src string
		if md.Source.Addr.IsValid() {
			// Unmapped, so one client is one address whether it reached a
			// dual-stack listener over IPv4 or as ::ffff:a.b.c.d.
			src = md.Source.Addr.Unmap().String()
		}
		var dest string
		if md.Destination.Addr.IsValid() {
			dest = md.Destination.Addr.Unmap().String()
		} else if md.Domain != "" {
			// A connection to a name that has not been resolved yet still has a
			// distinct destination; the name stands in for it so the peer count
			// is not silently short.
			dest = md.Domain
		} else {
			dest = md.Destination.Fqdn
		}
		var port string
		if md.Destination.Port != 0 {
			port = strconv.Itoa(int(md.Destination.Port))
		}
		out = append(out, conn{
			ID:       t.ID.String(),
			User:     strings.TrimSpace(md.User),
			IP:       src,
			DestIP:   dest,
			DestPort: port,
		})
	}
	return out, nil
}

// activity summarises what each user is doing, from one sample of the live
// connections. Counts only; see proto.Activity.
func activity(conns []conn) map[string]proto.Activity {
	type sets struct {
		peers, ports map[string]bool
		conns        int
	}
	byUser := map[string]*sets{}
	for _, c := range conns {
		if c.User == "" {
			continue
		}
		s := byUser[c.User]
		if s == nil {
			s = &sets{peers: map[string]bool{}, ports: map[string]bool{}}
			byUser[c.User] = s
		}
		s.conns++
		if c.DestIP != "" {
			s.peers[c.DestIP] = true
		}
		if c.DestPort != "" {
			s.ports[c.DestPort] = true
		}
	}

	out := make(map[string]proto.Activity, len(byUser))
	for user, s := range byUser {
		out[user] = proto.Activity{
			Conns: s.conns, Peers: len(s.peers), Ports: len(s.ports),
		}
	}
	return out
}

func (e *Engine) closeConnection(_ context.Context, id string) error {
	m := e.tracker()
	if m == nil {
		return nil
	}
	uid, err := uuid.FromString(id)
	if err != nil {
		return err
	}
	// Already gone is fine: it closed on its own between the sample and now.
	if t := m.Connection(uid); t != nil {
		return t.Close()
	}
	return nil
}

// EnforceIPLimits closes connections from addresses over a user's limit and
// returns how many distinct addresses each user has left.
//
// Called on a timer rather than per connection: sing-box has no hook that fires
// as one is accepted, and polling a few seconds apart is enough to make a
// shared subscription useless without spending anything per connection.
//
// The limit is per user across both cores: an address counts once whether it
// reached a sing-box inbound or an Xray one. Xray's connections cannot be
// closed one by one, so its share of the verdict is a block on the address.
func (e *Engine) EnforceIPLimits(ctx context.Context) (map[string]int, map[string]proto.Activity, error) {
	conns, err := e.connections(ctx)
	if err != nil {
		return nil, nil, err
	}
	// Sampled before anything is closed: what the panel is shown is what the
	// user was doing, not what was left of it afterwards. sing-box's only:
	// Xray reports addresses, not connections or where they go.
	acts := activity(conns)

	xctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	xconns, err := e.x.conns(xctx)
	if err != nil {
		// Without them the limit is applied to sing-box's connections alone
		// this round, which is better than to none.
		e.log.Debug("read xray connections", "error", err)
	}
	conns = append(conns, xconns...)

	toClose, counts := e.limits.enforce(conns, time.Now())
	if err == nil {
		e.x.setBlocks(xctx, e.limits.overAddrs())
	}
	closed := 0
	for _, id := range toClose {
		if strings.HasPrefix(id, xrayConnPrefix) {
			continue // blocked above
		}
		if err := e.closeConnection(ctx, id); err != nil {
			e.log.Debug("close connection over the address limit", "id", id, "error", err)
		}
		closed++
	}
	if closed > 0 {
		e.log.Info("closed connections over the address limit", "connections", closed)
	}
	return counts, acts, nil
}
