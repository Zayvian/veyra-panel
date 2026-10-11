// Package engine runs sing-box in this process.
//
// In-process is what makes hot-swapping possible. A subprocess could only be
// reconfigured by rewriting its config file and restarting it, which drops every
// live connection; here the inbound's user set can be replaced directly.
//
// This is also where the licence boundary sits. This package links sing-box and
// is therefore GPL-3.0, along with the rest of this repository. The panel links
// none of it and talks to this node over a network protocol.
package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	singJSON "github.com/sagernet/sing/common/json"
	"github.com/sagernet/sing/service"

	"github.com/Zayvian/veyra-node/internal/proto"
)

type Engine struct {
	log *slog.Logger

	mu      sync.Mutex
	box     *box.Box
	cancel  context.CancelFunc
	ctx     context.Context
	running bool

	// Last applied config, kept so a failed apply can be reported without
	// having torn down a working data plane.
	current json.RawMessage

	// What is actually being served, and why it is not what the panel last
	// sent. Both are reported upstream after every apply: the panel cannot
	// otherwise tell a configuration that took effect from one that was
	// rejected and rolled back.
	activeTags []string
	applyErr   string

	// Fingerprint of the last user list applied to this instance. The panel
	// re-sends the full list on every reconnect and after every change anywhere
	// on the panel, and an identical list need not be swapped in again. Cleared
	// whenever the instance is rebuilt: a new instance starts with no users, so
	// the same list must then be applied, not skipped.
	usersHash    [sha256.Size]byte
	usersHashSet bool

	// Loopback API addresses this node chose for itself; see apis.go.
	apis localAPIs

	counters *counters

	// Per-user cap on distinct source addresses. Its own lock: it is read and
	// written on a timer that must not wait behind a configuration swap.
	limits *limiter
}

func New(log *slog.Logger) *Engine {
	if log == nil {
		log = slog.Default()
	}
	e := &Engine{log: log, counters: newCounters(), limits: newLimiter()}
	apis, err := newLocalAPIs()
	if err != nil {
		// Only reachable if loopback itself is unusable, in which case nothing
		// this process does next would work either.
		log.Error("reserve local api ports", "error", err)
	}
	e.apis = apis
	return e
}

func (e *Engine) SingboxVersion() string { return C.Version }

