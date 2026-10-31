// Package link maintains the control channel to the panel.
//
// The node dials out and keeps one WebSocket open. Reconnection is this
// package's business alone: nothing above it needs to know the channel dropped,
// because the panel re-sends the full configuration and user list on every
// hello. There is no resume protocol and no state to reconcile.
package link

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/Zayvian/veyra-node/internal/proto"
)

// Reporting intervals. Variables rather than constants so tests can shorten
// them; nothing in the running program writes to them.
var (
	statsEvery  = 30 * time.Second
	onlineEvery = 30 * time.Second
	// Address limits are checked far more often than anything is reported: this
	// is what stands between one subscription and fifty strangers, and every
	// second it is late is a second they are online.
	limitEvery = 5 * time.Second
)

const (
	minBackoff = 1 * time.Second
	maxBackoff = 60 * time.Second

	// The panel pings every 30s. Three missed pings is a dead channel.
	readTimeout = 100 * time.Second

	maxFrame = 1 << 20
)

// Engine is the data plane, as the control channel needs to see it.
//
// Keeping it an interface is what lets the channel be tested against a fake
// panel without sing-box in the picture, and the data plane be tested without a
// network.
type Engine interface {
	// ApplyConfig replaces the running configuration. It rebuilds listeners and
	// therefore drops live connections, which is why users travel separately.
	ApplyConfig(ctx context.Context, config json.RawMessage) error

	// UpdateUsers swaps the user set on running inbounds without restarting
	// them, and refreshes the statistics allowlist.
	UpdateUsers(ctx context.Context, users proto.UsersData) error

	// Stats returns traffic since the last acknowledged report. It does not
	// advance the baseline; StatsReported does, once the report has actually
	// been written to the channel.
	Stats() (map[string]proto.Usage, *proto.SystemStats, error)

	// StatsReported acknowledges the last Stats result. Traffic that is read
	// but never reported must not be discarded — it is traffic nobody is
	// billed for.
	StatsReported()

	// Online lists users with at least one live connection.
	Online() ([]string, error)

	// EnforceIPLimits closes connections from source addresses over a user's
	// cap and returns how many distinct addresses each user has left. It runs
	// here rather than in the panel because this is where the connections are.
	EnforceIPLimits(ctx context.Context) (map[string]int, map[string]proto.Activity, error)

	// State is what the node is serving right now, reported after every
	// attempt to apply a configuration so the panel can tell an inbound that
	// took effect from one that was rejected.
	State() proto.StateData

	// SingboxVersion is reported in the hello.
	SingboxVersion() string
}

type Config struct {
	// PanelURL is the panel's base URL, e.g. https://panel.example.com.
	PanelURL string
	Token    string
	Version  string

	// EnvFile is where SKYSBX_PANEL is kept (node.env), rewritten when the
	// panel moves to a new domain so the next start uses it too. Empty, or a
	// file that does not exist, means the move lasts until the process exits.
	EnvFile string

	Engine Engine
	Log    *slog.Logger
}

type Client struct {
	cfg Config
	log *slog.Logger

	// probe checks a new panel address before the node commits to it.
	// Replaced in tests, which cannot get a certificate for a real name.
	probe func(ctx context.Context, panelURL string) error

	mu sync.Mutex
	ws *websocket.Conn
	// Last per-user address counts, from the enforcement tick. Reported with
	// the online users rather than measured again there, so the two cannot
	// disagree about who is connected.
	ipCounts map[string]int
	// Last per-user activity sample, from the same tick. Counts only; see
	// proto.Activity for why no destination is kept.
	activity map[string]proto.Activity

	// stateMu guards what follows. Separate from mu, which is held across a
	// websocket write: the read loop and the report loop both consult this
	// state and then send, and must not do so while holding the write lock.
	stateMu sync.Mutex
	// caps is what this session's panel agreed to in its welcome. Nil until a
	// welcome arrives — which, from a panel older than protocol 2, it never
	// does — and everything nil means is today's behaviour.
	caps map[string]bool
	// epoch names this process to the panel, and seq numbers its reports
	// within it. Under stats_ack the panel remembers the last (epoch, seq) it
	// committed and bills a report it has already seen only once. The epoch is
	// per process rather than per connection: a report resent after a
	// reconnect must carry the numbers the panel may already have recorded.
	epoch string
	seq   uint64
	// outbox is the stats report waiting for the panel's ok. While it waits
	// no new reading is taken: the next report would overlap it, and if the
	// panel had in fact committed this one, the overlap would be billed twice.
	// It survives reconnects; that is the point of it.
	outbox *outgoing
	// appliedRev is the rev of the configuration the engine last accepted,
	// reported under config_rev.
	appliedRev string
	// panelURL and wsURL are where the panel is. They change only when the
	// panel says it has moved (panel_url).
	panelURL string
	wsURL    string
}

