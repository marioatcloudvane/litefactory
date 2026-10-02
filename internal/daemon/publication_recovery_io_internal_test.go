package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
	"github.com/cloudvane/litefactory/internal/turn"
)

type publicationIO struct {
	creationPosts               int
	creationTitle, creationBody string
	creationPages               []string
	db                          *store.DB
	path                        string
	session                     store.Session
	d                           *Daemon
	runtime                     *turn.MCPRuntime
	cs                          *store.CreationStore
	now                         time.Time
	mu                          sync.Mutex
	posts                       int
	bodies                      []string
	postPaths                   []string
	pages                       []string
	lost                        bool
	// createOK makes the fake forge answer an issue POST with a well-formed
	// bot-authored issue instead of a truncated reply.
	createOK bool
	server   *httptest.Server
}

func publicationMust(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func newPublicationIO(t *testing.T) *publicationIO {
	t.Helper()
	f := &publicationIO{now: time.Now().UTC().Add(-time.Second), path: filepath.Join(t.TempDir(), "state.db")}
	var err error
	f.db, err = store.Open(t.Context(), f.path)
	publicationMust(t, err)
	t.Cleanup(func() { publicationMust(t, f.db.Close()) })
	publicationMust(t, f.db.Migrate(t.Context()))
	root := t.TempDir()
	repo, err := f.db.UpsertRepo(t.Context(), store.Repo{Path: root, Forge: store.ForgeGitHub, Owner: "owner", Name: "api", RemoteURL: "git@github.com:owner/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(root, "worktrees")})
	publicationMust(t, err)
	f.session, err = f.db.GetOrCreateSession(t.Context(), repo, 278)
	publicationMust(t, err)
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repos/owner/api/issues" {
			if r.Method == http.MethodPost {
				f.creationPosts++
				var a submission.CreationArtifact
				if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
					t.Error(err)
				}
				f.creationTitle, f.creationBody = a.Title, a.Body
				w.WriteHeader(http.StatusCreated)
				if f.createOK {
					_ = json.NewEncoder(w).Encode(map[string]any{"id": 9000 + f.creationPosts, "number": 900 + f.creationPosts, "title": a.Title, "body": a.Body, "state": "open", "user": map[string]any{"id": 20, "type": "Bot"}})
					return
				}
				_, _ = io.WriteString(w, `{"id":`)
				return
			}
			state, page := r.URL.Query().Get("state"), r.URL.Query().Get("page")
			f.creationPages = append(f.creationPages, state+":"+page)
			if r.URL.Query().Get("per_page") != "100" {
				t.Error("unbounded creation page")
			}
			if state == "open" && page == "1" {
				w.Header().Set("Link", fmt.Sprintf(`<%s/repos/owner/api/issues?state=open&page=2&per_page=100>; rel="next"`, f.server.URL))
			}
			if state == "closed" {
				_ = json.NewEncoder(w).Encode([]any{map[string]any{"id": 8877, "number": 888, "title": f.creationTitle, "body": f.creationBody, "state": "closed", "user": map[string]any{"id": 20, "type": "Bot"}}})
				return
			}
			_, _ = io.WriteString(w, `[]`)
			return
		}
		// Issue 200 is a Plan story sub-issue of parent 278: a stale reply there is
		// answered on that sub-issue.
		issue := "278"
		if strings.HasPrefix(r.URL.Path, "/repos/owner/api/issues/200/") {
			issue = "200"
		}
		if r.URL.Path != "/repos/owner/api/issues/278/comments" && r.URL.Path != "/repos/owner/api/issues/278" && r.URL.Path != "/repos/owner/api/issues/200/comments" {
			t.Errorf("foreign path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodPost {
			f.posts++
			f.postPaths = append(f.postPaths, r.URL.Path)
			var p struct {
				Body string `json:"body"`
			}
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
				t.Error(err)
			}
			f.bodies = append(f.bodies, p.Body)
			w.WriteHeader(http.StatusCreated)
			if f.lost {
				_, _ = io.WriteString(w, `{"id":`)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 500 + f.posts, "body": p.Body, "user": map[string]any{"id": 20, "type": "Bot"}})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/comments") {
			page := r.URL.Query().Get("page")
			f.pages = append(f.pages, page)
			if r.URL.Query().Get("per_page") != "100" {
				t.Error("unbounded page")
			}
			if page == "1" {
				w.Header().Set("Link", fmt.Sprintf(`<%s/repos/owner/api/issues/%s/comments?page=2&per_page=100>; rel="next"`, f.server.URL, issue))
				_, _ = io.WriteString(w, `[]`)
				return
			}
			comments := []any{}
			for i, body := range f.bodies {
				id := 991 + i
				comments = append(comments, map[string]any{"id": id, "body": body, "html_url": fmt.Sprintf("https://github.com/owner/api/issues/%s#issuecomment-%d", issue, id), "user": map[string]any{"id": 20, "type": "Bot"}})
			}
			_ = json.NewEncoder(w).Encode(comments)
			return
		}
		_, _ = io.WriteString(w, `{"id":2780,"number":278,"state":"open"}`)
	}))
	t.Cleanup(f.server.Close)
	client, err := github.New(github.Options{APIBase: f.server.URL, HTTPClient: f.server.Client(), Token: secrets.NewToken("offline-token")})
	publicationMust(t, err)
	redactor := redact.New(redact.Options{})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	f.runtime = &turn.MCPRuntime{Paths: config.NewPaths(t.TempDir()), Comments: client, Creation: client, Redactor: redactor, BotID: 20}
	f.reopenRuntime(t)
	t.Cleanup(func() { publicationMust(t, f.runtime.Authority.Close(context.WithoutCancel(t.Context()))) })
	renderer, err := report.New(report.Options{Redactor: redactor, Limit: github.CommentSizeLimit, Logger: logger})
	publicationMust(t, err)
	reporter, err := turn.NewReporter(turn.ReporterOptions{DB: f.db, Forge: client, Renderer: renderer, Logger: logger, BotUserID: 20, StoryLockDir: t.TempDir()})
	publicationMust(t, err)
	f.d = &Daemon{db: f.db, turn: turnExecutor{opts: turn.Options{MCP: f.runtime}}, reporter: reporter, log: logger, interval: time.Minute, now: func() time.Time { return f.now }, diagnosticRedactor: redactor}
	filter, err := turn.NewFilter(f.db, turn.Identities{OwnerUserID: 10, BotUserID: 20}, logger)
	publicationMust(t, err)
	f.d.filter = filter
	return f
}
func (f *publicationIO) reopenRuntime(t *testing.T) {
	t.Helper()
	var err error
	f.runtime.Authority, err = turn.NewAttemptAuthority(t.Context(), f.db)
	publicationMust(t, err)
	f.cs, err = store.NewCreationStore(f.db, func() time.Time { return f.now })
	publicationMust(t, err)
	f.runtime.CreationAuthorizer, err = turn.NewCreationAuthorizer(turn.CreationAuthorizerOptions{Store: f.cs, Enabled: true, OwnerID: 10, BotID: 20})
	publicationMust(t, err)
}
func (f *publicationIO) binding(t *testing.T) store.ResultBinding {
	t.Helper()
	seg, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, 1, "result", store.AgentCodex)
	publicationMust(t, err)
	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: seg.ID, Mode: "research", Agent: store.AgentCodex, TriggerCommentID: 1, LogPath: func(int) string { return "offline.log" }})
	publicationMust(t, err)
	b, err := f.db.BindResultAttempt(t.Context(), store.ResultAttempt{Task: f.session.TaskRef, Mode: "research", WorkContext: "approved", RunID: run.ID, Attempt: "test", SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2})
	publicationMust(t, err)
	return b
}
func TestPublicationDaemonIO_CommentRestartBoundedPages(t *testing.T) {
	f := newPublicationIO(t)
	b := f.binding(t)
	receipts, err := turn.NewReceiptService(f.db, b, nil, map[string]turn.FamilyProjection{"comment": turn.CommentReceiptProjection(f.db), "tool:issues_comment": turn.CommentReceiptProjection(f.db)})
	publicationMust(t, err)
	service, err := turn.NewIssuePublicationService(f.db, receipts, f.runtime.Comments, f.runtime.Redactor, []int{278}, 20)
	publicationMust(t, err)
	args := []byte(`{"issue_number":278,"message":"status ghp_0123456789abcdefghijklmnopqrstuvwxyz"}`)
	_, _, err = receipts.Begin(t.Context(), "lost-comment", "issues_comment", args)
	publicationMust(t, err)
	f.mu.Lock()
	f.lost = true
	f.mu.Unlock()
	if _, err = service.HandleComment(t.Context(), "lost-comment", args); err == nil {
		t.Fatal("lost reply accepted")
	}
	scope := store.FamilyScope{Repository: "owner/api", Task: b.Task, RunID: b.RunID, Attempt: b.Attempt}
	op, err := f.db.GetCommentOperation(t.Context(), scope, "lost-comment")
	publicationMust(t, err)
	if op.State != "uncertain" || op.CommentID != 0 || strings.Contains(op.Body, "ghp_") {
		t.Fatalf("unsafe premature intent %+v", op)
	}
	publicationMust(t, f.db.EndResultAttempt(t.Context(), b))
	publicationMust(t, f.runtime.Authority.Close(context.WithoutCancel(t.Context())))
	publicationMust(t, f.db.Close())
	f.db, err = store.Open(t.Context(), f.path)
	publicationMust(t, err)
	publicationMust(t, f.db.Migrate(t.Context()))
	f.reopenRuntime(t)
	f.d.db = f.db
	publicationMust(t, f.d.pollPublications(t.Context(), f.now))
	op, err = f.db.GetCommentOperation(t.Context(), scope, "lost-comment")
	publicationMust(t, err)
	if op.State != "uncertain" || f.d.publications.commentPage != 2 {
		t.Fatalf("first page completed early %+v", op)
	}
	publicationMust(t, f.d.pollPublications(t.Context(), f.now.Add(time.Minute-time.Nanosecond)))
	f.mu.Lock()
	calls := len(f.pages)
	f.mu.Unlock()
	if calls != 1 {
		t.Fatal("early tick performed I/O")
	}
	publicationMust(t, f.d.pollPublications(t.Context(), f.now.Add(time.Minute)))
	op, err = f.db.GetCommentOperation(t.Context(), scope, "lost-comment")
	publicationMust(t, err)
	if op.State != "confirmed" || op.CommentID != 991 {
		t.Fatalf("observation did not confirm exact numeric receipt %+v", op)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.posts != 1 || strings.Join(f.pages, ",") != "1,2" {
		t.Fatalf("redispatch or unbounded pages: posts %d pages %v", f.posts, f.pages)
	}
}
func TestPublicationDaemonIO_TrustedPreparationApproval(t *testing.T) {
	f := newPublicationIO(t)
	ref, err := naming.ParseRef(f.session.TaskRef)
	publicationMust(t, err)
	task := &task{log: f.d.log, session: f.session, ref: ref, forgeRepo: github.Repo{Owner: "owner", Name: "api"}, issue: github.Issue{Labels: []string{"Mode: Research"}}, tickAt: f.now}
	envelope := submission.CreationEnvelope{Version: 1, Repository: "owner/api", SourceTask: f.session.TaskRef, Relationship: "linked", Artifacts: []submission.CreationArtifact{{Title: "Exact requested issue", Body: "status ghp_0123456789abcdefghijklmnopqrstuvwxyz"}}}
	data, err := json.Marshal(envelope)
	publicationMust(t, err)
	c := github.Comment{ID: 70, Body: "/mcp-create prepare " + string(data), CreatedAt: f.now, Author: github.User{ID: 10, Login: "owner"}}
	publicationMust(t, f.d.handleComment(t.Context(), task, c))
	f.mu.Lock()
	posts := f.posts
	var body string
	if len(f.bodies) > 0 {
		body = f.bodies[0]
	}
	f.mu.Unlock()
	if posts != 1 || strings.Contains(body, "ghp_") || !strings.Contains(body, "Exact requested issue") || !strings.Contains(body, "278") {
		t.Fatalf("missing exact redacted presentation: posts=%d body=%s", posts, body)
	}
	if _, _, err = f.cs.EligibleCreationIntent(t.Context(), f.session.TaskRef); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("prepare granted authority %v", err)
	}
	// Deterministic preparation uses the same marker/hash even when replayed.
	prepared, err := f.runtime.PrepareCreationProposal(t.Context(), turn.OwnerGrantControl{Repository: "owner/api", SourceTask: f.session.TaskRef, Mode: "research", Body: c.Body, CommentID: c.ID, ActorID: 10, CreatedAt: f.now})
	publicationMust(t, err)
	if !strings.Contains(body, prepared.Hash) {
		t.Fatal("presented hash drifted on preparation retry")
	}
	bad := c
	bad.ID++
	bad.Author.ID = 11
	bad.Author.Login = "owner"
	bad.Body = "/mcp-create " + prepared.Hash + " linked"
	publicationMust(t, f.d.handleComment(t.Context(), task, bad))
	if _, _, err = f.cs.EligibleCreationIntent(t.Context(), f.session.TaskRef); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("display-name spoof granted authority %v", err)
	}
	good := bad
	good.ID++
	good.Author.ID = 10
	publicationMust(t, f.d.handleComment(t.Context(), task, good))
	source := store.CreationSource{Repository: "owner/api", SourceTask: f.session.TaskRef, CommentID: good.ID, OwnerID: 10}
	admitted, err := f.db.GetCreationAcknowledgment(t.Context(), source)
	publicationMust(t, err)
	if admitted.IntentID == 0 || admitted.Hash != prepared.Hash || admitted.Relationship != "linked" || f.creationPosts != 1 {
		t.Fatalf("approval not published %+v creations=%d", admitted, f.creationPosts)
	}
	// The factory published it, so no agent run may bind it a second time.
	if _, _, err = f.cs.EligibleCreationIntent(t.Context(), f.session.TaskRef); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("published approval still grantable: %v", err)
	}
	publicationMust(t, f.d.handleComment(t.Context(), task, good))
	again, err := f.db.GetCreationAcknowledgment(t.Context(), source)
	publicationMust(t, err)
	if again.IntentID != admitted.IntentID || f.creationPosts != 1 {
		t.Fatalf("approval retry minted new intent or republished: creations=%d", f.creationPosts)
	}
}

