package service

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Zayvian/veyra-panel/internal/store"
)

func TestRenameInboundPreservesConfigurationAndUsers(t *testing.T) {
	for _, protocol := range []string{store.ProtoVLESS, store.ProtoShadowsocks, store.ProtoAnyTLS, store.ProtoHysteria2, store.ProtoTUIC} {
		t.Run(protocol, func(t *testing.T) {
			svc, nodeID := editFixture(t)
			relay, _, err := svc.CreateNode("relay", "relay.example.com", "HK")
			if err != nil {
				t.Fatal(err)
			}
			in, err := svc.CreateInbound(nodeID, InboundSpec{Protocol: protocol, Tag: "old-name", Port: 8443,
				Handshake: DefaultHandshake, ServerName: "node.example.com", RelayNodeID: relay.ID, RelayPort: 443})
			if err != nil {
				t.Fatal(err)
			}
			u, err := svc.CreateUser(NewUser{Name: "alice"})
			if err != nil {
				t.Fatal(err)
			}
			if err := svc.SetUserInbounds(u.ID, []int64{in.ID}); err != nil {
				t.Fatal(err)
			}
			beforeConfig, err := ParseConfig(in)
			if err != nil {
				t.Fatal(err)
			}
			beforeUsers, err := svc.NodeUsers(nodeID)
			if err != nil {
				t.Fatal(err)
			}
			spy := &spyNotifier{}
			svc.SetNotifier(spy)
			name := "香港下载 01"
			edited, err := svc.EditInbound(in.ID, InboundEdit{Tag: &name, Port: in.Port,
				Handshake: DefaultHandshake, ServerName: "node.example.com", RelayNodeID: relay.ID, RelayPort: 443})
			if err != nil {
				t.Fatal(err)
			}
			if edited.ID != in.ID || edited.Tag != name || edited.Client != in.Client {
				t.Fatal("identity or client settings changed")
			}
			afterConfig, err := ParseConfig(edited)
			if err != nil {
				t.Fatal(err)
			}
			beforeConfig.Tag = name
			if !reflect.DeepEqual(beforeConfig, afterConfig) {
				t.Fatal("rename changed more than the config tag")
			}
			afterUsers, err := svc.NodeUsers(nodeID)
			if err != nil {
				t.Fatal(err)
			}
			if len(afterUsers[name]) != 1 || !reflect.DeepEqual(beforeUsers[in.Tag], afterUsers[name]) || len(afterUsers[in.Tag]) != 0 {
				t.Fatal("users did not follow the new tag")
			}
			if !reflect.DeepEqual(spy.config, []int64{nodeID, relay.ID}) {
				t.Fatalf("missing config notifications: %v", spy.config)
			}
			relayConfig, err := svc.NodeConfig(relay.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(relayConfig.Inbounds) != 1 || relayConfig.Inbounds[0].Tag != RelayTag(name) {
				t.Fatal("relay still has the old name")
			}
			subscription, err := svc.Subscription(u.SubToken)
			if err != nil {
				t.Fatal(err)
			}
			if subscription.User.VlessUUID != u.VlessUUID || subscription.User.Password != u.Password {
				t.Fatal("user credentials changed")
			}
		})
	}
}

func TestRenameInboundRejectsInvalidNamesWithoutChanges(t *testing.T) {
	svc, nodeID := editFixture(t)
	in, err := svc.CreateInbound(nodeID, InboundSpec{Protocol: store.ProtoShadowsocks, Tag: "original", Port: 8388})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateInbound(nodeID, InboundSpec{Protocol: store.ProtoShadowsocks, Tag: "occupied", Port: 8389})
	if err != nil {
		t.Fatal(err)
	}
	spy := &spyNotifier{}
	svc.SetNotifier(spy)
	for _, name := range []string{"", "  ", "bad/name", "bad\nname", RelayTagPrefix + "reserved", strings.Repeat("中", 129), "occupied"} {
		_, err := svc.EditInbound(in.ID, InboundEdit{Tag: &name, Port: in.Port})
		if name == "occupied" {
			if !errors.Is(err, store.ErrConflict) {
				t.Fatalf("expected duplicate rejection, got %v", err)
			}
		} else if !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted invalid name %q: %v", name, err)
		}
		stored, err := svc.Store().Inbound(in.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(stored, in) {
			t.Fatal("failed rename changed stored inbound")
		}
	}
	if len(spy.config) != 0 {
		t.Fatal("failed rename pushed configuration")
	}
}
