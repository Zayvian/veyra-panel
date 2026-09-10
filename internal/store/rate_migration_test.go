package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestRateMigrationPreservesExistingQuota(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	files, err := migrationFS.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.Name() >= "0008" {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + file.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations VALUES (?,0)`, file.Name()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO nodes (id,name,token_hash,address,created_at) VALUES (1,'old','hash','example.com',0);
  INSERT INTO users (id,name,vless_uuid,password,ss_password,sub_token,traffic_used,created_at) VALUES (1,'alice','uuid','pw','ss','token',12345,0);
  INSERT INTO inbounds (id,node_id,tag,protocol,port,config,client) VALUES (1,1,'old-inbound','shadowsocks',8388,'{}','{}');
  INSERT INTO user_inbounds (user_id,inbound_id) VALUES (1,1)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	n, err := s.Node(1)
	if err != nil || n.RateMilli != 1000 || n.SortOrder != 0 {
		t.Fatalf("old node rate: %+v %v", n, err)
	}
	in, err := s.Inbound(1)
	if err != nil || in.SortOrder != 0 || in.Tag != "old-inbound" {
		t.Fatal("inbound migration changed existing data")
	}
	ids, err := s.UserInboundIDs(1)
	if err != nil || len(ids) != 1 || ids[0] != 1 {
		t.Fatal("migration changed access restrictions")
	}
	u, err := s.User(1)
	if err != nil || u.TrafficUsed != 12345 || u.TrafficDown != 12345 || u.TrafficUp != 0 {
		t.Fatalf("old quota changed: %+v %v", u, err)
	}
}
