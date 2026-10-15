package link

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Zayvian/veyra-node/internal/proto"
)

func startWithEnv(t *testing.T, p *fakePanel, envFile string, probe func(context.Context, string) error) *Client {
	t.Helper()
	c, err := New(Config{
		PanelURL: p.srv.URL, Token: "tok", Version: "0.1.0-test", EnvFile: envFile,
		Engine: &fakeEngine{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	c.probe = probe
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return c
}

// A panel that moved names its new address; the node checks it, writes it to
// node.env without disturbing the token, and reconnects there.
func TestNodeFollowsThePanelToItsNewAddress(t *testing.T) {
	oldPanel, newPanel := newFakePanel(t), newFakePanel(t)
	env := filepath.Join(t.TempDir(), "node.env")
	if err := os.WriteFile(env, []byte("SKYSBX_PANEL="+oldPanel.srv.URL+"\nSKYSBX_TOKEN=tok\nSKYSBX_LOG=info\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	probed := ""
	startWithEnv(t, oldPanel, env, func(_ context.Context, u string) error { probed = u; return nil })
	oldPanel.await(proto.TypeHello, 3*time.Second)

	oldPanel.push(proto.TypePanelURL, 9, proto.PanelURLData{URL: newPanel.srv.URL + "/"})
	if ok := oldPanel.await(proto.TypeOK, 3*time.Second); ok.ID != 9 {
		t.Fatalf("ok for id %d, want 9", ok.ID)
	}
	newPanel.await(proto.TypeHello, 10*time.Second)

	if probed != newPanel.srv.URL {
		t.Fatalf("probed %q, want %q", probed, newPanel.srv.URL)
	}
	raw, err := os.ReadFile(env)
	if err != nil {
		t.Fatal(err)
	}
	want := "SKYSBX_PANEL=" + newPanel.srv.URL + "\nSKYSBX_TOKEN=tok\nSKYSBX_LOG=info\n"
	if string(raw) != want {
		t.Fatalf("node.env =\n%s\nwant\n%s", raw, want)
	}
	if info, _ := os.Stat(env); info.Mode().Perm() != 0o600 {
		t.Fatalf("node.env mode %v; it holds the token", info.Mode().Perm())
	}
}

// A new address that does not answer yet is refused, and the node stays.
func TestNodeStaysWhenTheNewAddressDoesNotWork(t *testing.T) {
	oldPanel, newPanel := newFakePanel(t), newFakePanel(t)
	env := filepath.Join(t.TempDir(), "node.env")
	before := "SKYSBX_PANEL=" + oldPanel.srv.URL + "\nSKYSBX_TOKEN=tok\n"
	if err := os.WriteFile(env, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	startWithEnv(t, oldPanel, env, func(context.Context, string) error { return errors.New("no certificate yet") })
	oldPanel.await(proto.TypeHello, 3*time.Second)

	oldPanel.push(proto.TypePanelURL, 4, proto.PanelURLData{URL: newPanel.srv.URL})
	if e := oldPanel.await(proto.TypeError, 3*time.Second); e.ID != 4 {
		t.Fatalf("error for id %d, want 4", e.ID)
	}
	time.Sleep(300 * time.Millisecond)
	if n := newPanel.connectAttempts(); n != 0 {
		t.Fatalf("node dialled the new address %d times", n)
	}
	if raw, _ := os.ReadFile(env); string(raw) != before {
		t.Fatalf("node.env changed to\n%s", raw)
	}
	oldPanel.push(proto.TypePing, 5, nil)
	oldPanel.await(proto.TypePong, 3*time.Second)
}

func TestProbeWantsThePanelsUnauthorized(t *testing.T) {
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/node/connect" {
			t.Errorf("probed %s", r.URL.Path)
		}
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
	}))
	defer panel.Close()
	if err := probePanel(context.Background(), panel.URL); err != nil {
		t.Fatal(err)
	}

	parked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "this domain is for sale")
	}))
	defer parked.Close()
	if err := probePanel(context.Background(), parked.URL); err == nil {
		t.Fatal("a page that is not the panel passed the probe")
	}
}

func TestSetEnvAddsAMissingKey(t *testing.T) {
	env := filepath.Join(t.TempDir(), "node.env")
	os.WriteFile(env, []byte("SKYSBX_TOKEN=tok"), 0o600)
	if err := setEnv(env, "SKYSBX_PANEL", "https://b.example"); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(env); string(raw) != "SKYSBX_TOKEN=tok\nSKYSBX_PANEL=https://b.example\n" {
		t.Fatalf("got %q", raw)
	}
	if err := setEnv(filepath.Join(t.TempDir(), "missing"), "K", "v"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file: %v", err)
	}
}
