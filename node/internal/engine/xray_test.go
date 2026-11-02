package engine

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zayvian/veyra-node/internal/proto"
)

func TestXrayFragmentAcceptsVLESSOnly(t *testing.T) {
	for name, raw := range map[string]string{
		"not vless":     `{"inbounds":[{"tag":"a","protocol":"vmess"}]}`,
		"no tag":        `{"inbounds":[{"protocol":"vless"}]}`,
		"duplicate tag": `{"inbounds":[{"tag":"a","protocol":"vless"},{"tag":"a","protocol":"vless"}]}`,
		"not json":      `{"inbounds":`,
	} {
		if _, _, err := parseXrayFragment(json.RawMessage(raw)); err == nil {
			t.Errorf("%s: accepted %s", name, raw)
		}
	}
	for _, raw := range []string{"", "null", `{"inbounds":[]}`} {
		in, _, err := parseXrayFragment(json.RawMessage(raw))
		if err != nil || len(in) != 0 {
			t.Errorf("%q: got %v, %v; want nothing to serve", raw, in, err)
		}
	}
}

// The panel's inbound reaches Xray exactly as sent, with the users filled in.
func TestXrayConfigKeepsThePanelsSettings(t *testing.T) {
	in, _, err := parseXrayFragment(json.RawMessage(`{"inbounds":[{"tag":"x","protocol":"vless","port":2053,
		"settings":{"decryption":"none","fallbacks":[]},
		"streamSettings":{"network":"xhttp","xhttpSettings":{"path":"/p"}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := buildXrayConfig(in, nil, map[string][]proto.User{
		"x": {{Name: "alice", UUID: "6f1c7a52-2c43-4e8e-9d0b-6d6d1c1f0a11", Flow: "xtls-rprx-vision"}},
	}, "127.0.0.1:9")
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		API struct {
			Listen string `json:"listen"`
		} `json:"api"`
		Inbounds []struct {
			Port     int `json:"port"`
			Settings struct {
				Clients   []vlessClient `json:"clients"`
				Fallbacks []any         `json:"fallbacks"`
			} `json:"settings"`
			Stream struct {
				XHTTP struct {
					Path string `json:"path"`
				} `json:"xhttpSettings"`
			} `json:"streamSettings"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.API.Listen != "127.0.0.1:9" || len(cfg.Inbounds) != 1 {
		t.Fatalf("config = %s", raw)
	}
	got := cfg.Inbounds[0]
	if got.Port != 2053 || got.Stream.XHTTP.Path != "/p" || got.Settings.Fallbacks == nil {
		t.Fatalf("panel settings lost: %s", raw)
	}
	want := vlessClient{ID: "6f1c7a52-2c43-4e8e-9d0b-6d6d1c1f0a11", Flow: "xtls-rprx-vision", Email: "alice"}
	if len(got.Settings.Clients) != 1 || got.Settings.Clients[0] != want {
		t.Fatalf("clients = %+v, want %+v", got.Settings.Clients, want)
	}
}

func TestXrayInboundsAreRefusedWithoutXray(t *testing.T) {
	e := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer e.Close()
	xray := xrayVLESS(t, "x", freePort(t))
	err := e.ApplyConfig(context.Background(), configWithShadowsocks(t, freePort(t)), xray)
	if err == nil || !strings.Contains(err.Error(), "no Xray") {
		t.Fatalf("apply = %v, want a refusal naming the missing Xray", err)
	}
	if e.running {
		t.Fatal("sing-box was started for a configuration that was refused")
	}
}

// testXray finds an Xray to run the tests against: VEYRA_TEST_XRAY, or xray on
// PATH. Without one, the tests that need a real Xray are skipped.
func testXray(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("VEYRA_TEST_XRAY"); p != "" {
		return p
	}
	if p, err := exec.LookPath("xray"); err == nil {
		return p
	}
	t.Skip("no xray binary: set VEYRA_TEST_XRAY")
	return ""
}

func xrayEngine(t *testing.T) *Engine {
	t.Helper()
	e := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { e.Close() })
	if err := e.UseXray(testXray(t)); err != nil {
		t.Fatal(err)
	}
	if e.XrayVersion() == "" {
		t.Fatal("no xray version")
	}
	return e
}

// A plain VLESS inbound: no TLS, so the test client needs nothing but the
// user's id. What is under test is the node's handling of Xray, not Xray's
// transports.
func xrayVLESS(t *testing.T, tag string, port int) json.RawMessage {
	t.Helper()
	return json.RawMessage(fmt.Sprintf(`{"inbounds":[{"tag":%q,"protocol":"vless",
		"listen":"127.0.0.1","port":%d,"settings":{"clients":[],"decryption":"none"}}]}`, tag, port))
}

const (
	aliceID = "6f1c7a52-2c43-4e8e-9d0b-6d6d1c1f0a11"
	bobID   = "0b3c56c0-5c1e-4a8e-8f42-3b0f1a9f6e21"
)

// The whole life of an Xray inbound: users added and removed while it runs,
// their traffic counted, a configuration change, a configuration Xray
// refuses, and a crash.
func TestXrayServesUsersAndCountsTheirTraffic(t *testing.T) {
	e := xrayEngine(t)
	ctx := context.Background()
	echo := echoServer(t)

	port := freePort(t)
	if err := e.ApplyConfig(ctx, configWithShadowsocks(t, freePort(t)), xrayVLESS(t, "vx", port)); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if st := e.State(); !containsTag(st.Inbounds, "vx") || !containsTag(st.Inbounds, "ss") {
		t.Fatalf("state inbounds = %v, want ss and vx", st.Inbounds)
	}

	alice := xrayClient(t, port, aliceID)
	if err := roundTrip(alice, echo, 1000); err == nil {
		t.Fatal("alice got through before she was added")
	}

	if err := e.UpdateUsers(ctx, proto.UsersData{
		ByTag:      map[string][]proto.User{"vx": {{Name: "alice", UUID: aliceID}}},
		StatsUsers: []string{"alice"},
	}); err != nil {
		t.Fatalf("add alice: %v", err)
	}
	if err := roundTrip(alice, echo, 1000); err != nil {
		t.Fatalf("alice after she was added: %v", err)
	}

	checkOnlineNames(t, e)
	users, _, err := e.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if users["alice"].Up < 1000 || users["alice"].Down < 1000 {
		t.Fatalf("alice's traffic = %+v, want at least 1000 each way", users["alice"])
	}
	e.StatsReported()

	// Bob joins; alice stays connected throughout.
	held := openTunnel(t, alice, echo)
	if err := e.UpdateUsers(ctx, proto.UsersData{
		ByTag: map[string][]proto.User{"vx": {
			{Name: "alice", UUID: aliceID}, {Name: "bob", UUID: bobID},
		}},
		StatsUsers: []string{"alice", "bob"},
	}); err != nil {
		t.Fatalf("add bob: %v", err)
	}
	if err := echoOver(held, 100); err != nil {
		t.Fatalf("alice's open connection did not survive bob being added: %v", err)
	}
	held.Close()
	bob := xrayClient(t, port, bobID)
	if err := roundTrip(bob, echo, 500); err != nil {
		t.Fatalf("bob: %v", err)
	}

	// Alice is removed.
	if err := e.UpdateUsers(ctx, proto.UsersData{
		ByTag:      map[string][]proto.User{"vx": {{Name: "bob", UUID: bobID}}},
		StatsUsers: []string{"bob"},
	}); err != nil {
		t.Fatalf("remove alice: %v", err)
	}
	if err := roundTrip(alice, echo, 100); err == nil {
		t.Fatal("alice still gets through after she was removed")
	}

	// A new port restarts Xray. Bob's traffic since the last report is
	// carried across, and bob is still a user of the new process.
	newPort := freePort(t)
	if err := e.ApplyConfig(ctx, configWithShadowsocks(t, freePort(t)), xrayVLESS(t, "vx", newPort)); err != nil {
		t.Fatalf("move port: %v", err)
	}
	if err := roundTrip(xrayClient(t, newPort, bobID), echo, 500); err != nil {
		t.Fatalf("bob after the restart: %v", err)
	}
	users, _, err = e.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if users["bob"].Up < 1000 {
		t.Fatalf("bob's traffic across the restart = %+v, want both sessions", users["bob"])
	}
	e.StatsReported()

	// A port Xray cannot bind is refused, and the running one stays.
	taken, release := heldPort(t)
	defer release()
	if err := e.ApplyConfig(ctx, configWithShadowsocks(t, freePort(t)), xrayVLESS(t, "vx", taken)); err == nil {
		t.Fatal("a configuration on a taken port was accepted")
	}
	if err := roundTrip(xrayClient(t, newPort, bobID), echo, 100); err != nil {
		t.Fatalf("bob after a refused configuration: %v", err)
	}
	if st := e.State(); st.Error == "" || !containsTag(st.Inbounds, "vx") {
		t.Fatalf("state = %+v, want the refusal and vx still served", st)
	}

	// A crash is recovered from without the panel doing anything.
	e.x.mu.Lock()
	_ = e.x.proc.cmd.Process.Kill()
	e.x.mu.Unlock()
	deadline := time.Now().Add(10 * time.Second)
	for roundTrip(xrayClient(t, newPort, bobID), echo, 100) != nil {
		if time.Now().After(deadline) {
			t.Fatal("xray was not restarted after it died")
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Nothing assigned to Xray stops it.
	if err := e.ApplyConfig(ctx, configWithShadowsocks(t, freePort(t)), nil); err != nil {
		t.Fatal(err)
	}
	waitListening(t, newPort, false)
	if st := e.State(); containsTag(st.Inbounds, "vx") {
		t.Fatalf("state inbounds = %v after xray was emptied", st.Inbounds)
	}
}

func containsTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}

// checkOnlineNames checks online users can be read and come back as plain
// names. Whether alice is still listed depends on timing — her connection has
// closed — so that is not asserted.
func checkOnlineNames(t *testing.T, e *Engine) {
	t.Helper()
	names, err := e.Online()
	if err != nil {
		t.Fatalf("online: %v", err)
	}
	for _, n := range names {
		if strings.Contains(n, ">>>") {
			t.Fatalf("online name %q was not unwrapped", n)
		}
	}
}

// xrayClient starts a second Xray as a client: a SOCKS5 listener whose
// connections go out over VLESS to the node's inbound as one user. Returns the
// SOCKS address.
func xrayClient(t *testing.T, serverPort int, id string) string {
	t.Helper()
	return xrayClientFrom(t, serverPort, id, "127.0.0.1")
}

// xrayClientFrom is xrayClient connecting from a chosen loopback address, so
// one test can be several clients. Xray leaves 127.0.0.1 out of its online
// addresses, so address-limit tests use others in 127.0.0.0/8.
func xrayClientFrom(t *testing.T, serverPort int, id, source string) string {
	t.Helper()
	socks := freePort(t)
	cfg := fmt.Sprintf(`{"log":{"loglevel":"none"},
		"inbounds":[{"listen":"127.0.0.1","port":%d,"protocol":"socks","settings":{"udp":false}}],
		"outbounds":[{"protocol":"vless","sendThrough":%q,"settings":{"vnext":[{"address":"127.0.0.1","port":%d,
			"users":[{"id":%q,"encryption":"none"}]}]}}]}`, socks, source, serverPort, id)
	path := filepath.Join(t.TempDir(), "client.json")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(testXray(t), "run", "-c", path)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	waitListening(t, socks, true)
	return fmt.Sprintf("127.0.0.1:%d", socks)
}

func echoServer(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); _, _ = io.Copy(c, c) }()
		}
	}()
	return l.Addr().String()
}

