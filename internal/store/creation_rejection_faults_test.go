package store_test

import (
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

func TestCreationRejectionFaultIO_RetainDispatchReceiptConsumption(t *testing.T) {
	for _, tc := range []struct {
		name, trigger string
		posts         int
		retained      bool
	}{
		{"retention", `CREATE TRIGGER rejection_fault BEFORE INSERT ON creation_rejections BEGIN SELECT RAISE(ABORT,'retention unavailable'); END`, 0, false},
		{"outcome-report-gap", `CREATE TRIGGER rejection_fault BEFORE INSERT ON report_deliveries BEGIN SELECT RAISE(ABORT,'delivery unavailable'); END`, 0, true},
		{"before-dispatch", `CREATE TRIGGER rejection_fault BEFORE UPDATE OF possibly_sent ON report_deliveries BEGIN SELECT RAISE(ABORT,'dispatch unavailable'); END`, 0, true},
		{"receipt", `CREATE TRIGGER rejection_fault BEFORE UPDATE OF comment_id ON report_deliveries BEGIN SELECT RAISE(ABORT,'receipt unavailable'); END`, 1, true},
		{"consumption", `CREATE TRIGGER rejection_fault BEFORE INSERT ON watermarks BEGIN SELECT RAISE(ABORT,'consumption unavailable'); END`, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(t.TempDir(), "state.db")
			db, err := store.Open(t.Context(), path)
			must(err)
			t.Cleanup(func() { must(db.Close()) })
			must(db.Migrate(t.Context()))
			repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "https://github.com/acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
			must(err)
			session, err := db.GetOrCreateSession(t.Context(), repo, 123)
			must(err)
			faults, err := sql.Open("sqlite", path)
			must(err)
			faults.SetMaxOpenConns(1)
			t.Cleanup(func() { must(faults.Close()) })
			_, err = faults.ExecContext(t.Context(), tc.trigger)
			must(err)
			remote := &deliveryHTTP{t: t}
			server := httptest.NewServer(http.HandlerFunc(remote.serve))
			t.Cleanup(server.Close)
			client, err := github.New(github.Options{APIBase: server.URL, Token: secrets.NewToken("fixture-token"), HTTPClient: server.Client()})
			must(err)
			renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{}}), Limit: 65536})
			must(err)
			reporter, err := turn.NewReporter(turn.ReporterOptions{DB: db, Forge: client, Renderer: renderer, BotUserID: 9, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
			must(err)
			ref, err := naming.ParseRef(session.TaskRef)
			must(err)
			if _, err = reporter.ReportCreationRejection(t.Context(), ref, session.ID, 55, "approval", "syntax"); err == nil {
				t.Fatal("injected storage failure succeeded")
			}
			row, err := db.GetCreationRejection(t.Context(), session.ID, 55)
			if tc.retained {
				must(err)
			} else if !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("failed retention created outcome: %v", err)
			}
			wm, _, err := db.GetWatermark(t.Context(), session.ID)
			must(err)
			if wm.LastCommentID != 0 {
				t.Fatal("failure advanced source")
			}
			remote.mu.Lock()
			posts := remote.posts
			remote.mu.Unlock()
			if posts != tc.posts {
				t.Fatalf("posts=%d want=%d", posts, tc.posts)
			}
			_, err = faults.ExecContext(t.Context(), `DROP TRIGGER rejection_fault`)
			must(err)
			must(db.Close())
			reopened, err := store.Open(t.Context(), path)
			must(err)
			*db = *reopened
			must(db.Migrate(t.Context()))
			if tc.retained {
				// Recovery knows only trusted source identity, not the edited source/config.
				_, found, e := reporter.ReportPending(t.Context(), session.ID, 55)
				must(e)
				if !found {
					t.Fatal("retained outcome lost")
				}
				after, e := db.GetCreationRejection(t.Context(), session.ID, 55)
				must(e)
				if after.Delivery.Body != row.Delivery.Body || after.Delivery.Key != row.Delivery.Key {
					t.Fatal("recovery changed retained result")
				}
			} else {
				_, err = reporter.ReportCreationRejection(t.Context(), ref, session.ID, 55, "approval", "syntax")
				must(err)
			}
			wm, _, err = db.GetWatermark(t.Context(), session.ID)
			must(err)
			if wm.LastCommentID != 55 {
				t.Fatalf("recovery did not consume: %+v", wm)
			}
			remote.mu.Lock()
			defer remote.mu.Unlock()
			if remote.posts != 1 {
				t.Fatalf("recovery posts=%d", remote.posts)
			}
		})
	}
}
