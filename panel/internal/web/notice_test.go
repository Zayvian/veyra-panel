package web

import (
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zayvian/veyra-panel/internal/service"
	"github.com/Zayvian/veyra-panel/internal/store"
)

// Through the real handler: an active user gets their nodes, the same user once
// disabled gets the notice and none of their nodes, and an unknown token is
// still a 404 — the notice must not make a guessed token distinguishable.
func TestInactiveSubscriptionCarriesANotice(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	svc := service.New(st)
	n, _, err := svc.CreateNode("tokyo", "jp.example.com", "JP")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateInbound(n.ID, service.InboundSpec{Protocol: store.ProtoShadowsocks, Port: 8388, Tag: "东京 01"}); err != nil {
		t.Fatal(err)
	}
	u, err := svc.CreateUser(service.NewUser{Name: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(svc, &fakeChannel{}, slog.New(slog.NewTextHandler(io.Discard, nil)), false)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()

	fetch := func(token, query string) (int, string) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sub/"+token+query, nil))
		body := rec.Body.String()
		if query == "" {
			// Share links: base64 of URIs whose names are percent-escaped.
			if raw, err := base64.StdEncoding.DecodeString(body); err == nil {
				body = string(raw)
			}
			if unescaped, err := url.QueryUnescape(body); err == nil {
				body = unescaped
			}
		}
		return rec.Code, body
	}

	if code, body := fetch(u.SubToken, ""); code != http.StatusOK || !strings.Contains(body, "jp.example.com") {
		t.Fatalf("active subscription: %d %q", code, body)
	}

	u.Enabled = false
	if err := svc.UpdateUser(u); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"", "?format=clash", "?format=singbox"} {
		code, body := fetch(u.SubToken, q)
		if code != http.StatusOK {
			t.Fatalf("%s: status %d", q, code)
		}
		if !strings.Contains(body, "账号已停用") {
			t.Fatalf("%s: notice missing: %s", q, body)
		}
		if strings.Contains(body, "jp.example.com") {
			t.Fatalf("%s: a disabled user still receives the node: %s", q, body)
		}
	}

	if code, _ := fetch("no-such-token", ""); code != http.StatusNotFound {
		t.Fatalf("unknown token: %d, want 404", code)
	}
}
