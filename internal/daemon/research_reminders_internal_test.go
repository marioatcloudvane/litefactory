package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

type reminderForge struct {
	Forge
	issue                    github.Issue
	comments                 []github.Comment
	posts                    int
	loseReceipt, hideReceipt bool
	observeErr               error
}

func (f *reminderForge) GetIssue(context.Context, github.Repo, int) (github.Issue, error) {
	return f.issue, f.observeErr
}
func (f *reminderForge) ListCommentsAfter(context.Context, github.Repo, int, int64) ([]github.Comment, error) {
	if f.hideReceipt {
		return f.comments[:1], f.observeErr
	}
	return f.comments, f.observeErr
}
func (f *reminderForge) PostComment(_ context.Context, repo github.Repo, issue int, body redact.Body) (github.Comment, error) {
	f.posts++
	c := github.Comment{ID: int64(200 + f.posts), Author: github.User{ID: 99}, Body: body.String(), HTMLURL: fmt.Sprintf("https://github.com/%s/%s/issues/%d#issuecomment-%d", repo.Owner, repo.Name, issue, 200+f.posts)}
	f.comments = append(f.comments, c)
	if f.loseReceipt {
		return github.Comment{}, errors.New("lost reminder POST reply")
	}
	return c, nil
}

func reminderDaemon(t *testing.T, kinds ...string) (*Daemon, *researchRecoveryFixture, *reminderForge) {
	t.Helper()
	kind := "question"
	if len(kinds) != 0 {
		kind = kinds[0]
	}
	f := newResearchRecoveryFixture(t)
	researchRecoveryCheck(t, f.db.CompleteGraphNodeAttempt(t.Context(), f.attempt.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "success", FinishedAt: f.at}))
	researchRecoveryCheck(t, f.db.RetainGraphComment(t.Context(), store.GraphCommentIntent{Key: "original-operation", InstanceID: f.instance.ID, SourceAttemptID: f.attempt.ID, Node: "comment", Kind: kind, Candidate: []byte(`{"outcome":"incomplete"}`)}))
	_, err := f.db.EnsureReportDelivery(t.Context(), store.ReportDelivery{Key: "original-operation", SessionID: f.session.ID, RunID: f.run.ID, TriggerID: 20, Task: f.session.TaskRef, Destination: f.session.TaskRef, TriggerNamespace: "issue-comment", Purpose: "research-" + kind, BotUserID: 99, Body: "Exact authored text", Marker: "original-operation"})
	researchRecoveryCheck(t, err)
	publishResearchQuestion(t, f)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	filter, e := turn.NewFilter(f.db, turn.Identities{OwnerUserID: 7, BotUserID: 99}, log)
	researchRecoveryCheck(t, e)
	forge := &reminderForge{issue: github.Issue{Number: 342, State: github.StateOpen, Labels: []string{"Mode: Research"}, Assignees: []github.User{{ID: 7}}}, comments: []github.Comment{{ID: 101, Body: "Exact authored text", Author: github.User{ID: 99}, CreatedAt: f.at, HTMLURL: "https://github.com/acme/research/issues/342#issuecomment-101"}}}
	renderer, e := report.New(report.Options{Redactor: redact.New(redact.Options{Token: secrets.NewToken("test-token"), Environ: []string{}, Logger: log}), Limit: github.CommentSizeLimit, Logger: log})
	researchRecoveryCheck(t, e)
	reporter, e := turn.NewReporter(turn.ReporterOptions{DB: f.db, BotUserID: 99, Forge: forge, Renderer: renderer, Logger: log})
	researchRecoveryCheck(t, e)
	return &Daemon{db: f.db, instanceID: "reminder-daemon", forge: forge, filter: filter, reporter: reporter, log: log}, f, forge
}

