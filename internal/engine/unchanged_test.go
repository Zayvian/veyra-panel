package engine

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/Zayvian/veyra-node/internal/proto"
)

// The panel re-sends the configuration on every reconnect. Rebuilding for an
// identical one would drop every connection on the node each time the panel
// restarts; it has to be recognised — whitespace and all — and left running.
func TestAnIdenticalConfigurationKeepsTheRunningInstance(t *testing.T) {
	e := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer e.Close()

	port := freePort(t)
	cfg := configWithShadowsocks(t, port)
	if err := e.ApplyConfig(context.Background(), cfg); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	first := e.box

	// Same configuration, different formatting.
	reformatted := json.RawMessage(strings.ReplaceAll(string(cfg), " ", "  "))
	if err := e.ApplyConfig(context.Background(), reformatted); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if e.box != first {
		t.Fatal("an identical configuration rebuilt the instance")
	}
	waitListening(t, port, true)

	// A real change still rebuilds.
	if err := e.ApplyConfig(context.Background(), configWithShadowsocks(t, freePort(t))); err != nil {
		t.Fatalf("third apply: %v", err)
	}
	if e.box == first {
		t.Fatal("a changed configuration did not rebuild the instance")
	}
}

// Skipping an unchanged user list is only safe within one instance. A rebuilt
// instance starts with no users, and the panel follows the rebuild with the very
// same list — skipping it there would leave every user locked out.
func TestTheSameUsersAreAppliedAgainAfterARebuild(t *testing.T) {
	e := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer e.Close()

	if err := e.ApplyConfig(context.Background(), configWithShadowsocks(t, freePort(t))); err != nil {
		t.Fatalf("apply: %v", err)
	}
	users := proto.UsersData{
		ByTag: map[string][]proto.User{"ss": {{
			Name:     "alice",
			Password: "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=",
		}}},
		StatsUsers: []string{"alice"},
	}
	if err := e.UpdateUsers(context.Background(), users); err != nil {
		t.Fatalf("update users: %v", err)
	}
	if !e.usersHashSet {
		t.Fatal("a successful update did not record its fingerprint")
	}
	if err := e.UpdateUsers(context.Background(), users); err != nil {
		t.Fatalf("repeat update: %v", err)
	}

	if err := e.ApplyConfig(context.Background(), configWithShadowsocks(t, freePort(t))); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if e.usersHashSet {
		t.Fatal("the fingerprint survived a rebuild; the same list would be skipped")
	}
	if err := e.UpdateUsers(context.Background(), users); err != nil {
		t.Fatalf("update after rebuild: %v", err)
	}
	if !e.usersHashSet {
		t.Fatal("the list was not applied after the rebuild")
	}
}
