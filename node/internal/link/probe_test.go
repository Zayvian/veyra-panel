package link

import (
	"context"
	"net"
	"strconv"
	"testing"

	"github.com/Zayvian/veyra-node/internal/proto"
)

func TestRunProbe(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	open := ln.Addr().(*net.TCPAddr).Port

	// A port that was just open and is now closed refuses at once.
	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := ln2.Addr().(*net.TCPAddr).Port
	ln2.Close()

	res := runProbe(context.Background(), proto.ProbeData{TimeoutMS: 2000, Targets: []proto.ProbeTarget{
		{Key: "open", Host: "127.0.0.1", Port: open},
		{Key: "closed", Host: "127.0.0.1", Port: closed},
		{Key: "bad", Host: "127.0.0.1", Port: 0},
	}})
	if len(res.Results) != 3 {
		t.Fatalf("results: %+v", res)
	}
	got := map[string]proto.ProbeResult{}
	for _, r := range res.Results {
		got[r.Key] = r
	}
	if !got["open"].OK || got["open"].Err != "" {
		t.Errorf("open port: %+v", got["open"])
	}
	if got["closed"].OK || got["closed"].Err == "" {
		t.Errorf("closed port %s: %+v", strconv.Itoa(closed), got["closed"])
	}
	if got["bad"].OK || got["bad"].Err != "bad target" {
		t.Errorf("bad target: %+v", got["bad"])
	}
}
