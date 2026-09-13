// handlers_users.go - 完整替换文件
//
// 修改：所有 s.page(w, ...) 改为 s.page(w, r, ...)

package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Zayvian/veyra-panel/internal/service"
)

// nowFunc exists so tests can pin time without a clock abstraction
// threaded through every call.
var nowFunc = time.Now

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	s.renderUsers(w, r, http.StatusOK)
}

// renderUsers renders either the whole page or just the table,
// depending on whether htmx asked. Both paths go through here so a
// create and a plain page load can never disagree about what the list
// looks like.
func (s *Server) renderUsers(w http.ResponseWriter, r *http.Request, code int) {
	users, err := s.svc.Users()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	inbounds, err := s.svc.Inbounds()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	restrictions, err := s.svc.Store().UserInboundMap()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	access := make(map[int64]int, len(users))
	for _, u := range users {
		if allowed, restricted := restrictions[u.ID]; restricted {
			access[u.ID] = len(allowed)
		} else {
			access[u.ID] = -1 // unrestricted
		}
	}
	data := map[string]any{
		"Users":              users,
		"SubscriptionOrigin": s.subscriptionOrigin(r),
		"Now":                nowFunc(),
		"Online":             s.nodes.OnlineUsers(),
		"IPs":                s.nodes.UserIPCounts(),
		"Access":             access,
		"InboundCount":       len(inbounds),
		// CSRFToken is added by s.page below; renderUsers is the
		// page- and fragment-level entry point, and htmx requests
		// re-render the table without the layout. CSRF lives in
		// the data either way so a fragment form can still find it.
	}
	if r.Header.Get("HX-Request") != "true" {
		data["Page"] = "users"
	}
	// Set the token up front so both the page and the fragment
	// render carry it.
	data["CSRFToken"] = s.csrf.csrfValue(r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	if r.Header.Get("HX-Request") == "true" {
		s.render(w, "user-table", data)
		return
	}
	s.render(w, "users", data)
}

// expiryFromForm reads the local date-time field. A blank value means no expiry,
// which is a nil pointer rather than the zero time — the zero time is
// in the past, and would lock everyone out.
func expiryFromForm(r *http.Request) (*time.Time, error) {
	v := strings.TrimSpace(r.FormValue("expires_at"))
	if v == "" {
		return nil, nil
	}
	t, err := time.ParseInLocation("2006-01-02T15:04", v, time.Local)
	if err == nil {
		return &t, nil
	}
	// Keep direct API callers and old bookmarked forms compatible. A legacy
	// date always meant the last second of that local day.
	t, err = time.ParseInLocation("2006-01-02", v, time.Local)
	if err != nil {
		return nil, fmt.Errorf("expiry must be like 2026-01-31 23:59")
	}
	t = t.Add(24*time.Hour - time.Second)
	return &t, nil
}

// ipLimitFromForm reads the concurrent-address cap. Blank and zero
// both mean no limit.
func ipLimitFromForm(r *http.Request) (int, error) {
	v := strings.TrimSpace(r.FormValue("ip_limit"))
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("address limit must be a whole number, or blank for no limit")
	}
	return n, nil
}

func resetDayFromForm(r *http.Request, created time.Time) int {
	v := strings.TrimSpace(r.FormValue("reset_day"))
	if v == "created" {
		if created.IsZero() {
			return time.Now().Day()
		}
		return created.In(time.Local).Day()
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return service.ClampResetDay(n)
}

func resetTimeFromForm(r *http.Request) (int, int, error) {
	v := strings.TrimSpace(r.FormValue("reset_time"))
	if v == "" {
		return 0, 0, nil
	}
	t, err := time.Parse("15:04", v)
	if err != nil {
		return 0, 0, fmt.Errorf("reset time must be like 00:00")
	}
	return t.Hour(), t.Minute(), nil
}

func limitFromForm(r *http.Request) (int64, error) {
	v := strings.TrimSpace(r.FormValue("traffic_limit_gb"))
	if v == "" {
		return 0, nil
	}
	gb, err := strconv.ParseFloat(v, 64)
	if err != nil || gb < 0 {
		return 0, fmt.Errorf("traffic limit must be a number of GiB")
	}
	return int64(gb * 1024 * 1024 * 1024), nil
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	nu := service.NewUser{
		Name: r.FormValue("name"),
		Note: strings.TrimSpace(r.FormValue("note")),
	}
	expires, err := expiryFromForm(r)
	if err != nil {
		s.errorBanner(w, http.StatusBadRequest, err.Error())
		return
	}
	nu.ExpiresAt = expires
	limit, err := limitFromForm(r)
	if err != nil {
		s.errorBanner(w, http.StatusBadRequest, err.Error())
		return
	}
	nu.TrafficLimit = limit
	ipLimit, err := ipLimitFromForm(r)
	if err != nil {
		s.errorBanner(w, http.StatusBadRequest, err.Error())
		return
	}
	nu.IPLimit = ipLimit
	nu.ResetDay = resetDayFromForm(r, time.Time{})
	nu.ResetHour, nu.ResetMinute, err = resetTimeFromForm(r)
	if err != nil {
		s.errorBanner(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.svc.CreateUser(nu); err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderUsers(w, r, http.StatusCreated)
}

func (s *Server) editUser(w http.ResponseWriter, r *http.Request) {
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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := map[string]any{
		"User":      u,
		"CSRFToken": s.csrf.csrfValue(r),
	}
	s.render(w, "user-edit-row", data)
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
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
	expires, err := expiryFromForm(r)
	if err != nil {
		s.errorBanner(w, http.StatusBadRequest, err.Error())
		return
	}
	limit, err := limitFromForm(r)
	if err != nil {
		s.errorBanner(w, http.StatusBadRequest, err.Error())
		return
	}
	ipLimit, err := ipLimitFromForm(r)
	if err != nil {
		s.errorBanner(w, http.StatusBadRequest, err.Error())
		return
	}
	u.Name = strings.TrimSpace(r.FormValue("name"))
	u.Note = strings.TrimSpace(r.FormValue("note"))
	u.ExpiresAt = expires
	u.TrafficLimit = limit
	u.IPLimit = ipLimit
	u.ResetDay = resetDayFromForm(r, u.CreatedAt)
	u.ResetHour, u.ResetMinute, err = resetTimeFromForm(r)
	if err != nil {
		s.errorBanner(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.svc.UpdateUser(u); err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderUsers(w, r, http.StatusOK)
}

func (s *Server) toggleUser(w http.ResponseWriter, r *http.Request) {
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
	u.Enabled = !u.Enabled
	if err := s.svc.UpdateUser(u); err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderUsers(w, r, http.StatusOK)
}

func (s *Server) resetUser(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.errorBanner(w, http.StatusBadRequest, "bad user id")
		return
	}
	if err := s.svc.ResetUserTraffic(id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderUsers(w, r, http.StatusOK)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.errorBanner(w, http.StatusBadRequest, "bad user id")
		return
	}
	if err := s.svc.DeleteUser(id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderUsers(w, r, http.StatusOK)
}
