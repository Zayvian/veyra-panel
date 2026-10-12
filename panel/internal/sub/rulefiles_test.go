package sub

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

// redirect sends every request to srv, whatever host it names.
type redirect struct{ srv *httptest.Server }

func (r redirect) RoundTrip(req *http.Request) (*http.Response, error) {
	u, _ := url.Parse(r.srv.URL)
	req = req.Clone(req.Context())
	req.URL.Scheme, req.URL.Host = u.Scheme, u.Host
	return http.DefaultTransport.RoundTrip(req)
}

func TestRuleFiles(t *testing.T) {
	var hits atomic.Int32
	var fail atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if fail.Load() {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		if r.URL.Path != "/MetaCubeX/meta-rules-dat/sing/geo/geosite/cn.srs" {
			t.Errorf("fetched %s", r.URL.Path)
		}
		w.Write([]byte("list"))
	}))
	defer srv.Close()

	now := time.Unix(1_700_000_000, 0)
	defer func(f func() time.Time) { nowFunc = f }(nowFunc)
	nowFunc = func() time.Time { return now }

	c := &RuleFiles{Client: &http.Client{Transport: redirect{srv}}, Refresh: time.Hour}
	ctx := context.Background()

	if _, err := c.Get(ctx, "../../etc/passwd"); !errors.Is(err, ErrUnknownRuleFile) {
		t.Fatalf("unlisted name: %v", err)
	}
	for range 2 {
		if b, err := c.Get(ctx, "geosite-cn.srs"); err != nil || string(b) != "list" {
			t.Fatalf("get: %q %v", b, err)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("fetched %d times within the refresh period", hits.Load())
	}

	// Stale, and upstream down: the old copy is still served.
	now = now.Add(2 * time.Hour)
	fail.Store(true)
	if b, err := c.Get(ctx, "geosite-cn.srs"); err != nil || string(b) != "list" {
		t.Fatalf("stale fallback: %q %v", b, err)
	}
	// Never fetched, and upstream down: an error.
	if _, err := c.Get(ctx, "geoip-cn.srs"); err == nil {
		t.Fatal("no copy and no upstream, yet no error")
	}
}
