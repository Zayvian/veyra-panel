package web

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
)

const (
	settingPanelAccessPath = "web.panel_access_path"
	settingRootRedirect    = "web.root_redirect"
)

// panelAccess is deliberately a path rather than another hostname: it keeps
// the panel, node control channel, and subscriptions on the certificate the
// operator already has.  It is an access hint, not authentication; the normal
// login, session, CSRF, and node-token checks remain the security boundary.
type panelAccess struct {
	sync.RWMutex
	path         string
	rootRedirect string
}

func normalizePanelAccessPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if !strings.HasPrefix(value, "/") || value == "/" || strings.HasSuffix(value, "/") ||
		strings.ContainsAny(value, "?#\\") || strings.Contains(value, "//") || len(value) > 128 {
		return "", fmt.Errorf("access path must start with / and contain one or more simple path segments")
	}
	for _, segment := range strings.Split(strings.TrimPrefix(value, "/"), "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("access path contains an invalid segment")
		}
		for _, c := range segment {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
				return "", fmt.Errorf("access path may contain only letters, numbers, hyphens, underscores, and /")
			}
		}
	}
	return value, nil
}

func normalizeRootRedirect(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	u, err := url.ParseRequestURI(value)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return "", fmt.Errorf("root redirect must be a complete http:// or https:// URL")
	}
	return u.String(), nil
}

func (s *Server) accessSettings() (path, redirect string) {
	s.access.RLock()
	defer s.access.RUnlock()
	return s.access.path, s.access.rootRedirect
}

// panelPath is used by every management-page link and form.  Node control and
// subscription URLs intentionally do not go through it: existing nodes and
// client subscriptions must survive an administrator changing this setting.
func (s *Server) panelPath(path string) string {
	base, _ := s.accessSettings()
	if base == "" {
		return path
	}
	if path == "/" {
		return base
	}
	return base + path
}

func (s *Server) setAccessSettings(path, redirect string) error {
	if err := s.svc.Store().SetSetting(settingPanelAccessPath, path); err != nil {
		return err
	}
	if err := s.svc.Store().SetSetting(settingRootRedirect, redirect); err != nil {
		return err
	}
	s.access.Lock()
	s.access.path = path
	s.access.rootRedirect = redirect
	s.access.Unlock()
	return nil
}
