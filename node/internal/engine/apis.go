package engine

import (
	"fmt"
	"net"

	"github.com/sagernet/sing-box/option"
)

// localAPIs are the loopback listeners sing-box needs so the node can read its
// own counters and connection table.
//
// The node picks them, not the panel. They are an implementation detail of how
// this process inspects itself — nothing outside the host can reach them, and
// nothing about them belongs in a configuration the panel stores. Letting the
// panel name them also makes two nodes on one host collide, which is exactly
// what happens while migrating from an older one.
type localAPIs struct {
	clash string
	v2ray string
}

func newLocalAPIs() (localAPIs, error) {
	clash, err := freeLoopbackAddr()
	if err != nil {
		return localAPIs{}, fmt.Errorf("reserve clash api port: %w", err)
	}
	v2ray, err := freeLoopbackAddr()
	if err != nil {
		return localAPIs{}, fmt.Errorf("reserve v2ray api port: %w", err)
	}
	return localAPIs{clash: clash, v2ray: v2ray}, nil
}

// freeLoopbackAddr asks the kernel for an unused port and gives it straight
// back. There is a window between closing this listener and sing-box binding
// the same port, but it is loopback and the alternative — a fixed port — fails
// far more often, and does so at the worst moment.
func freeLoopbackAddr() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer l.Close()
	return l.Addr().String(), nil
}

// override replaces whatever the panel put in the experimental block.
//
// The statistics service must stay enabled with an empty user list: it is an
// allowlist built when the configuration loads, and the authoritative list
// arrives separately. Pre-seeding it here from a config that may be seconds out
// of date would bill traffic to the wrong set of names.
func (a localAPIs) override(o *option.Options) {
	if o.Experimental == nil {
		o.Experimental = &option.ExperimentalOptions{}
	}
	if o.Experimental.ClashAPI == nil {
		o.Experimental.ClashAPI = &option.ClashAPIOptions{}
	}
	o.Experimental.ClashAPI.ExternalController = a.clash

	if o.Experimental.V2RayAPI == nil {
		o.Experimental.V2RayAPI = &option.V2RayAPIOptions{}
	}
	o.Experimental.V2RayAPI.Listen = a.v2ray
	if o.Experimental.V2RayAPI.Stats == nil {
		o.Experimental.V2RayAPI.Stats = &option.V2RayStatsServiceOptions{}
	}
	o.Experimental.V2RayAPI.Stats.Enabled = true
	o.Experimental.V2RayAPI.Stats.Users = nil
}
