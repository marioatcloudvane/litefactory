package discord_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/cloudvane/litefactory/internal/messaging/discord"
	"github.com/cloudvane/litefactory/internal/secrets"
)

// fakeGateway serves /gateway/bot and hands each WebSocket connection, with
// its path and number, to script.
type fakeGateway struct {
	t      *testing.T
	server *httptest.Server
	mu     sync.Mutex
	count  int
	script func(n int, path string, c *websocket.Conn)
}

func newFakeGateway(t *testing.T, script func(n int, path string, c *websocket.Conn)) *fakeGateway {
	g := &fakeGateway{t: t, script: script}
	g.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gateway/bot" {
			if r.Header.Get("Authorization") != "Bot gateway-token" {
				t.Errorf("gateway/bot without the bot token")
			}
			fmt.Fprintf(w, `{"url":"ws://%s/ws","session_start_limit":{"total":1000,"remaining":999,"reset_after":0,"max_concurrency":1}}`, r.Host)
			return
		}
		if r.URL.Query().Get("v") != "10" || r.URL.Query().Get("encoding") != "json" {
			t.Errorf("gateway query %q", r.URL.RawQuery)
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = c.CloseNow() }()
		g.mu.Lock()
		g.count++
		n := g.count
		g.mu.Unlock()
		g.script(n, r.URL.Path, c)
	}))
	t.Cleanup(g.server.Close)
	return g
}

func (g *fakeGateway) adapter() *discord.Adapter {
	return discord.New(discord.Options{APIURL: g.server.URL, Client: g.server.Client(), Token: secrets.NewToken("gateway-token")})
}

type sentFrame struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
}

func gwRead(t *testing.T, c *websocket.Conn) sentFrame {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, raw, err := c.Read(ctx)
	if err != nil {
		t.Errorf("read: %v", err)
		return sentFrame{Op: -1}
	}
	var f sentFrame
	if err = json.Unmarshal(raw, &f); err != nil {
		t.Error(err)
	}
	return f
}

// gwReadOp skips heartbeats until op arrives, acknowledging them when ack is set.
func gwReadOp(t *testing.T, c *websocket.Conn, op int, ack bool) sentFrame {
	t.Helper()
	for {
		f := gwRead(t, c)
		if f.Op == op || f.Op == -1 {
			return f
		}
		if f.Op == 1 && ack {
			gwWrite(c, 11, nil, 0, "")
		}
	}
}

// drain reads until the client goes away, acknowledging heartbeats when ack is set.
func drain(c *websocket.Conn, ack bool) {
	for {
		_, raw, err := c.Read(context.Background())
		if err != nil {
			return
		}
		var f sentFrame
		if json.Unmarshal(raw, &f) == nil && f.Op == 1 && ack {
			gwWrite(c, 11, nil, 0, "")
		}
	}
}

func gwWrite(c *websocket.Conn, op int, d any, s int64, event string) {
	frame := map[string]any{"op": op, "d": d}
	if event != "" {
		frame["t"], frame["s"] = event, s
	}
	raw, _ := json.Marshal(frame)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = c.Write(ctx, websocket.MessageText, raw)
}

func hello(c *websocket.Conn, interval int) {
	gwWrite(c, 10, map[string]any{"heartbeat_interval": interval}, 0, "")
}

func ready(c *websocket.Conn, s int64, resume string) {
	gwWrite(c, 0, map[string]any{"session_id": "session-1", "resume_gateway_url": resume}, s, "READY")
}

func message(id, channel, author string, bot bool) map[string]any {
	return map[string]any{"id": id, "channel_id": channel, "content": "private text", "author": map[string]any{"id": author, "bot": bot}}
}

// recorder collects callbacks from RunGateway.
type recorder struct {
	mu       sync.Mutex
	messages []string
	readies  int
	states   []discord.GatewayState
	signal   chan struct{}
}

func (r *recorder) poke() {
	select {
	case r.signal <- struct{}{}:
	default:
	}
}

func newRecorder() *recorder { return &recorder{signal: make(chan struct{}, 64)} }

func (r *recorder) options() discord.GatewayOptions {
	return discord.GatewayOptions{Owner: "7", Channels: []string{"3"}, MinBackoff: time.Millisecond, MaxBackoff: 5 * time.Millisecond,
		OnMessage: func(channel string) {
			r.mu.Lock()
			r.messages = append(r.messages, channel)
			r.mu.Unlock()
			r.poke()
		},
		OnReady: func() {
			r.mu.Lock()
			r.readies++
			r.mu.Unlock()
			r.poke()
		},
		OnState: func(s discord.GatewayState) {
			r.mu.Lock()
			r.states = append(r.states, s)
			r.mu.Unlock()
		},
	}
}

func (r *recorder) await(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		r.mu.Lock()
		done := ok()
		r.mu.Unlock()
		if done {
			return
		}
		select {
		case <-r.signal:
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func run(t *testing.T, a *discord.Adapter, o discord.GatewayOptions) (context.CancelFunc, chan error) {
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- a.RunGateway(ctx, o) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Error("RunGateway did not stop")
		}
	})
	return cancel, done
}

