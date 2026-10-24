package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/discord"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

// gatewayFixture is a fake Discord: REST history for channel 3 and a
// scripted Gateway.
type gatewayFixture struct {
	t        *testing.T
	server   *httptest.Server
	db       *store.DB
	mu       sync.Mutex
	history  []string // owner message IDs, oldest first
	reads    atomic.Int32
	conns    atomic.Int32
	script   func(n int32, path string, c *websocket.Conn)
	status   atomic.Pointer[string]
	logs     bytes.Buffer
	logMu    sync.Mutex
	notified atomic.Int32
}

func newGatewayFixture(t *testing.T, script func(n int32, path string, c *websocket.Conn)) *gatewayFixture {
	f := &gatewayFixture{t: t, script: script}
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "gateway.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = db.ActivateNaturalScan(t.Context(), "3", "100"); err != nil {
		t.Fatal(err)
	}
	f.db = db
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/applications/@me":
			fmt.Fprint(w, `{"flags":262144}`)
		case r.URL.Path == "/gateway/bot":
			fmt.Fprintf(w, `{"url":"ws://%s/ws","session_start_limit":{"remaining":10}}`, r.Host)
		case r.URL.Path == "/channels/3/messages" && r.URL.Query().Get("limit") == "1":
			fmt.Fprint(w, `[]`)
		case r.URL.Path == "/channels/3/messages":
			f.reads.Add(1)
			f.mu.Lock()
			var page []map[string]any
			at := time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
			for i := len(f.history) - 1; i >= 0; i-- {
				id := f.history[i]
				if before := r.URL.Query().Get("before"); before == "" || compareIntakeIDs(id, before) < 0 {
					page = append(page, map[string]any{"id": id, "channel_id": "3", "content": "private text", "timestamp": at, "author": map[string]any{"id": "7"}})
				}
			}
			f.mu.Unlock()
			_ = json.NewEncoder(w).Encode(page)
		default:
			c, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer func() { _ = c.CloseNow() }()
			f.script(f.conns.Add(1), r.URL.Path, c)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func compareIntakeIDs(a, b string) int {
	if len(a) != len(b) {
		return len(a) - len(b)
	}
	return strings.Compare(a, b)
}

func (f *gatewayFixture) add(id string) {
	f.mu.Lock()
	f.history = append(f.history, id)
	f.mu.Unlock()
}

func (f *gatewayFixture) start(safety time.Duration) {
	a := discord.New(discord.Options{APIURL: f.server.URL, Client: f.server.Client(), Token: secrets.NewToken("gateway-token")})
	intake := &messaging.Intake{DB: f.db, Reader: a, Owner: "7", Channels: []string{"3"}, Transport: "discord", Protect: func(_ context.Context, s string) (string, error) { return s, nil }}
	logger := slog.New(slog.NewTextHandler(lockedWriter{&f.logMu, &f.logs}, nil))
	n := &naturalIntake{intake: intake, run: a.RunGateway, notify: func() { f.notified.Add(1) }, status: func(s string) { f.status.Store(&s) }, reads: &discordReadLog{log: logger}, log: logger, safety: safety, backoff: [2]time.Duration{time.Millisecond, 5 * time.Millisecond}}
	ctx, cancel := context.WithCancel(f.t.Context())
	var wg sync.WaitGroup
	n.start(ctx, &wg)
	f.t.Cleanup(func() {
		cancel()
		wg.Wait()
		f.logMu.Lock()
		defer f.logMu.Unlock()
		if strings.Contains(f.logs.String(), "gateway-token") || strings.Contains(f.logs.String(), "private text") {
			f.t.Errorf("logs carry the token or message content:\n%s", f.logs.String())
		}
	})
}

func (f *gatewayFixture) pending() []string {
	messages, err := f.db.PendingNaturalMessages(context.Background(), "3", 100)
	if err != nil {
		f.t.Fatal(err)
	}
	var ids []string
	for _, m := range messages {
		ids = append(ids, m.SourceID)
	}
	return ids
}

