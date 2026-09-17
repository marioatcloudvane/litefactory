package mcpadapter_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/mcpadapter"
	"github.com/cloudvane/litefactory/internal/submission"
)

func TestMCPServer_ListMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, params string
		valid        bool
	}{
		{"absent", "", true}, {"empty", `,"params":{}`, true},
		{"captured Codex request", `,"params":{"_meta":{"progressToken":0}}`, true},
		{"extension", `,"params":{"_meta":{"progressToken":"p","vendor/test":{"mode":"validate"}}}`, true},
		{"unknown application field", `,"params":{"_meta":{"progressToken":0},"mode":"validate"}`, false},
		{"malformed metadata", `,"params":{"_meta":{"progressToken":false}}`, false},
		{"oversized metadata", `,"params":{"_meta":{"x":"` + strings.Repeat("x", 64<<10) + `"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := newPipeSession(t, fakeBackend{}, true)
			p.handshake(t, mcpadapter.ProtocolVersion)
			p.send(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list"`+tc.params+`}`)
			r := p.read(t)
			if !tc.valid {
				requireCode(t, r, -32602)
				return
			}
			if r.Error != nil || string(r.Result) != `{"tools":[]}` {
				t.Fatalf("discovery=%+v", r)
			}
		})
	}
}

func TestMCPServer_MetadataHandshakeAndCancellation(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	args := `{ "comment" : "two  spaces and \u0061", "number": 1e2 }`
	p := newPipeSession(t, fakeBackend{call: func(ctx context.Context, name string, data json.RawMessage) (submission.Response, error) {
		if name != "communication_submit" || string(data) != args {
			t.Errorf("contribution changed: %s %s", name, data)
		}
		close(started)
		<-ctx.Done()
		return submission.Response{}, ctx.Err()
	}}, true)
	p.send(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"},"_meta":{"progressToken":0,"vendor/test":true}}}`)
	if r := p.read(t); r.Error != nil {
		t.Fatalf("initialize=%+v", r)
	}
	p.send(t, `{"jsonrpc":"2.0","method":"notifications/initialized","params":{"_meta":{"progressToken":"init"}}}`)
	p.send(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{"_meta":{}}}`)
	if r := p.read(t); r.Error != nil {
		t.Fatalf("initialized=%+v", r)
	}
	p.send(t, `{"jsonrpc":"2.0","id":"work","method":"tools/call","params":{"name":"communication_submit","arguments":`+args+`,"_meta":{"progressToken":2}}}`)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("call did not start")
	}
	// Invalid metadata cannot gain cancellation authority. A subsequent list
	// reply acts as a barrier: an erroneously cancelled call would reply first.
	p.send(t, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":"work","_meta":{"progressToken":false}}}`)
	p.send(t, `{"jsonrpc":"2.0","id":"barrier","method":"tools/list"}`)
	if r := p.read(t); r.Error != nil || string(r.ID) != `"barrier"` {
		t.Fatalf("invalid metadata cancelled request: %+v", r)
	}
	p.send(t, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":"work","reason":"stop","_meta":{"progressToken":3,"vendor/test":{}}}}`)
	r := p.read(t)
	requireCode(t, r, -32603)
	if string(r.ID) != `"work"` {
		t.Fatalf("cancelled wrong request: %s", r.ID)
	}
	// Cancellation affects only the named call; discovery remains usable.
	p.send(t, `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)
	if r := p.read(t); r.Error != nil {
		t.Fatalf("connection lost after cancellation: %+v", r)
	}
}

func TestMCPServer_HandshakeMetadataRefusals(t *testing.T) {
	for _, tc := range []struct{ name, extra string }{
		{"null metadata", `,"_meta":null`},
		{"invalid progress token", `,"_meta":{"progressToken":[]}`},
		{"unknown application field", `,"_meta":{"progressToken":0},"mode":"validate"`},
		{"oversized metadata", `,"_meta":{"x":"` + strings.Repeat("x", 64<<10) + `"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := newPipeSession(t, fakeBackend{}, true)
			p.send(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}`+tc.extra+`}}`)
			requireCode(t, p.read(t), -32602)
			p.send(t, `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`)
			if r := p.read(t); r.Error != nil {
				t.Fatalf("retry initialization=%+v", r)
			}
			p.send(t, `{"jsonrpc":"2.0","method":"notifications/initialized","params":{`+strings.TrimPrefix(tc.extra, ",")+`}}`)
			p.send(t, `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)
			requireCode(t, p.read(t), -32002)
			p.send(t, `{"jsonrpc":"2.0","method":"notifications/initialized","params":{"_meta":{}}}`)
			p.send(t, `{"jsonrpc":"2.0","id":4,"method":"tools/list"}`)
			if r := p.read(t); r.Error != nil {
				t.Fatalf("valid initialized retry=%+v", r)
			}
		})
	}
}
