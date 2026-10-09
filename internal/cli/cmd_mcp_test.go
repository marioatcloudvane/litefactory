package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/cloudvane/litefactory/internal/testutil"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
	"github.com/cloudvane/litefactory/internal/turn"
)

type mcpCLIFixture struct {
	db                  *store.DB
	service             *turn.AgentResultService
	guard               *turn.AttemptGuard
	authority           *turn.AttemptAuthority
	broker              *submission.Broker
	block               atomic.Bool
	started, joined     chan struct{}
	startOnce, joinOnce sync.Once
}

func newMCPCLIFixture(t *testing.T) *mcpCLIFixture {
	t.Helper()
	root := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(root, "results.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: root, Forge: store.ForgeGitHub, Owner: "fixture", Name: "mcp", RemoteURL: "git@github.com:fixture/mcp.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(root, "worktrees")})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), repo, 277)
	if err != nil {
		t.Fatal(err)
	}
	segment, err := db.GetOrCreateSegment(t.Context(), session.ID, 1, "mcp", store.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "research", Agent: store.AgentClaude, TriggerCommentID: 1, LogPath: func(n int) string { return fmt.Sprintf("run-%d.log", n) }})
	if err != nil {
		t.Fatal(err)
	}
	f := &mcpCLIFixture{db: db, started: make(chan struct{}), joined: make(chan struct{})}
	service, err := turn.BindAgentResultService(t.Context(), turn.AgentResultServiceOptions{DB: db, Attempt: store.ResultAttempt{Task: session.TaskRef, Mode: "research", WorkContext: "owner-approved-work", RunID: run.ID, Attempt: "cold-1", SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2}, Resolve: func(ctx context.Context) (ar.Context, error) {
		if f.block.Load() {
			f.startOnce.Do(func() { close(f.started) })
			<-ctx.Done()
			f.joinOnce.Do(func() { close(f.joined) })
			return ar.Context{}, ctx.Err()
		}
		return ar.Context{Mode: "research", SchemaVersion: 1, WorkflowVersion: 2}, nil
	}, ReadTargets: ar.EditTargets("research"), EditTargets: ar.EditTargets("research")})
	if err != nil {
		t.Fatal(err)
	}
	f.service = service
	authority, err := turn.NewAttemptAuthority(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	f.authority = authority
	t.Cleanup(func() {
		if err := authority.Close(context.WithoutCancel(t.Context())); err != nil {
			t.Error(err)
		}
	})
	guard, err := authority.Bind(t.Context(), service.Binding(), service.MCPPermissionDigest(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	f.guard = guard
	dispatch, err := turn.NewToolDispatcher(guard, service)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("/tmp", "lf-mcp-cli-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	broker, err := submission.Start(t.Context(), submission.Options{Directory: dir, AllowTools: true, AttemptReference: service.Binding().Attempt, MaxWorkers: 4, RequestTimeout: 10 * time.Second, Handler: dispatch.Handler})
	if err != nil {
		t.Fatal(err)
	}
	f.broker = broker
	t.Cleanup(func() {
		if err := broker.Close(); err != nil {
			t.Error(err)
		}
	})
	return f
}

type mcpCLIProcess struct {
	input, output *os.File
	reader        *bufio.Reader
	done          chan struct{}
	err           error
	cancel        context.CancelFunc
	stderr        bytes.Buffer
}
type mcpCLIResponse struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code int `json:"code"`
	} `json:"error"`
}

func startMCPCLI(t *testing.T, descriptor string) *mcpCLIProcess {
	t.Helper()
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	p := &mcpCLIProcess{input: inW, output: outR, reader: bufio.NewReader(outR), done: make(chan struct{}), cancel: cancel}
	var command *Command
	for _, c := range Commands() {
		if c.Name == "mcp serve" {
			command = c
			break
		}
	}
	if command == nil {
		t.Fatal("mcp serve not registered")
	}
	flags := flag.NewFlagSet("mcp serve", flag.ContinueOnError)
	handler := command.New(flags)
	if err := flags.Parse([]string{"--descriptor", descriptor}); err != nil {
		t.Fatal(err)
	}
	go func() {
		defer close(p.done)
		defer inR.Close()
		defer outW.Close()
		p.err = handler(ctx, &Env{Stdin: inR, Stdout: outW, Stderr: &p.stderr, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, nil)
	}()
	t.Cleanup(func() { cancel(); _ = inW.Close(); _ = outR.Close(); p.join(t) })
	return p
}
func (p *mcpCLIProcess) join(t *testing.T) {
	t.Helper()
	select {
	case <-p.done:
	case <-time.After(testutil.Wait):
		t.Fatal("CLI and stdio workers failed to join")
	}
}
func (p *mcpCLIProcess) send(t *testing.T, s string) {
	t.Helper()
	if err := p.input.SetWriteDeadline(time.Now().Add(testutil.Wait)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(p.input, s+"\n"); err != nil {
		t.Fatal(err)
	}
}
func (p *mcpCLIProcess) read(t *testing.T) mcpCLIResponse {
	t.Helper()
	if err := p.output.SetReadDeadline(time.Now().Add(testutil.Wait)); err != nil {
		t.Fatal(err)
	}
	data, err := p.reader.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var r mcpCLIResponse
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatalf("nonprotocol stdout: %q %v", data, err)
	}
	return r
}
func (p *mcpCLIProcess) handshake(t *testing.T) {
	t.Helper()
	p.send(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"real-pipe-fixture","version":"1"}}}`)
	r := p.read(t)
	if r.Error != nil || !bytes.Contains(r.Result, []byte(`"protocolVersion":"2025-11-25"`)) {
		t.Fatalf("handshake=%+v", r)
	}
	p.send(t, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
}
func cliStructured(t *testing.T, r mcpCLIResponse) submission.Response {
	t.Helper()
	if r.Error != nil {
		t.Fatalf("RPC error %+v", r.Error)
	}
	var body struct {
		Structured submission.Response `json:"structuredContent"`
	}
	if err := json.Unmarshal(r.Result, &body); err != nil {
		t.Fatal(err)
	}
	return body.Structured
}

func TestMCPCLI_ExplicitServeWithAutomaticPolicyDisabled(t *testing.T) {
	f := newMCPCLIFixture(t)
	cfg, err := config.Decode(strings.NewReader("agents:\n  claude: {}\nsettings:\n  mcp_auto_start: false\n"))
	if err != nil {
		t.Fatal(err)
	}
	spec, err := turn.BuildMCPLaunchSpec(t.Context(), cfg.Settings.MCPAutoStart, "/factory/litefactory", f.broker, f.guard)
	if err != nil || spec != nil {
		t.Fatalf("false policy prepared registration: %+v %v", spec, err)
	}
	p := startMCPCLI(t, f.broker.DescriptorPath())
	p.handshake(t)
	p.send(t, `{"jsonrpc":"2.0","id":"list","method":"tools/list"}`)
	list := p.read(t)
	var tools struct {
		Tools []submission.ToolSpec `json:"tools"`
	}
	if err := json.Unmarshal(list.Result, &tools); err != nil || len(tools.Tools) != 3 {
		t.Fatalf("list=%+v %v", list, err)
	}
	data := `{ "result" : {"outcome":"incomplete","reason":"More work remains","comment":"Exact  bytes","payload":{"research":{"request":"Inspect boundary"}}} }`
	p.send(t, `{"jsonrpc":"2.0","id":"first","method":"tools/call","params":{"name":"communication_submit","arguments":`+data+`}}`)
	first := cliStructured(t, p.read(t))
	if first.Error != "" || first.RequestID == "" {
		t.Fatalf("submit=%+v", first)
	}
	var result turn.AgentResultSubmission
	if err := json.Unmarshal(first.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result.Receipt == nil || !result.Validation.Valid || !bytes.Equal(result.Receipt.Payload, []byte(data)) {
		t.Fatalf("shared submission changed: %+v", result)
	}
	states := result.Receipt.Latest
	if states.Input != "accepted" || states.Engineering != "pending" || states.Acceptance != "pending" || states.Publication != "pending" {
		t.Fatalf("receipt conflated success: %+v", states)
	}
	p.send(t, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"communication_submit","arguments":`+data+`}}`)
	duplicate := cliStructured(t, p.read(t))
	if duplicate.RequestID != first.RequestID {
		t.Fatal("connection ID changed application identity")
	}
	_ = p.input.Close()
	p.join(t)
	if p.err != nil || p.stderr.Len() != 0 {
		t.Fatalf("explicit server exit=%v stderr=%q", p.err, p.stderr.String())
	}
	// Same still-live descriptor reconnects through a new handshake and socket.
	reconnect := startMCPCLI(t, f.broker.DescriptorPath())
	reconnect.handshake(t)
	reconnect.send(t, `{"jsonrpc":"2.0","id":"receipt","method":"tools/call","params":{"name":"communication_receipt","arguments":{"request_id":"`+first.RequestID+`"}}}`)
	receipt := cliStructured(t, reconnect.read(t))
	if receipt.RequestID != first.RequestID {
		t.Fatal("reconnect lost correlation")
	}
	history, err := f.service.Receipt(t.Context(), f.service.Binding().RunID, first.RequestID)
	if err != nil || history.Receipt.ID != result.Receipt.ID {
		t.Fatalf("durable history=%+v %v", history, err)
	}
}

