// handlers_access.go - 完整替换文件
//
// 修改：render(...) 增加 CSRFToken

package web

import (
	"net/http"
	"strconv"

	"github.com/kosje/skysbx-panel/internal/store"
)

type accessGroup struct {
	Node   *store.Node
	Rows   []accessRow
	Chosen int
}

type accessRow struct {
	Inbound *store.Inbound
	Allowed bool
}

func (s *Server) getUserAccess(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.errorBanner(w, http.StatusBadRequest, "bad user id")
		return
	}
	s.renderUserAccess(w, r, id, http.StatusOK)
}

func (s *Server) renderUserAccess(w http.ResponseWriter, r *http.Request, userID int64, code int) {
	u, err := s.svc.User(userID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	nodes, err := s.svc.Nodes()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	inbounds, err := s.svc.Inbounds()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ids, err := s.svc.UserInboundIDs(userID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	unrestricted := len(ids) == 0
	allowed := make(map[int64]bool, len(ids))
	for _, id := range ids {
		allowed[id] = true
	}
	groups := make([]accessGroup, 0, len(nodes))
	for _, n := range nodes {
		g := accessGroup{Node: n}
		for _, in := range inbounds {
			if in.NodeID != n.ID {
				continue
			}
			ok := unrestricted || allowed[in.ID]
			if ok {
				g.Chosen++
			}
			g.Rows = append(g.Rows, accessRow{Inbound: in, Allowed: ok})
		}
		if len(g.Rows) > 0 {
			groups = append(groups, g)
		}
	}
	data := map[string]any{
		"User":         u,
		"Groups":       groups,
		"Unrestricted": unrestricted,
		"Page":         "users",
		"CSRFToken":    s.csrf.csrfValue(r),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	if r.Header.Get("HX-Request") == "true" {
		s.render(w, "access-form", data)
		return
	}
	s.render(w, "access", data)
}

func (s *Server) setUserAccess(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.errorBanner(w, http.StatusBadRequest, "bad user id")
		return
	}
	if err := r.ParseForm(); err != nil {
		s.errorBanner(w, http.StatusBadRequest, "bad form")
		return
	}
	inbounds, err := s.svc.Inbounds()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	picked := make([]int64, 0, len(r.Form["inbound"]))
	for _, v := range r.Form["inbound"] {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			s.errorBanner(w, http.StatusBadRequest, "bad inbound id")
			return
		}
		picked = append(picked, n)
	}
	if len(picked) == len(inbounds) {
		picked = nil
	}
	if err := s.svc.SetUserInbounds(id, picked); err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderUserAccess(w, r, id, http.StatusOK)
}
