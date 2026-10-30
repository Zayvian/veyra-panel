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
	tmpl, err := s.shareTemplate()
	if err != nil {
		s.fail(w, r, err)
		return
	}
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
		"Message": shareMessage(u, link, nowFunc(), tmpl),
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

// settingShareTemplate holds the operator's own wording for shareMessage.
// Empty means DefaultShareTemplate, so a saved copy of the default does not
// pin a panel to old wording.
const settingShareTemplate = "web.share_template"

// MaxShareTemplateRunes caps the template; it is a chat message, not a page.
const MaxShareTemplateRunes = 2000

// DefaultShareTemplate is the message every panel starts with.
const DefaultShareTemplate = `订阅信息（{用户名}）
订阅链接：{订阅链接}
到期时间：{到期时间}
流量：{流量}{重置}
同时在线：最多 {IP数} 个 IP

使用方法：复制订阅链接，在客户端（Clash Verge / mihomo、sing-box、Shadowrocket、v2rayN 等）里添加订阅并更新；也可以用客户端扫码导入。
订阅链接等同于账号密码，请勿转发给他人。`

// shareVars are the placeholders a template can use, in the order the
// settings page lists them.
var shareVars = []string{"{用户名}", "{订阅链接}", "{到期时间}", "{流量}", "{重置}", "{IP数}"}

// shareTemplate is the template in force: the saved one, or the default.
func (s *Server) shareTemplate() (string, error) {
	t, err := s.svc.Store().Setting(settingShareTemplate)
	if err != nil || strings.TrimSpace(t) != "" {
		return t, err
	}
	return DefaultShareTemplate, nil
}

// setShareTemplate saves the template. Blank text, or the default itself,
// clears the setting.
func (s *Server) setShareTemplate(text string) error {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if n := len([]rune(text)); n > MaxShareTemplateRunes {
		return fmt.Errorf("话术模板最多 %d 个字（现在 %d 个）", MaxShareTemplateRunes, n)
	}
	if text == DefaultShareTemplate {
		text = ""
	}
	return s.svc.Store().SetSetting(settingShareTemplate, text)
}

// shareMessage is the text an operator pastes to a customer: the template with
// this user's values filled in. Plain text, so it survives any chat app. A
// line whose placeholders all come out empty — the IP line of a user without
// an IP limit — is left out, rather than sent half-written.
func shareMessage(u *store.User, link string, now time.Time, tmpl string) string {
	expires := "长期有效"
	if u.ExpiresAt != nil {
		expires = u.ExpiresAt.In(now.Location()).Format("2006-01-02 15:04")
	}
	traffic, reset := "不限", ""
	if u.TrafficLimit > 0 {
		traffic = humanBytes(u.TrafficLimit)
		if day, hour, minute := u.ResetSchedule(); day > 0 {
			next := service.NextScheduledResetAt(day, hour, minute, now)
			reset = fmt.Sprintf("，%s %02d:%02d 自动重置（下次 %s）",
				planResetLabel(day), hour, minute, next.Format("01-02"))
		}
	}
	ips := ""
	if u.IPLimit > 0 {
		ips = fmt.Sprint(u.IPLimit)
	}
	values := map[string]string{
		"{用户名}": u.Name, "{订阅链接}": link, "{到期时间}": expires,
		"{流量}": traffic, "{重置}": reset, "{IP数}": ips,
	}
	pairs := make([]string, 0, 2*len(values))
	for k, v := range values {
		pairs = append(pairs, k, v)
	}
	fill := strings.NewReplacer(pairs...)

	var out []string
	for _, line := range strings.Split(tmpl, "\n") {
		used, filled := false, false
		for _, k := range shareVars {
			if strings.Contains(line, k) {
				used = true
				filled = filled || values[k] != ""
			}
		}
		if used && !filled {
			continue
		}
		out = append(out, fill.Replace(line))
	}
	return strings.Join(out, "\n")
}
