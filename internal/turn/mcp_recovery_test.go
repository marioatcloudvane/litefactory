package turn

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/mcpadapter"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// This fixture registers a test-only handler at the private broker. Production
// ToolProfile remains unchanged and contains no creation tool.
type creationFaultFixture struct {
	options     AgentResultServiceOptions
	session     store.Session
	db          *store.DB
	cs          *store.CreationStore
	auth        *CreationAuthorizer
	authority   *AttemptAuthority
	guard       *AttemptGuard
	broker      *submission.Broker
	client      submission.Client
	envelope    submission.CreationEnvelope
	stage       string
	hit, joined chan struct{}
	once        atomic.Bool
	operation   atomic.Int64
	dispatched  atomic.Int64
	remoteUnits atomic.Int64
}

func newCreationFaultFixture(t *testing.T, stage string) *creationFaultFixture {
	t.Helper()
	db, s, opts := agentResultFixture(t, "research")
	service, err := BindAgentResultService(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-time.Minute)
	cs, err := store.NewCreationStore(db, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	auth, err := NewCreationAuthorizer(CreationAuthorizerOptions{Store: cs, Enabled: true, OwnerID: 10, BotID: 20})
	if err != nil {
		t.Fatal(err)
	}
	e := submission.CreationEnvelope{Version: 1, Repository: "example/results", SourceTask: s.TaskRef, Relationship: "linked", Artifacts: []submission.CreationArtifact{{Title: "One", Body: "Exact first"}, {Title: "Two", Body: "Exact second"}}}
	hash, err := auth.RetainProposal(t.Context(), e)
	if err != nil {
		t.Fatal(err)
	}
	intent, err := auth.AdmitOwnerGrantIntent(t.Context(), OwnerGrantControl{Repository: e.Repository, SourceTask: s.TaskRef, Mode: "research", Body: "/mcp-create " + hash + " linked", ActorID: 10, CommentID: 1, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	authority, err := NewAttemptAuthority(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := authority.Close(context.WithoutCancel(t.Context())); err != nil {
			t.Error(err)
		}
	})
	guard, err := authority.Bind(t.Context(), service.Binding(), service.MCPPermissionDigest(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = auth.BindCreationGrant(t.Context(), guard, intent); err != nil {
		t.Fatal(err)
	}
	f := &creationFaultFixture{options: opts, session: s, db: db, cs: cs, auth: auth, authority: authority, guard: guard, envelope: e, stage: stage, hit: make(chan struct{}), joined: make(chan struct{})}
	dir, err := os.MkdirTemp("/tmp", "lf-fault-")
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
	f.broker, err = submission.Start(t.Context(), submission.Options{Directory: dir, AllowTools: true, AttemptReference: guard.Binding().Attempt, MaxWorkers: 4, RequestTimeout: time.Minute, Handler: f.handle})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.broker.Close(); err != nil {
			t.Error(err)
		}
	})
	f.client = submission.Client{DescriptorPath: f.broker.DescriptorPath()}
	return f
}
func (f *creationFaultFixture) handle(ctx context.Context, q submission.Request) (json.RawMessage, error) {
	live, release, err := f.guard.Context(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	if q.Method == "tools/list" {
		return json.RawMessage(`{"tools":[]}`), nil
	}
	call, err := submission.DecodeToolCall(q.Payload)
	if err != nil {
		return nil, err
	}
	if call.Name != "fixture_create" {
		return nil, fmt.Errorf("unknown fixture tool")
	}
	var e submission.CreationEnvelope
	if err = json.Unmarshal(call.Arguments, &e); err != nil {
		return nil, err
	}
	first := f.once.CompareAndSwap(false, true)
	if first {
		defer close(f.joined)
	}
	stop := func() { close(f.hit); <-live.Done() }
	if first && f.stage == "before reservation" {
		stop()
	}
	r, err := f.auth.ReserveCreation(live, f.guard, e)
	if err != nil {
		return nil, err
	}
	f.operation.Store(r.Operation.ID)
	if r.Permit == nil {
		return json.Marshal(r.Operation)
	}
	if first && (f.stage == "after reservation" || f.stage == "before dispatch") {
		stop()
		return nil, live.Err()
	}
	if _, err = f.auth.BeginDispatch(live, r.Permit); err != nil {
		return nil, err
	}
	f.dispatched.Add(1)
	if f.stage == "partial" {
		f.remoteUnits.Add(1)
	} else {
		f.remoteUnits.Add(int64(len(e.Artifacts)))
	}
	if first {
		stop()
	}
	// The original factory owner records the observation independently of the
	// interrupted caller, under a bounded context; it does not repeat the effect.
	recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	_, err = f.auth.RecordOperationOutcome(recovery, r.Permit, "uncertain")
	if err != nil {
		return nil, err
	}
	return nil, live.Err()
}

type creationFaultWire struct {
	input  *io.PipeWriter
	output *io.PipeReader
	reader *bufio.Reader
	done   chan error
	once   sync.Once
}

func newCreationFaultWire(t *testing.T, client submission.Client) *creationFaultWire {
	t.Helper()
	in, write := io.Pipe()
	read, out := io.Pipe()
	w := &creationFaultWire{input: write, output: read, reader: bufio.NewReader(read), done: make(chan error, 1)}
	go func() { w.done <- (mcpadapter.Server{}).Run(t.Context(), in, out, client) }()
	t.Cleanup(func() { w.close(t) })
	w.send(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"fixture","version":"1"}}}`)
	w.read(t)
	w.send(t, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	return w
}
func (w *creationFaultWire) send(t *testing.T, data string) {
	t.Helper()
	if _, err := fmt.Fprintln(w.input, data); err != nil {
		t.Fatal(err)
	}
}
func (w *creationFaultWire) read(t *testing.T) json.RawMessage {
	t.Helper()
	raw, err := w.reader.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(raw) {
		t.Fatalf("stdout not JSON: %s", raw)
	}
	return raw
}
func (w *creationFaultWire) close(t *testing.T) {
	t.Helper()
	w.once.Do(func() {
		_ = w.input.Close()
		_ = w.output.Close()
		select {
		case <-w.done:
		case <-time.After(5 * time.Second):
			t.Error("adapter failed to join")
		}
	})
}
func (w *creationFaultWire) call(t *testing.T, id int, e submission.CreationEnvelope) {
	t.Helper()
	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	w.send(t, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"fixture_create","arguments":%s}}`, id, raw))
}
func waitCreationFault(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("fault barrier did not complete")
	}
}

