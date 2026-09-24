package store_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

type deliveryComment struct {
	ID      int64        `json:"id"`
	Body    string       `json:"body"`
	HTMLURL string       `json:"html_url"`
	User    deliveryUser `json:"user"`
}
type deliveryUser struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}

type deliveryHTTP struct {
	t                     *testing.T
	mu                    sync.Mutex
	posts, gets           int
	comments              []deliveryComment
	postStatus, getStatus int
}

func (f *deliveryHTTP) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodGet {
		f.gets++
		if f.getStatus != 0 {
			w.WriteHeader(f.getStatus)
			return
		}
		if f.comments == nil {
			f.comments = []deliveryComment{}
		}
		if err := json.NewEncoder(w).Encode(f.comments); err != nil {
			f.t.Error(err)
		}
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	f.posts++
	var payload struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	comment := deliveryComment{ID: 900, Body: payload.Body, HTMLURL: "https://github.com/acme/api/issues/123#issuecomment-900", User: deliveryUser{ID: 9, Login: "bot"}}
	f.comments = append(f.comments, comment)
	if f.postStatus != 0 {
		w.WriteHeader(f.postStatus)
		return
	}
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(comment); err != nil {
		f.t.Error(err)
	}
}

func TestReportDeliveryHTTPAndSQLiteFailureWindows(t *testing.T) {
	reportDeliveryFailureWindows(t, false)
}

func TestCreationAcceptanceHTTPAndSQLiteFailureWindows(t *testing.T) {
	reportDeliveryFailureWindows(t, true)
}

