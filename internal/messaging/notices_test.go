package messaging_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestNoticeMappingAndQuestionBudget(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 10, 18, 0, 0, 0, time.UTC)
	record := store.NoticeRecord{ID: 7, TaskRef: "github:acme/repo#1", Workflow: "implement", Ready: true, At: at, Run: store.Run{ID: 7, Status: store.RunReported, SemanticOutcome: "failed"}}
	for _, tc := range []struct{ source, kind, key string }{{"run_starts", "started", "run:7:start"}, {"run_finishes", "failed", "run:7:finish"}, {"owner_waits", "waiting for you", "wait:question-key@" + store.FormatTime(at) + "@github:acme/repo#1"}} {
		t.Run(tc.source, func(t *testing.T) {
			r := record
			r.From = "build"
			r.To = "save"
			r.Key = "question-key"
			r.Kind = "question"
			r.CommentID = 91
			r.State = "waiting"
			r.Published = true
			r.Candidate = []byte(`{"questions":[{"text":"What scope?"}]}`)
			events, err := messaging.NoticeEvents(tc.source, r)
			if err != nil || len(events) != 1 {
				t.Fatalf("events=%+v err=%v", events, err)
			}
			e := events[0]
			if e.Kind != tc.kind || e.Key != tc.key {
				t.Fatalf("event=%+v", e)
			}
			text := messaging.NoticeText(e)
			if !strings.HasPrefix(text, tc.kind+": #1 (implement)\n") || strings.Contains(text, "github:") || !strings.Contains(text, "https://github.com/acme/repo/issues/1") {
				t.Fatalf("text=%s", text)
			}
			if tc.source == "owner_waits" && (!strings.Contains(text, "Asked: question") || !strings.Contains(text, "What scope?") || !strings.Contains(text, "#issuecomment-91")) {
				t.Fatal(text)
			}
		})
	}
	// A published review wait asks for a review, without question text.
	review := record
	review.Key, review.Kind, review.CommentID, review.State, review.Published = "review-key", "review", 92, "waiting", true
	review.Candidate = []byte(`{"outcome":"completed"}`)
	events, err := messaging.NoticeEvents("owner_waits", review)
	if err != nil || len(events) != 1 || events[0].Kind != "waiting for you" {
		t.Fatalf("review wait=%+v err=%v", events, err)
	}
	if text := messaging.NoticeText(events[0]); !strings.Contains(text, "Asked: review") || !strings.Contains(text, "#issuecomment-92") {
		t.Fatal(text)
	}
	// A wait that stopped waiting or is not yet published is never announced.
	for _, w := range []struct {
		state     string
		published bool
	}{{"allocated", true}, {"cancelled", true}, {"claimed", true}, {"waiting", false}, {"cancelled", false}} {
		r := review
		r.State, r.Published = w.state, w.published
		if events, err := messaging.NoticeEvents("owner_waits", r); err != nil || len(events) != 0 {
			t.Fatalf("%+v wait events=%+v err=%v", w, events, err)
		}
	}
	// Graph node moves are internal to one mode: never a notice.
	for _, nodes := range [][2]string{{"select_path", "budget"}, {"budget", "worker_agent"}, {"same", "same"}} {
		r := record
		r.From, r.To = nodes[0], nodes[1]
		if events, err := messaging.NoticeEvents("transitions", r); err != nil || len(events) != 0 {
			t.Fatalf("node transition events=%+v err=%v", events, err)
		}
	}
	// Same-mode follow-on runs, including recovered runs, do not start again.
	continued := record
	continued.PreviousMode = "implement"
	if events, err := messaging.NoticeEvents("run_starts", continued); err != nil || len(events) != 0 {
		t.Fatalf("same-mode start events=%+v err=%v", events, err)
	}
	changed := record
	changed.Workflow = "plan"
	changed.PreviousMode = "research"
	events, err = messaging.NoticeEvents("run_starts", changed)
	if err != nil || len(events) != 1 || events[0].Kind != "started" || !strings.Contains(messaging.NoticeText(events[0]), "Mode: research → plan") {
		t.Fatalf("mode change start=%+v err=%v", events, err)
	}
	record.Run.Status = store.RunFailed
	record.Run.RecoveryReason = `owner-stop:{"run_id":7,"at":"2026-10-10T18:00:00Z"}`
	events, err = messaging.NoticeEvents("run_finishes", record)
	if err != nil || events[0].Kind != "stopped" {
		t.Fatalf("stop=%+v err=%v", events, err)
	}
	parked := record
	parked.Ready = false
	parked.Key = "park"
	parked.StopOrigin = "build"
	parked.StopCause = "exhausted"
	parked.StopReason = "Budget spent"
	parked.Candidate = []byte(`{"reason":"Budget spent"}`)
	events, err = messaging.NoticeEvents("owner_waits", parked)
	if err != nil || len(events) != 1 || events[0].Kind != "stopped" || events[0].Key != "run:7:finish" {
		t.Fatalf("unpublished parked stop=%+v err=%v", events, err)
	}
	parked.Run.RecoveryReason = ""
	events, err = messaging.NoticeEvents("owner_waits", parked)
	if err != nil || len(events) != 1 || events[0].Key != "stop:park@"+store.FormatTime(at)+"@github:acme/repo#1" {
		t.Fatalf("independent graph stop=%+v err=%v", events, err)
	}
	parked.Run.ID = 0
	parked.Ready = true
	parked.State = "waiting"
	parked.Published = true
	events, err = messaging.NoticeEvents("owner_waits", parked)
	if err != nil || len(events) != 2 || events[0].Key != "stop:park@"+store.FormatTime(at)+"@github:acme/repo#1" || events[1].Kind != "waiting for you" || events[0].Asked != "" {
		t.Fatalf("runless parked stop=%+v err=%v", events, err)
	}
	ref, _ := naming.ParseRef(record.TaskRef)
	cost := "0.125"
	usage := &report.MeterSummary{EstimatedUSD: &cost, CollectionStatus: "complete", PricingStatus: "priced"}
	for _, kind := range []string{"completed", "failed", "stopped"} {
		text := messaging.NoticeText(messaging.NoticeEvent{Task: ref, Kind: kind, Usage: usage})
		if !strings.Contains(text, "Estimated cost: $0.125") {
			t.Fatal(text)
		}
	}
	text := messaging.NoticeText(messaging.NoticeEvent{Task: ref, Kind: "completed"})
	if strings.Contains(text, "Usage:") {
		t.Fatal(text)
	}
	text = messaging.NoticeText(messaging.NoticeEvent{Task: ref, Kind: "started", Workflow: "implement", Title: "Authorize work"})
	if !strings.HasPrefix(text, "started: #1 Authorize work (implement)\nhttps://github.com/acme/repo/issues/1") {
		t.Fatal(text)
	}
	text = messaging.NoticeText(messaging.NoticeEvent{Task: ref, Kind: "question", Question: strings.Repeat("😀", 3000), QuestionLink: ref.IssueURL() + "#issuecomment-91"})
	if len(utf16.Encode([]rune(text))) > 1900 || !strings.Contains(text, "#issuecomment-91") || !strings.HasSuffix(text, "…") {
		t.Fatal("question budget or link lost")
	}
}

