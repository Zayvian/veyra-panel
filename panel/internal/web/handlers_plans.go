package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Zayvian/veyra-panel/internal/store"
)

func (s *Server) listPlans(w http.ResponseWriter, r *http.Request) {
	s.renderPlans(w, r, http.StatusOK)
}

// renderPlans mirrors renderUsers: the whole page for a navigation, only the
// table for an htmx request.
func (s *Server) renderPlans(w http.ResponseWriter, r *http.Request, code int) {
	plans, err := s.svc.Plans()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	data := map[string]any{
		"Plans":     plans,
		"CSRFToken": s.csrf.csrfValue(r),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(code)
		s.render(w, "plan-table", data)
		return
	}
	data["Page"] = "plans"
	w.WriteHeader(code)
	s.render(w, "plans", data)
}

// daysFromForm reads a whole number of days. Blank is zero.
func daysFromForm(r *http.Request) (int, error) {
	v := strings.TrimSpace(r.FormValue("days"))
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("days must be a whole number")
	}
	return n, nil
}

func planFromForm(r *http.Request) (*store.Plan, error) {
	p := &store.Plan{
		Name: r.FormValue("name"),
		Note: r.FormValue("note"),
	}
	var err error
	if p.Days, err = daysFromForm(r); err != nil {
		return nil, err
	}
	if p.TrafficLimit, err = limitFromForm(r); err != nil {
		return nil, err
	}
	if p.IPLimit, err = ipLimitFromForm(r); err != nil {
		return nil, err
	}
	// 「按创建日」 cannot be resolved yet — there is no user — so it is kept
	// as itself and resolved when a user is made from the plan.
	if strings.TrimSpace(r.FormValue("reset_day")) == "created" {
		p.ResetDay = store.PlanResetOnCreation
	} else {
		p.ResetDay, _ = strconv.Atoi(strings.TrimSpace(r.FormValue("reset_day")))
	}
	return p, nil
}

func (s *Server) createPlan(w http.ResponseWriter, r *http.Request) {
	p, err := planFromForm(r)
	if err != nil {
		s.errorBanner(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.svc.CreatePlan(p); err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderPlans(w, r, http.StatusCreated)
}

func (s *Server) editPlan(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.errorBanner(w, http.StatusBadRequest, "bad plan id")
		return
	}
	p, err := s.svc.Plan(id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.render(w, "plan-edit-row", p)
}

func (s *Server) updatePlan(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.errorBanner(w, http.StatusBadRequest, "bad plan id")
		return
	}
	p, err := planFromForm(r)
	if err != nil {
		s.errorBanner(w, http.StatusBadRequest, err.Error())
		return
	}
	p.ID = id
	if err := s.svc.UpdatePlan(p); err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderPlans(w, r, http.StatusOK)
}

func (s *Server) deletePlan(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.errorBanner(w, http.StatusBadRequest, "bad plan id")
		return
	}
	if err := s.svc.DeletePlan(id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderPlans(w, r, http.StatusOK)
}
