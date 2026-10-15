package daemon

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

func restartPlanReporter(t *testing.T, f *publicationIO) *turn.Reporter {
	t.Helper()
	publicationMust(t, f.runtime.Authority.Close(t.Context()))
	publicationMust(t, f.db.Close())
	var err error
	f.db, err = store.Open(t.Context(), f.path)
	publicationMust(t, err)
	f.d.db = f.db
	f.reopenRuntime(t)
	renderer, err := report.New(report.Options{Redactor: f.runtime.Redactor, Limit: github.CommentSizeLimit, Logger: f.d.log})
	publicationMust(t, err)
	reporter, err := turn.NewReporter(turn.ReporterOptions{DB: f.db, Forge: f.runtime.Comments, Renderer: renderer, Logger: f.d.log, BotUserID: 20, StoryLockDir: t.TempDir()})
	publicationMust(t, err)
	f.d.reporter = reporter
	return reporter
}

func TestPlanReportingBoundaryNoticeLossRestartMaterialDebt(t *testing.T) {
	f := newPublicationIO(t)
	parent, err := naming.ParseRef(f.session.TaskRef)
	publicationMust(t, err)
	state := planbrief.PlanState{SharedRevision: "shared", Stories: []planbrief.PlanStoryState{{Key: "affected", Stage: "story", Lifecycle: planbrief.PlanLifecycle{State: "closed", Reason: "completed"}, Effect: "uncertain", EffectRequest: "original-request", EffectHold: "Unknown effect ghp_0123456789abcdefghijklmnopqrstuvwxyz"}, {Key: "healthy", Stage: "story"}}}
	revision, err := f.db.SavePlanWorkflowState(t.Context(), f.session.TaskRef, 0, state)
	publicationMust(t, err)
	f.mu.Lock()
	f.lost = true
	f.mu.Unlock()
	reporter, ok := f.d.reporter.(*turn.Reporter)
	if !ok {
		t.Fatal("fixture reporter has unexpected type")
	}
	if _, err = reporter.ReportPlanNotices(t.Context(), parent, f.session.ID); err == nil {
		t.Fatal("lost report response treated as confirmation")
	}
	notices, err := f.db.PendingPlanStoryNotices(t.Context(), parent.String())
	publicationMust(t, err)
	if len(notices) != 1 {
		t.Fatalf("lost lifecycle debt: %+v", notices)
	}
	originalID := notices[0].ID
	// A new observed change survives while the first delivery is unresolved.
	state.Stories[0].Lifecycle.Reason = "not_planned"
	revision, err = f.db.SavePlanWorkflowState(t.Context(), parent.String(), revision, state)
	publicationMust(t, err)
	_, err = f.db.SavePlanWorkflowState(t.Context(), parent.String(), revision, state)
	publicationMust(t, err)
	notices, err = f.db.PendingPlanStoryNotices(t.Context(), parent.String())
	publicationMust(t, err)
	if len(notices) != 2 || notices[0].ID == notices[1].ID {
		t.Fatalf("material change lost or duplicate unchanged poll: %+v", notices)
	}
	reporter = restartPlanReporter(t, f)
	f.mu.Lock()
	f.lost = false
	f.mu.Unlock()
	pending, err := reporter.ReportPlanNotices(t.Context(), parent, f.session.ID)
	publicationMust(t, err)
	if pending != 0 {
		t.Fatalf("recovery left %d notices", pending)
	}
	_, err = reporter.ReportPlanNotices(t.Context(), parent, f.session.ID)
	publicationMust(t, err)
	notices, err = f.db.PendingPlanStoryNotices(t.Context(), parent.String())
	publicationMust(t, err)
	if len(notices) != 0 {
		t.Fatalf("recovered notice remains pending id=%d %+v", originalID, notices)
	}
	f.mu.Lock()
	posts := f.posts
	bodies := append([]string(nil), f.bodies...)
	f.mu.Unlock()
	if posts != 2 {
		t.Fatalf("duplicate delivery: posts=%d", posts)
	}
	for _, body := range bodies {
		if strings.Contains(body, "ghp_") || !strings.Contains(body, "original-request") || !strings.Contains(body, "affected") {
			t.Fatalf("unsafe or missing notice detail: %s", body)
		}
	}
	retained, _, err := f.db.GetPlanWorkflowState(t.Context(), parent.String())
	publicationMust(t, err)
	if retained.Stories[0].Lifecycle.Reason != "not_planned" || retained.Stories[0].Effect != "uncertain" {
		t.Fatalf("delivery changed lifecycle/effect: %+v", retained)
	}
}

