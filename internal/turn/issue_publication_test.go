package turn

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

type commentHTTPFixture struct {
	mu                  sync.Mutex
	posts, observations int
	body, observation   string
	lost, pr            bool
	onPost              func()
	db                  *store.DB
	receipts            *ReceiptService
	service             *IssuePublicationService
	client              submission.Client
}

func commentHTTPSetup(t *testing.T) *commentHTTPFixture {
	t.Helper()
	f := &commentHTTPFixture{}
	db, _, opts := agentResultFixture(t, "research")
	f.db = db
	app, err := BindAgentResultService(t.Context(), opts)
	mustProgress(t, err)
	projection := CommentReceiptProjection(db)
	f.receipts, err = NewReceiptService(db, app.Binding(), nil, map[string]FamilyProjection{"comment": projection, "tool:issues_comment": projection})
	mustProgress(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		repo := f.receipts.scope.Repository
		if r.URL.Path != "/repos/"+repo+"/issues/284" && r.URL.Path != "/repos/"+repo+"/issues/284/comments" {
			t.Errorf("foreign target: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodPost {
			f.posts++
			var payload struct {
				Body string `json:"body"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
				return
			}
			f.body = payload.Body

			if f.onPost != nil {
				f.onPost()
			}
			if f.lost {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"id":`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 555, "body": payload.Body, "user": map[string]any{"id": 9, "type": "Bot"}})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/comments") {
			f.observations++
			if r.URL.Query().Get("page") != "1" || r.URL.Query().Get("per_page") != "100" {
				t.Error("unbounded observation")
			}
			if f.observation == "" {
				_, _ = w.Write([]byte(`[]`))
			} else {
				_, _ = w.Write([]byte(f.observation))
			}
			return
		}
		if f.pr {
			_, _ = w.Write([]byte(`{"number":284,"pull_request":{"url":"https://example.invalid/pr"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"number":284,"state":"open"}`))
	}))
	t.Cleanup(server.Close)
	forge, err := github.New(github.Options{APIBase: server.URL, HTTPClient: server.Client(), Token: secrets.NewToken("fixture-token")})
	mustProgress(t, err)
	f.service, err = NewIssuePublicationService(db, f.receipts, forge, redact.New(redact.Options{}), []int{284}, 9)
	mustProgress(t, err)
	feedback, err := NewFeedbackService(app, nil)
	mustProgress(t, err)
	f.client, _, _ = familySocketFixture(t, db, FamilyDispatcherOptions{Service: app, Receipts: f.receipts, Feedback: feedback, CommentTargets: []int{284}, Registrations: []FamilyRegistration{f.service.Registration()}})
	return f
}

