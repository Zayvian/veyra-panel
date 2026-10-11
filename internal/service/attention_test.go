package service

import (
	"testing"
	"time"

	"github.com/Zayvian/veyra-panel/internal/store"
)

func TestNeedsAttention(t *testing.T) {
	now := time.Date(2026, time.October, 11, 12, 0, 0, 0, time.Local)
	at := func(d time.Duration) *time.Time { x := now.Add(d); return &x }
	day := 24 * time.Hour

	users := []*store.User{
		{Name: "fine", Enabled: true, ExpiresAt: at(30 * day), TrafficLimit: 100, TrafficUsed: 10},
		{Name: "unlimited", Enabled: true},
		{Name: "soon", Enabled: true, ExpiresAt: at(3*day + time.Hour)},
		{Name: "today", Enabled: true, ExpiresAt: at(5 * time.Hour)},
		{Name: "lapsed", Enabled: true, ExpiresAt: at(-2 * day)},
		{Name: "just-lapsed", Enabled: true, ExpiresAt: at(-time.Minute)},
		{Name: "long-gone", Enabled: true, ExpiresAt: at(-40 * day)},
		{Name: "heavy", Enabled: true, TrafficLimit: 100, TrafficUsed: 93},
		{Name: "empty", Enabled: true, TrafficLimit: 100, TrafficUsed: 100},
		{Name: "monthly", Enabled: true, TrafficLimit: 100, TrafficUsed: 95, ResetDay: 15},
		{Name: "switched-off", Enabled: false, ExpiresAt: at(day)},
	}
	got := NeedsAttention(users, now)

	want := []struct {
		name, reason string
		lapsed       bool
	}{
		// Soonest first: the oldest lapse, then the newest, then the
		// exhausted allowance (stopped now), then upcoming expiries, then
		// heavy use.
		{"lapsed", "已到期 2 天", true},
		{"just-lapsed", "刚刚到期", true},
		{"empty", "流量已用完", true},
		{"today", "5 小时后到期", false},
		{"soon", "3 天后到期", false},
		{"heavy", "流量已用 93%", false},
		{"monthly", "流量已用 95%，10-15 重置", false},
	}
	if len(got) != len(want) {
		names := []string{}
		for _, a := range got {
			names = append(names, a.User.Name+": "+a.Reason)
		}
		t.Fatalf("got %d entries %v, want %d", len(got), names, len(want))
	}
	for i, w := range want {
		if got[i].User.Name != w.name || got[i].Reason != w.reason || got[i].Lapsed != w.lapsed {
			t.Errorf("entry %d = %s %q lapsed=%v, want %s %q lapsed=%v",
				i, got[i].User.Name, got[i].Reason, got[i].Lapsed, w.name, w.reason, w.lapsed)
		}
	}
}
