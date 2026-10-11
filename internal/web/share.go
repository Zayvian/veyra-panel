package web

import (
	"encoding/base64"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/Zayvian/veyra-panel/internal/service"
	"github.com/Zayvian/veyra-panel/internal/store"
)

// shareUser turns a user's row into what gets sent to them: the subscription
// link as a QR code, and a ready-to-paste message with the link and what the
// account includes. Handing an account over is the one thing done for every
// customer, and it was a copy, then a paste, then typing out the terms by hand.
func (s *Server) shareUser(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.errorBanner(w, http.StatusBadRequest, "bad user id")
		return
	}
	u, err := s.svc.User(id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	link := s.subscriptionOrigin(r) + "/sub/" + u.SubToken
	qr, err := qrDataURI(link)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.render(w, "user-share-row", map[string]any{
		"User":    u,
		"Link":    link,
		"QR":      qr,
		"Message": shareMessage(u, link, nowFunc()),
	})
}

// qrDataURI draws text as a QR code, inline as a PNG data URI. It is a
// template.URL because html/template rewrites any data: URI it is not told is
// safe; this one is an image the panel just drew.
func qrDataURI(text string) (template.URL, error) {
	png, err := qrcode.Encode(text, qrcode.Medium, 240)
	if err != nil {
		return "", err
	}
	return template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(png)), nil
}

// shareMessage is the text an operator pastes to a customer. Plain text, so it
// survives any chat app; one fact per line, so it can be trimmed by hand.
func shareMessage(u *store.User, link string, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "订阅信息（%s）\n", u.Name)
	fmt.Fprintf(&b, "订阅链接：%s\n", link)
	if u.ExpiresAt != nil {
		fmt.Fprintf(&b, "到期时间：%s\n", u.ExpiresAt.In(now.Location()).Format("2006-01-02 15:04"))
	} else {
		b.WriteString("到期时间：长期有效\n")
	}
	if u.TrafficLimit > 0 {
		fmt.Fprintf(&b, "流量：%s", humanBytes(u.TrafficLimit))
		if day := service.ClampResetDay(u.ResetDay); day > 0 {
			next := service.NextScheduledResetAt(u.ResetDay, u.ResetHour, u.ResetMinute, now)
			fmt.Fprintf(&b, "，%s %02d:%02d 自动重置（下次 %s）",
				planResetLabel(day), u.ResetHour, u.ResetMinute, next.Format("01-02"))
		}
		b.WriteString("\n")
	} else {
		b.WriteString("流量：不限\n")
	}
	if u.IPLimit > 0 {
		fmt.Fprintf(&b, "同时在线：最多 %d 个 IP\n", u.IPLimit)
	}
	b.WriteString("\n使用方法：复制订阅链接，在客户端（Clash Verge / mihomo、sing-box、Shadowrocket、v2rayN 等）里添加订阅并更新；也可以用客户端扫码导入。\n")
	b.WriteString("订阅链接等同于账号密码，请勿转发给他人。")
	return b.String()
}
