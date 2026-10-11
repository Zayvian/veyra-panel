package service

import (
	"errors"
	"testing"
	"time"

	"github.com/Zayvian/veyra-panel/internal/store"
)

// A renewal counts from the later of the current expiry and now, turns the
// account back on, optionally zeroes its counter, and is pushed as a user
// change rather than a listener rebuild.
func TestRenewUser(t *testing.T) {
	day := 24 * time.Hour
	// offset places the current expiry relative to now; fromNow says the new
	// expiry counts from now (lapsed) rather than from the old one (early).
	for _, tc := range []struct {
		name    string
		offset  time.Duration
		days    int
		fromNow bool
	}{
		{name: "early renewal adds to what is left", offset: 10 * day, days: 30},
		{name: "lapsed renewal counts from now", offset: -5 * day, days: 30, fromNow: true},
		{name: "expiring this instant counts from now", offset: 0, days: 7, fromNow: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newTestService(t)
			exp := time.Now().Add(tc.offset).Truncate(time.Second)
			u, err := svc.CreateUser(NewUser{Name: "alice", ExpiresAt: &exp})
			if err != nil {
				t.Fatal(err)
			}
			before := time.Now()
			got, err := svc.RenewUser(u.ID, tc.days, false)
			if err != nil {
				t.Fatal(err)
			}
			after := time.Now()
			if got.ExpiresAt == nil {
				t.Fatal("renewal removed the expiry")
			}
			if tc.fromNow {
				lo := before.Truncate(time.Second).AddDate(0, 0, tc.days)
				hi := after.AddDate(0, 0, tc.days)
				if got.ExpiresAt.Before(lo) || got.ExpiresAt.After(hi) {
					t.Fatalf("expiry %v, want %d days from now", got.ExpiresAt, tc.days)
				}
			} else if want := exp.AddDate(0, 0, tc.days); !got.ExpiresAt.Equal(want) {
				t.Fatalf("expiry %v, want %v", got.ExpiresAt, want)
			}
			if !got.Active(time.Now()) {
				t.Fatal("renewed account is not active")
			}
		})
	}
}

func TestRenewUserReenablesAndResetsTraffic(t *testing.T) {
	svc := newTestService(t)
	past := time.Now().Add(-time.Hour)
	u, err := svc.CreateUser(NewUser{Name: "alice", ExpiresAt: &past, TrafficLimit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	bob, err := svc.CreateUser(NewUser{Name: "bob", TrafficLimit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	u.Enabled = false
	if err := svc.UpdateUser(u); err != nil {
		t.Fatal(err)
	}
	n, _, err := svc.CreateNode("tokyo", "jp.example.com", "JP")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Store().AddTraffic(n.ID, 0,map[int64][2]int64{u.ID: {600, 600}, bob.ID: {10, 10}}); err != nil {
		t.Fatal(err)
	}
	spy := &spyNotifier{}
	svc.SetNotifier(spy)

	// Without the reset the usage stays.
	got, err := svc.RenewUser(u.ID, 30, false)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled {
		t.Fatal("renewal did not re-enable the account")
	}
	if got.TrafficUsed == 0 {
		t.Fatal("renewal without reset zeroed the counter")
	}

	got, err = svc.RenewUser(u.ID, 30, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.TrafficUsed != 0 || got.TrafficUp != 0 || got.TrafficDown != 0 {
		t.Fatalf("counter not zeroed: %+v", got)
	}
	if !got.Active(time.Now()) {
		t.Fatal("renewed account is not active")
	}
	if b, _ := svc.User(bob.ID); b.TrafficUsed == 0 {
		t.Fatal("renewing alice reset bob")
	}
	if spy.users != 2 || len(spy.config) != 0 {
		t.Fatalf("notifications: users=%d config=%v, want two user pushes and no config", spy.users, spy.config)
	}
}

// No expiry is not something a renewal should start enforcing.
func TestRenewUserWithoutExpiryKeepsNone(t *testing.T) {
	svc := newTestService(t)
	u, err := svc.CreateUser(NewUser{Name: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.RenewUser(u.ID, 30, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.ExpiresAt != nil {
		t.Fatalf("renewal gave an unlimited account an expiry: %v", got.ExpiresAt)
	}
}

func TestRenewUserRejectsBadInput(t *testing.T) {
	svc := newTestService(t)
	u, err := svc.CreateUser(NewUser{Name: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	for _, days := range []int{0, -1, MaxRenewDays + 1} {
		if _, err := svc.RenewUser(u.ID, days, false); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%d days: %v, want ErrInvalid", days, err)
		}
	}
	if _, err := svc.RenewUser(999, 30, false); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown user: %v, want ErrNotFound", err)
	}
}
