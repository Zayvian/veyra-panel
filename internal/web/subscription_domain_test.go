package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kosje/skysbx-panel/internal/service"
	"github.com/kosje/skysbx-panel/internal/store"
)

func TestSubscriptionDomain(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st)
	user, err := svc.CreateUser(service.NewUser{Name: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(svc, &fakeChannel{}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetSubscriptionDomain("sub.example.com")
	for _, path := range []string{"/login", "/setup", "/users", "/api/v1/node/connect", "/"} {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "https://sub.example.com"+path, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("subscription host exposes %s: %d", path, rec.Code)
		}
	}
	for _, host := range []string{"panel.example.com", "sub.example.com"} {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "https://"+host+"/sub/"+user.SubToken+"?format=html", nil))
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "https://sub.example.com/sub/"+user.SubToken) {
			t.Fatalf("wrong subscription URL: %s", rec.Body)
		}
	}
	for _, fragment := range []bool{false, true} {
		req := httptest.NewRequest("GET", "https://panel.example.com/users", nil)
		if fragment {
			req.Header.Set("HX-Request", "true")
		}
		rec := httptest.NewRecorder()
		srv.renderUsers(rec, req, 200)
		if !strings.Contains(rec.Body.String(), `data-sub-url="https://sub.example.com/sub/`+user.SubToken) {
			t.Fatalf("wrong copy URL in fragment=%v", fragment)
		}
	}
}

func TestDomainValidationAndRedirect(t *testing.T) {
	for _, value := range []string{"https://sub.example.com", "sub.example.com:443", "*.example.com", "sub.example.com/a", "sub..com", "-sub.example.com", "sub.example.com\nX:bad"} {
		if _, err := NormalizeDomain(value); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
	a, err := NewAutoTLS("panel.example.com", "", t.TempDir(), "sub.example.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"sub.example.com", "sub.example.com:80", "SUB.EXAMPLE.COM"} {
		rec := httptest.NewRecorder()
		a.ChallengeAndRedirect().ServeHTTP(rec, httptest.NewRequest("GET", "http://"+host+"/sub/token?format=clash", nil))
		if rec.Header().Get("Location") != "https://sub.example.com/sub/token?format=clash" {
			t.Fatalf("wrong redirect %s", rec.Header().Get("Location"))
		}
	}
	rec := httptest.NewRecorder()
	a.ChallengeAndRedirect().ServeHTTP(rec, httptest.NewRequest("GET", "http://evil.example.com/login", nil))
	if rec.Header().Get("Location") != "https://panel.example.com/login" {
		t.Fatal("untrusted redirect host")
	}
}
