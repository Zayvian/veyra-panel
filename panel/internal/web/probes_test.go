package web

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zayvian/veyra-panel/internal/hub"
	"github.com/Zayvian/veyra-panel/internal/probe"
	"github.com/Zayvian/veyra-panel/internal/service"
	"github.com/Zayvian/veyra-panel/internal/store"
)

type answeringProbe struct{}

func (answeringProbe) CanProbe(int64) bool { return true }

func (answeringProbe) Probe(_ context.Context, _ int64, targets []hub.ProbeTarget, _ time.Duration) ([]hub.ProbeResult, error) {
	out := make([]hub.ProbeResult, len(targets))
	for i, t := range targets {
		out[i] = hub.ProbeResult{Key: t.Key}
	}
	return out, nil
}

// The edit form marks a node as a probe, the list says so, and verdicts from
// the monitor appear on both the node and its inbound.
func TestProbeRoleAndVerdicts(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	svc := service.New(st)
	if err := svc.SetAdmin("admin", "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}
	// Loopback with nothing listening: the panel's own dial is refused at
	// once, so the verdict is "down from abroad" without waiting on a timeout.
	jp, _, err := svc.CreateNode("jp", "127.0.0.1", "JP", 1000)
	if err != nil {
		t.Fatal(err)
	}
	cn, _, err := svc.CreateNode("cn", "cn.example.com", "CN", 1000)
	if err != nil {
		t.Fatal(err)
	}
	in, err := svc.CreateInbound(jp.ID, service.InboundSpec{Protocol: store.ProtoShadowsocks, Port: 1})
	if err != nil {
		t.Fatal(err)
	}

	srv, err := New(svc, &fakeChannel{}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()
	session, csrf := loginForTest(t, h)
	request := func(method, path string, form url.Values) string {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		req.Header.Set("X-CSRF-Token", csrf.Value)
		req.AddCookie(session)
		req.AddCookie(csrf)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s: %d %s", method, path, rec.Code, rec.Body)
		}
		return rec.Body.String()
	}

	form := url.Values{"name": {"cn"}, "address": {"cn.example.com"}, "country": {"CN"},
		"traffic_rate": {"1"}, "sort_order": {"0"}, "probe_field": {"1"}, "probe": {"1"}}
	if body := request(http.MethodPost, fmt.Sprintf("/nodes/%d", cn.ID), form); !strings.Contains(body, "国内探针") {
		t.Fatalf("probe role not shown: %s", body)
	}
	// A save without the marker leaves the role alone.
	form.Del("probe_field")
	form.Del("probe")
	request(http.MethodPost, fmt.Sprintf("/nodes/%d", cn.ID), form)
	if marked, _ := st.ProbeNodes(); !marked[cn.ID] {
		t.Fatal("a save without the checkbox cleared the probe role")
	}

	m := probe.New(st, answeringProbe{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv.SetProbes(m)
	for range 2 {
		if err := m.Round(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := m.Inbound(in.ID); !ok {
		t.Fatal("no verdict after two rounds")
	}
	if body := request(http.MethodGet, "/nodes", nil); !strings.Contains(body, "海外也连不上") {
		t.Fatalf("node verdict not shown: %s", body)
	}
	if body := request(http.MethodGet, fmt.Sprintf("/nodes/%d/inbounds", jp.ID), nil); !strings.Contains(body, "海外也连不上") {
		t.Fatalf("inbound verdict not shown: %s", body)
	}
}
