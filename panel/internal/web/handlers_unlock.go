package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Zayvian/veyra-panel/internal/service"
	"github.com/Zayvian/veyra-panel/internal/store"
)

// exitView is one exit as the exits page lists it.
type exitView struct {
	service.Exit
	// Where is the exit in a line: the proxy's address, the resolver, or the
	// node and port.
	Where string
	// UsedBy names the nodes that send to it.
	UsedBy []string
	// Problem is why the exit is being skipped, if it is.
	Problem string
}

func (s *Server) getUnlock(w http.ResponseWriter, r *http.Request) {
	s.renderUnlock(w, r, http.StatusOK, 0)
}

func (s *Server) renderUnlock(w http.ResponseWriter, r *http.Request, code int, editID int64) {
	u, err := s.svc.Unlock()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	nodes, err := s.svc.Nodes()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	byID := make(map[int64]*store.Node, len(nodes))
	for _, n := range nodes {
		byID[n.ID] = n
	}
	views := make([]exitView, 0, len(u.Exits))
	for _, e := range u.Exits {
		v := exitView{Exit: e, Where: e.Summary()}
		if e.Kind == service.ExitNode {
			n, ok := byID[e.NodeID]
			switch {
			case !ok:
				v.Where, v.Problem = "节点已删除", "它的节点已经删除，用它的站点现在直连"
			case !n.Enabled:
				v.Where = n.Name + "，端口 " + strconv.Itoa(e.Port)
				v.Problem = "节点已停用，用它的站点现在直连"
			default:
				v.Where = n.Name + "（" + n.Address + "），端口 " + strconv.Itoa(e.Port)
			}
		}
		for _, id := range u.UsedBy(e.ID) {
			if n, ok := byID[id]; ok {
				v.UsedBy = append(v.UsedBy, n.Name)
			}
		}
		views = append(views, v)
	}
	data := map[string]any{
		"Exits":     views,
		"EditID":    editID,
		"Kinds":     service.ExitKinds,
		"Methods":   service.ExitSSMethods,
		"Nodes":     nodes,
		"Services":  service.UnlockServices(),
		"Page":      "unlock",
		"CSRFToken": s.csrf.csrfValue(r),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	if r.Header.Get("HX-Request") == "true" {
		s.render(w, "exit-table", data)
		return
	}
	s.render(w, "unlock", data)
}

// exitFromForm reads the exit form. Fields that do not belong to the chosen
// kind are dropped by the service, so the form can submit all of them.
func exitFromForm(r *http.Request) (service.Exit, error) {
	if err := r.ParseForm(); err != nil {
		return service.Exit{}, err
	}
	num := func(name string) int {
		n, _ := strconv.Atoi(strings.TrimSpace(r.FormValue(name)))
		return n
	}
	e := service.Exit{
		Name:      r.FormValue("name"),
		Kind:      r.FormValue("kind"),
		Server:    r.FormValue("server"),
		Port:      num("port"),
		Username:  r.FormValue("username"),
		Password:  r.FormValue("password"),
		Method:    r.FormValue("method"),
		UUID:      r.FormValue("uuid"),
		Flow:      r.FormValue("flow"),
		Security:  r.FormValue("security"),
		SNI:       r.FormValue("sni"),
		PublicKey: r.FormValue("public_key"),
		ShortID:   r.FormValue("short_id"),
	}
	switch e.Kind {
	case service.ExitDNS:
		e.Server, e.Port = r.FormValue("dns"), 0
	case service.ExitNode:
		e.NodeID = int64(num("node_id"))
		e.Port = num("node_port")
	}
	return e, nil
}

func (s *Server) createExit(w http.ResponseWriter, r *http.Request) {
	e, err := exitFromForm(r)
	if err != nil {
		s.errorBanner(w, http.StatusBadRequest, "bad form")
		return
	}
	if _, err := s.svc.SaveExit(e); err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderUnlock(w, r, http.StatusCreated, 0)
}

func (s *Server) editExit(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.errorBanner(w, http.StatusBadRequest, "bad exit id")
		return
	}
	s.renderUnlock(w, r, http.StatusOK, id)
}

