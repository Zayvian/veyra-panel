package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// A session cookie issued under one generation is rejected once the
// generation has been bumped. This is the whole point of the feature: a
// copied cookie that survives a logout must stop authenticating.
func TestSessionGenerationInvalidatesOldCookie(t *testing.T) {
	h := hardenedServer(t)

	// Log in and capture the session cookie.
	sessionCookie, _ := loginForTest(t, h)

	// The cookie is valid now.
	if code := authedRequestCode(t, h, sessionCookie, http.MethodGet, "/users"); code == http.StatusSeeOther {
		t.Fatal("session cookie was rejected before logout")
	}

	// Log out. This clears the browser cookie and bumps the generation.
	logoutRec := httptest.NewRecorder()
	lo := httptest.NewRequest(http.MethodPost, "/logout", nil)
	lo.AddCookie(sessionCookie)
	lo.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(logoutRec, lo)

	// The old cookie is no longer accepted: the generation changed.
	if code := authedRequestCode(t, h, sessionCookie, http.MethodGet, "/users"); code != http.StatusSeeOther {
		t.Errorf("old session cookie still authenticates after logout (got %d, want 303 redirect)", code)
	}
}

// The generation is cached in the Server; a logout through the same Server
// updates the cache so a freshly logged-in session is accepted immediately
// after a logout.
func TestSessionGenerationReLoginWorks(t *testing.T) {
	h := hardenedServer(t)

	// Log out (bumps generation).
	logoutRec := httptest.NewRecorder()
	lo := httptest.NewRequest(http.MethodPost, "/logout", nil)
	h.ServeHTTP(logoutRec, lo)

	// Log back in; the new cookie carries the new generation.
	sessionCookie, _ := loginForTest(t, h)
	if code := authedRequestCode(t, h, sessionCookie, http.MethodGet, "/users"); code == http.StatusSeeOther {
		t.Error("fresh session cookie after re-login was rejected")
	}
}

// authedRequestCode performs a request with the given session cookie and
// returns the response status code. A GET to a protected page returns 200
// when authenticated, 303 (redirect to login) when not.
func authedRequestCode(t *testing.T, h http.Handler, sessionCookie *http.Cookie, method, path string) int {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)
	r.AddCookie(sessionCookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec.Code
}