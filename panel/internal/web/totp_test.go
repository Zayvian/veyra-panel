package web

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// codeAt is an authenticator app: RFC 6238 with the panel's parameters.
func codeAt(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatal(err)
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(at.Unix()/30))
	m := hmac.New(sha1.New, key)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[off:off+4])&0x7fffffff)%1_000_000)
}

// Set up from the settings page, then sign in with it, then turn it off.
func TestTwoStepLoginThroughForms(t *testing.T) {
	svc, c := newAdminClient(t)

	login := func(code string) int {
		form := url.Values{"username": {"admin"}, "password": {"correct-horse-battery"}, "code": {code}}
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		c.h.ServeHTTP(rec, req)
		return rec.Code
	}
	loginPage := func() string {
		rec := httptest.NewRecorder()
		c.h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))
		return rec.Body.String()
	}
	if strings.Contains(loginPage(), `name="code"`) {
		t.Fatal("login page asks for a code while two-step login is off")
	}

	rec := c.do(http.MethodPost, "/settings/2fa/begin", url.Values{})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `src="data:image/png;base64,`) {
		t.Fatalf("begin: %d %s", rec.Code, rec.Body)
	}
	secret, _, err := svc.PendingTOTP("admin")
	if err != nil || secret == "" || !strings.Contains(rec.Body.String(), secret) {
		t.Fatalf("pending secret %q not shown: %v", secret, err)
	}

	if rec := c.do(http.MethodPost, "/settings/2fa/enable", url.Values{"code": {"000000"}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("wrong code: %d", rec.Code)
	}
	now := time.Now()
	rec = c.do(http.MethodPost, "/settings/2fa/enable", url.Values{"code": {codeAt(t, secret, now)}})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "恢复码，只显示这一次") {
		t.Fatalf("enable: %d %s", rec.Code, rec.Body)
	}

	if !strings.Contains(loginPage(), `name="code"`) {
		t.Fatal("login page does not ask for the code")
	}
	if code := login(""); code != http.StatusUnauthorized {
		t.Fatalf("password alone: %d, want 401", code)
	}
	// The code used to enable is spent; the next step's code signs in.
	next := now.Add(30 * time.Second)
	if code := login(codeAt(t, secret, next)); code != http.StatusSeeOther {
		t.Fatalf("password and code: %d, want a redirect", code)
	}

	// Turning it off needs a code; a session alone is not enough.
	if rec := c.do(http.MethodPost, "/settings/2fa/disable", url.Values{"code": {""}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("disable without a code: %d", rec.Code)
	}
	if st, _ := svc.TOTPStatus(); !st.Enabled {
		t.Fatal("disabled without a code")
	}
	if err := svc.ResetTOTP(); err != nil {
		t.Fatal(err)
	}
	if code := login(""); code != http.StatusSeeOther {
		t.Fatalf("after reset: %d", code)
	}
}
