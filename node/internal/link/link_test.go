package link

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Zayvian/veyra-node/internal/proto"
)

// fakeEngine records what the control channel asked the data plane to do, and
// can be made to fail on demand.
type fakeEngine struct {
	mu sync.Mutex

	configs []json.RawMessage
	users   []proto.UsersData

	configErr error
	usersErr  error

	stats     map[string]proto.Usage
	readCount int
	ackCount  int
	online    []string
	state     proto.StateData

	ipCounts     map[string]int
	enforceCount int
}

func (e *fakeEngine) State() proto.StateData {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state
}

func (e *fakeEngine) EnforceIPLimits(context.Context) (map[string]int, map[string]proto.Activity, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.enforceCount++
	return e.ipCounts, nil, nil
}

func (e *fakeEngine) ApplyConfig(_ context.Context, cfg json.RawMessage) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.configErr != nil {
		return e.configErr
	}
	e.configs = append(e.configs, cfg)
	return nil
}

func (e *fakeEngine) UpdateUsers(_ context.Context, u proto.UsersData) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.usersErr != nil {
		return e.usersErr
	}
	e.users = append(e.users, u)
	return nil
}

func (e *fakeEngine) Stats() (map[string]proto.Usage, *proto.SystemStats, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.readCount++
	return e.stats, nil, nil
}

func (e *fakeEngine) Online() ([]string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.online, nil
}

func (e *fakeEngine) SingboxVersion() string { return "1.14.0-test" }

func (e *fakeEngine) StatsReported() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ackCount++
}

func (e *fakeEngine) reads() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.readCount
}

func (e *fakeEngine) acks() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.ackCount
}

func (e *fakeEngine) counts() (int, int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.configs), len(e.users)
}

// fakePanel is the far side of the control channel: it accepts one node,
// records what it says, and can push commands at it.
type fakePanel struct {
	t    *testing.T
	srv  *httptest.Server
	recv chan proto.Envelope

	mu       sync.Mutex
	ws       *websocket.Conn
	tokens   []string
	attempts int
}

func newFakePanel(t *testing.T) *fakePanel {
	t.Helper()
	p := &fakePanel{t: t, recv: make(chan proto.Envelope, 64)}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/node/connect", func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.attempts++
		p.tokens = append(p.tokens, r.Header.Get("Authorization"))
		p.mu.Unlock()

		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		p.mu.Lock()
		p.ws = ws
		p.mu.Unlock()

		for {
			_, data, err := ws.Read(r.Context())
			if err != nil {
				return
			}
			var env proto.Envelope
			if json.Unmarshal(data, &env) == nil {
				select {
				case p.recv <- env:
				default:
				}
			}
		}
	})
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	return p
}

func (p *fakePanel) push(msgType string, id uint64, data any) {
	p.t.Helper()
	frame, err := proto.Encode(msgType, id, data)
	if err != nil {
		p.t.Fatalf("encode: %v", err)
	}
	p.mu.Lock()
	ws := p.ws
	p.mu.Unlock()
	if ws == nil {
		p.t.Fatal("no node connected")
	}
	if err := ws.Write(context.Background(), websocket.MessageText, frame); err != nil {
		p.t.Fatalf("push %s: %v", msgType, err)
	}
}

func (p *fakePanel) await(msgType string, within time.Duration) proto.Envelope {
	p.t.Helper()
	deadline := time.After(within)
	for {
		select {
		case env := <-p.recv:
			if env.Type == msgType {
				return env
			}
		case <-deadline:
			p.t.Fatalf("timed out waiting for %q", msgType)
		}
	}
}

func (p *fakePanel) dropConnection() {
	p.mu.Lock()
	ws := p.ws
	p.ws = nil
	p.mu.Unlock()
	if ws != nil {
		ws.Close(websocket.StatusInternalError, "dropped")
	}
}

func (p *fakePanel) connectAttempts() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.attempts
}

func start(t *testing.T, p *fakePanel, e Engine) *Client {
	t.Helper()
	c, err := New(Config{
		PanelURL: p.srv.URL, Token: "tok", Version: "0.1.0-test",
		Engine: e, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("control client did not stop")
		}
	})
	return c
}

// ── tests ───────────────────────────────────────────────────────────────────

