package web

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Zayvian/veyra-panel/internal/service"
	"github.com/Zayvian/veyra-panel/internal/store"
)

// settingPanelDomains records a domain change made from the settings page.
const settingPanelDomains = "panel_domains"

// PanelDomains is which names the panel answers to.
//
// The domain normally comes from the systemd unit's --domain, and the panel
// cannot rewrite that unit: it runs with /etc read-only. So a change made in
// the panel is kept here and wins over the flags for as long as the unit still
// says what it said when the change was made. The next `veyra` upgrade rewrites
// the unit from panel.env, which the panel does update, and the two agree again.
type PanelDomains struct {
	Domain    string `json:"domain"`
	SubDomain string `json:"sub_domain,omitempty"`

	// What --domain and --sub-domain said when the change was made.
	FlagDomain string `json:"flag_domain"`
	FlagSub    string `json:"flag_sub,omitempty"`

	// Names given up but still served, so that nodes and customers holding
	// them keep working until the administrator retires them. Each keeps doing
	// what it did: an old panel name serves the panel and the node channel, an
	// old subscription name serves /sub/. A panel name that also carried the
	// subscriptions — there was no separate subscription domain — is in both.
	OldPanel []string `json:"old_panel,omitempty"`
	OldSub   []string `json:"old_sub,omitempty"`

	// MoveHostFrom is the old panel domain, while nodes and inbounds that
	// named it still wait to be pointed at the new one. That waits for the new
	// certificate: see FinishDomainMove.
	MoveHostFrom string `json:"move_host_from,omitempty"`
}

// Current is the panel domain and, if there is one, the subscription domain.
func (d PanelDomains) Current() []string {
	names := []string{d.Domain}
	if d.SubDomain != "" {
		names = append(names, d.SubDomain)
	}
	return names
}

