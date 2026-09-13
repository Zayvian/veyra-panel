package web

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zayvian/veyra-panel/internal/service"
	"github.com/Zayvian/veyra-panel/internal/store"
)

func TestPanelAccessPathHidesAdminRoutesButKeepsMachineEndpoints(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	svc := service.New(st)
	if err := svc.SetAdmin("admin", "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}
	user, err := svc.CreateUser(service.NewUser{Name: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(svc, &fakeChannel{}, slog.New(slog.NewTextHandler(io.Discard, nil)), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.setAccessSettings("/skypanel", ""); err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	for _, path := range []string{"/", "/login", "/users", "/static/htmx.min.js"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "https://panel.example.com"+path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("hidden path %s = %d, want 404", path, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "https://panel.example.com/skypanel/login", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("prefixed login = %d, want 200", rec.Code)
	}
	for _, want := range []string{`hx-post="/skypanel/login"`, `src="/skypanel/static/htmx.min.js"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("prefixed login is missing %s", want)
		}
	}

	// Subscription clients and existing nodes never learn the management path.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "https://panel.example.com/sub/"+user.SubToken+"?format=clash", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("subscription became hidden with panel path: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "https://panel.example.com/skypanel/sub/"+user.SubToken, nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("prefixed subscription = %d, want 404", rec.Code)
	}
}

func TestPanelAccessRootRedirectAndValidation(t *testing.T) {
	for _, value := range []string{"skypanel", "/", "/a/", "/a//b", "/a?b", "/a/../b", "/中文"} {
		if _, err := normalizePanelAccessPath(value); err == nil {
			t.Errorf("accepted invalid access path %q", value)
		}
	}
	if got, err := normalizePanelAccessPath("/sky-panel/v2"); err != nil || got != "/sky-panel/v2" {
		t.Errorf("valid access path = %q, %v", got, err)
	}
	for _, value := range []string{"example.com", "ftp://example.com", "https://user@example.com"} {
		if _, err := normalizeRootRedirect(value); err == nil {
			t.Errorf("accepted invalid root redirect %q", value)
		}
	}
}
