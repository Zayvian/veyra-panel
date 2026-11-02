package sub

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Zayvian/veyra-panel/internal/service"
	"github.com/Zayvian/veyra-panel/internal/store"
)

// xhttpEntries is one user on a VLESS + XHTTP inbound served by Xray, beside
// a VLESS inbound on plain TCP served by Xray too.
func xhttpEntries(t *testing.T) (xh Entry, tcp Entry) {
	t.Helper()
	node := &store.Node{ID: 1, Name: "tokyo", Address: "jp.example.com", Enabled: true, RateMilli: 1000}
	var inbounds []*store.Inbound
	for i, spec := range []service.InboundSpec{
		{Protocol: store.ProtoVLESS, Core: store.CoreXray, Transport: service.TransportXHTTP,
			Tag: "xhttp-tokyo", Port: 2053},
		{Protocol: store.ProtoVLESS, Core: store.CoreXray, Tag: "vless-tokyo", Port: 443},
	} {
		in, err := service.BuildInbound(spec)
		if err != nil {
			t.Fatal(err)
		}
		in.ID, in.NodeID = int64(i+1), node.ID
		inbounds = append(inbounds, in)
	}
	u := &store.User{ID: 1, Name: "alice", Enabled: true, VlessUUID: service.NewUUID()}
	entries, err := Build(u, []*store.Node{node}, inbounds, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
	if entries[0].Network != "xhttp" {
		entries[0], entries[1] = entries[1], entries[0]
	}
	if entries[0].Network != "xhttp" || entries[1].Network != "" {
		t.Fatalf("entries = %+v, want one xhttp and one tcp", entries)
	}
	return entries[0], entries[1]
}

func TestXHTTPShareLink(t *testing.T) {
	xh, tcp := xhttpEntries(t)
	links := ShareLinks([]Entry{xh, tcp})

	u, err := url.Parse(links[0])
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("type") != "xhttp" || q.Get("path") != xh.Path || q.Get("mode") != "auto" ||
		q.Get("security") != "reality" || q.Get("pbk") == "" || q.Has("flow") {
		t.Errorf("xhttp link = %s", links[0])
	}
	if !strings.HasPrefix(xh.Path, "/") {
		t.Errorf("path %q", xh.Path)
	}

	// An Xray inbound on plain TCP is, to a client, any other VLESS + Reality
	// + Vision server.
	u, _ = url.Parse(links[1])
	if q := u.Query(); q.Get("type") != "tcp" || q.Get("flow") != service.FlowVision || q.Has("path") {
		t.Errorf("tcp link = %s", links[1])
	}
}

func TestXHTTPInClash(t *testing.T) {
	xh, tcp := xhttpEntries(t)
	data, err := Clash([]Entry{xh, tcp}, testRules)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Proxies) != 2 {
		t.Fatalf("proxies = %v", cfg.Proxies)
	}
	p := cfg.Proxies[0]
	opts, _ := p["xhttp-opts"].(map[string]any)
	if p["network"] != "xhttp" || opts["path"] != xh.Path || opts["mode"] != "auto" {
		t.Errorf("xhttp proxy = %v", p)
	}
	if _, ok := p["reality-opts"]; !ok || p["flow"] != nil {
		t.Errorf("xhttp proxy reality/flow = %v", p)
	}
	if cfg.Proxies[1]["network"] != "tcp" || cfg.Proxies[1]["xhttp-opts"] != nil {
		t.Errorf("tcp proxy = %v", cfg.Proxies[1])
	}
}

// sing-box has no XHTTP, so the entry is left out of its config rather than
// handed over as a server it cannot reach.
func TestXHTTPIsLeftOutOfSingBox(t *testing.T) {
	xh, tcp := xhttpEntries(t)
	data, err := SingBox([]Entry{xh, tcp}, testRules)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	var vless []string
	for _, ob := range cfg.Outbounds {
		if ob["type"] == "vless" {
			vless = append(vless, ob["tag"].(string))
		}
		if members, ok := ob["outbounds"].([]any); ok {
			for _, m := range members {
				if m == xh.Name {
					t.Errorf("group %v lists the xhttp entry", ob["tag"])
				}
			}
		}
	}
	if len(vless) != 1 || vless[0] != tcp.Name {
		t.Errorf("vless outbounds = %v, want only %s", vless, tcp.Name)
	}
}
