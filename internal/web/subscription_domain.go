package web

import (
	"fmt"
	"net/http"
	"strings"
)

// NormalizeDomain accepts a DNS hostname only, never a URL or a port.
func NormalizeDomain(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "", nil
	}
	if len(value) > 253 || !strings.Contains(value, ".") {
		return "", fmt.Errorf("expected a fully qualified domain name")
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", fmt.Errorf("invalid domain name")
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return "", fmt.Errorf("invalid domain name")
			}
		}
	}
	return value, nil
}

// SetSubscriptionDomain is called once before serving requests.
func (s *Server) SetSubscriptionDomain(domain string) { s.subscriptionDomain = domain }

func (s *Server) subscriptionOrigin(r *http.Request) string {
	if s.subscriptionDomain != "" {
		return "https://" + s.subscriptionDomain
	}
	return strings.TrimSuffix(subURL(r), r.URL.Path)
}
