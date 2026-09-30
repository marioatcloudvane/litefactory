package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

type doctorTestFilter struct{}

func (doctorTestFilter) Identities() turn.Identities {
	return turn.Identities{OwnerUserID: 7, BotUserID: 99}
}
func (doctorTestFilter) Admit(context.Context, int64, github.Comment) (turn.Admission, error) {
	return turn.Admission{}, errors.New("normal admission must not run")
}

type doctorTestForge struct {
	Forge
	entries []github.RepositoryComment
	posts   []github.Comment
	postErr error
	seen    []github.Comment
	issue   github.Issue
	scans   int
}

func (f *doctorTestForge) ListRepositoryCommentsPage(context.Context, github.Repo, time.Time, int, int) (github.RepositoryCommentPage, error) {
	f.scans++
	return github.RepositoryCommentPage{Comments: f.entries}, nil
}
func (f *doctorTestForge) GetIssue(context.Context, github.Repo, int) (github.Issue, error) {
	return f.issue, nil
}
func (f *doctorTestForge) ListCommentsPage(context.Context, github.Repo, int, int, int) (github.CommentPage, error) {
	return github.CommentPage{Comments: f.seen}, nil
}
func (f *doctorTestForge) PostComment(_ context.Context, _ github.Repo, _ int, body redact.Body) (github.Comment, error) {
	c := github.Comment{ID: int64(100 + len(f.posts)), Body: body.String(), Author: github.User{ID: 99}}
	f.posts = append(f.posts, c)
	return c, f.postErr
}

type doctorTestRunner struct {
	started chan struct{}
	calls   atomic.Int32
}

func (r *doctorTestRunner) IsolationEvidence(context.Context, string) (string, error) {
	return "controlled-fixture-only", nil
}
func (r *doctorTestRunner) Run(ctx context.Context, _ doctor.Invocation, _ *doctor.ResultService) (doctor.InvocationOutcome, error) {
	r.calls.Add(1)
	close(r.started)
	<-ctx.Done()
	return doctor.InvocationOutcome{}, ctx.Err()
}

