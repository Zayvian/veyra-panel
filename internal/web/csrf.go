// Package web — CSRF protection.
//
// Cross-site request forgery is the attack where a third-party page causes
// a browser to send a state-changing request to the panel using a session
// cookie it already holds. The session cookie is HttpOnly so it cannot be
// read from script, but the browser will still attach it to a request the
// user was tricked into making — that is the whole point of cookies.
//
// `SameSite=Lax` blocks cross-site POST, which closes the easy version of
// the attack. The remaining surface is same-site: a form submitted from a
// page hosted on the same origin (a phishing page on a subdomain, a
// misconfigured proxy) still carries the cookie, and that is the gap this
// middleware closes.
//
// A CSRF token is a per-session secret that:
//   - the server hands out only to authenticated requests,
//   - cannot be read by JavaScript on a different origin (it is set in
//     HTML and read back into JavaScript; the same-origin policy is what
//     stops a third-party page from doing the same),
//   - is required in every state-changing request.
//
// The token is bound to the session: stealing one cookie without the other
// is not enough.
package web

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const csrfCookie = "skysbx_csrf"

// csrfTTL is how long a CSRF token stays valid. 24 hours is long enough to
// keep an edit form open overnight and short enough that a leaked token
// has a definite shelf life. The TTL is part of the signed payload, not a
// server-side lookup, so the verifier is allocation-free.
const csrfTTL = 24 * time.Hour

// csrfToken issues and verifies CSRF tokens. The key is held for the
// lifetime of the process; rotating it logs everyone out in the same way
// rotating the session key does, and there is no upside to doing it more
// often than that.
type csrfToken struct {
	key []byte
}

func newCSRF(key []byte) *csrfToken {
	return &csrfToken{key: key}
}

func newCSRFKey() []byte {
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		// No entropy is a fatal startup condition, not a runtime one.
		panic("skysbx: cannot read random bytes: " + err.Error())
	}
	return k
}

// issue writes a CSRF token cookie bound to the session's username. The
// token is `HMAC(key, username|issuedAt)`; the same username is required at
// verify time, so a stolen cookie value for a different user (or for an
// unauthenticated request that has no username) does not validate.
func (c *csrfToken) issue(w http.ResponseWriter, username string, secure bool) {
	if username == "" {
		return
	}
	issued := strconv.FormatInt(time.Now().Unix(), 10)
	payload := username + "|" + issued
	value := base64.RawURLEncoding.EncodeToString([]byte(payload)) +
		"." + c.sign(payload)
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookie,
		Value:    value,
		Path:     "/",
		Expires:  time.Now().Add(csrfTTL),
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// clear drops the cookie on logout, so a logged-out browser does not carry
// a still-valid token in its cookie jar.
func (c *csrfToken) clear(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// read extracts the CSRF token from either the X-CSRF-Token header (used by
// htmx fetch requests) or the `csrf` form field. Headers come first so a
// page that does both is unambiguous.
func (c *csrfToken) read(r *http.Request) string {
	if v := r.Header.Get("X-CSRF-Token"); v != "" {
		return v
	}
	return r.FormValue("csrf")
}

// verify checks that the request's CSRF token is valid for the given
// session username. It returns false on every failure mode (no token,
// malformed token, wrong MAC, wrong user, stale timestamp, no
// authenticated user). The MAC comparison is constant-time so a guessed
// suffix does not leak by timing; the username comparison is also
// constant-time so a token signed for a different user does not give a
// different answer depending on how many characters happened to match.
func (c *csrfToken) verify(r *http.Request, username string) bool {
	if username == "" {
		return false
	}
	value := c.read(r)
	if value == "" {
		return false
	}
	encoded, mac, ok := strings.Cut(value, ".")
	if !ok {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return false
	}
	payload := string(raw)
	want := c.sign(payload)
	if !hmac.Equal([]byte(mac), []byte(want)) {
		return false
	}
	gotUser, issuedStr, ok := strings.Cut(payload, "|")
	if !ok {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(gotUser), []byte(username)) != 1 {
		return false
	}
	issued, err := strconv.ParseInt(issuedStr, 10, 64)
	if err != nil {
		return false
	}
	if time.Since(time.Unix(issued, 0)) > csrfTTL {
		return false
	}
	return true
}

func (c *csrfToken) sign(payload string) string {
	m := hmac.New(sha256.New, c.key)
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// csrfValue returns the current cookie value (or "" if none), so a
// template can render it into a hidden field or hand it to JavaScript.
// Reads only; the cookie is set by `issue` and cleared by `clear`.
func (c *csrfToken) csrfValue(r *http.Request) string {
	ck, err := r.Cookie(csrfCookie)
	if err != nil {
		return ""
	}
	return ck.Value
}
