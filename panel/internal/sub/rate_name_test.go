package sub

import (
	"net/url"
	"strings"
	"testing"
)

// A user choosing a server should see what it costs against their quota, in
// every format, without the operator having to keep the name and the rate in
// step by hand.
func TestNodeNameCarriesRate(t *testing.T) {
	for _, tc := range []struct {
		rate int64
		want string
	}{
		{500, "日本 Oracle 01 [0.5×]"},
		{1000, "日本 Oracle 01 [1×]"},
		{2000, "日本 Oracle 01 [2×]"},
		{1250, "日本 Oracle 01 [1.25×]"},
		{0, "日本 Oracle 01 [0×]"},
	} {
		u, nodes, inbounds := fixture(t)
		nodes[0].RateMilli = tc.rate
		inbounds = inbounds[2:] // shadowsocks
		inbounds[0].Tag = "日本 Oracle 01"

		entries, err := Build(u, nodes, inbounds, nil)
		if err != nil || len(entries) != 1 {
			t.Fatalf("rate %d: build: %v, %d entries", tc.rate, err, len(entries))
		}
		if entries[0].Name != tc.want || entries[0].display() != tc.want {
			t.Errorf("rate %d: name %q, want %q", tc.rate, entries[0].Name, tc.want)
		}
		link, err := url.Parse(ShareLinks(entries)[0])
		if err != nil || link.Fragment != tc.want {
			t.Errorf("rate %d: share link names it %q", tc.rate, link.Fragment)
		}
		clash, err := Clash(entries, testRules)
		if err != nil || !strings.Contains(string(clash), tc.want) {
			t.Errorf("rate %d: clash config does not name %q", tc.rate, tc.want)
		}
	}
}

// An operator who already typed the rate into the name gets it once, not twice.
func TestNodeNameRateNotDoubled(t *testing.T) {
	if got := withRate("日本 Oracle 01 [0.5×]", 500); got != "日本 Oracle 01 [0.5×]" {
		t.Errorf("got %q", got)
	}
	// A stale rate in the name is the operator's text; the real one still shows.
	if got := withRate("日本 Oracle 01 [1×]", 500); got != "日本 Oracle 01 [1×] [0.5×]" {
		t.Errorf("got %q", got)
	}
}
