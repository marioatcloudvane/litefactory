package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/testutil"
	"github.com/cloudvane/litefactory/internal/ticket"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
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
	entries    []github.RepositoryComment
	posts      []github.Comment
	postErr    error
	seen       []github.Comment
	issue      github.Issue
	scans      int
	issueReads int
}

func (f *doctorTestForge) ListRepositoryCommentsPage(context.Context, github.Repo, time.Time, int, int) (github.RepositoryCommentPage, error) {
	f.scans++
	return github.RepositoryCommentPage{Comments: f.entries}, nil
}
func (f *doctorTestForge) GetIssue(context.Context, github.Repo, int) (github.Issue, error) {
	f.issueReads++
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
	case <-time.After(testutil.Wait):
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
	waitCtx, cancel := context.WithTimeout(ctx, testutil.Wait)
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

func TestOwnerStopControlsAndReporting(t *testing.T) {
	for _, source := range []string{"cli", "comment"} {
		for _, shape := range []string{"workflow", "doctor", "both", "neither"} {
			t.Run(source+"/"+shape, func(t *testing.T) {
				d, forge, repo := doctorDaemonFixture(t)
				ctx := t.Context()
				check := func(err error) {
					t.Helper()
					if err != nil {
						t.Fatal(err)
					}
				}
				ref, err := doctor.Target("acme/api", 406)
				check(err)
				var run store.Run
				worker, cancel := context.WithCancel(ctx)
				defer cancel()
				other, otherCancel := context.WithCancel(ctx)
				defer otherCancel()
				d.inflight.enter(999)
				d.inflight.bindCancel(999, otherCancel)
				defer d.inflight.leave(999)
				if shape == "workflow" || shape == "both" {
					session, err := d.db.GetOrCreateSession(ctx, repo, 406)
					check(err)
					segment, err := d.db.GetOrCreateSegment(ctx, session.ID, 1, "implement", store.AgentClaude)
					check(err)
					run, err = d.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "implement", Agent: store.AgentClaude, LogPath: func(int) string { return "fixture.log" }})
					check(err)
					d.inflight.enter(session.ID)
					d.inflight.bindCancel(session.ID, cancel)
				}
				if shape == "doctor" || shape == "both" {
					_, err := d.db.BeginDoctorAttempt(ctx, ref.String(), store.DoctorAttempt{ID: "active", SourceID: "test:active", OwnerText: "diagnose", PromptVersion: "test", GuideVersion: "test"})
					check(err)
				}
				if !d.pool.try() {
					t.Fatal("no worker slot")
				}
				defer d.pool.done()
				if source == "cli" {
					// Separate service, as used by the CLI, with no shared cancel map.
					service := doctor.Service{DB: d.db, Paths: d.doctor.service.Paths, Config: d.doctor.service.Config, Redactor: d.doctor.service.Redactor}
					stop, err := service.StopIssue(ctx, ref)
					check(err)
					if stop.RunID != run.ID || stop.Conversation != (shape == "doctor" || shape == "both") {
						t.Fatalf("stop=%+v", stop)
					}
				} else {
					_, err := d.admitDoctor(ctx, repo, forge.issue, doctorComment(12, "/doctor stop"))
					check(err)
				}
				check(d.doctorControls(ctx))
				if run.ID != 0 {
					select {
					case <-worker.Done():
					case <-time.After(testutil.Wait):
						t.Fatal("workflow not cancelled with pool full")
					}
					check(d.publishDoctor(ctx))
					if len(forge.posts) != 0 {
						t.Fatal("reported before settlement")
					}
					finished, err := d.db.FinishRun(ctx, run.ID, store.RunOutcome{Status: store.RunFailed, Error: "cancelled", SemanticOutcome: "abandoned"})
					check(err)
					if finished.Error != store.ReasonOwnerStop {
						t.Fatalf("reason=%q", finished.Error)
					}
					check(d.db.SetRunPublication(ctx, run.ID, true, "Saved fixture work."))
					d.inflight.leave(run.SessionID)
					check(d.doctorControls(ctx))
				}
				if other.Err() != nil {
					t.Fatal("unrelated worker cancelled")
				}
				check(d.publishDoctor(ctx))
				check(d.publishDoctor(ctx))
				if source == "cli" {
					if len(forge.posts) != 0 {
						t.Fatal("CLI stop posted")
					}
					return
				}
				if len(forge.posts) != 1 {
					t.Fatalf("posts=%d", len(forge.posts))
				}
				body := forge.posts[0].Body
				if shape == "neither" {
					if !strings.Contains(body, "Nothing was running") {
						t.Fatal(body)
					}
				} else {
					if !strings.Contains(body, "Reply on this issue to resume") {
						t.Fatal(body)
					}
					if run.ID != 0 && !strings.Contains(body, "Workflow") {
						t.Fatal(body)
					}
					if (shape == "doctor" || shape == "both") && !strings.Contains(strings.ToLower(body), "doctor conversation") {
						t.Fatal(body)
					}
				}
			})
		}
	}
}

