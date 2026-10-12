package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zayvian/veyra-panel/internal/service"
	"github.com/Zayvian/veyra-panel/internal/store"
)

// The Clash subscription names the panel as where its rule lists come from:
// the subscription origin, and a path the panel answers on that host.
func TestRuleFilesAreServedBesideSubscriptions(t *testing.T) {
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
	h := srv.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "https://sub.example.com/sub/"+user.SubToken+"?format=clash", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("subscription: %d", rec.Code)
	}
	// alice has no nodes, so no lists; the route itself is what matters.
	for path, want := range map[string]int{
		"https://sub.example.com/sub/rules/nonsense.srs":   http.StatusNotFound,
		"https://panel.example.com/sub/rules/geoip-cn.srs": http.StatusNotFound, // wrong host
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != want {
			t.Errorf("%s: %d, want %d", path, rec.Code, want)
		}
	}
	if strings.Contains(rec.Body.String(), "raw.githubusercontent.com") {
		t.Error("the subscription sends clients to GitHub directly")
	}
}
