package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

type bugAgentCase struct {
	name         string
	labels       []string
	parent       bool
	parentLabels []string
	parentRuns   []string
	bugAgent     agent.Kind
	want         string
	source       string
	refuse       bool
	wantError    bool
	removeClaude bool
}

func TestEffectiveBugAgentPrecedence(t *testing.T) {
	for _, tc := range []bugAgentCase{
		{name: "final first-agent fallback", want: "claude", source: "default"},
		{name: "configured Bug default", bugAgent: "codex", want: "codex", source: "bug_agent"},
		{name: "parent Claude overrides Codex setting", parent: true, parentLabels: []string{"Coding Agent: Claude"}, bugAgent: "codex", want: "claude", source: "parent"},
		{name: "parent Claude without Bug setting", parent: true, parentLabels: []string{"Coding Agent: Claude"}, want: "claude", source: "parent"},
		{name: "parent Codex without Bug setting", parent: true, parentLabels: []string{"Coding Agent: Codex"}, want: "codex", source: "parent"},
		{name: "explicit Codex overrides parent and setting", labels: []string{"Coding Agent: Codex"}, parent: true, parentLabels: []string{"Coding Agent: Claude"}, bugAgent: "claude", want: "codex", source: "label"},
		{name: "parent has no agent uses Bug setting", parent: true, bugAgent: "codex", want: "codex", source: "bug_agent"},
		{name: "parent has no agent uses first configured", parent: true, want: "claude", source: "default"},
		{name: "recorded parent agent beats parent label", parent: true, parentRuns: []string{"codex"}, parentLabels: []string{"Coding Agent: Claude"}, bugAgent: "claude", want: "codex", source: "parent"},
		{name: "latest failed parent run beats earlier run and label", parent: true, parentRuns: []string{"codex", "claude"}, parentLabels: []string{"Coding Agent: Codex"}, bugAgent: "codex", want: "claude", source: "parent"},
		{name: "recorded parent bypasses malformed label", parent: true, parentRuns: []string{"codex"}, parentLabels: []string{"coding agent: Claude"}, want: "codex", source: "parent"},
		{name: "unknown explicit label refuses", labels: []string{"Coding Agent: Unknown"}, bugAgent: "codex", refuse: true, source: "label"},
		{name: "malformed explicit label refuses", labels: []string{"Coding Agent: codex"}, bugAgent: "codex", refuse: true, source: "label"},
		{name: "ambiguous explicit label refuses", labels: []string{"Coding Agent: Claude", "Coding Agent: Codex"}, refuse: true, source: "label"},
		{name: "unknown parent label refuses", parent: true, parentLabels: []string{"Coding Agent: Unknown"}, bugAgent: "codex", refuse: true, source: "parent"},
		{name: "malformed parent label refuses", parent: true, parentLabels: []string{"Coding Agent: claude"}, bugAgent: "codex", refuse: true, source: "parent"},
		{name: "ambiguous parent label refuses", parent: true, parentLabels: []string{"Coding Agent: Claude", "Coding Agent: Codex"}, bugAgent: "codex", refuse: true, source: "parent"},
		{name: "removed parent recorded agent fails", parent: true, parentRuns: []string{"claude"}, removeClaude: true, bugAgent: "codex", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, task, queryCount, parentReads, parent := bugAgentTestDaemon(t, tc, nil)
			decision, source, err := d.effectiveAgent(t.Context(), task, agent.WorkflowBug)
			if tc.wantError {
				if err == nil {
					t.Fatalf("selection bypassed unavailable inherited agent: %+v", decision)
				}
				return
			}
			if err != nil || decision.refused() != tc.refuse || source != tc.source || (!tc.refuse && decision.Agent != tc.want) {
				t.Fatalf("decision=%+v source=%q err=%v; want agent=%q source=%q refuse=%t", decision, source, err, tc.want, tc.source, tc.refuse)
			}
			if len(tc.labels) > 0 && queryCount.Load() != 0 {
				t.Fatal("explicit valid or refused label did not short-circuit parent query")
			}
			if len(tc.parentRuns) > 0 && parentReads.Load() != 0 {
				t.Fatal("recorded parent agent needlessly read mutable parent labels")
			}
			if tc.parent && len(tc.parentRuns) == 0 {
				if _, err := d.db.GetSessionByTicket(t.Context(), parent.Identity); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("selection created a parent session: %v", err)
				}
			}
		})
	}
}

