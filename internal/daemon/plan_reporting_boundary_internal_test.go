package daemon

import (
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
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
