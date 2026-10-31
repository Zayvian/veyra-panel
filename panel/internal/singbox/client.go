package singbox

// Client-side configuration. The types above describe what a node runs; these
// describe what a client runs. Both are just JSON shapes — nothing here imports
// sing-box, for the licence reasons in the package comment.

type ClientConfig struct {
	Log       *Log             `json:"log,omitempty"`
	DNS       *DNS             `json:"dns,omitempty"`
	Inbounds  []ClientInbound  `json:"inbounds"`
	Outbounds []ClientOutbound `json:"outbounds"`
	Route     *Route           `json:"route,omitempty"`
	// Experimental holds the cache file, which keeps downloaded rule sets
	// across restarts so a client does not refetch them on every launch.
	Experimental *ClientExperimental `json:"experimental,omitempty"`
}

type ClientExperimental struct {
	CacheFile *CacheFile `json:"cache_file,omitempty"`
}

type CacheFile struct {
	Enabled bool `json:"enabled"`
}

// ClientInbound is the local listener the client exposes to the machine it runs
// on. "mixed" speaks both SOCKS5 and HTTP, which covers every consumer.
type ClientInbound struct {
	Type       string `json:"type"`
	Tag        string `json:"tag"`
	Listen     string `json:"listen"`
	ListenPort int    `json:"listen_port"`
}

type ClientOutbound struct {
	Type string `json:"type"`
	Tag  string `json:"tag"`

	Server            string   `json:"server,omitempty"`
	ServerPort        int      `json:"server_port,omitempty"`
	ServerPorts       []string `json:"server_ports,omitempty"`
	HopInterval       string   `json:"hop_interval,omitempty"`
	CongestionControl string   `json:"congestion_control,omitempty"`
	Obfs              *Obfs    `json:"obfs,omitempty"` // Hysteria2

	UUID string `json:"uuid,omitempty"` // VLESS
	Flow string `json:"flow,omitempty"` // VLESS

	Password string `json:"password,omitempty"` // AnyTLS, Shadowsocks
	Method   string `json:"method,omitempty"`   // Shadowsocks

	TLS *ClientTLS `json:"tls,omitempty"`

	Transport *ClientTransport `json:"transport,omitempty"` // VLESS over a CDN

	// selector / urltest
	Outbounds []string `json:"outbounds,omitempty"`
	Default   string   `json:"default,omitempty"`
	URL       string   `json:"url,omitempty"`
	Interval  string   `json:"interval,omitempty"`
}

type ClientTLS struct {
	ALPN       []string       `json:"alpn,omitempty"`
	Enabled    bool           `json:"enabled"`
	ServerName string         `json:"server_name,omitempty"`
	UTLS       *UTLS          `json:"utls,omitempty"`
	Reality    *ClientReality `json:"reality,omitempty"`
}

// ClientTransport is the client half of a WebSocket transport. Host is the
// HTTP Host header: with a CDN in front, the address dialled is often one of
// the CDN's own IPs, and Host is then the only thing that says which site the
// request is for.
type ClientTransport struct {
	Type                string            `json:"type"`
	Path                string            `json:"path,omitempty"`
	Headers             map[string]string `json:"headers,omitempty"`
	MaxEarlyData        int               `json:"max_early_data,omitempty"`
	EarlyDataHeaderName string            `json:"early_data_header_name,omitempty"`
}

// UTLS makes the client's TLS handshake look like a browser's. Reality needs it
// on: the handshake is what the whole protocol hides behind.
type UTLS struct {
	Enabled     bool   `json:"enabled"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

type ClientReality struct {
	Enabled   bool   `json:"enabled"`
	PublicKey string `json:"public_key"`
	ShortID   string `json:"short_id,omitempty"`
}

type DNS struct {
	Servers []DNSServer `json:"servers"`
	Rules   []DNSRule   `json:"rules,omitempty"`
	Final   string      `json:"final,omitempty"`
}

// DNSServer is the post-1.12 form. The older shape — a single "address" string
// like "https://1.1.1.1/dns-query" — was removed in sing-box 1.14, and a config
// carrying it is rejected outright rather than warned about.
type DNSServer struct {
	Type   string `json:"type"`
	Tag    string `json:"tag"`
	Server string `json:"server"`
	Detour string `json:"detour,omitempty"`
}

type DNSRule struct {
	Domain  []string `json:"domain,omitempty"`
	RuleSet []string `json:"rule_set,omitempty"`
	Server  string   `json:"server"`
}

type Route struct {
	Rules   []RouteRule `json:"rules,omitempty"`
	RuleSet []RuleSet   `json:"rule_set,omitempty"`
	Final   string      `json:"final,omitempty"`
	Auto    bool        `json:"auto_detect_interface,omitempty"`
	// Required since sing-box 1.14: a config without it does not start, it
	// refuses with a migration notice.
	DefaultDomainResolver *DomainResolver `json:"default_domain_resolver,omitempty"`
}

type DomainResolver struct {
	Server string `json:"server"`
}

// RouteRule matches a connection. A rule with an Action other than the default
// "route" (sniff, resolve) carries no outbound and matching carries on past it.
type RouteRule struct {
	Protocol string   `json:"protocol,omitempty"`
	IPIsPriv bool     `json:"ip_is_private,omitempty"`
	RuleSet  []string `json:"rule_set,omitempty"`
	Inbound  []string `json:"inbound,omitempty"`
	Action   string   `json:"action,omitempty"`
	Outbound string   `json:"outbound,omitempty"`
}

// RuleSet is a remote binary rule set, fetched by the client and cached.
type RuleSet struct {
	Type           string `json:"type"` // always "remote"
	Tag            string `json:"tag"`
	Format         string `json:"format"` // always "binary"
	URL            string `json:"url"`
	DownloadDetour string `json:"download_detour,omitempty"`
	UpdateInterval string `json:"update_interval,omitempty"`
}