func TestOwnerStopTerminatesRunnerAndChild(t *testing.T) {
	for _, source := range []string{"cli", "comment"} {
		t.Run(source, func(t *testing.T) {
			d, forge, repo := doctorDaemonFixture(t)
			ctx := t.Context()
			check := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			session, err := d.db.GetOrCreateSession(ctx, repo, 406)
			check(err)
			segment, err := d.db.GetOrCreateSegment(ctx, session.ID, 1, "implement", store.AgentClaude)
			check(err)
			run, err := d.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "implement", Agent: store.AgentClaude, LogPath: func(int) string { return "fixture.log" }})
			check(err)
			listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
			check(err)
			defer listener.Close()
			check(listener.SetDeadline(time.Now().Add(20 * time.Second)))
			root := t.TempDir()
			script := filepath.Join(root, "claude")
			address, ok := listener.Addr().(*net.TCPAddr)
			if !ok {
				t.Fatal("fixture listener is not TCP")
			}
			body := fmt.Sprintf("#!/bin/sh\n/bin/sleep 120 &\nchild=$!\nprintf '%%s %%s\\n' \"$$\" \"$child\" | /usr/bin/nc 127.0.0.1 %d\nwait\n", address.Port)
			check(os.WriteFile(script, []byte(body), 0700))
			runner, err := harness.New(harness.Options{Paths: config.NewPaths(filepath.Join(root, "home")), Claude: config.Claude{Binary: script, Tools: []string{"Read"}, AllowedTools: []string{"Read"}, SettingsPath: filepath.Join(root, "settings.json"), TimeoutSeconds: 60}, Model: config.DefaultClaudeModel, Environ: []string{"PATH=/usr/bin:/bin", "HOME=" + root, "USER=fixture"}, Logger: d.log})
			check(err)
			worker, cancel := context.WithCancel(ctx)
			defer cancel()
			d.inflight.enter(session.ID)
			d.inflight.bindCancel(session.ID, cancel)
			done := make(chan error, 1)
			if !d.pool.try() {
				t.Fatal("workflow slot unavailable")
			}
			// One fixture worker, joined below and bounded by its runner timeout.
			go func() {
				defer d.pool.done()
				defer d.inflight.leave(session.ID)
				result, err := runner.Run(worker, agent.Request{Ref: mustDoctorTarget(t), Mode: "implement", RunOrdinal: 1, SegmentOrdinal: 1, Session: agent.SessionRef{Ref: "6f2a1c3d-9e4b-5a7c-8d1e-2f3a4b5c6d7e"}, WorktreePath: root, Prompt: "controlled cancellation"})
				if err == nil && (result == nil || result.Outcome != agent.OutcomeAbandoned) {
					err = fmt.Errorf("runner did not abandon: %+v", result)
				}
				if err == nil {
					_, err = d.db.FinishRun(context.WithoutCancel(ctx), run.ID, store.RunOutcome{Status: store.RunFailed, Error: result.Reason, SemanticOutcome: result.Outcome.String()})
				}
				done <- err
			}()
			conn, err := listener.AcceptTCP()
			if err != nil {
				select {
				case runnerErr := <-done:
					t.Fatalf("runner before readiness: %v", runnerErr)
				default:
					t.Fatal(err)
				}
			}

			check(conn.SetReadDeadline(time.Now().Add(20 * time.Second)))
			line, err := bufio.NewReader(conn).ReadString('\n')
			check(err)
			check(conn.Close())
			var parent, child int
			_, err = fmt.Sscanf(line, "%d %d", &parent, &child)
			check(err)
			if source == "cli" {
				_, err = d.doctor.service.StopIssue(ctx, mustDoctorTarget(t))
				check(err)
			} else {
				_, err = d.admitDoctor(ctx, repo, forge.issue, doctorComment(12, "/doctor stop"))
				check(err)
			}
			check(d.doctorControls(ctx))
			select {
			case err := <-done:
				check(err)
			case <-time.After(20 * time.Second):
				t.Fatal("runner did not settle")
			}
			settled, stopWaiting := context.WithTimeout(ctx, testutil.Wait)
			defer stopWaiting()
			check(d.inflight.drain(settled, session.ID, false))
			check(d.pool.quiesce(settled))
			for _, pid := range []int{parent, child} {
				if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
					t.Fatalf("process %d remains or could not be checked: %v", pid, err)
				}
			}

			check(d.doctorControls(ctx))
			check(d.publishDoctor(ctx))
			if source == "cli" && len(forge.posts) != 0 {
				t.Fatal("CLI posted")
			}
			if source == "comment" && len(forge.posts) != 1 {
				t.Fatalf("posts=%d", len(forge.posts))
			}
		})
	}
}

