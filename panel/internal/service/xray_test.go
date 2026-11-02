package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Zayvian/veyra-panel/internal/store"
)

type xrayFragment struct {
	Inbounds []struct {
		Tag      string `json:"tag"`
		Protocol string `json:"protocol"`
		Listen   string `json:"listen"`
		Port     int    `json:"port"`
		Settings struct {
			Clients    []any  `json:"clients"`
			Decryption string `json:"decryption"`
		} `json:"settings"`
		Stream struct {
			Network  string `json:"network"`
			Security string `json:"security"`
			XHTTP    *struct {
				Path string `json:"path"`
				Mode string `json:"mode"`
			} `json:"xhttpSettings"`
			Reality struct {
				Target      string   `json:"target"`
				ServerNames []string `json:"serverNames"`
				PrivateKey  string   `json:"privateKey"`
				ShortIDs    []string `json:"shortIds"`
			} `json:"realitySettings"`
		} `json:"streamSettings"`
	} `json:"inbounds"`
}

func xrayPart(t *testing.T, svc *Service, nodeID int64) xrayFragment {
	t.Helper()
	raw, err := svc.NodeXrayConfig(nodeID)
	if err != nil {
		t.Fatal(err)
	}
	var f xrayFragment
	if raw != nil {
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatalf("xray part %s: %v", raw, err)
		}
	}
	return f
}

func TestProtocolChoicesRoundTrip(t *testing.T) {
	for _, c := range ProtocolChoices {
		p, core, transport := ParseProtocolChoice(c.Value)
		in := &store.Inbound{Protocol: p, Core: core, Client: "{}"}
		if transport != "" {
			in.Client = `{"network":"xhttp"}`
		}
		if got := ProtocolLabel(in); got != c.Label {
			t.Errorf("%s: label %q, want %q", c.Value, got, c.Label)
		}
	}
}

func TestXrayInboundsAreVLESSRealityOnly(t *testing.T) {
	for name, spec := range map[string]InboundSpec{
		"xray anytls":   {Protocol: store.ProtoAnyTLS, Core: store.CoreXray, ServerName: "a.example.com"},
		"unknown core":  {Protocol: store.ProtoVLESS, Core: "v2ray"},
		"xhttp on sb":   {Protocol: store.ProtoVLESS, Transport: TransportXHTTP},
		"bad transport": {Protocol: store.ProtoVLESS, Core: store.CoreXray, Transport: "grpc"},
	} {
		spec.Tag, spec.Port = "t", 443
		if _, err := BuildInbound(spec); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v, want invalid", name, err)
		}
	}
}

