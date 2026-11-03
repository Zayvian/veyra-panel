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

// Hysteria2 inbounds that had salamander obfuscation on lose it on both the
// node's side and the subscription's, and keep everything else.
func TestHysteria2ObfsIsRemoved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	n := &Node{Name: "jp", Address: "jp.example.com", Enabled: true}
	if err := st.CreateNode(n); err != nil {
		t.Fatal(err)
	}
	obfs := &Inbound{NodeID: n.ID, Tag: "hy2", Protocol: ProtoHysteria2, Port: 8443, Enabled: true,
		Config: `{"type":"hysteria2","tag":"hy2","listen_port":8443,"obfs":{"type":"salamander","password":"p"},"masquerade":{"type":"proxy","url":"https://www.bing.com"}}`,
		Client: `{"sni":"jp.example.com","obfs":"p"}`}
	plain := &Inbound{NodeID: n.ID, Tag: "hy2-plain", Protocol: ProtoHysteria2, Port: 9443, Enabled: true,
		Config: `{"type":"hysteria2","tag":"hy2-plain","listen_port":9443}`, Client: `{"sni":"jp.example.com"}`}
	for _, in := range []*Inbound{obfs, plain} {
		if err := st.CreateInbound(in); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.db.Exec(`DELETE FROM schema_migrations WHERE name = '0017_remove_hysteria2_obfs.sql'`); err != nil {
		t.Fatal(err)
	}
	st.Close()

	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	got, err := st.Inbound(obfs.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Config != `{"type":"hysteria2","tag":"hy2","listen_port":8443,"masquerade":{"type":"proxy","url":"https://www.bing.com"}}` ||
		got.Client != `{"sni":"jp.example.com"}` {
		t.Fatalf("after the migration: %s / %s", got.Config, got.Client)
	}
	if p, _ := st.Inbound(plain.ID); p.Config != plain.Config || p.Client != plain.Client {
		t.Fatalf("an inbound without obfs changed: %s / %s", p.Config, p.Client)
	}
}