// ApplyConfig starts sing-box, or replaces a running instance.
//
// sing-box has no way to reload in place, so this stops and starts. Live
// connections on every inbound are dropped, which is precisely why user changes
// go through UpdateUsers instead of arriving as a new config.
func (e *Engine) ApplyConfig(ctx context.Context, raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("empty configuration")
	}
	raw = compactJSON(raw)

	// The panel sends the configuration on every reconnect, so a panel restart
	// or a network blip would otherwise rebuild every listener on every node and
	// drop every live connection, for a configuration that has not changed. An
	// identical one is already being served; there is nothing to do.
	e.mu.Lock()
	if e.running && bytes.Equal(raw, e.current) {
		// The panel has gone back to what is running, so whatever failed in
		// between is no longer the panel's request.
		e.applyErr = ""
		e.mu.Unlock()
		e.log.Debug("configuration unchanged; listeners kept")
		return nil
	}
	e.mu.Unlock()

	// Parse before stopping anything. A configuration the panel got wrong — a
	// certificate path that does not exist, a port already taken — must leave
	// the running data plane alone and be reported instead.
	options, err := parseOptions(ctx, raw)
	if err != nil {
		return fmt.Errorf("parse configuration: %w", err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	previous := e.current
	applyErr := e.swapLocked(options, raw)
	if applyErr == nil {
		e.applyErr = ""
		return nil
	}
	e.applyErr = applyErr.Error()

	// Building the new instance happens before the old one is stopped, so a
	// failure there costs nothing and e.running is still true. A failure to
	// *start* it costs everything: by then the old instance is gone, and one
	// mistyped port would take every inbound on this node offline until some
	// unrelated edit happened to push a new configuration.
	if previous == nil || e.running {
		return applyErr
	}

	prevOptions, err := parseOptions(ctx, previous)
	if err != nil {
		e.log.Error("cannot restore the previous configuration; this node is "+
			"serving nothing", "apply_error", applyErr, "parse_error", err)
		return applyErr
	}
	if err := e.swapLocked(prevOptions, previous); err != nil {
		e.log.Error("restoring the previous configuration failed; this node is "+
			"serving nothing", "apply_error", applyErr, "restore_error", err)
		return applyErr
	}
	// The panel re-sends users after every configuration push, so the restored
	// instance gets its user set back without asking for it.
	e.log.Warn("configuration rejected; the previous one is running again",
		"error", applyErr)
	return applyErr
}

// swapLocked builds an instance and, only once it exists, stops the running one
// and starts the new. On any error the engine is left in a truthful state:
// still running the old instance if the failure came before the swap, stopped
// if it came after. e.mu must be held.
func (e *Engine) swapLocked(options option.Options, raw json.RawMessage) error {
	// include.Context carries the protocol registries. They are needed both to
	// decode a configuration and to build the instance — sing-box looks them up
	// again when constructing inbounds, and without them refuses with "missing
	// endpoint registry in context".
	boxCtx, cancel := context.WithCancel(include.Context(context.Background()))
	e.apis.override(&options)
	if err := ensureShadowsocksUsers(&options); err != nil {
		cancel()
		return fmt.Errorf("prepare shadowsocks inbounds: %w", err)
	}
	instance, err := box.New(box.Options{Context: boxCtx, Options: options})
	if err != nil {
		cancel()
		return fmt.Errorf("build sing-box instance: %w", err)
	}

	// The old instance's counters go with it, so whatever it measured since the
	// last report is read out first and carried into the next one. Only now,
	// with the new instance built: a build failure leaves the old one running
	// and its counters where they are.
	if e.running {
		final, err := e.readTotals(e.ctx)
		if err != nil {
			e.log.Warn("read traffic before rebuild; this interval is lost", "error", err)
			final = nil
		}
		e.counters.retire(final)
	}

	e.stopLocked()

	if err := instance.Start(); err != nil {
		cancel()
		instance.Close()
		return fmt.Errorf("start sing-box: %w", err)
	}

	e.box, e.cancel, e.ctx, e.running = instance, cancel, boxCtx, true
	e.current = append(json.RawMessage(nil), raw...)

	e.activeTags = e.activeTags[:0]
	for _, in := range options.Inbounds {
		e.activeTags = append(e.activeTags, in.Tag)
	}

	// A new instance has no users until the panel's next user list arrives, and
	// that list may well be identical to the last one — it must be applied.
	e.usersHashSet = false

	e.log.Info("sing-box started", "inbounds", len(options.Inbounds))
	return nil
}

// State is what this node is serving right now, and the reason it differs from
// what the panel last sent, if it does.
func (e *Engine) State() proto.StateData {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Copied: activeTags is rewritten in place on the next apply, and the
	// caller marshals this after the lock is gone.
	return proto.StateData{
		Inbounds: append([]string(nil), e.activeTags...),
		Error:    e.applyErr,
	}
}

// parseOptions decodes a configuration using sing-box's own decoder, which
// applies the defaults and validation the plain JSON types do not.
func parseOptions(ctx context.Context, raw json.RawMessage) (option.Options, error) {
	// The registries tell the decoder which protocol types exist; without this
	// context every inbound decodes as unknown.
	ctx = include.Context(ctx)

	var options option.Options
	if err := options.UnmarshalJSONContext(ctx, raw); err != nil {
		return option.Options{}, err
	}
	return options, nil
}

func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stopLocked()
	return nil
}

func (e *Engine) stopLocked() {
	if !e.running {
		return
	}
	if e.box != nil {
		e.box.Close()
	}
	if e.cancel != nil {
		e.cancel()
	}
	// Nothing is being served once this returns, and saying otherwise upstream
	// would be worse than saying nothing.
	e.activeTags = e.activeTags[:0]
	e.box, e.cancel, e.ctx, e.running = nil, nil, nil, false
}

// UpdateUsers replaces the user set on each named inbound without restarting it,
// then refreshes the statistics allowlist.
//
// Both halves matter. Swapping users alone leaves the v2ray_api statistics
// service counting for the set it was built with, so a newly added user relays
// traffic that is attributed to nobody — no error, no log line, just a user who
// never appears to consume anything.
func (e *Engine) UpdateUsers(_ context.Context, data proto.UsersData) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return fmt.Errorf("sing-box is not running")
	}

	// Every swap has a cost — and, in sing-vmess and sing-shadowsocks, a narrow
	// window where a connection can read a half-published user map (see the
	// core's MODIFICATIONS.md). An identical list is skipped rather than paid
	// for again. encoding/json sorts map keys, so equal data hashes equally.
	encoded, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("fingerprint users: %w", err)
	}
	hash := sha256.Sum256(encoded)
	if e.usersHashSet && hash == e.usersHash {
		e.log.Debug("user list unchanged; swap skipped")
		return nil
	}
	// Forgotten until this update has fully succeeded: a list that failed
	// halfway must be applied again when it is re-sent, not skipped.
	e.usersHashSet = false

	manager := service.FromContext[adapter.InboundManager](e.ctx)
	if manager == nil {
		return fmt.Errorf("inbound manager unavailable")
	}

	for tag, users := range data.ByTag {
		inbound, ok := manager.Get(tag)
		if !ok {
			// The panel may know about an inbound this instance does not yet
			// have, between a config push and its apply. Not fatal.
			e.log.Debug("no such inbound for user update", "tag", tag)
			continue
		}
		if err := applyUsers(inbound, users); err != nil {
			return fmt.Errorf("inbound %s: %w", tag, err)
		}
	}

	if err := e.syncStatsUsersLocked(data.StatsUsers); err != nil {
		return err
	}

	// The address limits arrive with the users they apply to, because that is
	// the message that changes when a user is added, edited or removed —
	// carrying them separately would let the two drift.
	e.limits.setLimits(data.IPLimits)
	known := make(map[string]bool, len(data.StatsUsers))
	for _, name := range data.StatsUsers {
		known[name] = true
	}
	e.limits.forget(known)

	e.usersHash, e.usersHashSet = hash, true
	return nil
}

