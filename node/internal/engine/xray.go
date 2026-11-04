package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Zayvian/veyra-node/internal/proto"
)

// xrayCore runs Xray beside sing-box, for the inbounds the panel assigns to it.
//
// Xray is a separate process rather than a library linked in like sing-box.
// The two cores depend on different versions of the same libraries, and one
// binary holding both would pin one of them to versions it was never tested
// with. Nothing is lost by the separation: Xray's own API changes users on a
// running inbound, which is what the sing-box fork's patches exist to do.
//
// What carries over from sing-box is the contract with the panel. Users are
// swapped in without touching anyone else's connections; a configuration
// change restarts the process, dropping only connections to Xray inbounds;
// traffic is read as cumulative counters and turned into deltas by the same
// bookkeeping; and a configuration Xray refuses leaves the previous one
// running.
type xrayCore struct {
	log *slog.Logger

	// bin is the Xray executable; empty when this node has none.
	bin     string
	version string

	mu sync.Mutex

	// raw is the fragment being served, as the panel sent it (compacted);
	// inbounds is the same, parsed. Both empty when nothing is assigned to
	// Xray, which is the normal case on most nodes.
	raw      json.RawMessage
	inbounds []xrayInbound
	// routing is the panel's server policy and unlock routing, in Xray's
	// format.
	routing xrayRouting

	// want is the latest user list for every tag the panel named, whether or
	// not that tag is an Xray inbound yet: a configuration that moves an
	// inbound to Xray may arrive after the users for it. live is what the
	// running process has been given, per inbound and by email.
	want map[string][]proto.User
	live map[string]map[string]proto.User

	proc *xrayProcess

	// blocked is what the address limit currently blocks, per user, as the
	// key of the routing rule that does it (see setBlocks). Reset with every
	// process: a new Xray starts with no rules but the policy's.
	blocked map[string]string

	// Set by Close, and checked by the supervisor so a deliberate stop is not
	// mistaken for a crash.
	closed bool

	// dir holds the configuration file. Created on first use, private to this
	// user: the file carries every inbound's private keys and user ids.
	dir string

	counters *counters
}

type xrayInbound struct {
	Tag      string `json:"tag"`
	Protocol string `json:"protocol"`

	// fields is the whole inbound, kept verbatim so that every setting the
	// panel chose survives into the file Xray reads; only the client list is
	// filled in.
	fields map[string]json.RawMessage
}

// xrayProcess is one running Xray.
type xrayProcess struct {
	cmd  *exec.Cmd
	api  *xrayAPI
	done chan struct{} // closed once the process has exited
	logs *tailBuffer

	// stopping marks an exit this node asked for.
	stopping bool
}

func newXrayCore(log *slog.Logger) *xrayCore {
	return &xrayCore{
		log:      log.With("core", "xray"),
		want:     map[string][]proto.User{},
		live:     map[string]map[string]proto.User{},
		counters: newCounters(),
	}
}

// findXray resolves which Xray binary to use. An explicit path must work; with
// none, the node looks beside its own executable — where the installer puts
// Xray — and then on PATH. "off" turns Xray off even when a binary is there.
func findXray(explicit string) (string, error) {
	switch explicit {
	case "off", "none":
		return "", nil
	case "":
		if self, err := os.Executable(); err == nil {
			candidate := filepath.Join(filepath.Dir(self), "xray")
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, nil
			}
		}
		if p, err := exec.LookPath("xray"); err == nil {
			return p, nil
		}
		return "", nil
	default:
		if _, err := os.Stat(explicit); err != nil {
			return "", fmt.Errorf("xray binary: %w", err)
		}
		return explicit, nil
	}
}

// xrayVersion runs `xray version` and returns the number from its first line,
// "Xray 26.3.27 (Xray, Penetrates Everything.) ...".
func xrayVersion(bin string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "version").Output()
	if err != nil {
		return "", fmt.Errorf("%s version: %w", bin, err)
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 || fields[0] != "Xray" {
		return "", fmt.Errorf("%s does not look like Xray: %q", bin, firstLine(out))
	}
	return fields[1], nil
}

func firstLine(b []byte) string {
	line, _, _ := bytes.Cut(b, []byte("\n"))
	return string(line)
}

func (x *xrayCore) available() bool { return x.bin != "" }

