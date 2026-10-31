package probe

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zayvian/veyra-panel/internal/hub"
	"github.com/Zayvian/veyra-panel/internal/store"
)

func TestJudge(t *testing.T) {
	// Node 1 has two inbounds, node 2 one.
	targets := []target{
		{key: "in-10", nodeID: 1, inbound: 10},
		{key: "ctl-1", nodeID: 1},
		{key: "in-11", nodeID: 1, inbound: 11},
		{key: "in-20", nodeID: 2, inbound: 20},
		{key: "ctl-2", nodeID: 2},
	}
	for _, tc := range []struct {
		name   string
		abroad map[string]bool
		china  map[string][]bool
		want   map[int64]Verdict
	}{{
		name:   "everything reachable",
		abroad: map[string]bool{"in-10": true, "in-11": true, "ctl-1": true, "in-20": true, "ctl-2": true},
		china: map[string][]bool{"in-10": {true}, "in-11": {true}, "ctl-1": {true},
			"in-20": {true}, "ctl-2": {true}},
		want: map[int64]Verdict{10: VerdictOK, 11: VerdictOK, 20: VerdictOK},
	}, {
		name:   "one port blocked, the rest of the address fine",
		abroad: map[string]bool{"in-10": true, "in-11": true, "ctl-1": true},
		china:  map[string][]bool{"in-10": {false}, "in-11": {true}, "ctl-1": {true}},
		want:   map[int64]Verdict{10: VerdictPort, 11: VerdictOK},
	}, {
		// The case that started this: ping works, every TCP port times out.
		name:   "control port blocked too",
		abroad: map[string]bool{"in-20": true, "ctl-2": true},
		china:  map[string][]bool{"in-20": {false}, "ctl-2": {false}},
		want:   map[int64]Verdict{20: VerdictTCP},
	}, {
		name:   "ssh closed everywhere, but every inbound blocked",
		abroad: map[string]bool{"in-10": true, "in-11": true, "ctl-1": false},
		china:  map[string][]bool{"in-10": {false}, "in-11": {false}, "ctl-1": {false}},
		want:   map[int64]Verdict{10: VerdictTCP, 11: VerdictTCP},
	}, {
		// One inbound and no usable control: cannot tell port from address.
		name:   "single inbound, no control",
		abroad: map[string]bool{"in-20": true, "ctl-2": false},
		china:  map[string][]bool{"in-20": {false}, "ctl-2": {false}},
		want:   map[int64]Verdict{20: VerdictPort},
	}, {
		name:   "down from abroad is not a block",
		abroad: map[string]bool{"in-20": false, "ctl-2": true},
		china:  map[string][]bool{"in-20": {false}, "ctl-2": {true}},
		want:   map[int64]Verdict{20: VerdictDown},
	}, {
		name:   "probes disagree",
		abroad: map[string]bool{"in-20": true},
		china:  map[string][]bool{"in-20": {true, false}},
		want:   map[int64]Verdict{20: VerdictPartial},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got := Judge(targets, tc.abroad, tc.china)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for id, v := range tc.want {
				if got[id].verdict != v {
					t.Errorf("inbound %d: %q, want %q", id, got[id].verdict, v)
				}
			}
		})
	}
}

func TestBadVerdictNeedsConfirming(t *testing.T) {
	var st Status
	now := time.Now()
	st.confirm(VerdictOK, now)
	if st.Verdict != VerdictOK {
		t.Fatalf("ok not shown at once: %+v", st)
	}
	st.confirm(VerdictTCP, now)
	if st.Verdict != VerdictOK {
		t.Fatalf("one bad round flipped the verdict: %+v", st)
	}
	st.confirm(VerdictTCP, now)
	if st.Verdict != VerdictTCP {
		t.Fatalf("two bad rounds did not: %+v", st)
	}
	st.confirm(VerdictOK, now)
	if st.Verdict != VerdictOK {
		t.Fatalf("recovery not shown at once: %+v", st)
	}
}

