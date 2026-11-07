package service

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"net"
	"strings"
	"testing"

	"github.com/Zayvian/veyra-panel/internal/store"
)

// A self-signed inbound carries its certificate inline and points at no file
// on the node, so a node that never obtained a certificate still starts it.
func TestSelfSignedInboundCarriesItsCertificate(t *testing.T) {
	for _, proto := range []string{store.ProtoHysteria2, store.ProtoTUIC, store.ProtoAnyTLS} {
		t.Run(proto, func(t *testing.T) {
			in, err := BuildInbound(InboundSpec{Tag: proto, Protocol: proto, Port: 9443,
				ServerName: "www.bing.com", SelfSigned: true})
			if err != nil {
				t.Fatal(err)
			}
			sb, err := ParseConfig(in)
			if err != nil {
				t.Fatal(err)
			}
			if sb.TLS.CertificatePath != "" || sb.TLS.KeyPath != "" {
				t.Errorf("self-signed inbound still names files: %q %q",
					sb.TLS.CertificatePath, sb.TLS.KeyPath)
			}
			certPEM := strings.Join(sb.TLS.Certificate, "\n")
			keyPEM := strings.Join(sb.TLS.Key, "\n")
			pair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
			if err != nil {
				t.Fatalf("inline certificate and key do not form a pair: %v", err)
			}

			client, err := ParseClient(in)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(pair.Certificate[0])
			if client.CertSHA256 != hex.EncodeToString(sum[:]) {
				t.Errorf("client fingerprint %q is not the served certificate's", client.CertSHA256)
			}
			if strings.TrimSpace(client.Cert) != strings.TrimSpace(certPEM) {
				t.Error("client certificate is not the served certificate")
			}
		})
	}
}

// The point of pinning: a client that trusts exactly this certificate, and
// nothing else, completes a verified handshake with the server holding it —
// for a domain SNI and for a bare IP alike. This is what the sing-box
// subscription relies on.
func TestSelfSignedCertificateVerifiesWhenPinned(t *testing.T) {
	for _, name := range []string{"www.bing.com", "203.0.113.7"} {
		t.Run(name, func(t *testing.T) {
			c, err := NewSelfSignedCert(name)
			if err != nil {
				t.Fatal(err)
			}
			pair, err := tls.X509KeyPair([]byte(c.CertPEM), []byte(c.KeyPEM))
			if err != nil {
				t.Fatal(err)
			}
			ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{pair}})
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			go func() {
				conn, err := ln.Accept()
				if err == nil {
					_ = conn.(*tls.Conn).Handshake()
					conn.Close()
				}
			}()

			pool := x509.NewCertPool()
			pool.AppendCertsFromPEM([]byte(c.CertPEM))
			conn, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{RootCAs: pool, ServerName: name})
			if err != nil {
				t.Fatalf("pinned handshake failed: %v", err)
			}
			conn.Close()

			block, _ := pem.Decode([]byte(c.CertPEM))
			cert, _ := x509.ParseCertificate(block.Bytes)
			if err := cert.VerifyHostname("other.example.com"); err == nil {
				t.Error("certificate verifies for a name it was not issued for")
			}
			if ip := net.ParseIP(name); ip != nil && len(cert.IPAddresses) != 1 {
				t.Errorf("IP server name produced SANs %v", cert.IPAddresses)
			}
		})
	}
}

// Saving an unrelated change must not reissue the certificate: every client
// pins it, and a new one would cut them all off until they refresh. Changing
// the SNI must, because the old SAN no longer matches. Turning it off goes
// back to the node's files.
func TestEditingASelfSignedInbound(t *testing.T) {
	svc, nodeID := editFixture(t)
	in, err := svc.CreateInbound(nodeID, InboundSpec{Protocol: store.ProtoHysteria2,
		Port: 9443, ServerName: "www.bing.com", SelfSigned: true})
	if err != nil {
		t.Fatal(err)
	}
	first, _ := ParseClient(in)

	in, err = svc.EditInbound(in.ID, InboundEdit{Port: 9444, ServerName: "www.bing.com", SelfSigned: true})
	if err != nil {
		t.Fatal(err)
	}
	kept, _ := ParseClient(in)
	if kept.CertSHA256 != first.CertSHA256 {
		t.Error("an edit that kept the SNI reissued the certificate")
	}

	in, err = svc.EditInbound(in.ID, InboundEdit{Port: 9444, ServerName: "www.apple.com", SelfSigned: true})
	if err != nil {
		t.Fatal(err)
	}
	renamed, _ := ParseClient(in)
	if renamed.CertSHA256 == first.CertSHA256 || renamed.CertSHA256 == "" {
		t.Error("changing the SNI kept a certificate issued for the old name")
	}

	in, err = svc.EditInbound(in.ID, InboundEdit{Port: 9444, ServerName: "www.apple.com"})
	if err != nil {
		t.Fatal(err)
	}
	off, _ := ParseClient(in)
	sb, _ := ParseConfig(in)
	if off.CertSHA256 != "" || off.Cert != "" || len(sb.TLS.Certificate) != 0 || len(sb.TLS.Key) != 0 {
		t.Error("turning self-signed off left the inline certificate behind")
	}
	if sb.TLS.CertificatePath != DefaultCertPath || sb.TLS.KeyPath != DefaultKeyPath {
		t.Errorf("paths are %q %q, want the defaults", sb.TLS.CertificatePath, sb.TLS.KeyPath)
	}
}
