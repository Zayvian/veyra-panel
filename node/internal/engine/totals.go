package engine

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/experimental/v2rayapi"
	"github.com/sagernet/sing/service"

	"github.com/Zayvian/veyra-node/internal/proto"
)

// Counter names sing-box's statistics service uses.
const (
	userPrefix     = "user>>>"
	uplinkSuffix   = ">>>traffic>>>uplink"
	downlinkSuffix = ">>>traffic>>>downlink"
)

// statsQuerier is the in-process statistics service. Being in the same process
// means the counters can be read directly; the gRPC endpoint sing-box exposes
// for the same data would mean a listener, a client and generated stubs to
// reach a value already in memory.
type statsQuerier interface {
	QueryStats(context.Context, *v2rayapi.QueryStatsRequest) (*v2rayapi.QueryStatsResponse, error)
}

// readTotals returns the cumulative per-user byte counts.
//
// Reset_ is deliberately false. Resetting would hand back exact deltas with no
// bookkeeping, but it also consumes them: if the report that follows fails to
// reach the panel, that interval's traffic is gone with no way to recover it.
// Reading without resetting means a failed report costs nothing — the next one
// covers both intervals.
func (e *Engine) readTotals(ctx context.Context) (map[string]proto.Usage, error) {
	v2ray := service.FromContext[adapter.V2RayServer](ctx)
	if v2ray == nil {
		return nil, fmt.Errorf("v2ray_api is not enabled in this configuration")
	}
	querier, ok := v2ray.StatsService().(statsQuerier)
	if !ok {
		return nil, fmt.Errorf("the statistics service cannot be queried")
	}

	resp, err := querier.QueryStats(ctx, &v2rayapi.QueryStatsRequest{
		Patterns: []string{userPrefix},
	})
	if err != nil {
		return nil, fmt.Errorf("query stats: %w", err)
	}

	out := map[string]proto.Usage{}
	for _, stat := range resp.GetStat() {
		name, direction, ok := splitUserCounter(stat.GetName())
		if !ok {
			continue
		}
		usage := out[name]
		switch direction {
		case "up":
			usage.Up = stat.GetValue()
		case "down":
			usage.Down = stat.GetValue()
		}
		out[name] = usage
	}
	return out, nil
}

// splitUserCounter pulls the user name and direction out of a counter name like
// "user>>>alice>>>traffic>>>uplink".
func splitUserCounter(counter string) (name, direction string, ok bool) {
	rest, found := strings.CutPrefix(counter, userPrefix)
	if !found {
		return "", "", false
	}
	if n, found := strings.CutSuffix(rest, uplinkSuffix); found {
		return n, "up", n != ""
	}
	if n, found := strings.CutSuffix(rest, downlinkSuffix); found {
		return n, "down", n != ""
	}
	return "", "", false
}

var startedAt = time.Now()

// systemStats reports what the panel displays about the host. Deliberately
// cheap: memory from the Go runtime rather than the OS, because a node exists
// to move packets and this runs every thirty seconds.
func systemStats() *proto.SystemStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return &proto.SystemStats{
		MemUsed: int64(m.Sys),
		Uptime:  int64(time.Since(startedAt).Seconds()),
	}
}
