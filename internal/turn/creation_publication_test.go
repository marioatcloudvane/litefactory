package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

type creationHTTPFixture struct {
	onPost                    func()
	mu                        sync.Mutex
	posts, scans, attachments int
	lost, partial             bool
	items                     []github.Issue
	db                        *store.DB
	cs                        *store.CreationStore
	service                   *CreationPublicationService
	receipts                  *ReceiptService
	guard                     *AttemptGuard
	client                    submission.Client
	prepared                  PreparedCreation
	now                       time.Time
}

func creationHTTPSetup(t *testing.T, gate bool, attach bool) *creationHTTPFixture {
	t.Helper()
	f := &creationHTTPFixture{now: time.Now().UTC().Add(-time.Minute)}
	db, _, opts := agentResultFixture(t, "research")
	f.db = db
	app, err := BindAgentResultService(t.Context(), opts)
	mustProgress(t, err)
	f.cs, err = store.NewCreationStore(db, func() time.Time { return f.now })
	mustProgress(t, err)
	auth, err := NewCreationAuthorizer(CreationAuthorizerOptions{Store: f.cs, Enabled: gate, OwnerID: 10, BotID: 20})
	mustProgress(t, err)
	projection := CreationPublicationProjection(db, f.cs)
	f.receipts, err = NewReceiptService(db, app.Binding(), nil, map[string]FamilyProjection{"creation_publication": projection, "tool:issues_create_batch": projection})
	mustProgress(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if !strings.HasPrefix(r.URL.Path, "/repos/example/results/issues") {
			t.Errorf("foreign path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/sub_issues") {
			f.attachments++
			if f.partial {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		if r.Method == http.MethodPost {
			f.posts++
			var a submission.CreationArtifact
			if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
				t.Error(err)
				return
			}
			issue := github.Issue{ID: int64(900 + f.posts), Number: 400 + f.posts, Title: a.Title, Body: a.Body}
			issue.Author.ID = 20
			f.items = append(f.items, issue)
			if f.onPost != nil {
				f.onPost()
			}
			w.WriteHeader(http.StatusCreated)
			if f.lost {
				_, _ = w.Write([]byte(`{"id":`))
				return
			}
			_ = json.NewEncoder(w).Encode(creationWireIssue(issue))
			return
		}
		if r.URL.Path == "/repos/example/results/issues" {
			f.scans++
			if r.URL.Query().Get("per_page") != "100" || r.URL.Query().Get("page") != "1" {
				t.Error("unbounded scan")
			}
			items := make([]map[string]any, 0, len(f.items))
			for _, item := range f.items {
				items = append(items, creationWireIssue(item))
			}
			_ = json.NewEncoder(w).Encode(items)
			return
		}
		for _, i := range f.items {
			if r.URL.Path == fmt.Sprintf("/repos/example/results/issues/%d", i.Number) {
				_ = json.NewEncoder(w).Encode(creationWireIssue(i))
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 2630, "number": 263})
	}))
	t.Cleanup(server.Close)
	forge, err := github.New(github.Options{APIBase: server.URL, HTTPClient: server.Client(), Token: secrets.NewToken("fixture")})
	mustProgress(t, err)
	f.service, err = NewCreationPublicationService(db, f.receipts, auth, forge, redact.New(redact.Options{}), t.TempDir())
	mustProgress(t, err)
	envelope := submission.CreationEnvelope{Artifacts: []submission.CreationArtifact{{Title: "Reviewed", Body: "Exact body", LinkIssues: []int{280}}}}
	if attach {
		envelope.Relationship = "attach"
		envelope.Artifacts[0].ParentIssue = 263
		envelope.Artifacts = append(envelope.Artifacts, submission.CreationArtifact{Title: "Second", Body: "Do not dispatch after failure", ParentIssue: 263})
	}
	f.prepared, err = f.service.Prepare(t.Context(), envelope)
	mustProgress(t, err)
	// Trusted fixture approval must predate the next bound attempt; use the existing primitive directly.
	enabled, err := NewCreationAuthorizer(CreationAuthorizerOptions{Store: f.cs, Enabled: true, OwnerID: 10, BotID: 20})
	mustProgress(t, err)
	intent, err := enabled.AdmitOwnerGrantIntent(t.Context(), OwnerGrantControl{Repository: f.prepared.Envelope.Repository, SourceTask: app.Binding().Task, Mode: "research", Body: "/mcp-create " + f.prepared.Hash + " " + f.prepared.Envelope.Relationship, CommentID: 1, ActorID: 10, CreatedAt: f.now})
	mustProgress(t, err)
	feedback, err := NewFeedbackService(app, nil)
	mustProgress(t, err)
	f.client, f.guard, _ = familySocketFixture(t, db, FamilyDispatcherOptions{Service: app, Receipts: f.receipts, Feedback: feedback, AllowCreation: gate, Registrations: []FamilyRegistration{f.service.Registration()}})
	mustProgress(t, f.service.BindGuard(f.guard))
	_, err = enabled.BindCreationGrant(t.Context(), f.guard, intent)
	mustProgress(t, err)
	return f
}
func (f *creationHTTPFixture) call(t *testing.T) submission.Response {
	t.Helper()
	args, err := json.Marshal(f.prepared.Envelope)
	mustProgress(t, err)
	r, err := f.client.Call(t.Context(), "issues_create_batch", args)
	mustProgress(t, err)
	return r
}

