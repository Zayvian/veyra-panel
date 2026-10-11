package service

import (
	"github.com/Zayvian/veyra-panel/internal/store"
	"testing"
)

func TestHoppingValidation(t *testing.T) {
	for _, value := range []string{"0", "65536", "30000-20000", "20000-", "1,", "1;flush ruleset", "1-10,5-15"} {
		if _, _, err := ValidateHopping(store.ProtoHysteria2, value, "30s", "", 0); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
	ports, interval, err := ValidateHopping(store.ProtoHysteria2, "20000:30000, 40000", "", "", 0)
	if err != nil || ports != "20000-30000,40000" || interval != "30s" {
		t.Fatalf("normalize: %s %s %v", ports, interval, err)
	}
	for _, v := range []string{"4s", "1.5s", "-1s", "bogus", "25h"} {
		if _, _, err := ValidateHopping(store.ProtoHysteria2, "20000-30000", v, "", 0); err == nil {
			t.Errorf("accepted interval %q", v)
		}
	}
	if _, _, err := ValidateHopping(store.ProtoTUIC, "20000-30000", "30s", "", 0); err == nil {
		t.Fatal("TUIC hopping accepted")
	}
	if _, _, err := ValidateHopping(store.ProtoHysteria2, "20000-30000", "30s", "relay.example.com", 0); err == nil {
		t.Fatal("relayed hopping accepted")
	}
}

func TestQUICUsersAndHoppingEdits(t *testing.T) {
	svc := newTestService(t)
	node, _, err := svc.CreateNode("tokyo", "jp.example.com", "jp")
	if err != nil {
		t.Fatal(err)
	}
	user, err := svc.CreateUser(NewUser{Name: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	hy, err := svc.CreateInbound(node.ID, InboundSpec{Protocol: store.ProtoHysteria2, Tag: "hy", Port: 8443, ServerName: node.Address, HopPorts: "20000-30000"})
	if err != nil {
		t.Fatal(err)
	}
	tuic, err := svc.CreateInbound(node.ID, InboundSpec{Protocol: store.ProtoTUIC, Tag: "tuic", Port: 9443, ServerName: node.Address})
	if err != nil {
		t.Fatal(err)
	}
	users, err := svc.NodeUsers(node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if users[hy.Tag][0].Password != user.Password || users[tuic.Tag][0].UUID != user.VlessUUID || users[tuic.Tag][0].Password != user.Password {
		t.Fatal("wrong QUIC credentials")
	}
	if _, err := svc.CreateInbound(node.ID, InboundSpec{Protocol: store.ProtoTUIC, Tag: "collision", Port: 25000, ServerName: node.Address}); err == nil {
		t.Fatal("hop overlap accepted")
	}
	if _, err := svc.EditInbound(hy.ID, InboundEdit{Port: 8443, ServerName: node.Address, HopPorts: "9400-9500"}); err == nil {
		t.Fatal("hop range captures TUIC")
	}
	edited, err := svc.EditInbound(hy.ID, InboundEdit{Port: 8443, ServerName: node.Address})
	if err != nil {
		t.Fatal(err)
	}
	client, _ := ParseClient(edited)
	config, _ := ParseConfig(edited)
	if client.HopPorts != "" || config.HopPorts != "" {
		t.Fatal("hopping was not disabled")
	}
	user.Enabled = false
	if err := svc.UpdateUser(user); err != nil {
		t.Fatal(err)
	}
	users, err = svc.NodeUsers(node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(users[hy.Tag]) != 0 || len(users[tuic.Tag]) != 0 {
		t.Fatal("disabled user still sent to QUIC inbounds")
	}
}
