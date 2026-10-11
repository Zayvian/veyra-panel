// Package proto describes the control channel between a node and its panel.
//
// These types are defined here rather than imported from the panel, and that is
// deliberate. The panel is a separate program under a separate licence; the
// contract between them is the wire format, not a shared Go package. Duplicating
// a hundred lines of struct definitions is the price of a boundary that cannot
// be eroded by an import someone adds later without thinking about it.
//
// The node dials the panel, not the other way round. A node therefore needs no
// inbound control port, no certificate of its own for the control plane, and no
// route from the panel — it works behind NAT, and the panel never has to know
// how to reach it.
package proto

import "encoding/json"

// Message types.
const (
	// node -> panel
	TypeHello  = "hello"
	TypeOK     = "ok"
	TypeError  = "error"
	TypeStats  = "stats"
	TypeOnline = "online"
	TypeState  = "state"
	TypePong   = "pong"

	// panel -> node
	TypeConfig = "config"
	TypeUsers  = "users"
	TypePing   = "ping"
)

// Envelope wraps every frame. ID correlates a command with its reply and is
// zero on one-way reports.
type Envelope struct {
	Type string          `json:"t"`
	ID   uint64          `json:"id,omitempty"`
	Data json.RawMessage `json:"d,omitempty"`
}

type Hello struct {
	Version        string `json:"version"`
	OS             string `json:"os"`
	Arch           string `json:"arch"`
	Hostname       string `json:"hostname"`
	SingboxVersion string `json:"singbox_version"`
}

// ErrorData explains a command the node could not carry out. Reporting it
// matters: without it the panel goes on displaying a configuration the node
// never adopted, and the discrepancy surfaces only as users unable to connect.
type ErrorData struct {
	Message string `json:"msg"`
}

// StateData is what the node is actually serving, sent after every attempt to
// apply a configuration.
//
// The error reply to a config command says the node refused it; this says what
// it is running instead. The panel needs both: an operator who mistypes a port
// sees the inbound listed and enabled in the panel while the node quietly runs
// the previous configuration, and nothing short of the node's own account of
// its live inbounds makes that visible.
//
// Inbounds is authoritative rather than derived. Parsing the tag back out of an
// error message would be a guess, and would be wrong the moment sing-box
// rewords it.
type StateData struct {
	Inbounds []string `json:"inbounds"`
	Error    string   `json:"error,omitempty"`
}

// ConfigData carries a full sing-box configuration. Inbound user lists are
// empty; users arrive separately so that changing them is a hot swap rather
// than a listener rebuild.
type ConfigData struct {
	Config json.RawMessage `json:"config"`
}

// UsersData is the authoritative user list per inbound tag. It replaces
// whatever the node holds rather than amending it, so a node that missed a
// message converges on the next one.
type UsersData struct {
	ByTag map[string][]User `json:"by_tag"`

	// StatsUsers is the allowlist for sing-box's v2ray_api statistics service.
	// That service builds its set when a configuration loads and counts nothing
	// for a name outside it — so a user hot-added without refreshing this
	// relays traffic that is billed to nobody, silently.
	StatsUsers []string `json:"stats_users"`

	// IPLimits is how many distinct source addresses each user may have
	// connected at once, by name. Absent or zero means no limit.
	//
	// Enforced here rather than in the panel because this is where the
	// connections are: the panel hears about them up to thirty seconds late,
	// and could only respond by revoking the whole account.
	IPLimits map[string]int `json:"ip_limits,omitempty"`
}

// User carries whichever credential its protocol authenticates with.
type User struct {
	Name string `json:"name"`

	UUID string `json:"uuid,omitempty"` // VLESS
	Flow string `json:"flow,omitempty"` // VLESS

	Password string `json:"password,omitempty"` // AnyTLS, Shadowsocks
}

// StatsData reports traffic accumulated since the previous report.
//
// Deltas, not totals. A node restart zeroes its own counters, and a cumulative
// figure would make every user's usage appear to jump backwards — which the
// panel cannot tell apart from a deliberate reset.
type StatsData struct {
	Users  map[string]Usage `json:"users"`
	System *SystemStats     `json:"system,omitempty"`
}

type Usage struct {
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
}

type SystemStats struct {
	CPU      float64 `json:"cpu"`
	MemUsed  int64   `json:"mem_used"`
	MemTotal int64   `json:"mem_total"`
	Uptime   int64   `json:"uptime"`
}

// OnlineData lists users with at least one live connection.
type OnlineData struct {
	Users []string `json:"users"`

	// IPs is how many distinct source addresses each of those users currently
	// has connected. The panel shows it next to the limit, so that "3 / 2"
	// says both that a limit is being enforced and that someone is testing it.
	IPs map[string]int `json:"ips,omitempty"`

	// Activity is the shape of what each user is doing, for the panel to keep
	// and show. Counts only — no destinations.
	Activity map[string]Activity `json:"activity,omitempty"`
}

// Activity describes how an account is being used, without describing what it
// is being used for.
//
// Shape rather than destinations, deliberately. What distinguishes BitTorrent,
// a bulk download and a running speed test from ordinary browsing is the shape:
// how many connections at once, to how many different peers, across how many
// ports. Recording where someone browses would answer a question nobody asked
// and build a history that did not exist before.
// There is no "this was BitTorrent" counter, and not for want of trying: the
// clash API exposes the inbound type, not the sniffed protocol, and a
// connection the BitTorrent rule rejected is closed before any sample could
// see it. The shape is the signal, and it is the better one anyway — it does
// not care whether the payload was encrypted.
type Activity struct {
	Conns int `json:"conns"` // connections open at the moment of the sample
	Peers int `json:"peers"` // distinct destination addresses
	Ports int `json:"ports"` // distinct destination ports
}

// Encode builds a frame.
func Encode(msgType string, id uint64, data any) ([]byte, error) {
	env := Envelope{Type: msgType, ID: id}
	if data != nil {
		raw, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		env.Data = raw
	}
	return json.Marshal(env)
}
