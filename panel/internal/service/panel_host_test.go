package service

import (
	"slices"
	"testing"

	"github.com/Zayvian/veyra-panel/internal/store"
)

func TestMovePanelHostRewritesWhatNamedTheOldDomain(t *testing.T) {
	svc := newTestService(t)
	spy := &spyNotifier{}
	svc.SetNotifier(spy)

	local, _, _ := svc.CreateNode("local", "Panel.Old.com", "JP")
	other, _, _ := svc.CreateNode("other", "node.old.com", "US")
	anytls, err := svc.CreateInbound(local.ID, InboundSpec{
		Protocol: store.ProtoAnyTLS, Tag: "a", Port: 8443, ServerName: "panel.old.com"})
	if err != nil {
		t.Fatal(err)
	}
	reality, err := svc.CreateInbound(local.ID, InboundSpec{
		Protocol: store.ProtoVLESS, Tag: "r", Port: 443, Handshake: "www.apple.com"})
	if err != nil {
		t.Fatal(err)
	}
	elsewhere, err := svc.CreateInbound(other.ID, InboundSpec{
		Protocol: store.ProtoHysteria2, Tag: "h", Port: 9443, ServerName: "node.old.com"})
	if err != nil {
		t.Fatal(err)
	}
	spy.config = nil

	moved, err := svc.MovePanelHost("panel.old.com", "panel.new.net")
	if err != nil {
		t.Fatal(err)
	}
	if moved.Nodes != 1 || moved.Inbounds != 1 {
		t.Fatalf("moved %+v, want 1 node and 1 inbound", moved)
	}
	if n, _ := svc.Store().Node(local.ID); n.Address != "panel.new.net" {
		t.Fatalf("local node address %q", n.Address)
	}
	if n, _ := svc.Store().Node(other.ID); n.Address != "node.old.com" {
		t.Fatalf("other node moved to %q", n.Address)
	}

	in, _ := svc.Store().Inbound(anytls.ID)
	sb, _ := ParseConfig(in)
	cl, _ := ParseClient(in)
	if sb.TLS.ServerName != "panel.new.net" || cl.SNI != "panel.new.net" {
		t.Fatalf("anytls server name %q, sni %q", sb.TLS.ServerName, cl.SNI)
	}
	for _, id := range []int64{reality.ID, elsewhere.ID} {
		before := map[int64]*store.Inbound{reality.ID: reality, elsewhere.ID: elsewhere}[id]
		after, _ := svc.Store().Inbound(id)
		if after.Config != before.Config || after.Client != before.Client {
			t.Fatalf("inbound %s changed", after.Tag)
		}
	}
	if !slices.Contains(spy.config, local.ID) || slices.Contains(spy.config, other.ID) {
		t.Fatalf("config pushes %v", spy.config)
	}
}
