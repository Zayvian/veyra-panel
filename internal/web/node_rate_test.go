package web

import (
	"encoding/base64"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kosje/skysbx-panel/internal/service"
	"github.com/kosje/skysbx-panel/internal/store"
)

func TestRateFormsAndSubscriptionStatus(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st)
	n, _, err := svc.CreateNode("香港无限流量", "hk.example.com", "HK", 100)
	if err != nil {
		t.Fatal(err)
	}
	u, err := svc.CreateUser(service.NewUser{Name: "alice", TrafficLimit: 200 << 30})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateInbound(n.ID, service.InboundSpec{Protocol: store.ProtoShadowsocks, Port: 8388, Tag: "香港下载 01"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordTraffic(n.ID, map[string]service.Usage{"alice": {Up: 10 << 30, Down: 20 << 30}}); err != nil {
		t.Fatal(err)
	}
	srv, err := New(svc, &fakeChannel{}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.renderNodesEditing(rec, httptest.NewRequest("GET", "/nodes", nil), n.ID)
	if !strings.Contains(rec.Body.String(), `value="0.1"`) || !strings.Contains(rec.Body.String(), "香港无限流量") {
		t.Fatal("rate/name missing from edit form")
	}
	// Saving a rate must preserve it on the next read.
	form := url.Values{"name": {"香港无限流量"}, "address": {"hk.example.com"}, "country": {"HK"}, "traffic_rate": {"0"}}
	req := httptest.NewRequest("POST", "/nodes/1", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	srv.updateNode(rec, req)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	n, _ = svc.Node(n.ID)
	if n.RateMilli != 0 {
		t.Fatal("rate not saved")
	}
	for _, tc := range []struct {
		query, ua string
		status    bool
	}{
		{"", "Shadowrocket/2.2", true}, {"?format=shadowrocket", "Mozilla", true}, {"?format=base64", "v2rayN", false},
	} {
		req = httptest.NewRequest("GET", "https://panel.example.com/sub/"+u.SubToken+tc.query, nil)
		req.Header.Set("User-Agent", tc.ua)
		rec = httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("subscription status %d", rec.Code)
		}
		wantHeader := "upload=1073741824; download=2147483648; total=214748364800"
		if tc.status {
			wantHeader = ""
		}
		if rec.Header().Get("Subscription-Userinfo") != wantHeader {
			t.Fatal(rec.Header())
		}
		raw, err := base64.StdEncoding.DecodeString(rec.Body.String())
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(string(raw), "STATUS=") != tc.status {
			t.Fatal("wrong status selection")
		}
		if tc.status && !strings.Contains(string(raw), "上传：1.00 GB | 下载：2.00 GB | 总量：200.00 GB") {
			t.Fatal(string(raw))
		}
		if strings.Contains(string(raw), "alice") {
			t.Fatal("account leaked into name")
		}
	}
}
