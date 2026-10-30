package web

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Zayvian/veyra-panel/internal/service"
	"github.com/Zayvian/veyra-panel/internal/store"
)

// Saved on the settings page, the announcement heads every subscription; for
// an inactive account it follows the reason the account stopped working.
func TestAnnouncementHeadsTheSubscription(t *testing.T) {
	svc, c := newAdminClient(t)
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

	rec := c.do(http.MethodPost, "/settings/announcement", url.Values{"announcement": {"📢 官网：example.com\n续费请联系：abc"}})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "订阅公告已保存") {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}

	names := func() []string {
		t.Helper()
		rec := httptest.NewRecorder()
		c.h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sub/"+u.SubToken, nil))
		raw, err := base64.StdEncoding.DecodeString(rec.Body.String())
		if err != nil {
			t.Fatalf("subscription is not base64: %v", err)
		}
		var out []string
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			_, frag, _ := strings.Cut(line, "#")
			name, _ := url.QueryUnescape(frag)
			out = append(out, name)
		}
		return out
	}

	got := names()
	if len(got) != 3 || got[0] != "📢 官网：example.com" || got[1] != "续费请联系：abc" || got[2] != "东京 01 [1×]" {
		t.Fatalf("active subscription = %q", got)
	}

	u.Enabled = false
	if err := svc.UpdateUser(u); err != nil {
		t.Fatal(err)
	}
	got = names()
	if len(got) != 3 || got[0] != "账号已停用，请联系管理员" || got[1] != "📢 官网：example.com" {
		t.Fatalf("inactive subscription = %q", got)
	}

	// Blank turns it off.
	c.do(http.MethodPost, "/settings/announcement", url.Values{"announcement": {""}})
	u.Enabled = true
	if err := svc.UpdateUser(u); err != nil {
		t.Fatal(err)
	}
	if got := names(); len(got) != 1 || got[0] != "东京 01 [1×]" {
		t.Fatalf("after clearing = %q", got)
	}
}
