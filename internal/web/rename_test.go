package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kosje/skysbx-panel/internal/service"
	"github.com/kosje/skysbx-panel/internal/store"
)

func TestRenameThroughAdminForms(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	svc := service.New(st)
	if err := svc.SetAdmin("admin", "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}
	node, token, err := svc.CreateNode("香港旧名称", "hk.example.com", "HK", 100)
	if err != nil {
		t.Fatal(err)
	}
	user, err := svc.CreateUser(service.NewUser{Name: "alice", TrafficLimit: 200 << 30})
	if err != nil {
		t.Fatal(err)
	}
	in, err := svc.CreateInbound(node.ID, service.InboundSpec{Protocol: store.ProtoShadowsocks, Port: 8388})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordTraffic(node.ID, map[string]service.Usage{"alice": {Down: 1000}}); err != nil {
		t.Fatal(err)
	}
	srv, err := New(svc, &fakeChannel{}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()
	session, csrf := loginForTest(t, h)
	request := func(method, path string, form url.Values) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		req.Header.Set("X-CSRF-Token", csrf.Value)
		req.AddCookie(session)
		req.AddCookie(csrf)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	for _, tc := range []struct {
		path, field string
		form        url.Values
	}{
		{fmt.Sprintf("/users/%d", user.ID), "name", url.Values{"name": {"alice-new"}, "traffic_limit_gb": {"200"}}},
		{fmt.Sprintf("/nodes/%d", node.ID), "name", url.Values{"name": {"香港新名称"}, "address": {node.Address}, "country": {"HK"}, "traffic_rate": {"0.1"}, "sort_order": {"10"}}},
		{fmt.Sprintf("/inbounds/%d", in.ID), "tag", url.Values{"tag": {"香港下载 01"}, "port": {"8388"}, "sort_order": {"20"}}},
	} {
		rec := request(http.MethodGet, tc.path+"/edit", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("edit %s: %d %s", tc.path, rec.Code, rec.Body)
		}
		input := regexp.MustCompile(`<input\b[^>]*\bname="` + tc.field + `"[^>]*>`).FindString(rec.Body.String())
		if input == "" || strings.Contains(input, "disabled") || strings.Contains(input, "readonly") {
			t.Fatalf("name input is not editable at %s: %s", tc.path, input)
		}
		rec = request(http.MethodPost, tc.path, tc.form)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), tc.form.Get(tc.field)) {
			t.Fatalf("save %s: %d %s", tc.path, rec.Code, rec.Body)
		}
	}
	updatedUser, err := svc.User(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedUser.Name != "alice-new" || updatedUser.SubToken != user.SubToken || updatedUser.VlessUUID != user.VlessUUID ||
		updatedUser.Password != user.Password || updatedUser.SSPassword != user.SSPassword || updatedUser.TrafficUsed != 100 {
		t.Fatal("user rename changed credentials or usage")
	}
	if err := svc.RecordTraffic(node.ID, map[string]service.Usage{"alice-new": {Down: 1000}}); err != nil {
		t.Fatal(err)
	}
	updatedUser, err = svc.User(user.ID)
	if err != nil || updatedUser.TrafficUsed != 200 {
		t.Fatal("usage no longer recorded under new name")
	}
	updatedNode, err := svc.Node(node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedNode.Name != "香港新名称" || updatedNode.TokenSHA != service.TokenSHA(token) || updatedNode.RateMilli != 100 || updatedNode.SortOrder != 10 {
		t.Fatal("node name not saved or token/rate changed")
	}
	rec := request(http.MethodGet, "/sub/"+user.SubToken+"?format=clash", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "香港下载 01") {
		t.Fatalf("subscription missing renamed inbound: %d %s", rec.Code, rec.Body)
	}
	rec = request(http.MethodPost, fmt.Sprintf("/inbounds/%d", in.ID), url.Values{"tag": {""}, "port": {"8388"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty name accepted: %d", rec.Code)
	}
	stored, err := svc.Store().Inbound(in.ID)
	if err != nil || stored.Tag != "香港下载 01" || stored.SortOrder != 20 {
		t.Fatal("invalid edit changed the name")
	}
}
