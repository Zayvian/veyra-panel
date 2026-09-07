package web

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	sessionCookie = "skysbx_session"
	sessionMaxAge = 12 * time.Hour
)

// Sessions are a signed cookie rather than a server-side table: there is one
// administrator, so there is nothing to look up. The cookie carries the
// username, an expiry and a session generation, HMAC'd with a key generated
// on first run.
//
// The session generation is what makes logout meaningful. A signed cookie
// that is only checked for expiry and signature cannot be invalidated by the
// server — the operator logging out is telling the browser to delete the
// cookie, but a cookie that was copied before the logout, or that the browser
// refuses to delete, stays valid until it expires. The generation closes that
// gap: it is a counter in the `settings` table, bumped on every logout (and,
// when one is added, on every password change). A cookie is only valid if its
// generation matches the current one, so a bumped generation invalidates every
// cookie issued before it, whether the browser still holds them or not.
//
// A cookie — rather than a token in local storage — is also what keeps the UI
// swappable: the same scheme works unchanged if these handlers are ever
// replaced by a JSON API and a single-page frontend.
type sessions struct {
	key []byte
}

func newSessions(key []byte) *sessions { return &sessions{key: key} }

func newSessionKey() []byte {
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		panic("skysbx: cannot read random bytes: " + err.Error())
	}
	return k
}

func (s *sessions) sign(payload string) string {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// issue writes a session cookie carrying the current session generation.
// The generation is passed in by the caller (the Server), which reads it
// from the settings table; keeping it a parameter lets session.go stay
// free of a database dependency.
func (s *sessions) issue(w http.ResponseWriter, username string, secure bool, generation int64) {
	exp := time.Now().Add(sessionMaxAge)
	payload := username + "|" + strconv.FormatInt(exp.Unix(), 10) + "|" + strconv.FormatInt(generation, 10)
	value := base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + s.sign(payload)

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    value,
		Path:     "/",
		Expires:  exp,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *sessions) clear(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
}

// user returns the signed-in username, or an error describing why not. The
// caller passes the current session generation; a cookie carrying any other
// generation — one issued before a logout bumped it — is treated as invalid.
func (s *sessions) user(r *http.Request, generation int64) (string, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return "", fmt.Errorf("no session cookie")
	}
	encoded, sig, ok := strings.Cut(c.Value, ".")
	if !ok {
		return "", fmt.Errorf("malformed session cookie")
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("malformed session payload")
	}
	payload := string(raw)

	// Compare with hmac.Equal, not ==: a byte-at-a-time comparison would leak
	// how much of a forged signature was correct.
	if !hmac.Equal([]byte(s.sign(payload)), []byte(sig)) {
		return "", fmt.Errorf("bad session signature")
	}

	// The payload is `username|expiry|generation`. Reading generation by
	// splitting on `|` from the right keeps the username free to contain a
	// literal pipe without ambiguity.
	username, rest, ok := strings.Cut(payload, "|")
	if !ok {
		return "", fmt.Errorf("malformed session payload")
	}
	expStr, genStr, ok := strings.Cut(rest, "|")
	if !ok {
		// A cookie issued by a pre-generation build: no generation in the
		// payload. Treat it as invalid rather than guessing — a stale
		// cookie from before the upgrade is exactly the kind of thing the
		// generation exists to invalidate.
		return "", fmt.Errorf("session has no generation")
	}
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		return "", fmt.Errorf("malformed session expiry")
	}
	if time.Now().Unix() > exp {
		return "", fmt.Errorf("session expired")
	}
	gen, err := strconv.ParseInt(genStr, 10, 64)
	if err != nil {
		return "", fmt.Errorf("malformed session generation")
	}
	if gen != generation {
		return "", fmt.Errorf("session generation changed")
	}
	return username, nil
}