package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// A logged-in POST that carries no CSRF token is rejected by the
// requireCSRF middleware with a 403. This is the end-to-end version of
// TestCSRF_RejectsWhenTokenMissing: it goes through the real handler
// stack (auth → requireCSRF → createUser) rather than calling verify
// directly, so it proves the middleware is actually wired to the routes.
func TestCSRFMiddlewareRejectsStateChangingWithoutToken(t *testing.T) {
	h := hardenedServer(t)

	// A POST /users with a valid session but no token. The session has
	// to be established first (login issues it), so we do a login,
	// capture the session cookie, and replay it with a forged body.
	sessionCookie, _ := loginForTest(t, h)

	form := url.Values{"name": {"tester"}}
	r := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(sessionCookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusForbidden {
		t.Errorf("POST /users without a CSRF token = %d, want 403", rec.Code)
	}
}

// With a valid CSRF token, the same request goes through. This is the
// companion to the test above: it proves the middleware accepts a real
// token, not just that it rejects a missing one — otherwise a bug that
// blocked *every* POST would pass the first test and break the panel.
func TestCSRFMiddlewareAcceptsValidToken(t *testing.T) {
	h := hardenedServer(t)
	sessionCookie, csrfCookie := loginForTest(t, h)

	form := url.Values{"name": {"tester"}}
	r := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(sessionCookie)
	r.AddCookie(csrfCookie)
	r.Header.Set("X-CSRF-Token", csrfCookie.Value)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code == http.StatusForbidden {
		t.Error("POST /users with a valid CSRF token was rejected (403)")
	}
}

// loginForTest does a real POST /login and returns the session cookie
// and the CSRF cookie that the server issued. These are then replayed by
// the caller as a logged-in client.
//
// The login form itself is not CSRF-protected (it is the very request that
// establishes the session), so this needs no token of its own.
func loginForTest(t *testing.T, h http.Handler) (*http.Cookie, *http.Cookie) {
	t.Helper()
	form := url.Values{"username": {"admin"}, "password": {"correct-horse-battery"}}
	r := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	var session, csrf *http.Cookie
	for _, ck := range rec.Result().Cookies() {
		switch ck.Name {
		case sessionCookie:
			session = ck
		case csrfCookie:
			csrf = ck
		}
	}
	if session == nil {
		t.Fatal("login did not set a session cookie")
	}
	if csrf == nil {
		t.Fatal("login did not set a CSRF cookie")
	}
	return session, csrf
}