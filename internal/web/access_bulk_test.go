package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zayvian-lee/veyra-panel/internal/service"
	"github.com/zayvian-lee/veyra-panel/internal/store"
)

func TestBulkAccessSavesExplicitChoices(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	svc := service.New(st)
	u, err := svc.CreateUser(service.NewUser{Name: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, name := range []string{"HK", "JP"} {
		n, _, err := svc.CreateNode(name, "node.example.com", name)
		if err != nil {
			t.Fatal(err)
		}
		in, err := svc.CreateInbound(n.ID, service.InboundSpec{Protocol: store.ProtoShadowsocks, Port: 8388})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, fmt.Sprint(in.ID))
	}
	srv, err := New(svc, &fakeChannel{}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	post := func(mode string, values []string) int {
		r := httptest.NewRequest("POST", "/users/1/access", strings.NewReader(url.Values{"access_mode": {mode}, "inbound": values}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("HX-Request", "true")
		r.SetPathValue("id", fmt.Sprint(u.ID))
		w := httptest.NewRecorder()
		srv.setUserAccess(w, r)
		return w.Code
	}
	if post("selected", ids[1:]) != http.StatusOK {
		t.Fatal("cannot select Japan")
	}
	before, err := svc.UserInboundIDs(u.ID)
	if err != nil || len(before) != 1 {
		t.Fatal("wrong saved selection")
	}
	for _, values := range [][]string{nil, {"99999"}, {ids[1], "99999"}, {"bad"}} {
		if post("selected", values) != http.StatusBadRequest {
			t.Fatal("invalid selection accepted")
		}
		after, err := svc.UserInboundIDs(u.ID)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("invalid request changed permissions")
		}
	}
	if post("selected", []string{ids[1], ids[1]}) != http.StatusOK {
		t.Fatal("duplicate selection not handled")
	}
	if post("selected", ids) != http.StatusOK {
		t.Fatal("cannot select all current inbounds")
	}
	selected, err := svc.UserInboundIDs(u.ID)
	if err != nil || len(selected) != 2 {
		t.Fatal("all current was expanded to unrestricted")
	}
	n, _, err := svc.CreateNode("US", "new.example.com", "US")
	if err != nil {
		t.Fatal(err)
	}
	newInbound, err := svc.CreateInbound(n.ID, service.InboundSpec{Protocol: store.ProtoShadowsocks, Port: 8388})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := svc.Subscription(u.SubToken)
	if err != nil || sub.Allowed == nil || sub.Allowed[newInbound.ID] {
		t.Fatal("new inbound unexpectedly allowed")
	}
	users, err := svc.NodeUsers(n.ID)
	if err != nil || len(users[newInbound.Tag]) != 0 {
		t.Fatal("new node received unauthorized user")
	}
	if post("all", nil) != http.StatusOK {
		t.Fatal("cannot explicitly allow all")
	}
	sub, err = svc.Subscription(u.SubToken)
	if err != nil || sub.Allowed != nil {
		t.Fatal("unrestricted mode not saved")
	}
}
