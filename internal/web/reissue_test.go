package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zayvian/veyra-panel/internal/service"
	"github.com/Zayvian/veyra-panel/internal/store"
)

// The 重置链接 button end to end: the old link stops working, the new one
// works, and the table that comes back already shows the new link to copy.
func TestReissueThroughAdminForm(t *testing.T) {
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
	srv, err := New(svc, &fakeChannel{}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()
	session, csrf := loginForTest(t, h)

	get := func(path string) int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec.Code
	}
	if code := get("/sub/" + user.SubToken); code != http.StatusOK {
		t.Fatalf("subscription before reissue: %d", code)
	}

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/users/%d/reissue", user.ID), nil)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("X-CSRF-Token", csrf.Value)
	req.AddCookie(session)
	req.AddCookie(csrf)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("reissue: %d %s", rec.Code, rec.Body)
	}

	after, err := svc.User(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.SubToken == user.SubToken {
		t.Fatal("subscription token unchanged")
	}
	if !strings.Contains(rec.Body.String(), "/sub/"+after.SubToken) {
		t.Fatal("the returned table does not show the new link")
	}
	if strings.Contains(rec.Body.String(), "/sub/"+user.SubToken) {
		t.Fatal("the returned table still shows the old link")
	}
	if code := get("/sub/" + user.SubToken); code != http.StatusNotFound {
		t.Fatalf("old subscription link: %d, want 404", code)
	}
	if code := get("/sub/" + after.SubToken); code != http.StatusOK {
		t.Fatalf("new subscription link: %d", code)
	}
}

// Without the CSRF token the request is refused like every other state change.
func TestReissueRequiresCSRF(t *testing.T) {
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
	srv, err := New(svc, &fakeChannel{}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()
	session, _ := loginForTest(t, h)

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/users/%d/reissue", user.ID), nil)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("reissue accepted without a CSRF token")
	}
	after, err := svc.User(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.SubToken != user.SubToken {
		t.Fatal("a refused request still changed the subscription token")
	}
}
