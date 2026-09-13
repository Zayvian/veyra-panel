package sub

import (
	"encoding/base64"
	"github.com/Zayvian/veyra-panel/internal/store"
	"strings"
	"testing"
)

func TestShadowrocketGBStatus(t *testing.T) {
	u := &store.User{TrafficUp: 1 << 30, TrafficDown: 3 << 29, TrafficLimit: 200 << 30}
	entries := []Entry{{Name: "香港无限流量", Protocol: store.ProtoAnyTLS, Address: "hk.example.com", Port: 443, Password: "pw"}}
	raw, err := base64.StdEncoding.DecodeString(ShadowrocketBase64(entries, u))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(raw), "STATUS=上传：1.00 GB | 下载：1.50 GB | 总量：200.00 GB\r\n") {
		t.Fatal(string(raw))
	}
	if strings.Count(string(raw), "anytls://") != 1 {
		t.Fatal("status changed entries")
	}
	u.TrafficLimit = 0
	raw, _ = base64.StdEncoding.DecodeString(ShadowrocketBase64(nil, u))
	if !strings.Contains(string(raw), "总量：不限") {
		t.Fatal(string(raw))
	}
}
