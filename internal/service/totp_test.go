package service

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// RFC 6238 appendix B, SHA-1, truncated to the six digits used here.
func TestTOTPCodeMatchesRFC6238(t *testing.T) {
	key := []byte("12345678901234567890")
	for _, tc := range []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
		{20000000000, "353130"},
	} {
		if got := totpCode(key, tc.unix/totpPeriod); got != tc.want {
			t.Errorf("t=%d: %s, want %s", tc.unix, got, tc.want)
		}
	}
}

func currentCode(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	key, err := b32.DecodeString(secret)
	if err != nil {
		t.Fatal(err)
	}
	return totpCode(key, at.Unix()/totpPeriod)
}

func TestTOTPLifecycle(t *testing.T) {
	svc := newTestService(t)
	if err := svc.SetAdmin("admin", "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)

	// Off: the password alone signs in, whatever is in the code field.
	if err := svc.CheckAdminLogin("admin", "correct-horse-battery", "", now); err != nil {
		t.Fatalf("login without 2FA: %v", err)
	}

	secret, uri, err := svc.BeginTOTP("admin")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(uri, "otpauth://totp/Veyra:admin?") || !strings.Contains(uri, "secret="+secret) {
		t.Fatalf("uri: %s", uri)
	}
	// Parked, not on: a bad scan cannot lock anyone out.
	if st, _ := svc.TOTPStatus(); st.Enabled {
		t.Fatal("enabled before a code was confirmed")
	}
	if err := svc.CheckAdminLogin("admin", "correct-horse-battery", "", now); err != nil {
		t.Fatalf("pending setup blocked login: %v", err)
	}
	if _, err := svc.EnableTOTP("000000", now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("wrong confirmation code: %v", err)
	}
	codes, err := svc.EnableTOTP(currentCode(t, secret, now), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != recoveryCodeCount {
		t.Fatalf("%d recovery codes", len(codes))
	}
	if st, _ := svc.TOTPStatus(); !st.Enabled || st.RecoveryRemaining != recoveryCodeCount {
		t.Fatalf("status after enable: %+v", st)
	}
	if _, _, err := svc.BeginTOTP("admin"); !errors.Is(err, ErrInvalid) {
		t.Fatal("began a second setup while on")
	}

	// On: the password alone is not enough, and every failure looks the same.
	later := now.Add(time.Minute)
	for _, code := range []string{"", "123456", "not-a-code"} {
		if err := svc.CheckAdminLogin("admin", "correct-horse-battery", code, later); !errors.Is(err, ErrBadCredentials) {
			t.Fatalf("code %q: %v, want ErrBadCredentials", code, err)
		}
	}
	good := currentCode(t, secret, later)
	if err := svc.CheckAdminLogin("admin", "wrong-password-here", good, later); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("wrong password, good code: %v", err)
	}
	// A code with spaces, as some apps display it.
	if err := svc.CheckAdminLogin("admin", "correct-horse-battery", good[:3]+" "+good[3:], later); err != nil {
		t.Fatalf("good code: %v", err)
	}
	// The same code again is refused: one code, one login.
	if err := svc.CheckAdminLogin("admin", "correct-horse-battery", good, later); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("replayed code: %v", err)
	}
	// A phone 30 seconds slow still works.
	next := later.Add(2 * time.Minute)
	if err := svc.CheckAdminLogin("admin", "correct-horse-battery", currentCode(t, secret, next.Add(-30*time.Second)), next); err != nil {
		t.Fatalf("one step of skew: %v", err)
	}

	// A recovery code works once, typed however.
	rc := strings.ToUpper(codes[0])
	if err := svc.CheckAdminLogin("admin", "correct-horse-battery", rc, next); err != nil {
		t.Fatalf("recovery code: %v", err)
	}
	if err := svc.CheckAdminLogin("admin", "correct-horse-battery", rc, next); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("recovery code reused: %v", err)
	}
	if st, _ := svc.TOTPStatus(); st.RecoveryRemaining != recoveryCodeCount-1 {
		t.Fatalf("remaining: %d", st.RecoveryRemaining)
	}

	// New recovery codes replace the old ones.
	fresh, err := svc.RegenerateRecoveryCodes(codes[1], next)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.CheckAdminLogin("admin", "correct-horse-battery", codes[2], next); !errors.Is(err, ErrBadCredentials) {
		t.Fatal("an old recovery code survived regeneration")
	}

	// Turning it off needs a code too.
	if err := svc.DisableTOTP("", next); !errors.Is(err, ErrInvalid) {
		t.Fatalf("disable without a code: %v", err)
	}
	if err := svc.DisableTOTP(fresh[0], next); err != nil {
		t.Fatal(err)
	}
	if err := svc.CheckAdminLogin("admin", "correct-horse-battery", "", next); err != nil {
		t.Fatalf("login after disable: %v", err)
	}
}

func TestResetTOTP(t *testing.T) {
	svc := newTestService(t)
	if err := svc.SetAdmin("admin", "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	secret, _, err := svc.BeginTOTP("admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EnableTOTP(currentCode(t, secret, now), now); err != nil {
		t.Fatal(err)
	}
	if err := svc.ResetTOTP(); err != nil {
		t.Fatal(err)
	}
	if st, _ := svc.TOTPStatus(); st.Enabled || st.RecoveryRemaining != 0 {
		t.Fatalf("after reset: %+v", st)
	}
	if err := svc.CheckAdminLogin("admin", "correct-horse-battery", "", now); err != nil {
		t.Fatalf("login after reset: %v", err)
	}
}
