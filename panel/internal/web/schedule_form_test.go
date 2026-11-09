package web

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestExpiryFormUsesExactLocalMinuteAndKeepsLegacyDates(t *testing.T) {
	for input, want := range map[string]string{
		"2026-10-04T12:34": "2026-10-04 12:34",
		"2026-10-04":       "2026-10-04 23:59",
	} {
		form := url.Values{"expires_at": {input}}
		r := httptest.NewRequest("POST", "/users", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		got, err := expiryFromForm(r)
		if err != nil || got == nil {
			t.Fatalf("expiryFromForm(%q): %v", input, err)
		}
		if got.Local().Format("2006-01-02 15:04") != want {
			t.Fatalf("expiryFromForm(%q) = %s, want %s", input, got.Local(), want)
		}
	}
}

func TestResetDayCreatedIsTheDayAfterCreation(t *testing.T) {
	form := url.Values{"reset_day": {"created"}}
	r := httptest.NewRequest("POST", "/users", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for created, want := range map[string]int{
		"2026-11-09 05:14": 10,
		"2026-01-31 23:00": 1, // month end rolls over to the 1st
		"2026-02-28 00:00": 1,
		"2028-02-28 00:00": 29, // leap year
		"2026-04-30 12:00": 1,
	} {
		at, err := time.ParseInLocation("2006-01-02 15:04", created, time.Local)
		if err != nil {
			t.Fatal(err)
		}
		if got := resetDayFromForm(r, at); got != want {
			t.Errorf("created %s: reset day %d, want %d", created, got, want)
		}
	}
	if got, want := resetDayFromForm(r, time.Time{}), time.Now().AddDate(0, 0, 1).Day(); got != want {
		t.Errorf("new user: reset day %d, want %d", got, want)
	}
}
