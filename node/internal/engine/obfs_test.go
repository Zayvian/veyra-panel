//go:build with_quic && with_clash_api && with_v2ray_api

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

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/include"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/service"

	"github.com/Zayvian/veyra-node/internal/proto"
)

// The Hysteria2 inbound the panel generates with obfs and a masquerade site —
// the same JSON shape as protocol/testdata/config.json — must load in the
// node's sing-box and relay for a client that has the obfs password, and not
// for one that lacks it.
func TestHysteria2WithObfsAndMasquerade(t *testing.T) {
	cert, key := testCertificate(t)
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		for {
			c, err := echo.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()

	udp, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := udp.LocalAddr().(*net.UDPAddr).Port
	udp.Close()

	const obfs = "cGFzc3dvcmQtZm9yLW9iZnM"
	raw := json.RawMessage(fmt.Sprintf(`{"log":{"level":"error"},"inbounds":[{"type":"hysteria2","tag":"hy2","listen":"127.0.0.1","listen_port":%d,"tls":{"enabled":true,"server_name":"localhost","alpn":["h3"],"certificate_path":%q,"key_path":%q},"obfs":{"type":"salamander","password":%q},"masquerade":{"type":"proxy","url":"https://www.bing.com","rewrite_host":true}}],"outbounds":[{"type":"direct","tag":"direct"}]}`,
		port, cert, key, obfs))
	e := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer e.Close()
	ctx := context.Background()
	if err := e.ApplyConfig(ctx, raw); err != nil {
		t.Fatalf("the node refused the panel's Hysteria2 config: %v", err)
	}
	alice := proto.User{Name: "alice", Password: "alice-password"}
	if err := e.UpdateUsers(ctx, proto.UsersData{
		ByTag: map[string][]proto.User{"hy2": {alice}}, StatsUsers: []string{"alice"},
	}); err != nil {
		t.Fatal(err)
	}

	client := func(obfsPassword string) adapter.Outbound {
		t.Helper()
		out := map[string]any{"type": "hysteria2", "tag": "proxy", "server": "127.0.0.1",
			"server_port": port, "password": alice.Password,
			"tls": map[string]any{"enabled": true, "server_name": "localhost",
				"alpn": []string{"h3"}, "certificate_path": cert}}
		if obfsPassword != "" {
			out["obfs"] = map[string]any{"type": "salamander", "password": obfsPassword}
		}
		cfg, _ := json.Marshal(map[string]any{"log": map[string]string{"level": "panic"},
			"outbounds": []any{out}})
		cctx := include.Context(context.Background())
		opts, err := parseOptions(cctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		b, err := box.New(box.Options{Context: cctx, Options: opts})
		if err != nil {
			t.Fatal(err)
		}
		if err := b.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { b.Close() })
		time.Sleep(300 * time.Millisecond) // see testQUICClient
		ob, _ := service.FromContext[adapter.OutboundManager](cctx).Outbound("proxy")
		return ob
	}

	conn, err := dialQUIC(client(obfs), echo.Addr().String())
	if err != nil {
		t.Fatalf("a client with the obfs password cannot connect: %v", err)
	}
	defer conn.Close()
	if err := echoOnce(conn); err != nil {
		t.Fatalf("a client with the obfs password cannot relay: %v", err)
	}

	// Without the password the packets do not even look like Hysteria2.
	dctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	plain := client("")
	if c, err := plain.DialContext(dctx, "tcp", M.ParseSocksaddr(echo.Addr().String())); err == nil {
		err = echoOnce(c)
		c.Close()
		if err == nil {
			t.Fatal("a client without the obfs password relayed traffic")
		}
	}
}