func TestPlanReportingBoundaryStaleReplyLossRetainsCursor(t *testing.T) {
	f := newPublicationIO(t)
	parent, err := naming.ParseRef(f.session.TaskRef)
	publicationMust(t, err)
	f.mu.Lock()
	f.lost = true
	f.mu.Unlock()
	reporter, ok := f.d.reporter.(*turn.Reporter)
	if !ok {
		t.Fatal("fixture reporter has unexpected type")
	}
	if _, err = reporter.ReportPlanReply(t.Context(), parent, f.session.ID, "closed-story", 200, 55, "Child closed after this candidate; use current review."); err == nil {
		t.Fatal("lost explanation acknowledged")
	}
	cursor, err := f.db.StoryDiscoveryCursor(t.Context(), f.session.ID, 200)
	publicationMust(t, err)
	if cursor >= 55 {
		t.Fatalf("unconfirmed child input consumed: %d", cursor)
	}
	retained, err := f.db.PlanReplyExplanation(t.Context(), parent.String(), 55)
	publicationMust(t, err)
	delivery, err := f.db.GetReportDelivery(t.Context(), retained.DeliveryKey)
	publicationMust(t, err)
	if delivery.Completed || !delivery.PossiblySent {
		t.Fatalf("lost reply status %+v", delivery)
	}
	reporter = restartPlanReporter(t, f)
	_, err = reporter.ReportPlanReply(t.Context(), parent, f.session.ID, "closed-story", 200, 55, "The candidate has now changed again")
	publicationMust(t, err)
	after, err := f.db.PlanReplyExplanation(t.Context(), parent.String(), 55)
	publicationMust(t, err)
	if after.Reason != retained.Reason || after.DeliveryKey != retained.DeliveryKey {
		t.Fatal("replay replaced original explanation")
	}
	cursor, err = f.db.StoryDiscoveryCursor(t.Context(), f.session.ID, 200)
	publicationMust(t, err)
	if cursor != 55 {
		t.Fatalf("confirmed explanation did not consume input: %d", cursor)
	}
	_, err = reporter.ReportPlanReply(t.Context(), parent, f.session.ID, "closed-story", 200, 55, retained.Reason)
	publicationMust(t, err)
	f.mu.Lock()
	posts := f.posts
	paths := append([]string(nil), f.postPaths...)
	f.mu.Unlock()
	if posts != 1 {
		t.Fatalf("duplicate explanation posts=%d", posts)
	}
	// C4: the explanation is posted on the sub-issue the owner replied on.
	if len(paths) != 1 || paths[0] != "/repos/owner/api/issues/200/comments" {
		t.Fatalf("explanation posted to %v, want the child issue 200", paths)
	}
	if delivery.Destination != "github:owner/api#200" && !strings.HasSuffix(delivery.Destination, "#200") {
		t.Fatalf("explanation destination %q is not the child issue", delivery.Destination)
	}
	watermark, exists, err := f.db.GetWatermark(t.Context(), f.session.ID)
	publicationMust(t, err)
	if exists && watermark.LastCommentID != 0 {
		t.Fatalf("child explanation consumed parent input %+v", watermark)
	}
	if _, err = reporter.ReportPlanReply(t.Context(), parent, f.session.ID, "another-story", 201, 55, retained.Reason); err == nil || errors.Is(err, turn.ErrReportReceiptUncertain) {
		t.Fatalf("changed input identity accepted: %v", err)
	}
}

