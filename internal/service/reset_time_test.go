package service

import (
	"testing"
	"time"
)

func TestMonthlyScheduleHonoursTheSelectedClock(t *testing.T) {
	now := time.Date(2026, time.October, 4, 10, 30, 0, 0, time.Local)
	if got := LastScheduledResetAt(4, 12, 15, now); !got.Equal(time.Date(2026, time.September, 4, 12, 15, 0, 0, time.Local)) {
		t.Fatalf("last reset = %s", got)
	}
	if got := NextScheduledResetAt(4, 12, 15, now); !got.Equal(time.Date(2026, time.October, 4, 12, 15, 0, 0, time.Local)) {
		t.Fatalf("next reset = %s", got)
	}
	// At the exact minute, the current cycle has begun and the next one is next
	// month. This matches the expiry cutoff's exact-instant semantics.
	atReset := time.Date(2026, time.October, 4, 12, 15, 0, 0, time.Local)
	if got := NextScheduledResetAt(4, 12, 15, atReset); !got.Equal(time.Date(2026, time.November, 4, 12, 15, 0, 0, time.Local)) {
		t.Fatalf("next reset at boundary = %s", got)
	}
}

func TestNodeDashboardPeriodResetsWithoutDeletingHistory(t *testing.T) {
	svc := resetFixture(t)
	node, _, err := svc.CreateNode("tokyo", "jp.example.com", "JP")
	if err != nil {
		t.Fatal(err)
	}
	node.StatsResetDay, node.StatsResetHour, node.StatsResetMinute = 4, 12, 15
	if err := svc.UpdateNode(node); err != nil {
		t.Fatal(err)
	}
	last := time.Date(2026, time.September, 4, 12, 15, 0, 0, time.Local)
	if _, err := svc.Store().DB().Exec(`UPDATE nodes SET stats_up=100, stats_down=200, stats_last_reset_at=? WHERE id=?`, last.Unix(), node.ID); err != nil {
		t.Fatal(err)
	}
	n, err := svc.RunDueNodeStats(time.Date(2026, time.October, 4, 12, 15, 0, 0, time.Local))
	if err != nil || n != 1 {
		t.Fatalf("node resets = %d, err = %v", n, err)
	}
	got, err := svc.Node(node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.StatsUp != 0 || got.StatsDown != 0 {
		t.Fatalf("period counter left at %d/%d", got.StatsUp, got.StatsDown)
	}
}
