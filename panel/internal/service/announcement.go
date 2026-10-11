package service

import (
	"strings"
	"unicode"
)

const settingAnnouncement = "subscription.announcement"

// Limits on the subscription announcement. Each line becomes an entry at the
// top of every client's node list, so a few short lines is all a list can
// carry before it stops being a list of nodes.
const (
	MaxAnnouncementLines = 5
	MaxAnnouncementRunes = 60
)

// Announcement is the operator's notice for the top of every subscription,
// one entry per line. Empty when there is none.
func (s *Service) Announcement() ([]string, error) {
	raw, err := s.st.Setting(settingAnnouncement)
	if err != nil || raw == "" {
		return nil, err
	}
	return strings.Split(raw, "\n"), nil
}

// SetAnnouncement replaces it. Blank lines are dropped; blank text turns the
// announcement off.
//
// Lines must be distinct: each is the name of an entry, and Clash refuses a
// whole configuration that has two proxies with one name.
func (s *Service) SetAnnouncement(text string) error {
	var lines []string
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if n := len([]rune(line)); n > MaxAnnouncementRunes {
			return invalid("each announcement line can be at most %d characters (one has %d)", MaxAnnouncementRunes, n)
		}
		for _, r := range line {
			if unicode.IsControl(r) {
				return invalid("announcement lines cannot contain control characters")
			}
		}
		if seen[line] {
			return invalid("announcement line %q appears twice; clients need every name to be different", line)
		}
		seen[line] = true
		lines = append(lines, line)
	}
	if len(lines) > MaxAnnouncementLines {
		return invalid("the announcement can have at most %d lines", MaxAnnouncementLines)
	}
	return s.st.SetSetting(settingAnnouncement, strings.Join(lines, "\n"))
}