// tags lists the Xray inbounds actually being served.
func (x *xrayCore) tags() []string {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.proc == nil {
		return nil
	}
	out := make([]string, 0, len(x.inbounds))
	for _, in := range x.inbounds {
		out = append(out, in.Tag)
	}
	return out
}

// owns reports whether tag is one of the inbounds Xray is configured to serve.
func (x *xrayCore) owns(tag string) bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	for _, in := range x.inbounds {
		if in.Tag == tag {
			return true
		}
	}
	return false
}

// xrayRouting is the panel's part of Xray's routing: the rules that carry the
// server policy, the outbounds the panel adds beside the node's own direct
// and block, and the unlock rules that send chosen sites to them.
//
// Unlock rules are kept out of the configuration file and added through the
// API once Xray is up, after every address-limit rule. Xray matches rules in
// order and the limit's rules can only be appended, so an unlock rule in the
// file would come first and let a blocked address through to the sites it
// names. Each time a limit rule is added, the unlock rules are taken out and
// put back behind it.
type xrayRouting struct {
	Rules     []json.RawMessage
	Outbounds []json.RawMessage
	Unlock    []xrayUnlockRule
}

// xrayUnlockRule sends connections to the named domains, and everything under
// them, to one of the panel's outbounds.
type xrayUnlockRule struct {
	DomainSuffix []string `json:"domain_suffix"`
	Outbound     string   `json:"outbound"`
}

func xrayUnlockRuleTag(i int) string { return fmt.Sprintf("veyra-unlock:%d", i) }

// reservedXrayTags are the tags the node itself gives Xray's handlers. An
// outbound from the panel may not take one: it would replace where blocked
// traffic goes, or the API.
var reservedXrayTags = map[string]bool{"direct": true, xrayBlockOutbound: true, "api": true}

// parseXrayFragment decodes what the panel sent under config.xray: the
// inbounds, the routing rules that carry the server policy, and the outbounds
// those rules may send to besides the node's own. Only VLESS is
// accepted: it is what the panel assigns to Xray, and a protocol this node
// does not know how to give users to must fail here, not silently serve
// nobody.
func parseXrayFragment(raw json.RawMessage) ([]xrayInbound, xrayRouting, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, xrayRouting{}, nil
	}
	var frag struct {
		Inbounds  []json.RawMessage `json:"inbounds"`
		Outbounds []json.RawMessage `json:"outbounds"`
		Unlock    []xrayUnlockRule  `json:"unlock"`
		Routing   struct {
			Rules []json.RawMessage `json:"rules"`
		} `json:"routing"`
	}
	if err := json.Unmarshal(raw, &frag); err != nil {
		return nil, xrayRouting{}, fmt.Errorf("xray configuration: %w", err)
	}
	inbounds, err := parseXrayInbounds(frag.Inbounds)
	if err != nil {
		return nil, xrayRouting{}, err
	}
	seen := map[string]bool{}
	for i, rawOut := range frag.Outbounds {
		var out struct {
			Tag string `json:"tag"`
		}
		if err := json.Unmarshal(rawOut, &out); err != nil {
			return nil, xrayRouting{}, fmt.Errorf("xray outbound %d: %w", i, err)
		}
		switch {
		case out.Tag == "":
			return nil, xrayRouting{}, fmt.Errorf("xray outbound %d has no tag", i)
		case reservedXrayTags[out.Tag]:
			return nil, xrayRouting{}, fmt.Errorf("xray outbound tag %q is the node's own", out.Tag)
		case seen[out.Tag]:
			return nil, xrayRouting{}, fmt.Errorf("xray outbound tag %q is used twice", out.Tag)
		}
		seen[out.Tag] = true
	}
	for i, u := range frag.Unlock {
		if !seen[u.Outbound] {
			return nil, xrayRouting{}, fmt.Errorf("xray unlock rule %d: no outbound %q", i, u.Outbound)
		}
		if len(u.DomainSuffix) == 0 {
			return nil, xrayRouting{}, fmt.Errorf("xray unlock rule %d names no domain", i)
		}
	}
	return inbounds, xrayRouting{
		Rules: frag.Routing.Rules, Outbounds: frag.Outbounds, Unlock: frag.Unlock,
	}, nil
}

