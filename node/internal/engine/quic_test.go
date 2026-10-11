//go:build with_quic && with_clash_api && with_v2ray_api

package engine

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Zayvian/veyra-node/internal/proto"
	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/include"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/service"
)

func TestQUICHotSwapAndRevocation(t *testing.T) {
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
	for _, protocol := range []string{"hysteria2", "tuic"} {
		t.Run(protocol, func(t *testing.T) {
			ctx := context.Background()
			udp, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := udp.LocalAddr().(*net.UDPAddr).Port
			udp.Close()
			raw := json.RawMessage(fmt.Sprintf(`{"log":{"level":"error"},"inbounds":[{"type":%q,"tag":"quic","listen":"127.0.0.1","listen_port":%d,"tls":{"enabled":true,"server_name":"localhost","alpn":["h3"],"certificate_path":%q,"key_path":%q}}],"outbounds":[{"type":"direct","tag":"direct"}],"experimental":{"clash_api":{"external_controller":"127.0.0.1:9090"},"v2ray_api":{"listen":"127.0.0.1:6450","stats":{"enabled":true}}}}`, protocol, port, cert, key))
			e := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
			defer e.Close()
			if err := e.ApplyConfig(ctx, raw); err != nil {
				t.Fatal(err)
			}
			alice := proto.User{Name: "alice", UUID: "d7c78a62-4d10-4ecf-a4c4-1931745a98b8", Password: "alice-password"}
			bob := proto.User{Name: "bob", UUID: "9e5dfc1e-4797-4ee1-b50d-aa0edc4f6c83", Password: "bob-password"}
			update := func(users ...proto.User) {
				t.Helper()
				names := []string{}
				for _, u := range users {
					names = append(names, u.Name)
				}
				if err := e.UpdateUsers(ctx, proto.UsersData{ByTag: map[string][]proto.User{"quic": users}, StatsUsers: names}); err != nil {
					t.Fatal(err)
				}
			}
			update(alice, bob)
			instance := e.box
			ob := testQUICClient(t, protocol, port, cert, alice)
			conn, err := dialQUIC(ob, echo.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			roundTrip := func(c net.Conn) {
				t.Helper()
				c.SetDeadline(time.Now().Add(4 * time.Second))
				if _, err := c.Write([]byte("hello")); err != nil {
					t.Fatal(err)
				}
				b := make([]byte, 5)
				if _, err := io.ReadFull(c, b); err != nil {
					t.Fatal(err)
				}
				if string(b) != "hello" {
					t.Fatal("corrupt echo")
				}
			}
			roundTrip(conn)
			conns, err := e.connections(ctx)
			if err != nil {
				t.Fatal(err)
			}
			attributed := false
			_, echoPort, _ := net.SplitHostPort(echo.Addr().String())
			for _, c := range conns {
				if c.User == "alice" {
					attributed = true
					// Read in-process from sing-box's tracker: the address the
					// IP limit counts, and where the connection went.
					if c.IP != "127.0.0.1" || c.DestPort != echoPort || c.ID == "" {
						t.Fatalf("connection fields: %+v, want source 127.0.0.1 to port %s", c, echoPort)
					}
				}
			}
			if !attributed {
				t.Fatalf("missing alice attribution: %+v", conns)
			}
			usage, _, err := e.Stats()
			if err != nil {
				t.Fatal(err)
			}
			if usage["alice"].Up < 5 || usage["alice"].Down < 5 {
				t.Fatalf("QUIC traffic was not billed to alice: %+v", usage)
			}
			update(bob, alice)
			roundTrip(conn)
			if e.box != instance {
				t.Fatal("user update restarted listener")
			}
			update(bob)
			conn.SetDeadline(time.Now().Add(4 * time.Second))
			conn.Write([]byte("after-revocation"))
			if _, err := conn.Read(make([]byte, 32)); err == nil {
				t.Fatal("revoked QUIC session can still relay")
			}
			if e.box != instance {
				t.Fatal("revocation restarted listener")
			}
			bobOutbound := testQUICClient(t, protocol, port, cert, bob)
			// The fresh client's network monitor can report a change just after
			// it starts, and the client then closes its own new QUIC connection
			// ("Application error 0x0 (local)") — sometimes after the dial has
			// already succeeded. That is the test client settling, not the
			// server refusing bob, so the first exchange redials once or twice.
			var bobConn net.Conn
			for attempt := 1; ; attempt++ {
				bobConn, err = dialQUIC(bobOutbound, echo.Addr().String())
				if err == nil {
					if err = echoOnce(bobConn); err == nil {
						break
					}
					bobConn.Close()
				}
				if attempt == 5 {
					t.Fatalf("bob cannot relay after alice was revoked: %v", err)
				}
				time.Sleep(100 * time.Millisecond)
			}
			defer bobConn.Close()

			// What the address limit does to a connection over the line:
			// closed through the tracker, it stops relaying.
			conns, err = e.connections(ctx)
			if err != nil {
				t.Fatal(err)
			}
			closed := 0
			for _, c := range conns {
				if c.User == "bob" {
					if err := e.closeConnection(ctx, c.ID); err != nil {
						t.Fatal(err)
					}
					closed++
				}
			}
			if closed == 0 {
				t.Fatalf("bob's connection is not in the tracker: %+v", conns)
			}
			bobConn.SetDeadline(time.Now().Add(4 * time.Second))
			bobConn.Write([]byte("after-close"))
			if _, err := bobConn.Read(make([]byte, 32)); err == nil {
				t.Fatal("a connection closed through the tracker still relays")
			}
		})
	}
}

// echoOnce sends a few bytes through c and checks they come back.
func echoOnce(c net.Conn) error {
	c.SetDeadline(time.Now().Add(4 * time.Second))
	defer c.SetDeadline(time.Time{})
	if _, err := c.Write([]byte("hello")); err != nil {
		return err
	}
	b := make([]byte, 5)
	if _, err := io.ReadFull(c, b); err != nil {
		return err
	}
	if string(b) != "hello" {
		return fmt.Errorf("corrupt echo %q", b)
	}
	return nil
}

// The interface monitor can issue its initial network-change event just after
// Start. Retry that transient dial while keeping the integration test bounded.
func dialQUIC(ob adapter.Outbound, address string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var err error
	for i := 0; i < 10; i++ {
		var c net.Conn
		c, err = ob.DialContext(ctx, "tcp", M.ParseSocksaddr(address))
		if err == nil {
			return c, nil
		}
		if ctx.Err() != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil, err
}

func testQUICClient(t *testing.T, protocol string, port int, cert string, user proto.User) adapter.Outbound {
	t.Helper()
	ctx := include.Context(context.Background())
	outbound := map[string]any{"type": protocol, "tag": "proxy", "server": "127.0.0.1", "server_port": port, "password": user.Password, "tls": map[string]any{"enabled": true, "server_name": "localhost", "alpn": []string{"h3"}, "certificate_path": cert}}
	if protocol == "tuic" {
		outbound["uuid"] = user.UUID
	}
	raw, err := json.Marshal(map[string]any{"log": map[string]string{"level": "error"}, "outbounds": []any{outbound}})
	if err != nil {
		t.Fatal(err)
	}
	opts, err := parseOptions(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	client, err := box.New(box.Options{Context: ctx, Options: opts})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	// A freshly started client's network monitor reports its first "network
	// changed" shortly after Start, and the QUIC outbounds answer it by closing
	// their connections. Dialing before that has happened gives a connection
	// that dies under the test for reasons that have nothing to do with the
	// server — so let it settle first.
	time.Sleep(300 * time.Millisecond)
	manager := service.FromContext[adapter.OutboundManager](ctx)
	ob, ok := manager.Outbound("proxy")
	if !ok {
		t.Fatal("no client outbound")
	}
	return ob
}

func testCertificate(t *testing.T) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, &cert, &cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}
