package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func TestPublicationRuntimeCatalogFromProduction(t *testing.T) {
	for _, mode := range []string{"research", "plan", "spike", "implement", "validate"} {
		t.Run(mode, func(t *testing.T) {
			prep, _ := lifecycleMCPPreparation(t, mode)
			var session *SubmissionSession
			if mode == "implement" || mode == "validate" {
				f, _, ss, o := submissionSessionFixture(t, store.WorkflowSeparated)
				session = ss
				if mode == "validate" {
					mustProgress(t, ss.Close(t.Context()))
					code := int64(0)
					_, e := f.db.FinishRun(t.Context(), o.RunID, store.RunOutcome{Status: store.RunSucceeded, ExitCode: &code})
					mustProgress(t, e)
					prior, e := f.db.GetRun(t.Context(), o.RunID)
					mustProgress(t, e)
					next, e := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: prior.SegmentID, Mode: "validate", Agent: store.AgentClaude, TriggerCommentID: 2, LogPath: func(int) string { return "validate.log" }})
					mustProgress(t, e)
					o.RunID = next.ID
					session, e = StartSubmissionSession(t.Context(), SubmissionSessionOptions{DB: f.db, Progress: f.opts.StoryProgress, Paths: f.opts.Paths, RunID: next.ID, Worktree: o.Worktree, Validation: &agent.ValidationContext{Version: 1, Parent: f.session.TaskRef, Generation: 1, Stories: []agent.ValidationStory{{Story: o.Bundle.Stories[0], Criteria: []string{"AC-001"}}}}})
					mustProgress(t, e)
					t.Cleanup(func() { mustProgress(t, session.Close(context.WithoutCancel(t.Context()))) })
				}
				authority, e := NewAttemptAuthority(t.Context(), f.db)
				mustProgress(t, e)
				t.Cleanup(func() { mustProgress(t, authority.Close(context.WithoutCancel(t.Context()))) })
				ref, e := naming.ParseRef(f.session.TaskRef)
				mustProgress(t, e)
				prep = &preparation{ref: ref, session: f.session, run: store.Run{ID: o.RunID}, mcpRuntime: &MCPRuntime{Authority: authority, Paths: f.opts.Paths, Executable: "/factory/litefactory", Now: time.Now}}
			}
			var posts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodPost {
					posts.Add(1)
					var payload struct {
						Body string `json:"body"`
					}
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Error(err)
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"id": 555, "body": payload.Body, "user": map[string]any{"id": 9}})
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"number": prep.ref.Issue, "state": "open"})
			}))
			defer server.Close()
			forge, err := github.New(github.Options{APIBase: server.URL, HTTPClient: server.Client(), Token: secrets.NewToken("fixture")})
			mustProgress(t, err)
			prep.mcpRuntime.Comments = forge
			prep.mcpRuntime.Redactor = redact.New(redact.Options{})
			prep.mcpRuntime.BotID = 9
			p, spec, err := prepareMCP(t.Context(), prep, agent.Request{Mode: mode}, session)
			mustProgress(t, err)
			defer func() { mustProgress(t, p.close(context.WithoutCancel(t.Context()))) }()
			want := mode == "research" || mode == "implement" || mode == "validate"
			if slices.Contains(spec.PermittedTools(), "issues_comment") != want {
				t.Fatalf("catalog %v", spec.PermittedTools())
			}
			client := submission.Client{DescriptorPath: spec.DescriptorPath()}
			catalog, err := client.List(t.Context())
			mustProgress(t, err)
			if catalog.Error != "" {
				t.Fatal(catalog.Error)
			}
			response, err := client.Call(t.Context(), "issues_comment", []byte(`{"issue_number":999,"message":"foreign"}`))
			mustProgress(t, err)
			if response.Error == "" {
				t.Fatal("unrelated target admitted")
			}
			if want {
				response, err = client.Call(t.Context(), "issues_comment", []byte(fmt.Sprintf(`{"issue_number":%d,"message":"authorized status"}`, prep.ref.Issue)))
				mustProgress(t, err)
				if response.Error != "" {
					t.Fatal(response.Error)
				}
				if posts.Load() != 1 {
					t.Fatalf("authorized target posts %d", posts.Load())
				}
			} else if posts.Load() != 0 {
				t.Fatal("excluded profile published")
			}
			if slices.Contains(spec.PermittedTools(), "issues_create_batch") {
				t.Fatal("creation without approval")
			}
		})
	}
}

