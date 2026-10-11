package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

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
const ipGrace = 5 * time.Minute

// limiter tracks which addresses are using which account, and closes the ones
// over the line.
type limiter struct {
	mu     sync.Mutex
	limits map[string]int             // user -> max distinct addresses
	seen   map[string]map[string]slot // user -> address -> when it was first and last seen
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
		for _, c := range conns {
			if c.User == user && over[c.IP] {
				close = append(close, c.ID)
			}
		}
	}
	return close, counts
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

// connections reads the live connection list from the node's own clash API.
func (e *Engine) connections(ctx context.Context) ([]conn, error) {
	e.mu.Lock()
	running, addr := e.running, e.apis.clash
	e.mu.Unlock()
	if !running || addr == "" {
		return nil, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+addr+"/connections", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("clash api: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("clash api returned %s", resp.Status)
	}

	var body struct {
		Connections []struct {
			ID       string `json:"id"`
			Metadata struct {
				User            string `json:"user"`
				SourceIP        string `json:"sourceIP"`
				DestinationIP   string `json:"destinationIP"`
				DestinationPort string `json:"destinationPort"`
				Host            string `json:"host"`
			} `json:"metadata"`
		} `json:"connections"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("clash api response: %w", err)
	}

	out := make([]conn, 0, len(body.Connections))
	for _, c := range body.Connections {
		dest := strings.TrimSpace(c.Metadata.DestinationIP)
		if dest == "" {
			// A connection to a name that has not been resolved yet still has a
			// distinct destination; the host stands in for it so the peer count
			// is not silently short.
			dest = strings.TrimSpace(c.Metadata.Host)
		}
		out = append(out, conn{
			ID:       c.ID,
			User:     strings.TrimSpace(c.Metadata.User),
			IP:       strings.TrimSpace(c.Metadata.SourceIP),
			DestIP:   dest,
			DestPort: strings.TrimSpace(c.Metadata.DestinationPort),
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

func (e *Engine) closeConnection(ctx context.Context, id string) error {
	e.mu.Lock()
	addr := e.apis.clash
	e.mu.Unlock()
	if addr == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		"http://"+addr+"/connections/"+id, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// EnforceIPLimits closes connections from addresses over a user's limit and
// returns how many distinct addresses each user has left.
//
// Called on a timer rather than per connection: sing-box has no hook that fires
// as one is accepted, and polling a few seconds apart is enough to make a
// shared subscription useless without spending anything per connection.
func (e *Engine) EnforceIPLimits(ctx context.Context) (map[string]int, map[string]proto.Activity, error) {
	conns, err := e.connections(ctx)
	if err != nil {
		return nil, nil, err
	}
	// Sampled before anything is closed: what the panel is shown is what the
	// user was doing, not what was left of it afterwards.
	acts := activity(conns)

	toClose, counts := e.limits.enforce(conns, time.Now())
	for _, id := range toClose {
		if err := e.closeConnection(ctx, id); err != nil {
			e.log.Debug("close connection over the address limit", "id", id, "error", err)
		}
	}
	if len(toClose) > 0 {
		e.log.Info("closed connections over the address limit", "connections", len(toClose))
	}
	return counts, acts, nil
}