func parseXrayInbounds(frag []json.RawMessage) ([]xrayInbound, error) {
	out := make([]xrayInbound, 0, len(frag))
	seen := map[string]bool{}
	for i, rawIn := range frag {
		var in xrayInbound
		if err := json.Unmarshal(rawIn, &in); err != nil {
			return nil, fmt.Errorf("xray inbound %d: %w", i, err)
		}
		if err := json.Unmarshal(rawIn, &in.fields); err != nil {
			return nil, fmt.Errorf("xray inbound %d: %w", i, err)
		}
		if in.Tag == "" {
			return nil, fmt.Errorf("xray inbound %d has no tag", i)
		}
		if seen[in.Tag] {
			return nil, fmt.Errorf("xray inbound tag %q is used twice", in.Tag)
		}
		seen[in.Tag] = true
		if in.Protocol != "vless" {
			return nil, fmt.Errorf("xray inbound %s: protocol %q is not supported by this node", in.Tag, in.Protocol)
		}
		out = append(out, in)
	}
	return out, nil
}

// buildXrayConfig assembles the file Xray runs: the panel's inbounds with
// their users filled in, and everything the node needs around them.
//
// The surroundings belong to the node, as sing-box's API listeners do (see
// apis.go): an API on a loopback port of its choosing, per-user traffic and
// online counters, and the direct and block outbounds. The panel's rules may
// send a connection to block, or to one of the panel's own outbounds, which
// is how a node sends chosen sites out through another exit.
func buildXrayConfig(inbounds []xrayInbound, routing xrayRouting, users map[string][]proto.User, apiAddr string) ([]byte, error) {
	ins := make([]json.RawMessage, 0, len(inbounds))
	for _, in := range inbounds {
		fields := make(map[string]json.RawMessage, len(in.fields))
		for k, v := range in.fields {
			fields[k] = v
		}
		settings := map[string]json.RawMessage{}
		if raw, ok := fields["settings"]; ok && len(raw) > 0 && string(raw) != "null" {
			if err := json.Unmarshal(raw, &settings); err != nil {
				return nil, fmt.Errorf("xray inbound %s settings: %w", in.Tag, err)
			}
		}
		clients, err := json.Marshal(vlessClients(users[in.Tag]))
		if err != nil {
			return nil, err
		}
		settings["clients"] = clients
		if _, ok := settings["decryption"]; !ok {
			// VLESS refuses to start without it; "none" is the only value
			// that needs no further configuration.
			settings["decryption"] = json.RawMessage(`"none"`)
		}
		if fields["settings"], err = json.Marshal(settings); err != nil {
			return nil, err
		}
		b, err := json.Marshal(fields)
		if err != nil {
			return nil, err
		}
		ins = append(ins, b)
	}

	cfg := map[string]any{
		"log": map[string]any{"loglevel": "warning", "access": "none"},
		"api": map[string]any{
			"tag":      "api",
			"listen":   apiAddr,
			"services": []string{"HandlerService", "StatsService", "RoutingService"},
		},
		"stats": map[string]any{},
		"policy": map[string]any{
			"levels": map[string]any{
				"0": map[string]any{
					"statsUserUplink":   true,
					"statsUserDownlink": true,
					"statsUserOnline":   true,
				},
			},
		},
		"inbounds": ins,
	}
	// direct first: Xray sends whatever no rule matches to the first outbound.
	outbounds := []any{
		map[string]any{"tag": "direct", "protocol": "freedom"},
		map[string]any{"tag": xrayBlockOutbound, "protocol": "blackhole"},
	}
	for _, o := range routing.Outbounds {
		outbounds = append(outbounds, o)
	}
	cfg["outbounds"] = outbounds
	rules := routing.Rules
	if rules == nil {
		rules = []json.RawMessage{}
	}
	cfg["routing"] = map[string]any{"rules": rules}
	return json.MarshalIndent(cfg, "", "  ")
}

// xrayBlockOutbound is where blocked connections go: the policy's, and those
// of addresses over a user's limit.
const xrayBlockOutbound = "block"

type vlessClient struct {
	ID    string `json:"id"`
	Flow  string `json:"flow,omitempty"`
	Email string `json:"email"`
}

func vlessClients(users []proto.User) []vlessClient {
	out := make([]vlessClient, 0, len(users))
	for _, u := range users {
		out = append(out, vlessClient{ID: u.UUID, Flow: u.Flow, Email: u.Name})
	}
	return out
}

