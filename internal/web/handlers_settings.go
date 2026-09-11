package web

import "net/http"

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	s.renderSettings(w, r, "")
}

func (s *Server) renderSettings(w http.ResponseWriter, r *http.Request, saved string) {
	path, redirect := s.accessSettings()
	s.page(w, r, "settings", map[string]any{
		"AccessPath":   path,
		"RootRedirect": redirect,
		"Saved":        saved,
	})
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