func TestTargetsSkipWhatTCPCannotJudge(t *testing.T) {
	nodes := map[int64]*store.Node{
		1: {ID: 1, Address: "jp.example.com", Enabled: true},
		2: {ID: 2, Address: "cn.example.com", Enabled: true}, // the probe
	}
	inbounds := []*store.Inbound{
		{ID: 1, NodeID: 1, Protocol: store.ProtoVLESS, Port: 443, Enabled: true},
		{ID: 2, NodeID: 1, Protocol: store.ProtoHysteria2, Port: 8443, Enabled: true},
		{ID: 3, NodeID: 1, Protocol: store.ProtoTUIC, Port: 9443, Enabled: true},
		{ID: 4, NodeID: 1, Protocol: store.ProtoAnyTLS, Port: 8443, Enabled: true, Address: "relay.example.com"},
		{ID: 5, NodeID: 1, Protocol: store.ProtoAnyTLS, Port: 9443, Enabled: false},
		{ID: 6, NodeID: 2, Protocol: store.ProtoVLESS, Port: 443, Enabled: true},
	}
	var keys []string
	for _, tg := range Targets(nodes, map[int64]bool{2: true}, inbounds) {
		keys = append(keys, tg.key+"="+tg.addr)
	}
	if got := strings.Join(keys, " "); got != "in-1=jp.example.com:443 ctl-1=jp.example.com:22" {
		t.Fatalf("targets: %s", got)
	}
}

type fakeProber struct{ open map[string]bool }

func (f fakeProber) CanProbe(id int64) bool { return id == 2 }

func (f fakeProber) Probe(_ context.Context, _ int64, targets []hub.ProbeTarget, _ time.Duration) ([]hub.ProbeResult, error) {
	var out []hub.ProbeResult
	for _, t := range targets {
		out = append(out, hub.ProbeResult{Key: t.Key, OK: f.open[t.Key]})
	}
	return out, nil
}

// A full round against a real store: a node whose every TCP port is closed to
// the probe and open to the panel ends up marked as TCP-blocked, on the second
// round.
func TestRound(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	jp := &store.Node{Name: "jp", Address: "jp.example.com", Enabled: true}
	cn := &store.Node{Name: "cn", Address: "cn.example.com", Enabled: true}
	for _, n := range []*store.Node{jp, cn} {
		if err := st.CreateNode(n); err != nil {
			t.Fatal(err)
		}
	}
	in := &store.Inbound{NodeID: jp.ID, Tag: "vless-jp", Protocol: store.ProtoVLESS, Port: 443,
		Config: "{}", Client: "{}", Enabled: true}
	if err := st.CreateInbound(in); err != nil {
		t.Fatal(err)
	}
	if err := st.SetProbeNode(cn.ID, true); err != nil {
		t.Fatal(err)
	}

	m := New(st, fakeProber{open: map[string]bool{}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	m.dial = func(context.Context, string) error { return nil }
	ctx := context.Background()
	if err := m.Round(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Inbound(in.ID); ok {
		t.Fatal("verdict shown after a single bad round")
	}
	if err := m.Round(ctx); err != nil {
		t.Fatal(err)
	}
	got, ok := m.Inbound(in.ID)
	if !ok || got.Verdict != VerdictTCP || got.Probes != 1 {
		t.Fatalf("status: %+v %v", got, ok)
	}
	if v, _ := m.Node(jp.ID, []*store.Inbound{in}); v != VerdictTCP {
		t.Fatalf("node summary %q", v)
	}
	if _, n := m.LastRun(); n != 1 {
		t.Fatalf("%d probes answered", n)
	}

	// Unmarking the probe forgets everything rather than showing stale news.
	if err := st.SetProbeNode(cn.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := m.Round(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Inbound(in.ID); ok {
		t.Fatal("status kept with no probes")
	}
}