// check asks Xray whether it would accept a configuration, without starting
// anything. Run before the running process is touched, so a fragment Xray
// refuses costs nothing.
func (x *xrayCore) check(raw json.RawMessage) error {
	inbounds, routing, err := parseXrayFragment(raw)
	if err != nil {
		return err
	}
	if len(inbounds) == 0 {
		return nil
	}
	if !x.available() {
		return errors.New("this node has no Xray, so it cannot serve these inbounds: " +
			strings.Join(inboundTags(inbounds), ", "))
	}

	x.mu.Lock()
	users := copyUsers(x.want)
	dir, err := x.configDir()
	x.mu.Unlock()
	if err != nil {
		return err
	}

	// The API port in a test run is never bound, so any address will do.
	cfg, err := buildXrayConfig(inbounds, routing, users, "127.0.0.1:1")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "check.json")
	if err := os.WriteFile(path, cfg, 0o600); err != nil {
		return err
	}
	defer os.Remove(path)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, x.bin, "run", "-test", "-c", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("xray rejected the configuration: %s", xrayFailure(out))
	}
	return nil
}

// xrayFailure picks the line that says why from Xray's output, which opens
// with a banner and a line about which file it read.
func xrayFailure(out []byte) string {
	var last string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Xray ") ||
			strings.HasPrefix(line, "A unified platform") ||
			strings.Contains(line, "Reading config") {
			continue
		}
		last = line
	}
	if last == "" {
		return "no reason given"
	}
	return last
}

// apply serves a new fragment. An unchanged one is left running; an empty one
// stops Xray. A different one restarts it — and if the new process does not
// come up, the previous configuration is started again and the error returned.
func (x *xrayCore) apply(raw json.RawMessage) error {
	inbounds, routing, err := parseXrayFragment(raw)
	if err != nil {
		return err
	}

	x.mu.Lock()
	defer x.mu.Unlock()

	if bytes.Equal(raw, x.raw) && (x.proc != nil || len(inbounds) == 0) {
		return nil
	}
	if len(inbounds) > 0 && !x.available() {
		return errors.New("this node has no Xray, so it cannot serve these inbounds: " +
			strings.Join(inboundTags(inbounds), ", "))
	}

	prevRaw, prevInbounds, prevRouting := x.raw, x.inbounds, x.routing
	x.stopLocked()

	x.raw, x.inbounds, x.routing = append(json.RawMessage(nil), raw...), inbounds, routing
	if len(inbounds) == 0 {
		x.log.Info("no inbounds assigned to xray; stopped")
		return nil
	}
	startErr := x.startLocked()
	if startErr == nil {
		return nil
	}

	x.raw, x.inbounds, x.routing = prevRaw, prevInbounds, prevRouting
	if len(prevInbounds) == 0 {
		return startErr
	}
	if err := x.startLocked(); err != nil {
		x.log.Error("restoring the previous xray configuration failed; "+
			"no xray inbound is being served", "apply_error", startErr, "restore_error", err)
		return startErr
	}
	x.log.Warn("xray configuration rejected; the previous one is running again", "error", startErr)
	return startErr
}

// restart stops Xray and starts it again on the same fragment and users. It is
// for when the process is fine but the world under it changed — the machine's
// public address — and its listeners and outbound sockets are left on the old
// one. A configuration with no inbounds has nothing to restart.
func (x *xrayCore) restart(reason string) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	if len(x.inbounds) == 0 || !x.available() {
		return nil
	}
	x.log.Info("restarting xray", "reason", reason)
	x.stopLocked()
	return x.startLocked()
}

// startLocked starts Xray on the current fragment and the wanted users, and
// waits until its API answers. x.mu must be held.
func (x *xrayCore) startLocked() error {
	apiAddr, err := freeLoopbackAddr()
	if err != nil {
		return fmt.Errorf("reserve xray api port: %w", err)
	}
	users := map[string][]proto.User{}
	for _, in := range x.inbounds {
		users[in.Tag] = x.want[in.Tag]
	}
	cfg, err := buildXrayConfig(x.inbounds, x.routing, users, apiAddr)
	if err != nil {
		return err
	}
	dir, err := x.configDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, cfg, 0o600); err != nil {
		return err
	}

	logs := newTailBuffer(20)
	cmd := exec.Command(x.bin, "run", "-c", path)
	cmd.Stdout = io.MultiWriter(logs, &lineLogger{log: x.log})
	cmd.Stderr = cmd.Stdout
	cmd.SysProcAttr = xraySysProcAttr()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start xray: %w", err)
	}
	api, err := dialXrayAPI(apiAddr)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("xray api: %w", err)
	}
	p := &xrayProcess{cmd: cmd, api: api, done: make(chan struct{}), logs: logs}
	go func() {
		_ = cmd.Wait()
		close(p.done)
	}()

	if err := waitXrayReady(p); err != nil {
		p.stopping = true
		_ = cmd.Process.Kill()
		<-p.done
		api.Close()
		return err
	}

	if err := x.addUnlockRules(p.api); err != nil {
		p.stopping = true
		_ = cmd.Process.Kill()
		<-p.done
		api.Close()
		return fmt.Errorf("xray unlock rules: %w", err)
	}

	x.proc = p
	x.blocked = map[string]string{}
	x.live = map[string]map[string]proto.User{}
	for tag, list := range users {
		m := make(map[string]proto.User, len(list))
		for _, u := range list {
			m[u.Name] = u
		}
		x.live[tag] = m
	}
	go x.supervise(p)
	x.log.Info("xray started", "version", x.version, "inbounds", len(x.inbounds))
	return nil
}