// An Xray inbound reaches the node in Xray's format, and only through the
// Xray part: sing-box never sees it.
func TestXrayInboundIsSentToXrayOnly(t *testing.T) {
	svc, node, _ := relayFixture(t)
	mustInbound(t, svc, node, InboundSpec{Protocol: store.ProtoVLESS, Port: 443})
	x := mustInbound(t, svc, node, InboundSpec{Protocol: store.ProtoVLESS, Core: store.CoreXray,
		Port: 8443, Handshake: "www.example.com:8443"})
	xh := mustInbound(t, svc, node, InboundSpec{Protocol: store.ProtoVLESS, Core: store.CoreXray,
		Transport: TransportXHTTP, Port: 2053})

	if !strings.HasPrefix(xh.Tag, "xhttp-") {
		t.Errorf("xhttp inbound tag %q, want an xhttp- prefix", xh.Tag)
	}

	cfg, err := svc.NodeConfig(node)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Inbounds) != 1 || cfg.Inbounds[0].ListenPort != 443 {
		t.Fatalf("sing-box inbounds = %+v, want only the sing-box one", cfg.Inbounds)
	}

	f := xrayPart(t, svc, node)
	if len(f.Inbounds) != 2 {
		t.Fatalf("xray inbounds = %+v, want two", f.Inbounds)
	}
	tcp, http := f.Inbounds[0], f.Inbounds[1]
	if tcp.Tag != x.Tag || tcp.Protocol != "vless" || tcp.Port != 8443 || tcp.Listen != "::" {
		t.Errorf("tcp inbound = %+v", tcp)
	}
	if tcp.Stream.Network != "tcp" || tcp.Stream.Security != "reality" || tcp.Stream.XHTTP != nil {
		t.Errorf("tcp stream = %+v", tcp.Stream)
	}
	if tcp.Stream.Reality.Target != "www.example.com:8443" ||
		len(tcp.Stream.Reality.ServerNames) != 1 || tcp.Stream.Reality.ServerNames[0] != "www.example.com" {
		t.Errorf("tcp reality = %+v", tcp.Stream.Reality)
	}
	if tcp.Settings.Decryption != "none" || tcp.Settings.Clients == nil || len(tcp.Settings.Clients) != 0 {
		t.Errorf("settings = %+v, want no clients and no decryption", tcp.Settings)
	}

	// The keys are the ones generated for the inbound, so subscriptions that
	// already hold its public key keep working.
	sb, err := ParseConfig(x)
	if err != nil {
		t.Fatal(err)
	}
	if tcp.Stream.Reality.PrivateKey != sb.TLS.Reality.PrivateKey ||
		tcp.Stream.Reality.ShortIDs[0] != sb.TLS.Reality.ShortID[0] {
		t.Errorf("reality keys differ from the stored inbound")
	}

	client, err := ParseClient(xh)
	if err != nil {
		t.Fatal(err)
	}
	if http.Stream.Network != "xhttp" || http.Stream.XHTTP == nil ||
		http.Stream.XHTTP.Path != client.Path || http.Stream.XHTTP.Mode != "auto" {
		t.Errorf("xhttp stream = %+v, client = %+v", http.Stream, client)
	}
	if client.Network != "xhttp" || client.Flow != "" || len(client.Path) < 10 {
		t.Errorf("xhttp client params = %+v", client)
	}

	// Disabled, it is not sent at all.
	if err := svc.SetInboundEnabled(x.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetInboundEnabled(xh.ID, false); err != nil {
		t.Fatal(err)
	}
	if raw, err := svc.NodeXrayConfig(node); err != nil || raw != nil {
		t.Fatalf("xray part with every xray inbound disabled = %s, %v", raw, err)
	}
}

// Users get the flow their inbound's subscriptions tell clients to use.
func TestXrayUsersCarryTheirInboundsFlow(t *testing.T) {
	svc, node, _ := relayFixture(t)
	x := mustInbound(t, svc, node, InboundSpec{Protocol: store.ProtoVLESS, Core: store.CoreXray, Port: 443})
	xh := mustInbound(t, svc, node, InboundSpec{Protocol: store.ProtoVLESS, Core: store.CoreXray,
		Transport: TransportXHTTP, Port: 2053})
	if _, err := svc.CreateUser(NewUser{Name: "alice"}); err != nil {
		t.Fatal(err)
	}
	byTag, err := svc.NodeUsers(node)
	if err != nil {
		t.Fatal(err)
	}
	if u := byTag[x.Tag]; len(u) != 1 || u[0].Flow != FlowVision || u[0].UUID == "" {
		t.Errorf("tcp users = %+v, want alice with vision", u)
	}
	if u := byTag[xh.Tag]; len(u) != 1 || u[0].Flow != "" || u[0].UUID == "" {
		t.Errorf("xhttp users = %+v, want alice without a flow", u)
	}
}

// Editing an Xray inbound keeps it an Xray inbound with its transport, and the
// change reaches the Xray part.
func TestEditingAnXrayInbound(t *testing.T) {
	svc, node, _ := relayFixture(t)
	xh := mustInbound(t, svc, node, InboundSpec{Protocol: store.ProtoVLESS, Core: store.CoreXray,
		Transport: TransportXHTTP, Port: 2053})
	before := xrayPart(t, svc, node).Inbounds[0]

	edited, err := svc.EditInbound(xh.ID, InboundEdit{Port: 2083, Handshake: "www.example.org"})
	if err != nil {
		t.Fatal(err)
	}
	if edited.Core != store.CoreXray {
		t.Fatalf("core after edit = %q", edited.Core)
	}
	after := xrayPart(t, svc, node).Inbounds[0]
	if after.Port != 2083 || after.Stream.Reality.Target != "www.example.org:443" {
		t.Errorf("after edit = %+v", after)
	}
	if after.Stream.XHTTP == nil || after.Stream.XHTTP.Path != before.Stream.XHTTP.Path ||
		after.Stream.Reality.PrivateKey != before.Stream.Reality.PrivateKey {
		t.Errorf("edit changed the path or key: before %+v, after %+v", before.Stream, after.Stream)
	}

	n, err := svc.Node(node)
	if err != nil {
		t.Fatal(err)
	}
	n.Name = "osaka"
	if err := svc.UpdateNode(n); err != nil {
		t.Fatal(err)
	}
	if got := xrayPart(t, svc, node).Inbounds[0].Tag; got != "xhttp-osaka" {
		t.Errorf("tag after node rename = %q, want xhttp-osaka", got)
	}
}

