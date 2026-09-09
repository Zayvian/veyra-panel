package service

import (
	"github.com/kosje/skysbx-panel/internal/store"
	"testing"
)

func TestNodeRateParsing(t *testing.T) {
	for value, want := range map[string]int64{"": 1000, "0": 0, "0.1": 100, "1.001": 1001, "100": 100000} {
		got, err := ParseNodeRate(value)
		if err != nil || got != want {
			t.Errorf("%q: %d %v", value, got, err)
		}
	}
	for _, value := range []string{"-1", "NaN", "Inf", "1e2", "0.0001", "100.001", "9999999999999999999999"} {
		if _, err := ParseNodeRate(value); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
}
func TestChineseNamesAndNodeRates(t *testing.T) {
	svc := newTestService(t)
	n, _, err := svc.CreateNode("香港无限流量", "hk.example.com", "HK", 100)
	if err != nil {
		t.Fatal(err)
	}
	u, err := svc.CreateUser(NewUser{Name: "alice", TrafficLimit: 100})
	if err != nil {
		t.Fatal(err)
	}
	in, err := svc.CreateInbound(n.ID, InboundSpec{Protocol: store.ProtoShadowsocks, Port: 8388, Tag: "香港专线 01"})
	if err != nil {
		t.Fatal(err)
	}
	if in.Tag != "香港专线 01" {
		t.Fatal(in.Tag)
	}
	if err := svc.RecordTraffic(n.ID, map[string]Usage{"alice": {Up: 200, Down: 300}}); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.User(u.ID)
	if got.TrafficUsed != 50 || got.TrafficUp != 20 || got.TrafficDown != 30 {
		t.Fatalf("wrong billing: %+v", got)
	}
	n.RateMilli = 0
	spy := &spyNotifier{}
	svc.SetNotifier(spy)
	if err := svc.UpdateNode(n); err != nil {
		t.Fatal(err)
	}
	if len(spy.config) != 0 {
		t.Fatal("rate change restarted node")
	}
	if err := svc.RecordTraffic(n.ID, map[string]Usage{"alice": {Down: 10000}}); err != nil {
		t.Fatal(err)
	}
	got, _ = svc.User(u.ID)
	if got.TrafficUsed != 50 {
		t.Fatal("free traffic billed")
	}
	// Validation still applies through the service rather than only the UI.
	n.RateMilli = -1
	if err := svc.UpdateNode(n); err == nil {
		t.Fatal("negative rate accepted")
	}
	n.RateMilli = 1000
	n.Name = "日本无限流量"
	if err := svc.UpdateNode(n); err != nil {
		t.Fatal(err)
	}
	renamed, _ := svc.Store().Inbound(in.ID)
	if renamed.Tag != "ss-日本无限流量" {
		t.Fatal(renamed.Tag)
	}
}