func TestPlanHeldEffectNoticeSurvivesSettlement(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(fmt.Sprintf("lost=%v", lost), func(t *testing.T) {
			f := newPublicationIO(t)
			ctx := t.Context()
			parent, err := naming.ParseRef(f.session.TaskRef)
			publicationMust(t, err)
			repo, err := f.db.GetRepo(ctx, f.session.RepoID)
			publicationMust(t, err)
			_, err = f.db.AdmitPlanWorkflowPath(ctx, f.session.ID, "graph")
			publicationMust(t, err)
			state := planbrief.PlanState{SharedRevision: "shared", Stories: []planbrief.PlanStoryState{{Key: "story-001", Stage: "story", Effect: "uncertain"}}}
			state.Stories[0].SetEffectHold("plan-request-692-6680", "owner edits conflict with expected content revision")
			_, err = f.db.ReservePlanStory(ctx, parent.String(), "story-001")
			publicationMust(t, err)
			publicationMust(t, f.db.BindPlanStoryShell(ctx, parent.String(), "story-001", 2000, 200, "original-content"))
			_, err = f.db.SavePlanWorkflowState(ctx, parent.String(), 0, state)
			publicationMust(t, err)
			child, err := f.db.GetOrCreateSession(ctx, repo, 200)
			publicationMust(t, err)
			ref, err := naming.ParseRef(child.TaskRef)
			publicationMust(t, err)
			work := &task{repo: repo, session: child, ref: ref, issue: github.Issue{Number: 200}, log: f.d.log, tickAt: f.now}
			scheduled, _, err := f.db.CreateOrGetSchedule(ctx, store.CreateScheduleParams{SessionID: child.ID, SourceCommentID: planWorkID("effect", "story-001", "plan-request-692-6680"), OwnerHandle: "owner", Payload: planWorkPayload, DueAt: f.now})
			publicationMust(t, err)
			reporter, ok := f.d.reporter.(*turn.Reporter)
			if !ok {
				t.Fatal("fixture reporter has unexpected type")
			}
			f.mu.Lock()
			f.lost = lost
			f.mu.Unlock()
			_, err = reporter.ReportPlanNotices(ctx, parent, f.session.ID)
			if lost == (err == nil) {
				t.Fatalf("lost=%v report error=%v", lost, err)
			}
			notices, err := f.db.PendingPlanStoryNotices(ctx, parent.String())
			publicationMust(t, err)
			var noticeID int64
			if lost {
				if len(notices) != 1 {
					t.Fatalf("pending notices=%+v", notices)
				}
				noticeID = notices[0].ID
			} else if len(notices) != 0 {
				t.Fatalf("delivered notice still pending: %+v", notices)
			}
			reserved, ok, err := f.db.ReserveSchedule(ctx, scheduled.ID, f.now)
			publicationMust(t, err)
			if !ok || !f.d.planWorkSettled(ctx, work, reserved) {
				t.Fatal("held GitHub schedule not settled")
			}
			f.d.finishScheduled(ctx, work, reserved, store.ScheduleSucceeded, 0)
			reporter = restartPlanReporter(t, f)
			f.mu.Lock()
			f.lost = false
			f.mu.Unlock()
			if lost {
				notices, err = f.db.PendingPlanStoryNotices(ctx, parent.String())
				publicationMust(t, err)
				if len(notices) != 1 || notices[0].ID != noticeID {
					t.Fatal("settlement replaced delivery identity")
				}
			}
			for range 2 {
				_, err = reporter.ReportPlanNotices(ctx, parent, f.session.ID)
				publicationMust(t, err)
				queue := &repoQueue{repo: repo, forgeRepo: github.Repo{Owner: repo.Owner, Name: repo.Name}, log: f.d.log}
				publicationMust(t, f.d.graphPlanStories(ctx, map[int64]*repoQueue{repo.ID: queue}, map[int64]*candidate{}))
				if len(queue.candidates) != 0 {
					t.Fatal("held GitHub effect offered again")
				}
			}
			retained, _, err := f.db.GetPlanWorkflowState(ctx, parent.String())
			publicationMust(t, err)
			if !reflect.DeepEqual(state, retained) {
				t.Fatalf("notice changed conflict state: %+v", retained)
			}
			final, err := f.db.GetSchedule(ctx, scheduled.ID)
			publicationMust(t, err)
			if final.Status != store.ScheduleSucceeded || final.RunID != 0 {
				t.Fatalf("settlement=%+v", final)
			}
			f.mu.Lock()
			posts := f.posts
			f.mu.Unlock()
			if posts != 1 {
				t.Fatalf("notice posted %d times", posts)
			}
		})
	}
}

