// Package probe tells whether each node can still be reached from China.
//
// A blocked node looks healthy from everywhere the panel can see: its control
// channel is up, it reports traffic, its inbounds are live. What has changed is
// only that connections from China no longer arrive. So the panel asks nodes
// the operator marked as probes — machines in China — to try a TCP handshake
// with every other node's inbounds, tries the same targets itself from abroad,
// and reads the difference:
//
//	abroad   China    port 22 (China)   verdict
//	fails    —        —                 the node is down, nothing to do with China
//	ok       ok       —                 fine
//	ok       fails    ok / unknown      this port is blocked: change the port
//	ok       fails    fails             every TCP port on the IP is blocked: change
//	                                    the IP, or go through a CDN or a UDP protocol
//
// Port 22 is a control: it says nothing about the inbound, but if the panel
// can reach it and China cannot, the block is on the address, not the port.
// A node with two or more inbounds that all fail says the same without it.
//
// Only TCP inbounds dialled at the node's own address are probed. A UDP
// protocol cannot be checked with a TCP handshake, and a relayed or
// CDN-fronted inbound is reached through some other host — which is the
// point of having one when the node itself is blocked.
package probe

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/Zayvian/veyra-panel/internal/hub"
	"github.com/Zayvian/veyra-panel/internal/store"
)

const (
	every   = 5 * time.Minute
	timeout = 4 * time.Second

	// controlPort is dialled alongside each node's inbounds; see the package
	// comment.
	controlPort = 22

	// A bad verdict has to come up this many rounds in a row before it is
	// shown. One lost handshake is noise; ten minutes of them is a block.
	confirmRounds = 2
)

// Verdict is what a round of probes concluded about one inbound.
type Verdict string

const (
	VerdictOK      Verdict = "ok"
	VerdictPort    Verdict = "port"    // this port is blocked from China
	VerdictTCP     Verdict = "tcp"     // every TCP port on the address is blocked
	VerdictPartial Verdict = "partial" // some probes get through, some do not
	VerdictDown    Verdict = "down"    // not reachable from abroad either
)

// Label is the verdict as the UI shows it.
func (v Verdict) Label() string {
	switch v {
	case VerdictOK:
		return "国内可达"
	case VerdictPort:
		return "端口被封"
	case VerdictTCP:
		return "IP 的 TCP 被封"
	case VerdictPartial:
		return "部分探针不通"
	case VerdictDown:
		return "海外也连不上"
	}
	return ""
}

// Advice is what to do about it.
func (v Verdict) Advice() string {
	switch v {
	case VerdictPort:
		return "换一个端口通常就能恢复"
	case VerdictTCP:
		return "换端口没用：换 IP，或改用 CDN 入站（vless-ws）、Hysteria2 等 UDP 协议"
	case VerdictPartial:
		return "可能是个别运营商或地区在干扰，继续观察"
	case VerdictDown:
		return "节点本身或防火墙的问题，和墙无关"
	}
	return ""
}

// Class is the pill style for the verdict.
func (v Verdict) Class() string {
	switch v {
	case VerdictOK:
		return "ok"
	case VerdictPort, VerdictTCP:
		return "bad"
	case VerdictPartial, VerdictDown:
		return "warn-pill"
	}
	return "off"
}

// rank orders verdicts by how much they matter to an operator, for a node's
// summary of its inbounds.
func (v Verdict) rank() int {
	switch v {
	case VerdictTCP:
		return 5
	case VerdictPort:
		return 4
	case VerdictDown:
		return 3
	case VerdictPartial:
		return 2
	case VerdictOK:
		return 1
	}
	return 0
}

// Status is the confirmed verdict for one inbound.
type Status struct {
	Verdict Verdict
	Since   time.Time // when this verdict was first confirmed
	Checked time.Time // the last round that looked at it
	// Probes is how many probes answered for it in the last round.
	Probes int

	pending Verdict // a bad verdict seen but not yet confirmed
	streak  int
}

// Prober is the part of the hub this package needs.
type Prober interface {
	CanProbe(nodeID int64) bool
	Probe(ctx context.Context, nodeID int64, targets []hub.ProbeTarget, timeout time.Duration) ([]hub.ProbeResult, error)
}

