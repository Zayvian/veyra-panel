package proto

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// The wire format is the contract with the panel, which defines its own copy of
// these structs. Both sides decode the same samples in protocol/testdata and
// must encode them back unchanged; see protocol/PROTOCOL.md.
func TestProtocolSamplesRoundTrip(t *testing.T) {
	payload := map[string]func() any{
		TypeHello:  func() any { return new(Hello) },
		TypeConfig: func() any { return new(ConfigData) },
		TypeUsers:  func() any { return new(UsersData) },
		TypeError:  func() any { return new(ErrorData) },
		TypeState:  func() any { return new(StateData) },
		TypeStats:  func() any { return new(StatsData) },
		TypeOnline: func() any { return new(OnlineData) },
		TypeOK:     nil,
		TypePing:   nil,
		TypePong:   nil,
	}
	files, err := filepath.Glob(filepath.Join("..", "..", "..", "protocol", "testdata", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no protocol samples found: %v", err)
	}
	seen := map[string]bool{}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			var env Envelope
			if err := json.Unmarshal(raw, &env); err != nil {
				t.Fatal(err)
			}
			mk, known := payload[env.Type]
			if !known {
				t.Fatalf("sample has type %q, which the node does not know", env.Type)
			}
			seen[env.Type] = true
			var data any
			if mk != nil {
				data = mk()
				if err := json.Unmarshal(env.Data, data); err != nil {
					t.Fatal(err)
				}
			} else if len(env.Data) != 0 {
				t.Fatalf("%s carries no payload, but the sample has one", env.Type)
			}
			out, err := Encode(env.Type, env.ID, data)
			if err != nil {
				t.Fatal(err)
			}
			if !sameJSON(t, raw, out) {
				t.Fatalf("round trip changed the message\n sample: %s\nencoded: %s", raw, out)
			}
		})
	}
	for typ := range payload {
		if !seen[typ] {
			t.Errorf("no sample in protocol/testdata for %q", typ)
		}
	}
}

func sameJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatal(err)
	}
	return reflect.DeepEqual(x, y)
}
