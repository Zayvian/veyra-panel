package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func newTestCSRF(t *testing.T) *csrfToken {
	t.Helper()
	return newCSRF([]byte("test-key-do-not-use-in-prod-12345678"))
}

// readToken is a small helper used by several tests: a CSRF token value,
// the one written by `issue`, can be read back as a cookie by
// httptest.ResponseRecorder only if we go through `rec.Result()` — the
// recorder's Headers() do not include the Set-Cookie.
func readToken(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == csrfCookie {
			return ck.Value
		}
	}
	t.Fatal("no CSRF cookie was set")
	return ""
}

// A request that does not even have a session user is rejected. This is
// the gate that says: a CSRF token without a session is meaningless.
func TestCSRF_RejectsWhenUnauthenticated(t *testing.T) {
	c := newTestCSRF(t)
	r := httptest.NewRequest(http.MethodPost, "/x", nil)
	if c.verify(r, "") {
		t.Error("verified for empty username; the gate at the top of verify is missing")
	}
}

// A request that has a session but no token is rejected. This is the
// most important test: it is the regression we are closing.
func TestCSRF_RejectsWhenTokenMissing(t *testing.T) {
	c := newTestCSRF(t)
	r := httptest.NewRequest(http.MethodPost, "/x", nil)
	if c.verify(r, "alice") {
		t.Error("verified a request with no token; CSRF is not enforced")
	}
}

// A request that has a session and a valid token verifies.
func TestCSRF_AcceptsValidToken(t *testing.T) {
	c := newTestCSRF(t)
	rec := httptest.NewRecorder()
	c.issue(rec, "alice", false)
	token := readToken(t, rec)

	r := httptest.NewRequest(http.MethodPost, "/x", nil)
	r.Header.Set("X-CSRF-Token", token)
	if !c.verify(r, "alice") {
		t.Error("rejected a valid token")
	}
}

// A token signed for a different user is rejected. This is the line that
// makes the token not just "an opaque value" but a value bound to *this*
// session: an attacker who tricks the user into a CSRF cannot reuse a
// token they fetched under their own session.
func TestCSRF_RejectsTokenForDifferentUser(t *testing.T) {
	c := newTestCSRF(t)
	rec := httptest.NewRecorder()
	c.issue(rec, "alice", false)
	token := readToken(t, rec)

	r := httptest.NewRequest(http.MethodPost, "/x", nil)
	r.Header.Set("X-CSRF-Token", token)
	if c.verify(r, "bob") {
		t.Error("alice's token verified for bob; tokens are not bound to the user")
	}
}

// A token whose MAC has been tampered with is rejected.
func TestCSRF_RejectsTamperedSignature(t *testing.T) {
	c := newTestCSRF(t)
	rec := httptest.NewRecorder()
	c.issue(rec, "alice", false)
	token := readToken(t, rec)
	encoded, mac, ok := strings.Cut(token, ".")
	if !ok {
		t.Fatal("malformed token")
	}
	tampered := encoded + "." + mac[:len(mac)-1] + "A"
	if tampered == token {
		t.Fatal("the test did not change the MAC")
	}

	r := httptest.NewRequest(http.MethodPost, "/x", nil)
	r.Header.Set("X-CSRF-Token", tampered)
	if c.verify(r, "alice") {
		t.Error("a tampered MAC verified; the integrity check is broken")
	}
}

// A token older than the TTL is rejected. The test signs a payload with
// a past timestamp by hand — the verifier does not trust the timestamp
// in the cookie, only one that produces a valid MAC.
func TestCSRF_RejectsExpiredToken(t *testing.T) {
	c := newTestCSRF(t)
	past := time.Now().Add(-25 * time.Hour).Unix()
	payload := "alice|" + strconv.FormatInt(past, 10)
	mac := base64.RawURLEncoding.EncodeToString(hmacSHA256(c.key, payload))
	value := base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + mac

	r := httptest.NewRequest(http.MethodPost, "/x", nil)
	r.Header.Set("X-CSRF-Token", value)
	if c.verify(r, "alice") {
		t.Error("a 25-hour-old token verified; the freshness window is broken")
	}
}