// Monitor runs the rounds and keeps what they found. Results are held in
// memory only: after a restart the first round, a few minutes in, fills them
// again, and a verdict hours old would mislead more than it informs.
type Monitor struct {
	st     *store.Store
	prober Prober
	log    *slog.Logger
	// dial is the panel's own attempt, from abroad. Replaced in tests.
	dial func(ctx context.Context, addr string) error

	mu       sync.RWMutex
	inbounds map[int64]*Status
	lastRun  time.Time
	probes   int // probes that answered in the last round
}

func New(st *store.Store, prober Prober, log *slog.Logger) *Monitor {
	return &Monitor{st: st, prober: prober, log: log, dial: dialTCP,
		inbounds: map[int64]*Status{}}
}

func dialTCP(ctx context.Context, addr string) error {
	c, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	return c.Close()
}

// Run probes every few minutes until ctx ends. The first round waits a little,
// so probes that reconnect after a panel restart are back before it starts.
func (m *Monitor) Run(ctx context.Context) {
	wait := 45 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = every
		if err := m.Round(ctx); err != nil && !errors.Is(err, context.Canceled) {
			m.log.Warn("probe round", "error", err)
		}
	}
}

// target is one thing to dial and what it stands for.
type target struct {
	key     string
	nodeID  int64
	inbound int64 // zero for a node's control port
	addr    string
}

// Round runs one round of probes and updates the stored verdicts.
func (m *Monitor) Round(ctx context.Context) error {
	nodes, err := m.st.Nodes()
	if err != nil {
		return err
	}
	inbounds, err := m.st.Inbounds()
	if err != nil {
		return err
	}
	marked, err := m.st.ProbeNodes()
	if err != nil {
		return err
	}

	var probes []int64
	byID := map[int64]*store.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
		if marked[n.ID] && n.Enabled && m.prober.CanProbe(n.ID) {
			probes = append(probes, n.ID)
		}
	}

	targets := Targets(byID, marked, inbounds)
	if len(probes) == 0 || len(targets) == 0 {
		m.mu.Lock()
		m.inbounds = map[int64]*Status{}
		m.lastRun = time.Now()
		m.probes = 0
		m.mu.Unlock()
		return nil
	}

	abroad := m.dialAll(ctx, targets)

	wire := make([]hub.ProbeTarget, len(targets))
	for i, t := range targets {
		host, port, _ := net.SplitHostPort(t.addr)
		p, _ := strconv.Atoi(port)
		wire[i] = hub.ProbeTarget{Key: t.key, Host: host, Port: p}
	}
	china := map[string][]bool{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	answered := 0
	for _, id := range probes {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			pctx, cancel := context.WithTimeout(ctx, timeout+20*time.Second)
			defer cancel()
			res, err := m.prober.Probe(pctx, id, wire, timeout)
			if err != nil {
				m.log.Warn("probe failed", "probe", id, "error", err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			answered++
			for _, r := range res {
				china[r.Key] = append(china[r.Key], r.OK)
			}
		}(id)
	}
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}

	verdicts := Judge(targets, abroad, china)
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	next := map[int64]*Status{}
	for id, v := range verdicts {
		st := m.inbounds[id]
		if st == nil {
			st = &Status{}
		}
		st.confirm(v.verdict, now)
		st.Probes = v.probes
		next[id] = st
	}
	m.inbounds = next
	m.lastRun = now
	m.probes = answered
	return nil
}

// confirm folds one round's verdict into the status. A good result shows at
// once; a bad one only once it repeats.
func (st *Status) confirm(v Verdict, now time.Time) {
	st.Checked = now
	if v == VerdictOK || v == st.Verdict {
		if v != st.Verdict {
			st.Since = now
		}
		st.Verdict, st.pending, st.streak = v, "", 0
		return
	}
	if v == st.pending {
		st.streak++
	} else {
		st.pending, st.streak = v, 1
	}
	if st.streak >= confirmRounds {
		st.Verdict, st.Since, st.pending, st.streak = v, now, "", 0
	}
}

