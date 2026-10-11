package service

import (
	"reflect"
	"testing"

	"github.com/Zayvian/veyra-panel/internal/store"
)

func TestSortEditsPreserveNodeConfigAndCredentials(t *testing.T) {
	svc, nodeID := editFixture(t)
	in, err := svc.CreateInbound(nodeID, InboundSpec{Protocol: store.ProtoShadowsocks, Port: 8388})
	if err != nil {
		t.Fatal(err)
	}
	node, err := svc.Node(nodeID)
	if err != nil {
		t.Fatal(err)
	}
	token, name := node.TokenSHA, node.Name
	spy := &spyNotifier{}
	svc.SetNotifier(spy)
	node.SortOrder = 20
	if err := svc.UpdateNode(node); err != nil {
		t.Fatal(err)
	}
	order := 10
	edited, err := svc.EditInbound(in.ID, InboundEdit{Port: in.Port, SortOrder: &order})
	if err != nil {
		t.Fatal(err)
	}
	if edited.SortOrder != order || edited.Config != in.Config || edited.Client != in.Client || edited.Tag != in.Tag {
		t.Fatal("sort edit changed connection settings")
	}
	storedNode, err := svc.Node(nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if storedNode.SortOrder != 20 || storedNode.TokenSHA != token || storedNode.Name != name {
		t.Fatal("sort was not saved or node changed")
	}
	if len(spy.config) != 0 || spy.users != 0 {
		t.Fatal("sort-only edit notified nodes")
	}
	for _, invalid := range []int{-1, 1000000} {
		if _, err := svc.EditInbound(in.ID, InboundEdit{Port: in.Port, SortOrder: &invalid}); err == nil {
			t.Fatal("invalid order accepted")
		}
		stored, err := svc.Store().Inbound(in.ID)
		if err != nil || !reflect.DeepEqual(stored, edited) {
			t.Fatal("failed edit changed inbound")
		}
	}
}