// waitXrayReady returns once the API answers, or with Xray's own explanation
// if the process exits first — a port already taken, most often.
func waitXrayReady(p *xrayProcess) error {
	deadline := time.Now().Add(10 * time.Second)
	for {
		select {
		case <-p.done:
			return fmt.Errorf("xray exited: %s", xrayFailure([]byte(p.logs.String())))
		default:
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, err := p.api.QueryStats(ctx, "user>>>")
		cancel()
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			_ = p.cmd.Process.Kill()
			return fmt.Errorf("xray did not answer on its api: %w", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// stopLocked stops the running process, first reading out what it counted
// since the last report so that it is not lost with it. x.mu must be held.
func (x *xrayCore) stopLocked() {
	p := x.proc
	if p == nil {
		return
	}
	x.proc = nil
	p.stopping = true

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	final, err := xrayTotals(ctx, p.api)
	cancel()
	if err != nil {
		x.log.Warn("read xray traffic before stopping; this interval is lost", "error", err)
		final = nil
	}
	x.counters.retire(final)

	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		_ = p.cmd.Process.Kill()
		<-p.done
	}
	p.api.Close()
	x.live = map[string]map[string]proto.User{}
}

// supervise restarts Xray if it exits without being asked to. Users of Xray
// inbounds would otherwise stay offline until some unrelated change happened
// to push a new configuration.
func (x *xrayCore) supervise(p *xrayProcess) {
	<-p.done

	x.mu.Lock()
	if x.closed || p.stopping || x.proc != p {
		x.mu.Unlock()
		return
	}
	x.log.Error("xray exited unexpectedly; restarting",
		"reason", xrayFailure([]byte(p.logs.String())))
	x.proc = nil
	x.live = map[string]map[string]proto.User{}
	// The process is gone and its counters with it: whatever it measured
	// since the last report cannot be read any more.
	x.counters.retire(nil)
	p.api.Close()
	x.mu.Unlock()

	for backoff := time.Second; ; backoff = min(2*backoff, 30*time.Second) {
		time.Sleep(backoff)
		x.mu.Lock()
		// Someone else has started it since, or there is nothing to run.
		if x.closed || x.proc != nil || len(x.inbounds) == 0 {
			x.mu.Unlock()
			return
		}
		err := x.startLocked()
		x.mu.Unlock()
		if err == nil {
			return
		}
		x.log.Error("restart xray", "error", err)
	}
}

// updateUsers records the wanted users for every tag and brings each running
// Xray inbound in line with them: users removed or changed are taken out,
// users new or changed are put in. Nobody else's connection is touched.
func (x *xrayCore) updateUsers(ctx context.Context, byTag map[string][]proto.User) error {
	x.mu.Lock()
	defer x.mu.Unlock()

	x.want = copyUsers(byTag)
	if x.proc == nil {
		return nil
	}
	api := x.proc.api

	for _, in := range x.inbounds {
		want := map[string]proto.User{}
		for _, u := range x.want[in.Tag] {
			want[u.Name] = u
		}
		live := x.live[in.Tag]
		if live == nil {
			live = map[string]proto.User{}
			x.live[in.Tag] = live
		}

		for _, name := range sortedNames(live) {
			if w, ok := want[name]; ok && w == live[name] {
				continue
			}
			if err := api.RemoveUser(ctx, in.Tag, name); err != nil && !isXrayNotFound(err) {
				return fmt.Errorf("xray inbound %s: remove %s: %w", in.Tag, name, err)
			}
			delete(live, name)
		}
		for _, name := range sortedNames(want) {
			if _, ok := live[name]; ok {
				continue
			}
			u := want[name]
			if err := api.AddVLESSUser(ctx, in.Tag, u.Name, u.UUID, u.Flow); err != nil {
				return fmt.Errorf("xray inbound %s: add %s: %w", in.Tag, name, err)
			}
			live[name] = u
		}
	}
	return nil
}

// isXrayNotFound is Xray's answer to removing a user it does not have, which
// for a removal is the outcome wanted.
func isXrayNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "not found")
}

// totals reads the per-user counters of the running process, and the
// generation of the bookkeeping they belong to.
func (x *xrayCore) totals(ctx context.Context) (gen uint64, totals map[string]proto.Usage, running bool, err error) {
	x.mu.Lock()
	p := x.proc
	gen = x.counters.generation()
	x.mu.Unlock()
	if p == nil {
		return gen, nil, false, nil
	}
	totals, err = xrayTotals(ctx, p.api)
	return gen, totals, true, err
}

func xrayTotals(ctx context.Context, api *xrayAPI) (map[string]proto.Usage, error) {
	stats, err := api.QueryStats(ctx, userPrefix)
	if err != nil {
		return nil, fmt.Errorf("query xray stats: %w", err)
	}
	out := map[string]proto.Usage{}
	for counter, value := range stats {
		name, direction, ok := splitUserCounter(counter)
		if !ok {
			continue
		}
		usage := out[name]
		switch direction {
		case "up":
			usage.Up = value
		case "down":
			usage.Down = value
		}
		out[name] = usage
	}
	return out, nil
}

// online lists users with a live connection to an Xray inbound.
func (x *xrayCore) online(ctx context.Context) ([]string, error) {
	x.mu.Lock()
	p := x.proc
	x.mu.Unlock()
	if p == nil {
		return nil, nil
	}
	return p.api.OnlineUsers(ctx)
}

// conns lists who is connected to Xray from where, one entry per user and
// source address — Xray counts addresses, not connections. The IDs are not
// connections either: nothing can close an Xray connection from outside, so
// the address limit blocks the address instead (setBlocks).
func (x *xrayCore) conns(ctx context.Context) ([]conn, error) {
	x.mu.Lock()
	p := x.proc
	x.mu.Unlock()
	if p == nil {
		return nil, nil
	}
	users, err := p.api.OnlineUsers(ctx)
	if err != nil {
		return nil, err
	}
	var out []conn
	for _, user := range users {
		ips, err := p.api.OnlineIPs(ctx, user)
		if err != nil {
			return nil, err
		}
		for raw := range ips {
			addr, ok := xrayAddr(raw)
			if !ok {
				continue
			}
			ip := addr.String()
			out = append(out, conn{ID: xrayConnPrefix + user + "|" + ip, User: user, IP: ip})
		}
	}
	return out, nil
}

// xrayConnPrefix marks a conn that came from Xray rather than sing-box.
const xrayConnPrefix = "xray|"

// xrayAddr reads an address as Xray writes it — IPv6 in brackets — into the
// form sing-box's connections use, so one client is one address whichever
// core it reached.
func xrayAddr(s string) (netip.Addr, bool) {
	addr, err := netip.ParseAddr(strings.Trim(s, "[]"))
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

// setBlocks makes Xray refuse new connections from the addresses each user
// is over their limit with, and accept them again once they are not. One
// routing rule per user, replaced when the set changes.
//
// This is coarser than what sing-box gets: a connection already open from a
// blocked address carries on until it closes by itself. New ones — and a
// client reconnects all the time — are refused.
func (x *xrayCore) setBlocks(ctx context.Context, over map[string]map[string]bool) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.proc == nil {
		return
	}
	api := x.proc.api

	want := map[string]string{}
	addrs := map[string][]netip.Addr{}
	for user, ips := range over {
		var list []netip.Addr
		for ip := range ips {
			if a, err := netip.ParseAddr(ip); err == nil {
				list = append(list, a)
			}
		}
		if len(list) == 0 {
			continue
		}
		sort.Slice(list, func(i, j int) bool { return list[i].Less(list[j]) })
		key := make([]string, len(list))
		for i, a := range list {
			key[i] = a.String()
		}
		want[user] = strings.Join(key, ",")
		addrs[user] = list
	}

	for user, key := range x.blocked {
		if want[user] == key {
			continue
		}
		if err := api.RemoveRule(ctx, xrayLimitRuleTag(user)); err != nil {
			x.log.Warn("unblock addresses over the limit", "user", user, "error", err)
			continue
		}
		delete(x.blocked, user)
	}
	// A limit rule is appended, and has to come before the unlock rules to
	// bite: they are taken out first and put back after the last one.
	adding := false
	for user := range want {
		if _, ok := x.blocked[user]; !ok {
			adding = true
		}
	}
	if adding && len(x.routing.Unlock) > 0 {
		if err := x.removeUnlockRules(ctx, api); err != nil {
			x.log.Warn("take unlock rules out to add a limit rule", "error", err)
		}
		defer func() {
			if err := x.addUnlockRules(api); err != nil {
				x.log.Warn("put unlock rules back after the limit rules", "error", err)
			}
		}()
	}
	for user, key := range want {
		if _, ok := x.blocked[user]; ok {
			continue
		}
		if err := api.BlockSources(ctx, xrayLimitRuleTag(user), user, addrs[user]); err != nil {
			x.log.Warn("block addresses over the limit", "user", user, "error", err)
			continue
		}
		x.blocked[user] = key
		x.log.Info("blocked addresses over the limit", "user", user, "addresses", key)
	}
}

func xrayLimitRuleTag(user string) string { return "veyra-ip-limit:" + user }

// addUnlockRules appends the unlock rules, in the panel's order.
func (x *xrayCore) addUnlockRules(api *xrayAPI) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i, u := range x.routing.Unlock {
		if err := api.AddDomainRule(ctx, xrayUnlockRuleTag(i), u.Outbound, u.DomainSuffix); err != nil {
			return err
		}
	}
	return nil
}