func reportDeliveryFailureWindows(t *testing.T, acceptance bool) {
	for _, tc := range []struct {
		name, trigger                     string
		posts                             int
		uncertain, deleted, lookupFailure bool
	}{
		{name: "intent", trigger: `CREATE TRIGGER delivery_fault BEFORE INSERT ON report_deliveries BEGIN SELECT RAISE(ABORT,'intent failure'); END`, posts: 0},
		{name: "dispatch-boundary", trigger: `CREATE TRIGGER delivery_fault BEFORE UPDATE OF possibly_sent ON report_deliveries WHEN OLD.possibly_sent=0 AND NEW.possibly_sent=1 BEGIN SELECT RAISE(ABORT,'boundary failure'); END`, posts: 0},
		{name: "receipt", trigger: `CREATE TRIGGER delivery_fault BEFORE UPDATE OF comment_id ON report_deliveries WHEN NEW.comment_id IS NOT NULL BEGIN SELECT RAISE(ABORT,'receipt failure'); END`, posts: 1},
		{name: "completion-deleted-confirmed", trigger: `CREATE TRIGGER delivery_fault BEFORE UPDATE OF completed ON report_deliveries WHEN NEW.completed=1 BEGIN SELECT RAISE(ABORT,'completion failure'); END`, posts: 1, deleted: true},
		{name: "lost-response-empty-lookup", posts: 1, uncertain: true},
		{name: "lost-response-failed-lookup", posts: 1, lookupFailure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			path := filepath.Join(t.TempDir(), "reports.db")
			db, err := store.Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := db.Close(); err != nil {
					t.Error(err)
				}
			})
			if err := db.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "https://github.com/acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			session, err := db.GetOrCreateSession(ctx, repo, 123)
			if err != nil {
				t.Fatal(err)
			}
			var run store.Run
			if !acceptance {
				segment, err := db.GetOrCreateSegment(ctx, session.ID, store.FirstSegmentOrdinal, "report-session", store.AgentClaude)
				if err != nil {
					t.Fatal(err)
				}
				run, err = db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Agent: store.AgentClaude, Mode: "spike", TriggerCommentID: 55, LogPath: func(int) string { return "/logs/1.jsonl" }})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := db.FinishRun(ctx, run.ID, store.RunOutcome{Status: store.RunFailed, Error: "observed failure"}); err != nil {
					t.Fatal(err)
				}
			}

			faults, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			faults.SetMaxOpenConns(1)
			t.Cleanup(func() {
				if err := faults.Close(); err != nil {
					t.Error(err)
				}
			})
			if tc.trigger != "" {
				if _, err := faults.ExecContext(ctx, tc.trigger); err != nil {
					t.Fatal(err)
				}
			}
			remote := &deliveryHTTP{t: t}
			if tc.uncertain || tc.lookupFailure {
				remote.postStatus = http.StatusBadGateway
			}
			server := httptest.NewServer(http.HandlerFunc(remote.serve))
			t.Cleanup(server.Close)
			client, err := github.New(github.Options{APIBase: server.URL, Token: secrets.NewToken("fixture-token"), HTTPClient: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{}}), Limit: 65536})
			if err != nil {
				t.Fatal(err)
			}
			newReporter := func() *turn.Reporter {
				p, err := turn.NewReporter(turn.ReporterOptions{DB: db, Forge: client, Renderer: renderer, BotUserID: 9, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
				if err != nil {
					t.Fatal(err)
				}
				return p
			}
			ref, err := naming.ParseRef(session.TaskRef)
			if err != nil {
				t.Fatal(err)
			}
			req := turn.RunReport{SessionID: session.ID, CommentID: 55, RunID: run.ID, Run: report.Run{Ref: ref, Ordinal: run.Ordinal}}

			deliver := func() (turn.Posted, error) { return newReporter().ReportRun(ctx, req) }
			if acceptance {
				created := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
				reply := turn.ReplyRequest{Ref: ref, SessionID: session.ID, CommentID: 55, CreationAcceptance: &report.CreationAcceptance{Hash: strings.Repeat("a", 64), Relationship: "linked", CreatedAt: created, ExpiresAt: created.Add(30 * time.Minute)}}
				deliver = func() (turn.Posted, error) { return newReporter().ReportReply(ctx, reply) }
			}
			_, deliveryErr := deliver()
			if deliveryErr == nil {
				t.Fatal("failure window returned success")
			}
			if tc.name == "receipt" {
				var diagnostic *turn.DeliveryError
				if !errors.As(deliveryErr, &diagnostic) || diagnostic.Key == "" || diagnostic.State != "uncertain" {
					t.Fatalf("receipt failure lost recovery identity/state: %v", deliveryErr)
				}
				retained, found, err := db.FindPendingReportDelivery(ctx, session.ID, 55)
				if err != nil || !found || diagnostic.Key != retained.Key {
					t.Fatalf("diagnostic key does not identify pending delivery: diagnostic=%+v retained=%+v found=%v err=%v", diagnostic, retained, found, err)
				}
				_, pending, retryErr := newReporter().ReportPending(ctx, session.ID, 55)
				var retryDiagnostic *turn.DeliveryError
				if !pending || !errors.As(retryErr, &retryDiagnostic) || retryDiagnostic.Key != retained.Key || retryDiagnostic.State != "uncertain" {
					t.Fatalf("lookup receipt failure lost recovery identity/state: pending=%v error=%v", pending, retryErr)
				}
			}
			remote.mu.Lock()
			posts := remote.posts
			savedComments := append([]deliveryComment(nil), remote.comments...)
			remote.mu.Unlock()
			if posts != tc.posts {
				t.Fatalf("POSTs=%d want%d", posts, tc.posts)
			}
			wm, _, err := db.GetWatermark(ctx, session.ID)
			if err != nil || wm.LastCommentID != 0 {
				t.Fatalf("unconfirmed completion consumed=%+v %v", wm, err)
			}
			if tc.trigger != "" {
				if _, err := faults.ExecContext(ctx, `DROP TRIGGER delivery_fault`); err != nil {
					t.Fatal(err)
				}
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := store.Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			*db = *reopened
			remote.mu.Lock()
			if tc.deleted || tc.uncertain {
				remote.comments = nil
			}
			if tc.deleted || tc.lookupFailure {
				remote.getStatus = http.StatusBadGateway
			}
			getsBefore := remote.gets
			remote.mu.Unlock()
			if tc.uncertain || tc.lookupFailure {
				for range 2 {
					if _, found, err := newReporter().ReportPending(ctx, session.ID, 55); err == nil || !found {
						t.Fatalf("uncertain response=%v %v", found, err)
					}
				}
				wm, _, err := db.GetWatermark(ctx, session.ID)
				if err != nil || wm.LastCommentID != 0 {
					t.Fatal("uncertainty consumed source")
				}
				remote.mu.Lock()
				if remote.posts != 1 {
					t.Error("uncertain delivery reposted")
				}
				remote.comments = savedComments
				remote.getStatus = 0
				remote.mu.Unlock()
			}
			var posted turn.Posted
			if tc.posts == 0 {
				posted, err = deliver()
			} else {
				var found bool
				posted, found, err = newReporter().ReportPending(ctx, session.ID, 55)
				if !found {
					t.Fatal("restart lost pending source")
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			remote.mu.Lock()
			posts, gets := remote.posts, remote.gets
			remote.mu.Unlock()
			if posts != 1 || posted.CommentID != 900 {
				t.Fatalf("recovery effects posts=%d posted=%+v", posts, posted)
			}
			if tc.deleted && gets != getsBefore {
				t.Fatal("confirmed receipt performed remote lookup")
			}
			wm, _, err = db.GetWatermark(ctx, session.ID)
			if err != nil || wm.LastCommentID != 55 {
				t.Fatalf("completion source=%+v %v", wm, err)
			}
			if acceptance {
				runs, e := db.ListRuns(ctx, session.ID)
				if e != nil || len(runs) != 0 {
					t.Fatalf("acknowledgment allocated run: %+v %v", runs, e)
				}
				remote.mu.Lock()
				body := remote.comments
				remote.mu.Unlock()
				if !tc.deleted && (len(body) != 1 || !strings.Contains(body[0].Body, "2026-09-20T10:30:00Z")) {
					t.Fatalf("missing historical deadline: %+v", body)
				}
			} else {
				finished, err := db.GetRun(ctx, run.ID)
				if err != nil || finished.Status != store.RunReported || finished.ResultCommentID != 900 {
					t.Fatalf("completion run=%+v %v", finished, err)
				}
			}
			pending, err := db.ListPendingReportDeliveries(ctx)
			if err != nil || len(pending) != 0 {
				t.Fatalf("pending=%+v %v", pending, err)
			}
		})
	}
}
