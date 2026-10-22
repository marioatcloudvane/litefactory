package daemon

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// listingPoster is the recovery forge: it counts every POST and complete lookup
// and finds only the given comments.
type listingPoster struct {
	comments []github.Comment
	posts    int
	lookups  int
}

func (p *listingPoster) PostComment(context.Context, github.Repo, int, redact.Body) (github.Comment, error) {
	p.posts++
	return github.Comment{}, errors.New("recovery must not post a dropped doctor reply")
}
func (p *listingPoster) ListCommentsAfter(context.Context, github.Repo, int, int64) ([]github.Comment, error) {
	p.lookups++
	return p.comments, nil
}

// stoppedConsumeRepair runs a consume step whose POST fails with postErr, then
// stops the repair as the refused tool does. The session is left with the
// consume reply's recovery hold the incident saw.
type stoppedConsumeRepair struct {
	d       *Daemon
	t       *task
	op      store.DoctorRepair
	key     string
	failure string
	poster  *listingPoster
	logs    *bytes.Buffer
}

func newStoppedConsumeRepair(t *testing.T, postErr error) *stoppedConsumeRepair {
	t.Helper()
	d, forge, repo := doctorDaemonFixture(t)
	ctx := t.Context()
	session, e := d.db.GetOrCreateSession(ctx, repo, 406)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = d.db.BootstrapWatermark(ctx, session.ID, 10); e != nil {
		t.Fatal(e)
	}
	if _, e = d.db.RecordComment(ctx, store.Comment{SessionID: session.ID, ForgeCommentID: 11, AuthorHandle: "owner", AuthorUserID: 7, Body: "repair", CreatedAt: d.now(), Classification: store.ClassificationInstruction}); e != nil {
		t.Fatal(e)
	}
	ref, e := naming.ParseRef(session.TaskRef)
	if e != nil {
		t.Fatal(e)
	}
	evidence, e := d.db.ReadDoctorEvidence(ctx, ref)
	if e != nil {
		t.Fatal(e)
	}
	step := store.DoctorRepairStep{Number: 1, Kind: "consume_comment", Target: "comment:11", Before: "10", After: "11"}
	op := approveEffect(t, d.db, session, step, evidence)
	forge.postErr = postErr
	adapter := FactoryDoctorRepairEffects{DB: d.db, Comments: forge, BotID: 99, Redactor: d.doctor.service.Redactor}
	if _, e = adapter.Execute(ctx, ref, op, step, "claude"); !errors.Is(e, doctor.ErrRepairReplyNotPosted) {
		t.Fatalf("failed post was not categorized: %v", e)
	}
	failure := "step 1 consume_comment: doctor reply could not be posted. Run /doctor check to re-diagnose against the current state before approving another repair."
	if e = d.db.StopDoctorRepair(ctx, ref.String(), failure); e != nil {
		t.Fatal(e)
	}
	logs := &bytes.Buffer{}
	d.log = slog.New(slog.NewTextHandler(logs, nil))
	renderer, e := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{}}), Limit: github.CommentSizeLimit, Logger: d.log})
	if e != nil {
		t.Fatal(e)
	}
	poster := &listingPoster{}
	reporter, e := turn.NewReporter(turn.ReporterOptions{DB: d.db, Forge: poster, Renderer: renderer, Logger: d.log, BotUserID: 99})
	if e != nil {
		t.Fatal(e)
	}
	d.reporter = reporter
	tk := &task{session: session, ref: ref, tickAt: d.now()}
	// The incident's hold: an earlier recovery attempt left the source uncertain.
	source := store.ReportRecovery{Key: recoveryKey(session.ID, 11), SessionID: session.ID, TriggerID: 11, Task: ref.String()}
	d.recoveryFailure(ctx, source, tk.tickAt, &turn.DeliveryError{Key: "doctor-consume:" + op.ID + ":1", State: "uncertain", Cause: turn.ErrReportReceiptAbsent})
	if !d.sessionRecoveryHeld(session.ID, tk.tickAt) {
		t.Fatal("fixture did not hold the session")
	}
	saved, e := d.db.ReadDoctorRepair(ctx, ref.String())
	if e != nil {
		t.Fatal(e)
	}
	return &stoppedConsumeRepair{d: d, t: tk, op: saved, key: "doctor-consume:" + op.ID + ":1", failure: saved.Failure, poster: poster, logs: logs}
}

