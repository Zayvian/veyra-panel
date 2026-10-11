package hub

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Zayvian/veyra-panel/internal/service"
)

// helloV2 is what a protocol-2 node says.
func helloV2(caps ...string) Hello {
	return Hello{Version: "0.2.0", Proto: ProtoVersion, Caps: caps}
}

// next returns the next frame of any type except ping.
func (n *fakeNode) next(within time.Duration) Envelope {
	n.t.Helper()
	deadline := time.After(within)
	for {
		select {
		case env, ok := <-n.recv:
			if !ok {
				n.t.Fatal("connection closed")
			}
			if env.Type != TypePing {
				return env
			}
		case <-deadline:
			n.t.Fatal("timed out waiting for a frame")
		}
	}
}

// expectNothing fails if a frame of the given type arrives within d.
func (n *fakeNode) expectNothing(msgType string, d time.Duration) {
	n.t.Helper()
	deadline := time.After(d)
	for {
		select {
		case env, ok := <-n.recv:
			if !ok {
				return
			}
			if env.Type == msgType {
				n.t.Fatalf("unexpected %q (id %d)", msgType, env.ID)
			}
		case <-deadline:
			return
		}
	}
}

func usedBy(t *testing.T, h *harness, id int64) int64 {
	t.Helper()
	u, err := h.svc.User(id)
	if err != nil {
		t.Fatal(err)
	}
	return u.TrafficUsed
}

// An older node never hears of protocol 2: no welcome, its stats are billed
// the old way, and nothing is acknowledged.
func TestOlderNodeIsSpokenToAsBefore(t *testing.T) {
	h := newHarness(t)
	h.addInbounds(t)
	u, err := h.svc.CreateUser(service.NewUser{Name: "dave"})
	if err != nil {
		t.Fatal(err)
	}
	n := dialNode(t, h.srv.URL, h.token)
	n.send(TypeHello, 0, Hello{Version: "0.1.0"})
	if first := n.next(3 * time.Second); first.Type != TypeConfig {
		t.Fatalf("first frame to an older node is %q, want config", first.Type)
	}
	n.await(TypeUsers, 3*time.Second)

	n.send(TypeStats, 0, StatsData{Users: map[string]Usage{"dave": {Up: 100, Down: 900}}})
	n.expectNothing(TypeOK, 300*time.Millisecond)
	if got := usedBy(t, h, u.ID); got != 1000 {
		t.Fatalf("traffic_used = %d, want 1000", got)
	}
	if _, known := h.hub.AppliedRev(h.node.ID); known {
		t.Fatal("an older node's config rev is reported as known")
	}
}

// A protocol-2 node is welcomed before anything else, and its config carries
// the rev the panel will compare its state against.
func TestNewNodeIsWelcomedFirst(t *testing.T) {
	h := newHarness(t)
	h.addInbounds(t)
	n := dialNode(t, h.srv.URL, h.token)
	n.send(TypeHello, 0, helloV2(CapStatsAck, CapConfigRev))

	first := n.next(3 * time.Second)
	if first.Type != TypeWelcome {
		t.Fatalf("first frame is %q, want welcome", first.Type)
	}
	var w Welcome
	if err := json.Unmarshal(first.Data, &w); err != nil {
		t.Fatal(err)
	}
	if w.Proto != ProtoVersion || len(w.Caps) != 2 {
		t.Fatalf("welcome = %+v", w)
	}

	var cd ConfigData
	if err := json.Unmarshal(n.await(TypeConfig, 3*time.Second).Data, &cd); err != nil {
		t.Fatal(err)
	}
	want, err := h.svc.NodeConfigRev(h.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cd.Rev == "" || cd.Rev != want {
		t.Fatalf("config rev = %q, want %q", cd.Rev, want)
	}

	// The node's state names the rev it runs.
	n.send(TypeState, 0, StateData{Inbounds: []string{"x"}, AppliedRev: cd.Rev})
	deadline := time.Now().Add(3 * time.Second)
	for {
		if rev, known := h.hub.AppliedRev(h.node.ID); known && rev == cd.Rev {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("applied rev never recorded")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A numbered report is billed once and acknowledged every time it arrives: the
// resend of a report whose ok was lost must be answered, not billed again.
func TestNumberedStatsAreBilledOnce(t *testing.T) {
	h := newHarness(t)
	h.addInbounds(t)
	u, err := h.svc.CreateUser(service.NewUser{Name: "dave"})
	if err != nil {
		t.Fatal(err)
	}
	n := dialNode(t, h.srv.URL, h.token)
	n.send(TypeHello, 0, helloV2(CapStatsAck, CapConfigRev))
	n.await(TypeUsers, 3*time.Second)

	report := func(epoch string, seq uint64, up, down int64) {
		t.Helper()
		n.send(TypeStats, seq, StatsData{Epoch: epoch, Users: map[string]Usage{"dave": {Up: up, Down: down}}})
		if ok := n.await(TypeOK, 3*time.Second); ok.ID != seq {
			t.Fatalf("ok id = %d, want %d", ok.ID, seq)
		}
	}

	report("e1", 1, 100, 900)
	if got := usedBy(t, h, u.ID); got != 1000 {
		t.Fatalf("after report 1: %d, want 1000", got)
	}
	report("e1", 1, 100, 900) // its ok was lost; the node sends it again
	if got := usedBy(t, h, u.ID); got != 1000 {
		t.Fatalf("a repeated report was billed again: %d, want 1000", got)
	}
	report("e1", 2, 10, 90)
	if got := usedBy(t, h, u.ID); got != 1100 {
		t.Fatalf("after report 2: %d, want 1100", got)
	}
	report("e1", 1, 100, 900) // older than the last one: also a repeat
	if got := usedBy(t, h, u.ID); got != 1100 {
		t.Fatalf("an old report was billed: %d, want 1100", got)
	}
	// A restarted node has a new epoch and numbers from 1 again.
	report("e2", 1, 1, 9)
	if got := usedBy(t, h, u.ID); got != 1110 {
		t.Fatalf("after the restart: %d, want 1110", got)
	}
	// A report with no user traffic is still acknowledged.
	n.send(TypeStats, 2, StatsData{Epoch: "e2", Users: map[string]Usage{}})
	if ok := n.await(TypeOK, 3*time.Second); ok.ID != 2 {
		t.Fatalf("empty report ok id = %d", ok.ID)
	}
}

// The panel restarts between committing a report and the node hearing so. The
// resend reaches a new process, which must still recognise it.
func TestRepeatIsRecognisedAcrossAPanelRestart(t *testing.T) {
	h := newHarness(t)
	h.addInbounds(t)
	u, err := h.svc.CreateUser(service.NewUser{Name: "dave"})
	if err != nil {
		t.Fatal(err)
	}
	n := dialNode(t, h.srv.URL, h.token)
	n.send(TypeHello, 0, helloV2(CapStatsAck))
	n.await(TypeUsers, 3*time.Second)
	n.send(TypeStats, 7, StatsData{Epoch: "e", Users: map[string]Usage{"dave": {Up: 1, Down: 1}}})
	n.await(TypeOK, 3*time.Second)

	// A second hub over the same database is the restarted panel.
	h2 := New(h.svc, h.hub.log)
	dup, err := h2.svc.RecordTrafficReport(h.node.ID, "e", 7,
		map[string]service.Usage{"dave": {Up: 1, Down: 1}})
	if err != nil || !dup {
		t.Fatalf("resend after restart: duplicate=%v err=%v, want a duplicate", dup, err)
	}
	if got := usedBy(t, h, u.ID); got != 2 {
		t.Fatalf("traffic_used = %d, want 2", got)
	}
}
