package service

import (
	"net/url"
	"strings"

	"github.com/Zayvian/veyra-panel/internal/singbox"
)

// DefaultMasquerade is offered in the form: a large site that answers HTTP/3,
// so a probe sees a plausible response from a plausible host.
const DefaultMasquerade = "https://www.bing.com"

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