// A token issued at exactly the TTL boundary is still accepted. The
// comparison uses `>`, not `>=`, so the boundary is inclusive on the
// valid side.
func TestCSRF_AcceptsTokenAtTTLBoundary(t *testing.T) {
	c := newTestCSRF(t)
	justFresh := time.Now().Add(-csrfTTL + time.Minute).Unix()
	payload := "alice|" + strconv.FormatInt(justFresh, 10)
	mac := base64.RawURLEncoding.EncodeToString(hmacSHA256(c.key, payload))
	value := base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + mac

	r := httptest.NewRequest(http.MethodPost, "/x", nil)
	r.Header.Set("X-CSRF-Token", value)
	if !c.verify(r, "alice") {
		t.Error("a token just inside the TTL was rejected; the boundary is wrong")
	}
}

// The X-CSRF-Token header is read before the form field, so a page that
// sets both (uncommon but possible) is unambiguous.
func TestCSRF_HeaderPreferredOverForm(t *testing.T) {
	c := newTestCSRF(t)
	rec := httptest.NewRecorder()
	c.issue(rec, "alice", false)
	token := readToken(t, rec)

	form := url.Values{"csrf": {"not-the-right-value"}}
	r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-CSRF-Token", token)
	if !c.verify(r, "alice") {
		t.Error("valid header token was rejected when form also had a value")
	}
}

// A token in the form field (no header) is also accepted.
func TestCSRF_FormFieldAccepted(t *testing.T) {
	c := newTestCSRF(t)
	rec := httptest.NewRecorder()
	c.issue(rec, "alice", false)
	token := readToken(t, rec)

	form := url.Values{"csrf": {token}}
	r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if !c.verify(r, "alice") {
		t.Error("valid form token was rejected")
	}
}

// `clear` produces a cookie whose MaxAge is -1, the standard
// "delete the cookie" signal. This is what `postLogout` calls.
func TestCSRF_ClearRemovesCookie(t *testing.T) {
	c := newTestCSRF(t)
	rec := httptest.NewRecorder()
	c.clear(rec, true)
	var found *http.Cookie
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == csrfCookie {
			found = ck
			break
		}
	}
	if found == nil {
		t.Fatal("clear did not set a cookie at all")
	}
	if found.MaxAge >= 0 {
		t.Errorf("MaxAge = %d, want < 0 (the browser must delete it)", found.MaxAge)
	}
	if !found.Secure {
		t.Error("clear did not honor the secure flag")
	}
}

// `issue` does not write a cookie for an empty username. A misconfigured
// caller passing "" would otherwise set a token that no session can
// verify.
func TestCSRF_IssueSkipsEmptyUsername(t *testing.T) {
	c := newTestCSRF(t)
	rec := httptest.NewRecorder()
	c.issue(rec, "", false)
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == csrfCookie && ck.Value != "" {
			t.Errorf("issue wrote a cookie for empty username: %q", ck.Value)
		}
	}
}

// `csrfValue` returns the current cookie value, or "" if the request
// does not carry one. This is what templates use to embed the token in a
// hidden field.
func TestCSRF_CSRFValue(t *testing.T) {
	c := newTestCSRF(t)
	rec := httptest.NewRecorder()
	c.issue(rec, "alice", false)
	token := readToken(t, rec)

	// The cookie the recorder wrote is also what the next request will
	// see. We attach it to the request directly to exercise the read
	// path that templates use.
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.AddCookie(&http.Cookie{Name: csrfCookie, Value: token})
	if got := c.csrfValue(r); got != token {
		t.Errorf("csrfValue = %q, want %q", got, token)
	}

	// No cookie at all → empty string, not an error.
	r = httptest.NewRequest(http.MethodGet, "/x", nil)
	if got := c.csrfValue(r); got != "" {
		t.Errorf("csrfValue on bare request = %q, want \"\"", got)
	}
}

func hmacSHA256(key []byte, payload string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(payload))
	return m.Sum(nil)
}
