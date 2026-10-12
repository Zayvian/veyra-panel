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

// A line named like a group would collide with it and fail the whole file.
func TestAnnouncementSkipsGroupNames(t *testing.T) {
	got := Announcement([]string{groupAI, "联系 abc"}, nil)
	if len(got) != 1 || got[0].Name != "联系 abc" {
		t.Fatalf("got %+v", got)
	}
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

// Listed for the user to read, in a group of its own that no traffic uses.
func TestInfoEntriesStayOutOfAutomaticSelection(t *testing.T) {
	entries := append(Announcement([]string{"📢 公告"}, nil), node("东京 01"), node("大阪 01"))

	raw, err := Clash(entries, testRules)
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
		case groupNotice:
			if !slices.Equal(g.Proxies, []string{"📢 公告"}) {
				t.Fatalf("%s = %q, want the announcement alone", g.Name, g.Proxies)
			}
		default:
			if slices.Contains(g.Proxies, "📢 公告") {
				t.Fatalf("%s = %q: traffic could be sent to the announcement", g.Name, g.Proxies)
			}
		}
	}
	if clash.ProxyGroups[0].Name != groupNotice {
		t.Fatalf("the announcement group is not first: %s", clash.ProxyGroups[0].Name)
	}

	raw, err = SingBox(entries, testRules)
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
		if ob.Type == "selector" && ob.Tag != groupNotice && slices.Contains(ob.Outbounds, "📢 公告") {
			t.Fatalf("sing-box %s includes the announcement: %q", ob.Tag, ob.Outbounds)
		}
	}

	// Share links keep the order, announcement first.
	if links := ShareLinks(entries); len(links) != 3 {
		t.Fatalf("links = %d", len(links))
	}
}

// An inactive account's list is info entries only. The groups then hold them
// rather than going empty, which would fail the whole file.
func TestOnlyInfoEntriesStillMakeAValidConfig(t *testing.T) {
	entries := append(Notice(&store.User{Enabled: false}, nowFunc()), Announcement([]string{"联系 abc"}, nil)...)
	raw, err := Clash(entries, testRules)
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
