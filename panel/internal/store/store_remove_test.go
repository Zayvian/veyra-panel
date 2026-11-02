package store

import (
	"path/filepath"
	"strconv"
	"testing"
)

// A panel that ran the CDN and probe features must still open, with the
// leftovers gone, once they are removed.
func TestLeftoverCDNInboundsAreRemoved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	n := &Node{Name: "jp", Address: "jp.example.com", Enabled: true}
	if err := st.CreateNode(n); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateInbound(&Inbound{NodeID: n.ID, Tag: "keep", Protocol: ProtoVLESS, Port: 443,
		Config: "{}", Client: "{}", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	// What an older panel left behind.
	for _, q := range []string{
		`INSERT INTO inbounds (node_id, tag, protocol, port, config, client, enabled)
		 VALUES (` + strconv.FormatInt(n.ID, 10) + `, 'cdn', 'vless-ws', 2053, '{}', '{}', 1)`,
		`CREATE TABLE probe_nodes (node_id INTEGER PRIMARY KEY)`,
		`DELETE FROM schema_migrations WHERE name = '0015_remove_cdn_and_probe.sql'`,
	} {
		if _, err := st.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	st.Close()

	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var cdn, keep, probe int
	st.db.QueryRow(`SELECT COUNT(*) FROM inbounds WHERE protocol = 'vless-ws'`).Scan(&cdn)
	st.db.QueryRow(`SELECT COUNT(*) FROM inbounds WHERE tag = 'keep'`).Scan(&keep)
	st.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'probe_nodes'`).Scan(&probe)
	if cdn != 0 || keep != 1 || probe != 0 {
		t.Fatalf("cdn %d, keep %d, probe table %d", cdn, keep, probe)
	}
}
