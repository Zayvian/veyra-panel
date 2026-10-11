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

type adminClient struct {
	t       *testing.T
	h       http.Handler
	session *http.Cookie
	csrf    *http.Cookie
}

func newAdminClient(t *testing.T) (*service.Service, *adminClient) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	svc := service.New(st)
	if err := svc.SetAdmin("admin", "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}
	srv, err := New(svc, &fakeChannel{}, slog.New(slog.NewTextHandler(io.Discard, nil)), false)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()
	session, csrf := loginForTest(t, h)
	return svc, &adminClient{t: t, h: h, session: session, csrf: csrf}
}

// do sends an htmx request as the logged-in admin, with the CSRF token.
func (c *adminClient) do(method, path string, form url.Values) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("HX-Request", "true")
	req.Header.Set("X-CSRF-Token", c.csrf.Value)
	req.AddCookie(c.session)
	req.AddCookie(c.csrf)
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	return rec
}

func TestPlansThroughAdminForms(t *testing.T) {
	svc, c := newAdminClient(t)

	rec := c.do(http.MethodPost, "/plans", url.Values{
		"name": {"月付 100G"}, "days": {"30"}, "traffic_limit_gb": {"100"},
		"ip_limit": {"3"}, "reset_day": {"created"}, "note": {"¥30"},
	})
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), "月付 100G") {
		t.Fatalf("create plan: %d %s", rec.Code, rec.Body)
	}
	plans, err := svc.Plans()
	if err != nil || len(plans) != 1 {
		t.Fatalf("plans: %v %v", plans, err)
	}
	p := plans[0]
	if p.Days != 30 || p.TrafficLimit != 100<<30 || p.IPLimit != 3 || p.ResetDay != store.PlanResetOnCreation {
		t.Fatalf("stored plan: %+v", p)
	}

	// The users page offers it, carrying its values for the form.
	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	req.AddCookie(c.session)
	rec = httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, `data-days="30"`) || !strings.Contains(body, `data-reset="created"`) {
		t.Fatalf("users page lacks the plan option: %s", body)
	}

	// The edit row shows the stored values.
	rec = c.do(http.MethodGet, fmt.Sprintf("/plans/%d/edit", p.ID), nil)
	if !strings.Contains(rec.Body.String(), `value="30"`) {
		t.Fatalf("edit row: %s", rec.Body)
	}
	rec = c.do(http.MethodPost, fmt.Sprintf("/plans/%d", p.ID), url.Values{"name": {"季付"}, "days": {"90"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body)
	}
	if got, _ := svc.Plan(p.ID); got.Name != "季付" || got.Days != 90 || got.TrafficLimit != 0 {
		t.Fatalf("updated plan: %+v", got)
	}

	if rec := c.do(http.MethodDelete, fmt.Sprintf("/plans/%d", p.ID), nil); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d", rec.Code)
	}
	if plans, _ := svc.Plans(); len(plans) != 0 {
		t.Fatal("plan not deleted")
	}
}

// 有效天数 sets the expiry from now; an explicit date takes precedence.
func TestCreateUserWithDays(t *testing.T) {
	svc, c := newAdminClient(t)

	if rec := c.do(http.MethodPost, "/users", url.Values{"name": {"alice"}, "days": {"30"}}); rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	if rec := c.do(http.MethodPost, "/users", url.Values{
		"name": {"bob"}, "days": {"30"}, "expires_at": {"2030-01-02T03:04"},
	}); rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	if rec := c.do(http.MethodPost, "/users", url.Values{"name": {"carol"}, "days": {"-3"}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("negative days: %d", rec.Code)
	}

	users, err := svc.Users()
	if err != nil || len(users) != 2 {
		t.Fatalf("users: %v %v", users, err)
	}
	for _, u := range users {
		switch u.Name {
		case "alice":
			if u.ExpiresAt == nil {
				t.Fatal("alice has no expiry")
			}
			if left := time.Until(*u.ExpiresAt); left < 29*24*time.Hour || left > 31*24*time.Hour {
				t.Fatalf("alice expires %v, want about 30 days out", u.ExpiresAt)
			}
		case "bob":
			want := time.Date(2030, 1, 2, 3, 4, 0, 0, time.Local)
			if u.ExpiresAt == nil || !u.ExpiresAt.Equal(want) {
				t.Fatalf("bob expires %v, want %v", u.ExpiresAt, want)
			}
		}
	}
}
