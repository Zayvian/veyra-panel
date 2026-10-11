package web

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
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
