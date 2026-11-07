package sub

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/Zayvian/veyra-panel/internal/service"
	"github.com/Zayvian/veyra-panel/internal/store"
	"gopkg.in/yaml.v3"
)

// A self-signed inbound is useless unless every format tells the client what
// to trust: sing-box the certificate, mihomo its fingerprint, share links the
// insecure flags (and Hysteria2's pin). A CA-signed inbound must get none of
// it, or a typo'd domain would quietly stop being verified.
func TestSelfSignedSubscriptionFormats(t *testing.T) {
	user, nodes, base := fixture(t)
	var inbounds []*store.Inbound
	for i, proto := range []string{store.ProtoHysteria2, store.ProtoTUIC, store.ProtoAnyTLS} {
		in, err := service.BuildInbound(service.InboundSpec{Tag: "self-" + proto, Protocol: proto,
			Port: 9443 + i, ServerName: "www.bing.com", SelfSigned: true})
		if err != nil {
			t.Fatal(err)
		}
		in.ID = int64(100 + i)
		in.NodeID = nodes[0].ID
		inbounds = append(inbounds, in)
	}
	inbounds = append(inbounds, base...) // includes a CA-signed AnyTLS

	entries, err := Build(user, nodes, inbounds, nil)
	if err != nil {
		t.Fatal(err)
	}
	pins := map[string]string{}
	for _, e := range entries {
		pins[e.Name] = e.CertSHA256
	}
	if pins["self-hysteria2"] == "" || pins["anytls-tokyo"] != "" {
		t.Fatalf("fingerprints not carried as expected: %v", pins)
	}

	for _, link := range ShareLinks(entries) {
		u, err := url.Parse(link)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		name, _ := url.PathUnescape(u.Fragment)
		self := strings.HasPrefix(name, "self-")
		if self != (q.Get("insecure") == "1") {
			t.Errorf("%s: insecure=%q", name, q.Get("insecure"))
		}
		if name == "self-hysteria2" && q.Get("pinSHA256") != pins[name] {
			t.Errorf("hysteria2 link does not pin the certificate: %s", link)
		}
		if name == "self-tuic" && q.Get("allow_insecure") != "1" {
			t.Errorf("tuic link lacks allow_insecure: %s", link)
		}
	}

	raw, err := Clash(entries, testRules)
	if err != nil {
		t.Fatal(err)
	}
	var clash struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal(raw, &clash); err != nil {
		t.Fatal(err)
	}
	for _, p := range clash.Proxies {
		name := p["name"].(string)
		if strings.HasPrefix(name, "self-") {
			if p["fingerprint"] != pins[name] || p["skip-cert-verify"] != true {
				t.Errorf("mihomo %s does not pin: %v", name, p)
			}
		} else if p["fingerprint"] != nil || p["skip-cert-verify"] != nil {
			t.Errorf("mihomo %s skips verification without a self-signed certificate", name)
		}
	}

	raw, err = SingBox(entries, testRules)
	if err != nil {
		t.Fatal(err)
	}
	var sb struct {
		Outbounds []struct {
			Tag string `json:"tag"`
			TLS *struct {
				Certificate []string `json:"certificate"`
				Insecure    bool     `json:"insecure"`
			} `json:"tls"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal(raw, &sb); err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, ob := range sb.Outbounds {
		if ob.TLS == nil {
			continue
		}
		if ob.TLS.Insecure {
			t.Errorf("sing-box %s turns verification off", ob.Tag)
		}
		hasCert := len(ob.TLS.Certificate) > 0
		if hasCert != strings.HasPrefix(ob.Tag, "self-") {
			t.Errorf("sing-box %s: certificate present = %v", ob.Tag, hasCert)
		}
		if hasCert {
			seen++
			if !strings.HasPrefix(ob.TLS.Certificate[0], "-----BEGIN CERTIFICATE") {
				t.Errorf("sing-box %s certificate is not PEM lines", ob.Tag)
			}
		}
	}
	if seen != 3 {
		t.Errorf("%d sing-box outbounds carry a certificate, want 3", seen)
	}
}