func (s *stoppedConsumeRepair) delivery(t *testing.T) (store.ReportDelivery, bool) {
	t.Helper()
	d, err := s.d.db.GetReportDelivery(t.Context(), s.key)
	if errors.Is(err, store.ErrNotFound) {
		return d, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return d, true
}

func (s *stoppedConsumeRepair) requireSettled(t *testing.T) {
	t.Helper()
	if _, exists := s.delivery(t); exists {
		t.Fatal("dropped reply row retained")
	}
	if s.d.sessionRecoveryHeld(s.t.session.ID, s.t.tickAt) {
		t.Fatal("session recovery hold not released")
	}
	// The gate every d.run passes first now lets the comment through.
	if pending, err := s.d.resumeReport(t.Context(), s.t, 11); pending || err != nil {
		t.Fatalf("later run still blocked: pending=%v err=%v", pending, err)
	}
	repair, err := s.d.db.ReadDoctorRepair(t.Context(), s.t.ref.String())
	if err != nil {
		t.Fatal(err)
	}
	if repair.State != s.op.State || repair.Failure != s.failure+"; doctor reply dropped unposted" {
		t.Fatalf("repair %s failure %q", repair.State, repair.Failure)
	}
	logged := s.logs.String()
	if !strings.Contains(logged, "doctor consume reply settled") || !strings.Contains(logged, "repair_id="+s.op.ID) || !strings.Contains(logged, "step=1") || !strings.Contains(logged, "delivery="+s.key) {
		t.Fatalf("settlement log: %s", logged)
	}
	if s.poster.posts != 0 {
		t.Fatal("dropped reply was posted")
	}
	// C13: a second settlement attempt appends nothing.
	if _, settled, err := s.d.db.SettleFencedDoctorConsumeDelivery(t.Context(), s.key, true); err != nil || settled {
		t.Fatalf("second settlement: settled=%v err=%v", settled, err)
	}
	if again, _ := s.d.db.ReadDoctorRepair(t.Context(), s.t.ref.String()); again.Failure != repair.Failure {
		t.Fatalf("note appended twice: %q", again.Failure)
	}
}

// C8, C13: an unconfirmed consume reply of a stopped repair is settled once a
// complete lookup finds no trace, releasing the session.
func TestStoppedRepairUncertainConsumeReplySettlesAfterEmptyLookup(t *testing.T) {
	s := newStoppedConsumeRepair(t, errors.New("reply lost"))
	if d, exists := s.delivery(t); !exists || !d.PossiblySent || d.CommentID != 0 {
		t.Fatalf("lost reply not uncertain: %+v", d)
	}
	// Inside the backoff nothing is looked up, so nothing proves non-delivery.
	if pending, _ := s.d.resumeReport(t.Context(), s.t, 11); !pending {
		t.Fatal("uncertain reply settled without a lookup")
	}
	if _, exists := s.delivery(t); !exists || s.poster.lookups != 0 {
		t.Fatal("reply settled or looked up during backoff")
	}
	s.t.tickAt = s.t.tickAt.Add(time.Hour)
	if pending, err := s.d.resumeReport(t.Context(), s.t, 11); pending || err != nil {
		t.Fatalf("empty lookup did not settle: pending=%v err=%v", pending, err)
	}
	if s.poster.lookups != 1 {
		t.Fatalf("lookups=%d", s.poster.lookups)
	}
	s.requireSettled(t)
}

// C12, C13: a reply GitHub refused with a rate limit is un-marked inside the
// consume step and later dropped without any POST or lookup.
func TestRateLimitedConsumeReplyIsUnmarkedAndDroppedWithoutPost(t *testing.T) {
	s := newStoppedConsumeRepair(t, &github.RateLimitError{APIError: github.APIError{StatusCode: 403, Err: github.ErrRateLimited}})
	if d, exists := s.delivery(t); !exists || d.PossiblySent || d.CommentID != 0 {
		t.Fatalf("rate-limited reply still possibly sent: %+v", d)
	}
	if s.op.Failure != "step 1 consume_comment: doctor reply could not be posted. Run /doctor check to re-diagnose against the current state before approving another repair." {
		t.Fatalf("repair failure %q", s.op.Failure)
	}
	// Settled at once, inside the backoff, without a POST or a lookup.
	if pending, err := s.d.resumeReport(t.Context(), s.t, 11); pending || err != nil {
		t.Fatalf("un-marked reply not settled: pending=%v err=%v", pending, err)
	}
	if s.poster.lookups != 0 {
		t.Fatalf("lookups=%d", s.poster.lookups)
	}
	s.requireSettled(t)
}

// C9: a conflicting remote comment under the reply's marker keeps the row and
// the uncertain failure.
func TestStoppedRepairConsumeReplyConflictIsNotSettled(t *testing.T) {
	s := newStoppedConsumeRepair(t, errors.New("reply lost"))
	d, _ := s.delivery(t)
	s.poster.comments = []github.Comment{{ID: 500, Body: "changed " + d.Marker, Author: github.User{ID: 99}, HTMLURL: "https://github.com/acme/api/issues/406#issuecomment-500"}}
	s.t.tickAt = s.t.tickAt.Add(time.Hour)
	pending, err := s.d.resumeReport(t.Context(), s.t, 11)
	if !pending || !errors.Is(err, turn.ErrReportReceiptUncertain) || errors.Is(err, turn.ErrReportReceiptAbsent) {
		t.Fatalf("conflict settled: pending=%v err=%v", pending, err)
	}
	if _, exists := s.delivery(t); !exists || !s.d.sessionRecoveryHeld(s.t.session.ID, s.t.tickAt) {
		t.Fatal("conflicting reply dropped or hold released")
	}
}