func mustDoctorTarget(t *testing.T) naming.Ref {
	t.Helper()
	ref, err := doctor.Target("acme/api", 406)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

// routedBridgeForge mirrors the routed ticket bridge: it can post and list
// issue comments but has no repository comment listing.
type routedBridgeForge struct {
	Forge
	t *testing.T
}

func (f routedBridgeForge) ListCommentsPage(context.Context, github.Repo, int, int, int) (github.CommentPage, error) {
	f.t.Error("doctor read through the default forge instead of the connection client")
	return github.CommentPage{}, errors.New("unexpected default forge read")
}
func (f routedBridgeForge) PostComment(context.Context, github.Repo, int, redact.Body) (github.Comment, error) {
	f.t.Error("doctor posted through the default forge instead of the connection client")
	return github.Comment{}, errors.New("unexpected default forge post")
}

func TestDoctorUsesTicketConnectionWhenDefaultForgeIsRoutedBridge(t *testing.T) {
	f := newDoctorProjectFixture(t, false)
	f.d.forge = routedBridgeForge{t: t}
	ctx := t.Context()
	start := f.appointment.ActivatedAt
	if _, err := f.d.db.DoctorScanWindow(ctx, f.repo.ID, start); err != nil {
		t.Fatal(err)
	}
	at := start.Add(2 * time.Minute)
	f.d.discovery.tick(at)
	if err := f.d.discoverDoctor(ctx, at); err != nil {
		t.Fatalf("doctor discovery without repository listing on the default forge: %v", err)
	}
	command, err := f.d.db.ClaimDoctorCommand(ctx, "", false, "bridge-doctor")
	if err != nil || command.CommentID != f.source.ID || command.TaskRef != "github:team/tickets#406" {
		t.Fatalf("doctor command not discovered through the ticket connection: %+v %v", command, err)
	}
	if err := f.d.db.CompleteDoctorCommand(ctx, command.ID, "Diagnosis answered through the ticket connection.", 0); err != nil {
		t.Fatal(err)
	}
	if err := f.d.publishDoctor(ctx); err != nil {
		t.Fatalf("doctor publication without the default forge: %v", err)
	}
	pending, err := f.d.db.PendingDoctorDeliveries(ctx, "")
	if err != nil || len(pending) != 0 {
		t.Fatalf("doctor answer not delivered: %+v %v", pending, err)
	}
	f.publication.mu.Lock()
	defer f.publication.mu.Unlock()
	body := ""
	if len(f.publication.comments) == 1 {
		body, _ = f.publication.comments[0]["body"].(string)
	}
	if f.publication.posts != 1 || !strings.Contains(body, "Diagnosis answered through the ticket connection.") {
		t.Fatalf("doctor answer not posted on the ticket issue: %d %+v", f.publication.posts, f.publication.comments)
	}
}

func doctorLinearConnection(repoID int64) store.ConnectionVersion {
	return store.ConnectionVersion{RepoID: repoID, Role: store.TicketConnection, Provider: ticket.Linear, Origin: "https://api.linear.app", Destination: "team/project", CredentialSource: "env", CredentialEnv: "DOCTOR_LINEAR_TEST", Enabled: true, Provenance: "explicit", Linear: store.LinearConfiguration{WorkspaceID: "workspace", TeamID: "team", ProjectID: "project", AuthKind: "api_key", ModeLabelsJSON: `{"plan":"plan"}`, LifecycleStatesJSON: `{"completed":"done","retired":"cancelled"}`}}
}

func TestDoctorSkipsUnsupportedBeforeAnyScanOrClient(t *testing.T) {
	d, forge, repo := doctorDaemonFixture(t)
	ctx := t.Context()
	var logs bytes.Buffer
	d.log = slog.New(slog.NewTextHandler(&logs, nil))
	d.connections = projectconnection.NewFactory(projectconnection.Options{Resolver: secrets.Resolver{LookupEnv: func(string) (string, bool) { t.Error("resolved unsupported credential"); return "", false }}})
	version, err := d.db.SetConnection(ctx, doctorLinearConnection(repo.ID), 0)
	if err != nil {
		t.Fatal(err)
	}
	tokens := d.discovery.tokens
	for i := 0; i < 3; i++ {
		if err := d.discoverDoctor(ctx, d.now().Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if forge.scans != 0 || d.discovery.tokens != tokens {
		t.Fatal("unsupported project consumed discovery requests")
	}
	if strings.Count(logs.String(), "level=INFO") != 1 || !strings.Contains(logs.String(), "provider=linear") || !strings.Contains(logs.String(), "project=acme/api") || !strings.Contains(logs.String(), "unsupported; skipped") {
		t.Fatalf("skip log: %s", &logs)
	}
	version, err = d.db.SetConnection(ctx, version, version.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.discoverDoctor(ctx, d.now().Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if strings.Count(logs.String(), "level=INFO") != 2 {
		t.Fatalf("new version not logged once: %s", &logs)
	}
	if _, err := d.db.DisableConnection(ctx, repo.ID, store.TicketConnection, version.Revision); err != nil {
		t.Fatal(err)
	}
	if err := d.discoverDoctor(ctx, d.now().Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if strings.Count(logs.String(), "level=INFO") != 2 {
		t.Fatal("disabled connection logged as unsupported")
	}
	at := d.now().Add(5 * time.Minute)
	scan, err := d.db.DoctorScanWindow(ctx, repo.ID, at)
	if err != nil || !scan.ActivatedAt.Equal(at) || !scan.CompletedAt.Equal(at) {
		t.Fatalf("skip initialized scan: %+v %v", scan, err)
	}
}

func TestDoctorMixedProjectsRotatePastUnsupportedAndFailures(t *testing.T) {
	d, _, linearRepo := doctorDaemonFixture(t)
	ctx := t.Context()
	if _, err := d.db.SetConnection(ctx, doctorLinearConnection(linearRepo.ID), 0); err != nil {
		t.Fatal(err)
	}
	var pages []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/user":
			fmt.Fprint(w, `{"id":99,"login":"bot"}`)
		case strings.HasSuffix(r.URL.Path, "/issues/comments"):
			pages = append(pages, r.URL.Path)
			if r.URL.Query().Get("per_page") != "12" {
				t.Errorf("page bound: %s", r.URL)
			}
			if strings.Contains(r.URL.Path, "/z-last/") {
				http.Error(w, "ordinary failure", http.StatusInternalServerError)
				return
			}
			fmt.Fprint(w, `[]`)
		default:
			t.Errorf("unexpected request %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	d.connections = projectconnection.NewFactory(projectconnection.Options{HTTPClient: server.Client(), Resolver: secrets.Resolver{LookupEnv: func(name string) (string, bool) {
		if name == "DOCTOR_LINEAR_TEST" {
			t.Error("resolved Linear credential")
		}
		return "offline-token", true
	}}})
	// IDs increase while names decrease, unlike ListRepos ordering.
	for _, name := range []string{"z-last", "a-first"} {
		repo := linearRepo
		repo.ID, repo.Path, repo.Name = 0, t.TempDir(), name
		repo, err := d.db.UpsertRepo(ctx, repo)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.db.SetConnection(ctx, store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.GitHub, Origin: ticket.Origin(server.URL), Destination: repo.Slug(), CredentialSource: "env", CredentialEnv: "DOCTOR_GITHUB_TEST", Enabled: true, Provenance: "explicit"}, 0); err != nil {
			t.Fatal(err)
		}
		if _, err := d.db.DoctorScanWindow(ctx, repo.ID, d.now()); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 4; i++ {
		at := d.now().Add(time.Duration(i+1) * time.Minute)
		d.discovery.tick(at)
		err := d.discoverDoctor(ctx, at)
		if (err != nil) != (i%2 == 0) {
			t.Fatalf("tick %d: %v", i, err)
		}
		if len(pages) != i+1 {
			t.Fatalf("tick %d consumed %d pages", i, len(pages))
		}
	}
	want := "/repos/acme/z-last/issues/comments,/repos/acme/a-first/issues/comments,/repos/acme/z-last/issues/comments,/repos/acme/a-first/issues/comments"
	if strings.Join(pages, ",") != want {
		t.Fatalf("rotation: %v", pages)
	}
}

func TestDoctorDiscoveryRetainsTwelveTargetBound(t *testing.T) {
	d, forge, _ := doctorDaemonFixture(t)
	ctx := t.Context()
	if err := d.discoverDoctor(ctx, d.now()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		forge.entries = append(forge.entries, github.RepositoryComment{IssueNumber: 406, Comment: doctorComment(int64(i+1), "/doctor explain")})
	}
	if err := d.discoverDoctor(ctx, d.now().Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if forge.scans != 1 || forge.issueReads != 12 {
		t.Fatalf("pages=%d target reads=%d", forge.scans, forge.issueReads)
	}
}
