package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

func rejectionTask(t *testing.T, f *publicationIO) *task {
	t.Helper()
	original := f.server.Config.Handler
	f.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/comments") {
			f.mu.Lock()
			defer f.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			comments := []any{}
			for _, body := range f.bodies {
				comments = append(comments, map[string]any{"id": 991, "body": body, "html_url": "https://github.com/owner/api/issues/278#issuecomment-991", "user": map[string]any{"id": 20, "type": "Bot"}})
			}
			_ = json.NewEncoder(w).Encode(comments)
			return
		}
		original.ServeHTTP(w, r)
	})
	ref, err := naming.ParseRef(f.session.TaskRef)
	publicationMust(t, err)
	return &task{log: f.d.log, session: f.session, ref: ref, forgeRepo: github.Repo{Owner: "owner", Name: "api"}, issue: github.Issue{Number: 278, Labels: []string{"Mode: Research"}}, tickAt: f.now}
}
func rejectionSource(f *publicationIO, body string) github.Comment {
	return github.Comment{ID: 70, Body: body, CreatedAt: f.now, Author: github.User{ID: 10, Login: "owner"}}
}
func rejectionWatermark(t *testing.T, f *publicationIO, want int64) {
	t.Helper()
	w, found, err := f.db.GetWatermark(t.Context(), f.session.ID)
	publicationMust(t, err)
	if want == 0 {
		if found && w.LastCommentID != 0 {
			t.Fatalf("premature consumption: %+v", w)
		}
		return
	}
	if !found || w.LastCommentID != want {
		t.Fatalf("watermark %+v found=%v, want %d", w, found, want)
	}
}
func rejectionRestart(t *testing.T, f *publicationIO) {
	t.Helper()
	publicationMust(t, f.db.Close())
	reopened, err := store.Open(t.Context(), f.path)
	publicationMust(t, err)
	*f.db = *reopened
	publicationMust(t, f.db.Migrate(t.Context()))
	f.d.recoveries = reportRecoveryCache{}
	f.now = f.now.Add(24 * time.Hour)
}

func TestCreationRejectionIO_MalformedCommandsRetainAndConsume(t *testing.T) {
	for _, tc := range []struct{ body, kind string }{{"/mcp-create please approve private-payload", "approval"}, {"/mcp-create prepare {private-payload", "preparation"}} {
		t.Run(tc.kind, func(t *testing.T) {
			f := newPublicationIO(t)
			task := rejectionTask(t, f)
			publicationMust(t, f.d.handleComment(t.Context(), task, rejectionSource(f, tc.body)))
			row, err := f.db.GetCreationRejection(t.Context(), f.session.ID, 70)
			publicationMust(t, err)
			if row.CommandKind != tc.kind || row.Reason == "" || strings.Contains(row.Delivery.Body, "private-payload") {
				t.Fatalf("unsafe outcome %+v", row)
			}
			delivery, err := f.db.GetReportDelivery(t.Context(), row.Delivery.Key)
			publicationMust(t, err)
			if !delivery.Completed || delivery.CommentID == 0 {
				t.Fatalf("unconfirmed rejection %+v", delivery)
			}
			rejectionWatermark(t, f, 70)
			if _, _, err = f.cs.EligibleCreationIntent(t.Context(), f.session.TaskRef); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("rejection admitted intent: %v", err)
			}
			if f.posts != 1 || f.creationPosts != 0 {
				t.Fatalf("posts=%d creations=%d", f.posts, f.creationPosts)
			}
			// A replay or edited formerly rejected source must only recover its original reply.
			c := rejectionSource(f, "/mcp-create prepare {}")
			task.issue.Labels = []string{"Mode: Implement"}
			publicationMust(t, f.d.handleComment(t.Context(), task, c))
			if f.posts != 1 {
				t.Fatal("replayed rejection posted twice")
			}
		})
	}
}

func TestCreationRejectionIO_LostReplyRestartEditedSource(t *testing.T) {
	f := newPublicationIO(t)
	task := rejectionTask(t, f)
	f.lost = true
	publicationMust(t, f.d.handleComment(t.Context(), task, rejectionSource(f, "/mcp-create invalid")))
	rejectionWatermark(t, f, 0)
	before, err := f.db.GetCreationRejection(t.Context(), f.session.ID, 70)
	publicationMust(t, err)
	delivery, err := f.db.GetReportDelivery(t.Context(), before.Delivery.Key)
	publicationMust(t, err)
	if !delivery.PossiblySent || delivery.Completed || delivery.CommentID != 0 {
		t.Fatalf("lost response not pending %+v", delivery)
	}
	rejectionRestart(t, f)
	task.tickAt = f.now
	task.issue.Labels = []string{"Mode: Implement"}
	f.runtime.CreationAuthorizer, err = turn.NewCreationAuthorizer(turn.CreationAuthorizerOptions{Store: f.cs, Enabled: false, OwnerID: 10, BotID: 20})
	publicationMust(t, err)
	publicationMust(t, f.d.handleComment(t.Context(), task, rejectionSource(f, "edited into ordinary work")))
	rejectionWatermark(t, f, 70)
	after, err := f.db.GetCreationRejection(t.Context(), f.session.ID, 70)
	publicationMust(t, err)
	if after.Delivery.Body != before.Delivery.Body || after.Delivery.Key != before.Delivery.Key || after.Reason != before.Reason || f.posts != 1 {
		t.Fatalf("unstable recovery: before=%+v after=%+v posts=%d", before, after, f.posts)
	}
}

