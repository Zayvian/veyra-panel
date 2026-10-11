package service

import (
	"reflect"
	"testing"

	"github.com/Zayvian/veyra-panel/internal/store"
)

// Every node reports every thirty seconds. If each report pushed the full user
// list to every node, the panel would do work proportional to the square of
// the node count for reports that change nothing. Only a report that takes
// someone over their limit changes who may connect.
func TestTrafficNotifiesOnlyWhenALimitIsCrossed(t *testing.T) {
	svc := newTestService(t)
	n, _, err := svc.CreateNode("tokyo", "jp.example.com", "JP")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateUser(NewUser{Name: "alice", TrafficLimit: 100}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateUser(NewUser{Name: "bob"}); err != nil { // unlimited
		t.Fatal(err)
	}
	if _, err := svc.CreateInbound(n.ID, InboundSpec{Protocol: store.ProtoShadowsocks, Port: 8388}); err != nil {
		t.Fatal(err)
	}
	spy := &spyNotifier{}
	svc.SetNotifier(spy)

	report := func(name string, down int64) {
		t.Helper()
		if err := svc.RecordTraffic(n.ID, map[string]Usage{name: {Down: down}}); err != nil {
			t.Fatal(err)
		}
	}

	report("alice", 60)
	report("bob", 1<<30)
	if spy.users != 0 {
		t.Fatalf("reports under the limit notified %d times", spy.users)
	}

	report("alice", 40) // exactly at the limit: at is over
	if spy.users != 1 {
		t.Fatalf("crossing the limit notified %d times, want 1", spy.users)
	}

	// Already over: the node no longer has her, and the push already happened.
	report("alice", 10)
	if spy.users != 1 {
		t.Fatalf("a report past the limit notified again (%d)", spy.users)
	}
}

// A node is told the address limits of the users it serves and no one else's:
// the list goes to rented machines, and a limit for a name the node has never
// heard of does nothing but disclose it.
func TestIPLimitsAreScopedToTheNode(t *testing.T) {
	svc := newTestService(t)
	a, _, err := svc.CreateNode("tokyo", "jp.example.com", "JP")
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := svc.CreateNode("paris", "fr.example.com", "FR")
	if err != nil {
		t.Fatal(err)
	}
	onA, err := svc.CreateInbound(a.ID, InboundSpec{Protocol: store.ProtoShadowsocks, Port: 8388})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateInbound(b.ID, InboundSpec{Protocol: store.ProtoShadowsocks, Port: 8388}); err != nil {
		t.Fatal(err)
	}

	alice, err := svc.CreateUser(NewUser{Name: "alice", IPLimit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetUserInbounds(alice.ID, []int64{onA.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateUser(NewUser{Name: "bob", IPLimit: 3}); err != nil { // every node
		t.Fatal(err)
	}
	if _, err := svc.CreateUser(NewUser{Name: "carol"}); err != nil { // no limit
		t.Fatal(err)
	}

	for _, tc := range []struct {
		node int64
		want map[string]int
	}{
		{a.ID, map[string]int{"alice": 2, "bob": 3}},
		{b.ID, map[string]int{"bob": 3}},
	} {
		got, err := svc.UserIPLimits(tc.node)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("node %d: limits = %v, want %v", tc.node, got, tc.want)
		}
	}
}
