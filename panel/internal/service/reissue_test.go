package service

import (
	"errors"
	"testing"

	"github.com/Zayvian/veyra-panel/internal/store"
)

// One shared link is answered by reissuing that one user: new URL, new
// credentials, everyone else untouched, and no listener rebuilt anywhere.
func TestReissueUserSubscriptionReplacesOnlyThatUser(t *testing.T) {
	svc := newTestService(t)
	alice, err := svc.CreateUser(NewUser{Name: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	bob, err := svc.CreateUser(NewUser{Name: "bob"})
	if err != nil {
		t.Fatal(err)
	}
	spy := &spyNotifier{}
	svc.SetNotifier(spy)

	got, err := svc.ReissueUserSubscription(alice.ID)
	if err != nil {
		t.Fatalf("reissue: %v", err)
	}
	if got.SubToken == alice.SubToken || got.VlessUUID == alice.VlessUUID ||
		got.Password == alice.Password || got.SSPassword == alice.SSPassword {
		t.Fatalf("reissue left something unchanged: before %+v after %+v", alice, got)
	}
	if got.Name != alice.Name || got.TrafficUsed != alice.TrafficUsed {
		t.Fatal("reissue changed the account itself")
	}

	// The old URL is gone; the new one resolves.
	if _, err := svc.Store().UserBySubToken(alice.SubToken); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("old subscription token still resolves: %v", err)
	}
	if u, err := svc.Store().UserBySubToken(got.SubToken); err != nil || u.ID != alice.ID {
		t.Fatalf("new subscription token does not resolve: %v", err)
	}

	// Bob is untouched.
	after, err := svc.User(bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if credentialSnapshot(after) != credentialSnapshot(bob) {
		t.Fatal("reissuing alice changed bob")
	}

	// A hot user swap, not a rebuild: nobody else on the node is dropped.
	if spy.users != 1 || len(spy.config) != 0 {
		t.Fatalf("notifications: users=%d config=%v, want one user push and no config", spy.users, spy.config)
	}
}

func TestReissueUnknownUser(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.ReissueUserSubscription(999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
