package web

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Zayvian/veyra-panel/internal/service"
	"github.com/Zayvian/veyra-panel/internal/store"
)

type domainHarness struct {
	svc      *service.Service
	srv      *Server
	c        *adminClient
	dir      string
	restarts atomic.Int32
}

func newDomainHarness(t *testing.T, d PanelDomains, dns map[string][]string) *domainHarness {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "skysbx.db"))
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
	h := &domainHarness{svc: svc, srv: srv, dir: dir}
	srv.SetPanelDomains(d, dir, func() { h.restarts.Add(1) })
	srv.lookupHost = func(_ context.Context, name string) ([]string, error) {
		if a, ok := dns[name]; ok {
			return a, nil
		}
		return nil, errors.New("no such host")
	}
	handler := srv.Handler()
	session, csrf := loginForTest(t, handler)
	h.c = &adminClient{t: t, h: handler, session: session, csrf: csrf}
	return h
}

func (h *domainHarness) saved(t *testing.T) PanelDomains {
	t.Helper()
	d, err := ResolveDomains(h.svc.Store(), "panel.old.com", "")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestChangingTheDomainFromSettings(t *testing.T) {
	h := newDomainHarness(t,
		PanelDomains{Domain: "panel.old.com", FlagDomain: "panel.old.com"},
		map[string][]string{"panel.old.com": {"203.0.113.7"}, "panel.new.net": {"203.0.113.7"}})
	env := filepath.Join(h.dir, "panel.env")
	os.WriteFile(env, []byte("SKYSBX_DOMAIN=panel.old.com\nSKYSBX_SUB_DOMAIN=\nSKYSBX_ACME_EMAIL=a@b.c\n"), 0o600)

	page := h.c.do(http.MethodGet, "/settings", nil).Body.String()
	if !strings.Contains(page, "更换域名") || !strings.Contains(page, "panel.old.com") {
		t.Fatal("settings page has no domain change form")
	}

	rec := h.c.do(http.MethodPost, "/settings/domain", url.Values{"domain": {"Panel.New.NET"}})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "https://panel.new.net/settings") {
		t.Fatalf("change: %d %s", rec.Code, rec.Body)
	}

	d := h.saved(t)
	if d.Domain != "panel.new.net" || d.SubDomain != "" || d.MoveHostFrom != "panel.old.com" {
		t.Fatalf("saved %+v", d)
	}
	// The old name carried the subscriptions as well as the panel, and keeps
	// doing both.
	if !slices.Equal(d.OldPanel, []string{"panel.old.com"}) || !slices.Equal(d.OldSub, []string{"panel.old.com"}) {
		t.Fatalf("old names %+v", d)
	}
	raw, _ := os.ReadFile(env)
	if string(raw) != "SKYSBX_DOMAIN=panel.new.net\nSKYSBX_SUB_DOMAIN=\nSKYSBX_ACME_EMAIL=a@b.c\n" {
		t.Fatalf("panel.env = %q", raw)
	}
}

func TestDomainChangeChecksDNSFirst(t *testing.T) {
	h := newDomainHarness(t,
		PanelDomains{Domain: "panel.old.com", FlagDomain: "panel.old.com"},
		map[string][]string{"panel.old.com": {"203.0.113.7"}, "elsewhere.net": {"198.51.100.1"}})

	for _, tc := range []struct{ domain, want string }{
		{"elsewhere.net", "不是这台服务器"},
		{"nowhere.net", "查不到"},
		{"https://x.net", "格式不对"},
		{"panel.old.com", "没有变化"},
	} {
		rec := h.c.do(http.MethodPost, "/settings/domain", url.Values{"domain": {tc.domain}})
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tc.want) {
			t.Fatalf("%s: %d %s", tc.domain, rec.Code, rec.Body)
		}
	}
	if raw, _ := h.svc.Store().Setting(settingPanelDomains); raw != "" {
		t.Fatalf("a refused change was saved: %s", raw)
	}

	rec := h.c.do(http.MethodPost, "/settings/domain", url.Values{"domain": {"elsewhere.net"}, "skip_dns": {"on"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("skip_dns: %d %s", rec.Code, rec.Body)
	}
}

func TestResolveDomainsFollowsTheUnit(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	saved := changeDomains(PanelDomains{Domain: "a.com", FlagDomain: "a.com"}, "b.com", "")
	if err := saveDomains(st, saved); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ flag, want string }{
		{"a.com", "b.com"}, // the unit has not been rewritten yet
		{"b.com", "b.com"}, // an upgrade rewrote it from panel.env
		{"c.com", "c.com"}, // someone ran the installer with a new --domain
	} {
		d, err := ResolveDomains(st, tc.flag, "")
		if err != nil {
			t.Fatal(err)
		}
		if d.Domain != tc.want {
			t.Errorf("unit says %s: serving %s, want %s", tc.flag, d.Domain, tc.want)
		}
	}
}

func TestChangeDomainsKeepsOldNamesOnce(t *testing.T) {
	d := PanelDomains{Domain: "a.com", SubDomain: "s.com", FlagDomain: "a.com", FlagSub: "s.com"}
	d = changeDomains(d, "b.com", "t.com")
	if !slices.Equal(d.OldPanel, []string{"a.com"}) || !slices.Equal(d.OldSub, []string{"s.com"}) {
		t.Fatalf("first change: %+v", d)
	}
	d = changeDomains(d, "a.com", "t.com") // and back again
	if !slices.Equal(d.OldPanel, []string{"b.com"}) || d.MoveHostFrom != "" {
		t.Fatalf("moving back: %+v", d)
	}
	if d.FlagDomain != "a.com" {
		t.Fatalf("flag domain lost: %+v", d)
	}
}

