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

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestNoticeMappingAndQuestionBudget(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 10, 18, 0, 0, 0, time.UTC)
	record := store.NoticeRecord{ID: 7, TaskRef: "github:acme/repo#1", Workflow: "implement", Ready: true, At: at, Run: store.Run{ID: 7, Status: store.RunReported, SemanticOutcome: "failed"}}
	for _, tc := range []struct{ source, kind, key string }{{"run_starts", "started", "run:7:start"}, {"run_finishes", "failed", "run:7:finish"}, {"transitions", "stage changed", "transition:7@" + store.FormatTime(at) + "@github:acme/repo#1"}, {"owner_waits", "question", "wait:question-key@" + store.FormatTime(at) + "@github:acme/repo#1"}} {
		t.Run(tc.source, func(t *testing.T) {
			r := record
			r.From = "build"
			r.To = "save"
			r.Key = "question-key"
			r.Kind = "question"
			r.CommentID = 91
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
			if !strings.Contains(text, r.TaskRef) || !strings.Contains(text, "https://github.com/acme/repo/issues/1") {
				t.Fatalf("text=%s", text)
			}
			if tc.source == "owner_waits" && (!strings.Contains(text, "What scope?") || !strings.Contains(text, "#issuecomment-91")) {
				t.Fatal(text)
			}
		})
	}
	record.From = "same"
	record.To = "same"
	if events, err := messaging.NoticeEvents("transitions", record); err != nil || len(events) != 0 {
		t.Fatalf("same-node events=%+v err=%v", events, err)
	}
	record.Run.Status = store.RunFailed
	record.Run.RecoveryReason = `owner-stop:{"run_id":7,"at":"2026-10-10T18:00:00Z"}`
	events, err := messaging.NoticeEvents("run_finishes", record)
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
	events, err = messaging.NoticeEvents("owner_waits", parked)
	if err != nil || len(events) != 2 || events[0].Key != "stop:park@"+store.FormatTime(at)+"@github:acme/repo#1" || events[1].Kind != "question" {
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
	allocate := func(trigger int64, start time.Time, pre bool) store.Run {
		t.Helper()
		r, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: seg.ID, Mode: "implement", Agent: "codex", TriggerCommentID: trigger, StartedAt: start, Prelaunch: pre, LogPath: func(n int) string { return fmt.Sprintf("/logs/%d.jsonl", n) }})
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
	c.Protect = func(_ context.Context, text string) (string, error) {
		if strings.HasPrefix(text, "started:") {
			return "", errors.New("controlled protection failure")
		}
		return text, nil
	}
	if err := c.Collect(ctx, at); err == nil {
		t.Fatal("collector hid protection failure")
	}
	if _, err := db.ReadChatNotice(ctx, "test", fmt.Sprintf("run:%d:finish", history.ID)); err != nil {
		t.Fatalf("one source blocked another: %v", err)
	}
	c.Protect = nil
	if err := c.Collect(ctx, at); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{fmt.Sprintf("run:%d:start", pre.ID), fmt.Sprintf("run:%d:finish", history.ID)} {
		if _, err := db.ReadChatNotice(ctx, "test", key); err != nil {
			t.Fatal(err)
		}
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
