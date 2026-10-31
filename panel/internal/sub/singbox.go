package sub

import (
	"encoding/json"
	"net"
	"strings"

	"github.com/Zayvian/veyra-panel/internal/service"
	"github.com/Zayvian/veyra-panel/internal/singbox"
	"github.com/Zayvian/veyra-panel/internal/store"
)

// Tags for the outbounds the panel always emits. The main group is the shared
// groupProxy; tagProxy names it here for the DNS detour and the empty case.
const (
	tagProxy  = groupProxy
	tagDirect = "direct"
)

// SingBox renders a complete sing-box client configuration.
//
// The proxy outbounds are the point of this file; the DNS and inbound blocks
// are a small working default, and the routing is the shared one of
// routing.go.
//
// rulesBase is the origin the rule sets are downloaded from: the panel.
func SingBox(entries []Entry, rulesBase string) ([]byte, error) {
	outbounds := make([]singbox.ClientOutbound, 0, len(entries)+12)
	kept := make([]Entry, 0, len(entries))

	for _, e := range entries {
		ob := singbox.ClientOutbound{
			Tag: e.Name, Server: e.Address, ServerPort: e.Port,
		}
		switch e.Protocol {
		case store.ProtoVLESS:
			ob.Type = "vless"
			ob.UUID = e.UUID
			ob.Flow = e.Flow
			ob.TLS = &singbox.ClientTLS{
				Enabled:    true,
				ServerName: e.SNI,
				// Reality hides behind a browser-shaped handshake, so uTLS is
				// not optional here.
				UTLS:    &singbox.UTLS{Enabled: true, Fingerprint: fpOr(e.FP)},
				Reality: &singbox.ClientReality{Enabled: true, PublicKey: e.PBK, ShortID: e.SID},
			}
		case store.ProtoVLESSWS:
			ob.Type = "vless"
			ob.UUID = e.UUID
			ob.TLS = &singbox.ClientTLS{
				Enabled:    true,
				ServerName: e.SNI,
				UTLS:       &singbox.UTLS{Enabled: true, Fingerprint: fpOr(e.FP)},
			}
			ob.Transport = &singbox.ClientTransport{
				Type: "ws", Path: e.Path, Headers: map[string]string{"Host": e.Host},
				MaxEarlyData: service.WSEarlyData, EarlyDataHeaderName: service.WSEarlyDataHeader,
			}
		case store.ProtoAnyTLS:
			ob.Type = "anytls"
			ob.Password = e.Password
			ob.TLS = &singbox.ClientTLS{
				Enabled:    true,
				ServerName: e.SNI,
				UTLS:       &singbox.UTLS{Enabled: true, Fingerprint: fpOr(e.FP)},
			}
		case store.ProtoHysteria2, store.ProtoTUIC:
			ob.Type = e.Protocol
			ob.Password = e.Password
			ob.TLS = &singbox.ClientTLS{Enabled: true, ServerName: e.SNI, ALPN: []string{"h3"}}
			if e.Protocol == store.ProtoTUIC {
				ob.UUID = e.UUID
				ob.CongestionControl = "bbr"
			}
			if e.Protocol == store.ProtoHysteria2 && e.HopPorts != "" {
				ob.ServerPort = 0
				ob.ServerPorts = strings.Split(strings.ReplaceAll(e.HopPorts, "-", ":"), ",")
				ob.HopInterval = e.HopInterval
			}
			if e.Protocol == store.ProtoHysteria2 && e.Obfs != "" {
				ob.Obfs = &singbox.Obfs{Type: "salamander", Password: e.Obfs}
			}
		case store.ProtoShadowsocks:
			ob.Type = "shadowsocks"
			ob.Method = e.Method
			ob.Password = e.Password
		default:
			continue
		}
		outbounds = append(outbounds, ob)
		kept = append(kept, e)
	}

	var rules []singbox.RouteRule
	var sets []singbox.RuleSet
	if len(kept) == 0 {
		// An inactive or unassigned user still gets a parseable config. A
		// selector with no members makes sing-box refuse to start, which looks
		// to the user like a broken client rather than an expired account.
		outbounds = append(outbounds, singbox.ClientOutbound{
			Type: "selector", Tag: tagProxy, Outbounds: []string{tagDirect}, Default: tagDirect})
	} else {
		var groups []singbox.ClientOutbound
		groups, rules, sets = singBoxRouting(kept, rulesBase)
		outbounds = append(outbounds, groups...)
	}
	outbounds = append(outbounds, singbox.ClientOutbound{Type: tagDirect, Tag: tagDirect})

	dns := &singbox.DNS{
		Servers: []singbox.DNSServer{
			{Type: "https", Tag: "remote", Server: "1.1.1.1", Detour: tagProxy},
			// No detour: that already means direct, and naming the empty
			// direct outbound explicitly is rejected as meaningless.
			{Type: "https", Tag: "local", Server: "223.5.5.5"},
		},
		Final: "remote",
	}
	// Resolving a server's own hostname through the proxy that has not been
	// dialled yet is a loop. Send those names to the local resolver.
	if hosts := serverDomains(entries); len(hosts) > 0 {
		dns.Rules = []singbox.DNSRule{{Domain: hosts, Server: "local"}}
	}
	// Domestic names resolve at home, so they get domestic CDN addresses.
	if len(sets) > 0 {
		dns.Rules = append(dns.Rules, singbox.DNSRule{RuleSet: []string{"geosite-cn"}, Server: "local"})
	}

	cfg := singbox.ClientConfig{
		Log: &singbox.Log{Level: "warn"},
		DNS: dns,
		Inbounds: []singbox.ClientInbound{{
			Type: "mixed", Tag: "mixed-in", Listen: "127.0.0.1", ListenPort: 2080,
		}},
		Outbounds: outbounds,
		Route: &singbox.Route{
			Rules:   append([]singbox.RouteRule{{IPIsPriv: true, Outbound: tagDirect}}, rules...),
			RuleSet: sets,
			Final:   final(len(sets) > 0),
			Auto:    true,
			// Which resolver turns a dialled name into an address. "local" so
			// that a node addressed by name is resolved outside the tunnel that
			// name is the far end of.
			DefaultDomainResolver: &singbox.DomainResolver{Server: "local"},
		},
	}
	if len(sets) > 0 {
		// Keeps the downloaded rule sets, so a restart does not depend on
		// fetching them again.
		cfg.Experimental = &singbox.ClientExperimental{CacheFile: &singbox.CacheFile{Enabled: true}}
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func final(routed bool) string {
	if routed {
		return groupFinal
	}
	return tagProxy
}

// singBoxRouting builds the groups, rules and rule sets of routing.go.
func singBoxRouting(kept []Entry, rulesBase string) ([]singbox.ClientOutbound, []singbox.RouteRule, []singbox.RuleSet) {
	nodes, info := splitInfo(kept)
	tag := func(t string) string {
		if t == targetDirect {
			return tagDirect
		}
		return t
	}
	selector := func(name string, members []string) singbox.ClientOutbound {
		out := make([]string, len(members))
		for i, m := range members {
			out[i] = tag(m)
		}
		return singbox.ClientOutbound{Type: "selector", Tag: name, Outbounds: out, Default: out[0]}
	}

	var groups []singbox.ClientOutbound
	if len(info) > 0 {
		groups = append(groups, selector(groupNotice, info))
	}
	groups = append(groups, selector(groupProxy, nodes))
	for _, g := range serviceGroups {
		groups = append(groups, selector(g.name, groupMembers(g, nodes)))
	}
	groups = append(groups, selector(groupFinal, groupMembers(routeGroup{name: groupFinal}, nodes)))

	// Sniffing recovers the domain of a connection that arrives as a bare
	// address (a TUN inbound), so the domain lists can match it.
	rules := []singbox.RouteRule{{Action: "sniff"}}
	var sets []singbox.RuleSet
	for _, r := range routeRules {
		var tags []string
		for _, set := range r.sets {
			tags = append(tags, set.tag())
			sets = append(sets, singbox.RuleSet{
				Type: "remote", Tag: set.tag(), Format: "binary",
				URL: ruleFileURL(rulesBase, set, "srs"),
				// Straight to the panel. Left unset, sing-box downloads
				// through the first outbound — a node, which may be down.
				// Deprecated in 1.14 for http_client, which clients older
				// than 1.14 reject; removed in 1.16, by when it must move.
				DownloadDetour: tagDirect,
				UpdateInterval: "1d",
			})
		}
		if r.sets[0] == (ruleSet{"geoip", "cn"}) {
			// A domestic site missing from the domain list should still go
			// direct; its address is what gives it away.
			rules = append(rules, singbox.RouteRule{Action: "resolve"})
		}
		rules = append(rules, singbox.RouteRule{RuleSet: tags, Outbound: tag(r.target)})
	}
	return groups, rules, sets
}

// serverDomains is the set of node addresses that are names rather than
// literal addresses, deduplicated and in first-seen order.
func serverDomains(entries []Entry) []string {
	seen := make(map[string]bool, len(entries))
	var hosts []string
	for _, e := range entries {
		if e.Address == "" || net.ParseIP(e.Address) != nil || seen[e.Address] {
			continue
		}
		seen[e.Address] = true
		hosts = append(hosts, e.Address)
	}
	return hosts
}

func fpOr(fp string) string {
	if fp == "" {
		return "chrome"
	}
	return fp
}