// Targets lists what to dial: each probed inbound, and one control port per
// node that has any. Probe nodes themselves are left out — they sit in China
// and serve nobody.
func Targets(nodes map[int64]*store.Node, probes map[int64]bool, inbounds []*store.Inbound) []target {
	var out []target
	control := map[int64]bool{}
	for _, in := range inbounds {
		n := nodes[in.NodeID]
		if n == nil || !n.Enabled || probes[n.ID] || !in.Enabled || n.Address == "" {
			continue
		}
		if in.Address != "" || in.RelayNodeID != 0 {
			continue
		}
		switch in.Protocol {
		case store.ProtoVLESS, store.ProtoAnyTLS, store.ProtoShadowsocks:
		default:
			continue
		}
		out = append(out, target{key: fmt.Sprintf("in-%d", in.ID), nodeID: n.ID, inbound: in.ID,
			addr: net.JoinHostPort(n.Address, strconv.Itoa(in.Port))})
		if !control[n.ID] {
			control[n.ID] = true
			out = append(out, target{key: fmt.Sprintf("ctl-%d", n.ID), nodeID: n.ID,
				addr: net.JoinHostPort(n.Address, strconv.Itoa(controlPort))})
		}
	}
	return out
}

func (m *Monitor) dialAll(ctx context.Context, targets []target) map[string]bool {
	out := make(map[string]bool, len(targets))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 32)
	for _, t := range targets {
		wg.Add(1)
		go func(t target) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			dctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			err := m.dial(dctx, t.addr)
			mu.Lock()
			out[t.key] = err == nil
			mu.Unlock()
		}(t)
	}
	wg.Wait()
	return out
}

type judged struct {
	verdict Verdict
	probes  int
}

// Judge turns one round's results into a verdict per inbound: abroad is the
// panel's own result by target key, china every probe's.
func Judge(targets []target, abroad map[string]bool, china map[string][]bool) map[int64]judged {
	// Per node: does the control port show an address-wide block, and do all
	// of its inbounds fail from China while working from abroad?
	type nodeFacts struct {
		controlBlocked bool
		inbounds       int
		allBlocked     bool
	}
	facts := map[int64]*nodeFacts{}
	for _, t := range targets {
		f := facts[t.nodeID]
		if f == nil {
			f = &nodeFacts{allBlocked: true}
			facts[t.nodeID] = f
		}
		cn := china[t.key]
		blocked := abroad[t.key] && len(cn) > 0 && none(cn)
		if t.inbound == 0 {
			f.controlBlocked = blocked
			continue
		}
		f.inbounds++
		if !blocked {
			f.allBlocked = false
		}
	}

	out := map[int64]judged{}
	for _, t := range targets {
		if t.inbound == 0 {
			continue
		}
		cn := china[t.key]
		if len(cn) == 0 {
			continue // no probe answered for it; say nothing
		}
		var v Verdict
		f := facts[t.nodeID]
		switch {
		case !abroad[t.key]:
			v = VerdictDown
		case all(cn):
			v = VerdictOK
		case none(cn):
			v = VerdictPort
			if f.controlBlocked || (f.inbounds >= 2 && f.allBlocked) {
				v = VerdictTCP
			}
		default:
			v = VerdictPartial
		}
		out[t.inbound] = judged{verdict: v, probes: len(cn)}
	}
	return out
}

func all(xs []bool) bool {
	for _, x := range xs {
		if !x {
			return false
		}
	}
	return true
}

func none(xs []bool) bool {
	for _, x := range xs {
		if x {
			return false
		}
	}
	return true
}

// Inbound is the confirmed status of one inbound, if a round has judged it.
func (m *Monitor) Inbound(id int64) (Status, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.inbounds[id]
	if !ok || st.Verdict == "" {
		return Status{}, false
	}
	return *st, true
}

// Node is the worst confirmed verdict among a node's inbounds.
func (m *Monitor) Node(nodeID int64, inbounds []*store.Inbound) (Verdict, bool) {
	var worst Verdict
	for _, in := range inbounds {
		if in.NodeID != nodeID {
			continue
		}
		if st, ok := m.Inbound(in.ID); ok && st.Verdict.rank() > worst.rank() {
			worst = st.Verdict
		}
	}
	return worst, worst != ""
}

// LastRun is when the last round finished and how many probes answered.
func (m *Monitor) LastRun() (time.Time, int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastRun, m.probes
}