func TestPlanUnboundSummaryDeduplicatesAndClears(t *testing.T) {
	f := newPublicationIO(t)
	ctx := t.Context()
	parent, err := naming.ParseRef(f.session.TaskRef)
	publicationMust(t, err)
	state := planbrief.PlanState{SharedRevision: "shared", Stories: []planbrief.PlanStoryState{{Key: "story-002", Stage: "story"}}}
	_, err = f.db.ReservePlanStory(ctx, parent.String(), "story-002")
	publicationMust(t, err)
	_, err = f.db.SavePlanWorkflowState(ctx, parent.String(), 0, state)
	publicationMust(t, err)
	baseForge, ok := f.runtime.Comments.(turn.PlanStoryForge)
	if !ok {
		t.Fatal("fixture lacks story forge")
	}
	forge := &unboundSummaryForge{PlanStoryForge: baseForge}
	renderer, err := report.New(report.Options{Redactor: f.runtime.Redactor, Limit: github.CommentSizeLimit, Logger: f.d.log})
	publicationMust(t, err)
	reporter, err := turn.NewReporter(turn.ReporterOptions{DB: f.db, Forge: forge, Renderer: renderer, Logger: f.d.log, BotUserID: 20})
	publicationMust(t, err)
	for range 2 {
		publicationMust(t, reporter.ReportPlanSummary(ctx, parent, f.session.ID))
	}
	if f.posts != 1 || !strings.Contains(f.bodies[0], "story-002") || !strings.Contains(f.bodies[0], "cannot draft") || !strings.Contains(f.bodies[0], "plan assembly is blocked") || !strings.Contains(f.bodies[0], "MAR-13") {
		t.Fatalf("summary: %v", f.bodies)
	}
	publicationMust(t, f.db.BindPlanStoryShell(ctx, parent.String(), "story-002", 4010, 401, "shell"))
	for range 2 {
		publicationMust(t, reporter.ReportPlanSummary(ctx, parent, f.session.ID))
	}
	if len(forge.edits) != 1 || strings.Contains(forge.edits[0], "cannot draft") {
		t.Fatalf("warning not cleared: %v", forge.edits)
	}
}

type unboundSummaryForge struct {
	turn.PlanStoryForge
	edits []string
}

func (f *unboundSummaryForge) ListFollowupIssues(context.Context, github.Repo) ([]github.Issue, error) {
	return []github.Issue{{ID: 4010, Number: 401, Body: "<!-- lf:plan-story parent=2780 key=story-002 -->\nShell"}}, nil
}
func (f *unboundSummaryForge) GetIssue(ctx context.Context, repo github.Repo, number int) (github.Issue, error) {
	if number != 401 {
		return f.PlanStoryForge.GetIssue(ctx, repo, number)
	}
	return github.Issue{ID: 4010, Number: 401, Body: "<!-- lf:plan-story parent=2780 key=story-002 -->\nShell", QualifiedSnapshot: &ticket.Snapshot{Ref: ticket.Ref{Identity: ticket.Identity{Provider: ticket.Linear}}, DisplayKey: "MAR-13", BrowserURL: "https://linear.app/test/issue/MAR-13"}}, nil
}
func (f *unboundSummaryForge) GetParentIssue(context.Context, github.Repo, int) (github.Issue, error) {
	return github.Issue{ID: 2780, Number: 278}, nil
}
func (f *unboundSummaryForge) EditComment(_ context.Context, _ github.Repo, _ int64, body redact.Body) error {
	f.edits = append(f.edits, body.String())
	return nil
}
