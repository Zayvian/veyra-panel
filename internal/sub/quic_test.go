package sub

import (
	"encoding/json"
	"github.com/kosje/skysbx-panel/internal/service"
	"github.com/kosje/skysbx-panel/internal/store"
	"gopkg.in/yaml.v3"
	"net/url"
	"strings"
	"testing"
)

func TestQUICSubscriptionFormats(t *testing.T) {
	user, nodes, _ := fixture(t)
	user.Password = "a+b:@ /?测试"
	var inbounds []*store.Inbound
	for _, protocol := range []string{store.ProtoHysteria2, store.ProtoTUIC} {
		spec := service.InboundSpec{Tag: protocol, Protocol: protocol, Port: 8443, ServerName: nodes[0].Address}
		if protocol == store.ProtoHysteria2 {
			spec.HopPorts = "20000-30000,40000"
			spec.HopInterval = "15s"
		}
		in, err := service.BuildInbound(spec)
		if err != nil {
			t.Fatal(err)
		}
		in.NodeID = nodes[0].ID
		inbounds = append(inbounds, in)
	}
	entries, err := Build(user, nodes, inbounds, nil)
	if err != nil {
		t.Fatal(err)
	}
	links := ShareLinks(entries)
	if len(links) != 2 {
		t.Fatal("missing QUIC links")
	}
	hy, err := url.Parse(links[0])
	if err != nil {
		t.Fatal(err)
	}
	if hy.User.Username() != user.Password || hy.Query().Get("mport") != "20000-30000,40000" {
		t.Fatal("broken hy2 URI")
	}
	tu, err := url.Parse(links[1])
	if err != nil {
		t.Fatal(err)
	}
	password, _ := tu.User.Password()
	if tu.User.Username() != user.VlessUUID || password != user.Password || tu.Query().Get("mport") != "" {
		t.Fatal("broken TUIC URI")
	}
	raw, err := SingBox(entries)
	if err != nil {
		t.Fatal(err)
	}
	var sb struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal(raw, &sb); err != nil {
		t.Fatal(err)
	}
	var found int
	for _, ob := range sb.Outbounds {
		if ob["type"] == "hysteria2" {
			found++
			if ob["server_port"] != nil || ob["hop_interval"] != "15s" || !strings.Contains(string(raw), "20000:30000") {
				t.Fatal("wrong sing-box hop config")
			}
		}
		if ob["type"] == "tuic" {
			found++
			if ob["uuid"] != user.VlessUUID || ob["password"] != user.Password || ob["server_ports"] != nil {
				t.Fatal("wrong TUIC config")
			}
		}
	}
	if found != 2 {
		t.Fatal("missing sing-box QUIC proxies")
	}
	raw, err = Clash(entries)
	if err != nil {
		t.Fatal(err)
	}
	var clash struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(raw, &clash); err != nil {
		t.Fatal(err)
	}
	if len(clash.Proxies) != 2 || clash.Proxies[0]["ports"] != "20000-30000,40000" || clash.Proxies[0]["hop-interval"] != 15 || clash.Proxies[1]["uuid"] != user.VlessUUID {
		t.Fatalf("invalid Mihomo config %s", raw)
	}
	user.Enabled = false
	entries, err = Build(user, nodes, inbounds, nil)
	if err != nil || len(entries) != 0 {
		t.Fatal("inactive QUIC subscription not empty")
	}
}
