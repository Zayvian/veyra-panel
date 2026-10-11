package service

import (
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"time"
)

// HandshakeCandidates are sites commonly used as Reality handshake targets.
// They are suggestions for the form, nothing more: whether one works well from
// a given node, and from where its users are, is for CheckHandshake and for
// the users to say.
var HandshakeCandidates = []string{
	"www.microsoft.com:443",
	"www.apple.com:443",
	"swdist.apple.com:443",
	"www.amazon.com:443",
	"addons.mozilla.org:443",
	"www.nvidia.com:443",
	"www.samsung.com:443",
}

// handshakeDial is replaced in tests.
var handshakeDial = func(network, addr string, cfg *tls.Config) (*tls.Conn, error) {
	return tls.DialWithDialer(&net.Dialer{Timeout: 6 * time.Second}, network, addr, cfg)
}

// CheckHandshake tells whether a site can serve as a Reality handshake target:
// it has to complete a TLS 1.3 handshake, offer HTTP/2, and present a
// certificate valid for its own name. Reality borrows exactly that handshake,
// and a site without any one of them makes every connection to the inbound
// fail in a way no log explains.
//
// The check runs from the panel, not from the node, so a site that passes may
// still be slow or blocked where the node is. A site that fails, though, will
// not work anywhere.
func CheckHandshake(target string) (string, error) {
	host, port, err := splitHandshake(target)
	if err != nil {
		return "", err
	}
	start := time.Now()
	conn, err := handshakeDial("tcp", net.JoinHostPort(host, strconv.Itoa(port)), &tls.Config{
		ServerName: host,
		MinVersion: tls.VersionTLS13,
		NextProtos: []string{"h2", "http/1.1"},
	})
	if err != nil {
		return "", fmt.Errorf("TLS 1.3 handshake with %s failed: %w", host, err)
	}
	defer conn.Close()
	elapsed := time.Since(start)
	if p := conn.ConnectionState().NegotiatedProtocol; p != "h2" {
		return "", fmt.Errorf("%s does not offer HTTP/2 (negotiated %q)", host, p)
	}
	return fmt.Sprintf("TLS 1.3、HTTP/2、证书都正常，从面板连过去 %d ms", elapsed.Milliseconds()), nil
}
