// Package singbox describes sing-box's configuration as Go structs.
//
// It deliberately contains nothing but type definitions and marshalling. In
// particular it must never import github.com/sagernet/sing-box: that package is
// GPL-3.0, and linking it would make this panel a derivative work and erase the
// licence boundary the two repositories are built around. The node is where
// sing-box actually runs; here we only describe what to send it.
//
// Only the subset the panel emits is modelled — three inbound protocols and the
// two API services the node needs for accounting. Anything else a node might
// support is out of scope by design.
package singbox

import "encoding/json"

type Config struct {
	Log          *Log          `json:"log,omitempty"`
	Inbounds     []Inbound     `json:"inbounds"`
	Outbounds    []Outbound    `json:"outbounds"`
	DNS          *ServerDNS    `json:"dns,omitempty"`
	Route        *ServerRoute  `json:"route,omitempty"`
	Experimental *Experimental `json:"experimental,omitempty"`
}

// ServerRoute is what the node does with a connection once it has been
// authenticated. Absent means everything goes out, which is what a node with no
// policy configured gets.
//
// Named apart from the client's Route in client.go on purpose: they serialise
// to the same key in two different documents and mean opposite things. One
// picks which outbound a client should use; this one decides what a server
// refuses to carry.
type ServerRoute struct {
	Rules []ServerRouteRule `json:"rules,omitempty"`

	// DefaultDomainResolver names the DNS server outbounds resolve their own
	// server addresses with. Set whenever DNS is: with servers configured,
	// sing-box wants to be told which one that is.
	DefaultDomainResolver string `json:"default_domain_resolver,omitempty"`
}

// ServerDNS is the node's resolver. Absent means the system's, which is what
// every node without a DNS unlock exit gets. Apart from the client's DNS for
// the same reason ServerRoute is apart from Route.
type ServerDNS struct {
	Servers []ServerDNSServer `json:"servers"`
	// Final is the server for every lookup no rule picks another for.
	Final string `json:"final,omitempty"`
}

// ServerDNSServer is one resolver. Type "local" is the system's; "udp" is a
// plain DNS server, which is what DNS unlock services give out.
type ServerDNSServer struct {
	Type       string `json:"type"`
	Tag        string `json:"tag"`
	Server     string `json:"server,omitempty"`
	ServerPort int    `json:"server_port,omitempty"`
}

// ServerRouteRule matches a connection and says what to do with it. The fields
// are a subset of sing-box's: the ones a policy expressed in the panel can
// produce.
//
// An empty matcher matches everything, which is how the sniffing rule is
// written — it has to run before any rule that matches on what was sniffed.
type ServerRouteRule struct {
	// Inbound matches on the tag a connection arrived through. Used to take
	// relay listeners out of the policy rules below: what passes through one is
	// an already-encrypted proxy stream, so sniffing it finds the tunnel's own
	// handshake rather than what the client is really doing.
	Inbound []string `json:"inbound,omitempty"`

	Protocol      []string `json:"protocol,omitempty"`
	Domain        []string `json:"domain,omitempty"`
	DomainSuffix  []string `json:"domain_suffix,omitempty"`
	DomainKeyword []string `json:"domain_keyword,omitempty"`
	Port          []int    `json:"port,omitempty"`
	Network       []string `json:"network,omitempty"`

	Action  string   `json:"action"`
	Sniffer []string `json:"sniffer,omitempty"`
	Timeout string   `json:"timeout,omitempty"`

	// Outbound is where action "route" sends the connection.
	Outbound string `json:"outbound,omitempty"`

	// Server is the DNS server action "resolve" looks the destination up
	// with. The connection carries on to the rules below with the address it
	// got, which is how a DNS unlock exit works.
	Server string `json:"server,omitempty"`
}

type Log struct {
	Level     string `json:"level,omitempty"`
	Timestamp bool   `json:"timestamp,omitempty"`
}

type Inbound struct {
	Type              string `json:"type"`
	Tag               string `json:"tag"`
	Listen            string `json:"listen"`
	ListenPort        int    `json:"listen_port"`
	HopPorts          string `json:"skysbx_hop_ports,omitempty"`
	CongestionControl string `json:"congestion_control,omitempty"`

	// Users is empty in a stored inbound and in the config message. The node
	// merges the authoritative list from a separate `users` message, which is
	// what makes a user change a hot swap rather than a listener rebuild.
	Users []User `json:"users,omitempty"`

	TLS *TLS `json:"tls,omitempty"`

	// Shadowsocks only. Password here is the *server* half of the SS2022 key
	// pair; each user carries their own half.
	Method   string   `json:"method,omitempty"`
	Password string   `json:"password,omitempty"`
	Network  []string `json:"network,omitempty"`

	// Type "direct" only: where this listener forwards to. Together they make a
	// plain layer-4 forwarder — the node copies bytes to another host and reads
	// none of them. This is how one node relays another's inbound.
	OverrideAddress string `json:"override_address,omitempty"`
	OverridePort    int    `json:"override_port,omitempty"`

	// Hysteria2 only.
	//
	// Obfs wraps every packet so the flow no longer looks like QUIC, for
	// networks that throttle or block QUIC on sight. Clients must use the same
	// password, so it is part of the subscription.
	Obfs *Obfs `json:"obfs,omitempty"`
	// Masquerade is what the port answers to anyone who is not a client — a
	// browser, or someone probing — instead of an error that names a proxy.
	// Server-side only: clients never see it.
	Masquerade *Masquerade `json:"masquerade,omitempty"`

	// XHTTP is not sing-box's: it marks a VLESS inbound served by Xray over
	// its XHTTP transport, which sing-box does not have. Such an inbound is
	// stored in this shape like every other and never reaches sing-box — it
	// is translated into Xray's format when the node's configuration is built.
	XHTTP *XHTTP `json:"veyra_xhttp,omitempty"`
}

