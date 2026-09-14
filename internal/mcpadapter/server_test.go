package mcpadapter_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/mcpadapter"
	"github.com/cloudvane/litefactory/internal/submission"
)

type fakeBackend struct {
	list func(context.Context) (submission.Response, error)
	call func(context.Context, string, json.RawMessage) (submission.Response, error)
}

func (f fakeBackend) List(ctx context.Context) (submission.Response, error) {
	if f.list != nil {
		return f.list(ctx)
	}
	return submission.Response{Result: json.RawMessage(`{"tools":[]}`)}, nil
}
func (f fakeBackend) Call(ctx context.Context, name string, args json.RawMessage) (submission.Response, error) {
	if f.call != nil {
		return f.call(ctx, name, args)
	}
	return submission.Response{RequestID: "durable-1", Result: json.RawMessage(`{"valid":true}`)}, nil
}

type wireResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}
type pipeSession struct {
	in             *io.PipeWriter
	out            *io.PipeReader
	messages       chan []byte
	done, readDone chan struct{}
	err            error
	cancel         context.CancelFunc
}

func newPipeSession(t *testing.T, backend fakeBackend, consume bool) *pipeSession {
	t.Helper()
	in, writer := io.Pipe()
	reader, out := io.Pipe()
	ctx, cancel := context.WithCancel(t.Context())
	p := &pipeSession{in: writer, out: reader, messages: make(chan []byte, 32), done: make(chan struct{}), readDone: make(chan struct{}), cancel: cancel}
	go func() { defer close(p.done); p.err = (mcpadapter.Server{}).Run(ctx, in, out, backend) }()
	if consume {
		go func() {
			defer close(p.readDone)
			scanner := bufio.NewScanner(reader)
			scanner.Buffer(make([]byte, 4096), mcpadapter.MaxOutputBytes+1)
			for scanner.Scan() {
				select {
				case p.messages <- bytes.Clone(scanner.Bytes()):
				case <-ctx.Done():
					return
				}
			}
		}()
	} else {
		close(p.readDone)
	}
	t.Cleanup(func() {
		cancel()
		_ = writer.Close()
		_ = reader.Close()
		p.join(t)
		select {
		case <-p.readDone:
		case <-time.After(3 * time.Second):
			t.Error("output consumer failed to join")
		}
	})
	return p
}
func (p *pipeSession) send(t *testing.T, data string) {
	t.Helper()
	if _, err := io.WriteString(p.in, data+"\n"); err != nil {
		t.Fatalf("pipe input: %v", err)
	}
}
func (p *pipeSession) read(t *testing.T) wireResponse {
	t.Helper()
	select {
	case data := <-p.messages:
		var r wireResponse
		if err := json.Unmarshal(data, &r); err != nil || r.JSONRPC != "2.0" {
			t.Fatalf("stdout is not pure protocol JSON: %q %v", data, err)
		}
		return r
	case <-time.After(3 * time.Second):
		t.Fatal("protocol response timeout")
		return wireResponse{}
	}
}
func (p *pipeSession) join(t *testing.T) {
	t.Helper()
	select {
	case <-p.done:
	case <-time.After(3 * time.Second):
		t.Fatal("server failed to join owned workers")
	}
}
func (p *pipeSession) handshake(t *testing.T, version string) {
	t.Helper()
	p.send(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"`+version+`","capabilities":{},"clientInfo":{"name":"offline-test","version":"1"}}}`)
	r := p.read(t)
	if r.Error != nil || !bytes.Contains(r.Result, []byte(`"protocolVersion":"2025-11-25"`)) {
		t.Fatalf("initialize: %+v", r)
	}
	var negotiated struct {
		Capabilities map[string]json.RawMessage `json:"capabilities"`
	}
	if err := json.Unmarshal(r.Result, &negotiated); err != nil || len(negotiated.Capabilities) != 1 || negotiated.Capabilities["tools"] == nil {
		t.Fatalf("server advertised extra authority: %s %v", r.Result, err)
	}
	p.send(t, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
}
func requireCode(t *testing.T, r wireResponse, code int) {
	t.Helper()
	if r.Error == nil || r.Error.Code != code {
		t.Fatalf("error=%+v want code %d response=%+v", r.Error, code, r)
	}
}

func TestMCPServer_HandshakeStateAndVersions(t *testing.T) {
	for _, version := range []string{mcpadapter.ProtocolVersion, "2024-11-05", "future-version"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			p := newPipeSession(t, fakeBackend{}, true)
			p.send(t, `{"jsonrpc":"2.0","id":"before","method":"tools/list"}`)
			requireCode(t, p.read(t), -32002)
			p.handshake(t, version)
			p.send(t, `{"jsonrpc":"2.0","id":"list","method":"tools/list","params":{}}`)
			r := p.read(t)
			if r.Error != nil || string(r.ID) != `"list"` || string(r.Result) != `{"tools":[]}` {
				t.Fatalf("list=%+v", r)
			}
			p.send(t, `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{}}`)
			requireCode(t, p.read(t), -32600)
		})
	}
	t.Run("initialized required", func(t *testing.T) {
		p := newPipeSession(t, fakeBackend{}, true)
		p.send(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`)
		r := p.read(t)
		if r.Error != nil {
			t.Fatal(r.Error)
		}
		p.send(t, `{"jsonrpc":"2.0","method":"notifications/initialized","params":{"extra":true}}`)
		p.send(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
		requireCode(t, p.read(t), -32002)
	})
}