func TestCommentHTTPConcurrentRetries(t *testing.T) {
	f := commentHTTPSetup(t)
	args := []byte(`{"issue_number":284,"message":"status ghp_0123456789abcdefghijklmnopqrstuvwxyz"}`)
	var wg sync.WaitGroup
	ids := make(chan string, 12)
	for range 12 {
		wg.Go(func() {
			r, e := f.client.Call(t.Context(), "issues_comment", args)
			if e != nil {
				t.Error(e)
				return
			}
			if r.Error != "" {
				t.Error(r.Error)
			}
			ids <- r.RequestID
		})
	}
	wg.Wait()
	close(ids)
	var id string
	for got := range ids {
		if id != "" && id != got {
			t.Fatal("retry identity changed")
		}
		id = got
	}
	f.mu.Lock()
	posts, body := f.posts, f.body
	f.mu.Unlock()
	if posts != 1 || strings.Contains(body, "ghp_") || !strings.Contains(body, "<!-- lf:mcp-comment v=1 key=") {
		t.Fatalf("posts=%d prepared=%q", posts, body)
	}
	receipt, err := f.receipts.Get(t.Context(), id)
	mustProgress(t, err)
	if receipt.States.Effects != "confirmed" {
		t.Fatalf("receipt: %+v", receipt)
	}
	op, err := f.db.GetCommentOperation(t.Context(), f.receipts.scope, id)
	mustProgress(t, err)
	if op.CommentID != 555 || op.Body != body {
		t.Fatalf("intent/outcome %+v", op)
	}
	for range 3 {
		r, e := f.client.Call(t.Context(), "receipts_get", mcpReceiptArgs(t, id))
		mustProgress(t, e)
		if r.Error != "" {
			t.Fatal(r.Error)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.posts != 1 || f.observations != 0 {
		t.Fatal("receipt redispatched")
	}
}

func TestCommentHTTPRefusesInvalidTargetsAndPreparedContent(t *testing.T) {
	for _, tc := range []struct {
		name, message string
		number        int
		pr            bool
	}{
		{"foreign target", "message", 285, false}, {"zero", "message", 0, false}, {"blank", "  ", 284, false},
		{"oversized", strings.Repeat("x", github.CommentSizeLimit+1), 284, false},
		{"marker exceeds final bound", strings.Repeat("x", github.CommentSizeLimit), 284, false},
		{"pull request", "message", 284, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := commentHTTPSetup(t)
			f.pr = tc.pr
			args, err := json.Marshal(IssueCommentRequest{IssueNumber: tc.number, Message: tc.message})
			mustProgress(t, err)
			r, err := f.client.Call(t.Context(), "issues_comment", args)
			mustProgress(t, err)
			if r.Error == "" {
				t.Fatal("invalid comment accepted")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.posts != 0 {
				t.Fatal("refusal published")
			}
		})
	}
}

func TestCommentHTTPLostResponseReopenAndExactObservation(t *testing.T) {
	f := commentHTTPSetup(t)
	f.lost = true
	args := []byte(`{"issue_number":284,"message":"reviewed status"}`)
	r, err := f.client.Call(t.Context(), "issues_comment", args)
	mustProgress(t, err)
	if r.Error == "" {
		t.Fatal("lost response reported confirmed")
	}
	reopened, err := store.Open(t.Context(), f.db.Path())
	mustProgress(t, err)
	defer reopened.Close()
	receipts, err := NewReceiptService(reopened, f.receipts.Binding(), nil, map[string]FamilyProjection{"comment": CommentReceiptProjection(reopened), "tool:issues_comment": CommentReceiptProjection(reopened)})
	mustProgress(t, err)
	svc, err := NewIssuePublicationService(reopened, receipts, f.service.forge, redact.New(redact.Options{}), []int{284}, 9)
	mustProgress(t, err)
	before, err := receipts.Get(t.Context(), r.RequestID)
	mustProgress(t, err)
	if before.States.Effects != "uncertain" {
		t.Fatalf("lost reply: %+v", before)
	}
	_, err = svc.HandleComment(t.Context(), r.RequestID, args)
	mustProgress(t, err)
	denied := func(context.Context, store.CommentOperation) error { return store.ErrResultAuthority }
	if _, e := svc.ReconcileComment(t.Context(), receipts.scope, r.RequestID, 1, denied); !errors.Is(e, store.ErrResultAuthority) {
		t.Fatalf("denied observer: %v", e)
	}
	f.mu.Lock()
	observations := f.observations
	f.mu.Unlock()
	if observations != 0 {
		t.Fatal("denied observer contacted forge")
	}
	foreign := receipts.scope
	foreign.Repository = "foreign/repo"
	if _, e := svc.ReconcileComment(t.Context(), foreign, r.RequestID, 1, denied); e == nil {
		t.Fatal("foreign scope observed")
	}
	authorize := func(context.Context, store.CommentOperation) error { return nil }
	f.mu.Lock()
	body := f.body
	f.mu.Unlock()
	comment := func(id, bot int, body string) map[string]any {
		return map[string]any{"id": id, "body": body, "user": map[string]any{"id": bot}}
	}
	for _, tc := range []struct {
		name      string
		items     []map[string]any
		confirmed bool
	}{
		{"empty", nil, false}, {"wrong author", []map[string]any{comment(555, 8, body)}, false},
		{"changed content", []map[string]any{comment(555, 9, "edited "+body)}, false},
		{"missing marker", []map[string]any{comment(555, 9, "reviewed status")}, false},
		{"ambiguous", []map[string]any{comment(555, 9, body), comment(556, 9, body)}, false},
		{"exact", []map[string]any{comment(555, 9, body)}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, e := json.Marshal(tc.items)
			mustProgress(t, e)
			f.mu.Lock()
			f.observation = string(data)
			f.mu.Unlock()
			op, e := svc.ReconcileComment(t.Context(), receipts.scope, r.RequestID, 1, authorize)
			mustProgress(t, e)
			if (op.State == "confirmed") != tc.confirmed {
				t.Fatalf("observation: %+v", op)
			}
		})
	}
	if _, err = svc.ReconcileComment(t.Context(), receipts.scope, r.RequestID, 1, nil); !errors.Is(err, store.ErrResultAuthority) {
		t.Fatalf("missing observer grant: %v", err)
	}
	got, err := receipts.Get(t.Context(), r.RequestID)
	mustProgress(t, err)
	if got.States.Effects != "confirmed" {
		t.Fatalf("recovered receipt: %+v", got)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.posts != 1 {
		t.Fatalf("recreated lost comment: %d", f.posts)
	}
}

func TestCommentHTTPCancellationRetainsIntentBeforeDispatch(t *testing.T) {
	f := commentHTTPSetup(t)
	const id = "cancelled-dispatch"
	args := []byte(`{"issue_number":284,"message":"cancellable status"}`)
	_, _, err := f.receipts.Begin(t.Context(), id, "issues_comment", args)
	mustProgress(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f.onPost = func() {
		db, e := store.Open(t.Context(), f.db.Path())
		if e != nil {
			t.Error(e)
			cancel()
			return
		}
		defer db.Close()
		op, e := db.GetCommentOperation(t.Context(), f.receipts.scope, id)
		if e != nil || op.State != "uncertain" || op.Body == "" {
			t.Errorf("intent absent before HTTP response: %+v %v", op, e)
		}
		cancel()
	}
	_, err = f.service.HandleComment(ctx, id, args)
	if err == nil {
		t.Fatal("cancelled publication reported success")
	}
	// Wait for the HTTP handler to leave its critical section before observing.
	f.mu.Lock()
	f.onPost = nil
	f.mu.Unlock()
	receipt, err := f.receipts.Get(t.Context(), id)
	mustProgress(t, err)
	if receipt.States.Effects != "uncertain" {
		t.Fatalf("cancelled receipt: %+v", receipt)
	}
	_, err = f.service.HandleComment(t.Context(), id, args)
	mustProgress(t, err)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.posts != 1 {
		t.Fatalf("cancelled dispatch reposted: %d", f.posts)
	}
}
