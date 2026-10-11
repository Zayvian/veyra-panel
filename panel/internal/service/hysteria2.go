package service

import (
	"math/rand/v2"
	"net/url"
	"strings"

	"github.com/Zayvian/veyra-panel/internal/singbox"
)

// DefaultMasquerade is the form's placeholder: a large site, so a probe sees a
// plausible response from a plausible host.
const DefaultMasquerade = "https://www.bing.com"

// MasqueradeCandidates are offered in the form, and one is picked at random
// for each new inbound.
//
// Large, stable, ordinary HTTPS sites the node can reach from anywhere: what a
// prober sees has to look like nothing in particular. Spreading inbounds across
// several means they do not all answer with the same page, which would be a
// pattern of its own. Left out on purpose: sites blocked in China (they read
// as a statement), and sites that put visitors through a bot check (a
// challenge page is not what a real web server answers with).
var MasqueradeCandidates = []string{
	"https://www.bing.com",
	"https://www.microsoft.com",
	"https://www.apple.com",
	"https://www.amazon.com",
	"https://www.mozilla.org",
	"https://www.nvidia.com",
	"https://www.intel.com",
	"https://www.samsung.com",
	"https://www.adobe.com",
	"https://www.oracle.com",
	"https://www.ibm.com",
	"https://www.dell.com",
	"https://www.hp.com",
	"https://www.lenovo.com",
	"https://www.sony.com",
	"https://www.logitech.com",
}

// RandomMasquerade picks one of MasqueradeCandidates.
func RandomMasquerade() string {
	return MasqueradeCandidates[rand.IntN(len(MasqueradeCandidates))]
}

// applyHysteria2 sets the two Hysteria2-only options on an inbound and its
// client parameters.
//
// Obfs (salamander) disguises every packet so the flow no longer looks like
// QUIC, for networks that throttle or block QUIC on sight. The password is
// generated, kept as long as obfs stays on — changing it would cut off every
// client until it refreshed — and goes in the subscription, because clients
// need it. Turning obfs on or off on an inbound in use has the same effect, so
// the form says to add a new inbound instead.
//
// Masquerade is what the port answers to anything that is not a client: the
// node proxies such requests to a real site, so a browser or a prober sees
// that site rather than an error naming a proxy. Server-side only; changing it
// never affects clients.
func applyHysteria2(in *singbox.Inbound, client *ClientParams, obfs bool, masquerade string) error {
	switch {
	case !obfs:
		in.Obfs = nil
	case in.Obfs == nil || in.Obfs.Password == "":
		in.Obfs = &singbox.Obfs{Type: "salamander", Password: NewPassword()}
	}
	client.Obfs = ""
	if in.Obfs != nil {
		client.Obfs = in.Obfs.Password
	}

	masquerade = strings.TrimSpace(masquerade)
	if masquerade == "" {
		in.Masquerade = nil
		return nil
	}
	u, err := url.Parse(masquerade)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return invalid("masquerade must be a website address like %s", DefaultMasquerade)
	}
	// rewrite_host, so the site receives its own name in the Host header
	// rather than the node's and answers as it would to anyone.
	in.Masquerade = &singbox.Masquerade{Type: "proxy", URL: masquerade, RewriteHost: true}
	return nil
}
