package web

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/kosje/skysbx-panel/internal/service"
	"github.com/kosje/skysbx-panel/internal/store"
)

// The setup form is available within the window after first boot.
// hardenedServer creates a fresh database with an admin, so /setup
// redirects to /login. To test the open window we need a server with
// no admin — built here from scratch.
func noAdminServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	svc := service.New(st)
	srv, err := New(svc, &fakeChannel{}, slog.New(slog.NewTextHandler(io.Discard, nil)), false)
	if err != nil {
		t.Fatal(err)
	}
	return srv, st
}

// The setup form renders on first boot, when the deadline has not been
// written yet. setupOpen claims it and returns true.
func TestSetupWindowOpenOnFirstBoot(t *testing.T) {
	srv, _ := noAdminServer(t)
	h := srv.Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/setup", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("GET /setup on first boot = %d, want 200", rec.Code)
	}
}

// After the setup window expires, GET /setup returns 403 instead of
// rendering the form.
func TestSetupWindowLocksAfterExpiry(t *testing.T) {
	srv, st := noAdminServer(t)
	// Set the deadline to epoch — already expired.
	if err := st.SetSetting(settingSetupDeadline, "1"); err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/setup", nil))
	if rec.Code != http.StatusForbidden {
		t.Errorf("GET /setup after deadline = %d, want 403", rec.Code)
	}
}

// POST /setup is also locked after the window expires.
func TestSetupWindowLocksPostAfterExpiry(t *testing.T) {
	srv, st := noAdminServer(t)
	if err := st.SetSetting(settingSetupDeadline, "1"); err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/setup", nil))
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST /setup after deadline = %d, want 403", rec.Code)
	}
}

// Once the admin exists, /setup redirects to /login regardless of the
// window.
func TestSetupRedirectsWhenAdminExists(t *testing.T) {
	h := hardenedServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/setup", nil))
	if rec.Code != http.StatusSeeOther {
		t.Errorf("GET /setup with admin = %d, want 303", rec.Code)
	}
}

// A deadline far in the future still allows setup.
func TestSetupWindowOpenBeforeExpiry(t *testing.T) {
	srv, st := noAdminServer(t)
	future := strconv.FormatInt(9999999999, 10) // year 2286
	if err := st.SetSetting(settingSetupDeadline, future); err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/setup", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("GET /setup before deadline = %d, want 200", rec.Code)
	}
}