type noticeTransport struct {
	delivery messaging.Delivery
	calls    int
	target   messaging.Target
}

func (f *noticeTransport) Deliver(_ context.Context, target messaging.Target, _ string) messaging.Delivery {
	f.calls++
	f.target = target
	return f.delivery
}

type noticeIssues struct {
	fail  bool
	calls int
}

func (f *noticeIssues) GetIssue(_ context.Context, repo github.Repo, n int) (github.Issue, error) {
	f.calls++
	if f.fail {
		return github.Issue{}, errors.New("controlled issue read failure")
	}
	return github.Issue{Number: n, Title: "Authorize work for " + repo.Owner + "/" + repo.Name}, nil
}

func TestNoticeCollectionEnableStaleAndRetry(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "notices.db"))
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
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "repo", RemoteURL: "git@github.com:acme/repo.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(ctx, repo, 1)
	if err != nil {
		t.Fatal(err)
	}
	seg, err := db.GetOrCreateSegment(ctx, session.ID, 1, "session", "codex")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 10, 18, 0, 0, 0, time.UTC)
	mode := "implement"
	allocate := func(trigger int64, start time.Time, pre bool) store.Run {
		t.Helper()
		r, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: seg.ID, Mode: mode, Agent: "codex", TriggerCommentID: trigger, StartedAt: start, Prelaunch: pre, LogPath: func(n int) string { return fmt.Sprintf("/logs/%d.jsonl", n) }})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	history := allocate(1, at.Add(-time.Hour), false)
	c := &messaging.NoticeCollector{DB: db, Transport: "test"}
	if err := c.Collect(ctx, at); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.ListNoticeRecords(ctx, "test", "run_starts", at); err == nil {
		t.Fatal("disabled collector wrote positions")
	}
	c.Channel = "3"
	var logs bytes.Buffer
	c.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	if err := c.Collect(ctx, at); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ReadChatNotice(ctx, "test", fmt.Sprintf("run:%d:start", history.ID)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("history sent: %v", err)
	}
	old := allocate(2, at.Add(-25*time.Hour), false)
	mode = "validate" // A mode change after the earlier implement runs.
	pre := allocate(3, at, true)
	if err := c.Collect(ctx, at); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ReadChatNotice(ctx, "test", fmt.Sprintf("run:%d:start", old.ID)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("stale sent: %v", err)
	}
	if !strings.Contains(logs.String(), "skipping stale") {
		t.Fatal("stale not logged")
	}
	if _, err := db.StartRun(ctx, pre.ID); err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	if _, err := db.FinishRun(ctx, history.ID, store.RunOutcome{Status: store.RunSucceeded, ExitCode: &zero, FinishedAt: at}); err != nil {
		t.Fatal(err)
	}
	// A failed title read still posts the notice with the issue number only.
	issues := &noticeIssues{fail: true}
	c.Issues = issues
	c.Protect = func(_ context.Context, text string) (string, error) {
		if strings.HasPrefix(text, "started:") {
			return "", errors.New("controlled protection failure")
		}
		return text, nil
	}
	if err := c.Collect(ctx, at); err == nil {
		t.Fatal("collector hid protection failure")
	}
	finished, err := db.ReadChatNotice(ctx, "test", fmt.Sprintf("run:%d:finish", history.ID))
	if err != nil {
		t.Fatalf("one source blocked another: %v", err)
	}
	if !strings.HasPrefix(finished.ReplyText, "completed: #1 (implement)\n") {
		t.Fatalf("untitled notice: %s", finished.ReplyText)
	}
	c.Protect = nil
	issues.fail, issues.calls = false, 0
	if err := c.Collect(ctx, at); err != nil {
		t.Fatal(err)
	}
	started, err := db.ReadChatNotice(ctx, "test", fmt.Sprintf("run:%d:start", pre.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(started.ReplyText, "started: #1 Authorize work for acme/repo (validate)\n") || strings.Contains(started.ReplyText, "github:") || issues.calls != 1 {
		t.Fatalf("titled notice: %s calls=%d", started.ReplyText, issues.calls)
	}
	sender := &noticeTransport{delivery: messaging.Delivery{RetryAfter: 10 * time.Second}}
	service := messaging.New(db, "test", "owner", []string{"3"}, sender, nil)
	service.Logger = c.Logger
	if _, err := service.NoticeStep(ctx, at); err != nil {
		t.Fatal(err)
	}
	key := sender.target.MessageID
	row, err := db.ReadChatNotice(ctx, "test", key)
	if err != nil || !row.NextAttemptAt.Equal(at.Add(10*time.Second)) {
		t.Fatalf("retry=%+v err=%v", row, err)
	}
	if sender.target.Handle != "" {
		t.Fatal("notice has interaction handle")
	}
	// First discard the other pending notice, then retry this one at its due time.
	sender.delivery = messaging.Delivery{Permanent: true}
	if _, err := service.NoticeStep(ctx, at); err != nil {
		t.Fatal(err)
	}
	sender.delivery = messaging.Delivery{}
	if _, err := service.NoticeStep(ctx, row.NextAttemptAt); err != nil {
		t.Fatal(err)
	}
	row, err = db.ReadChatNotice(ctx, "test", key)
	if err != nil || !row.NextAttemptAt.Equal(at.Add(12*time.Second)) {
		t.Fatalf("growing retry=%+v err=%v", row, err)
	}
	if _, err := service.NoticeStep(ctx, at.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	row, err = db.ReadChatNotice(ctx, "test", key)
	if err != nil || row.State != "failed" || row.ReplyText != "" {
		t.Fatalf("expiry=%+v err=%v", row, err)
	}
	refused := allocate(4, at, true)
	if _, err := db.FinishRun(ctx, refused.ID, store.RunOutcome{Status: store.RunFailed, SemanticOutcome: "refused", FinishedAt: at}); err != nil {
		t.Fatal(err)
	}
	if err := c.Collect(ctx, at); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ReadChatNotice(ctx, "test", fmt.Sprintf("run:%d:start", refused.ID)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unlaunched refusal generated start: %v", err)
	}
}