// Nodes without Xray inbounds keep the rev they had before Xray existed, so an
// upgraded panel does not mark every node as waiting to apply.
func TestConfigRevWithoutXrayIsUnchanged(t *testing.T) {
	svc, node, _ := relayFixture(t)
	mustInbound(t, svc, node, InboundSpec{Protocol: store.ProtoVLESS, Port: 443})
	cfg, err := svc.NodeConfig(node)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(cfg)
	sum := sha256.Sum256(raw)
	want := hex.EncodeToString(sum[:])
	for _, xray := range []bool{false, true} {
		got, err := svc.NodeConfigRev(node, xray)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("rev (xray %v) = %s, want the sing-box hash %s", xray, got, want)
		}
	}

	mustInbound(t, svc, node, InboundSpec{Protocol: store.ProtoVLESS, Core: store.CoreXray, Port: 8443})
	without, _ := svc.NodeConfigRev(node, false)
	with, _ := svc.NodeConfigRev(node, true)
	if without != want || with == want {
		t.Errorf("revs with an xray inbound: without xray %s, with %s; want %s and something else", without, with, want)
	}
}

// The server policy reaches Xray inbounds too: sniffing on the inbounds and
// rules that send what it blocks to the node's block outbound.
func TestPolicyReachesXray(t *testing.T) {
	svc, node, _ := relayFixture(t)
	mustInbound(t, svc, node, InboundSpec{Protocol: store.ProtoVLESS, Core: store.CoreXray, Port: 443})

	plain, err := svc.NodeXrayConfig(node)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "sniffing") || strings.Contains(string(plain), "routing") {
		t.Fatalf("xray part without a policy = %s", plain)
	}

	if err := svc.SetPolicy(Policy{BlockBitTorrent: true, BlockSpeedtest: true,
		BlockedDomains: []string{"example.net"}}); err != nil {
		t.Fatal(err)
	}
	raw, err := svc.NodeXrayConfig(node)
	if err != nil {
		t.Fatal(err)
	}
	var part struct {
		Inbounds []struct {
			Sniffing *struct {
				Enabled      bool     `json:"enabled"`
				DestOverride []string `json:"destOverride"`
				RouteOnly    bool     `json:"routeOnly"`
			} `json:"sniffing"`
		} `json:"inbounds"`
		Routing struct {
			Rules []struct {
				Protocol    []string `json:"protocol"`
				Domain      []string `json:"domain"`
				OutboundTag string   `json:"outboundTag"`
			} `json:"rules"`
		} `json:"routing"`
	}
	if err := json.Unmarshal(raw, &part); err != nil {
		t.Fatal(err)
	}
	s := part.Inbounds[0].Sniffing
	if s == nil || !s.Enabled || !s.RouteOnly || strings.Join(s.DestOverride, ",") != "http,tls,quic" {
		t.Errorf("sniffing = %+v", s)
	}
	rules := part.Routing.Rules
	if len(rules) != 2 || rules[0].Protocol[0] != "bittorrent" || rules[0].OutboundTag != "block" {
		t.Fatalf("rules = %+v", rules)
	}
	d := rules[1].Domain
	if rules[1].OutboundTag != "block" || d[0] != "domain:speedtest.net" || d[len(d)-1] != "domain:example.net" {
		t.Errorf("domain rule = %+v", rules[1])
	}
}
