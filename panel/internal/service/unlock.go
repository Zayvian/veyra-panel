package service

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"github.com/Zayvian/veyra-panel/internal/singbox"
	"github.com/Zayvian/veyra-panel/internal/store"
)

// Unlock routing sends chosen sites out of a node through somewhere other than
// the node's own address. A site that refuses a Hong Kong address, or that
// shows a datacenter address only its own catalogue, sees the exit's address
// instead, while everything else keeps going straight out.
//
// It is configured in two places. Exits are the somewhere: a proxy on a
// landing server, another node of this panel, or a DNS unlock service. Each
// node then says which services go to which exit. What the node runs is
// ordinary sing-box routing: sniff the domain, match it, route or resolve.

const settingUnlock = "unlock"

// Exit kinds.
const (
	ExitSOCKS       = "socks"
	ExitShadowsocks = "shadowsocks"
	ExitVLESS       = "vless"
	// ExitNode is another node of this panel. The panel opens a Shadowsocks
	// listener on it for the purpose, so there is nothing to type but a port.
	ExitNode = "node"
	// ExitDNS is a DNS unlock service: the listed sites are looked up with its
	// resolver, which answers with an address of its own in the region it
	// unlocks. The connection still leaves from the node.
	ExitDNS = "dns"
)

// ExitKinds is the menu on the exits page, in display order.
var ExitKinds = []ProtocolChoice{
	{ExitSOCKS, "SOCKS5"},
	{ExitShadowsocks, "Shadowsocks"},
	{ExitVLESS, "VLESS"},
	{ExitNode, "本面板节点"},
	{ExitDNS, "DNS 解锁"},
}

// ExitSSMethods are the Shadowsocks methods an exit may use.
var ExitSSMethods = []string{
	"2022-blake3-aes-128-gcm",
	"2022-blake3-aes-256-gcm",
	"2022-blake3-chacha20-poly1305",
	"aes-128-gcm",
	"aes-256-gcm",
	"chacha20-ietf-poly1305",
}

