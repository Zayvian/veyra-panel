package link

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Zayvian/veyra-node/internal/proto"
)

// A protocol-2 panel: answers the hello with a welcome offering caps.
func welcomed(t *testing.T, p *fakePanel, caps ...string) proto.Hello {
	t.Helper()
	var h proto.Hello
	if err := json.Unmarshal(p.await(proto.TypeHello, 3*time.Second).Data, &h); err != nil {
		t.Fatal(err)
	}
	p.push(proto.TypeWelcome, 0, proto.Welcome{Proto: 2, Caps: caps})
	return h
}

func stats(t *testing.T, p *fakePanel) (uint64, proto.StatsData) {
	t.Helper()
	env := p.await(proto.TypeStats, 3*time.Second)
	var sd proto.StatsData
	if err := json.Unmarshal(env.Data, &sd); err != nil {
		t.Fatal(err)
	}
	return env.ID, sd
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestHelloOffersProtocol2(t *testing.T) {
	p := newFakePanel(t)
	start(t, p, &fakeEngine{})
	h := welcomed(t, p)
	if h.Proto != proto.ProtoVersion || len(h.Caps) != 2 {
		t.Fatalf("hello = %+v, want proto 2 with caps", h)
	}
}

// Under stats_ack a report is done when the panel says so, not when it is
// written; until then the same report is resent and nothing new is read.
func TestStatsWaitForTheAck(t *testing.T) {
	saved, online := statsEvery, onlineEvery
	t.Cleanup(func() { restore(saved, online) })
	setIntervals(30*time.Millisecond, time.Hour)

	p := newFakePanel(t)
	e := &fakeEngine{stats: map[string]proto.Usage{"alice": {Up: 1, Down: 2}}}
	start(t, p, e)
	welcomed(t, p, proto.CapStatsAck)

	id, first := stats(t, p)
	if id != 1 || first.Epoch == "" {
		t.Fatalf("first report: id=%d epoch=%q, want id 1 with an epoch", id, first.Epoch)
	}
	// Unacknowledged: resent unchanged, and the engine is read only once.
	for range 3 {
		again, sd := stats(t, p)
		if again != id || sd.Epoch != first.Epoch || sd.Users["alice"] != first.Users["alice"] {
			t.Fatalf("resend changed: id=%d %+v, want id=%d %+v", again, sd, id, first)
		}
	}
	if e.acks() != 0 || e.reads() != 1 {
		t.Fatalf("before ok: acks=%d reads=%d, want 0 and 1", e.acks(), e.reads())
	}

	// An ok for some other number settles nothing.
	p.push(proto.TypeOK, 99, nil)
	p.push(proto.TypeOK, id, nil)
	waitFor(t, "the ack", func() bool { return e.acks() == 1 })

	next, sd := stats(t, p)
	for next == id { // a resend already in flight when the ok landed
		next, sd = stats(t, p)
	}
	if next != id+1 || sd.Epoch != first.Epoch {
		t.Fatalf("next report: id=%d epoch=%q, want id=%d, same epoch", next, sd.Epoch, id+1)
	}
	if e.acks() != 1 {
		t.Fatalf("acks = %d, want 1", e.acks())
	}
}

// The ok was lost with the connection. After reconnecting the node sends the
// very same report — same epoch, same number, same traffic — so the panel can
// tell it has seen it, and the engine's baseline moves exactly once.
func TestUnackedReportSurvivesAReconnect(t *testing.T) {
	saved, online := statsEvery, onlineEvery
	t.Cleanup(func() { restore(saved, online) })
	setIntervals(30*time.Millisecond, time.Hour)

	p := newFakePanel(t)
	e := &fakeEngine{stats: map[string]proto.Usage{"alice": {Up: 5, Down: 7}}}
	start(t, p, e)
	welcomed(t, p, proto.CapStatsAck)
	id, first := stats(t, p)

	p.dropConnection()
	welcomed(t, p, proto.CapStatsAck)
	again, sd := stats(t, p)
	if again != id || sd.Epoch != first.Epoch || sd.Users["alice"] != first.Users["alice"] {
		t.Fatalf("after reconnect: id=%d %+v, want the same report id=%d %+v", again, sd, id, first)
	}
	if e.reads() != 1 {
		t.Fatalf("reads = %d: a new reading was taken while a report was outstanding", e.reads())
	}
	p.push(proto.TypeOK, id, nil)
	waitFor(t, "the ack", func() bool { return e.acks() == 1 })
}

// An older panel never says welcome. The node reports the old way: no number,
// no epoch, done once written.
func TestOlderPanelGetsTheOldStats(t *testing.T) {
	saved, online := statsEvery, onlineEvery
	t.Cleanup(func() { restore(saved, online) })
	setIntervals(30*time.Millisecond, time.Hour)

	p := newFakePanel(t)
	e := &fakeEngine{stats: map[string]proto.Usage{"alice": {Up: 1, Down: 1}}}
	start(t, p, e)
	p.await(proto.TypeHello, 3*time.Second)
	id, sd := stats(t, p)
	if id != 0 || sd.Epoch != "" {
		t.Fatalf("legacy report: id=%d epoch=%q, want neither", id, sd.Epoch)
	}
	waitFor(t, "the write to count as delivery", func() bool { return e.acks() >= 1 })
}

// A panel offering only config_rev: stats stay on the old path.
func TestCapabilitiesAreUsedOnlyWhenBothSidesAgree(t *testing.T) {
	saved, online := statsEvery, onlineEvery
	t.Cleanup(func() { restore(saved, online) })
	setIntervals(30*time.Millisecond, time.Hour)

	p := newFakePanel(t)
	e := &fakeEngine{stats: map[string]proto.Usage{"alice": {Up: 1, Down: 1}}}
	start(t, p, e)
	welcomed(t, p, proto.CapConfigRev, "something_new")
	waitFor(t, "a report", func() bool { return e.reads() >= 1 })
	id, sd := stats(t, p)
	if id != 0 || sd.Epoch != "" {
		t.Fatalf("report without stats_ack: id=%d epoch=%q", id, sd.Epoch)
	}
}

func TestStateReportsTheAppliedRev(t *testing.T) {
	p := newFakePanel(t)
	e := &fakeEngine{}
	start(t, p, e)
	welcomed(t, p, proto.CapConfigRev)
	time.Sleep(50 * time.Millisecond) // let the welcome land before the config

	state := func() proto.StateData {
		t.Helper()
		var st proto.StateData
		if err := json.Unmarshal(p.await(proto.TypeState, 3*time.Second).Data, &st); err != nil {
			t.Fatal(err)
		}
		return st
	}

	p.push(proto.TypeConfig, 1, proto.ConfigData{Rev: "rev-1", Config: json.RawMessage(`{}`)})
	if st := state(); st.AppliedRev != "rev-1" {
		t.Fatalf("applied_rev = %q, want rev-1", st.AppliedRev)
	}

	// Refused: the node keeps running rev-1, and says so.
	e.mu.Lock()
	e.configErr = errNotToday
	e.mu.Unlock()
	p.push(proto.TypeConfig, 2, proto.ConfigData{Rev: "rev-2", Config: json.RawMessage(`{}`)})
	if st := state(); st.AppliedRev != "rev-1" {
		t.Fatalf("after a refused config applied_rev = %q, want rev-1", st.AppliedRev)
	}
}

func TestStateCarriesNoRevForAnOlderPanel(t *testing.T) {
	p := newFakePanel(t)
	start(t, p, &fakeEngine{})
	p.await(proto.TypeHello, 3*time.Second)
	p.push(proto.TypeConfig, 1, proto.ConfigData{Rev: "rev-1", Config: json.RawMessage(`{}`)})
	var st proto.StateData
	if err := json.Unmarshal(p.await(proto.TypeState, 3*time.Second).Data, &st); err != nil {
		t.Fatal(err)
	}
	if st.AppliedRev != "" {
		t.Fatalf("applied_rev = %q sent to a panel that never asked", st.AppliedRev)
	}
}