func TestMCPCLI_StaleDescriptorRefusedBeforeHandshake(t *testing.T) {
	for _, state := range []string{"cancelled", "closed", "restarted"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			f := newMCPCLIFixture(t)
			if state == "restarted" {
				fresh, err := turn.NewAttemptAuthority(t.Context(), f.db)
				if err != nil {
					t.Fatal(err)
				}
				defer fresh.Close(context.WithoutCancel(t.Context()))
			} else {
				if err := f.guard.Revoke(t.Context(), state); err != nil {
					t.Fatal(err)
				}
			}
			p := startMCPCLI(t, f.broker.DescriptorPath())
			p.join(t)
			if p.err == nil {
				t.Fatal("stale descriptor served a handshake")
			}
			data, err := io.ReadAll(p.output)
			if err != nil || len(data) != 0 {
				t.Fatalf("stale descriptor produced stdout: %q %v", data, err)
			}
		})
	}
}

func TestMCPCLI_CancellationReachesSharedService(t *testing.T) {
	for _, method := range []string{"notification", "EOF"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			f := newMCPCLIFixture(t)
			p := startMCPCLI(t, f.broker.DescriptorPath())
			p.handshake(t)
			f.block.Store(true)
			p.send(t, `{"jsonrpc":"2.0","id":22,"method":"tools/call","params":{"name":"communication_preflight","arguments":{"result":{"outcome":"incomplete","reason":"Pending","comment":"Work","payload":{"research":{"request":"Inspect"}}}}}}`)
			select {
			case <-f.started:
			case <-time.After(testutil.Wait):
				t.Fatal("shared service was not reached through socket")
			}
			if method == "notification" {
				p.send(t, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":22}}`)
				if r := p.read(t); r.Error == nil {
					t.Fatal("cancelled call falsely succeeded")
				}
			} else {
				_ = p.input.Close()
				p.join(t)
			}
			select {
			case <-f.joined:
			case <-time.After(testutil.Wait):
				t.Fatal("cancellation failed to reach broker/service context")
			}
			revisions, err := f.db.ResultRevisionNumbers(t.Context(), f.service.Binding())
			if err != nil || len(revisions) != 0 {
				t.Fatalf("cancelled preflight admitted mutation: %v %v", revisions, err)
			}
		})
	}
}