func TestURLDerivation(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://panel.example.com", "wss://panel.example.com/api/v1/node/connect"},
		{"https://panel.example.com/", "wss://panel.example.com/api/v1/node/connect"},
		{"http://127.0.0.1:8080", "ws://127.0.0.1:8080/api/v1/node/connect"},
		{"https://example.com/panel", "wss://example.com/panel/api/v1/node/connect"},
	}
	for _, c := range cases {
		got, err := New(Config{PanelURL: c.in, Token: "t", Engine: &fakeEngine{}})
		if err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if got.wsURL != c.want {
			t.Errorf("%s -> %s, want %s", c.in, got.wsURL, c.want)
		}
	}

	if _, err := New(Config{PanelURL: "ftp://x", Token: "t", Engine: &fakeEngine{}}); err == nil {
		t.Error("a non-HTTP panel URL was accepted")
	}
	if _, err := New(Config{PanelURL: "https://x", Engine: &fakeEngine{}}); err == nil {
		t.Error("a missing token was accepted")
	}
}

func TestHelloIsSentWithToken(t *testing.T) {
	p := newFakePanel(t)
	start(t, p, &fakeEngine{})

	env := p.await(proto.TypeHello, 3*time.Second)
	var hello proto.Hello
	if err := json.Unmarshal(env.Data, &hello); err != nil {
		t.Fatalf("hello payload: %v", err)
	}
	if hello.Version != "0.1.0-test" || hello.SingboxVersion != "1.14.0-test" {
		t.Errorf("hello = %+v", hello)
	}
	if hello.OS == "" || hello.Arch == "" {
		t.Errorf("hello omits platform: %+v", hello)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.tokens) == 0 || p.tokens[0] != "Bearer tok" {
		t.Errorf("authorization header = %v", p.tokens)
	}
}

func TestConfigAndUsersReachTheEngine(t *testing.T) {
	p := newFakePanel(t)
	e := &fakeEngine{}
	start(t, p, e)
	p.await(proto.TypeHello, 3*time.Second)

	p.push(proto.TypeConfig, 7, proto.ConfigData{Config: json.RawMessage(`{"inbounds":[]}`)})
	if env := p.await(proto.TypeOK, 3*time.Second); env.ID != 7 {
		t.Errorf("ok correlates to id %d, want 7", env.ID)
	}

	p.push(proto.TypeUsers, 8, proto.UsersData{
		ByTag:      map[string][]proto.User{"vless-a": {{Name: "alice", UUID: "u"}}},
		StatsUsers: []string{"alice"},
	})
	if env := p.await(proto.TypeOK, 3*time.Second); env.ID != 8 {
		t.Errorf("ok correlates to id %d, want 8", env.ID)
	}

	configs, users := e.counts()
	if configs != 1 || users != 1 {
		t.Fatalf("engine saw %d configs and %d user pushes, want 1 and 1", configs, users)
	}
	if got := e.users[0].StatsUsers; len(got) != 1 || got[0] != "alice" {
		t.Errorf("stats allowlist did not reach the engine: %v", got)
	}
}

// A command the node cannot carry out has to be reported. Without it the panel
// keeps showing a configuration the node never adopted, and the only symptom is
// users who cannot connect.
func TestFailedCommandIsReported(t *testing.T) {
	p := newFakePanel(t)
	e := &fakeEngine{configErr: errNotToday}
	start(t, p, e)
	p.await(proto.TypeHello, 3*time.Second)

	p.push(proto.TypeConfig, 3, proto.ConfigData{Config: json.RawMessage(`{}`)})

	env := p.await(proto.TypeError, 3*time.Second)
	if env.ID != 3 {
		t.Errorf("error correlates to id %d, want 3", env.ID)
	}
	var ed proto.ErrorData
	if err := json.Unmarshal(env.Data, &ed); err != nil || ed.Message == "" {
		t.Errorf("error payload is empty: %v %v", ed, err)
	}
}

// A malformed payload must also produce an error reply rather than silence.
func TestMalformedPayloadIsReported(t *testing.T) {
	p := newFakePanel(t)
	start(t, p, &fakeEngine{})
	p.await(proto.TypeHello, 3*time.Second)

	frame, _ := json.Marshal(proto.Envelope{
		Type: proto.TypeUsers, ID: 5, Data: json.RawMessage(`"not an object"`)})
	p.mu.Lock()
	ws := p.ws
	p.mu.Unlock()
	ws.Write(context.Background(), websocket.MessageText, frame)

	if env := p.await(proto.TypeError, 3*time.Second); env.ID != 5 {
		t.Errorf("error correlates to id %d, want 5", env.ID)
	}
}

func TestPingIsAnswered(t *testing.T) {
	p := newFakePanel(t)
	start(t, p, &fakeEngine{})
	p.await(proto.TypeHello, 3*time.Second)

	p.push(proto.TypePing, 42, nil)
	if env := p.await(proto.TypePong, 3*time.Second); env.ID != 42 {
		t.Errorf("pong correlates to id %d, want 42", env.ID)
	}
}