func (f *gatewayFixture) await(what string, ok func() bool) {
	f.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			status := ""
			if s := f.status.Load(); s != nil {
				status = *s
			}
			f.t.Fatalf("timed out waiting for %s; status %q", what, status)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (f *gatewayFixture) statusContains(text string) bool {
	s := f.status.Load()
	return s != nil && strings.Contains(*s, text)
}

type lockedWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (l lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func gatewaySend(c *websocket.Conn, op int, d any, s int64, event string) {
	frame := map[string]any{"op": op, "d": d}
	if event != "" {
		frame["t"], frame["s"] = event, s
	}
	raw, _ := json.Marshal(frame)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = c.Write(ctx, websocket.MessageText, raw)
}

// gatewayServe acknowledges heartbeats until stop closes or the client leaves.
func gatewayServe(c *websocket.Conn, stop <-chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	for {
		_, raw, err := c.Read(ctx)
		if err != nil {
			return
		}
		var frame struct {
			Op int `json:"op"`
		}
		if json.Unmarshal(raw, &frame) == nil && frame.Op == 1 {
			gatewaySend(c, 11, nil, 0, "")
		}
	}
}

func TestGatewayStagesPushedMessagesAndCatchesUpAfterReconnect(t *testing.T) {
	events := make(chan string, 4)
	drop := make(chan struct{})
	var resumeURL string
	f := newGatewayFixture(t, func(n int32, path string, c *websocket.Conn) {
		gatewaySend(c, 10, map[string]any{"heartbeat_interval": 20}, 0, "")
		_, _, _ = c.Read(context.Background())
		if n == 1 {
			gatewaySend(c, 0, map[string]any{"session_id": "s", "resume_gateway_url": resumeURL}, 1, "READY")
			go func() {
				for id := range events {
					gatewaySend(c, 0, map[string]any{"id": id, "channel_id": "3", "content": "private text", "author": map[string]any{"id": "7"}}, 2, "MESSAGE_CREATE")
				}
			}()
			gatewayServe(c, drop)
			_ = c.Close(4000, "")
			return
		}
		gatewaySend(c, 0, nil, 3, "RESUMED")
		gatewayServe(c, nil)
	})
	resumeURL = strings.Replace(f.server.URL, "http", "ws", 1) + "/resume"
	f.start(50 * time.Millisecond)
	f.await("a connected Gateway", func() bool { return f.statusContains("Gateway connected") })

	f.add("101")
	events <- "101"
	events <- "101"
	f.await("the pushed message", func() bool { return len(f.pending()) == 1 })

	// Healthy: no reads happen without an event, even across many safety periods.
	time.Sleep(50 * time.Millisecond)
	reads := f.reads.Load()
	time.Sleep(300 * time.Millisecond)
	if got := f.reads.Load(); got != reads {
		t.Fatalf("%d reads while the Gateway was healthy", got-reads)
	}

	// A message written while the Gateway is down arrives by the catch-up read.
	close(events)
	f.add("102")
	close(drop)
	f.await("the catch-up read", func() bool { return len(f.pending()) == 2 })
	if ids := f.pending(); ids[0] != "101" || ids[1] != "102" {
		t.Fatalf("pending %v", ids)
	}
	if f.conns.Load() != 2 {
		t.Fatalf("%d connections", f.conns.Load())
	}
	f.await("a reconnected Gateway", func() bool { return f.statusContains("Gateway connected") })
	if f.notified.Load() == 0 {
		t.Fatal("the conversation service was never notified")
	}
	f.logMu.Lock()
	defer f.logMu.Unlock()
	if !strings.Contains(f.logs.String(), "Discord Gateway disconnected") {
		t.Fatalf("logs miss the disconnect:\n%s", f.logs.String())
	}
}

func TestGatewayFailureFallsBackToSafetyPolls(t *testing.T) {
	f := newGatewayFixture(t, func(_ int32, _ string, c *websocket.Conn) {
		gatewaySend(c, 10, map[string]any{"heartbeat_interval": 1000}, 0, "")
		_, _, _ = c.Read(context.Background())
		_ = c.Close(4014, "Disallowed intent(s).")
	})
	f.start(30 * time.Millisecond)
	f.await("a permanent failure", func() bool { return f.statusContains("permanently failed") && f.statusContains("Message Content") })
	f.add("101")
	f.await("a safety read", func() bool { return len(f.pending()) == 1 })
	reads := f.reads.Load()
	f.await("further safety reads", func() bool { return f.reads.Load() >= reads+2 })
	if f.conns.Load() != 1 {
		t.Fatalf("%d connections after a permanent close", f.conns.Load())
	}
}