func TestCreationRejectionIO_OutcomeReportGapAndLegacyIsolation(t *testing.T) {
	f := newPublicationIO(t)
	task := rejectionTask(t, f)
	marker := "<!-- lf:delivery v=1 key=retained-rejection-gap -->"
	row := store.CreationRejection{Repository: "owner/api", SourceTask: f.session.TaskRef, SessionID: f.session.ID, CommentID: 70, CommandKind: "approval", Reason: "syntax", Delivery: store.ReportDelivery{Key: "retained-rejection-gap", Version: 1, SessionID: f.session.ID, TriggerID: 70, Task: f.session.TaskRef, Destination: f.session.TaskRef, TriggerNamespace: "comment", Purpose: "creation-rejection", BotUserID: 20, Body: "Creation approval syntax was rejected. Submit a fresh comment.\n\n" + marker, Marker: marker}}
	retained, err := f.db.RetainCreationRejection(t.Context(), row)
	publicationMust(t, err)
	if _, err = f.db.GetReportDelivery(t.Context(), row.Delivery.Key); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("gap absent: %v", err)
	}
	row.Reason = "mode"
	row.Delivery.Body = "changed"
	again, err := f.db.RetainCreationRejection(t.Context(), row)
	publicationMust(t, err)
	if again.Reason != retained.Reason || again.Delivery.Body != retained.Delivery.Body {
		t.Fatal("retained outcome mutated")
	}
	// An unrelated pre-journal task-only bot reply must not suppress this purpose.
	f.bodies = []string{"Old unrelated reply\n\n<!-- lf:reply task=github:owner/api#278 -->"}
	rejectionRestart(t, f)
	task.tickAt = f.now
	task.issue.Labels = nil
	publicationMust(t, f.d.handleComment(t.Context(), task, rejectionSource(f, "")))
	rejectionWatermark(t, f, 70)
	if f.posts != 1 || f.bodies[len(f.bodies)-1] != retained.Delivery.Body {
		t.Fatal("gap recovery did not dispatch exact retained bytes")
	}
}

func TestCreationRejectionIO_CounterfeitConfirmationStaysPending(t *testing.T) {
	for _, variant := range []string{"wrong bot", "conflicting body", "missing receipt"} {
		t.Run(variant, func(t *testing.T) {
			f := newPublicationIO(t)
			task := rejectionTask(t, f)
			f.lost = true
			original := f.server.Config.Handler
			f.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/comments") {
					w.Header().Set("Content-Type", "application/json")
					f.mu.Lock()
					body := ""
					if len(f.bodies) > 0 {
						body = f.bodies[0]
					}
					f.mu.Unlock()
					id := int64(20)
					if variant == "wrong bot" {
						id = 11
					}
					if variant == "conflicting body" {
						body = "conflicting\n" + body
					}
					comments := []any{}
					if variant != "missing receipt" {
						comments = append(comments, map[string]any{"id": 991, "body": body, "html_url": "https://github.com/owner/api/issues/278#issuecomment-991", "user": map[string]any{"id": id, "type": "Bot"}})
					}
					_ = json.NewEncoder(w).Encode(comments)
					return
				}
				original.ServeHTTP(w, r)
			})
			publicationMust(t, f.d.handleComment(t.Context(), task, rejectionSource(f, "/mcp-create invalid")))
			rejectionRestart(t, f)
			task.tickAt = f.now
			publicationMust(t, f.d.handleComment(t.Context(), task, rejectionSource(f, "/mcp-create corrected")))
			rejectionWatermark(t, f, 0)
			if f.posts != 1 {
				t.Fatal("uncertain absence redispatched")
			}
		})
	}
}

func TestCreationRejectionIO_FilteredIdentityDoesNotReport(t *testing.T) {
	for _, actor := range []github.User{{ID: 11, Login: "owner"}, {ID: 20, Login: "owner"}, {ID: 10, Login: "owner", IsBot: true}} {
		t.Run(fmt.Sprintf("actor-%d-bot-%t", actor.ID, actor.IsBot), func(t *testing.T) {
			f := newPublicationIO(t)
			c := rejectionSource(f, "/mcp-create malformed")
			c.Author = actor
			publicationMust(t, f.d.handleComment(t.Context(), rejectionTask(t, f), c))
			rejectionWatermark(t, f, 70)
			if _, err := f.db.GetCreationRejection(t.Context(), f.session.ID, 70); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("filtered source retained rejection: %v", err)
			}
			if f.posts != 0 || f.creationPosts != 0 {
				t.Fatal("filtered input produced effect")
			}
		})
	}
}
