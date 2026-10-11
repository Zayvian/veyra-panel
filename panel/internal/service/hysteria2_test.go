package service

import (
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/Zayvian/veyra-panel/internal/singbox"
	"github.com/Zayvian/veyra-panel/internal/store"
)

func TestHysteria2ObfsAndMasquerade(t *testing.T) {
	svc := newTestService(t)
	n, _, err := svc.CreateNode("tokyo", "jp.example.com", "JP")
	if err != nil {
		t.Fatal(err)
	}
	in, err := svc.CreateInbound(n.ID, InboundSpec{
		Protocol: store.ProtoHysteria2, Tag: "hy2", Port: 8443, ServerName: "jp.example.com",
		Obfs: true, Masquerade: " https://www.bing.com ",
	})
	if err != nil {
		t.Fatal(err)
	}
	sb, _ := ParseConfig(in)
	client, _ := ParseClient(in)
	if sb.Obfs == nil || sb.Obfs.Type != "salamander" || sb.Obfs.Password == "" {
		t.Fatalf("obfs = %+v", sb.Obfs)
	}
	if client.Obfs != sb.Obfs.Password {
		t.Fatal("clients are not given the server's obfs password")
	}
	if sb.Masquerade == nil || sb.Masquerade.URL != "https://www.bing.com" || !sb.Masquerade.RewriteHost {
		t.Fatalf("masquerade = %+v", sb.Masquerade)
	}
	password := sb.Obfs.Password

	edit := func(obfs bool, masq string) (*store.Inbound, error) {
		return svc.EditInbound(in.ID, InboundEdit{Port: 8443, ServerName: "jp.example.com",
			Obfs: obfs, Masquerade: masq})
	}

	// Changing the masquerade keeps the obfs password: clients are unaffected.
	got, err := edit(true, "https://www.apple.com")
	if err != nil {
		t.Fatal(err)
	}
	sb, _ = ParseConfig(got)
	if sb.Obfs == nil || sb.Obfs.Password != password {
		t.Fatal("an edit that left obfs on changed its password")
	}
	if sb.Masquerade.URL != "https://www.apple.com" {
		t.Fatalf("masquerade not updated: %+v", sb.Masquerade)
	}

	// Off removes it from both sides; on again mints a new password.
	got, err = edit(false, "")
	if err != nil {
		t.Fatal(err)
	}
	sb, _ = ParseConfig(got)
	client, _ = ParseClient(got)
	if sb.Obfs != nil || client.Obfs != "" || sb.Masquerade != nil {
		t.Fatalf("after turning both off: obfs=%+v client=%q masq=%+v", sb.Obfs, client.Obfs, sb.Masquerade)
	}

	for _, bad := range []string{"www.bing.com", "ftp://x.example", "https://"} {
		if _, err := edit(true, bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("masquerade %q accepted: %v", bad, err)
		}
	}

	// Other protocols are untouched by the fields.
	tuic, err := svc.CreateInbound(n.ID, InboundSpec{Protocol: store.ProtoTUIC, Tag: "tuic",
		Port: 9443, ServerName: "jp.example.com", Obfs: true, Masquerade: "https://www.bing.com"})
	if err != nil {
		t.Fatal(err)
	}
	if sb, _ := ParseConfig(tuic); sb.Obfs != nil || sb.Masquerade != nil {
		t.Fatal("TUIC got Hysteria2 options")
	}
}

// A local TLS server stands in for the internet: one that speaks HTTP/2, and
// one that does not.
func TestCheckHandshake(t *testing.T) {
	h2 := httptest.NewUnstartedServer(http.NotFoundHandler())
	h2.EnableHTTP2 = true
	h2.StartTLS()
	defer h2.Close()
	h1 := httptest.NewUnstartedServer(http.NotFoundHandler())
	h1.TLS = &tls.Config{NextProtos: []string{"http/1.1"}}
	h1.StartTLS()
	defer h1.Close()

	saved := handshakeDial
	defer func() { handshakeDial = saved }()
	// Trust the test servers' certificate and dial them whatever name is asked
	// for; the name still goes in the SNI and is still verified.
	pool := h2.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	target := map[string]string{
		"h2.test:443": h2.Listener.Addr().String(),
		"h1.test:443": h1.Listener.Addr().String(),
	}
	handshakeDial = func(network, addr string, cfg *tls.Config) (*tls.Conn, error) {
		c := cfg.Clone()
		c.RootCAs = pool
		c.ServerName = "example.com" // the name httptest's certificate is for
		raw, err := net.Dial(network, target[addr])
		if err != nil {
			return nil, err
		}
		conn := tls.Client(raw, c)
		if err := conn.Handshake(); err != nil {
			raw.Close()
			return nil, err
		}
		return conn, nil
	}

	if msg, err := CheckHandshake("h2.test"); err != nil || !strings.Contains(msg, "HTTP/2") {
		t.Fatalf("an HTTP/2 site: %q %v", msg, err)
	}
	if _, err := CheckHandshake("h1.test:443"); err == nil || !strings.Contains(err.Error(), "HTTP/2") {
		t.Fatalf("a site without HTTP/2 passed: %v", err)
	}
	if _, err := CheckHandshake("bad:port:here"); err == nil {
		t.Fatal("a malformed target passed")
	}
}

// Every site the form offers, and every random pick, has to pass the
// panel's own validation.
func TestMasqueradeCandidatesAreAccepted(t *testing.T) {
	for _, site := range MasqueradeCandidates {
		var in singbox.Inbound
		var client ClientParams
		if err := applyHysteria2(&in, &client, false, site); err != nil {
			t.Errorf("%s: %v", site, err)
		}
	}
	seen := map[string]bool{}
	for range 200 {
		pick := RandomMasquerade()
		if !slices.Contains(MasqueradeCandidates, pick) {
			t.Fatalf("random pick %q is not a candidate", pick)
		}
		seen[pick] = true
	}
	if len(seen) < 2 {
		t.Fatal("the random pick never varies")
	}
}
