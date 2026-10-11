package service

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestAnnouncement(t *testing.T) {
	svc := newTestService(t)
	if lines, err := svc.Announcement(); err != nil || lines != nil {
		t.Fatalf("fresh panel: %v %v, want none", lines, err)
	}

	if err := svc.SetAnnouncement("  📢 官网：example.com \r\n\n续费请联系微信：abc\n"); err != nil {
		t.Fatal(err)
	}
	lines, err := svc.Announcement()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(lines, []string{"📢 官网：example.com", "续费请联系微信：abc"}) {
		t.Fatalf("lines = %q", lines)
	}

	// Blank turns it off.
	if err := svc.SetAnnouncement(" \n "); err != nil {
		t.Fatal(err)
	}
	if lines, _ := svc.Announcement(); lines != nil {
		t.Fatalf("blank announcement still has %q", lines)
	}

	for _, bad := range []string{
		strings.Repeat("长", MaxAnnouncementRunes+1),
		strings.Repeat("a\n", 0) + "1\n2\n3\n4\n5\n6",
		"same\nsame",
		"tab\tinside",
	} {
		if err := svc.SetAnnouncement(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q: %v, want ErrInvalid", bad, err)
		}
	}
}
