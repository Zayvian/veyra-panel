package web

import (
	"testing"
	"time"
)

func TestLoadTimezone(t *testing.T) {
	loc, err := loadTimezone("")
	if err != nil || loc.String() != DefaultTimezone {
		t.Fatalf("unset zone = %v, %v; want %s", loc, err, DefaultTimezone)
	}
	if loc, err := loadTimezone(timezoneServer); err != nil || loc != serverLocal {
		t.Fatalf("server zone = %v, %v", loc, err)
	}
	if _, err := loadTimezone("Mars/Olympus"); err == nil {
		t.Fatal("unknown zone accepted")
	}
	for _, c := range timezoneChoices {
		if _, err := loadTimezone(c.Value); err != nil {
			t.Errorf("choice %s: %v", c.Value, err)
		}
	}
}

// The case that started this: 22:35 in Beijing is 14:35 UTC, and a one-day
// plan has to read as 22:35 the next day, not 14:35.
func TestBeijingExpiryDisplay(t *testing.T) {
	loc, _ := loadTimezone("Asia/Shanghai")
	created := time.Date(2026, 10, 30, 14, 35, 0, 0, time.UTC)
	got := created.In(loc).AddDate(0, 0, 1).In(loc).Format("2006-01-02 15:04")
	if got != "2026-10-31 22:35" {
		t.Fatalf("expiry = %s", got)
	}
}