// XHTTP is Xray's HTTP-based transport: requests on a path, with the upload
// and download carried as the mode says.
type XHTTP struct {
	Path string `json:"path"`
	Mode string `json:"mode"` // always "auto": the client picks
}

// Obfs is Hysteria2's salamander obfuscation.
type Obfs struct {
	Type     string `json:"type"` // always "salamander"
	Password string `json:"password"`
}

// Masquerade proxies non-client requests to a real website.
type Masquerade struct {
	Type        string `json:"type"` // always "proxy"
	URL         string `json:"url"`
	RewriteHost bool   `json:"rewrite_host,omitempty"`
}

// User covers all three protocols. Name is the billing identity the node
// reports traffic against and is the same string everywhere for a given user.
type User struct {
	Name string `json:"name"`

	UUID string `json:"uuid,omitempty"` // VLESS
	Flow string `json:"flow,omitempty"` // VLESS, xtls-rprx-vision

	Password string `json:"password,omitempty"` // AnyTLS, Shadowsocks
}

type TLS struct {
	Enabled    bool     `json:"enabled"`
	ServerName string   `json:"server_name,omitempty"`
	ALPN       []string `json:"alpn,omitempty"`

	// sing-box reads these files once, at start. Rotating a certificate
	// therefore requires restarting the listener, not just replacing the file.
	CertificatePath string `json:"certificate_path,omitempty"`
	KeyPath         string `json:"key_path,omitempty"`

	Reality *Reality `json:"reality,omitempty"`
}

type Reality struct {
	Enabled   bool      `json:"enabled"`
	Handshake Handshake `json:"handshake"`

	// PrivateKey is base64url *without* padding: sing-box decodes it with
	// base64.RawURLEncoding, and a trailing '=' fails outright.
	PrivateKey string   `json:"private_key"`
	ShortID    []string `json:"short_id"`
}

type Handshake struct {
	Server     string `json:"server"`
	ServerPort int    `json:"server_port"`
}

// Outbound is where the node sends a connection: "direct", or one of the
// proxies a node's unlock routing sends chosen sites through.
type Outbound struct {
	Type string `json:"type"`
	Tag  string `json:"tag"`

	Server     string `json:"server,omitempty"`
	ServerPort int    `json:"server_port,omitempty"`

	// SOCKS.
	Version  string `json:"version,omitempty"`
	Username string `json:"username,omitempty"`
	// SOCKS and Shadowsocks.
	Password string `json:"password,omitempty"`
	// Shadowsocks.
	Method string `json:"method,omitempty"`
	// VLESS.
	UUID string       `json:"uuid,omitempty"`
	Flow string       `json:"flow,omitempty"`
	TLS  *OutboundTLS `json:"tls,omitempty"`
}

// OutboundTLS is the client half of TLS, with Reality.
type OutboundTLS struct {
	Enabled    bool             `json:"enabled"`
	ServerName string           `json:"server_name,omitempty"`
	UTLS       *UTLS            `json:"utls,omitempty"`
	Reality    *OutboundReality `json:"reality,omitempty"`
}

type OutboundReality struct {
	Enabled   bool   `json:"enabled"`
	PublicKey string `json:"public_key"`
	ShortID   string `json:"short_id,omitempty"`
}

type Experimental struct {
	ClashAPI *ClashAPI `json:"clash_api,omitempty"`
	V2RayAPI *V2RayAPI `json:"v2ray_api,omitempty"`
}

// ClashAPI backs online-user and connection queries on the node.
type ClashAPI struct {
	ExternalController string `json:"external_controller"`
}

// V2RayAPI backs per-user traffic counters.
type V2RayAPI struct {
	Listen string `json:"listen"`
	Stats  Stats  `json:"stats"`
}

type Stats struct {
	Enabled bool     `json:"enabled"`
	Users   []string `json:"users"`
}

func (c *Config) JSON() ([]byte, error) { return json.MarshalIndent(c, "", "  ") }
