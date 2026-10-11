package web

import (
	"fmt"
	"net/http"
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
		IPLimit: 3, ResetDay: 15, ResetHour: 4}, link, now)
	for _, want := range []string{
		"订阅信息（alice）", "订阅链接：" + link, "到期时间：2026-11-10 23:59",
		"流量：100.0 GiB，每月 15 号 04:00 自动重置（下次 10-15）", "最多 3 个 IP", "请勿转发",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message lacks %q:\n%s", want, msg)
		}
	}

	msg = shareMessage(&store.User{Name: "bob"}, link, now)
	for _, want := range []string{"到期时间：长期有效", "流量：不限"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message lacks %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "IP") {
		t.Errorf("unlimited user's message mentions an IP limit:\n%s", msg)
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
