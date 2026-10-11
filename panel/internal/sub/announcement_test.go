package sub

import (
	"encoding/json"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Zayvian/veyra-panel/internal/store"
)

func node(name string) Entry {
	return Entry{Name: name, Label: name, Protocol: store.ProtoShadowsocks,
		Address: "jp.example.com", Port: 8388, Method: "2022-blake3-aes-256-gcm",
		Password: "a:b"}
}

func TestAnnouncementEntries(t *testing.T) {
	nodes := []Entry{node("东京 01")}
	got := Announcement([]string{"📢 公告", "", "东京 01", "联系 abc"}, nodes)
	var names []string
	for _, e := range got {
		names = append(names, e.Name)
		if !e.Info || e.Address != "127.0.0.1" || e.Port != 1 {
			t.Fatalf("announcement entry is not an info entry on the closed port: %+v", e)
		}
	}
	// The line that collides with a node's name is dropped, not duplicated.
	if !slices.Equal(names, []string{"📢 公告", "联系 abc"}) {
		t.Fatalf("names = %q", names)
	}
}

// Listed for the user to read, but never measured or picked automatically.
func TestInfoEntriesStayOutOfAutomaticSelection(t *testing.T) {
	entries := append(Announcement([]string{"📢 公告"}, nil), node("东京 01"), node("大阪 01"))

	raw, err := Clash(entries)
	if err != nil {
		t.Fatal(err)
	}
	var clash struct {
		Proxies     []struct{ Name string }
		ProxyGroups []struct {
			Name    string
			Proxies []string
		} `yaml:"proxy-groups"`
	}
	if err := yaml.Unmarshal(raw, &clash); err != nil {
		t.Fatal(err)
	}
	if clash.Proxies[0].Name != "📢 公告" {
		t.Fatalf("the announcement is not first: %+v", clash.Proxies)
	}
	for _, g := range clash.ProxyGroups {
		switch g.Name {
		case "PROXY":
			if g.Proxies[0] != "AUTO" || !slices.Contains(g.Proxies, "📢 公告") {
				t.Fatalf("PROXY = %q: AUTO must stay the default, the announcement listed", g.Proxies)
			}
		case "AUTO":
			if slices.Contains(g.Proxies, "📢 公告") || len(g.Proxies) != 2 {
				t.Fatalf("AUTO = %q, want only the two nodes", g.Proxies)
			}
		}
	}

	raw, err = SingBox(entries)
	if err != nil {
		t.Fatal(err)
	}
	var sb struct {
		Outbounds []struct {
			Type, Tag, Default string
			Outbounds          []string
		}
	}
	if err := json.Unmarshal(raw, &sb); err != nil {
		t.Fatal(err)
	}
	for _, ob := range sb.Outbounds {
		switch ob.Type {
		case "urltest":
			if slices.Contains(ob.Outbounds, "📢 公告") {
				t.Fatalf("sing-box urltest includes the announcement: %q", ob.Outbounds)
			}
		case "selector":
			if ob.Default == "📢 公告" || !slices.Contains(ob.Outbounds, "📢 公告") {
				t.Fatalf("sing-box selector: default %q, members %q", ob.Default, ob.Outbounds)
			}
		}
	}

	// Share links keep the order, announcement first.
	if links := ShareLinks(entries); len(links) != 3 {
		t.Fatalf("links = %d", len(links))
	}
}

// An inactive account's list is info entries only. Automatic selection then
// keeps them rather than going empty, which would fail the whole file.
func TestOnlyInfoEntriesStillMakeAValidConfig(t *testing.T) {
	entries := append(Notice(&store.User{Enabled: false}, nowFunc()), Announcement([]string{"联系 abc"}, nil)...)
	raw, err := Clash(entries)
	if err != nil {
		t.Fatal(err)
	}
	var clash struct {
		ProxyGroups []struct {
			Name    string
			Proxies []string
		} `yaml:"proxy-groups"`
	}
	if err := yaml.Unmarshal(raw, &clash); err != nil {
		t.Fatal(err)
	}
	for _, g := range clash.ProxyGroups {
		if len(g.Proxies) == 0 {
			t.Fatalf("group %s is empty", g.Name)
		}
	}
}
