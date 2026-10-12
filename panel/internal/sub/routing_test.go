package sub

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const testRules = "https://sub.example.com"

func TestClashRouting(t *testing.T) {
	raw, err := Clash([]Entry{node("东京 01"), node("大阪 01")}, testRules)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		ProxyGroups []struct {
			Name, Type string
			Proxies    []string
		} `yaml:"proxy-groups"`
		RuleProviders map[string]struct {
			Behavior, Format, URL string
		} `yaml:"rule-providers"`
		Rules []string
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}

	var names []string
	first := map[string]string{}
	for _, g := range cfg.ProxyGroups {
		names = append(names, g.Name)
		first[g.Name] = g.Proxies[0]
		if g.Type != "select" {
			t.Errorf("%s is %s: nothing is chosen automatically", g.Name, g.Type)
		}
	}
	want := []string{groupProxy, groupAI, groupYouTube, groupNetflix, groupGoogle, groupApple, groupMS, groupFinal}
	if !slices.Equal(names, want) {
		t.Fatalf("groups = %q, want %q", names, want)
	}
	if first[groupProxy] != "东京 01" || first[groupAI] != groupProxy ||
		first[groupApple] != "DIRECT" || first[groupFinal] != groupProxy {
		t.Fatalf("defaults = %v", first)
	}

	// Every list a rule names is defined, and served by the panel.
	for _, r := range cfg.Rules {
		parts := strings.Split(r, ",")
		if parts[0] != "RULE-SET" {
			continue
		}
		p, ok := cfg.RuleProviders[parts[1]]
		if !ok {
			t.Fatalf("rule %q names an undefined list", r)
		}
		if p.Format != "mrs" || p.URL != testRules+RulesPath+parts[1]+".mrs" {
			t.Errorf("list %s = %+v", parts[1], p)
		}
		if _, ok := ruleFile(parts[1] + ".mrs"); !ok {
			t.Errorf("the panel does not serve %s", parts[1])
		}
	}
	if !slices.Contains(cfg.Rules, "RULE-SET,geosite-cn,DIRECT") ||
		!slices.Contains(cfg.Rules, "RULE-SET,geoip-cn,DIRECT") ||
		cfg.Rules[len(cfg.Rules)-1] != "MATCH,"+groupFinal {
		t.Fatalf("rules = %q", cfg.Rules)
	}
	// GitHub is in Microsoft's list, which goes direct by default.
	if slices.Index(cfg.Rules, "RULE-SET,geosite-github,"+groupProxy) > slices.Index(cfg.Rules, "RULE-SET,geosite-microsoft,"+groupMS) {
		t.Error("GitHub is matched after Microsoft")
	}
}

func TestSingBoxRouting(t *testing.T) {
	raw, err := SingBox([]Entry{node("东京 01")}, testRules)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Outbounds []struct {
			Type, Tag, Default string
			Outbounds          []string
		}
		Route struct {
			Rules []struct {
				Action, Outbound string
				RuleSet          []string `json:"rule_set"`
			}
			RuleSet []struct {
				Tag, URL       string
				DownloadDetour string `json:"download_detour"`
			} `json:"rule_set"`
			Final string
		}
		Experimental struct {
			CacheFile struct{ Enabled bool } `json:"cache_file"`
		}
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	tags := map[string]bool{}
	defaults := map[string]string{}
	for _, ob := range cfg.Outbounds {
		tags[ob.Tag] = true
		if ob.Type == "urltest" {
			t.Errorf("urltest %s is still emitted", ob.Tag)
		}
		if ob.Type == "selector" {
			defaults[ob.Tag] = ob.Default
			for _, m := range ob.Outbounds {
				if m == "DIRECT" {
					t.Errorf("%s names Clash's DIRECT", ob.Tag)
				}
			}
		}
	}
	if defaults[groupProxy] != "东京 01" || defaults[groupNetflix] != groupProxy || defaults[groupMS] != tagDirect {
		t.Fatalf("defaults = %v", defaults)
	}
	sets := map[string]bool{}
	for _, s := range cfg.Route.RuleSet {
		sets[s.Tag] = true
		if s.URL != testRules+RulesPath+s.Tag+".srs" || s.DownloadDetour != tagDirect {
			t.Errorf("rule set %+v", s)
		}
		if _, ok := ruleFile(s.Tag + ".srs"); !ok {
			t.Errorf("the panel does not serve %s", s.Tag)
		}
	}
	for _, r := range cfg.Route.Rules {
		for _, s := range r.RuleSet {
			if !sets[s] {
				t.Errorf("rule names undefined set %s", s)
			}
		}
		if r.Outbound != "" && !tags[r.Outbound] {
			t.Errorf("rule targets undefined outbound %s", r.Outbound)
		}
	}
	if cfg.Route.Final != groupFinal || !cfg.Experimental.CacheFile.Enabled {
		t.Fatalf("final = %q, cache = %v", cfg.Route.Final, cfg.Experimental.CacheFile.Enabled)
	}
}