// outgoing is one stats report under stats_ack, kept exactly as first sent.
type outgoing struct {
	seq  uint64
	data proto.StatsData
}

func (c *Client) has(capability string) bool {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c.caps[capability]
}

func (c *Client) lastSample() (map[string]int, map[string]proto.Activity) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var counts map[string]int
	if len(c.ipCounts) > 0 {
		counts = make(map[string]int, len(c.ipCounts))
		for k, v := range c.ipCounts {
			counts[k] = v
		}
	}
	var acts map[string]proto.Activity
	if len(c.activity) > 0 {
		acts = make(map[string]proto.Activity, len(c.activity))
		for k, v := range c.activity {
			acts[k] = v
		}
	}
	return counts, acts
}

func New(cfg Config) (*Client, error) {
	if cfg.PanelURL == "" {
		return nil, errors.New("panel URL is required")
	}
	if cfg.Token == "" {
		return nil, errors.New("node token is required")
	}
	if cfg.Engine == nil {
		return nil, errors.New("engine is required")
	}
	wsURL, err := controlURL(cfg.PanelURL)
	if err != nil {
		return nil, err
	}

	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	var epoch [8]byte
	if _, err := crand.Read(epoch[:]); err != nil {
		return nil, fmt.Errorf("stats epoch: %w", err)
	}
	return &Client{cfg: cfg, log: log, probe: probePanel,
		panelURL: cfg.PanelURL, wsURL: wsURL, epoch: hex.EncodeToString(epoch[:])}, nil
}

// controlURL turns a panel base URL into the control channel's address.
func controlURL(panelURL string) (string, error) {
	u, err := url.Parse(strings.TrimRight(panelURL, "/"))
	if err != nil {
		return "", fmt.Errorf("panel URL %q: %w", panelURL, err)
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		// The join token goes out in an Authorization header on every connect
		// and every reconnect. Over plain HTTP that is a long-lived credential
		// for this node, in the clear, on a schedule — and it is worth more than
		// the traffic it protects, because it lets the holder be this node.
		//
		// Allowed only against loopback, which is how the protocol is developed
		// and tested and where there is no network to read it off.
		if !isLoopback(u.Hostname()) {
			return "", fmt.Errorf(
				"panel URL %q is plain HTTP: the node token would be sent in the "+
					"clear on every reconnect. Use https://", panelURL)
		}
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("panel URL %q must be http or https", panelURL)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/v1/node/connect"

	return u.String(), nil
}

// isLoopback reports whether a host is this machine. Names as well as
// literals: "localhost" is what a developer types, and refusing it would make
// the check something to work around rather than something to keep.
func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// Run keeps the channel up until ctx is cancelled.
func (c *Client) Run(ctx context.Context) error {
	backoff := minBackoff
	for {
		err := c.session(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			c.log.Warn("control channel lost", "error", err, "retry_in", backoff)
		}

		// Jitter matters when a panel restarts: without it every node it serves
		// reconnects in the same instant, which is the moment the panel can
		// least afford a thundering herd.
		wait := backoff + rand.N(backoff/2+time.Millisecond)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func (c *Client) session(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	dialCtx, dialCancel := context.WithTimeout(ctx, 30*time.Second)
	defer dialCancel()

	c.stateMu.Lock()
	wsURL, panelURL := c.wsURL, c.panelURL
	c.stateMu.Unlock()
	ws, _, err := websocket.Dial(dialCtx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + c.cfg.Token}},
	})
	if err != nil {
		return fmt.Errorf("dial %s: %w", wsURL, err)
	}
	ws.SetReadLimit(maxFrame)
	defer ws.Close(websocket.StatusNormalClosure, "")

	c.mu.Lock()
	c.ws = ws
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.ws = nil
		c.mu.Unlock()
	}()

	// Every session negotiates afresh: the panel at the other end may have
	// been upgraded or rolled back since the last one.
	c.stateMu.Lock()
	c.caps = nil
	c.stateMu.Unlock()

	hostname, _ := os.Hostname()
	if err := c.send(ctx, proto.TypeHello, 0, proto.Hello{
		Version:        c.cfg.Version,
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
		Hostname:       hostname,
		SingboxVersion: c.cfg.Engine.SingboxVersion(),
		Proto:          proto.ProtoVersion,
		Caps:           proto.NodeCaps,
	}); err != nil {
		return fmt.Errorf("hello: %w", err)
	}
	c.log.Info("control channel up", "panel", panelURL)

	reportDone := make(chan struct{})
	go func() {
		defer close(reportDone)
		c.report(ctx)
	}()
	defer func() {
		cancel()
		<-reportDone
	}()

	for {
		readCtx, readCancel := context.WithTimeout(ctx, readTimeout)
		_, data, err := ws.Read(readCtx)
		readCancel()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		c.handle(ctx, data)
	}
}