// Exit is one place a node can send chosen sites out through.
type Exit struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`

	// Server and Port: the proxy, for SOCKS, Shadowsocks and VLESS. For
	// ExitDNS, the resolver (Port 0 means 53). For ExitNode, Port is the
	// listener the panel opens on that node; Server is unused.
	Server string `json:"server,omitempty"`
	Port   int    `json:"port,omitempty"`

	Username string `json:"username,omitempty"` // SOCKS
	Password string `json:"password,omitempty"` // SOCKS, Shadowsocks
	Method   string `json:"method,omitempty"`   // Shadowsocks

	UUID     string `json:"uuid,omitempty"`     // VLESS
	Flow     string `json:"flow,omitempty"`     // VLESS
	Security string `json:"security,omitempty"` // VLESS: reality, tls or none
	SNI      string `json:"sni,omitempty"`      // VLESS with TLS or Reality
	// VLESS with Reality.
	PublicKey string `json:"public_key,omitempty"`
	ShortID   string `json:"short_id,omitempty"`

	// ExitNode.
	NodeID int64 `json:"node_id,omitempty"`
	// ServerKey and UserKey are the listener's Shadowsocks 2022 keys,
	// generated with the exit. Two because the node serves Shadowsocks as a
	// multi-user listener, which is what lets it change users in place.
	ServerKey string `json:"server_key,omitempty"`
	UserKey   string `json:"user_key,omitempty"`
}

// Summary is how an exit is described in a list.
func (e Exit) Summary() string {
	switch e.Kind {
	case ExitNode:
		return fmt.Sprintf("本面板节点 #%d，端口 %d", e.NodeID, e.Port)
	case ExitDNS:
		return "DNS " + e.dnsAddr()
	case ExitVLESS:
		return fmt.Sprintf("VLESS %s（%s）", net.JoinHostPort(e.Server, strconv.Itoa(e.Port)), e.Security)
	case ExitShadowsocks:
		return fmt.Sprintf("Shadowsocks %s（%s）", net.JoinHostPort(e.Server, strconv.Itoa(e.Port)), e.Method)
	default:
		return "SOCKS5 " + net.JoinHostPort(e.Server, strconv.Itoa(e.Port))
	}
}

func (e Exit) dnsAddr() string {
	port := e.Port
	if port == 0 {
		port = 53
	}
	return net.JoinHostPort(e.Server, strconv.Itoa(port))
}

// UnlockRule sends a node's connections to the named services and domains out
// through one exit.
type UnlockRule struct {
	ExitID   int64    `json:"exit_id"`
	Services []string `json:"services,omitempty"`
	Domains  []string `json:"domains,omitempty"`
}

// Unlock is everything the unlock routing stores: the exits, and each node's
// rules, by node id.
type Unlock struct {
	Exits []Exit                 `json:"exits,omitempty"`
	Nodes map[int64][]UnlockRule `json:"nodes,omitempty"`
}

// Exit finds an exit by id.
func (u Unlock) Exit(id int64) (Exit, bool) {
	for _, e := range u.Exits {
		if e.ID == id {
			return e, true
		}
	}
	return Exit{}, false
}

// UsedBy lists the nodes with a rule that sends to exit id.
func (u Unlock) UsedBy(id int64) []int64 {
	var out []int64
	for nodeID, rules := range u.Nodes {
		for _, r := range rules {
			if r.ExitID == id {
				out = append(out, nodeID)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// UnlockService is a site the panel knows the domains of.
type UnlockService struct {
	Key     string
	Name    string
	Domains []string
}

// unlockServices are suffixes: each entry goes with everything under it. Only
// what the service itself serves from, so that choosing it sends nothing else
// through the exit.
var unlockServices = []UnlockService{
	{"openai", "ChatGPT / OpenAI", []string{
		"openai.com", "chatgpt.com", "chat.com", "sora.com",
		"oaistatic.com", "oaiusercontent.com",
	}},
	{"claude", "Claude", []string{
		"claude.ai", "claude.com", "anthropic.com", "claudeusercontent.com",
	}},
	{"gemini", "Gemini", []string{
		"gemini.google.com", "gemini.google", "aistudio.google.com",
		"generativelanguage.googleapis.com", "alkalimakersuite-pa.clients6.google.com",
		"makersuite.google.com", "notebooklm.google.com", "notebooklm.google",
		"bard.google.com", "deepmind.google", "labs.google",
	}},
	{"grok", "Grok", []string{"grok.com", "x.ai"}},
	{"netflix", "Netflix", []string{
		"netflix.com", "netflix.net", "nflxext.com", "nflximg.com",
		"nflximg.net", "nflxso.net", "nflxvideo.net",
	}},
	{"disney", "Disney+", []string{
		"disneyplus.com", "disney-plus.net", "dssott.com", "bamgrid.com",
		"disneystreaming.com",
	}},
	{"youtube", "YouTube", []string{
		"youtube.com", "youtu.be", "ytimg.com", "googlevideo.com",
		"youtube-nocookie.com", "youtubei.googleapis.com",
	}},
	{"tiktok", "TikTok", []string{
		"tiktok.com", "tiktokv.com", "tiktokcdn.com", "tiktokcdn-us.com",
		"byteoversea.com", "ibytedtos.com", "ibyteimg.com", "muscdn.com",
		"ttwstatic.com",
	}},
}

// UnlockServices is the built-in list, so the page can show what choosing a
// service actually sends through an exit.
func UnlockServices() []UnlockService { return unlockServices }

func unlockService(key string) (UnlockService, bool) {
	for _, s := range unlockServices {
		if s.Key == key {
			return s, true
		}
	}
	return UnlockService{}, false
}

// Unlock reads the stored exits and rules. One that will not parse reads as
// none, for the same reason a corrupt policy does: it must not take every
// node's configuration with it.
func (s *Service) Unlock() (Unlock, error) {
	raw, err := s.st.Setting(settingUnlock)
	if err != nil || raw == "" {
		return Unlock{}, err
	}
	var u Unlock
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		return Unlock{}, nil
	}
	return u, nil
}

func (s *Service) saveUnlock(u Unlock) error {
	raw, err := json.Marshal(u)
	if err != nil {
		return err
	}
	return s.st.SetSetting(settingUnlock, string(raw))
}

// SaveExit adds an exit, or replaces the one with the same id, and pushes a
// new configuration to every node it touches: those that send to it, and for
// ExitNode the node that serves it.
func (s *Service) SaveExit(e Exit) (Exit, error) {
	u, err := s.Unlock()
	if err != nil {
		return Exit{}, err
	}
	var old Exit
	idx := -1
	if e.ID != 0 {
		for i, x := range u.Exits {
			if x.ID == e.ID {
				old, idx = x, i
			}
		}
		if idx < 0 {
			return Exit{}, invalid("没有这个出口")
		}
	}
	if err := s.checkExit(&e, old, u); err != nil {
		return Exit{}, err
	}
	if idx < 0 {
		for _, x := range u.Exits {
			if x.ID >= e.ID {
				e.ID = x.ID + 1
			}
		}
		if e.ID == 0 {
			e.ID = 1
		}
		u.Exits = append(u.Exits, e)
	} else {
		u.Exits[idx] = e
	}
	if err := s.saveUnlock(u); err != nil {
		return Exit{}, err
	}
	touched := map[int64]bool{}
	for _, id := range u.UsedBy(e.ID) {
		touched[id] = true
	}
	if old.Kind == ExitNode {
		touched[old.NodeID] = true
	}
	if e.Kind == ExitNode {
		touched[e.NodeID] = true
	}
	for id := range touched {
		s.notify.ConfigChanged(id)
	}
	return e, nil
}

// DeleteExit removes an exit no node sends to. One still in use is refused
// rather than taken out from under the nodes: their chosen sites would quietly
// start leaving from their own address again.
func (s *Service) DeleteExit(id int64) error {
	u, err := s.Unlock()
	if err != nil {
		return err
	}
	e, ok := u.Exit(id)
	if !ok {
		return nil
	}
	if users := u.UsedBy(id); len(users) > 0 {
		names := make([]string, 0, len(users))
		for _, nid := range users {
			if n, err := s.st.Node(nid); err == nil {
				names = append(names, n.Name)
			}
		}
		if len(names) > 0 {
			return invalid("出口 %s 还在被 %s 使用，先在它们的分流里取消",
				e.Name, strings.Join(names, ", "))
		}
	}
	kept := u.Exits[:0]
	for _, x := range u.Exits {
		if x.ID != id {
			kept = append(kept, x)
		}
	}
	u.Exits = kept
	for nid, rules := range u.Nodes {
		r := rules[:0]
		for _, x := range rules {
			if x.ExitID != id {
				r = append(r, x)
			}
		}
		u.Nodes[nid] = r
	}
	if err := s.saveUnlock(u); err != nil {
		return err
	}
	if e.Kind == ExitNode {
		s.notify.ConfigChanged(e.NodeID)
	}
	return nil
}

// checkExit validates and normalises an exit in place. old is the stored
// exit it replaces, if any: an ExitNode keeps its keys across edits, so a
// change of port does not mean a change of secret.
func (s *Service) checkExit(e *Exit, old Exit, u Unlock) error {
	e.Name = strings.TrimSpace(e.Name)
	e.Server = strings.TrimSpace(e.Server)
	if e.Name == "" {
		return invalid("出口需要一个名称")
	}
	if len([]rune(e.Name)) > 64 {
		return invalid("出口名称太长")
	}
	for _, x := range u.Exits {
		if x.ID != e.ID && x.Name == e.Name {
			return invalid("已经有一个叫 %s 的出口", e.Name)
		}
	}
	proxy := func() error {
		if e.Server == "" {
			return invalid("出口 %s 需要服务器地址", e.Name)
		}
		if strings.ContainsAny(e.Server, " /") {
			return invalid("服务器 %q 不是域名或 IP", e.Server)
		}
		if e.Port < 1 || e.Port > 65535 {
			return invalid("端口 %d 超出范围", e.Port)
		}
		return nil
	}

	switch e.Kind {
	case ExitSOCKS:
		e.Method, e.UUID, e.Flow, e.Security, e.SNI, e.PublicKey, e.ShortID = "", "", "", "", "", "", ""
		return proxy()

	case ExitShadowsocks:
		e.Username, e.UUID, e.Flow, e.Security, e.SNI, e.PublicKey, e.ShortID = "", "", "", "", "", "", ""
		if err := proxy(); err != nil {
			return err
		}
		ok := false
		for _, m := range ExitSSMethods {
			ok = ok || m == e.Method
		}
		if !ok {
			return invalid("不支持的 Shadowsocks 加密方式 %q", e.Method)
		}
		if e.Password == "" {
			return invalid("Shadowsocks 需要密码")
		}
		return nil

	case ExitVLESS:
		e.Username, e.Password, e.Method = "", "", ""
		if err := proxy(); err != nil {
			return err
		}
		e.UUID = strings.TrimSpace(e.UUID)
		if len(e.UUID) != 36 {
			return invalid("VLESS 需要 UUID")
		}
		switch e.Flow = strings.TrimSpace(e.Flow); e.Flow {
		case "", FlowVision:
		default:
			return invalid("不支持的 VLESS 流控 %q", e.Flow)
		}
		switch e.Security {
		case "reality":
			if e.PublicKey = strings.TrimSpace(e.PublicKey); e.PublicKey == "" {
				return invalid("Reality 需要服务端公钥")
			}
			if e.SNI = strings.TrimSpace(e.SNI); e.SNI == "" {
				return invalid("Reality 需要 SNI")
			}
			e.ShortID = strings.TrimSpace(e.ShortID)
		case "tls":
			e.PublicKey, e.ShortID = "", ""
			e.SNI = strings.TrimSpace(e.SNI)
		case "none":
			e.PublicKey, e.ShortID, e.SNI = "", "", ""
			if e.Flow != "" {
				return invalid("流控 %s 需要 TLS 或 Reality", e.Flow)
			}
		default:
			return invalid("VLESS 安全只能是 Reality、TLS 或无")
		}
		return nil

	case ExitDNS:
		*e = Exit{ID: e.ID, Name: e.Name, Kind: ExitDNS, Server: e.Server, Port: e.Port}
		host, port := e.Server, e.Port
		if h, p, err := net.SplitHostPort(e.Server); err == nil {
			n, err := strconv.Atoi(p)
			if err != nil {
				return invalid("DNS 端口 %q 不是数字", p)
			}
			host, port = h, n
		}
		addr, err := netip.ParseAddr(host)
		if err != nil {
			// A name would need resolving before the resolver could be used,
			// and an unlock vendor hands out addresses anyway.
			return invalid("DNS 服务器要填 IP 地址，不是 %q", host)
		}
		if port == 0 {
			port = 53
		}
		if port < 1 || port > 65535 {
			return invalid("DNS 端口 %d 超出范围", port)
		}
		e.Server, e.Port = addr.String(), port
		return nil

	case ExitNode:
		*e = Exit{ID: e.ID, Name: e.Name, Kind: ExitNode, NodeID: e.NodeID, Port: e.Port,
			ServerKey: old.ServerKey, UserKey: old.UserKey}
		n, err := s.st.Node(e.NodeID)
		if err == store.ErrNotFound {
			return invalid("没有这个节点")
		} else if err != nil {
			return err
		}
		if strings.TrimSpace(n.Address) == "" {
			return invalid("节点 %s 没有可连接的地址", n.Name)
		}
		taken, err := s.portsTaken(e.NodeID, e.ID, u)
		if err != nil {
			return err
		}
		if e.Port == 0 {
			if e.Port, err = freePort(taken); err != nil {
				return err
			}
		}
		if e.Port < 1 || e.Port > 65535 {
			return invalid("端口 %d 超出范围", e.Port)
		}
		if what, ok := taken[e.Port]; ok {
			return invalid("%s 的端口 %d 已被占用（%s）", n.Name, e.Port, what)
		}
		if old.Kind != ExitNode || e.ServerKey == "" {
			e.ServerKey, e.UserKey = NewSSPassword(), NewSSPassword()
		}
		return nil

	default:
		return invalid("未知的出口类型 %q", e.Kind)
	}
}

// portsTaken is every port a node already listens on, and what for: its own
// inbounds, what it relays for others, and the exits it serves, except
// exceptExit.
func (s *Service) portsTaken(nodeID, exceptExit int64, u Unlock) (map[int]string, error) {
	taken := map[int]string{}
	own, err := s.st.NodeInbounds(nodeID)
	if err != nil {
		return nil, err
	}
	for _, in := range own {
		taken[in.Port] = "入站 " + in.Tag
	}
	carried, err := s.st.InboundsRelayedVia(nodeID)
	if err != nil {
		return nil, err
	}
	for _, in := range carried {
		taken[in.RelayPort] = "中转 " + in.Tag
	}
	for _, x := range u.Exits {
		if x.Kind == ExitNode && x.NodeID == nodeID && x.ID != exceptExit {
			taken[x.Port] = "出口 " + x.Name
		}
	}
	return taken, nil
}

// freePort picks a port for a new exit listener at random from a range no
// common service uses, so two panels do not converge on the same one.
func freePort(taken map[int]string) (int, error) {
	const lo, hi = 20000, 50000
	for i := 0; i < 1000; i++ {
		var b [4]byte
		if _, err := rand.Read(b[:]); err != nil {
			return 0, err
		}
		p := lo + int(binary.BigEndian.Uint32(b[:])%uint32(hi-lo))
		if _, ok := taken[p]; !ok {
			return p, nil
		}
	}
	return 0, invalid("没找到空闲端口，请手动填一个")
}

// SetNodeUnlock replaces one node's rules.
//
// A service or domain may go to one exit only. Two would be decided by rule
// order, which the page does not show, so it is refused instead.
func (s *Service) SetNodeUnlock(nodeID int64, rules []UnlockRule) error {
	if _, err := s.st.Node(nodeID); err != nil {
		return err
	}
	u, err := s.Unlock()
	if err != nil {
		return err
	}
	owner := map[string]string{}
	var kept []UnlockRule
	for _, r := range rules {
		e, ok := u.Exit(r.ExitID)
		if !ok {
			return invalid("没有这个出口")
		}
		if e.Kind == ExitNode && e.NodeID == nodeID {
			return invalid("出口 %s 就是这台节点自己", e.Name)
		}
		r.Domains = cleanDomains(r.Domains)
		var services []string
		for _, key := range r.Services {
			svc, ok := unlockService(key)
			if !ok {
				return invalid("未知的服务 %q", key)
			}
			if prev, dup := owner["svc:"+key]; dup && prev != e.Name {
				return invalid("%s 同时选了 %s 和 %s 两个出口", svc.Name, prev, e.Name)
			}
			owner["svc:"+key] = e.Name
			services = append(services, key)
		}
		for _, d := range r.Domains {
			if prev, dup := owner[d]; dup && prev != e.Name {
				return invalid("%s 同时选了 %s 和 %s 两个出口", d, prev, e.Name)
			}
			owner[d] = e.Name
		}
		r.Services = services
		if len(r.Services) == 0 && len(r.Domains) == 0 {
			continue
		}
		kept = append(kept, r)
	}
	if u.Nodes == nil {
		u.Nodes = map[int64][]UnlockRule{}
	}
	if len(kept) == 0 {
		delete(u.Nodes, nodeID)
	} else {
		u.Nodes[nodeID] = kept
	}
	if err := s.saveUnlock(u); err != nil {
		return err
	}
	s.notify.ConfigChanged(nodeID)
	return nil
}

// unlockPlan is what one node's configuration gets from the unlock routing.
type unlockPlan struct {
	// outbounds and dns are what the rules send to.
	outbounds []singbox.Outbound
	dns       *singbox.ServerDNS
	// rules go after the policy's: a site the policy blocks stays blocked,
	// whichever exit it would have gone through.
	rules []singbox.ServerRouteRule
	// domains is every domain the rules match, which is what Xray's inbounds
	// hand to sing-box (see bridge).
	domains []string

	// exitInbounds are the listeners this node serves as an ExitNode for
	// others. Exempt from the rules, like relays: what arrives on one has
	// been routed already, and routing it again could send it straight back.
	exitInbounds []singbox.Inbound
}

func (p unlockPlan) empty() bool { return len(p.rules) == 0 }

// ReservedTagPrefix starts the tags of the listeners the panel adds to a
// node's configuration itself: an exit's, and the Xray bridge. An inbound
// may not take one.
const ReservedTagPrefix = "veyra-"

// Tags the unlock routing gives what it adds to a node's configuration.
func unlockOutboundTag(exitID int64) string { return fmt.Sprintf("unlock-%d", exitID) }
func unlockDNSTag(exitID int64) string      { return fmt.Sprintf("unlock-dns-%d", exitID) }
func exitInboundTag(exitID int64) string    { return fmt.Sprintf("veyra-exit-%d", exitID) }

const (
	// xrayBridgeTag is the loopback SOCKS listener Xray's inbounds hand the
	// unlock routing's sites to, so sing-box routes them as it does its own.
	xrayBridgeTag = "veyra-xray-unlock"
	// xrayUnlockOutbound is Xray's side of the same bridge.
	xrayUnlockOutbound = "veyra-unlock"
	// exitUser names the one user of an ExitNode listener.
	exitUser = "veyra-exit"
	// localDNS is the system resolver, which every lookup an unlock rule does
	// not claim keeps using.
	localDNS = "local"
)

// unlockFor builds node's unlock routing. A node that is off serves no exits
// and needs no routing; an exit whose node is gone or off is skipped, so its
// sites go straight out rather than nowhere.
func (s *Service) unlockFor(node *store.Node) (unlockPlan, error) {
	var plan unlockPlan
	if !node.Enabled {
		return plan, nil
	}
	u, err := s.Unlock()
	if err != nil {
		return plan, err
	}
	nodes, err := s.st.Nodes()
	if err != nil {
		return plan, err
	}
	byID := make(map[int64]*store.Node, len(nodes))
	for _, n := range nodes {
		byID[n.ID] = n
	}

	for _, e := range u.Exits {
		if e.Kind != ExitNode || e.NodeID != node.ID {
			continue
		}
		plan.exitInbounds = append(plan.exitInbounds, singbox.Inbound{
			Type:       store.ProtoShadowsocks,
			Tag:        exitInboundTag(e.ID),
			Listen:     "::",
			ListenPort: e.Port,
			Method:     SSMethod,
			Password:   e.ServerKey,
			Network:    []string{"tcp", "udp"},
			Users:      []singbox.User{{Name: exitUser, Password: e.UserKey}},
		})
	}

	seen := map[int64]bool{}
	for _, r := range u.Nodes[node.ID] {
		e, ok := u.Exit(r.ExitID)
		if !ok {
			continue
		}
		var domains []string
		for _, key := range r.Services {
			if svc, ok := unlockService(key); ok {
				domains = append(domains, svc.Domains...)
			}
		}
		domains = append(domains, r.Domains...)
		if len(domains) == 0 {
			continue
		}

		switch e.Kind {
		case ExitDNS:
			if plan.dns == nil {
				plan.dns = &singbox.ServerDNS{
					Servers: []singbox.ServerDNSServer{{Type: localDNS, Tag: localDNS}},
					Final:   localDNS,
				}
			}
			if !seen[e.ID] {
				plan.dns.Servers = append(plan.dns.Servers, singbox.ServerDNSServer{
					Type: "udp", Tag: unlockDNSTag(e.ID), Server: e.Server, ServerPort: e.Port,
				})
			}
			plan.rules = append(plan.rules, singbox.ServerRouteRule{
				DomainSuffix: domains, Action: "resolve", Server: unlockDNSTag(e.ID),
			})
		default:
			out, ok := exitOutbound(e, byID)
			if !ok {
				continue
			}
			if !seen[e.ID] {
				plan.outbounds = append(plan.outbounds, out)
			}
			plan.rules = append(plan.rules, singbox.ServerRouteRule{
				DomainSuffix: domains, Action: "route", Outbound: out.Tag,
			})
		}
		seen[e.ID] = true
		plan.domains = append(plan.domains, domains...)
	}
	return plan, nil
}

// exitOutbound is the outbound that reaches a proxy exit. ok is false for an
// ExitNode whose node is gone, off, or has no address.
func exitOutbound(e Exit, nodes map[int64]*store.Node) (singbox.Outbound, bool) {
	out := singbox.Outbound{Tag: unlockOutboundTag(e.ID), Server: e.Server, ServerPort: e.Port}
	switch e.Kind {
	case ExitSOCKS:
		out.Type, out.Version = "socks", "5"
		out.Username, out.Password = e.Username, e.Password
	case ExitShadowsocks:
		out.Type, out.Method, out.Password = "shadowsocks", e.Method, e.Password
	case ExitVLESS:
		out.Type, out.UUID, out.Flow = "vless", e.UUID, e.Flow
		switch e.Security {
		case "reality":
			out.TLS = &singbox.OutboundTLS{
				Enabled: true, ServerName: e.SNI,
				UTLS:    &singbox.UTLS{Enabled: true, Fingerprint: "chrome"},
				Reality: &singbox.OutboundReality{Enabled: true, PublicKey: e.PublicKey, ShortID: e.ShortID},
			}
		case "tls":
			out.TLS = &singbox.OutboundTLS{Enabled: true, ServerName: e.SNI}
		}
	case ExitNode:
		n, ok := nodes[e.NodeID]
		if !ok || !n.Enabled || strings.TrimSpace(n.Address) == "" {
			return singbox.Outbound{}, false
		}
		out.Type, out.Method = "shadowsocks", SSMethod
		out.Server = strings.TrimSpace(n.Address)
		// Multi-user Shadowsocks 2022: the server's key, then the user's.
		out.Password = e.ServerKey + ":" + e.UserKey
	default:
		return singbox.Outbound{}, false
	}
	return out, true
}

// xrayBridgePort picks the loopback port for the bridge: the first one from a
// fixed start that nothing else on the node uses, so that the same node gets
// the same port on every push and an unchanged configuration stays unchanged.
func xrayBridgePort(taken map[int]bool) int {
	p := 39100
	for taken[p] {
		p++
	}
	return p
}

// xrayUnlockPart is what a node's Xray configuration gets from the unlock
// routing: the outbound to the bridge and the rule that sends the plan's
// domains there.
type xrayUnlockPart struct {
	Outbounds []json.RawMessage `json:"outbounds,omitempty"`
	Unlock    []xrayUnlockRule  `json:"unlock,omitempty"`
}

type xrayUnlockRule struct {
	DomainSuffix []string `json:"domain_suffix"`
	Outbound     string   `json:"outbound"`
}

func xrayBridgeOutbound(port int) json.RawMessage {
	b, _ := json.Marshal(map[string]any{
		"tag":      xrayUnlockOutbound,
		"protocol": "socks",
		"settings": map[string]any{
			"servers": []map[string]any{{"address": "127.0.0.1", "port": port}},
		},
	})
	return b
}

// dedupe keeps the first of each string, in order.
func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := in[:0:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