func TestResearchReminderBoundaryOnceAndLateReply(t *testing.T) {
	d, f, forge := reminderDaemon(t)
	original, e := f.db.GetGraphOwnerWait(t.Context(), f.instance.ID)
	researchRecoveryCheck(t, e)
	wm, _, e := f.db.GetWatermark(t.Context(), f.session.ID)
	researchRecoveryCheck(t, e)
	d.researchReminders(t.Context(), f.at.Add(store.ResearchReminderDelay-time.Millisecond))
	if forge.posts != 0 {
		t.Fatal("reminder posted early")
	}
	d.researchReminders(t.Context(), f.at.Add(store.ResearchReminderDelay))
	d.researchReminders(t.Context(), f.at.Add(30*24*time.Hour))
	if forge.posts != 1 {
		t.Fatalf("posts=%d", forge.posts)
	}
	body := forge.comments[1].Body
	if !strings.Contains(body, "#issuecomment-101") || !strings.Contains(body, "may reply later") {
		t.Fatalf("reminder body=%s", body)
	}
	got, e := f.db.GetGraphOwnerWait(t.Context(), f.instance.ID)
	researchRecoveryCheck(t, e)
	afterWM, _, e := f.db.GetWatermark(t.Context(), f.session.ID)
	researchRecoveryCheck(t, e)
	if !reflect.DeepEqual(got, original) || !reflect.DeepEqual(wm, afterWM) {
		t.Fatal("reminder changed owner wait/input watermark")
	}
	_, e = AdmitResearchOwnerReply(t.Context(), f.db, f.instance.ID, 7, f.session.TaskRef, f.session.TaskRef, "research", github.Comment{ID: 300, Author: github.User{ID: 7}, Body: "please continue"}, f.at.Add(31*24*time.Hour))
	researchRecoveryCheck(t, e)
	if _, e = AdmitResearchOwnerReply(t.Context(), f.db, f.instance.ID, 7, f.session.TaskRef, f.session.TaskRef, "research", github.Comment{ID: 300, Author: github.User{ID: 7}, Body: "please continue"}, f.at.Add(31*24*time.Hour)); e == nil {
		t.Fatal("duplicate late reply admitted")
	}
}

func TestResearchReminderObservationDefersAndSuppresses(t *testing.T) {
	for _, state := range []string{"unknown publication", "wrong original bot", "closed", "mode changed", "pending owner reply", "observation failed"} {
		t.Run(state, func(t *testing.T) {
			d, f, forge := reminderDaemon(t)
			switch state {
			case "unknown publication":
				forge.comments[0].CreatedAt = time.Time{}
			case "wrong original bot":
				forge.comments[0].Author.ID = 8
			case "closed":
				forge.issue.State = "closed"
			case "mode changed":
				forge.issue.Labels = []string{"Mode: Plan"}
			case "pending owner reply":
				forge.comments = append(forge.comments, github.Comment{ID: 150, Author: github.User{ID: 7}, Body: "done"})
			case "observation failed":
				forge.observeErr = errors.New("offline")
			}
			d.researchReminders(t.Context(), f.at.Add(store.ResearchReminderDelay))
			if forge.posts != 0 {
				t.Fatal("ineligible reminder posted")
			}
			wait, e := f.db.GetGraphOwnerWait(t.Context(), f.instance.ID)
			researchRecoveryCheck(t, e)
			if wait.State != "waiting" || wait.ReplyID != 0 {
				t.Fatal("reminder path changed reply authority")
			}
		})
	}
}

func TestResearchReminderLostReceiptRecoversWithoutRepost(t *testing.T) {
	d, f, forge := reminderDaemon(t)
	forge.loseReceipt = true
	at := f.at.Add(store.ResearchReminderDelay)
	d.researchReminders(t.Context(), at)
	reminder, e := f.db.GetResearchReminder(t.Context(), "original-operation")
	researchRecoveryCheck(t, e)
	delivery, e := f.db.GetReportDelivery(t.Context(), reminder.DeliveryKey)
	researchRecoveryCheck(t, e)
	if !delivery.PossiblySent || delivery.Completed {
		t.Fatal("lost receipt did not remain uncertain")
	}
	// At-least-once: a receipt hidden from the complete lookup is reposted
	// once under the same marker, and that reply is lost too.
	forge.hideReceipt = true
	d.researchReminders(t.Context(), at.Add(time.Minute))
	if forge.posts != 2 {
		t.Fatalf("hidden receipt reposts=%d, want one repost", forge.posts-1)
	}
	// Simulate restart of SQLite and Reporter, preserving the fixed journal identity.
	path := f.db.Path()
	researchRecoveryCheck(t, f.db.Close())
	f.db, e = store.Open(t.Context(), path)
	researchRecoveryCheck(t, e)
	researchRecoveryCheck(t, f.db.Migrate(t.Context()))
	d.db = f.db
	renderer, e := report.New(report.Options{Redactor: redact.New(redact.Options{Token: secrets.NewToken("test-token"), Environ: []string{}, Logger: d.log}), Limit: github.CommentSizeLimit, Logger: d.log})
	researchRecoveryCheck(t, e)
	reporter, e := turn.NewReporter(turn.ReporterOptions{DB: f.db, BotUserID: 99, Forge: forge, Renderer: renderer, Logger: d.log})
	researchRecoveryCheck(t, e)
	d.reporter = reporter
	forge.hideReceipt = false
	forge.issue.State = "closed"
	d.researchReminders(t.Context(), at.Add(2*time.Minute))
	final, e := f.db.GetReportDelivery(t.Context(), delivery.Key)
	researchRecoveryCheck(t, e)
	if !final.Completed || forge.posts != 2 || final.Body != delivery.Body {
		t.Fatal("receipt recovery changed identity or reposted")
	}
	wait, e := f.db.GetGraphOwnerWait(t.Context(), f.instance.ID)
	researchRecoveryCheck(t, e)
	if wait.State != "waiting" {
		t.Fatal("notice completion settled owner wait")
	}
}

