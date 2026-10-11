package engine

import (
	"crypto/rand"
	"encoding/base64"

	"github.com/sagernet/sing-box/option"
)

// placeholderUser is the name given to the filler entry below. It never
// authenticates anyone: its key is random, is generated per apply, and is never
// sent anywhere.
const placeholderUser = "__skysbx_placeholder__"

// ensureShadowsocksUsers gives every Shadowsocks inbound at least one user.
//
// sing-box decides at construction time, from len(users), whether to build a
// multi-user Shadowsocks listener or a single-user one. The single-user type has
// no method to change its users at all, so an inbound built empty can never
// accept one afterwards — every later update fails, and the protocol stays
// unusable until something restarts the listener.
//
// The panel sends empty user lists on purpose: users travel in their own message
// so that changing them is a hot swap. Reconciling the two is the node's job,
// not the panel's — which sing-box inbound type gets built is a detail of
// running sing-box, and nothing the panel should have to model.
//
// The single-user shape is also wrong on its own terms. In that mode the
// inbound's shared password is the entire credential: it authenticates, belongs
// to no user, and its traffic is therefore counted against nobody.
func ensureShadowsocksUsers(o *option.Options) error {
	for i := range o.Inbounds {
		opts, ok := o.Inbounds[i].Options.(*option.ShadowsocksInboundOptions)
		if !ok || len(opts.Users) > 0 {
			continue
		}
		key, err := randomKey()
		if err != nil {
			return err
		}
		opts.Users = []option.ShadowsocksUser{{Name: placeholderUser, Password: key}}
	}
	return nil
}

// randomKey returns base64 of 32 bytes, the key size every method the panel
// emits requires.
func randomKey() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b[:]), nil
}