// compactJSON strips insignificant whitespace, so two encodings of the same
// configuration compare equal. Invalid JSON is returned unchanged; the decoder
// will reject it with a better message than this could.
func compactJSON(raw json.RawMessage) json.RawMessage {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return raw
	}
	return buf.Bytes()
}

// applyUsers dispatches on the inbound's protocol.
//
// The type assertions are the contract with the sing-box patches: each protocol
// gains an update method the underlying service already had. An inbound of a
// known type that does not implement it means the patch is missing from this
// build, and that must fail loudly — silently skipping it would leave user
// changes with no effect at all and nothing to explain why.
func applyUsers(inbound adapter.Inbound, users []proto.User) error {
	switch inbound.Type() {
	case C.TypeVLESS:
		u, ok := inbound.(adapter.UpdatableInbound[option.VLESSUser])
		if !ok {
			return fmt.Errorf("this sing-box build cannot hot-swap vless users")
		}
		list := make([]option.VLESSUser, 0, len(users))
		for _, x := range users {
			list = append(list, option.VLESSUser{Name: x.Name, UUID: x.UUID, Flow: x.Flow})
		}
		return u.UpdateUsers(list)

	case C.TypeHysteria2:
		u, ok := inbound.(adapter.UpdatableInbound[option.Hysteria2User])
		if !ok {
			return fmt.Errorf("this sing-box build cannot hot-swap hysteria2 users")
		}
		list := make([]option.Hysteria2User, 0, len(users))
		for _, x := range users {
			list = append(list, option.Hysteria2User{Name: x.Name, Password: x.Password})
		}
		return u.UpdateUsers(list)
	case C.TypeTUIC:
		u, ok := inbound.(adapter.UpdatableInbound[option.TUICUser])
		if !ok {
			return fmt.Errorf("this sing-box build cannot hot-swap tuic users")
		}
		list := make([]option.TUICUser, 0, len(users))
		for _, x := range users {
			list = append(list, option.TUICUser{Name: x.Name, UUID: x.UUID, Password: x.Password})
		}
		return u.UpdateUsers(list)
	case C.TypeAnyTLS:
		u, ok := inbound.(adapter.UpdatableInbound[option.AnyTLSUser])
		if !ok {
			return fmt.Errorf("this sing-box build cannot hot-swap anytls users")
		}
		list := make([]option.AnyTLSUser, 0, len(users))
		for _, x := range users {
			list = append(list, option.AnyTLSUser{Name: x.Name, Password: x.Password})
		}
		return u.UpdateUsers(list)

	case C.TypeShadowsocks:
		u, ok := inbound.(adapter.UpdatableShadowsocksInbound)
		if !ok {
			// Reached when the listener was built with no users at all:
			// sing-box picks a single-user inbound type in that case, and that
			// type has no update method. The panel avoids it by never sending
			// an empty Shadowsocks user list, so this is a bug report, not a
			// condition to paper over.
			return fmt.Errorf(
				"shadowsocks inbound cannot hot-swap users; it was built without any")
		}
		list := make([]option.ShadowsocksUser, 0, len(users))
		for _, x := range users {
			list = append(list, option.ShadowsocksUser{Name: x.Name, Password: x.Password})
		}
		return u.UpdateUsersByOptions(list)

	default:
		return fmt.Errorf("unsupported inbound type %q", inbound.Type())
	}
}

// statsUserUpdater is the patched sing-box statistics service. It is reached
// through an anonymous interface so this package does not depend on sing-box's
// experimental tree.
type statsUserUpdater interface {
	UpdateUsers(users []string)
}

func (e *Engine) syncStatsUsersLocked(names []string) error {
	v2ray := service.FromContext[adapter.V2RayServer](e.ctx)
	if v2ray == nil {
		return fmt.Errorf("v2ray_api is not enabled in this configuration")
	}
	stats := v2ray.StatsService()
	if stats == nil {
		return fmt.Errorf("v2ray_api statistics are not enabled")
	}
	updater, ok := stats.(statsUserUpdater)
	if !ok {
		return fmt.Errorf("this sing-box build cannot update the statistics allowlist")
	}
	updater.UpdateUsers(names)
	return nil
}

// marshalOptions is used by tests to confirm a config round-trips.
func marshalOptions(o option.Options) ([]byte, error) { return singJSON.Marshal(o) }
