package service

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"
)

// selfSignedValidity is how long a generated certificate lasts. Nothing
// renews it, and clients pin this exact certificate rather than trusting a
// CA, so a short lifetime would only mean every node going dark on the same
// day. Ten years is past the life of any VPS.
const selfSignedValidity = 10 * 365 * 24 * time.Hour

// SelfSignedCert is a certificate the panel generated for one inbound, for a
// node that cannot obtain a real one: a NAT VPS whose port 80 is not
// forwarded, or a host with no domain at all.
//
// The certificate and key are sent to the node inline in the inbound's
// configuration, so there is nothing to install on the node. Clients cannot
// verify it against a CA, so the subscription tells them what to trust
// instead: sing-box gets the certificate itself, mihomo and share links its
// SHA-256 fingerprint.
type SelfSignedCert struct {
	CertPEM string
	KeyPEM  string
	// SHA256 is the lowercase hex SHA-256 of the certificate's DER, the form
	// Hysteria2's pinSHA256 and mihomo's fingerprint both take.
	SHA256 string
}

// NewSelfSignedCert makes an ECDSA P-256 certificate for serverName, which
// becomes its only subject alternative name — an IP SAN when it is an
// address. Clients verify the SAN against the SNI they send, so the two must
// match; EditInbound regenerates the certificate when the SNI changes.
func NewSelfSignedCert(serverName string) (*SelfSignedCert, error) {
	serverName = strings.TrimSpace(serverName)
	if serverName == "" {
		return nil, invalid("a self-signed certificate needs a server name")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate certificate key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, fmt.Errorf("generate certificate serial: %w", err)
	}
	// Backdated an hour so a client whose clock runs slightly behind the
	// panel's does not see a certificate from the future.
	now := time.Now().Add(-time.Hour)
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: serverName},
		NotBefore:             now,
		NotAfter:              now.Add(selfSignedValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if ip := net.ParseIP(strings.Trim(serverName, "[]")); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{serverName}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("create certificate: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshal certificate key: %w", err)
	}
	sum := sha256.Sum256(der)
	return &SelfSignedCert{
		CertPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		KeyPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})),
		SHA256:  hex.EncodeToString(sum[:]),
	}, nil
}

// PEMLines splits a PEM block into the line array sing-box's inline
// certificate and key fields take.
func PEMLines(p string) []string {
	return strings.Split(strings.TrimRight(p, "\n"), "\n")
}
