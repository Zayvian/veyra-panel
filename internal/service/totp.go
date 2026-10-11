package service

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Two-step login for the administrator: a TOTP code (RFC 6238 — SHA-1, six
// digits, 30 seconds, what every authenticator app speaks) after the
// password, with one-time recovery codes for a lost phone, and -reset-2fa on
// the command line for a lost phone and lost codes.
//
// The password guards the panel against the internet; this guards it against
// the password having leaked, which for a panel that holds every customer's
// credentials is the case worth paying one more field for.

const (
	settingTOTPSecret   = "admin.totp_secret"    // base32; non-empty means enabled
	settingTOTPPending  = "admin.totp_pending"   // a secret shown but not yet confirmed
	settingTOTPLastStep = "admin.totp_last_step" // the last time step accepted
	settingTOTPRecovery = "admin.totp_recovery"  // sha256 hex of each unused code, one per line

	totpPeriod        = 30
	totpDigits        = 6
	totpSkew          = 1 // steps either side, for a phone clock that has drifted
	recoveryCodeCount = 10
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// TOTPStatus is what the settings page shows.
type TOTPStatus struct {
	Enabled           bool
	RecoveryRemaining int
}

func (s *Service) TOTPStatus() (TOTPStatus, error) {
	secret, err := s.st.Setting(settingTOTPSecret)
	if err != nil {
		return TOTPStatus{}, err
	}
	codes, err := s.recoveryHashes()
	if err != nil {
		return TOTPStatus{}, err
	}
	return TOTPStatus{Enabled: secret != "", RecoveryRemaining: len(codes)}, nil
}

// BeginTOTP makes a new secret and parks it until a code from it is entered,
// so a scan that went wrong cannot lock the administrator out. It returns the
// secret for typing in by hand and the otpauth:// URI for the QR code.
func (s *Service) BeginTOTP(account string) (secret, uri string, err error) {
	if st, err := s.TOTPStatus(); err != nil {
		return "", "", err
	} else if st.Enabled {
		return "", "", invalid("two-step login is already on")
	}
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	secret = b32.EncodeToString(raw)
	if err := s.st.SetSetting(settingTOTPPending, secret); err != nil {
		return "", "", err
	}
	return secret, totpURI(secret, account), nil
}

// PendingTOTP is the secret BeginTOTP parked, if any, so the page can show
// the same QR code again after a mistyped code.
func (s *Service) PendingTOTP(account string) (secret, uri string, err error) {
	secret, err = s.st.Setting(settingTOTPPending)
	if err != nil || secret == "" {
		return "", "", err
	}
	return secret, totpURI(secret, account), nil
}

func totpURI(secret, account string) string {
	label := url.PathEscape("Veyra:" + account)
	q := url.Values{
		"secret": {secret}, "issuer": {"Veyra"},
		"algorithm": {"SHA1"}, "digits": {strconv.Itoa(totpDigits)}, "period": {strconv.Itoa(totpPeriod)},
	}
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// EnableTOTP confirms the parked secret with a code from it and turns two-step
// login on. The recovery codes it returns are shown once and stored only as
// hashes.
func (s *Service) EnableTOTP(code string, now time.Time) ([]string, error) {
	s.totpMu.Lock()
	defer s.totpMu.Unlock()
	secret, err := s.st.Setting(settingTOTPPending)
	if err != nil {
		return nil, err
	}
	if secret == "" {
		return nil, invalid("start setting up two-step login first")
	}
	step, ok := totpMatch(secret, code, now, 0)
	if !ok {
		return nil, invalid("that code is not right — check the phone's clock and try the next one")
	}
	codes, hashes, err := newRecoveryCodes()
	if err != nil {
		return nil, err
	}
	if err := s.st.SetSettings(map[string]string{
		settingTOTPSecret:   secret,
		settingTOTPPending:  "",
		settingTOTPLastStep: strconv.FormatInt(step, 10),
		settingTOTPRecovery: hashes,
	}); err != nil {
		return nil, err
	}
	return codes, nil
}

// DisableTOTP turns two-step login off. It takes a current code — or a
// recovery code — so a session left open is not enough to remove it.
func (s *Service) DisableTOTP(code string, now time.Time) error {
	s.totpMu.Lock()
	defer s.totpMu.Unlock()
	if err := s.checkSecondFactorLocked(code, now); err != nil {
		return err
	}
	return s.ResetTOTP()
}

// RegenerateRecoveryCodes replaces every recovery code, used or not.
func (s *Service) RegenerateRecoveryCodes(code string, now time.Time) ([]string, error) {
	s.totpMu.Lock()
	defer s.totpMu.Unlock()
	if err := s.checkSecondFactorLocked(code, now); err != nil {
		return nil, err
	}
	codes, hashes, err := newRecoveryCodes()
	if err != nil {
		return nil, err
	}
	return codes, s.st.SetSetting(settingTOTPRecovery, hashes)
}

// ResetTOTP turns two-step login off unconditionally. It is what -reset-2fa
// runs: root on the panel host already owns the database, so asking it for a
// code would protect nothing.
func (s *Service) ResetTOTP() error {
	return s.st.SetSettings(map[string]string{
		settingTOTPSecret:   "",
		settingTOTPPending:  "",
		settingTOTPLastStep: "",
		settingTOTPRecovery: "",
	})
}

// CheckAdminLogin is CheckAdmin plus the second step when it is on. Any
// failure is ErrBadCredentials, whichever part failed, so a correct password
// with a wrong code tells a guesser nothing about the password.
func (s *Service) CheckAdminLogin(username, password, code string, now time.Time) error {
	if err := s.CheckAdmin(username, password); err != nil {
		return err
	}
	s.totpMu.Lock()
	defer s.totpMu.Unlock()
	secret, err := s.st.Setting(settingTOTPSecret)
	if err != nil {
		return err
	}
	if secret == "" {
		return nil
	}
	if err := s.checkSecondFactorLocked(code, now); err != nil {
		if errors.Is(err, ErrInvalid) {
			return ErrBadCredentials
		}
		return err
	}
	return nil
}

// checkSecondFactorLocked accepts a current TOTP code, once, or an unused
// recovery code, which it then spends. Callers hold totpMu, which is what
// makes "once" true for two logins racing with the same code.
func (s *Service) checkSecondFactorLocked(code string, now time.Time) error {
	secret, err := s.st.Setting(settingTOTPSecret)
	if err != nil {
		return err
	}
	if secret == "" {
		return invalid("two-step login is not on")
	}
	code = normalizeCode(code)
	if len(code) == totpDigits && isDigits(code) {
		lastRaw, err := s.st.Setting(settingTOTPLastStep)
		if err != nil {
			return err
		}
		last, _ := strconv.ParseInt(lastRaw, 10, 64)
		// A code is good for one login: anything at or before the last step
		// accepted is refused, so a code read over a shoulder is already spent.
		if step, ok := totpMatch(secret, code, now, last); ok {
			return s.st.SetSetting(settingTOTPLastStep, strconv.FormatInt(step, 10))
		}
		return invalid("wrong code")
	}
	hashes, err := s.recoveryHashes()
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(code))
	want := hex.EncodeToString(sum[:])
	for i, h := range hashes {
		if subtle.ConstantTimeCompare([]byte(h), []byte(want)) == 1 {
			rest := append(hashes[:i:i], hashes[i+1:]...)
			return s.st.SetSetting(settingTOTPRecovery, strings.Join(rest, "\n"))
		}
	}
	return invalid("wrong code")
}

// totpMatch finds the time step within the skew window whose code is code,
// considering only steps after notAfter. It reports the step it matched.
func totpMatch(secret, code string, now time.Time, notAfter int64) (int64, bool) {
	key, err := b32.DecodeString(secret)
	if err != nil {
		return 0, false
	}
	code = normalizeCode(code)
	cur := now.Unix() / totpPeriod
	for d := int64(-totpSkew); d <= totpSkew; d++ {
		step := cur + d
		if step <= notAfter {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(totpCode(key, step)), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}

// totpCode is RFC 4226's HOTP for one counter value.
func totpCode(key []byte, counter int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(counter))
	m := hmac.New(sha1.New, key)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, v%1_000_000)
}

// newRecoveryCodes makes the codes to show — ten characters as two groups of
// five, from an alphabet with nothing to confuse — and their stored hashes.
func newRecoveryCodes() (codes []string, hashes string, err error) {
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789"
	var lines []string
	for range recoveryCodeCount {
		raw := make([]byte, 10)
		if _, err := rand.Read(raw); err != nil {
			return nil, "", err
		}
		for i := range raw {
			// 256 is not a multiple of 31, so this is very slightly biased.
			// Fifty bits of a ten-character code leave plenty either way.
			raw[i] = alphabet[int(raw[i])%len(alphabet)]
		}
		code := string(raw[:5]) + "-" + string(raw[5:])
		sum := sha256.Sum256([]byte(normalizeCode(code)))
		codes = append(codes, code)
		lines = append(lines, hex.EncodeToString(sum[:]))
	}
	return codes, strings.Join(lines, "\n"), nil
}

func (s *Service) recoveryHashes() ([]string, error) {
	raw, err := s.st.Setting(settingTOTPRecovery)
	if err != nil || raw == "" {
		return nil, err
	}
	return strings.Split(raw, "\n"), nil
}

// normalizeCode forgives what people do when typing a code: spaces, dashes,
// capitals.
func normalizeCode(code string) string {
	code = strings.ToLower(code)
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '\t' {
			return -1
		}
		return r
	}, code)
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