func TestEffectiveBugAgentParentErrorsNeverFallThrough(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{name: "inaccessible child", status: http.StatusOK, body: `{"data":{"repository":{"issue":null}}}`},
		{name: "unsupported endpoint", status: http.StatusNotFound, body: `{"message":"Not Found"}`, want: github.ErrNotFound},
		{name: "rate limited", status: http.StatusTooManyRequests, body: `{"message":"rate limit"}`, want: github.ErrRateLimited},
		{name: "partial GraphQL response", status: http.StatusOK, body: `{"data":{"repository":{"issue":null}},"errors":[{"type":"FORBIDDEN"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, task, _, _, _ := bugAgentTestDaemon(t, bugAgentCase{bugAgent: "codex"}, func(w http.ResponseWriter) {
				w.Header().Set("Retry-After", "17")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			decision, _, err := d.effectiveAgent(t.Context(), task, agent.WorkflowBug)
			if err == nil || decision.Agent != "" || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Fatalf("parent read error bypassed inheritance: decision=%+v err=%v", decision, err)
			}
		})
	}
	d, task, _, _, _ := bugAgentTestDaemon(t, bugAgentCase{bugAgent: "codex"}, nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if decision, _, err := d.effectiveAgent(ctx, task, agent.WorkflowBug); !errors.Is(err, context.Canceled) || decision.Agent != "" {
		t.Fatalf("cancelled observation selected fallback: decision=%+v err=%v", decision, err)
	}
}

func TestEffectiveAgentKeepsNonBugDefault(t *testing.T) {
	d, task, queries, _, _ := bugAgentTestDaemon(t, bugAgentCase{bugAgent: "codex", parent: true, parentLabels: []string{"Coding Agent: Codex"}}, nil)
	decision, source, err := d.effectiveAgent(t.Context(), task, agent.WorkflowImplement)
	if err != nil || decision.Agent != "claude" || source != "default" || queries.Load() != 0 {
		t.Fatalf("Bug preference changed Story selection: decision=%+v source=%s queries=%d err=%v", decision, source, queries.Load(), err)
	}
}

func TestBugContinuationUsesSameSelectionAndCancelsProviderChanges(t *testing.T) {
	for _, tc := range []struct {
		name        string
		selection   bugAgentCase
		savedAgent  string
		cancelled   bool
		parentError bool
	}{
		{name: "Bug setting matches recorded Codex", selection: bugAgentCase{bugAgent: "codex"}, savedAgent: "codex"},
		{name: "Claude parent overrides Codex setting", selection: bugAgentCase{parent: true, parentLabels: []string{"Coding Agent: Claude"}, bugAgent: "codex"}, savedAgent: "claude"},
		{name: "explicit Codex overrides Claude parent", selection: bugAgentCase{labels: []string{"Coding Agent: Codex"}, parent: true, parentLabels: []string{"Coding Agent: Claude"}, bugAgent: "claude"}, savedAgent: "codex"},
		{name: "provider change cancels old conversation", selection: bugAgentCase{bugAgent: "codex"}, savedAgent: "claude", cancelled: true},
		{name: "invalid label cancels old conversation", selection: bugAgentCase{labels: []string{"Coding Agent: Unknown"}, bugAgent: "codex"}, savedAgent: "claude", cancelled: true},
		{name: "parent failure preserves pending recovery", selection: bugAgentCase{bugAgent: "codex"}, savedAgent: "codex", parentError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var failure func(http.ResponseWriter)
			if tc.parentError {
				failure = func(w http.ResponseWriter) { w.WriteHeader(http.StatusServiceUnavailable) }
			}
			d, task, _, _, _ := bugAgentTestDaemon(t, tc.selection, failure)
			continuation := bugAgentContinuation(t, d, task, tc.savedAgent)
			ready, err := d.observeContinuation(t.Context(), task, continuation)
			if tc.parentError {
				if err == nil || ready {
					t.Fatalf("parent failure allowed recovery: ready=%t err=%v", ready, err)
				}
			} else if err != nil || ready == tc.cancelled {
				t.Fatalf("continuation selection differs: ready=%t cancelled=%t err=%v", ready, tc.cancelled, err)
			}
			saved, err := d.db.GetContinuation(t.Context(), continuation.ID)
			if err != nil {
				t.Fatal(err)
			}
			wantStatus := "pending"
			if tc.cancelled {
				wantStatus = "cancelled"
			}
			if saved.Status != wantStatus || (tc.cancelled && saved.CancellationReason == "") {
				t.Fatalf("recovery decision not durable: %+v", saved)
			}
			run, err := d.db.GetRun(t.Context(), continuation.SourceRunID)
			if err != nil || run.Agent != tc.savedAgent {
				t.Fatalf("historical provider rewritten: run=%+v err=%v", run, err)
			}
		})
	}
}

func bugAgentContinuation(t *testing.T, d *Daemon, task *task, selected string) store.Continuation {
	t.Helper()
	repo, err := d.db.UpsertRepo(t.Context(), store.Repo{Path: filepath.Join(t.TempDir(), "continuation-clone"), Forge: store.ForgeGitHub, Owner: "owner", Name: "tasks", RemoteURL: "git@github.com:owner/tasks.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(t.TempDir(), "trees")})
	if err != nil {
		t.Fatal(err)
	}
	session, err := d.db.GetOrCreateSession(t.Context(), repo, 7)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.db.AdmitWorkflow(t.Context(), store.TaskWorkflowAdmission{SessionID: session.ID, Version: 1, Family: "standalone", Workflow: "bug"}); err != nil {
		t.Fatal(err)
	}
	if err := d.db.SetSessionWorktree(t.Context(), session.ID, "/saved/bug-worktree"); err != nil {
		t.Fatal(err)
	}
	session, err = d.db.GetSession(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	segment, err := d.db.GetOrCreateSegment(t.Context(), session.ID, store.FirstSegmentOrdinal, "saved-conversation", selected)
	if err != nil {
		t.Fatal(err)
	}
	run, err := d.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Workflow: "bug", Agent: selected, TriggerCommentID: 42, LogPath: func(int) string { return filepath.Join(t.TempDir(), "run.jsonl") }})
	if err != nil {
		t.Fatal(err)
	}
	at := run.StartedAt.Add(time.Second)
	resume := segment.SessionRef
	if selected != store.AgentClaude {
		resume = "saved-codex-token"
	}
	_, continuation, err := d.db.FinishInterruptedRun(t.Context(), run.ID, store.RunOutcome{Status: store.RunFailed, SemanticOutcome: "interrupted", FinishedAt: at}, &store.Continuation{DueAt: at.Add(2 * time.Minute), ResetBound: at.Add(time.Minute), Precision: "exact", Context: store.ContinuationContext{Task: session.TaskRef, Worktree: session.WorktreePath, Agent: selected, Workflow: "bug", Request: "continue original Bug work", ResumeIdentity: resume, OwnerCommentID: 42}}, resume)
	if err != nil {
		t.Fatal(err)
	}
	task.session, task.repo, task.tickAt = session, repo, at.Add(2*time.Minute)
	return *continuation
}

func bugAgentTestDaemon(t *testing.T, tc bugAgentCase, parentFailure func(http.ResponseWriter)) (*Daemon, *task, *atomic.Int32, *atomic.Int32, ticket.Ref) {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	queries, parentReads := new(atomic.Int32), new(atomic.Int32)
	var origin string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var response any
		switch r.URL.Path {
		case "/repos/owner/tasks/issues/7":
			labels := []map[string]string{}
			for _, label := range tc.labels {
				labels = append(labels, map[string]string{"name": label})
			}
			response = map[string]any{"id": 700, "number": 7, "url": origin + r.URL.Path, "state": "open", "labels": labels}
		case "/repos/owner/tasks/issues/7/comments":
			response = []any{}
		case "/repos/owner/tasks/issues/9":
			parentReads.Add(1)
			labels := []map[string]string{}
			for _, label := range tc.parentLabels {
				labels = append(labels, map[string]string{"name": label})
			}
			response = map[string]any{"id": 900, "number": 9, "url": origin + r.URL.Path, "labels": labels}
		case "/graphql":
			queries.Add(1)
			if parentFailure != nil {
				parentFailure(w)
				return
			}
			var parent any
			if tc.parent {
				parent = map[string]any{"databaseId": 900, "number": 9, "repository": map[string]string{"nameWithOwner": "owner/tasks"}}
			}
			response = map[string]any{"data": map[string]any{"repository": map[string]any{"issue": map[string]any{"databaseId": 700, "number": 7, "repository": map[string]string{"nameWithOwner": "owner/tasks"}, "parent": parent}}}}
		default:
			t.Errorf("unexpected selection request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	origin = server.URL
	client, err := github.New(github.Options{APIBase: origin, Token: secrets.NewToken("test-selection-token"), HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	d := &Daemon{db: db, forge: client, defaultAgent: "claude", bugAgent: tc.bugAgent, agents: turn.RunnerSet{"claude": nil, "codex": nil}}
	if tc.removeClaude {
		delete(d.agents, "claude")
	}
	parent := ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: ticket.Origin(origin), RemoteID: "900"}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: ticket.Origin(origin), Container: "owner/tasks", Key: "9"}}
	if len(tc.parentRuns) > 0 {
		bugAgentRecordParent(t, db, parent, tc.parentRuns)
	}
	return d, &task{forgeRepo: github.Repo{Owner: "owner", Name: "tasks"}, issue: github.Issue{ID: 700, Number: 7, Labels: tc.labels}}, queries, parentReads, parent
}

func bugAgentRecordParent(t *testing.T, db *store.DB, parent ticket.Ref, agents []string) {
	t.Helper()
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: filepath.Join(t.TempDir(), "clone"), Forge: store.ForgeGitHub, Owner: "owner", Name: "tasks", RemoteURL: "git@github.com:owner/tasks.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(t.TempDir(), "trees")})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), repo, 9)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.BindSessionTicket(t.Context(), session.ID, parent); err != nil {
		t.Fatal(err)
	}
	segment, err := db.GetOrCreateSegment(t.Context(), session.ID, store.FirstSegmentOrdinal, "github:owner/tasks#9/s01", store.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	for i, selected := range agents {
		run, err := db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Agent: selected, Mode: store.ModeSpike, TriggerCommentID: int64(i + 1), LogPath: func(ordinal int) string { return filepath.Join(t.TempDir(), strconv.Itoa(ordinal)+".jsonl") }})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.FinishRun(t.Context(), run.ID, store.RunOutcome{Status: store.RunFailed, Error: fmt.Sprintf("failed parent attempt %d", i+1)}); err != nil {
			t.Fatal(err)
		}
	}
}
