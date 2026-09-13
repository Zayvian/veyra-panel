package service

import (
	"testing"

	"github.com/Zayvian/veyra-panel/internal/store"
)

func TestForceSubscriptionRefreshRotatesCredentialsKeepsSubscriptionURL(t *testing.T) {
	svc := newTestService(t)
	spy := &spyNotifier{}
	svc.SetNotifier(spy)

	first, err := svc.CreateUser(NewUser{Name: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateUser(NewUser{Name: "bob"})
	if err != nil {
		t.Fatal(err)
	}
	firstBefore := credentialSnapshot(first)
	secondBefore := credentialSnapshot(second)

	nodeA, _, err := svc.CreateNode("tokyo", "jp.example.com", "JP")
	if err != nil {
		t.Fatal(err)
	}
	nodeB, _, err := svc.CreateNode("osaka", "osaka.example.com", "JP")
	if err != nil {
		t.Fatal(err)
	}

	result, err := svc.ForceSubscriptionRefresh("admin")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if result.At == nil || result.UserCount != 2 || result.NodeCount != 2 || result.Actor != "admin" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if len(spy.config) != 2 || spy.config[0] != nodeA.ID || spy.config[1] != nodeB.ID {
		t.Fatalf("nodes did not receive config rebuild notifications: %#v", spy.config)
	}

	for _, before := range []struct {
		before storeUserSnapshot
		id     int64
	}{
		{before: firstBefore, id: first.ID},
		{before: secondBefore, id: second.ID},
	} {
		after, err := svc.User(before.id)
		if err != nil {
			t.Fatal(err)
		}
		if after.VlessUUID == before.before.VlessUUID || after.Password == before.before.Password || after.SSPassword == before.before.SSPassword {
			t.Fatalf("credentials were not fully rotated for user %d", after.ID)
		}
		if after.SubToken != before.before.SubToken {
			t.Fatalf("subscription token changed for user %d", after.ID)
		}
	}

	status, err := svc.SubscriptionRefreshStatus()
	if err != nil {
		t.Fatalf("refresh status: %v", err)
	}
	if status.At == nil || status.UserCount != 2 || status.Actor != "admin" {
		t.Fatalf("unexpected stored status: %#v", status)
	}
}

type storeUserSnapshot struct {
	VlessUUID  string
	Password   string
	SSPassword string
	SubToken   string
}

func credentialSnapshot(user *store.User) storeUserSnapshot {
	return storeUserSnapshot{VlessUUID: user.VlessUUID, Password: user.Password, SSPassword: user.SSPassword, SubToken: user.SubToken}
}
