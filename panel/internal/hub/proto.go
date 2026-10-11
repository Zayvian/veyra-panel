// Package hub owns the node control channel: one WebSocket per node, dialled by
// the node rather than by the panel.
//
// That direction is the whole design. A node needs no inbound control port, no
// certificate of its own for the control plane, and no route from the panel —
// so it works behind NAT, and the panel never has to know how to reach it. The
// node's own address matters only to clients, and only for subscriptions.
package hub

import (
	"encoding/json"

	"github.com/Zayvian/veyra-panel/internal/singbox"
)

// Message types. Three commands travel down; the rest are the node reporting.
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
	TypeWelcome = "welcome"
	TypeConfig  = "config"
	TypeUsers   = "users"
	TypePing    = "ping"
)

// Protocol 2 adds capability negotiation. A node that says proto >= 2 in its
// hello is answered with a welcome naming what this panel supports, before
// anything else; a node that says nothing is an older one and is spoken to
// exactly as before. Each capability is used only when both sides named it.
const (
	ProtoVersion = 2

	// CapStatsAck: stats carry an epoch and a sequence number (the envelope
	// id), the panel answers ok{id} once the report is committed, and a report
	// the panel has already committed is acknowledged without being billed
	// again. See protocol/PROTOCOL.md.
	CapStatsAck = "stats_ack"
	// CapConfigRev: config carries rev, and state reports the rev the node is
	// actually running.
	CapConfigRev = "config_rev"
)

// PanelCaps is what this panel offers in its welcome.
var PanelCaps = []string{CapStatsAck, CapConfigRev}

// Welcome answers a protocol-2 hello.
type Welcome struct {
	Proto int      `json:"proto"`
	Caps  []string `json:"caps"`
}

// Envelope wraps every frame. ID correlates a command with its reply and is
// zero for one-way reports.
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

	// Proto and Caps are absent from nodes older than protocol 2.
	Proto int      `json:"proto,omitempty"`
	Caps  []string `json:"caps,omitempty"`
}

// ErrorData explains a rejected command. A node that cannot apply a config —
// a certificate path that does not exist, a port already bound — has to say so
// rather than fail silently, or the panel will keep showing a configuration the
// node never adopted.
type ErrorData struct {
	Message string `json:"msg"`
}

// ConfigData carries the sing-box configuration. Inbound user lists are empty;
// see UsersData.
type ConfigData struct {
	// Rev identifies this configuration (see service.ConfigRev). A node with
	// config_rev reports back the rev it is running, which is how the panel
	// tells a configuration that took effect from one still pending or refused.
	Rev    string          `json:"rev,omitempty"`
	Config *singbox.Config `json:"config"`
}

// UsersData is the authoritative user list keyed by inbound tag. It replaces
// whatever the node has — it is not a delta — so a node that missed a message
// converges as soon as it receives the next one.
//
// StatsUsers is the flat list of names for the node's v2ray_api allowlist.
// Sending it alongside is deliberate: that allowlist is built when a config
// loads, so a hot-added user missing from it relays traffic that is never
// counted, with nothing in any log to say so.
type UsersData struct {
	ByTag      map[string][]singbox.User `json:"by_tag"`
	StatsUsers []string                  `json:"stats_users"`

	// IPLimits is how many distinct source addresses each user may have
	// connected at once, by name. Absent or zero means no limit.
	//
	// Sent with the users rather than separately: this is the message that
	// changes when a user is added, edited or removed, and a second channel
	// for the limits would let the two drift.
	IPLimits map[string]int `json:"ip_limits,omitempty"`
}

// StatsData reports traffic since the previous report, not cumulative totals.
// A node restart zeroes its own counters; deltas make that a gap rather than a
// total that jumps backwards.
type StatsData struct {
	// Epoch names the node process that sent the report, and the envelope id
	// is its sequence number within that process. Both are set only under
	// stats_ack; see hub.applyStats.
	Epoch  string           `json:"epoch,omitempty"`
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

// OnlineData lists users with at least one live connection right now, and how
// many distinct source addresses each of them is connected from.
type OnlineData struct {
	Users []string       `json:"users"`
	IPs   map[string]int `json:"ips,omitempty"`

	// Activity is the shape of what each user is doing — connections, distinct
	// destinations, distinct ports. Counts only; the node does not report where
	// anyone went and the panel does not store it.
	Activity map[string]Activity `json:"activity,omitempty"`
}

type Activity struct {
	Conns int `json:"conns"`
	Peers int `json:"peers"`
	Ports int `json:"ports"`
}

// StateData is the node's own account of what it is serving, sent after every
// attempt to apply a configuration.
//
// An error reply says the node refused a configuration; this says what it is
// running instead. Both are needed. A node that rejects a configuration keeps
// serving the previous one, so without this the panel would show an inbound as
// enabled — the operator asked for it, after all — while the node has never
// heard of it, and the only symptom would be users unable to connect to that
// one port.
type StateData struct {
	Inbounds []string `json:"inbounds"`
	Error    string   `json:"error,omitempty"`
	// AppliedRev is the rev of the configuration the node is running, under
	// config_rev. Empty before the first configuration has been applied.
	AppliedRev string `json:"applied_rev,omitempty"`
}

func encode(t string, id uint64, data any) ([]byte, error) {
	env := Envelope{Type: t, ID: id}
	if data != nil {
		raw, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		env.Data = raw
	}
	return json.Marshal(env)
}
