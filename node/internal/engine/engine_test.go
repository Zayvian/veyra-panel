package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"
)

// A configuration that fails to start must not cost the node the one it was
// already running. sing-box builds instances fine and only discovers a taken
// port at Start, by which time the old instance has been stopped — so without a
// rollback, one mistyped port takes every inbound on the node offline until
// some later edit happens to push a new configuration.
func TestAFailedStartRestoresThePreviousConfiguration(t *testing.T) {
	e := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer e.Close()

	good := freePort(t)
	cfg := configWithShadowsocks(t, good)
	if err := e.ApplyConfig(context.Background(), cfg); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	waitListening(t, good, true)

	// Hold a port, then hand the engine a configuration that wants it.
	taken, release := heldPort(t)
	defer release()

	err := e.ApplyConfig(context.Background(), configWithShadowsocks(t, taken))
	if err == nil {
		t.Fatal("applying a configuration on a taken port succeeded")
	}

	if !e.running {
		t.Fatal("engine is not running after a failed apply")
	}
	waitListening(t, good, true)
}

// The failure that happens before the swap — a certificate that is not there —
// must leave the running instance untouched, which is the cheaper path and the
// one that needs no rollback.
func TestABuildFailureLeavesTheRunningInstanceAlone(t *testing.T) {
	e := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer e.Close()

	good := freePort(t)
	if err := e.ApplyConfig(context.Background(), configWithShadowsocks(t, good)); err != nil {
		t.Fatalf("first apply: %v", err)
	}

	bad := fmt.Sprintf(`{"inbounds":[{"type":"anytls","tag":"a","listen":"::",
		"listen_port":%d,"tls":{"enabled":true,"server_name":"example.com",
		"certificate_path":"/nonexistent/cert.pem","key_path":"/nonexistent/key.pem"}}]}`,
		freePort(t))
	if err := e.ApplyConfig(context.Background(), json.RawMessage(bad)); err == nil {
		t.Fatal("applying a configuration with a missing certificate succeeded")
	}
	if !e.running {
		t.Fatal("engine is not running after a build failure")
	}
	waitListening(t, good, true)
}

func configWithShadowsocks(t *testing.T, port int) json.RawMessage {
	t.Helper()
	// A 32-byte key, which is what 2022-blake3-aes-256-gcm requires.
	const psk = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	return json.RawMessage(fmt.Sprintf(
		`{"inbounds":[{"type":"shadowsocks","tag":"ss","listen":"::",
		  "listen_port":%d,"method":"2022-blake3-aes-256-gcm","password":%q}]}`,
		port, psk))
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

// heldPort returns a port with a listener still on it, so anything else trying
// to bind fails the way a port collision does in production.
func heldPort(t *testing.T) (int, func()) {
	t.Helper()
	l, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("hold a port: %v", err)
	}
	return l.Addr().(*net.TCPAddr).Port, func() { l.Close() }
}

func waitListening(t *testing.T, port int, want bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		c, err := net.DialTimeout("tcp",
			net.JoinHostPort("127.0.0.1", fmt.Sprint(port)), 200*time.Millisecond)
		if err == nil {
			c.Close()
		}
		if (err == nil) == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("port %d listening=%v, want %v", port, err == nil, want)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
