package sub

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/Zayvian/veyra-panel/internal/service"
	"github.com/Zayvian/veyra-panel/internal/store"
	"gopkg.in/yaml.v3"
)

func TestSubscriptionSortAcrossFormats(t *testing.T) {
	u := &store.User{Enabled: true, Password: "test"}
	nodes := []*store.Node{
		{ID: 1, Name: "US", Address: "us.example.com", Enabled: true, SortOrder: 30},
		{ID: 2, Name: "JP", Address: "jp.example.com", Enabled: true, SortOrder: 20},
		{ID: 3, Name: "HK", Address: "hk.example.com", Enabled: true, SortOrder: 10},
	}
	var ins []*store.Inbound
	for _, n := range nodes {
		for _, suffix := range []string{"A", "Z"} {
			in, err := service.BuildInbound(service.InboundSpec{Protocol: store.ProtoAnyTLS, Tag: n.Name + "-" + suffix, Port: 8443, ServerName: n.Address})
			if err != nil {
				t.Fatal(err)
			}
			in.ID, in.NodeID = int64(len(ins)+1), n.ID
			if suffix == "A" {
				in.SortOrder = 20
			} else {
				in.SortOrder = 10
			}
			ins = append(ins, in)
		}
	}
	original := append([]*store.Inbound(nil), ins...)
	entries, err := Build(u, nodes, ins, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"HK-Z", "HK-A", "JP-Z", "JP-A", "US-Z", "US-A"}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
	}
	if !reflect.DeepEqual(names, want) || !reflect.DeepEqual(ins, original) {
		t.Fatalf("wrong order or mutated input: %v", names)
	}
	clash, err := Clash(entries)
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Proxies []struct {
			Name string `yaml:"name"`
		} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(clash, &c); err != nil {
		t.Fatal(err)
	}
	sing, err := SingBox(entries)
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Outbounds []struct {
			Tag  string `json:"tag"`
			Type string `json:"type"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal(sing, &s); err != nil {
		t.Fatal(err)
	}
	var singNames []string
	for _, out := range s.Outbounds {
		if out.Type == "anytls" {
			singNames = append(singNames, out.Tag)
		}
	}
	if !reflect.DeepEqual(singNames, want) {
		t.Fatal("sing-box order differs")
	}
	raw, err := base64.StdEncoding.DecodeString(Base64(entries))
	if err != nil {
		t.Fatal(err)
	}
	links := strings.Split(strings.TrimSpace(string(raw)), "\n")
	for i, name := range want {
		link, err := url.Parse(links[i])
		if err != nil || link.Fragment != name || c.Proxies[i].Name != name {
			t.Fatal("Clash/share link order differs")
		}
	}
	allowed := map[int64]bool{ins[0].ID: true, ins[3].ID: true}
	filtered, err := Build(u, nodes, ins, allowed)
	if err != nil || len(filtered) != 2 || filtered[0].Name != "JP-Z" || filtered[1].Name != "US-A" {
		t.Fatal("sort bypassed access filtering")
	}
	for _, n := range nodes {
		n.SortOrder = 0
	}
	for _, in := range ins {
		in.SortOrder = 0
	}
	entries, err = Build(u, nodes, ins, nil)
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].Name != "US-A" || entries[2].Name != "JP-A" || entries[4].Name != "HK-A" {
		t.Fatal("default order changed")
	}
}