func TestCreationHTTPExactBytesConcurrentRetry(t *testing.T) {
	f := creationHTTPSetup(t, true, false)
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			f.call(t) // Concurrent callers may observe pending; durable recovery follows below.
		})
	}
	wg.Wait()
	r := f.call(t)
	receipt, err := f.receipts.Get(t.Context(), r.RequestID)
	mustProgress(t, err)
	if receipt.States.Effects != "confirmed" {
		t.Fatalf("receipt %+v", receipt)
	}
	if len(receipt.Publications) != 1 || receipt.Publications[0].IssueID != 901 || receipt.Publications[0].IssueNumber != 401 {
		t.Fatalf("missing confirmed publication identity %+v", receipt)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.posts != 1 || len(f.items) != 1 || f.items[0].Body != f.prepared.Envelope.Artifacts[0].Body || f.items[0].Title != f.prepared.Envelope.Artifacts[0].Title {
		t.Fatalf("posts=%d items=%+v", f.posts, f.items)
	}
	if !strings.Contains(f.items[0].Body, "Related: https://github.com/example/results/issues/280") || !strings.Contains(f.items[0].Body, "<!-- lf:mcp-create") {
		t.Fatal("missing reviewed links/marker")
	}
}
func TestCreationHTTPDefaultOffRefusesLiveGrant(t *testing.T) {
	defaults := string(config.DefaultFileContents())
	for _, body := range []string{strings.Replace(defaults, "  allow_mcp_story_creation: false\n", "", 1), defaults} {
		t.Run(fmt.Sprint(len(body)), func(t *testing.T) {
			paths := config.NewPaths(t.TempDir())
			mustProgress(t, os.WriteFile(paths.ConfigPath(), []byte(body), 0600))
			cfg, err := config.Load(paths)
			mustProgress(t, err)
			f := creationHTTPSetup(t, cfg.Settings.AllowMCPStoryCreation, false)
			catalog, err := f.client.List(t.Context())
			mustProgress(t, err)
			if strings.Contains(string(catalog.Result), "issues_create_batch") {
				t.Fatal("gate-off advertised creation")
			}
			r := f.call(t)
			if r.Error == "" {
				t.Fatal("gate-off accepted live grant")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.posts != 0 {
				t.Fatal("gate-off published")
			}
		})
	}
}
func TestCreationHTTPLostReplyReopenObservation(t *testing.T) {
	f := creationHTTPSetup(t, true, false)
	f.lost = true
	r := f.call(t)
	if r.Error == "" {
		t.Fatal("lost response confirmed")
	}
	reopened, err := store.Open(t.Context(), f.db.Path())
	mustProgress(t, err)
	defer reopened.Close()
	cs, err := store.NewCreationStore(reopened, func() time.Time { return f.now })
	mustProgress(t, err)
	projection := CreationPublicationProjection(reopened, cs)
	receipts, err := NewReceiptService(reopened, f.receipts.Binding(), nil, map[string]FamilyProjection{"creation_publication": projection, "tool:issues_create_batch": projection})
	mustProgress(t, err)
	auth, err := NewCreationAuthorizer(CreationAuthorizerOptions{Store: cs, Enabled: true, OwnerID: 10, BotID: 20})
	mustProgress(t, err)
	svc, err := NewCreationPublicationService(reopened, receipts, auth, f.service.forge, redact.New(redact.Options{}), t.TempDir())
	mustProgress(t, err)
	id, err := reopened.CreationPublicationRequest(t.Context(), receipts.scope, r.RequestID)
	mustProgress(t, err)
	before, err := receipts.Get(t.Context(), r.RequestID)
	mustProgress(t, err)
	if before.States.Effects != "uncertain" {
		t.Fatalf("before %+v", before)
	}
	if _, err = svc.Reconcile(t.Context(), id, "open", 1, nil); err == nil {
		t.Fatal("unauthorized observation")
	}
	for range 3 {
		f.call(t)
	}
	op, err := svc.Reconcile(t.Context(), id, "open", 1, func(context.Context, store.CreationProposal) error { return nil })
	mustProgress(t, err)
	if op.State != "confirmed" {
		t.Fatalf("op %+v", op)
	}
	got, err := receipts.Get(t.Context(), r.RequestID)
	mustProgress(t, err)
	if got.States.Effects != "confirmed" {
		t.Fatalf("got %+v", got)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.posts != 1 || f.scans != 1 {
		t.Fatalf("posts=%d scans=%d", f.posts, f.scans)
	}
}
func TestCreationHTTPPartialRelationshipStopsBatch(t *testing.T) {
	f := creationHTTPSetup(t, true, true)
	f.partial = true
	r := f.call(t)
	if r.Error == "" {
		t.Fatal("partial batch confirmed")
	}
	for range 3 {
		f.call(t)
	}
	got, err := f.receipts.Get(t.Context(), r.RequestID)
	mustProgress(t, err)
	if got.States.Effects != "uncertain" {
		t.Fatalf("receipt %+v", got)
	}
	id, err := f.db.CreationPublicationRequest(t.Context(), f.receipts.scope, r.RequestID)
	mustProgress(t, err)
	details, err := f.db.CreationPublicationArtifacts(t.Context(), id)
	mustProgress(t, err)
	if len(details) != 1 || details[0].IssueID != 901 || details[0].Relationship != "uncertain" {
		t.Fatalf("details %+v", details)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.posts != 1 || f.attachments != 1 {
		t.Fatalf("posts=%d attachments=%d", f.posts, f.attachments)
	}
}

func creationWireIssue(i github.Issue) map[string]any {
	return map[string]any{"id": i.ID, "number": i.Number, "title": i.Title, "body": i.Body, "state": "open", "user": map[string]any{"id": i.Author.ID}}
}

func TestCreationHTTPRefusesEnvelopeMutationAndExpiredReplay(t *testing.T) {
	for _, kind := range []string{"body", "title", "source", "repository", "relationship", "links", "expiry", "revoked"} {
		t.Run(kind, func(t *testing.T) {
			f := creationHTTPSetup(t, true, false)
			switch kind {
			case "body":
				f.prepared.Envelope.Artifacts[0].Body += " changed"
			case "title":
				f.prepared.Envelope.Artifacts[0].Title += " changed"
			case "source":
				f.prepared.Envelope.SourceTask = "github:example/results#264"
			case "repository":
				f.prepared.Envelope.Repository = "foreign/repo"
			case "relationship":
				f.prepared.Envelope.Relationship = "attach"
				f.prepared.Envelope.Artifacts[0].ParentIssue = 263
			case "links":
				f.prepared.Envelope.Artifacts[0].LinkIssues = []int{281}
			case "expiry":
				f.now = f.now.Add(31 * time.Minute)
			case "revoked":
				mustProgress(t, f.guard.Revoke(t.Context(), "cancelled"))
			}
			if r := f.call(t); r.Error == "" {
				t.Fatal("invalid authority/content admitted")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.posts != 0 {
				t.Fatal("refused request published")
			}
		})
	}
	t.Run("expiry after confirmed operation", func(t *testing.T) {
		f := creationHTTPSetup(t, true, false)
		r := f.call(t)
		if r.Error != "" {
			t.Fatal(r.Error)
		}
		f.now = f.now.Add(31 * time.Minute)
		if retry := f.call(t); retry.Error == "" {
			t.Fatal("expired grant replay accepted")
		}
		receipt, err := f.receipts.Get(t.Context(), r.RequestID)
		mustProgress(t, err)
		if receipt.States.Effects != "confirmed" {
			t.Fatalf("expired replay damaged outcome %+v", receipt)
		}
	})
}

func TestCreationHTTPCancellationAfterDispatchNeverRecreates(t *testing.T) {
	f := creationHTTPSetup(t, true, false)
	args, err := json.Marshal(f.prepared.Envelope)
	mustProgress(t, err)
	const request = "cancelled-creation"
	_, _, err = f.receipts.Begin(t.Context(), request, "issues_create_batch", args)
	mustProgress(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	posted := make(chan struct{})
	f.onPost = func() {
		id, e := f.db.CreationPublicationRequest(t.Context(), f.receipts.scope, request)
		if e != nil {
			t.Error(e)
		} else {
			details, e := f.db.CreationPublicationArtifacts(t.Context(), id)
			if e != nil || len(details) != 1 || !details[0].Dispatched {
				t.Errorf("intent not persisted before response: %+v %v", details, e)
			}
		}
		cancel()
		close(posted)
	}
	if _, err = f.service.HandleCreate(ctx, request, args); err == nil {
		t.Fatal("cancelled call confirmed")
	}
	<-posted
	f.mu.Lock()
	f.onPost = nil
	f.mu.Unlock()
	receipt, err := f.receipts.Get(t.Context(), request)
	mustProgress(t, err)
	if receipt.States.Effects == "confirmed" {
		t.Fatalf("cancelled outcome %+v", receipt)
	}
	// A fresh journal identity still recovers the same grant-scoped operation.
	f.call(t)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.posts != 1 {
		t.Fatalf("cancelled POST repeated %d", f.posts)
	}
}

func TestCreationHTTPObservationRequiresExactUnambiguousProvenance(t *testing.T) {
	f := creationHTTPSetup(t, true, false)
	f.lost = true
	r := f.call(t)
	if r.Error == "" {
		t.Fatal("lost response confirmed")
	}
	id, err := f.db.CreationPublicationRequest(t.Context(), f.receipts.scope, r.RequestID)
	mustProgress(t, err)
	f.mu.Lock()
	exact := f.items[0]
	f.mu.Unlock()
	wrongAuthor := exact
	wrongAuthor.Author.ID = 11
	changed := exact
	changed.Body += " attacker text"
	duplicate := exact
	duplicate.ID++
	duplicate.Number++
	for _, items := range [][]github.Issue{nil, {wrongAuthor}, {changed}, {exact, duplicate}} {
		f.mu.Lock()
		f.items = items
		f.mu.Unlock()
		op, err := f.service.Reconcile(t.Context(), id, "open", 1, func(context.Context, store.CreationProposal) error { return nil })
		mustProgress(t, err)
		if op.State != "uncertain" {
			t.Fatalf("untrusted/ambiguous observation confirmed %+v", op)
		}
	}
	f.mu.Lock()
	f.items = []github.Issue{exact}
	f.mu.Unlock()
	op, err := f.service.Reconcile(t.Context(), id, "open", 1, func(context.Context, store.CreationProposal) error { return nil })
	mustProgress(t, err)
	if op.State != "confirmed" {
		t.Fatalf("exact observation not confirmed %+v", op)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.posts != 1 {
		t.Fatalf("observation recreated %d", f.posts)
	}
}
