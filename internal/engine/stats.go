package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Zayvian/veyra-node/internal/proto"
)

// counters turns sing-box's cumulative per-user totals into the deltas the
// panel expects.
//
// The panel accumulates what it is sent, so it must be sent differences. It
// cannot be sent totals: this process restarts, sing-box's counters start again
// at zero, and a total that goes backwards is indistinguishable from an
// operator resetting someone's usage.
type counters struct {
	mu sync.Mutex
	// reported is the baseline the last acknowledged report was measured
	// against; pending is the reading behind the report now in flight. Both
	// belong to the running instance.
	reported map[string]proto.Usage
	pending  map[string]proto.Usage

	// carry is traffic measured on instances that have since been replaced and
	// not yet acknowledged by the panel. A configuration apply rebuilds sing-box
	// and its counters start again at zero; without this, everything since the
	// last report — up to half a minute for every user on the node — would be
	// thrown away with the old instance on every inbound edit.
	carry map[string]proto.Usage
	// carrySent is the part of carry included in the report now in flight,
	// removed from carry once that report is acknowledged.
	carrySent map[string]proto.Usage

	// gen identifies the instance the baseline belongs to. A reading taken from
	// an instance that has been retired since must not be measured against the
	// new one's baseline: it would be counted a second time.
	gen uint64
}

func newCounters() *counters {
	return &counters{reported: map[string]proto.Usage{}, carry: map[string]proto.Usage{}}
}

// generation is the instance a reading about to be taken will belong to.
func (c *counters) generation() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen
}

// retire closes the books on an instance about to be stopped. final is its last
// reading; whatever it holds beyond the acknowledged baseline moves to carry, to
// be reported alongside the next instance's traffic.
//
// A report may be in flight at this moment. Its share of this instance's
// traffic is inside final too, so it is booked as already sent: if that report
// is acknowledged it comes off carry with the rest of carrySent, and if it is
// not, the next delta rebuilds carrySent from the whole of carry and reports it
// again. Either way it is counted exactly once.
//
// final may be nil when the instance could not be read; the unreported interval
// is then lost, as it always was before carry existed.
func (c *counters) retire(final map[string]proto.Usage) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for name, cur := range final {
		addUsage(c.carry, name, sub(cur, c.reported[name]))
	}
	if c.pending != nil {
		if c.carrySent == nil {
			c.carrySent = map[string]proto.Usage{}
		}
		for name, cur := range c.pending {
			addUsage(c.carrySent, name, sub(cur, c.reported[name]))
		}
	}
	c.reported = map[string]proto.Usage{}
	c.pending = nil
	c.gen++
}

// delta returns the change since the last acknowledged report, plus anything
// carried over from replaced instances. It does not move the baseline — commit
// does, once the report has actually reached the panel. Advancing here instead
// would discard an interval whenever the channel dropped between reading and
// sending, and traffic that is never reported is never billed.
//
// A reading from a generation other than the current one is stale and yields
// nothing; the next tick reads the new instance.
func (c *counters) delta(gen uint64, now map[string]proto.Usage) map[string]proto.Usage {
	c.mu.Lock()
	defer c.mu.Unlock()

	if gen != c.gen {
		// This report carries nothing, so acknowledging it must settle
		// nothing — least of all a carrySent left by a report that failed.
		c.pending, c.carrySent = nil, nil
		return map[string]proto.Usage{}
	}

	out := make(map[string]proto.Usage, len(now)+len(c.carry))
	for name, cur := range now {
		addUsage(out, name, sub(cur, c.reported[name]))
	}
	c.carrySent = make(map[string]proto.Usage, len(c.carry))
	for name, u := range c.carry {
		addUsage(out, name, u)
		c.carrySent[name] = u
	}
	c.pending = now
	return out
}

// commit accepts the reading behind the report that just succeeded.
//
// Names absent from it are dropped with the old map. A user removed and later
// re-added starts from zero, which is right: everything up to the removal has
// already been counted.
func (c *counters) commit() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending != nil {
		c.reported = c.pending
		c.pending = nil
	}
	for name, sent := range c.carrySent {
		left := sub(c.carry[name], sent)
		if left.Up == 0 && left.Down == 0 {
			delete(c.carry, name)
		} else {
			c.carry[name] = left
		}
	}
	c.carrySent = nil
}

// sub is cur minus before. A counter that went backwards means sing-box reset
// it — the user being removed and re-added — so the new value is the whole
// difference rather than a negative one, which the panel refuses anyway.
func sub(cur, before proto.Usage) proto.Usage {
	up, down := cur.Up-before.Up, cur.Down-before.Down
	if up < 0 {
		up = cur.Up
	}
	if down < 0 {
		down = cur.Down
	}
	return proto.Usage{Up: up, Down: down}
}

// addUsage adds u to m[name], leaving no entry for a zero.
func addUsage(m map[string]proto.Usage, name string, u proto.Usage) {
	if u.Up == 0 && u.Down == 0 {
		return
	}
	cur := m[name]
	m[name] = proto.Usage{Up: cur.Up + u.Up, Down: cur.Down + u.Down}
}

// Stats reads the current per-user totals and returns the change since the last
// call. Calling it advances the baseline, so exactly one caller may do so.
func (e *Engine) Stats() (map[string]proto.Usage, *proto.SystemStats, error) {
	// The generation is taken under the same lock as the context, so the
	// reading and the baseline it is measured against are of one instance.
	e.mu.Lock()
	running, ctx := e.running, e.ctx
	gen := e.counters.generation()
	e.mu.Unlock()

	if !running {
		return nil, nil, nil
	}

	totals, err := e.readTotals(ctx)
	if err != nil {
		return nil, nil, err
	}
	return e.counters.delta(gen, totals), systemStats(), nil
}

// StatsReported is called once the panel has acknowledged the last report, and
// is what advances the baseline. Until it is called, the traffic stays pending
// and is included again in the next report.
func (e *Engine) StatsReported() { e.counters.commit() }

// Online lists users with at least one live connection, read from the clash_api
// connection table.
func (e *Engine) Online() ([]string, error) {
	e.mu.Lock()
	running := e.running
	addr := e.apis.clash
	e.mu.Unlock()

	if !running || addr == "" {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

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
			Metadata struct {
				User string `json:"user"`
			} `json:"metadata"`
		} `json:"connections"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("clash api response: %w", err)
	}

	seen := map[string]bool{}
	var out []string
	for _, c := range body.Connections {
		name := strings.TrimSpace(c.Metadata.User)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out, nil
}