func TestPublicationDaemonIO_CreationRestartOpenClosedPages(t *testing.T) {
	f := newPublicationIO(t)
	b := f.binding(t)
	receipts, err := turn.NewReceiptService(f.db, b, nil, map[string]turn.FamilyProjection{"creation_publication": turn.CreationPublicationProjection(f.db, f.cs), "tool:issues_create_batch": turn.CreationPublicationProjection(f.db, f.cs)})
	publicationMust(t, err)
	service, err := turn.NewCreationPublicationService(f.db, receipts, f.runtime.CreationAuthorizer, f.runtime.Creation, f.runtime.Redactor, t.TempDir())
	publicationMust(t, err)
	prepared, err := service.Prepare(t.Context(), submission.CreationEnvelope{Artifacts: []submission.CreationArtifact{{Title: "Retained issue", Body: "Exact ghp_0123456789abcdefghijklmnopqrstuvwxyz", LinkIssues: []int{278}}}})
	publicationMust(t, err)
	id, err := f.runtime.CreationAuthorizer.AdmitOwnerGrantIntent(t.Context(), turn.OwnerGrantControl{Repository: "owner/api", SourceTask: b.Task, Mode: "research", Body: "/mcp-create " + prepared.Hash + " linked", CommentID: 90, ActorID: 10, CreatedAt: f.now})
	publicationMust(t, err)
	guard, err := f.runtime.Authority.Bind(t.Context(), b, strings.Repeat("a", 64), time.Now().Add(time.Hour))
	publicationMust(t, err)
	publicationMust(t, service.BindGuard(guard))
	_, err = f.runtime.CreationAuthorizer.BindCreationGrant(t.Context(), guard, id)
	publicationMust(t, err)
	args, err := json.Marshal(prepared.Envelope)
	publicationMust(t, err)
	live, release, err := guard.Context(t.Context())
	publicationMust(t, err)
	_, _, err = receipts.Begin(live, "lost-creation", "issues_create_batch", args)
	publicationMust(t, err)
	release()
	if _, err = service.HandleCreate(t.Context(), "lost-creation", args); err == nil {
		t.Fatal("lost create reply accepted")
	}
	rows, err := f.db.PendingCreationOperations(t.Context(), 0, 1)
	publicationMust(t, err)
	if len(rows) != 1 {
		t.Fatalf("pending creation missing %+v", rows)
	}
	opID := rows[0].ID
	live, release, err = guard.Context(t.Context())
	publicationMust(t, err)
	receipt, err := receipts.Get(live, "lost-creation")
	publicationMust(t, err)
	release()
	if receipt.States.Effects != "uncertain" || len(receipt.Publications) != 1 || receipt.Publications[0].IssueID != 0 || receipt.Publications[0].IssueNumber != 0 {
		t.Fatalf("premature receipt %+v", receipt)
	}
	publicationMust(t, f.runtime.Authority.Close(context.WithoutCancel(t.Context())))
	publicationMust(t, f.db.EndResultAttempt(t.Context(), b))
	publicationMust(t, f.db.Close())
	f.db, err = store.Open(t.Context(), f.path)
	publicationMust(t, err)
	publicationMust(t, f.db.Migrate(t.Context()))
	f.reopenRuntime(t)
	f.d.db = f.db
	// Disabling future creation must not disable observation of existing effects.
	f.runtime.CreationAuthorizer, err = turn.NewCreationAuthorizer(turn.CreationAuthorizerOptions{Store: f.cs, Enabled: false, OwnerID: 10, BotID: 20})
	publicationMust(t, err)
	for i := range 3 {
		publicationMust(t, f.d.pollPublications(t.Context(), f.now.Add(time.Duration(i)*time.Minute)))
		op, err := f.cs.GetCreationOperation(t.Context(), opID)
		publicationMust(t, err)
		if i < 2 && op.State != "uncertain" {
			t.Fatalf("premature success at page %d: %+v", i, op)
		}
		if i == 2 && op.State != "confirmed" {
			t.Fatalf("closed issue not recovered %+v", op)
		}
	}
	details, err := f.db.CreationPublicationArtifacts(t.Context(), opID)
	publicationMust(t, err)
	if len(details) != 1 || details[0].IssueID != 8877 || details[0].IssueNumber != 888 || details[0].Relationship != "linked" {
		t.Fatalf("wrong recovered identity %+v", details)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.creationPosts != 1 || strings.Join(f.creationPages, ",") != "open:1,open:2,closed:1" || strings.Contains(f.creationBody, "ghp_") {
		t.Fatalf("unsafe recovery: posts=%d pages=%v body=%s", f.creationPosts, f.creationPages, f.creationBody)
	}
}