func (s *Server) updateExit(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.errorBanner(w, http.StatusBadRequest, "bad exit id")
		return
	}
	e, err := exitFromForm(r)
	if err != nil {
		s.errorBanner(w, http.StatusBadRequest, "bad form")
		return
	}
	e.ID = id
	if _, err := s.svc.SaveExit(e); err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderUnlock(w, r, http.StatusOK, 0)
}

func (s *Server) deleteExit(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.errorBanner(w, http.StatusBadRequest, "bad exit id")
		return
	}
	if err := s.svc.DeleteExit(id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderUnlock(w, r, http.StatusOK, 0)
}

// nodeExitView is one exit on a node's unlock page, with what this node sends
// to it.
type nodeExitView struct {
	service.Exit
	Where    string
	Services map[string]bool
	Domains  string
}

func (s *Server) getNodeUnlock(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.errorBanner(w, http.StatusBadRequest, "bad node id")
		return
	}
	s.renderNodeUnlock(w, r, id, http.StatusOK, "")
}

func (s *Server) renderNodeUnlock(w http.ResponseWriter, r *http.Request, nodeID int64, code int, saved string) {
	node, err := s.svc.Node(nodeID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	u, err := s.svc.Unlock()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	nodes, err := s.svc.Nodes()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	names := make(map[int64]string, len(nodes))
	for _, n := range nodes {
		names[n.ID] = n.Name
	}
	rules := map[int64]service.UnlockRule{}
	for _, rule := range u.Nodes[nodeID] {
		rules[rule.ExitID] = rule
	}
	var exits []nodeExitView
	for _, e := range u.Exits {
		// A node cannot be its own exit.
		if e.Kind == service.ExitNode && e.NodeID == nodeID {
			continue
		}
		v := nodeExitView{Exit: e, Where: e.Summary(), Services: map[string]bool{}}
		if e.Kind == service.ExitNode {
			v.Where = "节点 " + names[e.NodeID]
		}
		if rule, ok := rules[e.ID]; ok {
			for _, key := range rule.Services {
				v.Services[key] = true
			}
			v.Domains = strings.Join(rule.Domains, "\n")
		}
		exits = append(exits, v)
	}

	// Xray inbounds hand the chosen sites over only on a node new enough to
	// take that; say so rather than leave them going straight out unexplained.
	inbounds, err := s.svc.NodeInbounds(nodeID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	hasXray := false
	for _, in := range inbounds {
		hasXray = hasXray || (in.Enabled && in.Core == store.CoreXray)
	}
	xray, known := s.nodes.XrayCapable(nodeID)
	oldXray := hasXray && known && xray && !s.nodes.XrayUnlockCapable(nodeID)

	data := map[string]any{
		"Node":      node,
		"Exits":     exits,
		"Services":  service.UnlockServices(),
		"OldXray":   oldXray,
		"Saved":     saved,
		"Page":      "nodes",
		"CSRFToken": s.csrf.csrfValue(r),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	if r.Header.Get("HX-Request") == "true" {
		s.render(w, "node-unlock-form", data)
		return
	}
	s.render(w, "node-unlock", data)
}

func (s *Server) setNodeUnlock(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.errorBanner(w, http.StatusBadRequest, "bad node id")
		return
	}
	if err := r.ParseForm(); err != nil {
		s.errorBanner(w, http.StatusBadRequest, "bad form")
		return
	}
	u, err := s.svc.Unlock()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var rules []service.UnlockRule
	for _, e := range u.Exits {
		key := strconv.FormatInt(e.ID, 10)
		rule := service.UnlockRule{
			ExitID:   e.ID,
			Services: r.Form["svc-"+key],
			Domains:  strings.Split(r.FormValue("domains-"+key), "\n"),
		}
		if len(rule.Services) == 0 && strings.TrimSpace(r.FormValue("domains-"+key)) == "" {
			continue
		}
		rules = append(rules, rule)
	}
	if err := s.svc.SetNodeUnlock(id, rules); err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderNodeUnlock(w, r, id, http.StatusOK, "已保存，正在推送到节点")
}
