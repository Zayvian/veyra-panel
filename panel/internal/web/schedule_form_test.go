package web

import (
	"net/http"
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

func TestResetByCreationTakesCreationTimeOfDay(t *testing.T) {
	post := func(v url.Values) *http.Request {
		r := httptest.NewRequest("POST", "/users", strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return r
	}
	created := time.Date(2026, 11, 9, 13, 20, 45, 0, time.Local)
	// The time field is ignored: 「按创建日」 means the creation moment.
	d, h, m, err := resetScheduleFromForm(post(url.Values{"reset_day": {"created"}, "reset_time": {"00:00"}}), created)
	if err != nil || d != 9 || h != 13 || m != 20 {
		t.Fatalf("by creation: %d %02d:%02d %v, want 9 13:20", d, h, m, err)
	}
	// A new user is created now.
	now := time.Now()
	d, h, m, _ = resetScheduleFromForm(post(url.Values{"reset_day": {"created"}}), time.Time{})
	if d != now.Day() || h != now.Hour() || (m != now.Minute() && m != now.Add(-time.Minute).Minute()) {
		t.Fatalf("new user: %d %02d:%02d, want about %s", d, h, m, now.Format("2 15:04"))
	}
	// A fixed day still uses the time the form gives.
	d, h, m, err = resetScheduleFromForm(post(url.Values{"reset_day": {"15"}, "reset_time": {"08:30"}}), created)
	if err != nil || d != 15 || h != 8 || m != 30 {
		t.Fatalf("fixed day: %d %02d:%02d %v, want 15 08:30", d, h, m, err)
	}
}
