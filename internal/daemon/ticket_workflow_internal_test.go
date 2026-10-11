package daemon

import (
	"context"
	"encoding/json"
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

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

type explicitWorkflowClassifier struct{ t *testing.T }

func (c explicitWorkflowClassifier) Classify(context.Context, agent.Classification) (agent.Mode, error) {
	c.t.Error("explicit owner command unexpectedly invoked model classification")
	return "", fmt.Errorf("explicit command must select its mode without classification")
}

// TestNeutralTicketWorkflow exercises the adapter, factory admission, Plan
// lifecycle and durable report ledger against real HTTP and SQLite boundaries.
func TestNeutralTicketWorkflow(t *testing.T) {
	for _, lost := range []bool{false, true} {
		name := "confirmed"
		if lost {
			name = "lost_receipt_restart"
		}
		t.Run(name, func(t *testing.T) {
			var mu sync.Mutex
			var base string
			childBody, childTitle, childState := "", "", "open"
			attached := false
			creates, attachments, posts := 0, 0, 0
			var published []map[string]any
			var parentLabels []map[string]any
			issue := func(number int) map[string]any {
				id, title, body, state := 700, "parent", "parent content", "open"
				authorID := 42
				if number == 8 {
					id, title, body, state = 800, childTitle, childBody, childState
					authorID = 99
				}
				out := map[string]any{"id": id, "number": number, "url": fmt.Sprintf("%s/repos/owner/repo/issues/%d", base, number), "html_url": fmt.Sprintf("https://github.com/owner/repo/issues/%d", number), "title": title, "body": body, "state": state, "user": map[string]any{"id": authorID, "login": "owner"}, "assignees": []map[string]any{{"id": 42, "login": "owner"}}}
				if number == 7 {
					out["labels"] = parentLabels
				}
				return out
			}
			ownerReply := func() map[string]any {
				return map[string]any{"id": 10, "url": base + "/repos/owner/repo/issues/comments/10", "issue_url": base + "/repos/owner/repo/issues/7", "body": "/implement continue the approved story", "user": map[string]any{"id": 42, "login": "renamed-owner"}}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				write := func(v any) {
					if err := json.NewEncoder(w).Encode(v); err != nil {
						t.Error(err)
					}
				}
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/issues":
					if creates == 0 {
						write([]any{issue(7)})
					} else {
						write([]any{issue(7), issue(8)})
					}
				case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/issues/7":
					write(issue(7))
				case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/issues/8":
					write(issue(8))
				case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/issues/7/sub_issues":
					if attached {
						write([]any{issue(8)})
					} else {
						write([]any{})
					}
				case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/issues/8/parent":
					if attached {
						write(issue(7))
					} else {
						w.WriteHeader(http.StatusNotFound)
						write(map[string]any{"message": "Not Found"})
					}
				case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/issues":
					var in struct{ Title, Body string }
					if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
						t.Error(err)
					}
					creates++
					childTitle, childBody = in.Title, in.Body
					w.WriteHeader(http.StatusCreated)
					write(issue(8))
				case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/issues/7/sub_issues":
					var in struct {
						ID int64 `json:"sub_issue_id"`
					}
					if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
						t.Error(err)
					}
					if in.ID != 800 {
						t.Errorf("attachment used local number instead of immutable child ID: %d", in.ID)
					}
					attachments++
					attached = true
					w.WriteHeader(http.StatusCreated)
					write(issue(8))
				case r.Method == http.MethodPatch && r.URL.Path == "/repos/owner/repo/issues/8":
					var in struct {
						State       string
						StateReason string `json:"state_reason"`
					}
					if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
						t.Error(err)
					}
					if in.State != "closed" || in.StateReason != "completed" {
						t.Errorf("story lifecycle changed: %+v", in)
					}
					childState = in.State
					write(issue(8))
				case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/issues/7/comments":
					replies := []any{ownerReply()}
					for _, p := range published {
						replies = append(replies, p)
					}
					write(replies)
				case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/issues/7/labels":
					var labels []string
					if err := json.NewDecoder(r.Body).Decode(&labels); err != nil {
						t.Error(err)
					}
					if len(labels) != 1 || labels[0] != agent.ModeImplement.Label() {
						t.Errorf("wrong mode mutation: %v", labels)
					}
					for _, label := range labels {
						parentLabels = append(parentLabels, map[string]any{"name": label})
					}
					write(parentLabels)
				case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/issues/7/comments":
					var in struct{ Body string }
					if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
						t.Error(err)
					}
					posts++
					p := map[string]any{"id": 900, "url": base + "/repos/owner/repo/issues/comments/900", "issue_url": base + "/repos/owner/repo/issues/7", "body": in.Body, "user": map[string]any{"id": 99, "login": "factory", "type": "Bot"}}
					published = append(published, p)
					w.WriteHeader(http.StatusCreated)
					if lost {
						write(map[string]any{"id": 0})
					} else {
						write(p)
					}
				default:
					t.Errorf("unexpected workflow request %s %s", r.Method, r.URL.String())
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			defer server.Close()
			base = server.URL
			client, err := github.New(github.Options{APIBase: base, Token: secrets.NewToken("offline-token")})
			if err != nil {
				t.Fatal(err)
			}
			tickets, err := github.NewTickets(client)
			if err != nil {
				t.Fatal(err)
			}
			dbPath := filepath.Join(t.TempDir(), "history.db")
			db, err := store.Open(t.Context(), dbPath)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if db != nil {
					if err := db.Close(); err != nil {
						t.Error(err)
					}
				}
			}()
			if err := db.Migrate(t.Context()); err != nil {
				t.Fatal(err)
			}
			repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: filepath.Join(t.TempDir(), "repo"), Forge: store.ForgeGitHub, Owner: "owner", Name: "repo", RemoteURL: "git@github.com:owner/repo.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(t.TempDir(), "trees")})
			if err != nil {
				t.Fatal(err)
			}
			session, err := db.GetOrCreateSession(t.Context(), repo, 7)
			if err != nil {
				t.Fatal(err)
			}
			filter, err := turn.NewFilter(db, turn.Identities{OwnerUserID: 42, BotUserID: 99, TicketOrigin: tickets.Origin()}, nil)
			if err != nil {
				t.Fatal(err)
			}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			ref, err := naming.ParseRef(session.TaskRef)
			if err != nil {
				t.Fatal(err)
			}
			d := &Daemon{db: db, tickets: tickets, filter: filter, classifiers: map[agent.Kind]agent.Classifier{agent.Kind("claude"): explicitWorkflowClassifier{t: t}}}

			comments, err := d.readComments(t.Context(), github.Repo{Owner: "owner", Name: "repo"}, 7, 0)
			if err != nil || len(comments) != 1 {
				t.Fatalf("read owner reply: %v %v", comments, err)
			}
			qualified, err := d.qualifyReply(t.Context(), session.ID, comments[0])
			if err != nil {
				t.Fatal(err)
			}
			admission, err := d.admitReply(t.Context(), session.ID, comments[0], qualified)
			if err != nil || !admission.Triggers() {
				t.Fatalf("owner admission: %+v %v", admission, err)
			}
			command := turn.ParseCommand(admission.Comment.Body)
			mode, actionable := command.Mode()
			if !actionable || mode != agent.ModeImplement || command.Text != "continue the approved story" {
				t.Fatalf("owner command behavior changed: %+v %s", command, mode)
			}
			observedIssue, err := d.readIssue(t.Context(), github.Repo{Owner: "owner", Name: "repo"}, 7)
			if err != nil {
				t.Fatal(err)
			}
			task := &task{repo: repo, forgeRepo: github.Repo{Owner: "owner", Name: "repo"}, issue: observedIssue, session: session, ref: ref, log: logger, tickAt: time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)}
			workflow, selected, err := d.admitWorkflow(t.Context(), task, turn.Trigger{CommentID: 10, Body: admission.Comment.Body, Author: comments[0].Author.Login}, command)
			if err != nil || workflow != agent.WorkflowImplement || selected != agent.ModeImplement {
				t.Fatalf("factory mode admission: %s %s %v", workflow, selected, err)
			}
			savedAdmission, err := db.GetTaskWorkflowAdmission(t.Context(), session.ID)
			if err != nil || savedAdmission.Family != "mode" || savedAdmission.Workflow != "implement" || savedAdmission.SourceTrigger != 10 {
				t.Fatalf("workflow admission not retained: %+v %v", savedAdmission, err)
			}
			selection, err := db.GetModeSelection(t.Context(), session.ID, 10)
			if err != nil || selection.Mode != "implement" || !selection.LabelApplied {
				t.Fatalf("mode transition not confirmed: %+v %v", selection, err)
			}
			plan, err := turn.NewGitHubTicketWorkflow(db, tickets, 99)
			if err != nil {
				t.Fatal(err)
			}
			redactor := redact.New(redact.Options{Token: secrets.NewToken("offline-token"), Environ: []string{}})
			renderer, err := report.New(report.Options{Redactor: redactor, Limit: github.CommentSizeLimit, Logger: logger})
			if err != nil {
				t.Fatal(err)
			}
			proposal := planbrief.PlanProposal{Version: planbrief.ProposalVersion, Parent: ref.String(), Generation: 1, Stories: []planbrief.StoryProposal{{Key: "story-001", Intent: "create", Title: "approved story", Body: "## User Story\nOwner retains the existing workflow.\n## Acceptance Criteria\nPublication is confirmed on the intended ticket.\n## Tasks\nPreserve qualified ticket identities.\n## Architecture\nUse the neutral adapter and existing durable ledgers.\n## Dependencies\nNone.\n"}}}
			index, err := turn.PublishPlan(t.Context(), turn.PublicationOptions{DB: db, Forge: plan, Renderer: renderer, Parent: ref, Generation: 1, BotID: 99}, proposal)
			if err != nil || !strings.Contains(string(index), "https://github.com/owner/repo/issues/8") {
				t.Fatalf("durable Plan publication: %s %v", index, err)
			}
			stories, err := db.ListManagedStories(t.Context(), ref.String(), 1)
			if err != nil || len(stories) != 1 || stories[0].IssueID != 800 || stories[0].IssueNumber != 8 {
				t.Fatalf("durable child identity: %+v %v", stories, err)
			}
			parent, err := plan.GetParentIssue(t.Context(), github.Repo{Owner: "owner", Name: "repo"}, 8)
			if err != nil || parent.ID != 700 {
				t.Fatalf("direct parent: %+v %v", parent, err)
			}
			if err := plan.SetStoryState(t.Context(), github.Repo{Owner: "owner", Name: "repo"}, 7, 700, 8, 800, true); err != nil {
				t.Fatal(err)
			}
			newReporter := func() *turn.Reporter {
				bridge, err := turn.NewGitHubTicketWorkflow(db, tickets, 99)
				if err != nil {
					t.Fatal(err)
				}
				reporter, err := turn.NewReporter(turn.ReporterOptions{DB: db, Forge: bridge, Renderer: renderer, Logger: logger, BotUserID: 99})
				if err != nil {
					t.Fatal(err)
				}
				return reporter
			}
			reporter := newReporter()
			posted, err := reporter.ReportRun(t.Context(), turn.RunReport{Run: report.Run{Ref: ref, Ordinal: 1}, SessionID: session.ID, CommentID: 10})
			if lost {
				if err == nil {
					t.Fatal("lost remote receipt was accepted")
				}
				if _, present, e := db.GetWatermark(t.Context(), session.ID); e != nil || present {
					t.Fatalf("uncertain write consumed owner source: %v %v", present, e)
				}
				pending, found, e := db.FindPendingReportDelivery(t.Context(), session.ID, 10)
				if e != nil || !found || !pending.PossiblySent || pending.CommentID != 0 {
					t.Fatalf("uncertain intent not retained: %+v %v", pending, e)
				}
				bytes, key := pending.Body, pending.Key
				if e := db.Close(); e != nil {
					t.Fatal(e)
				}
				db = nil
				db, e = store.Open(t.Context(), dbPath)
				if e != nil {
					t.Fatal(e)
				}
				reporter = newReporter()
				var exists bool
				posted, exists, err = reporter.ReportPending(t.Context(), session.ID, 10)
				if err != nil || !exists {
					t.Fatalf("restart receipt recovery: %+v %v %v", posted, exists, err)
				}
				retained, e := db.GetReportDelivery(t.Context(), key)
				if e != nil || retained.Body != bytes || !retained.Completed {
					t.Fatalf("restart changed intent: %+v %v", retained, e)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if posted.CommentID != 900 {
				t.Fatalf("wrong publication receipt: %+v", posted)
			}
			watermark, present, err := db.GetWatermark(t.Context(), session.ID)
			if err != nil || !present || watermark.LastCommentID != 10 {
				t.Fatalf("confirmation did not advance source: %+v %v %v", watermark, present, err)
			}
			binding, err := db.GetSessionTicket(t.Context(), session.ID)
			if err != nil || binding.RemoteID != "700" || binding.TaskRef != session.TaskRef {
				t.Fatalf("historical task identity changed: %+v %v", binding, err)
			}
			mu.Lock()
			defer mu.Unlock()
			if creates != 1 || attachments != 1 || posts != 1 || childState != "closed" {
				t.Fatalf("duplicate or missing effects: creates=%d attachments=%d posts=%d state=%s", creates, attachments, posts, childState)
			}
			t.Logf("owner implement command, qualified story creation/attachment/completion and report receipt 900 confirmed; lost-response restart=%v; exactly one publication", lost)
		})
	}
}
