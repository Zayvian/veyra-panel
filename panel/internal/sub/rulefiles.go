package sub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// RulesPath is where the panel serves the rule lists, under the subscription
// prefix so it is reachable wherever subscriptions are: the subscription
// domain, and outside the admin's private path.
const RulesPath = "/sub/rules/"

// Clients download the lists from the panel rather than from GitHub. They
// already reach the panel — that is how they got the subscription — whereas
// GitHub is unreliable from China, and sing-box refuses to start at all when a
// rule set it has never had cannot be downloaded.
func ruleFileURL(base string, r ruleSet, ext string) string {
	return base + RulesPath + r.tag() + "." + ext
}

// ruleFile resolves a served file name ("geosite-cn.srs") to the upstream
// list, and refuses anything not in routeRules: this is a public endpoint,
// and must not become a way to make the panel fetch arbitrary URLs.
func ruleFile(name string) (url string, ok bool) {
	for _, r := range routeRules {
		for _, set := range r.sets {
			switch name {
			case set.tag() + ".mrs":
				return ruleSetURL("meta", "mrs", set), true
			case set.tag() + ".srs":
				return ruleSetURL("sing", "srs", set), true
			}
		}
	}
	return "", false
}

// RuleFiles is the panel's copy of the rule lists, refetched from upstream
// at most every refresh and kept in memory (about 1.2 MB in all). An old copy
// is served when upstream fails: a day-old list beats a client that cannot
// load its configuration.
type RuleFiles struct {
	Client  *http.Client
	Refresh time.Duration

	mu    sync.Mutex
	files map[string]*ruleCopy
}

type ruleCopy struct {
	mu      sync.Mutex // one fetch per file at a time
	data    []byte
	fetched time.Time
}

const maxRuleFile = 8 << 20

var ErrUnknownRuleFile = errors.New("unknown rule file")

// Get returns the named file, fetching it if the copy is missing or stale.
func (c *RuleFiles) Get(ctx context.Context, name string) ([]byte, error) {
	url, ok := ruleFile(name)
	if !ok {
		return nil, ErrUnknownRuleFile
	}
	c.mu.Lock()
	if c.files == nil {
		c.files = map[string]*ruleCopy{}
	}
	f := c.files[name]
	if f == nil {
		f = &ruleCopy{}
		c.files[name] = f
	}
	c.mu.Unlock()

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.data != nil && nowFunc().Sub(f.fetched) < c.Refresh {
		return f.data, nil
	}
	data, err := c.fetch(ctx, url)
	if err != nil {
		if f.data != nil {
			return f.data, nil
		}
		return nil, err
	}
	f.data, f.fetched = data, nowFunc()
	return data, nil
}

func (c *RuleFiles) fetch(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := c.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxRuleFile+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > maxRuleFile {
		return nil, fmt.Errorf("fetch %s: %d bytes", url, len(data))
	}
	return data, nil
}
