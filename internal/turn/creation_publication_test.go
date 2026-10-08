package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/store/creationfixture"
	"github.com/cloudvane/litefactory/internal/submission"
)

type creationHTTPFixture struct {
	mu           sync.Mutex
	posts, scans int
	items        []github.Issue
	db           *store.DB
	cs           *store.CreationStore
	service      *CreationPublicationService
	receipts     *ReceiptService
	guard        *AttemptGuard
	client       submission.Client
	prepared     PreparedCreation
	intent       int64
	now          time.Time
}

func creationHTTPSetup(t *testing.T, gate bool) *creationHTTPFixture {
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
		if r.Method == http.MethodPost {
			f.posts++
			t.Error("recovery attempted publication")
			w.WriteHeader(http.StatusInternalServerError)
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
	f.service, err = NewCreationPublicationService(db, f.receipts, auth, forge, redact.New(redact.Options{}))
	mustProgress(t, err)
	envelope := submission.CreationEnvelope{Artifacts: []submission.CreationArtifact{{Title: "Reviewed", Body: "Exact body", LinkIssues: []int{280}}}}
	f.prepared, err = f.service.prepare(t.Context(), envelope, [16]byte{1}, 0, auth.RetainProposal)
	mustProgress(t, err)
	// Trusted fixture approval must predate the next bound attempt; use the existing primitive directly.
	enabled, err := NewCreationAuthorizer(CreationAuthorizerOptions{Store: f.cs, Enabled: true, OwnerID: 10, BotID: 20})
	mustProgress(t, err)
	intent, err := enabled.AdmitOwnerGrantIntent(t.Context(), OwnerGrantControl{Repository: f.prepared.Envelope.Repository, SourceTask: app.Binding().Task, Mode: "research", Body: "/mcp-create " + f.prepared.Hash + " " + f.prepared.Envelope.Relationship, CommentID: 1, ActorID: 10, CreatedAt: f.now})
	mustProgress(t, err)
	feedback, err := NewFeedbackService(app, nil)
	mustProgress(t, err)
	f.client, f.guard, _ = familySocketFixture(t, db, FamilyDispatcherOptions{Service: app, Receipts: f.receipts, Feedback: feedback})
	f.intent = intent
	return f
}

// seedHistory models a lost legacy response: one remote call happened, but its
// issue identity was not recorded before the process stopped. No retired API runs.
func (f *creationHTTPFixture) seedHistory(t *testing.T) string {
	t.Helper()
	const request = "lost-creation"
	args, err := json.Marshal(f.prepared.Envelope)
	mustProgress(t, err)
	live, release, err := f.guard.Context(t.Context())
	mustProgress(t, err)
	defer release()
	_, _, err = f.receipts.Begin(live, request, "issues_create_batch", args)
	mustProgress(t, err)
	b := f.receipts.Binding()
	id := creationfixture.CreationHistory(t, store.DSN(f.db.Path()), f.intent, b.RunID, b.Attempt, request, 0)
	mustProgress(t, f.receipts.Finish(t.Context(), request, "uncertain", store.FamilyReference{Kind: "creation_publication", ID: id}))
	a := f.prepared.Envelope.Artifacts[0]
	issue := github.Issue{ID: 901, Number: 401, Title: a.Title, Body: a.Body}
	issue.Author.ID = 20
	f.mu.Lock()
	f.items = []github.Issue{issue}
	f.posts = 1 // Historical remote call; subsequent observation must add none.
	f.mu.Unlock()
	return request
}

func TestCreationHTTPLostReplyReopenObservation(t *testing.T) {
	f := creationHTTPSetup(t, true)
	request := f.seedHistory(t)
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
	svc, err := NewCreationPublicationService(reopened, receipts, auth, f.service.forge, redact.New(redact.Options{}))
	mustProgress(t, err)
	id, err := reopened.CreationPublicationRequest(t.Context(), receipts.scope, request)
	mustProgress(t, err)
	before, err := receipts.Get(t.Context(), request)
	mustProgress(t, err)
	if before.States.Effects != "uncertain" {
		t.Fatalf("before %+v", before)
	}
	if _, err = svc.Reconcile(t.Context(), id, "open", 1, nil); err == nil {
		t.Fatal("unauthorized observation")
	}
	for range 3 {
		reply, err := f.client.Call(t.Context(), "issues_create_batch", []byte(`{}`))
		mustProgress(t, err)
		if reply.Error == "" {
			t.Fatal("retired batch tool accepted")
		}
	}
	op, err := svc.Reconcile(t.Context(), id, "open", 1, func(context.Context, store.CreationProposal) error { return nil })
	mustProgress(t, err)
	if op.State != "confirmed" {
		t.Fatalf("op %+v", op)
	}
	got, err := receipts.Get(t.Context(), request)
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
func creationWireIssue(i github.Issue) map[string]any {
	return map[string]any{"id": i.ID, "number": i.Number, "title": i.Title, "body": i.Body, "state": "open", "user": map[string]any{"id": i.Author.ID}}
}

func TestCreationHTTPObservationRequiresExactUnambiguousProvenance(t *testing.T) {
	f := creationHTTPSetup(t, true)
	request := f.seedHistory(t)
	id, err := f.db.CreationPublicationRequest(t.Context(), f.receipts.scope, request)
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
