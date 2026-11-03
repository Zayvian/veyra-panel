package service

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zayvian/veyra-panel/internal/singbox"
	"github.com/Zayvian/veyra-panel/internal/store"
)

func mustExit(t *testing.T, svc *Service, e Exit) Exit {
	t.Helper()
	got, err := svc.SaveExit(e)
	if err != nil {
		t.Fatalf("save exit %s: %v", e.Name, err)
	}
	return got
}

// dumpConfig writes a configuration where VEYRA_DUMP_UNLOCK says, so the
// node's tests can check that sing-box and Xray accept what the panel builds.
func dumpConfig(t *testing.T, name string, v any) {
	dir := os.Getenv("VEYRA_DUMP_UNLOCK")
	if dir == "" {
		return
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func ruleFor(cfg *singbox.Config, match func(singbox.ServerRouteRule) bool) (int, singbox.ServerRouteRule) {
	if cfg.Route == nil {
		return -1, singbox.ServerRouteRule{}
	}
	for i, r := range cfg.Route.Rules {
		if match(r) {
			return i, r
		}
	}
	return -1, singbox.ServerRouteRule{}
}

// The whole feature on one node: a proxy exit for AI, a DNS unlock for
// Netflix, the policy still ahead of both, and everything else direct.
func TestUnlockRoutesChosenSitesThroughTheirExits(t *testing.T) {
	svc, hk, _ := relayFixture(t)
	mustInbound(t, svc, hk, InboundSpec{Protocol: store.ProtoVLESS, Port: 443})
	if err := svc.SetPolicy(Policy{BlockSpeedtest: true}); err != nil {
		t.Fatal(err)
	}
	us := mustExit(t, svc, Exit{Name: "US", Kind: ExitSOCKS, Server: "us.example.com", Port: 1080,
		Username: "u", Password: "p"})
	dns := mustExit(t, svc, Exit{Name: "NF", Kind: ExitDNS, Server: "203.0.113.53"})
	if dns.Port != 53 {
		t.Errorf("dns port = %d, want 53 by default", dns.Port)
	}
	spy := &spyNotifier{}
	svc.SetNotifier(spy)
	if err := svc.SetNodeUnlock(hk, []UnlockRule{
		{ExitID: us.ID, Services: []string{"openai", "claude"}, Domains: []string{"https://Example.AI/x"}},
		{ExitID: dns.ID, Services: []string{"netflix"}},
	}); err != nil {
		t.Fatal(err)
	}
	if len(spy.config) != 1 || spy.config[0] != hk {
		t.Errorf("pushed to %v, want only the node", spy.config)
	}

	cfg, err := svc.NodeConfig(hk)
	if err != nil {
		t.Fatal(err)
	}
	dumpConfig(t, "singbox-unlock.json", cfg)

	if len(cfg.Outbounds) != 2 || cfg.Outbounds[0].Tag != "direct" ||
		cfg.Outbounds[1].Type != "socks" || cfg.Outbounds[1].Server != "us.example.com" ||
		cfg.Outbounds[1].Username != "u" {
		t.Fatalf("outbounds = %+v", cfg.Outbounds)
	}
	sniff, s := ruleFor(cfg, func(r singbox.ServerRouteRule) bool { return r.Action == "sniff" })
	if sniff < 0 || !strings.Contains(strings.Join(s.Sniffer, ","), "quic") {
		t.Errorf("sniff rule = %+v, want one that reads QUIC too", s)
	}
	block, _ := ruleFor(cfg, func(r singbox.ServerRouteRule) bool { return r.Action == "reject" })
	route, ai := ruleFor(cfg, func(r singbox.ServerRouteRule) bool { return r.Action == "route" })
	resolve, nf := ruleFor(cfg, func(r singbox.ServerRouteRule) bool { return r.Action == "resolve" })
	if !(sniff < block && block < route && block < resolve) {
		t.Errorf("rule order sniff %d, block %d, route %d, resolve %d: the policy must come first",
			sniff, block, route, resolve)
	}
	if ai.Outbound != cfg.Outbounds[1].Tag {
		t.Errorf("AI rule goes to %q", ai.Outbound)
	}
	joined := strings.Join(ai.DomainSuffix, " ")
	for _, d := range []string{"chatgpt.com", "claude.ai", "example.ai"} {
		if !strings.Contains(joined, d) {
			t.Errorf("AI rule %v lacks %s", ai.DomainSuffix, d)
		}
	}
	if strings.Contains(joined, "netflix") {
		t.Errorf("AI rule %v carries Netflix", ai.DomainSuffix)
	}
	if cfg.DNS == nil || cfg.DNS.Final != "local" || len(cfg.DNS.Servers) != 2 ||
		cfg.DNS.Servers[1].Server != "203.0.113.53" || cfg.DNS.Servers[1].ServerPort != 53 {
		t.Fatalf("dns = %+v", cfg.DNS)
	}
	if nf.Server != cfg.DNS.Servers[1].Tag || !strings.Contains(strings.Join(nf.DomainSuffix, " "), "netflix.com") {
		t.Errorf("netflix rule = %+v", nf)
	}
	if cfg.Route.DefaultDomainResolver != "local" {
		t.Errorf("default domain resolver = %q, want local", cfg.Route.DefaultDomainResolver)
	}

	// The exit is in use, so it stays.
	if err := svc.DeleteExit(us.ID); !errors.Is(err, ErrInvalid) {
		t.Errorf("deleting an exit in use: %v, want refused", err)
	}
	// Clearing the rules takes everything back out.
	if err := svc.SetNodeUnlock(hk, nil); err != nil {
		t.Fatal(err)
	}
	cfg, _ = svc.NodeConfig(hk)
	if len(cfg.Outbounds) != 1 || cfg.DNS != nil || cfg.Route == nil ||
		len(cfg.Route.Rules) != 2 || cfg.Route.DefaultDomainResolver != "" {
		t.Errorf("after clearing: outbounds %+v, dns %+v, route %+v", cfg.Outbounds, cfg.DNS, cfg.Route)
	}
	if err := svc.DeleteExit(us.ID); err != nil {
		t.Errorf("deleting an exit no longer used: %v", err)
	}
}

// Another node as an exit: the panel opens the listener on it, and the node
// that sends to it gets a matching outbound.
func TestNodeExitOpensAListenerOnThatNode(t *testing.T) {
	svc, hk, jp := relayFixture(t)
	in := mustInbound(t, svc, jp, InboundSpec{Protocol: store.ProtoVLESS, Port: 443})
	if _, err := svc.SaveExit(Exit{Name: "JP", Kind: ExitNode, NodeID: jp, Port: in.Port}); !errors.Is(err, ErrInvalid) {
		t.Errorf("exit on a port the node already uses: %v, want refused", err)
	}
	spy := &spyNotifier{}
	svc.SetNotifier(spy)
	jpExit := mustExit(t, svc, Exit{Name: "JP", Kind: ExitNode, NodeID: jp})
	if jpExit.Port < 20000 || jpExit.Port >= 50000 || jpExit.ServerKey == "" || jpExit.UserKey == "" {
		t.Fatalf("exit = %+v, want a port picked and keys made", jpExit)
	}
	if len(spy.config) != 1 || spy.config[0] != jp {
		t.Errorf("pushed to %v, want the exit's node", spy.config)
	}
	if err := svc.SetNodeUnlock(jp, []UnlockRule{{ExitID: jpExit.ID, Services: []string{"netflix"}}}); !errors.Is(err, ErrInvalid) {
		t.Errorf("a node sending to itself: %v, want refused", err)
	}
	if err := svc.SetNodeUnlock(hk, []UnlockRule{{ExitID: jpExit.ID, Services: []string{"netflix"}}}); err != nil {
		t.Fatal(err)
	}

	// With a policy, so that jp has routing for the listener to be exempt from.
	if err := svc.SetPolicy(Policy{BlockSpeedtest: true}); err != nil {
		t.Fatal(err)
	}
	jpCfg, err := svc.NodeConfig(jp)
	if err != nil {
		t.Fatal(err)
	}
	dumpConfig(t, "singbox-exit.json", jpCfg)
	var listener *singbox.Inbound
	for i := range jpCfg.Inbounds {
		if jpCfg.Inbounds[i].Tag == exitInboundTag(jpExit.ID) {
			listener = &jpCfg.Inbounds[i]
		}
	}
	if listener == nil || listener.Type != "shadowsocks" || listener.ListenPort != jpExit.Port ||
		listener.Password != jpExit.ServerKey || len(listener.Users) != 1 ||
		listener.Users[0].Password != jpExit.UserKey {
		t.Fatalf("exit listener = %+v", listener)
	}
	// Exempt from routing, so it cannot be sent anywhere but out.
	if jpCfg.Route == nil || jpCfg.Route.Rules[0].Action != "route" ||
		jpCfg.Route.Rules[0].Outbound != "direct" || jpCfg.Route.Rules[0].Inbound[0] != listener.Tag {
		t.Errorf("jp route = %+v, want the exit listener bypassed", jpCfg.Route)
	}
	// Its users are the panel's own and never touched by the user list.
	users, err := svc.NodeUsers(jp)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := users[listener.Tag]; ok {
		t.Errorf("exit listener is in the user list")
	}

	hkCfg, err := svc.NodeConfig(hk)
	if err != nil {
		t.Fatal(err)
	}
	out := hkCfg.Outbounds[1]
	if out.Type != "shadowsocks" || out.Server != "jp.example.com" && out.Server != "hk.example.com" ||
		out.ServerPort != jpExit.Port || out.Password != jpExit.ServerKey+":"+jpExit.UserKey {
		t.Errorf("outbound = %+v", out)
	}

	// An edit keeps the keys; turning the exit's node off sends the sites
	// straight out instead of to a listener that is gone.
	again := mustExit(t, svc, Exit{ID: jpExit.ID, Name: "JP", Kind: ExitNode, NodeID: jp, Port: 31234})
	if again.ServerKey != jpExit.ServerKey || again.Port != 31234 {
		t.Errorf("edited exit = %+v", again)
	}
	jpNode, err := svc.Node(jp)
	if err != nil {
		t.Fatal(err)
	}
	jpNode.Enabled = false
	if err := svc.UpdateNode(jpNode); err != nil {
		t.Fatal(err)
	}
	hkCfg, _ = svc.NodeConfig(hk)
	if len(hkCfg.Outbounds) != 1 {
		t.Errorf("outbounds with the exit node off = %+v", hkCfg.Outbounds)
	}
}

// Xray's inbounds hand the chosen sites to sing-box over a loopback listener,
// and only a node that said it can take that is sent it.
func TestUnlockReachesXrayInboundsThroughTheBridge(t *testing.T) {
	svc, hk, _ := relayFixture(t)
	mustInbound(t, svc, hk, InboundSpec{Protocol: store.ProtoVLESS, Core: store.CoreXray, Port: 8443,
		Handshake: "www.example.com:443"})
	us := mustExit(t, svc, Exit{Name: "US", Kind: ExitVLESS, Server: "203.0.113.9", Port: 443,
		UUID: "6f9619ff-8b86-d011-b42d-00cf4fc964ff", Flow: FlowVision, Security: "reality",
		SNI: "www.example.com", PublicKey: "jNXHt1yRo0vDuchQlIP6Z0ZvjT3KtzVI-T4E7RoLJS0", ShortID: "0123"})
	if err := svc.SetNodeUnlock(hk, []UnlockRule{{ExitID: us.ID, Services: []string{"gemini"}}}); err != nil {
		t.Fatal(err)
	}

	cfg, err := svc.NodeConfig(hk)
	if err != nil {
		t.Fatal(err)
	}
	dumpConfig(t, "singbox-bridge.json", cfg)
	var bridge *singbox.Inbound
	for i := range cfg.Inbounds {
		if cfg.Inbounds[i].Tag == xrayBridgeTag {
			bridge = &cfg.Inbounds[i]
		}
	}
	if bridge == nil || bridge.Type != "socks" || bridge.Listen != "127.0.0.1" || bridge.ListenPort == 0 {
		t.Fatalf("bridge = %+v", bridge)
	}
	if cfg.Outbounds[1].TLS == nil || cfg.Outbounds[1].TLS.Reality == nil ||
		cfg.Outbounds[1].TLS.UTLS == nil || cfg.Outbounds[1].Flow != FlowVision {
		t.Errorf("vless outbound = %+v", cfg.Outbounds[1])
	}

	raw, err := svc.NodeXrayConfig(hk, true)
	if err != nil {
		t.Fatal(err)
	}
	dumpConfig(t, "xray-bridge.json", json.RawMessage(raw))
	var part struct {
		Inbounds []struct {
			Sniffing *xraySniffing `json:"sniffing"`
		} `json:"inbounds"`
		Outbounds []struct {
			Tag      string `json:"tag"`
			Protocol string `json:"protocol"`
			Settings struct {
				Servers []struct {
					Address string `json:"address"`
					Port    int    `json:"port"`
				} `json:"servers"`
			} `json:"settings"`
		} `json:"outbounds"`
		Unlock []xrayUnlockRule `json:"unlock"`
	}
	if err := json.Unmarshal(raw, &part); err != nil {
		t.Fatal(err)
	}
	if len(part.Outbounds) != 1 || part.Outbounds[0].Protocol != "socks" ||
		part.Outbounds[0].Settings.Servers[0].Port != bridge.ListenPort {
		t.Errorf("xray outbounds = %+v, want the bridge on %d", part.Outbounds, bridge.ListenPort)
	}
	if len(part.Unlock) != 1 || part.Unlock[0].Outbound != part.Outbounds[0].Tag ||
		!strings.Contains(strings.Join(part.Unlock[0].DomainSuffix, " "), "gemini.google.com") {
		t.Errorf("xray unlock = %+v", part.Unlock)
	}
	if s := part.Inbounds[0].Sniffing; s == nil || !s.Enabled || !s.RouteOnly {
		t.Errorf("sniffing = %+v, want on and route-only", s)
	}

	// A node that cannot take it gets the inbounds as before.
	raw, err = svc.NodeXrayConfig(hk, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "unlock") || strings.Contains(string(raw), "sniffing") {
		t.Errorf("xray part without the capability = %s", raw)
	}
}

func TestUnlockRefusesAmbiguousRules(t *testing.T) {
	svc, hk, _ := relayFixture(t)
	a := mustExit(t, svc, Exit{Name: "A", Kind: ExitSOCKS, Server: "a.example", Port: 1080})
	b := mustExit(t, svc, Exit{Name: "B", Kind: ExitSOCKS, Server: "b.example", Port: 1080})
	for name, rules := range map[string][]UnlockRule{
		"service twice": {{ExitID: a.ID, Services: []string{"openai"}}, {ExitID: b.ID, Services: []string{"openai"}}},
		"domain twice":  {{ExitID: a.ID, Domains: []string{"x.example"}}, {ExitID: b.ID, Domains: []string{"X.example"}}},
		"no such exit":  {{ExitID: 99, Services: []string{"openai"}}},
		"no such svc":   {{ExitID: a.ID, Services: []string{"myspace"}}},
	} {
		if err := svc.SetNodeUnlock(hk, rules); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v, want refused", name, err)
		}
	}
	for name, e := range map[string]Exit{
		"no name":      {Kind: ExitSOCKS, Server: "a", Port: 1},
		"same name":    {Name: "A", Kind: ExitSOCKS, Server: "a", Port: 1},
		"dns by name":  {Name: "D", Kind: ExitDNS, Server: "dns.example"},
		"ss no method": {Name: "S", Kind: ExitShadowsocks, Server: "a", Port: 1, Password: "x"},
		"vless no key": {Name: "V", Kind: ExitVLESS, Server: "a", Port: 1, Security: "reality",
			UUID: "6f9619ff-8b86-d011-b42d-00cf4fc964ff", SNI: "a"},
	} {
		if _, err := svc.SaveExit(e); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v, want refused", name, err)
		}
	}
	if _, err := svc.SaveExit(Exit{Name: "D", Kind: ExitDNS, Server: "[2001:db8::53]:5353"}); err != nil {
		t.Errorf("dns with a v6 address and port: %v", err)
	}
	if _, err := svc.CreateInbound(hk, InboundSpec{Protocol: store.ProtoVLESS, Port: 443, Tag: "veyra-exit-1"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("an inbound tag in the panel's own namespace: %v, want refused", err)
	}
}