// Losing the channel must be the node's problem to solve. The panel re-sends
// everything on hello, so there is nothing to resume — only to reconnect.
func TestReconnectsAndSaysHelloAgain(t *testing.T) {
	p := newFakePanel(t)
	start(t, p, &fakeEngine{})
	p.await(proto.TypeHello, 3*time.Second)

	if got := p.connectAttempts(); got != 1 {
		t.Fatalf("connect attempts = %d, want 1", got)
	}
	p.dropConnection()

	// A second hello proves the node re-established the channel on its own.
	p.await(proto.TypeHello, 10*time.Second)
	if got := p.connectAttempts(); got < 2 {
		t.Fatalf("connect attempts = %d, want at least 2", got)
	}
}

func TestStatsAndOnlineAreReported(t *testing.T) {
	// The production intervals are 30s, which is far too long for a test. They
	// are package-level so a test can shorten them; nothing else writes to them.
	stats, online := statsEvery, onlineEvery
	t.Cleanup(func() { restore(stats, online) })
	setIntervals(50*time.Millisecond, 50*time.Millisecond)

	p := newFakePanel(t)
	e := &fakeEngine{
		stats:  map[string]proto.Usage{"alice": {Up: 10, Down: 20}},
		online: []string{"alice"},
	}
	start(t, p, e)
	p.await(proto.TypeHello, 3*time.Second)

	var sd proto.StatsData
	if err := json.Unmarshal(p.await(proto.TypeStats, 3*time.Second).Data, &sd); err != nil {
		t.Fatalf("stats payload: %v", err)
	}
	if sd.Users["alice"].Down != 20 {
		t.Errorf("stats = %+v", sd.Users)
	}

	var od proto.OnlineData
	if err := json.Unmarshal(p.await(proto.TypeOnline, 3*time.Second).Data, &od); err != nil {
		t.Fatalf("online payload: %v", err)
	}
	if len(od.Users) != 1 || od.Users[0] != "alice" {
		t.Errorf("online = %v", od.Users)
	}
}

func setIntervals(stats, online time.Duration) {
	statsEvery, onlineEvery = stats, online
}

func restore(stats, online time.Duration) {
	statsEvery, onlineEvery = stats, online
}

var errNotToday = &engineError{"certificate file not found"}

type engineError struct{ msg string }

func (e *engineError) Error() string { return e.msg }

// Traffic that was read but never reached the panel must not be discarded.
// Anything the node drops here is traffic nobody is billed for, and there is no
// later opportunity to notice.
func TestStatsBaselineAdvancesOnlyOnSuccess(t *testing.T) {
	stats, online := statsEvery, onlineEvery
	t.Cleanup(func() { restore(stats, online) })
	setIntervals(40*time.Millisecond, time.Hour)

	p := newFakePanel(t)
	e := &fakeEngine{stats: map[string]proto.Usage{"alice": {Up: 1, Down: 2}}}
	start(t, p, e)
	p.await(proto.TypeHello, 3*time.Second)
	p.await(proto.TypeStats, 3*time.Second)

	// One acknowledgement per delivered report, and never more.
	deadline := time.Now().Add(2 * time.Second)
	for e.acks() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("a delivered report was never acknowledged")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got, acks := e.reads(), e.acks(); acks > got {
		t.Fatalf("%d acknowledgements for %d reads", acks, got)
	}
}

// The join token goes out in an Authorization header on every reconnect. Over
// plain HTTP that is a long-lived credential for this node, in the clear, on a
// schedule — and it is worth more than the traffic it protects, because it lets
// the holder be this node.
func TestPlainHTTPPanelIsRefused(t *testing.T) {
	for _, tc := range []struct {
		url string
		ok  bool
	}{
		{"https://panel.example.com", true},
		{"https://panel.example.com:8443/base", true},
		{"http://panel.example.com", false},
		{"http://203.0.113.9:8080", false},
		// Loopback is where the protocol is developed and tested, and there is
		// no network there to read the token off.
		{"http://127.0.0.1:8080", true},
		{"http://localhost:8080", true},
		{"http://[::1]:8080", true},
		{"ftp://panel.example.com", false},
	} {
		_, err := New(Config{PanelURL: tc.url, Token: "t", Engine: &fakeEngine{}})
		if tc.ok && err != nil {
			t.Errorf("New(%q) = %v, want accepted", tc.url, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("New(%q) was accepted; the token would go out in the clear", tc.url)
		}
	}
}
