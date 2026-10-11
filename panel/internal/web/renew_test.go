package web

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zayvian/veyra-panel/internal/service"
	"github.com/Zayvian/veyra-panel/internal/store"
)

// The 续费 button end to end: the form row opens, submitting it brings a
// lapsed account back, and without a CSRF token nothing changes.
func TestRenewThroughAdminForm(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	svc := service.New(st)
	if err := svc.SetAdmin("admin", "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-48 * time.Hour)
	user, err := svc.CreateUser(service.NewUser{Name: "alice", ExpiresAt: &past})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(svc, &fakeChannel{}, slog.New(slog.NewTextHandler(io.Discard, nil)), false)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()
	session, csrf := loginForTest(t, h)

	// The form row.
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/users/%d/renew", user.ID), nil)
	req.Header.Set("HX-Request", "true")
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `name="days"`) {
		t.Fatalf("renew form: %d %s", rec.Code, rec.Body)
	}

	post := func(withCSRF bool, form url.Values) int {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/users/%d/renew", user.ID),
			strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		req.AddCookie(session)
		if withCSRF {
			req.Header.Set("X-CSRF-Token", csrf.Value)
			req.AddCookie(csrf)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := post(false, url.Values{"days": {"30"}}); code == http.StatusOK {
		t.Fatal("renewal accepted without a CSRF token")
	}
	if u, _ := svc.User(user.ID); u.Active(time.Now()) {
		t.Fatal("a refused request still renewed the account")
	}

	if code := post(true, url.Values{"days": {"abc"}}); code != http.StatusBadRequest {
		t.Fatalf("non-numeric days: %d, want 400", code)
	}

	if code := post(true, url.Values{"days": {"30"}, "reset_traffic": {"1"}}); code != http.StatusOK {
		t.Fatalf("renew: %d", code)
	}
	u, err := svc.User(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !u.Active(time.Now()) {
		t.Fatal("renewed account is not active")
	}
	if left := time.Until(*u.ExpiresAt); left < 29*24*time.Hour || left > 31*24*time.Hour {
		t.Fatalf("expiry %v is not about 30 days out", u.ExpiresAt)
	}
}