func TestMCPRecovery_FaultBoundaryMatrix(t *testing.T) {
	cases := []struct{ stage, interrupt string }{{"before reservation", "cancel"}, {"after reservation", "EOF"}, {"before dispatch", "close"}, {"after remote", "cancel"}, {"partial", "replacement"}, {"after remote", "restart"}}
	for _, tc := range cases {
		t.Run(tc.stage+"/"+tc.interrupt, func(t *testing.T) {
			f := newCreationFaultFixture(t, tc.stage)
			w := newCreationFaultWire(t, f.client)
			w.call(t, 2, f.envelope)
			waitCreationFault(t, f.hit)
			switch tc.interrupt {
			case "cancel":
				w.send(t, `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":2}}`)
			case "EOF":
				w.close(t)
			case "close":
				if err := f.authority.Close(t.Context()); err != nil {
					t.Fatal(err)
				}
			case "replacement":
				if err := f.guard.Revoke(t.Context(), "replaced"); err != nil {
					t.Fatal(err)
				}
			case "restart":
				next, err := NewAttemptAuthority(t.Context(), f.db)
				if err != nil {
					t.Fatal(err)
				}
				defer next.Close(context.WithoutCancel(t.Context()))
				w.close(t)
			}
			waitCreationFault(t, f.joined)
			if tc.interrupt == "cancel" || tc.interrupt == "close" || tc.interrupt == "replacement" {
				w.read(t)
			}
			id := f.operation.Load()
			if tc.stage == "before reservation" {
				if id != 0 {
					t.Fatal("cancelled preadmission reserved")
				}
				if _, err := f.cs.GetCreationOperation(t.Context(), 1); err == nil {
					t.Fatal("unexpected durable reservation")
				}
				return
			}
			op, err := f.cs.GetCreationOperation(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			want := "reserved"
			if tc.stage == "after remote" || tc.stage == "partial" {
				want = "uncertain"
			}
			if op.State != want || op.Artifacts != 2 {
				t.Fatalf("lost truthful operation: %+v", op)
			}
			if want == "uncertain" && f.dispatched.Load() != 1 {
				t.Fatal("dispatch ownership lost")
			}
			if tc.stage == "partial" && f.remoteUnits.Load() != 1 {
				t.Fatal("partial effect fixture wrong")
			}
			if tc.interrupt == "close" || tc.interrupt == "replacement" || tc.interrupt == "restart" {
				r, e := f.client.Call(t.Context(), "fixture_create", mustCreationJSON(t, f.envelope))
				if e == nil && r.Error == "" {
					t.Fatal("stale authority replay accepted")
				}
			} else {
				reconnect := newCreationFaultWire(t, f.client)
				reconnect.call(t, 3, f.envelope)
				raw := reconnect.read(t)
				var response struct {
					Result struct {
						Structured submission.Response `json:"structuredContent"`
					} `json:"result"`
				}
				if err = json.Unmarshal(raw, &response); err != nil {
					t.Fatal(err)
				}
				var replay store.CreationOperation
				if err = json.Unmarshal(response.Result.Structured.Result, &replay); err != nil {
					t.Fatal(err)
				}
				if replay.ID != id || replay.State != want {
					t.Fatalf("reconnect changed operation: %+v", replay)
				}
			}
			if want == "uncertain" {
				if _, err = f.cs.ReconcileOperation(t.Context(), id, "never_dispatched"); err == nil {
					t.Fatal("uncertain quota released")
				}
			}
		})
	}
}
func mustCreationJSON(t *testing.T, e submission.CreationEnvelope) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestMCPRecovery_ConcurrentRetriesLostReply(t *testing.T) {
	f := newCreationFaultFixture(t, "after remote")
	w := newCreationFaultWire(t, f.client)
	w.call(t, 2, f.envelope)
	waitCreationFault(t, f.hit)
	w.call(t, 3, f.envelope)
	w.call(t, 4, f.envelope)
	for range 2 {
		raw := w.read(t)
		var response struct {
			Result struct {
				Structured submission.Response `json:"structuredContent"`
			} `json:"result"`
		}
		if err := json.Unmarshal(raw, &response); err != nil {
			t.Fatal(err)
		}
		var op store.CreationOperation
		if err := json.Unmarshal(response.Result.Structured.Result, &op); err != nil {
			t.Fatal(err)
		}
		if op.ID != f.operation.Load() || op.State != "dispatched" {
			t.Fatalf("duplicate elected operation: %+v", op)
		}
	}
	w.close(t)
	waitCreationFault(t, f.joined)
	again := newCreationFaultWire(t, f.client)
	again.call(t, 5, f.envelope)
	again.read(t)
	if f.dispatched.Load() != 1 || f.remoteUnits.Load() != 2 {
		t.Fatalf("repeated simulated effects: %d/%d", f.dispatched.Load(), f.remoteUnits.Load())
	}
	if _, err := f.cs.GetCreationOperation(t.Context(), f.operation.Load()+1); err == nil {
		t.Fatal("duplicate reservation persisted")
	}
}

func TestMCPRecovery_ReplacementReceiptCannotDispatch(t *testing.T) {
	f := newCreationFaultFixture(t, "partial")
	w := newCreationFaultWire(t, f.client)
	w.call(t, 2, f.envelope)
	waitCreationFault(t, f.hit)
	w.close(t)
	waitCreationFault(t, f.joined)
	id := f.operation.Load()
	opts := f.options
	opts.Attempt = agentResultRun(t, f.db, f.session, "research", "resume-receipt", 2)
	service, err := BindAgentResultService(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	guard, err := f.authority.Bind(t.Context(), service.Binding(), service.MCPPermissionDigest(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	// A future receipt family owns target lookup. This internal fixture binds one
	// retained operation to the original task and permits reads only, with no grant.
	handler := func(ctx context.Context, q submission.Request) (json.RawMessage, error) {
		live, release, e := guard.Context(ctx)
		if e != nil {
			return nil, e
		}
		defer release()
		if q.Method == "tools/list" {
			return json.RawMessage(`{"tools":[]}`), nil
		}
		call, e := submission.DecodeToolCall(q.Payload)
		if e != nil {
			return nil, e
		}
		if call.Name != "fixture_receipt" {
			return nil, fmt.Errorf("receipt authority cannot dispatch")
		}
		var args struct {
			ID   int64  `json:"operation_id"`
			Task string `json:"task"`
		}
		if e = submission.DecodeJSON(call.Arguments, &args); e != nil {
			return nil, e
		}
		if args.ID != id || args.Task != guard.Binding().Task {
			return nil, fmt.Errorf("foreign receipt")
		}
		op, e := f.cs.GetCreationOperation(live, args.ID)
		if e != nil {
			return nil, e
		}
		return json.Marshal(op)
	}
	dir, err := os.MkdirTemp("/tmp", "lf-read-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	broker, err := submission.Start(t.Context(), submission.Options{Directory: dir, AllowTools: true, AttemptReference: guard.Binding().Attempt, MaxWorkers: 2, RequestTimeout: time.Minute, Handler: handler})
	if err != nil {
		t.Fatal(err)
	}
	defer broker.Close()
	reader := newCreationFaultWire(t, submission.Client{DescriptorPath: broker.DescriptorPath()})
	args, err := json.Marshal(map[string]any{"operation_id": id, "task": guard.Binding().Task})
	if err != nil {
		t.Fatal(err)
	}
	reader.send(t, fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"fixture_receipt","arguments":%s}}`, args))
	raw := reader.read(t)
	var response struct {
		Result struct {
			Structured submission.Response `json:"structuredContent"`
		} `json:"result"`
	}
	if err = json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	var op store.CreationOperation
	if err = json.Unmarshal(response.Result.Structured.Result, &op); err != nil {
		t.Fatal(err)
	}
	if op.ID != id || op.State != "uncertain" {
		t.Fatalf("receipt changed history %+v", op)
	}
	reader.call(t, 3, f.envelope)
	raw = reader.read(t)
	if err = json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	if response.Result.Structured.Error == "" {
		t.Fatal("read authority dispatched")
	}
	foreign, err := (submission.Client{DescriptorPath: broker.DescriptorPath()}).Call(t.Context(), "fixture_receipt", json.RawMessage(fmt.Sprintf(`{"operation_id":%d,"task":"github:foreign/task#1"}`, id)))
	if err == nil && foreign.Error == "" {
		t.Fatal("foreign receipt returned")
	}
	old, err := f.client.Call(t.Context(), "fixture_create", mustCreationJSON(t, f.envelope))
	if err == nil && old.Error == "" {
		t.Fatal("replaced attempt replayed")
	}
	if f.dispatched.Load() != 1 || f.remoteUnits.Load() != 1 {
		t.Fatal("receipt repeated partial effect")
	}
}
