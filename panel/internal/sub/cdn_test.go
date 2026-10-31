package sub

import (
	"encoding/json"
	"net/url"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Zayvian/veyra-panel/internal/service"
	"github.com/Zayvian/veyra-panel/internal/store"
)

// A CDN entry dials one address and names another site in TLS and in the
// Host header; every format has to carry both, plus the path, or the CDN
// cannot route the request to the node.
func TestCDNVLESSInEveryFormat(t *testing.T) {
	e := Entry{Name: "cdn", Label: "cdn", Protocol: store.ProtoVLESSWS,
		Address: "104.16.1.1", Port: 2053, SNI: "cdn.example.com", Host: "cdn.example.com",
		Path: "/abcDEF123", FP: "chrome", UUID: "11111111-1111-1111-1111-111111111111"}

	link := ShareLinks([]Entry{e})[0]
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Scheme != "vless" || u.Host != "104.16.1.1:2053" || u.User.Username() != e.UUID ||
		q.Get("type") != "ws" || q.Get("security") != "tls" || q.Get("sni") != "cdn.example.com" ||
		q.Get("host") != "cdn.example.com" || q.Get("path") != "/abcDEF123?ed=2048" ||
		q.Get("flow") != "" || q.Get("pbk") != "" {
		t.Fatalf("share link: %s", link)
	}

	raw, err := Clash([]Entry{e}, testRules)
	if err != nil {
		t.Fatal(err)
	}
	var clash struct {
		Proxies []struct {
			Type, Server, Network, Servername, Flow string
			TLS                                     bool
			WSOpts                                  struct {
				Path                string
				Headers             map[string]string
				MaxEarlyData        int    `yaml:"max-early-data"`
				EarlyDataHeaderName string `yaml:"early-data-header-name"`
			} `yaml:"ws-opts"`
			RealityOpts any `yaml:"reality-opts"`
		}
	}
	if err := yaml.Unmarshal(raw, &clash); err != nil {
		t.Fatal(err)
	}
	p := clash.Proxies[0]
	if p.Type != "vless" || p.Network != "ws" || !p.TLS || p.Servername != "cdn.example.com" ||
		p.Flow != "" || p.RealityOpts != nil || p.WSOpts.Path != "/abcDEF123" ||
		p.WSOpts.Headers["Host"] != "cdn.example.com" || p.WSOpts.MaxEarlyData != 2048 ||
		p.WSOpts.EarlyDataHeaderName != "Sec-WebSocket-Protocol" {
		t.Fatalf("clash proxy: %+v\n%s", p, raw)
	}

	raw, err = SingBox([]Entry{e}, testRules)
	if err != nil {
		t.Fatal(err)
	}
	var sb struct {
		Outbounds []struct {
			Tag, Type, Flow string
			TLS             *struct {
				ServerName string `json:"server_name"`
				Reality    any
			}
			Transport *struct {
				Type, Path          string
				Headers             map[string]string
				MaxEarlyData        int    `json:"max_early_data"`
				EarlyDataHeaderName string `json:"early_data_header_name"`
			}
		}
	}
	if err := json.Unmarshal(raw, &sb); err != nil {
		t.Fatal(err)
	}
	for _, ob := range sb.Outbounds {
		if ob.Tag != "cdn" {
			continue
		}
		if ob.Type != "vless" || ob.Flow != "" || ob.TLS == nil || ob.TLS.ServerName != "cdn.example.com" ||
			ob.TLS.Reality != nil || ob.Transport == nil || ob.Transport.Type != "ws" ||
			ob.Transport.Path != "/abcDEF123" || ob.Transport.Headers["Host"] != "cdn.example.com" ||
			ob.Transport.MaxEarlyData != 2048 {
			t.Fatalf("sing-box outbound: %s", raw)
		}
		return
	}
	t.Fatalf("no cdn outbound: %s", raw)
}

// The node's own address is often the very IP the firewall blocked, which is
// why the CDN inbound exists; handing it to clients would make the CDN inbound
// time out exactly like the inbounds it was meant to replace.
func TestCDNVLESSDialsTheCDNDomain(t *testing.T) {
	user, nodes, _ := fixture(t)
	nodes[0].Address = "38.64.60.249"
	in, err := service.BuildInbound(service.InboundSpec{Tag: "cdn", Protocol: store.ProtoVLESSWS,
		Port: 2053, ServerName: "cdn.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	in.NodeID = nodes[0].ID
	entries, err := Build(user, nodes, []*store.Inbound{in}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Address != "cdn.example.com" || entries[0].Port != 2053 {
		t.Fatalf("entries: %+v", entries)
	}

	// An explicit address, such as a preferred CDN IP, still wins.
	in.Address = "104.16.1.1"
	entries, err = Build(user, nodes, []*store.Inbound{in}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].Address != "104.16.1.1" || entries[0].Host != "cdn.example.com" {
		t.Fatalf("entries: %+v", entries)
	}
}
