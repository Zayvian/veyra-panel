// handlers_policy.go - 完整替换文件
//
// 修改：render(...) 增加 CSRFToken
// getPolicy 改成 s.page(w, r, ...) 形式

package web

import (
	"net/http"
	"strings"

	"github.com/kosje/skysbx-panel/internal/service"
)

func (s *Server) getPolicy(w http.ResponseWriter, r *http.Request) {
	s.renderPolicy(w, r, http.StatusOK, "")
}

func (s *Server) renderPolicy(w http.ResponseWriter, r *http.Request, code int, saved string) {
	p, err := s.svc.Policy()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	data := map[string]any{
		"Policy":     p,
		"Domains":    strings.Join(p.BlockedDomains, "\n"),
		"Speedtests": service.SpeedtestDomains(),
		"Saved":      saved,
		"Page":       "policy",
		"CSRFToken":  s.csrf.csrfValue(r),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	if r.Header.Get("HX-Request") == "true" {
		s.render(w, "policy-form", data)
		return
	}
	s.render(w, "policy", data)
}

func (s *Server) setPolicy(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.errorBanner(w, http.StatusBadRequest, "bad form")
		return
	}
	p := service.Policy{
		BlockBitTorrent: r.FormValue("block_bittorrent") != "",
		BlockSpeedtest:  r.FormValue("block_speedtest") != "",
		BlockedDomains:  strings.Split(r.FormValue("blocked_domains"), "\n"),
	}
	if err := s.svc.SetPolicy(p); err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderPolicy(w, r, http.StatusOK, "已保存，正在推送到所有节点")
}
