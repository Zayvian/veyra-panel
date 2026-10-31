package service

import (
	"strings"
	"testing"

	"github.com/Zayvian/veyra-panel/internal/store"
)

func TestBuildVLESSOverCDN(t *testing.T) {
	in, err := BuildInbound(InboundSpec{
		Protocol: store.ProtoVLESSWS, Tag: "cdn-tokyo", Port: 2053,
		ServerName: "cdn.example.com",
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	sb, err := ParseConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	// sing-box knows it as plain vless; only the panel tells the two apart.
	if sb.Type != "vless" || in.Protocol != store.ProtoVLESSWS {
		t.Fatalf("type %q, protocol %q", sb.Type, in.Protocol)
	}
	if sb.TLS == nil || sb.TLS.Reality != nil || sb.TLS.ServerName != "cdn.example.com" ||
		sb.TLS.CertificatePath != DefaultCertPath || sb.TLS.KeyPath != DefaultKeyPath {
		t.Fatalf("tls: %+v", sb.TLS)
	}
	tr := sb.Transport
	if tr == nil || tr.Type != "ws" || !strings.HasPrefix(tr.Path, "/") || len(tr.Path) < 8 ||
		tr.MaxEarlyData != WSEarlyData || tr.EarlyDataHeaderName != WSEarlyDataHeader {
		t.Fatalf("transport: %+v", tr)
	}

	c, err := ParseClient(in)
	if err != nil {
		t.Fatal(err)
	}
	if c.Flow != "" || c.Network != "ws" || c.Path != tr.Path || c.Host != "cdn.example.com" ||
		c.SNI != "cdn.example.com" || c.PBK != "" {
		t.Fatalf("client params: %+v", c)
	}

	if _, err := BuildInbound(InboundSpec{Protocol: store.ProtoVLESSWS, Tag: "x", Port: 2053}); err == nil {
		t.Fatal("built a CDN inbound with no domain")
	}
}

// Each inbound gets its own path, so a leaked one exposes one inbound only.
func TestWSPathsDiffer(t *testing.T) {
	if NewWSPath() == NewWSPath() {
		t.Fatal("two paths alike")
	}
}

// Vision only works on the raw TLS stream. A user pushed to a WebSocket
// inbound with the flow set is rejected as a flow mismatch on every connect.
func TestCDNUsersCarryNoFlow(t *testing.T) {
	u := &store.User{Name: "alice", VlessUUID: "11111111-1111-1111-1111-111111111111"}
	su, ok := singboxUser(u, store.ProtoVLESSWS)
	if !ok || su.UUID != u.VlessUUID || su.Flow != "" {
		t.Fatalf("cdn user: %+v %v", su, ok)
	}
	su, _ = singboxUser(u, store.ProtoVLESS)
	if su.Flow != FlowVision {
		t.Fatalf("reality user lost its flow: %+v", su)
	}
}

func TestCDNVLESSRefusesAnIP(t *testing.T) {
	_, err := BuildInbound(InboundSpec{Tag: "cdn", Protocol: store.ProtoVLESSWS, Port: 2053,
		ServerName: "38.64.60.249"})
	if err == nil {
		t.Fatal("an IP was accepted as the CDN domain")
	}
}