func TestPublicationRuntimePreparedOwnerIntentNativeLifetime(t *testing.T) {
	prep, db := lifecycleMCPPreparation(t)
	f := creationHTTPSetup(t, true)
	now := time.Now().UTC()
	cs, err := store.NewCreationStore(db, func() time.Time { return now })
	mustProgress(t, err)
	auth, err := NewCreationAuthorizer(CreationAuthorizerOptions{Store: cs, Enabled: true, OwnerID: 10, BotID: 20})
	mustProgress(t, err)
	runtime := prep.mcpRuntime
	runtime.CreationAuthorizer = auth
	runtime.Creation = f.service.forge
	runtime.Redactor = redact.New(redact.Options{})
	runtime.BotID = 20
	envelope := submission.CreationEnvelope{Version: 1, Artifacts: []submission.CreationArtifact{{Title: "Exact title", Body: "Exact body", LinkIssues: []int{280}}}}
	wire, err := json.Marshal(envelope)
	mustProgress(t, err)
	control := OwnerGrantControl{Repository: "example/results", SourceTask: prep.session.TaskRef, Mode: "research", ActorID: 10, CommentID: 700, CreatedAt: now, Body: "/mcp-create prepare " + string(wire)}
	prepared, err := runtime.PrepareCreationProposal(t.Context(), control)
	mustProgress(t, err)
	replay, err := runtime.PrepareCreationProposal(t.Context(), control)
	mustProgress(t, err)
	if replay.Hash != prepared.Hash {
		t.Fatal("owner retry changed exact proposal")
	}
	a := prepared.Envelope.Artifacts[0]
	if a.Title != "Exact title" || !slices.Contains(a.LinkIssues, prep.ref.Issue) || !slices.Contains(a.LinkIssues, 280) {
		t.Fatalf("retained content %+v", a)
	}
	if _, _, err = cs.EligibleCreationIntent(t.Context(), prep.session.TaskRef); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("preparation grants permission: %v", err)
	}
	control.CommentID++
	control.Body = "/mcp-create " + prepared.Hash + " linked"
	intent, err := runtime.AdmitCreationApproval(t.Context(), control)
	mustProgress(t, err)
	prep.runner = lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) { return &agent.RunResult{}, nil })
	runtime.DisableMCP = true
	p, _, err := prepareMCP(t.Context(), prep, agent.Request{Mode: "research"}, nil)
	mustProgress(t, err)
	mustProgress(t, p.close(context.WithoutCancel(t.Context())))
	got, _, err := cs.EligibleCreationIntent(t.Context(), prep.session.TaskRef)
	mustProgress(t, err)
	if got != intent {
		t.Fatal("disabled registration consumed owner intent")
	}
	runtime.DisableMCP = false
	p, spec, err := prepareMCP(t.Context(), prep, agent.Request{Mode: "research"}, nil)
	mustProgress(t, err)
	defer func() { mustProgress(t, p.close(context.WithoutCancel(t.Context()))) }()
	if !slices.Contains(spec.PermittedTools(), "issues_create_batch") || len(p.creationProposal) == 0 {
		t.Fatalf("approved native proposal unavailable: %v", spec.PermittedTools())
	}
	var bound submission.CreationEnvelope
	mustProgress(t, json.Unmarshal(p.creationProposal, &bound))
	expected, err := json.Marshal(prepared.Envelope)
	mustProgress(t, err)
	actual, err := json.Marshal(bound)
	mustProgress(t, err)
	if string(actual) != string(expected) {
		t.Fatal("bound bytes differ from approval")
	}
	if _, _, err = cs.EligibleCreationIntent(t.Context(), prep.session.TaskRef); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("consumed intent remains eligible: %v", err)
	}
	replacement, nextSpec, err := prepareMCP(t.Context(), prep, agent.Request{Mode: "research"}, nil)
	mustProgress(t, err)
	defer func() { mustProgress(t, replacement.close(context.WithoutCancel(t.Context()))) }()
	if slices.Contains(nextSpec.PermittedTools(), "issues_create_batch") {
		t.Fatal("replacement inherited consumed creation grant")
	}
	if p.guard.Check(t.Context()) == nil {
		t.Fatal("obsolete launch mutation authority remains active")
	}
}

func TestPublicationRuntimeCreationRestartObservation(t *testing.T) {
	f := creationHTTPSetup(t, true)
	f.lost = true
	reply := f.call(t)
	if reply.Error == "" {
		t.Fatal("lost response confirmed")
	}
	db, err := store.Open(t.Context(), f.db.Path())
	mustProgress(t, err)
	defer db.Close()
	cs, err := store.NewCreationStore(db, func() time.Time { return f.now })
	mustProgress(t, err)
	auth, err := NewCreationAuthorizer(CreationAuthorizerOptions{Store: cs, Enabled: false, OwnerID: 10, BotID: 20})
	mustProgress(t, err)
	authority, err := NewAttemptAuthority(t.Context(), db)
	mustProgress(t, err)
	defer func() { mustProgress(t, authority.Close(context.WithoutCancel(t.Context()))) }()
	runtime := &MCPRuntime{Authority: authority, CreationAuthorizer: auth, Creation: f.service.forge, Redactor: redact.New(redact.Options{}), BotID: 20, Paths: config.NewPaths(t.TempDir())}
	rows, err := db.PendingCreationOperations(t.Context(), 0, 1)
	mustProgress(t, err)
	if len(rows) != 1 {
		t.Fatalf("pending rows %v", rows)
	}
	more, err := db.PendingCreationOperations(t.Context(), rows[0].ID, 1)
	mustProgress(t, err)
	if len(more) != 0 {
		t.Fatal("keyset repeats operation")
	}
	observed, _, err := runtime.ObserveCreation(t.Context(), rows[0], "open", 1)
	mustProgress(t, err)
	if observed.State != "confirmed" {
		t.Fatalf("observation %+v", observed)
	}
	rows, err = db.PendingCreationOperations(t.Context(), 0, 1)
	mustProgress(t, err)
	if len(rows) != 0 {
		t.Fatal("confirmed effect remains pending")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.posts != 1 || f.scans != 1 {
		t.Fatalf("redispatch or unbounded observation: posts %d scans %d", f.posts, f.scans)
	}
}
