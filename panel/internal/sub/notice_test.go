package sub

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Zayvian/veyra-panel/internal/store"
)

func TestNoticeNamesTheReason(t *testing.T) {
	now := time.Date(2026, time.October, 11, 12, 0, 0, 0, time.Local)
	past := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.Local)
	future := now.Add(30 * 24 * time.Hour)

	for _, tc := range []struct {
		name string
		user store.User
		want string // substring; empty means no notice at all
	}{
		{"active", store.User{Enabled: true, ExpiresAt: &future, TrafficLimit: 100, TrafficUsed: 10}, ""},
		{"unlimited", store.User{Enabled: true}, ""},
		{"disabled", store.User{Enabled: false}, "账号已停用"},
		{"expired", store.User{Enabled: true, ExpiresAt: &past}, "已于 2026-10-01 00:00 到期"},
		{"expires this minute", store.User{Enabled: true, ExpiresAt: &now}, "到期"},
		{"over quota", store.User{Enabled: true, TrafficLimit: 100, TrafficUsed: 100}, "流量已用完，请联系续费"},
		{"over quota, monthly", store.User{Enabled: true, TrafficLimit: 100, TrafficUsed: 150, ResetDay: 15},
			"本期流量已用完，10-15 00:00 自动恢复"},
		// The reason given is the first one Active would fail on.
		{"disabled and expired", store.User{Enabled: false, ExpiresAt: &past}, "账号已停用"},
		{"expired and over quota", store.User{Enabled: true, ExpiresAt: &past, TrafficLimit: 1, TrafficUsed: 5}, "到期"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Notice(&tc.user, now)
			if tc.want == "" {
				if got != nil {
					t.Fatalf("active user got a notice: %+v", got)
				}
				if !tc.user.Active(now) {
					t.Fatal("fixture: user should be active")
				}
				return
			}
			if tc.user.Active(now) {
				t.Fatal("fixture: user should be inactive")
			}
			if len(got) != 1 || !strings.Contains(got[0].Label, tc.want) {
				t.Fatalf("notice = %+v, want one entry containing %q", got, tc.want)
			}
		})
	}
}

// The notice has to survive every format a client might ask for — an entry that
// one client refuses to import puts that user back to an empty-looking list.
func TestNoticeRendersInEveryFormat(t *testing.T) {
	u := &store.User{Enabled: false}
	entries := Notice(u, time.Now())
	const want = "账号已停用，请联系管理员"

	links := ShareLinks(entries)
	if len(links) != 1 || !strings.HasPrefix(links[0], "ss://") {
		t.Fatalf("share links: %v", links)
	}
	if !strings.Contains(links[0], "@127.0.0.1:1#") {
		t.Fatalf("notice does not point at the closed loopback port: %s", links[0])
	}

	raw, err := Clash(entries, testRules)
	if err != nil {
		t.Fatal(err)
	}
	var clash struct {
		Proxies []struct {
			Name, Server string
			Port         int
		}
		ProxyGroups []struct{ Proxies []string } `yaml:"proxy-groups"`
	}
	if err := yaml.Unmarshal(raw, &clash); err != nil {
		t.Fatalf("clash yaml: %v", err)
	}
	if len(clash.Proxies) != 1 || clash.Proxies[0].Name != want || clash.Proxies[0].Server != "127.0.0.1" {
		t.Fatalf("clash proxies: %+v", clash.Proxies)
	}
	// A group with members, so the client lists the notice rather than an
	// empty selector.
	if len(clash.ProxyGroups) == 0 {
		t.Fatal("clash has no proxy group to show the notice in")
	}

	raw, err = SingBox(entries, testRules)
	if err != nil {
		t.Fatal(err)
	}
	var sb struct {
		Outbounds []struct {
			Type, Tag string
			Outbounds []string
		}
	}
	if err := json.Unmarshal(raw, &sb); err != nil {
		t.Fatalf("sing-box json: %v", err)
	}
	found := false
	for _, ob := range sb.Outbounds {
		if ob.Tag == want && ob.Type == "shadowsocks" {
			found = true
		}
	}
	if !found {
		t.Fatalf("sing-box outbounds lack the notice: %+v", sb.Outbounds)
	}

	if s := ShadowrocketBase64(entries, u); s == "" {
		t.Fatal("shadowrocket output empty")
	}
}
