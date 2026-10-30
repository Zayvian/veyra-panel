package web

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Zayvian/veyra-panel/internal/service"
)

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	s.renderSettings(w, r, "")
}

func (s *Server) renderSettings(w http.ResponseWriter, r *http.Request, saved string) {
	s.renderSettingsWith(w, r, saved, nil)
}

// renderSettingsWith adds extra to the page data — the recovery codes, which
// exist only in the response that created them.
func (s *Server) renderSettingsWith(w http.ResponseWriter, r *http.Request, saved string, extra map[string]any) {
	path, redirect := s.accessSettings()
	refresh, err := s.svc.SubscriptionRefreshStatus()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	nodes, err := s.svc.Nodes()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	data := map[string]any{
		"AccessPath":          path,
		"RootRedirect":        redirect,
		"Saved":               saved,
		"SubscriptionRefresh": refresh,
		"NodeCount":           len(nodes),
	}
	lines, err := s.svc.Announcement()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	data["Announcement"] = strings.Join(lines, "\n")
	data["AnnouncementLines"] = service.MaxAnnouncementLines
	data["AnnouncementRunes"] = service.MaxAnnouncementRunes
	tmpl, err := s.shareTemplate()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	data["ShareTemplate"] = tmpl
	data["ShareVars"] = shareVars
	data["ShareTemplateRunes"] = MaxShareTemplateRunes
	tz, err := s.svc.Store().Setting(settingTimezone)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if tz == "" {
		tz = DefaultTimezone
	}
	data["Timezone"] = tz
	data["TimezoneChoices"] = timezoneChoices
	data["TimezoneNow"] = time.Now().Format("2006-01-02 15:04")
	data["TimezoneRestart"] = s.restart != nil
	for k, v := range s.domainSettings() {
		data[k] = v
	}
	if err := s.totpSettings(data); err != nil {
		s.fail(w, r, err)
		return
	}
	for k, v := range extra {
		data[k] = v
	}
	s.page(w, r, "settings", data)
}

func (s *Server) postSettings(w http.ResponseWriter, r *http.Request) {
	path, err := normalizePanelAccessPath(r.FormValue("access_path"))
	if err != nil {
		s.errorBanner(w, http.StatusBadRequest, err.Error())
		return
	}
	redirect, err := normalizeRootRedirect(r.FormValue("root_redirect"))
	if err != nil {
		s.errorBanner(w, http.StatusBadRequest, err.Error())
		return
	}
	oldPath, oldRedirect := s.accessSettings()
	if err := s.setAccessSettings(path, redirect); err != nil {
		s.fail(w, r, err)
		return
	}
	if path != oldPath {
		// The current request came through the old path.  Sending an htmx
		// redirect makes the browser leave it immediately, instead of
		// returning a page whose links point at a location it has not loaded.
		s.redirect(w, r, "/")
		return
	}
	if redirect != oldRedirect {
		s.renderSettings(w, r, "已保存。根域名访问行为已更新")
		return
	}
	s.renderSettings(w, r, "已保存")
}

func (s *Server) postAnnouncement(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.SetAnnouncement(r.FormValue("announcement")); err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderSettings(w, r, "订阅公告已保存，客户更新订阅后就能看到")
}

func (s *Server) postShareTemplate(w http.ResponseWriter, r *http.Request) {
	if err := s.setShareTemplate(r.FormValue("share_template")); err != nil {
		s.errorBanner(w, http.StatusBadRequest, err.Error())
		return
	}
	s.renderSettings(w, r, "话术模板已保存，之后打开的每个用户都会用新话术")
}

func (s *Server) postForceSubscriptionRefresh(w http.ResponseWriter, r *http.Request) {
	if r.FormValue("confirmation") != "全部断连" {
		s.errorBanner(w, http.StatusBadRequest, "请输入“全部断连”确认此操作")
		return
	}
	actor, err := s.sess.user(r, s.sessionGen)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	result, err := s.svc.ForceSubscriptionRefresh(actor)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderSettings(w, r, fmt.Sprintf("已轮换 %d 个用户的连接凭据，并通知 %d 个节点重建连接。用户更新原订阅后即可恢复。", result.UserCount, result.NodeCount))
}

// postTimezone saves the panel's time zone. It takes effect on the next start
// (see ApplyTimezone), so when the panel runs under systemd it restarts itself.
func (s *Server) postTimezone(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("timezone"))
	if _, err := loadTimezone(name); err != nil {
		s.errorBanner(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.svc.Store().SetSetting(settingTimezone, name); err != nil {
		s.fail(w, r, err)
		return
	}
	if s.restart == nil {
		s.renderSettings(w, r, "时区已保存，重启面板后生效")
		return
	}
	s.log.Info("panel time zone changed; restarting", "zone", name)
	s.renderSettings(w, r, "时区已保存，面板正在重启，几秒后刷新页面即可")
	go func() {
		time.Sleep(2 * time.Second)
		s.restart()
	}()
}