func TestGatewayIdentifiesHeartbeatsAndReportsOwnerMessages(t *testing.T) {
	beats := make(chan json.RawMessage, 8)
	g := newFakeGateway(t, func(n int, path string, c *websocket.Conn) {
		hello(c, 20)
		identify := gwRead(t, c)
		var d struct {
			Token      string            `json:"token"`
			Intents    int               `json:"intents"`
			Compress   bool              `json:"compress"`
			Properties map[string]string `json:"properties"`
		}
		_ = json.Unmarshal(identify.D, &d)
		if identify.Op != 2 || d.Token != "gateway-token" || d.Intents != 1<<9|1<<15 || d.Compress {
			t.Errorf("identify %+v", d)
		}
		ready(c, 1, "")
		beat := gwReadOp(t, c, 1, false)
		beats <- beat.D
		gwWrite(c, 11, nil, 0, "")
		gwWrite(c, 0, message("101", "3", "7", false), 2, "MESSAGE_CREATE")
		gwWrite(c, 0, message("101", "3", "7", false), 3, "MESSAGE_CREATE")
		gwWrite(c, 0, message("102", "3", "7", true), 4, "MESSAGE_CREATE")
		gwWrite(c, 0, message("103", "3", "8", false), 5, "MESSAGE_CREATE")
		gwWrite(c, 0, message("104", "4", "7", false), 6, "MESSAGE_CREATE")
		gwWrite(c, 0, message("105", "3", "7", false), 7, "MESSAGE_UPDATE")
		gwWrite(c, 0, message("106", "3", "7", false), 8, "MESSAGE_CREATE")
		drain(c, true)
	})
	r := newRecorder()
	run(t, g.adapter(), r.options())
	r.await(t, "two owner messages", func() bool { return len(r.messages) >= 2 })
	if beat := <-beats; string(beat) != "1" {
		t.Errorf("heartbeat carried %s, want the last sequence 1", beat)
	}
	time.Sleep(50 * time.Millisecond)
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.messages) != 2 || r.readies != 1 {
		t.Fatalf("messages %v readies %d: want the owner's two messages in channel 3 once each", r.messages, r.readies)
	}
	if r.states[0].Phase != discord.GatewayConnecting || r.states[len(r.states)-1].Phase != discord.GatewayConnected {
		t.Fatalf("states %+v", r.states)
	}
}

func TestGatewayResumesAfterMissedHeartbeatACK(t *testing.T) {
	resumed := make(chan sentFrame, 1)
	var resumeURL string
	g := newFakeGateway(t, func(n int, path string, c *websocket.Conn) {
		hello(c, 20)
		switch n {
		case 1:
			gwRead(t, c)
			ready(c, 1, resumeURL)
			gwWrite(c, 0, message("101", "3", "8", false), 5, "MESSAGE_CREATE")
			// Never acknowledge: the client must drop this zombie connection.
			drain(c, false)
		case 2:
			if path != "/resume" {
				t.Errorf("resumed at %s, want the resume_gateway_url", path)
			}
			resumed <- gwRead(t, c)
			gwWrite(c, 0, nil, 6, "RESUMED")
			drain(c, true)
		}
	})
	resumeURL = strings.Replace(g.server.URL, "http", "ws", 1) + "/resume"
	r := newRecorder()
	run(t, g.adapter(), r.options())
	r.await(t, "the resumed session", func() bool { return r.readies == 2 })
	f := <-resumed
	var d struct {
		Token   string `json:"token"`
		Session string `json:"session_id"`
		Seq     int64  `json:"seq"`
	}
	_ = json.Unmarshal(f.D, &d)
	if f.Op != 6 || d.Session != "session-1" || d.Seq != 5 || d.Token != "gateway-token" {
		t.Fatalf("resume op %d %+v", f.Op, d)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	saw := false
	for _, s := range r.states {
		if s.Phase == discord.GatewayReconnecting && s.Reason == "heartbeat ACK missed" {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("states %+v miss the missed ACK", r.states)
	}
}

func TestGatewayIdentifiesAgainAfterInvalidSession(t *testing.T) {
	second := make(chan sentFrame, 1)
	g := newFakeGateway(t, func(n int, path string, c *websocket.Conn) {
		hello(c, 1000)
		first := gwRead(t, c)
		switch n {
		case 1:
			ready(c, 1, "")
			gwWrite(c, 9, false, 0, "")
			drain(c, true)
		case 2:
			second <- first
			ready(c, 1, "")
			drain(c, true)
		}
	})
	r := newRecorder()
	run(t, g.adapter(), r.options())
	r.await(t, "a second READY", func() bool { return r.readies == 2 })
	if f := <-second; f.Op != 2 {
		t.Fatalf("after an invalid session the client sent op %d, want IDENTIFY", f.Op)
	}
}

func TestGatewayDisallowedIntentIsPermanent(t *testing.T) {
	g := newFakeGateway(t, func(n int, path string, c *websocket.Conn) {
		hello(c, 1000)
		gwRead(t, c)
		_ = c.Close(4014, "Disallowed intent(s).")
	})
	r := newRecorder()
	_, done := run(t, g.adapter(), r.options())
	select {
	case err := <-done:
		done <- err
		if err == nil || errors.Is(err, context.Canceled) {
			t.Fatal("4014 ended without its error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("4014 did not stop the Gateway")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	last := r.states[len(r.states)-1]
	if last.Phase != discord.GatewayFailed || !strings.Contains(last.Reason, "4014") || !strings.Contains(last.Reason, "Message Content") {
		t.Fatalf("final state %+v", last)
	}
	if g.count != 1 {
		t.Fatalf("connected %d times after a permanent close", g.count)
	}
}
