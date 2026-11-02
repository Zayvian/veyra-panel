package sub

import (
	"gopkg.in/yaml.v3"
	"time"

	"github.com/Zayvian/veyra-panel/internal/store"
)

// clashProxy is mihomo's proxy schema. Field order here is the order they
// appear in the output, which matters only for readability.
//
// Written through a real YAML marshaller rather than a template: every
// credential this emits is base64 or base64url, so it contains '+', '/' and
// '=' — characters that a hand-rolled emitter gets wrong exactly often enough
// to be hard to notice.
type clashProxy struct {
	Name              string   `yaml:"name"`
	Type              string   `yaml:"type"`
	Server            string   `yaml:"server"`
	Port              int      `yaml:"port"`
	UDP               bool     `yaml:"udp"`
	Ports             string   `yaml:"ports,omitempty"`
	HopInterval       int      `yaml:"hop-interval,omitempty"`
	ALPN              []string `yaml:"alpn,omitempty"`
	CongestionControl string   `yaml:"congestion-controller,omitempty"`
	Obfs              string   `yaml:"obfs,omitempty"`
	ObfsPassword      string   `yaml:"obfs-password,omitempty"`

	UUID    string `yaml:"uuid,omitempty"`
	Flow    string `yaml:"flow,omitempty"`
	Network string `yaml:"network,omitempty"`
	TLS     bool   `yaml:"tls,omitempty"`

	Password string `yaml:"password,omitempty"`
	Cipher   string `yaml:"cipher,omitempty"`

	SNI               string            `yaml:"servername,omitempty"`
	AnyTLSSNI         string            `yaml:"sni,omitempty"`
	ClientFingerprint string            `yaml:"client-fingerprint,omitempty"`
	RealityOpts       *clashRealityOpts `yaml:"reality-opts,omitempty"`
}

type clashRealityOpts struct {
	PublicKey string `yaml:"public-key"`
	ShortID   string `yaml:"short-id,omitempty"`
}

type clashGroup struct {
	Name    string   `yaml:"name"`
	Type    string   `yaml:"type"`
	Proxies []string `yaml:"proxies"`
}

// clashProvider is a rule list mihomo downloads and keeps on disk.
type clashProvider struct {
	Type     string `yaml:"type"`
	Behavior string `yaml:"behavior"`
	Format   string `yaml:"format"`
	URL      string `yaml:"url"`
	Path     string `yaml:"path"`
	Interval int    `yaml:"interval"`
}

type clashConfig struct {
	MixedPort     int                      `yaml:"mixed-port"`
	AllowLAN      bool                     `yaml:"allow-lan"`
	Mode          string                   `yaml:"mode"`
	LogLevel      string                   `yaml:"log-level"`
	Proxies       []clashProxy             `yaml:"proxies"`
	ProxyGroups   []clashGroup             `yaml:"proxy-groups"`
	RuleProviders map[string]clashProvider `yaml:"rule-providers,omitempty"`
	Rules         []string                 `yaml:"rules"`
}

// Clash renders a mihomo configuration.
//
// AnyTLS is included. mihomo supports it; the older Clash forks do not and will
// reject the file outright rather than skip the proxy. Anyone on one of those
// should use the sing-box or share-link format instead.
//
// rulesBase is the origin the rule lists are downloaded from: the panel.
func Clash(entries []Entry, rulesBase string) ([]byte, error) {
	proxies := make([]clashProxy, 0, len(entries))
	kept := make([]Entry, 0, len(entries))

	for _, e := range entries {
		p := clashProxy{
			Name: e.Name, Server: e.Address, Port: e.Port, UDP: true,
			ClientFingerprint: fpOr(e.FP),
		}
		switch e.Protocol {
		case store.ProtoVLESS:
			p.Type = "vless"
			p.UUID = e.UUID
			p.Flow = e.Flow
			p.Network = "tcp"
			p.TLS = true
			p.SNI = e.SNI
			p.RealityOpts = &clashRealityOpts{PublicKey: e.PBK, ShortID: e.SID}
		case store.ProtoAnyTLS:
			p.Type = "anytls"
			p.Password = e.Password
			p.AnyTLSSNI = e.SNI
		case store.ProtoHysteria2, store.ProtoTUIC:
			p.Type, p.Password, p.AnyTLSSNI = e.Protocol, e.Password, e.SNI
			p.ClientFingerprint = ""
			p.ALPN = []string{"h3"}
			if e.Protocol == store.ProtoTUIC {
				p.UUID = e.UUID
				p.CongestionControl = "bbr"
			}
			if e.Protocol == store.ProtoHysteria2 && e.HopPorts != "" {
				p.Ports = e.HopPorts
				d, _ := time.ParseDuration(e.HopInterval)
				p.HopInterval = int(d / time.Second)
			}
			if e.Protocol == store.ProtoHysteria2 && e.Obfs != "" {
				p.Obfs, p.ObfsPassword = "salamander", e.Obfs
			}
		case store.ProtoShadowsocks:
			p.Type = "ss"
			p.Cipher = e.Method
			p.Password = e.Password
			p.ClientFingerprint = "" // meaningless without TLS
		default:
			continue
		}
		proxies = append(proxies, p)
		kept = append(kept, e)
	}

	groups := []clashGroup{}
	rules := []string{"MATCH,DIRECT"}
	var providers map[string]clashProvider
	if len(kept) > 0 {
		groups, rules, providers = clashRouting(kept, rulesBase)
	}

	return yaml.Marshal(clashConfig{
		MixedPort: 7890, AllowLAN: false, Mode: "rule", LogLevel: "warning",
		Proxies: proxies, ProxyGroups: groups, RuleProviders: providers, Rules: rules,
	})
}

// clashRouting builds the groups, rules and rule lists of routing.go.
func clashRouting(kept []Entry, rulesBase string) ([]clashGroup, []string, map[string]clashProvider) {
	nodes, info := splitInfo(kept)

	var groups []clashGroup
	// Announcements get a group of their own, listed first so the client
	// shows them on top, and out of every group traffic actually uses.
	if len(info) > 0 {
		groups = append(groups, clashGroup{Name: groupNotice, Type: "select", Proxies: info})
	}
	groups = append(groups, clashGroup{Name: groupProxy, Type: "select", Proxies: nodes})
	for _, g := range serviceGroups {
		groups = append(groups, clashGroup{Name: g.name, Type: "select", Proxies: groupMembers(g, nodes)})
	}
	groups = append(groups, clashGroup{Name: groupFinal, Type: "select",
		Proxies: groupMembers(routeGroup{name: groupFinal}, nodes)})

	providers := map[string]clashProvider{}
	rules := []string{"GEOIP,PRIVATE,DIRECT,no-resolve"}
	for _, r := range routeRules {
		for _, set := range r.sets {
			behavior := "domain"
			if set.kind == "geoip" {
				behavior = "ipcidr"
			}
			providers[set.tag()] = clashProvider{
				Type: "http", Behavior: behavior, Format: "mrs",
				URL:      ruleFileURL(rulesBase, set, "mrs"),
				Path:     "./ruleset/" + set.tag() + ".mrs",
				Interval: 86400,
			}
			rule := "RULE-SET," + set.tag() + "," + r.target
			// An address list only needs resolving for China, where an
			// unlisted domestic name should still go direct. For the others,
			// matching apps that dial addresses outright is enough.
			if set.kind == "geoip" && set.name != "cn" {
				rule += ",no-resolve"
			}
			rules = append(rules, rule)
		}
	}
	rules = append(rules, "MATCH,"+groupFinal)
	return groups, rules, providers
}