func (x *xrayCore) removeUnlockRules(ctx context.Context, api *xrayAPI) error {
	for i := range x.routing.Unlock {
		if err := api.RemoveRule(ctx, xrayUnlockRuleTag(i)); err != nil {
			return err
		}
	}
	return nil
}

func (x *xrayCore) close() {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.closed = true
	x.stopLocked()
	if x.dir != "" {
		_ = os.RemoveAll(x.dir)
		x.dir = ""
	}
}

// configDir returns the private directory for Xray's configuration file.
// x.mu must be held.
func (x *xrayCore) configDir() (string, error) {
	if x.dir != "" {
		return x.dir, nil
	}
	dir, err := os.MkdirTemp("", "veyra-xray-")
	if err != nil {
		return "", fmt.Errorf("xray config directory: %w", err)
	}
	x.dir = dir
	return dir, nil
}

func inboundTags(inbounds []xrayInbound) []string {
	out := make([]string, 0, len(inbounds))
	for _, in := range inbounds {
		out = append(out, in.Tag)
	}
	return out
}

func copyUsers(m map[string][]proto.User) map[string][]proto.User {
	out := make(map[string][]proto.User, len(m))
	for k, v := range m {
		out[k] = append([]proto.User(nil), v...)
	}
	return out
}

func sortedNames(m map[string]proto.User) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// tailBuffer keeps the last few lines a process wrote, for error messages.
type tailBuffer struct {
	mu    sync.Mutex
	max   int
	lines []string
	part  []byte
}

func newTailBuffer(max int) *tailBuffer { return &tailBuffer{max: max} }

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.part = append(t.part, p...)
	for {
		i := bytes.IndexByte(t.part, '\n')
		if i < 0 {
			break
		}
		t.lines = append(t.lines, string(t.part[:i]))
		t.part = t.part[i+1:]
		if len(t.lines) > t.max {
			t.lines = t.lines[len(t.lines)-t.max:]
		}
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := strings.Join(t.lines, "\n")
	if len(t.part) > 0 {
		s += "\n" + string(t.part)
	}
	return s
}

// lineLogger passes Xray's output into the node's log, a line at a time.
type lineLogger struct {
	log  *slog.Logger
	part []byte
}

func (l *lineLogger) Write(p []byte) (int, error) {
	l.part = append(l.part, p...)
	for {
		i := bytes.IndexByte(l.part, '\n')
		if i < 0 {
			break
		}
		if line := strings.TrimSpace(string(l.part[:i])); line != "" {
			l.log.Info(line)
		}
		l.part = l.part[i+1:]
	}
	return len(p), nil
}