func TestMCPServer_ProtocolRefusalMatrix(t *testing.T) {
	cases := []struct {
		name, wire string
		code       int
	}{
		{"malformed", `{`, -32700},
		{"invalid utf8", string([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}), -32600},
		{"duplicate keys", `{"jsonrpc":"2.0","id":2,"id":3,"method":"tools/list"}`, -32600},
		{"null id", `{"jsonrpc":"2.0","id":null,"method":"tools/list"}`, -32600},
		{"fraction id", `{"jsonrpc":"2.0","id":1.5,"method":"tools/list"}`, -32600},
		{"object id", `{"jsonrpc":"2.0","id":{},"method":"tools/list"}`, -32600},
		{"array envelope", `[]`, -32600},
		{"wrong rpc", `{"jsonrpc":"1.0","id":2,"method":"tools/list"}`, -32600},
		{"unknown envelope", `{"jsonrpc":"2.0","id":2,"method":"tools/list","scope":"foreign"}`, -32600},
		{"unknown method", `{"jsonrpc":"2.0","id":2,"method":"resources/list"}`, -32601},
		{"list selector", `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{"mode":"research"}}`, -32602},
		{"call missing arguments", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"communication_submit"}}`, -32602},
		{"call scalar arguments", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"communication_submit","arguments":42}}`, -32602},
		{"duplicate argument key", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"communication_submit","arguments":{"task":"a","task":"b"}}}`, -32600},
		{"depth", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"x","arguments":{"nested":` + strings.Repeat(`[`, 34) + `0` + strings.Repeat(`]`, 34) + `}}}`, -32600},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			p := newPipeSession(t, fakeBackend{call: func(context.Context, string, json.RawMessage) (submission.Response, error) {
				calls.Add(1)
				return submission.Response{}, nil
			}}, true)
			p.handshake(t, mcpadapter.ProtocolVersion)
			p.send(t, tt.wire)
			requireCode(t, p.read(t), tt.code)
			if calls.Load() != 0 {
				t.Fatal("invalid protocol forwarded to backend")
			}
		})
	}
}

func TestMCPServer_ResultProjectionAndExactArguments(t *testing.T) {
	cases := []struct {
		name, domain string
		wantError    bool
	}{
		{"pending receipt", `{"Validation":{"valid":true},"Receipt":{"Latest":{"Input":"accepted","Engineering":"pending","Acceptance":"pending","Publication":"pending"}}}`, false},
		{"domain validation", `{"valid":false,"diagnostics":[{"code":"scope","message":"Use the bound story"}]}`, true},
		{"submit validation", `{"Validation":{"valid":false,"diagnostics":[{"code":"mcp_creation_refused"}]}}`, true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			args := `{ "result" : { "comment":"two  spaces and \u0061" } }`
			var calls atomic.Int32
			p := newPipeSession(t, fakeBackend{call: func(_ context.Context, name string, data json.RawMessage) (submission.Response, error) {
				calls.Add(1)
				if name != "communication_submit" || string(data) != args {
					return submission.Response{}, errors.New("argument bytes changed")
				}
				return submission.Response{RequestID: "application-123", Result: json.RawMessage(tt.domain)}, nil
			}}, true)
			p.handshake(t, mcpadapter.ProtocolVersion)
			p.send(t, `{"jsonrpc":"2.0","id":"connection-999","method":"tools/call","params":{"name":"communication_submit","arguments":`+args+`}}`)
			r := p.read(t)
			if r.Error != nil || string(r.ID) != `"connection-999"` {
				t.Fatalf("tool response=%+v", r)
			}
			var result struct {
				IsError    bool                          `json:"isError"`
				Content    []struct{ Type, Text string } `json:"content"`
				Structured submission.Response           `json:"structuredContent"`
			}
			if err := json.Unmarshal(r.Result, &result); err != nil {
				t.Fatal(err)
			}
			if result.IsError != tt.wantError || result.Structured.RequestID != "application-123" || len(result.Content) != 1 || result.Content[0].Type != "text" {
				t.Fatalf("projection=%+v", result)
			}
			var text submission.Response
			if err := json.Unmarshal([]byte(result.Content[0].Text), &text); err != nil || text.RequestID != result.Structured.RequestID || !bytes.Equal(text.Result, result.Structured.Result) {
				t.Fatalf("incompatible text projection: %+v %v", text, err)
			}
			if calls.Load() != 1 {
				t.Fatalf("backend calls=%d", calls.Load())
			}
		})
	}
}

