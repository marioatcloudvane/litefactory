package turn

import (
	"context"
	"encoding/json"
	"github.com/cloudvane/litefactory/internal/testutil"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/mcpadapter"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// catalogPipe connects the public MCP protocol to the real bound socket client.
// HTTP alone is a controlled external forge boundary; every local layer is real.
type catalogPipe struct {
	in   *json.Encoder
	out  *json.Decoder
	next int
}

func newCatalogPipe(t *testing.T, client submission.Client) *catalogPipe {
	t.Helper()
	input, writer := io.Pipe()
	reader, output := io.Pipe()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); _ = (mcpadapter.Server{}).Run(ctx, input, output, client) }()
	t.Cleanup(func() {
		cancel()
		_ = writer.Close()
		_ = reader.Close()
		select {
		case <-done:
		case <-time.After(testutil.Wait):
			t.Error("MCP workers did not join")
		}
	})
	p := &catalogPipe{in: json.NewEncoder(writer), out: json.NewDecoder(reader)}
	p.rpc(t, "initialize", map[string]any{"protocolVersion": mcpadapter.ProtocolVersion, "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "catalog-fixture", "version": "1"}})
	mustProgress(t, p.in.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}))
	return p
}
func (p *catalogPipe) rpc(t *testing.T, method string, params any) json.RawMessage {
	t.Helper()
	p.next++
	mustProgress(t, p.in.Encode(map[string]any{"jsonrpc": "2.0", "id": p.next, "method": method, "params": params}))
	var result struct {
		ID     int             `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	mustProgress(t, p.out.Decode(&result))
	if result.ID != p.next {
		t.Fatalf("RPC response: %+v", result)
	}
	if len(result.Error) > 0 {
		raw, err := json.Marshal(map[string]any{"structuredContent": submission.Response{Error: string(result.Error)}})
		mustProgress(t, err)
		return raw
	}
	return result.Result
}
func (p *catalogPipe) call(t *testing.T, tool string, args json.RawMessage) submission.Response {
	t.Helper()
	raw := p.rpc(t, "tools/call", map[string]any{"name": tool, "arguments": args})
	var result struct {
		Structured submission.Response `json:"structuredContent"`
	}
	mustProgress(t, json.Unmarshal(raw, &result))
	return result.Structured
}

func catalogApplication(t *testing.T, mode string) (*store.DB, *AgentResultService, *SubmissionSession, json.RawMessage) {
	t.Helper()
	if mode != "implement" && mode != "validate" {
		db, _, opts := agentResultFixture(t, mode)
		app, err := BindAgentResultService(t.Context(), opts)
		mustProgress(t, err)
		return db, app, nil, nil
	}
	f, _, session, options := submissionSessionFixture(t, store.WorkflowSeparated)
	payload, _ := json.Marshal(capturedContribution(t, options.Worktree, "US-1", "engineering"))
	if mode == "validate" {
		mustProgress(t, session.Close(t.Context()))
		code := int64(0)
		_, err := f.db.FinishRun(t.Context(), options.RunID, store.RunOutcome{Status: store.RunSucceeded, ExitCode: &code})
		mustProgress(t, err)
		old, err := f.db.GetRun(t.Context(), options.RunID)
		mustProgress(t, err)
		run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: old.SegmentID, Mode: mode, Agent: store.AgentClaude, TriggerCommentID: 2, LogPath: func(int) string { return "validate.log" }})
		mustProgress(t, err)
		validation := &agent.ValidationContext{Version: 1, Parent: f.session.TaskRef, Generation: 1, Stories: []agent.ValidationStory{{Story: options.Bundle.Stories[0], Criteria: []string{"AC-001"}}}}
		session, err = StartSubmissionSession(t.Context(), SubmissionSessionOptions{DB: f.db, Progress: f.opts.StoryProgress, Paths: f.opts.Paths, RunID: run.ID, Worktree: options.Worktree, Validation: validation})
		mustProgress(t, err)
		t.Cleanup(func() { mustProgress(t, session.Close(context.WithoutCancel(t.Context()))) })
		payload = json.RawMessage(`{"stage":"validate","story":"US-1","outcome":"blocked","reason":"Fixture has no retained engineering snapshot"}`)
	}
	b := session.CheckpointBinding()
	app, err := BindAgentResultService(t.Context(), AgentResultServiceOptions{DB: f.db, Attempt: store.ResultAttempt{Task: b.Parent, RunID: b.RunID, Attempt: b.Attempt, Mode: mode, WorkContext: "complete-catalog", SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2}, Resolve: func(context.Context) (ar.Context, error) {
		return ar.Context{Mode: mode, SchemaVersion: 1, WorkflowVersion: 2}, nil
	}})
	mustProgress(t, err)
	return f.db, app, session, payload
}

func TestMCPFamilyCompleteCatalogPipeSocketHTTP(t *testing.T) {
	for _, mode := range []string{"research", "implement", "validate", "plan", "spike"} {
		t.Run(mode, func(t *testing.T) {
			db, app, session, evidencePayload := catalogApplication(t, mode)
			now := time.Now().UTC().Add(-time.Minute)
			cs, err := store.NewCreationStore(db, func() time.Time { return now })
			mustProgress(t, err)
			cp := CreationPublicationProjection(db, cs)
			ep := EvidenceReceiptProjection(db)
			commentProjection := CommentReceiptProjection(db)
			receipts, err := NewReceiptService(db, app.Binding(), nil, map[string]FamilyProjection{"comment": commentProjection, "tool:issues_comment": commentProjection, "creation_publication": cp, "tool:issues_create_batch": cp, "submission": ep, "tool:evidence_submit": ep})
			mustProgress(t, err)
			var posts, reads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasPrefix(r.URL.Path, "/repos/"+receipts.scope.Repository+"/issues") {
					t.Errorf("foreign path %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodPost {
					posts.Add(1)
					w.WriteHeader(http.StatusCreated)
					if strings.HasSuffix(r.URL.Path, "/comments") {
						var payload struct {
							Body string `json:"body"`
						}
						if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
							t.Error(err)
						}
						_ = json.NewEncoder(w).Encode(map[string]any{"id": 555, "body": payload.Body, "user": map[string]any{"id": 20, "type": "Bot"}})
					} else {
						var artifact submission.CreationArtifact
						if err := json.NewDecoder(r.Body).Decode(&artifact); err != nil {
							t.Error(err)
						}
						_ = json.NewEncoder(w).Encode(map[string]any{"id": 901, "number": 401, "title": artifact.Title, "body": artifact.Body, "user": map[string]any{"id": 20, "type": "Bot"}})
					}
					return
				}
				reads.Add(1)
				if strings.HasSuffix(r.URL.Path, "/issues") {
					_, _ = w.Write([]byte(`[{"number":284,"title":"Visible","state":"open"}]`))
				} else {
					_, _ = w.Write([]byte(`{"id":2840,"number":284,"title":"Visible","state":"open"}`))
				}
			}))
			t.Cleanup(server.Close)
			forge, err := github.New(github.Options{APIBase: server.URL, HTTPClient: server.Client(), Token: secrets.NewToken("catalog-fixture")})
			mustProgress(t, err)
			query, err := NewIssueQueryService(receipts, forge)
			mustProgress(t, err)
			comments, err := NewIssuePublicationService(db, receipts, forge, redact.New(redact.Options{}), []int{284}, 20)
			mustProgress(t, err)
			activity, err := NewActivityService(ActivityOptions{Receipts: receipts, Now: now})
			mustProgress(t, err)
			var evidence *EvidenceService
			if session != nil {
				evidence, err = NewEvidenceService(session, receipts)
				mustProgress(t, err)
			}
			feedback, err := NewFeedbackService(app, evidence)
			mustProgress(t, err)
			registrations := append(query.Registrations(), comments.Registration(), FamilyRegistration{Name: "activity_update", Owner: activity, Handle: activity.HandleUpdate}, FamilyRegistration{Name: "activity_get", Owner: activity, Handle: activity.HandleGet})
			options := FamilyDispatcherOptions{Service: app, Receipts: receipts, Feedback: feedback, Evidence: evidence, CommentTargets: []int{284}, Registrations: registrations}
			client, guard, _ := familySocketFixture(t, db, options)
			if _, err := NewFamilyToolDispatcher(nil, options); err == nil {
				t.Fatal("missing guard accepted")
			}
			changed := options
			changed.CommentTargets = []int{285}
			if _, err := NewFamilyToolDispatcher(guard, changed); err == nil {
				t.Fatal("mismatched scope accepted")
			}
			pipe := newCatalogPipe(t, client)
			var listed struct {
				Tools []submission.ToolSpec `json:"tools"`
			}
			mustProgress(t, json.Unmarshal(pipe.rpc(t, "tools/list", map[string]any{}), &listed))
			names := []string{}
			for _, tool := range listed.Tools {
				names = append(names, tool.Name)
			}
			expected := []string{"communication_preflight", "communication_submit", "communication_receipt"}
			familyMode := mode == "research" || mode == "implement" || mode == "validate"
			if familyMode {
				expected = append(expected, "feedback_preflight", "issues_list", "issues_get", "issues_comment", "activity_update", "activity_get", "receipts_get")
			}
			if session != nil {
				expected = append(expected, "evidence_submit")
			}
			slices.Sort(names)
			slices.Sort(expected)
			if !slices.Equal(names, expected) {
				t.Fatalf("catalog=%v want=%v", names, expected)
			}
			calls := []struct {
				name string
				args json.RawMessage
			}{{"feedback_preflight", json.RawMessage(`{"kind":"creation","input":{"artifacts":[{"title":"Proposed","body":"Unapproved"}]}}`)}, {"issues_list", json.RawMessage(`{}`)}, {"issues_get", json.RawMessage(`{"issue_number":284}`)}, {"issues_comment", json.RawMessage(`{"issue_number":284,"message":"Catalog comment"}`)}, {"activity_update", json.RawMessage(`{"sequence":1,"message":"Working"}`)}, {"activity_get", json.RawMessage(`{}`)}, {"evidence_submit", evidencePayload}}
			for _, call := range calls {
				if call.args == nil {
					call.args = json.RawMessage(`{}`)
				}
				beforePosts, beforeReads := posts.Load(), reads.Load()
				response := pipe.call(t, call.name, call.args)
				allowed := slices.Contains(expected, call.name)
				if !allowed {
					if response.Error == "" || posts.Load() != beforePosts || reads.Load() != beforeReads {
						t.Fatalf("forbidden %s effected: %+v", call.name, response)
					}
					continue
				}
				if response.Error != "" {
					t.Fatalf("allowed %s failed: %s", call.name, response.Error)
				}
				beforePosts, beforeReads = posts.Load(), reads.Load()
				recovered := pipe.call(t, "receipts_get", mcpReceiptArgs(t, response.RequestID))
				if recovered.Error != "" {
					t.Fatal(recovered.Error)
				}
				var receipt FamilyReceipt
				mustProgress(t, json.Unmarshal(recovered.Result, &receipt))
				if receipt.Tool != call.name || receipt.Disposition == "missing" || receipt.Disposition == "pending" {
					t.Fatalf("lost family outcome: %+v", receipt)
				}
				if call.name == "issues_comment" && (len(receipt.Publications) != 1 || receipt.Publications[0].CommentID != 555) {
					t.Fatalf("missing comment identity %+v", receipt)
				}
				if posts.Load() != beforePosts || reads.Load() != beforeReads {
					t.Fatal("receipt redispatched HTTP")
				}
			}
			if !familyMode {
				beforePosts, beforeReads := posts.Load(), reads.Load()
				denied := pipe.call(t, "receipts_get", mcpReceiptArgs(t, "missing"))
				if denied.Error == "" || posts.Load() != beforePosts || reads.Load() != beforeReads {
					t.Fatalf("forbidden receipt access: %+v", denied)
				}
			}
			// The retired batch tool is rejected regardless of authored prose or tokens.
			for _, args := range []string{`{}`, `{"token":"approved","comment_count":10000}`, `{"confirmation":"yes"}`} {
				before := posts.Load()
				response := pipe.call(t, "issues_create_batch", json.RawMessage(args))
				if response.Error == "" || posts.Load() != before {
					t.Fatalf("ungranted creation: %+v", response)
				}
			}
			if familyMode {
				for _, state := range []string{"pending", "uncertain", "rejected"} {
					id := "catalog-" + state
					_, _, err := receipts.Begin(t.Context(), id, "issues_list", []byte(`{}`))
					mustProgress(t, err)
					if state != "pending" {
						mustProgress(t, receipts.Finish(t.Context(), id, state, store.FamilyReference{}))
					}
					r := pipe.call(t, "receipts_get", mcpReceiptArgs(t, id))
					var got FamilyReceipt
					mustProgress(t, json.Unmarshal(r.Result, &got))
					if got.Disposition != state {
						t.Fatalf("state=%s receipt=%+v", state, got)
					}
				}
				r := pipe.call(t, "receipts_get", mcpReceiptArgs(t, "not-recorded"))
				var missing FamilyReceipt
				mustProgress(t, json.Unmarshal(r.Result, &missing))
				if missing.Disposition != "missing" {
					t.Fatalf("recursive receipt %+v", missing)
				}
			}
			mustProgress(t, guard.Revoke(t.Context(), "cancelled"))
			beforePosts, beforeReads := posts.Load(), reads.Load()
			for _, name := range expected {
				response := pipe.call(t, name, json.RawMessage(`{}`))
				if response.Error == "" {
					t.Fatalf("revoked %s admitted", name)
				}
			}
			if posts.Load() != beforePosts || reads.Load() != beforeReads {
				t.Fatal("revoked calls reached forge")
			}
		})
	}
}