func doctorDaemonFixture(t *testing.T) (*Daemon, *doctorTestForge, store.Repo) {
	t.Helper()
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "doctor.db"))
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
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Decode(strings.NewReader("agents:\n  claude: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	forge := &doctorTestForge{issue: github.Issue{ID: 10406, Number: 406, State: "closed"}}
	service := &doctor.Service{DB: db, Paths: config.NewPaths(t.TempDir()), Config: cfg, Redactor: redact.New(redact.Options{Environ: []string{"API_SECRET=doctor-owner-secret"}})}
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	d := &Daemon{instanceID: "live-doctor", db: db, forge: forge, filter: doctorTestFilter{}, log: slog.New(slog.DiscardHandler), doctor: doctorLane{service: service, pool: newPool(1)}, pool: newPool(1), now: func() time.Time { return at }}
	d.discovery.tick(at)
	return d, forge, repo
}
func doctorComment(id int64, body string) github.Comment {
	return github.Comment{ID: id, Body: body, Author: github.User{ID: 7}, CreatedAt: time.Date(2026, 9, 29, 12, 1, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 9, 29, 12, 1, 0, 0, time.UTC)}
}

func TestDoctorPriorityWorkerAndStopIgnoreFullWorkflowPool(t *testing.T) {
	d, forge, repo := doctorDaemonFixture(t)
	ctx := t.Context()
	runner := &doctorTestRunner{started: make(chan struct{})}
	d.doctor.service.Runner = runner
	if !d.pool.try() {
		t.Fatal("workflow slot unavailable")
	}
	defer d.pool.done()
	if _, err := d.admitDoctor(ctx, repo, forge.issue, doctorComment(10, "/doctor diagnose doctor-owner-secret")); err != nil {
		t.Fatal(err)
	}
	d.dispatchDoctor(ctx)
	select {
	case <-runner.started:
	case <-time.After(3 * time.Second):
		t.Fatal("doctor waited for workflow capacity")
	}
	if _, err := d.admitDoctor(ctx, repo, forge.issue, doctorComment(11, "/doctor later")); err != nil {
		t.Fatal(err)
	}
	d.dispatchDoctor(ctx)
	if runner.calls.Load() != 1 {
		t.Fatal("reserved doctor worker exceeded capacity")
	}
	if _, err := d.admitDoctor(ctx, repo, forge.issue, doctorComment(12, "/doctor stop")); err != nil {
		t.Fatal(err)
	}
	if err := d.doctorControls(ctx); err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := d.doctor.pool.quiesce(waitCtx); err != nil {
		t.Fatal("stop did not cancel doctor", err)
	}
	if len(d.pool.slots) != 1 {
		t.Fatal("doctor stop changed workflow capacity")
	}
	ref, err := doctor.Target("acme/api", 406)
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := d.doctor.service.ReadConversation(ctx, ref)
	if err != nil || conversation.Conversation.State != "stopped" {
		t.Fatalf("conversation %+v %v", conversation, err)
	}
	if strings.Contains(conversation.Attempts[0].OwnerText, "doctor-owner-secret") {
		t.Fatal("secret retained")
	}
	if runner.calls.Load() != 1 {
		t.Fatal("queued pre-stop input launched")
	}
}

func TestDoctorDiscoveryActivationOwnerAndClosedIssue(t *testing.T) {
	d, forge, _ := doctorDaemonFixture(t)
	ctx := t.Context()
	at := d.now()
	if err := d.discoverDoctor(ctx, at); err != nil {
		t.Fatal(err)
	}
	if forge.scans != 0 {
		t.Fatal("initial activation replayed history")
	}
	owner := doctorComment(10, "/doctor explain")
	foreign := doctorComment(11, "/doctor foreign")
	foreign.Author.ID = 8
	bot := doctorComment(12, "/doctor bot")
	bot.Author.IsBot = true
	old := doctorComment(13, "/doctor old")
	old.CreatedAt = at.Add(-time.Hour)
	forge.entries = []github.RepositoryComment{{IssueNumber: 406, Comment: owner}, {IssueNumber: 406, Comment: foreign}, {IssueNumber: 406, Comment: bot}, {IssueNumber: 406, Comment: old}}
	if err := d.discoverDoctor(ctx, at.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	c, err := d.db.ClaimDoctorCommand(ctx, "", false, "test-instance")
	if err != nil || c.CommentID != 10 || c.IssueNumber != 406 {
		t.Fatalf("owner closed target %+v %v", c, err)
	}
	if err := d.db.CompleteDoctorCommand(ctx, c.ID, "answer", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := d.db.ClaimDoctorCommand(ctx, "", false, "test-instance"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("nonowner/bot/history admitted: %v", err)
	}
}

func TestDoctorLostPostNeverBlindlyRepostsAndRequiresBotProvenance(t *testing.T) {
	d, forge, repo := doctorDaemonFixture(t)
	ctx := t.Context()
	if _, err := d.admitDoctor(ctx, repo, forge.issue, doctorComment(10, "/doctor apply")); err != nil {
		t.Fatal(err)
	}
	if err := d.doctorControls(ctx); err != nil {
		t.Fatal(err)
	}
	forge.postErr = errors.New("reply lost")
	if err := d.publishDoctor(ctx); err == nil {
		t.Fatal("post loss was hidden")
	}
	if len(forge.posts) != 1 {
		t.Fatal("missing attempted post")
	}
	if err := d.publishDoctor(ctx); err != nil {
		t.Fatal(err)
	}
	if len(forge.posts) != 1 {
		t.Fatal("blind repost after empty page")
	}
	forged := forge.posts[0]
	forged.Author.ID = 7
	forge.seen = []github.Comment{forged}
	if err := d.publishDoctor(ctx); err != nil {
		t.Fatal(err)
	}
	pending, err := d.db.PendingDoctorDeliveries(ctx)
	if err != nil || len(pending) != 1 {
		t.Fatalf("forged provenance confirmed: %+v %v", pending, err)
	}
	forge.seen = forge.posts
	if err := d.publishDoctor(ctx); err != nil {
		t.Fatal(err)
	}
	pending, err = d.db.PendingDoctorDeliveries(ctx)
	if err != nil || len(pending) != 0 {
		t.Fatalf("trusted receipt missing: %+v %v", pending, err)
	}
	if len(forge.posts) != 1 {
		t.Fatal("reconciliation posted again")
	}
}

func TestDoctorRecoveryAdoptsAcceptedReceiptWithoutRelaunch(t *testing.T) {
	d, forge, repo := doctorDaemonFixture(t)
	ctx := t.Context()
	if _, err := d.admitDoctor(ctx, repo, forge.issue, doctorComment(10, "/doctor explain")); err != nil {
		t.Fatal(err)
	}
	command, err := d.db.ClaimDoctorCommand(ctx, "", false, "dead-process")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := doctor.Target("acme/api", 406)
	if err != nil {
		t.Fatal(err)
	}
	a := store.DoctorAttempt{ID: "crashed-attempt", SourceID: command.SourceID(), OwnerText: command.Instructions, PromptVersion: doctor.PromptVersion, GuideVersion: doctor.GuideVersion}
	conversation, err := d.db.BeginDoctorAttempt(ctx, command.TaskRef, a)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := d.db.ReadDoctorEvidence(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	a.Evidence = string(data)
	if err := d.db.PrepareDoctorAttempt(ctx, a); err != nil {
		t.Fatal(err)
	}
	results, err := doctor.BindResults(ctx, d.db, a.ID, evidence, d.doctor.service.Redactor)
	if err != nil {
		t.Fatal(err)
	}
	input := doctor.ResultInput{Diagnosis: doctor.Diagnosis{Findings: []doctor.Finding{{Explanation: "Retained result from before process loss", Uncertainty: "Source unavailable"}}, CoverageGaps: evidence.Gaps}}
	payload, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := results.Submit(ctx, "lost-reply", payload); err != nil {
		t.Fatal(err)
	}
	if err := d.recoverDoctor(ctx); err != nil {
		t.Fatal(err)
	}
	pending, err := d.db.DoctorCommandsNeedingReports(ctx)
	if err != nil || len(pending) != 1 || pending[0].ConversationID != conversation.ID || !strings.Contains(pending[0].ResultBody, "Retained result from before process loss") {
		t.Fatalf("recovery %+v %v", pending, err)
	}
	if _, err := d.db.ClaimDoctorCommand(ctx, "", false, "live-doctor"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("recovery queued another diagnosis: %v", err)
	}
	if err := d.recoverDoctor(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestDoctorRecoveryBudgetIsOneRetry(t *testing.T) {
	d, forge, repo := doctorDaemonFixture(t)
	ctx := t.Context()
	if _, err := d.admitDoctor(ctx, repo, forge.issue, doctorComment(10, "/doctor explain")); err != nil {
		t.Fatal(err)
	}
	first, err := d.db.ClaimDoctorCommand(ctx, "", false, "dead-one")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.recoverDoctor(ctx); err != nil {
		t.Fatal(err)
	}
	retry, err := d.db.ClaimDoctorCommand(ctx, "", false, "dead-two")
	if err != nil || retry.ID != first.ID || retry.Attempts != 2 {
		t.Fatalf("retry %+v %v", retry, err)
	}
	if err := d.recoverDoctor(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := d.db.ClaimDoctorCommand(ctx, "", false, "live-doctor"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unbounded retry admitted: %v", err)
	}
	reports, err := d.db.DoctorCommandsNeedingReports(ctx)
	if err != nil || len(reports) != 1 || !strings.Contains(reports[0].ResultBody, "interrupted twice") {
		t.Fatalf("missing truthful failure %+v %v", reports, err)
	}
}
