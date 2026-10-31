package link

import (
	"context"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/Zayvian/veyra-node/internal/proto"
)

const (
	defaultProbeTimeout = 4 * time.Second
	maxProbeTimeout     = 15 * time.Second
	maxProbeTargets     = 512
	probeParallel       = 32
)

// probeDial is replaced in tests.
var probeDial = func(ctx context.Context, addr string) error {
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	return conn.Close()
}

// runProbe tries a TCP handshake with every target and reports which ones
// completed. Nothing is sent over a connection once it is made: whether the
// handshake gets through is the whole question, and anything more would make
// the probe look like a client to whatever is listening.
func runProbe(ctx context.Context, pd proto.ProbeData) proto.ProbeResultData {
	timeout := time.Duration(pd.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = defaultProbeTimeout
	}
	if timeout > maxProbeTimeout {
		timeout = maxProbeTimeout
	}
	targets := pd.Targets
	if len(targets) > maxProbeTargets {
		targets = targets[:maxProbeTargets]
	}

	out := proto.ProbeResultData{Results: make([]proto.ProbeResult, len(targets))}
	var wg sync.WaitGroup
	sem := make(chan struct{}, probeParallel)
	for i, t := range targets {
		wg.Add(1)
		go func(i int, t proto.ProbeTarget) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			r := proto.ProbeResult{Key: t.Key}
			if t.Host == "" || t.Port < 1 || t.Port > 65535 {
				r.Err = "bad target"
				out.Results[i] = r
				return
			}
			dctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			start := time.Now()
			err := probeDial(dctx, net.JoinHostPort(t.Host, strconv.Itoa(t.Port)))
			if err != nil {
				r.Err = err.Error()
			} else {
				r.OK = true
				r.MS = int(time.Since(start) / time.Millisecond)
			}
			out.Results[i] = r
		}(i, t)
	}
	wg.Wait()
	return out
}
