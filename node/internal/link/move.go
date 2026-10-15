package link

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// movePanel switches the node to the panel's new address: check it, write it
// down, then use it.
//
// Checking first is the point. The panel sends this as soon as it serves the
// new name, but DNS or the new certificate may still be on their way, and a
// node that moved to an address it cannot reach would be cut off from the only
// thing that could tell it otherwise. Staying put costs nothing: the old name
// keeps working until the administrator retires it, and the panel asks again
// on every reconnect.
func (c *Client) movePanel(ctx context.Context, panelURL string) (moved bool, err error) {
	u, err := url.Parse(panelURL)
	if err != nil || u.Hostname() == "" ||
		strings.Trim(u.Path, "/") != "" || u.RawQuery != "" || u.User != nil {
		return false, fmt.Errorf("new panel address %q is not a base URL", panelURL)
	}
	panelURL = u.Scheme + "://" + u.Host
	// The same rules as the address the node was started with: https, or
	// plain http to this machine only.
	wsURL, err := controlURL(panelURL)
	if err != nil {
		return false, err
	}
	c.stateMu.Lock()
	same := c.panelURL == panelURL
	c.stateMu.Unlock()
	if same {
		return false, nil
	}

	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := c.probe(probeCtx, panelURL); err != nil {
		return false, err
	}
	if c.cfg.EnvFile != "" {
		switch err := setEnv(c.cfg.EnvFile, "SKYSBX_PANEL", panelURL); {
		case errors.Is(err, fs.ErrNotExist):
			c.log.Warn("no environment file to remember the new panel address in; "+
				"it lasts until this process exits", "file", c.cfg.EnvFile)
		case err != nil:
			return false, fmt.Errorf("remember the new panel address: %w", err)
		}
	}

	c.stateMu.Lock()
	c.panelURL, c.wsURL = panelURL, wsURL
	c.stateMu.Unlock()
	c.log.Info("panel moved; reconnecting", "panel", panelURL)
	return true, nil
}

// probePanel asks the new address for the control channel without a token. The
// panel answers 401; anything else — no answer, a certificate for another
// name, a page that is not the panel — means it is not ready to be moved to.
func probePanel(ctx context.Context, panelURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		panelURL+"/api/v1/node/connect", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach %s: %w", panelURL, err)
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		return fmt.Errorf("%s answered %s, not like a Veyra panel", panelURL, resp.Status)
	}
	return nil
}

// setEnv replaces one KEY=value line of a systemd environment file, or adds it,
// leaving every other line as it was. The file is replaced by rename, so a crash
// part way leaves the old file rather than half of one: it holds the token.
func setEnv(path, key, value string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var out bytes.Buffer
	found := false
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(strings.TrimSpace(line), key+"=") {
			if found {
				continue
			}
			line, found = key+"="+value, true
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if !found {
		out.WriteString(key + "=" + value + "\n")
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(out.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