func TestMCPServer_CancellationAndWorkerLimits(t *testing.T) {
	started := make(chan string, mcpadapter.MaxWorkers)
	cancelled := make(chan string, mcpadapter.MaxWorkers)
	releases := make(chan struct{})
	var active, maxActive atomic.Int32
	backend := fakeBackend{call: func(ctx context.Context, _ string, args json.RawMessage) (submission.Response, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := maxActive.Load(); n > old && !maxActive.CompareAndSwap(old, n); old = maxActive.Load() {
		}
		var v struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(args, &v); err != nil {
			return submission.Response{}, err
		}
		started <- v.Token
		select {
		case <-ctx.Done():
			cancelled <- v.Token
			return submission.Response{}, ctx.Err()
		case <-releases:
			return submission.Response{Result: json.RawMessage(`{"valid":true}`)}, nil
		}
	}}
	p := newPipeSession(t, backend, true)
	p.handshake(t, mcpadapter.ProtocolVersion)
	for i := 0; i < mcpadapter.MaxWorkers; i++ {
		p.send(t, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"communication_submit","arguments":{"token":"%d"}}}`, i+10, i))
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("worker did not start")
		}
	}
	p.send(t, `{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"communication_submit","arguments":{}}}`)
	requireCode(t, p.read(t), -32600)
	p.send(t, `{"jsonrpc":"2.0","id":99,"method":"tools/call","params":{"name":"communication_submit","arguments":{}}}`)
	requireCode(t, p.read(t), -32600)
	p.send(t, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":999}}`)
	p.send(t, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":10}}`)
	select {
	case token := <-cancelled:
		if token != "0" {
			t.Fatalf("cancelled wrong request %s", token)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("matching cancellation did not propagate")
	}
	requireCode(t, p.read(t), -32603)
	select {
	case token := <-cancelled:
		t.Fatalf("unrelated request cancelled: %s", token)
	default:
	}
	close(releases)
	for i := 1; i < mcpadapter.MaxWorkers; i++ {
		if r := p.read(t); r.Error != nil {
			t.Fatalf("unrelated request failed: %+v", r)
		}
	}
	if maxActive.Load() != mcpadapter.MaxWorkers {
		t.Fatalf("worker bound=%d", maxActive.Load())
	}
}

func TestMCPServer_EOFAndBlockedOutputJoin(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(fmt.Sprintf("blocked-output-%t", blocked), func(t *testing.T) {
			p := newPipeSession(t, fakeBackend{}, !blocked)
			p.send(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`)
			if !blocked {
				if r := p.read(t); r.Error != nil {
					t.Fatal(r.Error)
				}
			}
			if err := p.in.Close(); err != nil {
				t.Fatal(err)
			}
			p.join(t)
			if p.err != nil {
				t.Fatalf("EOF failed: %v", p.err)
			}
		})
	}
	t.Run("EOF joins admitted backend", func(t *testing.T) {
		started := make(chan struct{})
		joined := make(chan struct{})
		p := newPipeSession(t, fakeBackend{call: func(ctx context.Context, _ string, _ json.RawMessage) (submission.Response, error) {
			close(started)
			defer close(joined)
			<-ctx.Done()
			return submission.Response{}, ctx.Err()
		}}, true)
		p.handshake(t, mcpadapter.ProtocolVersion)
		p.send(t, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"communication_submit","arguments":{}}}`)
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("backend not started")
		}
		_ = p.in.Close()
		p.join(t)
		select {
		case <-joined:
		default:
			t.Fatal("Run returned before backend joined")
		}
	})
}