// handle processes one frame. A command that fails is reported back rather than
// logged and forgotten: the panel is the only place an operator will look, and
// it has no other way to learn the node did not adopt what it sent.
func (c *Client) handle(ctx context.Context, data []byte) {
	var env proto.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		c.log.Warn("malformed frame", "error", err)
		return
	}

	switch env.Type {
	case proto.TypeWelcome:
		var w proto.Welcome
		if err := json.Unmarshal(env.Data, &w); err != nil {
			c.log.Warn("malformed welcome", "error", err)
			return
		}
		agreed := map[string]bool{}
		for _, offered := range w.Caps {
			for _, mine := range proto.NodeCaps {
				if offered == mine {
					agreed[mine] = true
				}
			}
		}
		c.stateMu.Lock()
		c.caps = agreed
		c.stateMu.Unlock()
		c.log.Info("panel protocol", "proto", w.Proto, "caps", w.Caps)

	case proto.TypeOK:
		// Under stats_ack the panel answers a stats report with its sequence
		// number once the traffic is committed. That, and not the write
		// succeeding, is when the report is done.
		c.stateMu.Lock()
		done := c.outbox != nil && c.outbox.seq == env.ID
		if done {
			c.outbox = nil
		}
		c.stateMu.Unlock()
		if done {
			c.cfg.Engine.StatsReported()
		}

	case proto.TypeConfig:
		var cd proto.ConfigData
		if err := json.Unmarshal(env.Data, &cd); err != nil {
			c.reply(ctx, env.ID, fmt.Errorf("config payload: %w", err))
			return
		}
		err := c.cfg.Engine.ApplyConfig(ctx, cd.Config)
		if err != nil {
			c.log.Error("apply config", "error", err)
		} else {
			c.log.Info("configuration applied")
			c.stateMu.Lock()
			c.appliedRev = cd.Rev
			c.stateMu.Unlock()
		}
		c.reply(ctx, env.ID, err)
		// Sent on success too. The panel has no other way to learn that a
		// rolled-back node is serving the previous inbounds, and no way to
		// learn that it has stopped doing so once the operator fixes it.
		st := c.cfg.Engine.State()
		if c.has(proto.CapConfigRev) {
			// On failure this is still the previous rev: the node keeps
			// serving the configuration it had, and says which one that is.
			c.stateMu.Lock()
			st.AppliedRev = c.appliedRev
			c.stateMu.Unlock()
		}
		_ = c.send(ctx, proto.TypeState, 0, st)

	case proto.TypeUsers:
		var ud proto.UsersData
		if err := json.Unmarshal(env.Data, &ud); err != nil {
			c.reply(ctx, env.ID, fmt.Errorf("users payload: %w", err))
			return
		}
		err := c.cfg.Engine.UpdateUsers(ctx, ud)
		if err != nil {
			c.log.Error("update users", "error", err)
		} else {
			c.log.Debug("users updated", "tags", len(ud.ByTag), "stats", len(ud.StatsUsers))
		}
		c.reply(ctx, env.ID, err)

	case proto.TypePanelURL:
		var pu proto.PanelURLData
		if err := json.Unmarshal(env.Data, &pu); err != nil {
			c.reply(ctx, env.ID, fmt.Errorf("panel_url payload: %w", err))
			return
		}
		moved, err := c.movePanel(ctx, pu.URL)
		if err != nil {
			c.log.Warn("panel moved, but the new address does not work yet; staying",
				"url", pu.URL, "error", err)
		}
		c.reply(ctx, env.ID, err)
		if moved {
			// The session still runs on the old name. Closing it makes Run
			// dial the new one, which is the only way to be sure it works.
			c.mu.Lock()
			ws := c.ws
			c.mu.Unlock()
			if ws != nil {
				ws.Close(websocket.StatusNormalClosure, "panel moved")
			}
		}

	case proto.TypeProbe:
		var pd proto.ProbeData
		if err := json.Unmarshal(env.Data, &pd); err != nil {
			c.reply(ctx, env.ID, fmt.Errorf("probe payload: %w", err))
			return
		}
		// Dialling takes seconds; the read loop must not wait for it, or pings
		// go unanswered and the panel drops the connection.
		go func(id uint64) {
			res := runProbe(ctx, pd)
			if err := c.send(ctx, proto.TypeProbeResult, id, res); err != nil {
				c.log.Debug("probe result", "error", err)
			}
		}(env.ID)

	case proto.TypePing:
		if err := c.send(ctx, proto.TypePong, env.ID, nil); err != nil {
			c.log.Debug("pong", "error", err)
		}

	default:
		c.log.Warn("unknown message type", "type", env.Type)
	}
}

