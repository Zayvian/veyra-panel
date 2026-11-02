package service

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"

	"github.com/Zayvian/veyra-panel/internal/singbox"
	"github.com/Zayvian/veyra-panel/internal/store"
)

// An inbound can be served by Xray instead of sing-box, for what only Xray has.
// Only VLESS with Reality, over plain TCP with Vision or over XHTTP: the rest
// of the protocols the panel offers are sing-box's strengths, and stay there.
//
// The stored inbound keeps sing-box's shape (store.Inbound.Config), and is
// translated here into Xray's when the node's configuration is built. Editing,
// renaming and the subscription code therefore read one shape, whichever core
// serves it.

// TransportXHTTP is Xray's XHTTP transport for VLESS. Empty means plain TCP.
const TransportXHTTP = "xhttp"

// ProtocolChoice is one entry of the protocol menu for a new inbound.
type ProtocolChoice struct {
	Value string
	Label string
}

// ProtocolChoices is that menu, in display order.
var ProtocolChoices = []ProtocolChoice{
	{store.ProtoVLESS, "vless"},
	{"xray-" + store.ProtoVLESS, "vless（Xray）"},
	{"xray-" + TransportXHTTP, "vless-xhttp（Xray）"},
	{store.ProtoAnyTLS, "anytls"},
	{store.ProtoShadowsocks, "shadowsocks"},
	{store.ProtoHysteria2, "hysteria2"},
	{store.ProtoTUIC, "tuic"},
}

// ParseProtocolChoice turns a menu value back into what an InboundSpec holds.
// An unknown value comes back as the protocol, for BuildInbound to refuse.
func ParseProtocolChoice(v string) (protocol, core, transport string) {
	switch v {
	case "xray-" + store.ProtoVLESS:
		return store.ProtoVLESS, store.CoreXray, ""
	case "xray-" + TransportXHTTP:
		return store.ProtoVLESS, store.CoreXray, TransportXHTTP
	default:
		return v, "", ""
	}
}

// ProtocolLabel is how an existing inbound is named in the panel: the menu
// label it was created from.
func ProtocolLabel(in *store.Inbound) string {
	if in.Core != store.CoreXray {
		return in.Protocol
	}
	if c, err := ParseClient(in); err == nil && c.Network == TransportXHTTP {
		return "vless-xhttp（Xray）"
	}
	return in.Protocol + "（Xray）"
}

// xrayInbound translates a stored VLESS + Reality inbound into Xray's inbound
// format. The client list is left empty: users reach the node separately, as
// for sing-box, and the node fills them in.
//
// sniff is the sniffing block a policy needs, or nil for none.
func xrayInbound(sb singbox.Inbound, sniff *xraySniffing) (json.RawMessage, error) {
	if sb.Type != store.ProtoVLESS || sb.TLS == nil || sb.TLS.Reality == nil {
		return nil, fmt.Errorf("inbound %s: only VLESS with Reality can be served by Xray", sb.Tag)
	}
	r := sb.TLS.Reality

	type realitySettings struct {
		Target      string   `json:"target"`
		ServerNames []string `json:"serverNames"`
		PrivateKey  string   `json:"privateKey"`
		ShortIDs    []string `json:"shortIds"`
	}
	type xhttpSettings struct {
		Path string `json:"path"`
		Mode string `json:"mode"`
	}
	type streamSettings struct {
		Network  string          `json:"network"`
		Security string          `json:"security"`
		XHTTP    *xhttpSettings  `json:"xhttpSettings,omitempty"`
		Reality  realitySettings `json:"realitySettings"`
	}
	type settings struct {
		Clients    []struct{} `json:"clients"`
		Decryption string     `json:"decryption"`
	}
	type inbound struct {
		Tag      string         `json:"tag"`
		Protocol string         `json:"protocol"`
		Listen   string         `json:"listen"`
		Port     int            `json:"port"`
		Settings settings       `json:"settings"`
		Stream   streamSettings `json:"streamSettings"`
		Sniffing *xraySniffing  `json:"sniffing,omitempty"`
	}

	out := inbound{
		Tag:      sb.Tag,
		Protocol: "vless",
		Listen:   sb.Listen,
		Port:     sb.ListenPort,
		Settings: settings{Clients: []struct{}{}, Decryption: "none"},
		Sniffing: sniff,
		Stream: streamSettings{
			Network:  "tcp",
			Security: "reality",
			Reality: realitySettings{
				Target:      net.JoinHostPort(r.Handshake.Server, strconv.Itoa(r.Handshake.ServerPort)),
				ServerNames: []string{sb.TLS.ServerName},
				// Both cores take the same key encoding: base64url without
				// padding, which is what newRealityKeypair writes.
				PrivateKey: r.PrivateKey,
				ShortIDs:   r.ShortID,
			},
		},
	}
	if sb.XHTTP != nil {
		out.Stream.Network = TransportXHTTP
		out.Stream.XHTTP = &xhttpSettings{Path: sb.XHTTP.Path, Mode: sb.XHTTP.Mode}
	}
	return json.Marshal(out)
}

// xraySniffing is an Xray inbound's sniffing block.
type xraySniffing struct {
	Enabled      bool     `json:"enabled"`
	DestOverride []string `json:"destOverride"`
	// RouteOnly uses what was sniffed for routing only, and still connects to
	// the address the client asked for: the policy decides, it does not
	// redirect.
	RouteOnly bool `json:"routeOnly"`
}

// xrayRule is one of Xray's routing rules.
type xrayRule struct {
	Type        string   `json:"type"`
	Protocol    []string `json:"protocol,omitempty"`
	Domain      []string `json:"domain,omitempty"`
	OutboundTag string   `json:"outboundTag"`
}

// xrayBlock is the outbound the node's Xray configuration drops traffic into.
const xrayBlock = "block"

// xrayPolicy is the policy written for Xray: the sniffing its inbounds need
// and the rules that act on it, the same blocks routeRules gives sing-box.
// Nil for an empty policy, so a node without one is configured as before.
//
// Relay listeners are sing-box's, so there is nothing to exempt here.
func (p Policy) xrayPolicy() (*xraySniffing, []xrayRule) {
	if !p.BlockBitTorrent && !p.BlockSpeedtest && len(p.BlockedDomains) == 0 {
		return nil, nil
	}
	// Xray sniffs BitTorrent whenever sniffing is on; destOverride only names
	// what may supply a domain.
	sniff := &xraySniffing{Enabled: true, DestOverride: []string{"http", "tls"}, RouteOnly: true}
	var rules []xrayRule
	if p.BlockBitTorrent {
		sniff.DestOverride = append(sniff.DestOverride, "quic")
		rules = append(rules, xrayRule{Type: "field", Protocol: []string{"bittorrent"}, OutboundTag: xrayBlock})
	}
	var domains []string
	if p.BlockSpeedtest {
		domains = append(domains, speedtestDomains...)
	}
	domains = append(domains, p.BlockedDomains...)
	if len(domains) > 0 {
		// "domain:" is Xray's suffix match, the same as sing-box's
		// domain_suffix: the name and everything under it.
		d := make([]string, len(domains))
		for i, v := range domains {
			d[i] = "domain:" + v
		}
		rules = append(rules, xrayRule{Type: "field", Domain: d, OutboundTag: xrayBlock})
	}
	return sniff, rules
}