func TestMCPServer_SizeBounds(t *testing.T) {
	t.Run("full contribution plus envelope", func(t *testing.T) {
		arguments := `{"data":"` + strings.Repeat("x", (4<<20)-32) + `"}`
		var intact atomic.Bool
		p := newPipeSession(t, fakeBackend{call: func(_ context.Context, _ string, data json.RawMessage) (submission.Response, error) {
			intact.Store(string(data) == arguments)
			return submission.Response{Result: json.RawMessage(`{"valid":true}`)}, nil
		}}, true)
		p.handshake(t, mcpadapter.ProtocolVersion)
		p.send(t, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"communication_submit","arguments":`+arguments+`}}`)
		if r := p.read(t); r.Error != nil || !intact.Load() {
			t.Fatalf("full contribution rejected or altered: %+v", r)
		}
	})
	t.Run("input frame", func(t *testing.T) {
		p := newPipeSession(t, fakeBackend{}, true)
		_, _ = io.WriteString(p.in, strings.Repeat("x", mcpadapter.MaxInputBytes+1)+"\n")
		p.join(t)
		if p.err == nil {
			t.Fatal("oversize input accepted")
		}
	})
	t.Run("backend response", func(t *testing.T) {
		p := newPipeSession(t, fakeBackend{call: func(context.Context, string, json.RawMessage) (submission.Response, error) {
			return submission.Response{Result: json.RawMessage(strings.Repeat("x", submission.MaxFrameBytes+1))}, nil
		}}, true)
		p.handshake(t, mcpadapter.ProtocolVersion)
		p.send(t, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"communication_submit","arguments":{}}}`)
		requireCode(t, p.read(t), -32603)
	})
}

func TestMCPServer_InitializationParameterRefusals(t *testing.T) {
	for _, params := range []string{`{}`, `{"protocolVersion":"2025-11-25","capabilities":null,"clientInfo":{"name":"test","version":"1"}}`, `{"protocolVersion":"2025-11-25","capabilities":[],"clientInfo":{"name":"test","version":"1"}}`, `{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test"}}`} {
		t.Run(params, func(t *testing.T) {
			t.Parallel()
			p := newPipeSession(t, fakeBackend{}, true)
			p.send(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":`+params+`}`)
			requireCode(t, p.read(t), -32602)
			p.handshake(t, mcpadapter.ProtocolVersion)
		})
	}
}

func TestMCPServer_StringNumericAndConnectionCancellationIsolation(t *testing.T) {
	started := make(chan string, 3)
	cancelled := make(chan string, 3)
	release := make(chan struct{})
	backend := fakeBackend{call: func(ctx context.Context, _ string, args json.RawMessage) (submission.Response, error) {
		var p struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return submission.Response{}, err
		}
		started <- p.Token
		select {
		case <-ctx.Done():
			cancelled <- p.Token
			return submission.Response{}, ctx.Err()
		case <-release:
			return submission.Response{Result: json.RawMessage(`{"valid":true}`)}, nil
		}
	}}
	first := newPipeSession(t, backend, true)
	second := newPipeSession(t, backend, true)
	first.handshake(t, mcpadapter.ProtocolVersion)
	second.handshake(t, mcpadapter.ProtocolVersion)
	first.send(t, `{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"x","arguments":{"token":"numeric"}}}`)
	first.send(t, `{"jsonrpc":"2.0","id":"10","method":"tools/call","params":{"name":"x","arguments":{"token":"string"}}}`)
	second.send(t, `{"jsonrpc":"2.0","id":"10","method":"tools/call","params":{"name":"x","arguments":{"token":"other-connection"}}}`)
	for range 3 {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("distinct IDs collided or worker did not start")
		}
	}
	first.send(t, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":"10"}}`)
	select {
	case token := <-cancelled:
		if token != "string" {
			t.Fatalf("cancelled foreign request %s", token)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("string cancellation did not propagate")
	}
	requireCode(t, first.read(t), -32603)
	close(release)
	if r := first.read(t); r.Error != nil || string(r.ID) != "10" {
		t.Fatalf("numeric ID affected: %+v", r)
	}
	if r := second.read(t); r.Error != nil || string(r.ID) != `"10"` {
		t.Fatalf("other connection affected: %+v", r)
	}
	first.send(t, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":"10"}}`)
	first.send(t, `{"jsonrpc":"2.0","id":11,"method":"tools/list"}`)
	if r := first.read(t); r.Error != nil {
		t.Fatalf("completed cancellation poisoned connection: %+v", r)
	}
}

func TestMCPServer_OutputBacklogAndContextJoin(t *testing.T) {
	t.Run("context interrupts blocked output", func(t *testing.T) {
		p := newPipeSession(t, fakeBackend{}, false)
		p.send(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`)
		p.cancel()
		p.join(t)
		if p.err == nil {
			t.Fatal("cancelled context reported ordinary success")
		}
	})
	t.Run("bounded response backlog", func(t *testing.T) {
		p := newPipeSession(t, fakeBackend{}, false)
		for i := 0; i < mcpadapter.MaxQueuedResponses+4; i++ {
			_, err := fmt.Fprintf(p.in, "{\"jsonrpc\":\"2.0\",\"id\":%d,\"method\":\"tools/list\"}\n", i)
			if err != nil {
				break
			}
		}
		p.join(t)
		if p.err == nil {
			t.Fatal("blocked output backlog accepted without termination")
		}
	})
}