func (c *Client) reply(ctx context.Context, id uint64, err error) {
	if err == nil {
		_ = c.send(ctx, proto.TypeOK, id, nil)
		return
	}
	_ = c.send(ctx, proto.TypeError, id, proto.ErrorData{Message: err.Error()})
}

// report pushes traffic and online users on a timer for as long as the session
// lasts.
func (c *Client) report(ctx context.Context) {
	statsTick := time.NewTicker(statsEvery)
	onlineTick := time.NewTicker(onlineEvery)
	limitTick := time.NewTicker(limitEvery)
	defer statsTick.Stop()
	defer onlineTick.Stop()
	defer limitTick.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-statsTick.C:
			if err := c.reportStats(ctx); err != nil {
				// The session is gone; Run will reconnect. The baseline is
				// deliberately not advanced, so this interval is carried into
				// the next report rather than lost.
				return
			}

		case <-onlineTick.C:
			names, err := c.cfg.Engine.Online()
			if err != nil {
				c.log.Debug("read online users", "error", err)
				continue
			}
			counts, acts := c.lastSample()
			if err := c.send(ctx, proto.TypeOnline, 0,
				proto.OnlineData{Users: names, IPs: counts, Activity: acts}); err != nil {
				return
			}

		case <-limitTick.C:
			// Far more often than the reports: this is the thing standing
			// between one subscription and fifty strangers, and every tick it
			// is late is a tick they are online.
			counts, acts, err := c.cfg.Engine.EnforceIPLimits(ctx)
			if err != nil {
				c.log.Debug("enforce address limits", "error", err)
				continue
			}
			c.mu.Lock()
			c.ipCounts, c.activity = counts, acts
			c.mu.Unlock()
		}
	}
}

// reportStats sends one stats report. An error means the channel is gone.
//
// Without stats_ack — an older panel — a report is done once it is written,
// as it always was. That loses the interval if the connection dies between the
// write and the panel's commit, but it never bills anything twice.
//
// With stats_ack the report is done when the panel says ok with its sequence
// number. Until then it is resent unchanged on every tick, across reconnects,
// and no new reading is taken; the panel commits each (epoch, seq) at most
// once, so a resend after a lost ok is acknowledged without being billed.
func (c *Client) reportStats(ctx context.Context) error {
	ack := c.has(proto.CapStatsAck)

	c.stateMu.Lock()
	out := c.outbox
	c.stateMu.Unlock()

	if out == nil {
		users, system, err := c.cfg.Engine.Stats()
		if err != nil {
			c.log.Warn("read stats", "error", err)
			return nil
		}
		if len(users) == 0 && system == nil {
			return nil
		}
		if !ack {
			if err := c.send(ctx, proto.TypeStats, 0,
				proto.StatsData{Users: users, System: system}); err != nil {
				return err
			}
			c.cfg.Engine.StatsReported()
			return nil
		}
		c.stateMu.Lock()
		c.seq++
		out = &outgoing{seq: c.seq,
			data: proto.StatsData{Epoch: c.epoch, Users: users, System: system}}
		c.outbox = out
		c.stateMu.Unlock()
	}

	if !ack {
		// A report left waiting by a session with a newer panel, and now an
		// older one: the panel was rolled back while a report was in flight.
		// The older panel cannot recognise a repeat, so if the newer one had
		// committed this report before going away it is counted twice. That
		// takes a rollback landing inside one report's round trip; dropping
		// the report instead would lose it in every other case, so it goes
		// out once more the old way and is done.
		data := out.data
		data.Epoch = ""
		if err := c.send(ctx, proto.TypeStats, 0, data); err != nil {
			return err
		}
		c.stateMu.Lock()
		if c.outbox == out {
			c.outbox = nil
		}
		c.stateMu.Unlock()
		c.cfg.Engine.StatsReported()
		return nil
	}
	return c.send(ctx, proto.TypeStats, out.seq, out.data)
}

func (c *Client) send(ctx context.Context, msgType string, id uint64, data any) error {
	frame, err := proto.Encode(msgType, id, data)
	if err != nil {
		return err
	}

	// One writer at a time: the read loop replies to commands while the report
	// goroutine pushes stats.
	c.mu.Lock()
	ws := c.ws
	defer c.mu.Unlock()
	if ws == nil {
		return errors.New("control channel is down")
	}

	writeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return ws.Write(writeCtx, websocket.MessageText, frame)
}