// openTunnel connects to target through the SOCKS5 proxy at socks.
func openTunnel(t *testing.T, socks, target string) net.Conn {
	t.Helper()
	c, err := dialSOCKS(socks, target)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func dialSOCKS(socks, target string) (net.Conn, error) {
	c, err := net.DialTimeout("tcp", socks, 2*time.Second)
	if err != nil {
		return nil, err
	}
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	host, portStr, _ := net.SplitHostPort(target)
	var port uint16
	fmt.Sscan(portStr, &port)
	ip := net.ParseIP(host).To4()

	// No authentication; then CONNECT to an IPv4 address.
	req := []byte{5, 1, 0, 5, 1, 0, 1}
	req = append(req, ip...)
	req = binary.BigEndian.AppendUint16(req, port)
	if _, err := c.Write(req); err != nil {
		c.Close()
		return nil, err
	}
	reply := make([]byte, 2+10)
	if _, err := io.ReadFull(c, reply); err != nil {
		c.Close()
		return nil, err
	}
	if reply[0] != 5 || reply[1] != 0 || reply[2] != 5 || reply[3] != 0 {
		c.Close()
		return nil, fmt.Errorf("socks refused: %v", reply)
	}
	_ = c.SetDeadline(time.Time{})
	return c, nil
}

// roundTrip sends n bytes to the echo server through the proxy and expects
// them back. Xray's SOCKS inbound answers CONNECT before the VLESS side has
// been tried, so a refused user shows up as the echo never arriving.
func roundTrip(socks, target string, n int) error {
	c, err := dialSOCKS(socks, target)
	if err != nil {
		return err
	}
	defer c.Close()
	return echoOver(c, n)
}

func echoOver(c net.Conn, n int) error {
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	defer c.SetDeadline(time.Time{})
	msg := []byte(strings.Repeat("x", n))
	if _, err := c.Write(msg); err != nil {
		return err
	}
	got := make([]byte, n)
	if _, err := io.ReadFull(c, got); err != nil {
		return err
	}
	return nil
}

// The address limit covers Xray inbounds: a second address over the limit is
// refused new connections while the first keeps working, and is let back in
// when the limit allows it.
func TestXrayAddressLimit(t *testing.T) {
	e := xrayEngine(t)
	ctx := context.Background()
	echo := echoServer(t)
	port := freePort(t)
	if err := e.ApplyConfig(ctx, configWithShadowsocks(t, freePort(t)), xrayVLESS(t, "vx", port)); err != nil {
		t.Fatal(err)
	}
	users := func(limit int) proto.UsersData {
		return proto.UsersData{
			ByTag:      map[string][]proto.User{"vx": {{Name: "alice", UUID: aliceID}}},
			StatsUsers: []string{"alice"},
			IPLimits:   map[string]int{"alice": limit},
		}
	}
	if err := e.UpdateUsers(ctx, users(1)); err != nil {
		t.Fatal(err)
	}

	first := xrayClientFrom(t, port, aliceID, "127.0.0.2")
	second := xrayClientFrom(t, port, aliceID, "127.0.0.3")

	held := openTunnel(t, first, echo)
	defer held.Close()
	if err := echoOver(held, 10); err != nil {
		t.Fatal(err)
	}
	counts, _, err := e.EnforceIPLimits(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if counts["alice"] != 1 {
		t.Fatalf("alice's addresses = %d, want 1", counts["alice"])
	}

	// The second address connects; the next round finds it over the limit.
	late := openTunnel(t, second, echo)
	if err := echoOver(late, 10); err != nil {
		t.Fatalf("second address before the limit was enforced: %v", err)
	}
	if counts, _, _ = e.EnforceIPLimits(ctx); counts["alice"] != 2 {
		t.Fatalf("alice's addresses = %d, want 2", counts["alice"])
	}
	late.Close()

	if err := roundTrip(second, echo, 10); err == nil {
		t.Fatal("the address over the limit still gets new connections through")
	}
	if err := roundTrip(first, echo, 10); err != nil {
		t.Fatalf("the first address lost its place: %v", err)
	}

	// Raising the limit lets the second address back in.
	if err := e.UpdateUsers(ctx, users(2)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.EnforceIPLimits(ctx); err != nil {
		t.Fatal(err)
	}
	if err := roundTrip(second, echo, 10); err != nil {
		t.Fatalf("second address after the limit was raised: %v", err)
	}
}

// Routing rules the panel sends with the Xray part are applied: here, one
// that blocks the echo server's port.
func TestXrayAppliesThePanelsRoutingRules(t *testing.T) {
	e := xrayEngine(t)
	ctx := context.Background()
	echo := echoServer(t)
	_, echoPort, _ := net.SplitHostPort(echo)
	port := freePort(t)
	frag := fmt.Sprintf(`{"inbounds":[{"tag":"vx","protocol":"vless","listen":"127.0.0.1","port":%d,
		"settings":{"clients":[],"decryption":"none"}}],
		"routing":{"rules":[{"type":"field","port":%q,"outboundTag":"block"}]}}`, port, echoPort)
	if err := e.ApplyConfig(ctx, configWithShadowsocks(t, freePort(t)), json.RawMessage(frag)); err != nil {
		t.Fatal(err)
	}
	if err := e.UpdateUsers(ctx, proto.UsersData{
		ByTag: map[string][]proto.User{"vx": {{Name: "alice", UUID: aliceID}}}, StatsUsers: []string{"alice"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := roundTrip(xrayClient(t, port, aliceID), echo, 10); err == nil {
		t.Fatal("a connection the policy blocks got through")
	}
}