// Old names keep doing what they did, and nothing more.
func TestOldNamesKeepTheirJob(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st)
	user, _ := svc.CreateUser(service.NewUser{Name: "alice"})
	srv, err := New(svc, &fakeChannel{}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	// panel.a carried subscriptions (no sub domain then); panel.b had sub.b.
	srv.SetPanelDomains(PanelDomains{Domain: "panel.c", SubDomain: "sub.c",
		OldPanel: []string{"panel.a", "panel.b"}, OldSub: []string{"panel.a", "sub.b"}}, "", nil)
	h := srv.Handler()
	get := func(host, path string) int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "https://"+host+path, nil))
		return rec.Code
	}
	sub := "/sub/" + user.SubToken
	for _, tc := range []struct {
		host, path string
		ok         bool
	}{
		{"sub.c", sub, true}, {"panel.c", sub, false},
		{"panel.a", sub, true}, {"panel.a", "/login", true},
		{"panel.b", sub, false}, {"panel.b", "/login", true},
		{"sub.b", sub, true}, {"sub.b", "/login", false},
	} {
		code := get(tc.host, tc.path)
		if (code != http.StatusNotFound) != tc.ok {
			t.Errorf("%s%s: %d", tc.host, tc.path, code)
		}
	}
}

func TestRetiringOldDomains(t *testing.T) {
	d := changeDomains(PanelDomains{Domain: "panel.old.com", FlagDomain: "panel.old.com"}, "panel.new.net", "")
	h := newDomainHarness(t, d, nil)
	if err := saveDomains(h.svc.Store(), d); err != nil {
		t.Fatal(err)
	}
	if page := h.c.do(http.MethodGet, "/settings", nil).Body.String(); !strings.Contains(page, "停用旧域名") {
		t.Fatal("no way to retire the old domain")
	}
	rec := h.c.do(http.MethodPost, "/settings/domain/retire", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "旧域名已停用") {
		t.Fatalf("retire: %d %s", rec.Code, rec.Body)
	}
	got, _ := ResolveDomains(h.svc.Store(), "panel.old.com", "")
	if got.Domain != "panel.new.net" || len(got.Old()) != 0 {
		t.Fatalf("after retiring: %+v", got)
	}
}

func TestRelinkOnlyTouchesThePanelsOwnCertificates(t *testing.T) {
	dir := t.TempDir()
	oldCrt := filepath.Join(dir, "certs", "certificates", "acme", "a.com", "a.com.crt")
	newCrt := filepath.Join(dir, "certs", "certificates", "acme", "b.com", "b.com.crt")
	for _, f := range []string{oldCrt, newCrt} {
		os.MkdirAll(filepath.Dir(f), 0o700)
		os.WriteFile(f, nil, 0o600)
		os.WriteFile(strings.TrimSuffix(f, ".crt")+".key", nil, 0o600)
	}
	if got := findCertificate(dir, "b.com"); got != newCrt {
		t.Fatalf("found %q", got)
	}
	os.Symlink(oldCrt, filepath.Join(dir, "cert.pem"))
	os.Symlink(strings.TrimSuffix(oldCrt, ".crt")+".key", filepath.Join(dir, "key.pem"))
	if err := relinkNodeCertificate(dir, newCrt); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.Readlink(filepath.Join(dir, "cert.pem")); got != newCrt {
		t.Fatalf("cert.pem -> %s", got)
	}
	if got, _ := os.Readlink(filepath.Join(dir, "key.pem")); got != strings.TrimSuffix(newCrt, ".crt")+".key" {
		t.Fatalf("key.pem -> %s", got)
	}

	// A certificate someone placed by hand stays.
	own := t.TempDir()
	os.WriteFile(filepath.Join(own, "cert.pem"), []byte("mine"), 0o600)
	if err := relinkNodeCertificate(own, newCrt); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(filepath.Join(own, "cert.pem")); string(raw) != "mine" {
		t.Fatal("a hand-placed certificate was replaced")
	}
}

// Once the new certificate exists, the node on the panel's server and its TLS
// inbounds follow the domain, and the pending move is cleared.
func TestFinishDomainMoveWaitsForTheCertificateThenMoves(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "skysbx.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st)
	local, _, _ := svc.CreateNode("local", "panel.old.com", "JP")
	d := changeDomains(PanelDomains{Domain: "panel.old.com", FlagDomain: "panel.old.com"}, "panel.new.net", "")
	if err := saveDomains(st, d); err != nil {
		t.Fatal(err)
	}
	crt := filepath.Join(dir, "certs", "certificates", "acme", "panel.new.net", "panel.new.net.crt")
	os.MkdirAll(filepath.Dir(crt), 0o700)
	os.WriteFile(crt, nil, 0o600)

	FinishDomainMove(context.Background(), svc, dir, d, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if n, _ := st.Node(local.ID); n.Address != "panel.new.net" {
		t.Fatalf("local node still at %s", n.Address)
	}
	got, _ := ResolveDomains(st, "panel.old.com", "")
	if got.MoveHostFrom != "" || got.Domain != "panel.new.net" || len(got.OldPanel) != 1 {
		t.Fatalf("after the move: %+v", got)
	}
}