// Old is every name kept for the time being.
func (d PanelDomains) Old() []string {
	out := slices.Clone(d.OldPanel)
	for _, n := range d.OldSub {
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

// ResolveDomains works out which names to serve from the command line and a
// change saved in the panel. A unit that names something the panel never saw
// means someone ran the installer with a new --domain by hand, and that wins.
func ResolveDomains(st *store.Store, flagDomain, flagSub string) (PanelDomains, error) {
	flags := PanelDomains{Domain: flagDomain, SubDomain: flagSub,
		FlagDomain: flagDomain, FlagSub: flagSub}
	if flagDomain == "" {
		return flags, nil
	}
	raw, err := st.Setting(settingPanelDomains)
	if err != nil || raw == "" {
		return flags, err
	}
	var saved PanelDomains
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		return flags, fmt.Errorf("stored panel domains: %w", err)
	}
	unitUnchanged := strings.EqualFold(flagDomain, saved.FlagDomain) && strings.EqualFold(flagSub, saved.FlagSub)
	unitCaughtUp := strings.EqualFold(flagDomain, saved.Domain) && strings.EqualFold(flagSub, saved.SubDomain)
	if !unitUnchanged && !unitCaughtUp {
		return flags, nil
	}
	return saved, nil
}

// changeDomains is the saved state after moving to domain and sub. What is
// being given up joins the old names; what is being taken leaves them.
func changeDomains(cur PanelDomains, domain, sub string) PanelDomains {
	next := cur
	next.Domain, next.SubDomain = domain, sub
	next.OldPanel, next.OldSub = slices.Clone(cur.OldPanel), slices.Clone(cur.OldSub)
	if cur.Domain != domain {
		next.OldPanel = append(next.OldPanel, cur.Domain)
		if cur.SubDomain == "" {
			next.OldSub = append(next.OldSub, cur.Domain)
		}
		if next.MoveHostFrom == "" {
			next.MoveHostFrom = cur.Domain
		}
	}
	if cur.SubDomain != "" && cur.SubDomain != sub {
		next.OldSub = append(next.OldSub, cur.SubDomain)
	}
	keep := func(names []string) []string {
		var out []string
		for _, n := range names {
			if n != domain && n != sub && !slices.Contains(out, n) {
				out = append(out, n)
			}
		}
		return out
	}
	next.OldPanel = keep(next.OldPanel)
	next.OldSub = keep(next.OldSub)
	if next.MoveHostFrom == domain {
		next.MoveHostFrom = "" // moved back before anything was rewritten
	}
	return next
}

func saveDomains(st *store.Store, d PanelDomains) error {
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return st.SetSetting(settingPanelDomains, string(raw))
}

// SetPanelDomains is called once before serving requests. dataDir is where
// panel.env lives; restart makes the process exit for systemd to start it
// again on the new names.
func (s *Server) SetPanelDomains(d PanelDomains, dataDir string, restart func()) {
	s.domains = d
	s.subscriptionDomain = d.SubDomain
	s.dataDir = dataDir
	s.restart = restart
}

// isSubscriptionHost: the subscription domain, or an old name that served
// subscriptions.
func (s *Server) isSubscriptionHost(host string) bool {
	if s.subscriptionDomain != "" && strings.EqualFold(host, s.subscriptionDomain) {
		return true
	}
	return slices.ContainsFunc(s.domains.OldSub, func(d string) bool { return strings.EqualFold(d, host) })
}

func (s *Server) isOldPanelHost(host string) bool {
	return slices.ContainsFunc(s.domains.OldPanel, func(d string) bool { return strings.EqualFold(d, host) })
}

// pointsHere reports whether name resolves to this server: to an address the
// current domain resolves to, or one on a local interface.
func (s *Server) pointsHere(ctx context.Context, name string) ([]string, error) {
	lookup := s.lookupHost
	if lookup == nil {
		lookup = net.DefaultResolver.LookupHost
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	addrs, err := lookup(ctx, name)
	if err != nil || len(addrs) == 0 {
		return nil, fmt.Errorf("查不到 %s 的解析记录。请先在域名服务商那里添加 A 记录，指向这台服务器", name)
	}
	here := map[string]bool{}
	for _, known := range append(s.domains.Current(), s.domains.OldPanel...) {
		if ips, err := lookup(ctx, known); err == nil {
			for _, ip := range ips {
				here[ip] = true
			}
		}
	}
	if ifaces, err := net.InterfaceAddrs(); err == nil {
		for _, a := range ifaces {
			if ipn, ok := a.(*net.IPNet); ok {
				here[ipn.IP.String()] = true
			}
		}
	}
	for _, a := range addrs {
		if here[a] {
			return addrs, nil
		}
	}
	return addrs, fmt.Errorf("%s 解析到 %s，不是这台服务器。请先把它的 A 记录改成这台服务器的 IP，等几分钟生效后再试",
		name, strings.Join(addrs, "、"))
}

func (s *Server) domainSettings() map[string]any {
	d := s.domains
	stillOld := 0
	if dc, ok := s.nodes.(interface{ DialedHost(int64) string }); ok && d.Domain != "" {
		if nodes, err := s.svc.Nodes(); err == nil {
			for _, n := range nodes {
				if h := dc.DialedHost(n.ID); h != "" && !strings.EqualFold(h, d.Domain) {
					stillOld++
				}
			}
		}
	}
	return map[string]any{
		"PanelDomain":        d.Domain,
		"SubDomain":          d.SubDomain,
		"OldDomains":         d.Old(),
		"NodesOnOldDomain":   stillOld,
		"DomainChangeEnable": d.Domain != "" && s.restart != nil,
	}
}

func (s *Server) postDomain(w http.ResponseWriter, r *http.Request) {
	if s.domains.Domain == "" || s.restart == nil {
		s.errorBanner(w, http.StatusBadRequest, "面板没有以域名方式运行（没有 --domain），不能在这里更换域名")
		return
	}
	domain, err := NormalizeDomain(r.FormValue("domain"))
	if err != nil || domain == "" {
		s.errorBanner(w, http.StatusBadRequest, "新面板域名格式不对，只填域名，比如 panel.example.com，不要带 https:// 或端口")
		return
	}
	sub, err := NormalizeDomain(r.FormValue("sub_domain"))
	if err != nil {
		s.errorBanner(w, http.StatusBadRequest, "订阅域名格式不对，只填域名，比如 sub.example.com；不用单独的订阅域名就留空")
		return
	}
	if sub == domain {
		s.errorBanner(w, http.StatusBadRequest, "订阅域名不能和面板域名相同；想共用就把订阅域名留空")
		return
	}
	cur := s.domains
	if domain == cur.Domain && sub == cur.SubDomain {
		s.errorBanner(w, http.StatusBadRequest, "域名没有变化")
		return
	}
	if r.FormValue("skip_dns") != "on" {
		for _, name := range []string{domain, sub} {
			if name == "" || name == cur.Domain || name == cur.SubDomain || slices.Contains(cur.Old(), name) {
				continue
			}
			if _, err := s.pointsHere(r.Context(), name); err != nil {
				s.errorBanner(w, http.StatusBadRequest, err.Error())
				return
			}
		}
	}

	next := changeDomains(cur, domain, sub)
	if err := saveDomains(s.svc.Store(), next); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := writePanelEnv(filepath.Join(s.dataDir, "panel.env"), domain, sub); err != nil {
		// The database already says it, and that is what the panel starts
		// from. panel.env only matters to the next upgrade.
		s.log.Warn("update panel.env", "error", err)
	}
	s.log.Info("panel domain changed; restarting", "from", cur.Domain, "to", domain,
		"sub_from", cur.SubDomain, "sub_to", sub)

	s.page(w, r, "domain-moving", map[string]any{
		"NewURL": "https://" + domain + s.panelPath("/settings"),
		"OldURL": "https://" + cur.Domain + s.panelPath("/settings"),
	})
	// After the response has gone out; systemd starts the panel again.
	go func() {
		time.Sleep(2 * time.Second)
		s.restart()
	}()
}

func (s *Server) postRetireOldDomains(w http.ResponseWriter, r *http.Request) {
	if s.restart == nil || len(s.domains.Old()) == 0 {
		s.renderSettings(w, r, "没有需要停用的旧域名")
		return
	}
	next := s.domains
	next.OldPanel, next.OldSub = nil, nil
	if err := saveDomains(s.svc.Store(), next); err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("old domains retired; restarting", "domains", s.domains.Old())
	s.page(w, r, "domain-moving", map[string]any{
		"NewURL":  "https://" + s.domains.Domain + s.panelPath("/settings"),
		"Retired": true,
	})
	go func() {
		time.Sleep(2 * time.Second)
		s.restart()
	}()
}

// writePanelEnv updates the domain lines of the installer's panel.env and
// leaves the rest alone. A missing file is not an error: the panel was not
// installed by the installer, and nothing else reads it.
func writePanelEnv(path, domain, sub string) error {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	set := map[string]string{"SKYSBX_DOMAIN": domain, "SKYSBX_SUB_DOMAIN": sub}
	done := map[string]bool{}
	var out bytes.Buffer
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := sc.Text()
		if k, _, ok := strings.Cut(line, "="); ok {
			if v, ours := set[k]; ours {
				if done[k] {
					continue
				}
				line, done[k] = k+"="+v, true
			}
		}
		out.WriteString(line + "\n")
	}
	if err := sc.Err(); err != nil {
		return err
	}
	for _, k := range []string{"SKYSBX_DOMAIN", "SKYSBX_SUB_DOMAIN"} {
		if !done[k] {
			out.WriteString(k + "=" + set[k] + "\n")
		}
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, out.Bytes(), info.Mode().Perm()); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// FinishDomainMove points nodes and inbounds that named the old panel domain at
// the new one, once the new domain has a certificate.
//
// Not before. The node on the panel's own server serves AnyTLS, Hysteria2 and
// TUIC with the panel's certificate, through the cert.pem link in the data
// directory; sing-box reads it only when it rebuilds its listeners, which is
// what rewriting the server name makes it do. Rewriting first would rebuild
// them on the old certificate and leave them there.
func FinishDomainMove(ctx context.Context, svc *service.Service, dataDir string, d PanelDomains, log *slog.Logger) {
	if d.MoveHostFrom == "" || d.Domain == "" {
		return
	}
	for {
		crt := findCertificate(dataDir, d.Domain)
		if crt != "" {
			if err := relinkNodeCertificate(dataDir, crt); err != nil {
				log.Warn("point the local node at the new certificate", "error", err)
			}
			moved, err := svc.MovePanelHost(d.MoveHostFrom, d.Domain)
			if err != nil {
				log.Error("move nodes to the new panel domain", "error", err)
			} else {
				log.Info("moved to the new panel domain", "from", d.MoveHostFrom, "to", d.Domain,
					"nodes", moved.Nodes, "inbounds", moved.Inbounds)
				// Re-read: the administrator may have saved another change
				// since this process started, and that one must not be lost.
				latest := d
				if raw, err := svc.Store().Setting(settingPanelDomains); err == nil && raw != "" {
					json.Unmarshal([]byte(raw), &latest)
				}
				if latest.MoveHostFrom == d.MoveHostFrom && latest.Domain == d.Domain {
					latest.MoveHostFrom = ""
					if err := saveDomains(svc.Store(), latest); err != nil {
						log.Warn("save panel domains", "error", err)
					}
				}
			}
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
}

// findCertificate is CertMagic's file for domain, under the data directory.
func findCertificate(dataDir, domain string) string {
	var found string
	root := filepath.Join(dataDir, "certs", "certificates")
	filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() && e.Name() == domain+".crt" {
			found = path
			return fs.SkipAll
		}
		return nil
	})
	return found
}

// relinkNodeCertificate moves cert.pem and key.pem to crt, but only when they
// are links into the panel's own certificates — which is how the installer
// shares them with a node on the same server. A certificate someone put there
// by hand is theirs.
func relinkNodeCertificate(dataDir, crt string) error {
	certs := filepath.Join(dataDir, "certs") + string(filepath.Separator)
	key := strings.TrimSuffix(crt, ".crt") + ".key"
	for link, target := range map[string]string{"cert.pem": crt, "key.pem": key} {
		path := filepath.Join(dataDir, link)
		cur, err := os.Readlink(path)
		if err != nil || !strings.HasPrefix(cur, certs) || cur == target {
			continue
		}
		tmp := path + ".new"
		os.Remove(tmp)
		if err := os.Symlink(target, tmp); err != nil {
			return err
		}
		if err := os.Rename(tmp, path); err != nil {
			return err
		}
	}
	return nil
}
