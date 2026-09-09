package store

import "testing"

func TestRatesPreserveRawTrafficAndFractionalBilling(t *testing.T) {
	s := openTemp(t)
	n := &Node{Name: "node", TokenHash: "hash", Address: "example.com", Country: "HK", Enabled: true}
	if err := s.CreateNode(n, 100); err != nil {
		t.Fatal(err)
	}
	u := &User{Name: "alice", VlessUUID: "uuid", Password: "pw", SSPassword: "ss", SubToken: "sub", Enabled: true}
	if err := s.CreateUser(u); err != nil {
		t.Fatal(err)
	}
	add := func(up, down int64) {
		t.Helper()
		if err := s.AddTraffic(n.ID, 1, map[int64][2]int64{u.ID: {up, down}}); err != nil {
			t.Fatal(err)
		}
	}
	// Ten small reports must have exactly the same cost as one large report.
	for i := 0; i < 10; i++ {
		add(1, 2)
	}
	got, _ := s.User(u.ID)
	if got.TrafficUsed != 3 || got.TrafficUp != 1 || got.TrafficDown != 2 {
		t.Fatalf("fractional billing: %+v", got)
	}
	n.RateMilli = 0
	if err := s.UpdateNode(n); err != nil {
		t.Fatal(err)
	}
	add(1000, 2000)
	got, _ = s.User(u.ID)
	if got.TrafficUsed != 3 {
		t.Fatal("0x charged quota")
	}
	var rawUp, rawDown int64
	if err := s.DB().QueryRow(`SELECT up,down FROM traffic WHERE user_id=?`, u.ID).Scan(&rawUp, &rawDown); err != nil {
		t.Fatal(err)
	}
	if rawUp != 1010 || rawDown != 2020 {
		t.Fatalf("raw traffic lost: %d %d", rawUp, rawDown)
	}
	n.RateMilli = 2000
	if err := s.UpdateNode(n); err != nil {
		t.Fatal(err)
	}
	add(10, 20)
	got, _ = s.User(u.ID)
	if got.TrafficUsed != 63 {
		t.Fatal("changed rate repriced history")
	}
	// Reset clears all billed counters and any sub-byte remainder.
	n.RateMilli = 100
	if err := s.UpdateNode(n); err != nil {
		t.Fatal(err)
	}
	add(9, 9)
	if err := s.ResetUserTraffic(u.ID); err != nil {
		t.Fatal(err)
	}
	add(1, 1)
	got, _ = s.User(u.ID)
	if got.TrafficUsed != 0 || got.TrafficUp != 0 || got.TrafficDown != 0 {
		t.Fatal("reset left billing state")
	}
	if err := s.AddTraffic(n.ID, 1, map[int64][2]int64{u.ID: {-1, 2}}); err == nil {
		t.Fatal("negative delta accepted")
	}
}