func TestResearchReminderClaimRechecksOwnerWait(t *testing.T) {
	d, f, _ := reminderDaemon(t)
	at := f.at.Add(store.ResearchReminderDelay)
	researchRecoveryCheck(t, f.db.VerifyResearchWaitPublication(t.Context(), "original-operation", f.at))
	reminder, e := f.db.GetResearchReminder(t.Context(), "original-operation")
	researchRecoveryCheck(t, e)
	intent := store.ReportDelivery{Key: "prepared-reminder", SessionID: f.session.ID, Task: reminder.Task, Destination: reminder.Destination, TriggerNamespace: store.ResearchReminderNamespace, Purpose: store.ResearchReminderNamespace, BotUserID: 99, Body: "notice", Marker: "notice-marker"}
	_, e = f.db.EnsureReportDelivery(t.Context(), intent)
	researchRecoveryCheck(t, e)
	researchRecoveryCheck(t, f.db.BindResearchReminder(t.Context(), reminder.WaitKey, intent.Key, at))
	_, e = AdmitResearchOwnerReply(t.Context(), f.db, f.instance.ID, 7, f.session.TaskRef, f.session.TaskRef, "research", github.Comment{ID: 150, Author: github.User{ID: 7}, Body: "answer"}, at)
	researchRecoveryCheck(t, e)
	claimed, e := f.db.MarkReportDeliverySent(t.Context(), intent.Key)
	researchRecoveryCheck(t, e)
	if claimed {
		t.Fatal("owner claim lost to reminder send")
	}
	reminder, e = f.db.GetResearchReminder(t.Context(), reminder.WaitKey)
	researchRecoveryCheck(t, e)
	if !reminder.Suppressed {
		t.Fatal("prepared reminder was not suppressed")
	}
	reporter, ok := d.reporter.(researchReminderReporter)
	if !ok {
		t.Fatal("reporter lacks reminder support")
	}
	posted, e := reporter.ReportResearchReminder(t.Context(), reminder.WaitKey, at)
	researchRecoveryCheck(t, e)
	if !posted.Skipped {
		t.Fatal("suppressed reminder not skipped")
	}
	if _, found, e := f.db.FindPendingReportDelivery(t.Context(), f.session.ID, 0); e != nil || found {
		t.Fatal("reminder leaked into source recovery")
	}
}

func TestResearchReminderReviewKeepsItsOwnReplyCutoff(t *testing.T) {
	d, f, forge := reminderDaemon(t, "review")
	at := f.at.Add(store.ResearchReminderDelay)
	d.researchReminders(t.Context(), at)
	if forge.posts != 1 {
		t.Fatal("review reminder missing")
	}
	before, e := f.db.GetGraphOwnerWait(t.Context(), f.instance.ID)
	researchRecoveryCheck(t, e)
	if before.State != "waiting" || before.ReplyID != 0 {
		t.Fatal("reminder approved review")
	}
	_, e = AdmitResearchOwnerReply(t.Context(), f.db, f.instance.ID, 7, f.session.TaskRef, f.session.TaskRef, "research", github.Comment{ID: 101, Author: github.User{ID: 7}, Body: "done"}, at)
	if !errors.Is(e, store.ErrGraphReplyPredatesQuestion) {
		t.Fatalf("original reply cutoff changed: %v", e)
	}
	_, e = AdmitResearchOwnerReply(t.Context(), f.db, f.instance.ID, 7, f.session.TaskRef, f.session.TaskRef, "research", github.Comment{ID: 300, Author: github.User{ID: 7}, Body: "done"}, at.Add(time.Hour))
	researchRecoveryCheck(t, e)
	d.researchReminders(t.Context(), at.Add(24*time.Hour))
	if forge.posts != 1 {
		t.Fatal("answered review reminded again")
	}
}

