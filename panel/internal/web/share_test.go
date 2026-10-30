package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Zayvian/veyra-panel/internal/service"
	"github.com/Zayvian/veyra-panel/internal/store"
)

func TestShareMessage(t *testing.T) {
	now := time.Date(2026, time.October, 11, 12, 0, 0, 0, time.Local)
	exp := time.Date(2026, time.November, 10, 23, 59, 0, 0, time.Local)
	const link = "https://sub.example.com/sub/tok"

	msg := shareMessage(&store.User{Name: "alice", ExpiresAt: &exp, TrafficLimit: 100 << 30,
		IPLimit: 3, ResetDay: 15, ResetHour: 4}, link, now, DefaultShareTemplate)
	for _, want := range []string{
		"订阅信息（alice）", "订阅链接：" + link, "到期时间：2026-11-10 23:59",
		"流量：100.0 GiB，每月 15 号 04:00 自动重置（下次 10-15）", "最多 3 个 IP", "请勿转发",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message lacks %q:\n%s", want, msg)
		}
	}

	msg = shareMessage(&store.User{Name: "bob"}, link, now, DefaultShareTemplate)
	for _, want := range []string{"到期时间：长期有效", "流量：不限"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message lacks %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "IP") {
		t.Errorf("unlimited user's message mentions an IP limit:\n%s", msg)
	}
}

// The default template reproduces the message the panel always sent.
func TestDefaultShareTemplateKeepsTheOldMessage(t *testing.T) {
	now := time.Date(2026, time.October, 30, 12, 0, 0, 0, time.Local)
	exp := time.Date(2026, time.November, 24, 18, 2, 0, 0, time.Local)
	got := shareMessage(&store.User{Name: "Rusakura", ExpiresAt: &exp, TrafficLimit: 100 << 30,
		IPLimit: 3, ResetDay: 25}, "https://s/sub/t", now, DefaultShareTemplate)
	want := "订阅信息（Rusakura）\n订阅链接：https://s/sub/t\n到期时间：2026-11-24 18:02\n" +
		"流量：100.0 GiB，每月 25 号 00:00 自动重置（下次 11-25）\n同时在线：最多 3 个 IP\n\n" +
		"使用方法：复制订阅链接，在客户端（Clash Verge / mihomo、sing-box、Shadowrocket、v2rayN 等）里添加订阅并更新；也可以用客户端扫码导入。\n" +
		"订阅链接等同于账号密码，请勿转发给他人。"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// A template saved on the settings page is what every user's share row uses;
// saving it blank goes back to the default.
func TestShareTemplateSetting(t *testing.T) {
	svc, c := newAdminClient(t)
	u, err := svc.CreateUser(service.NewUser{Name: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	rec := c.do(http.MethodPost, "/settings/share-template", url.Values{"share_template": {"你好 {用户名}\n链接 {订阅链接}\nIP {IP数}"}})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "话术模板已保存") {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	body := c.do(http.MethodGet, fmt.Sprintf("/users/%d/share", u.ID), nil).Body.String()
	if !strings.Contains(body, "你好 alice\n链接 ") || strings.Contains(body, "IP ") || strings.Contains(body, "使用方法") {
		t.Fatalf("share row ignores the saved template: %s", body)
	}

	c.do(http.MethodPost, "/settings/share-template", url.Values{"share_template": {"  "}})
	body = c.do(http.MethodGet, fmt.Sprintf("/users/%d/share", u.ID), nil).Body.String()
	if !strings.Contains(body, "订阅信息（alice）") {
		t.Fatalf("blank template did not restore the default: %s", body)
	}

	long := strings.Repeat("字", MaxShareTemplateRunes+1)
	if rec := c.do(http.MethodPost, "/settings/share-template", url.Values{"share_template": {long}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("over-long template: %d", rec.Code)
	}
}

func TestShareRow(t *testing.T) {
	svc, c := newAdminClient(t)
	u, err := svc.CreateUser(service.NewUser{Name: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	rec := c.do(http.MethodGet, fmt.Sprintf("/users/%d/share", u.ID), nil)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("share: %d %s", rec.Code, body)
	}
	if !strings.Contains(body, `src="data:image/png;base64,`) {
		t.Fatalf("no QR image (html/template may have blocked the data URI): %s", body)
	}
	if !strings.Contains(body, "/sub/"+u.SubToken) {
		t.Fatalf("message lacks the subscription link: %s", body)
	}
	if rec := c.do(http.MethodGet, "/users/999/share", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown user: %d", rec.Code)
	}
}
