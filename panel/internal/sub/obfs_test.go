package sub

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Zayvian/veyra-panel/internal/store"
)

// A client that does not get the obfs password cannot connect at all, so it
// has to be in every format the panel serves.
func TestHysteria2ObfsInEveryFormat(t *testing.T) {
	e := Entry{Name: "hy2", Label: "hy2", Protocol: store.ProtoHysteria2,
		Address: "jp.example.com", Port: 8443, SNI: "jp.example.com",
		Password: "user-pass", Obfs: "obfs-pass/+="}

	link := ShareLinks([]Entry{e})[0]
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	if q := u.Query(); q.Get("obfs") != "salamander" || q.Get("obfs-password") != "obfs-pass/+=" {
		t.Fatalf("share link lacks obfs: %s", link)
	}

	raw, err := Clash([]Entry{e}, testRules)
	if err != nil {
		t.Fatal(err)
	}
	var clash struct {
		Proxies []map[string]any
	}
	if err := yaml.Unmarshal(raw, &clash); err != nil {
		t.Fatal(err)
	}
	if p := clash.Proxies[0]; p["obfs"] != "salamander" || p["obfs-password"] != "obfs-pass/+=" {
		t.Fatalf("clash proxy lacks obfs: %+v", p)
	}

	raw, err = SingBox([]Entry{e}, testRules)
	if err != nil {
		t.Fatal(err)
	}
	var sb struct {
		Outbounds []struct {
			Tag  string
			Obfs *struct{ Type, Password string }
		}
	}
	if err := json.Unmarshal(raw, &sb); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ob := range sb.Outbounds {
		if ob.Tag == "hy2" {
			found = ob.Obfs != nil && ob.Obfs.Type == "salamander" && ob.Obfs.Password == "obfs-pass/+="
		}
	}
	if !found {
		t.Fatalf("sing-box outbound lacks obfs: %s", raw)
	}

	// Without obfs nothing about it appears.
	e.Obfs = ""
	if link := ShareLinks([]Entry{e})[0]; strings.Contains(link, "obfs") {
		t.Fatalf("obfs in a link for an inbound without it: %s", link)
	}
}