func TestResearchReminderPublicationAgeAndPreparationStayBound(t *testing.T) {
	_, f, _ := reminderDaemon(t)
	researchRecoveryCheck(t, f.db.VerifyResearchWaitPublication(t.Context(), "original-operation", f.at))
	if e := f.db.VerifyResearchWaitPublication(t.Context(), "original-operation", f.at.Add(time.Hour)); e == nil {
		t.Fatal("verified age changed")
	}
	r, e := f.db.GetResearchReminder(t.Context(), "original-operation")
	researchRecoveryCheck(t, e)
	if !r.PublishedAt.Equal(f.at) {
		t.Fatal("original publication age lost")
	}
	intent := store.ReportDelivery{Key: "early-reminder", SessionID: f.session.ID, Task: r.Task, Destination: r.Destination, TriggerNamespace: store.ResearchReminderNamespace, Purpose: store.ResearchReminderNamespace, BotUserID: 99, Body: "notice", Marker: "notice-marker"}
	_, e = f.db.EnsureReportDelivery(t.Context(), intent)
	researchRecoveryCheck(t, e)
	if e = f.db.BindResearchReminder(t.Context(), r.WaitKey, intent.Key, f.at.Add(store.ResearchReminderDelay-time.Millisecond)); e == nil {
		t.Fatal("early reminder binding accepted")
	}
	claimed, e := f.db.MarkReportDeliverySent(t.Context(), intent.Key)
	if e == nil || claimed {
		t.Fatal("unbound reminder sent")
	}
}

func TestResearchReminderUntrustedReceiptCannotSettleDelivery(t *testing.T) {
	for _, mismatch := range []string{"author", "body", "destination"} {
		t.Run(mismatch, func(t *testing.T) {
			d, f, forge := reminderDaemon(t)
			forge.loseReceipt = true
			at := f.at.Add(store.ResearchReminderDelay)
			d.researchReminders(t.Context(), at)
			r, err := f.db.GetResearchReminder(t.Context(), "original-operation")
			researchRecoveryCheck(t, err)
			before, err := f.db.GetReportDelivery(t.Context(), r.DeliveryKey)
			researchRecoveryCheck(t, err)
			switch mismatch {
			case "author":
				forge.comments[1].Author.ID = 7
			case "body":
				forge.comments[1].Body += " changed"
			case "destination":
				forge.comments[1].HTMLURL = "https://github.com/acme/other/issues/342#issuecomment-201"
			}
			d.researchReminders(t.Context(), at.Add(time.Minute))
			after, err := f.db.GetReportDelivery(t.Context(), r.DeliveryKey)
			researchRecoveryCheck(t, err)
			if after.Completed || !after.PossiblySent || after.Key != before.Key || after.Body != before.Body || forge.posts != 1 {
				t.Fatalf("untrustworthy receipt settled or replaced uncertain delivery: before=%+v after=%+v posts=%d", before, after, forge.posts)
			}
		})
	}
}

func TestResearchReminderPossiblySentCannotBeSuppressed(t *testing.T) {
	d, f, forge := reminderDaemon(t)
	forge.loseReceipt = true
	at := f.at.Add(store.ResearchReminderDelay)
	d.researchReminders(t.Context(), at)
	r, err := f.db.GetResearchReminder(t.Context(), "original-operation")
	researchRecoveryCheck(t, err)
	researchRecoveryCheck(t, f.db.SuppressResearchReminder(t.Context(), r.WaitKey))
	after, err := f.db.GetResearchReminder(t.Context(), r.WaitKey)
	researchRecoveryCheck(t, err)
	if after.Suppressed || after.DeliveryKey != r.DeliveryKey {
		t.Fatalf("possibly sent notice lost recovery identity: %+v", after)
	}
	_, err = AdmitResearchOwnerReply(t.Context(), f.db, f.instance.ID, 7, f.session.TaskRef, f.session.TaskRef, "research", github.Comment{ID: 300, Author: github.User{ID: 7}, Body: "answer"}, at)
	researchRecoveryCheck(t, err)
	d.researchReminders(t.Context(), at.Add(time.Minute))
	delivery, err := f.db.GetReportDelivery(t.Context(), r.DeliveryKey)
	researchRecoveryCheck(t, err)
	wait, err := f.db.GetGraphOwnerWait(t.Context(), f.instance.ID)
	researchRecoveryCheck(t, err)
	if !delivery.Completed || forge.posts != 1 || wait.ReplyID != 300 {
		t.Fatalf("notice recovery disturbed late reply: delivery=%+v wait=%+v posts=%d", delivery, wait, forge.posts)
	}
}
