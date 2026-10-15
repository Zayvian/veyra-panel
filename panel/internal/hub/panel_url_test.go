package hub

import (
	"encoding/json"
	"testing"
	"time"
)

// A node that dialled a name other than the panel's current domain is told the
// new one, after its config and users — and only if it said it understands.
func TestNodeOnOldDomainIsToldTheNewOne(t *testing.T) {
	h := newHarness(t)
	h.addInbounds(t)
	h.hub.SetPanelHost("New.Example.NET")

	n := dialNode(t, h.srv.URL, h.token)
	n.send(TypeHello, 0, helloV2(CapStatsAck, CapConfigRev, CapPanelURL))
	n.await(TypeUsers, 3*time.Second)
	env := n.await(TypePanelURL, 3*time.Second)
	var d PanelURLData
	if err := json.Unmarshal(env.Data, &d); err != nil {
		t.Fatal(err)
	}
	if d.URL != "https://new.example.net" {
		t.Fatalf("panel_url = %q", d.URL)
	}
	if env.ID == 0 {
		t.Fatal("panel_url carries no id for the node to answer")
	}
	if got := h.hub.DialedHost(h.node.ID); got != "127.0.0.1" {
		t.Fatalf("dialled host = %q", got)
	}
}

func TestPanelURLNeedsTheCapability(t *testing.T) {
	h := newHarness(t)
	h.addInbounds(t)
	h.hub.SetPanelHost("new.example.net")

	n := dialNode(t, h.srv.URL, h.token)
	n.send(TypeHello, 0, helloV2(CapStatsAck, CapConfigRev))
	n.await(TypeUsers, 3*time.Second)
	n.expectNothing(TypePanelURL, 300*time.Millisecond)
}

func TestNodeOnCurrentDomainIsLeftAlone(t *testing.T) {
	h := newHarness(t)
	h.addInbounds(t)
	h.hub.SetPanelHost("127.0.0.1")

	n := dialNode(t, h.srv.URL, h.token)
	n.send(TypeHello, 0, helloV2(CapStatsAck, CapConfigRev, CapPanelURL))
	n.await(TypeUsers, 3*time.Second)
	n.expectNothing(TypePanelURL, 300*time.Millisecond)
}
