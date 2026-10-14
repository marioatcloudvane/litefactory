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

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
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
			if !strings.HasPrefix(text, "Implementation #1 ") || strings.Contains(text, "github:") || !strings.Contains(text, "https://github.com/acme/repo/issues/1") {
				t.Fatalf("text=%s", text)
			}
			if tc.source == "owner_waits" && (!strings.Contains(text, "has a question for you:") || !strings.Contains(text, "What scope?") || !strings.Contains(text, "#issuecomment-91")) {
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
	if text := messaging.NoticeText(events[0]); !strings.Contains(text, "is ready for your review.") || !strings.Contains(text, "#issuecomment-92") {
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
	if err != nil || len(events) != 1 || events[0].Kind != "started" || !strings.Contains(messaging.NoticeText(events[0]), "Plan #1 started.") {
		t.Fatalf("mode change start=%+v err=%v", events, err)
	}
	// A run abandoned in process by a daemon shutdown is relaunched by
	// recovery and is not announced as failed; real failures still are.
	for _, f := range []struct {
		status store.RunStatus
		error  string
		kinds  []string
	}{
		{store.RunFailed, "abandoned: the caller's run context ended before claude finished (daemon shutdown or cancellation)", nil},
		{store.RunFailed, "claude exited with status 1", []string{"failed"}},
		{store.RunFailed, "timed out after 30m0s; the process subtree was killed", []string{"failed"}},
		{store.RunFailed, "abandoned", []string{"failed"}},
		{store.RunSucceeded, "", []string{"completed"}},
	} {
		r := record
		r.Run.Status, r.Run.Error, r.Run.SemanticOutcome = f.status, f.error, ""
		events, err := messaging.NoticeEvents("run_finishes", r)
		kinds := []string(nil)
		for _, e := range events {
			kinds = append(kinds, e.Kind)
		}
		if err != nil || strings.Join(kinds, ",") != strings.Join(f.kinds, ",") {
			t.Fatalf("finish %q events=%+v err=%v", f.error, events, err)
		}
	}
	record.Run.Status = store.RunFailed
	record.Run.RecoveryReason = `owner-stop:{"run_id":7,"at":"2026-10-10T18:00:00Z"}`
	events, err = messaging.NoticeEvents("run_finishes", record)
	if err != nil || events[0].Kind != "stopped" {
		t.Fatalf("stop=%+v err=%v", events, err)
	}
	abandonedStop := record
	abandonedStop.Run.Error = "abandoned: the caller's run context ended before claude finished (daemon shutdown or cancellation)"
	events, err = messaging.NoticeEvents("run_finishes", abandonedStop)
	if err != nil || len(events) != 1 || events[0].Kind != "stopped" {
		t.Fatalf("abandoned owner stop=%+v err=%v", events, err)
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
	if err != nil || len(events) != 1 || events[0].Key != "stop:park@"+store.FormatTime(at)+"@github:acme/repo#1" || !strings.Contains(messaging.NoticeText(events[0]), "stopped and needs your guidance") {
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
	// Usage is one short cost phrase, never raw token counts or statuses.
	cost, tokens := "0.9591808", int64(501004)
	usage := &report.MeterSummary{EstimatedUSD: &cost, KnownTokens: agent.TokenUsage{CacheRead: &tokens}, CollectionStatus: "complete", PricingStatus: "priced"}
	for _, kind := range []string{"completed", "failed", "stopped"} {
		text := messaging.NoticeText(messaging.NoticeEvent{Task: ref, Kind: kind, Usage: usage})
		if !strings.Contains(text, "≈ $0.96") || strings.Contains(text, "tokens") || strings.Contains(text, "cache") || strings.Contains(text, "Collection") || strings.Contains(text, "501004") {
			t.Fatal(text)
		}
	}
	subtotal := "1.5"
	if text := messaging.NoticeText(messaging.NoticeEvent{Task: ref, Kind: "completed", Usage: &report.MeterSummary{PricedSubtotalUSD: &subtotal}}); strings.Contains(text, "$") {
		t.Fatal(text)
	}
	text := messaging.NoticeText(messaging.NoticeEvent{Task: ref, Kind: "completed", Usage: &report.MeterSummary{CollectionStatus: "partial"}})
	if strings.Contains(text, "$") || strings.Contains(text, "partial") || strings.Count(text, "\n") != 1 {
		t.Fatal(text)
	}
	text = messaging.NoticeText(messaging.NoticeEvent{Task: ref, Kind: "started", Workflow: "implement", Title: "Authorize work"})
	if !strings.HasPrefix(text, "Implementation #1 (Authorize work) started.\nhttps://github.com/acme/repo/issues/1") {
		t.Fatal(text)
	}
	text = messaging.NoticeText(messaging.NoticeEvent{Task: ref, Kind: "question", Question: strings.Repeat("😀", 3000), QuestionLink: ref.IssueURL() + "#issuecomment-91"})
	if len(utf16.Encode([]rune(text))) > 1900 || !strings.Contains(text, "#issuecomment-91") || !strings.Contains(text, "…") {
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
		if strings.Contains(text, " started.") {
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
	if !strings.HasPrefix(finished.ReplyText, "Implementation #1 finished.\n") {
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
	if !strings.HasPrefix(started.ReplyText, "Validation #1 (Authorize work for acme/repo) started.\n") || strings.Contains(started.ReplyText, "github:") || issues.calls != 1 {
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
	if sender.target.Handle != "" || !sender.target.Notice {
		t.Fatal("notice has interaction handle or is not marked as a notice")
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

// A review-ready Bug analysis notice is a short heads-up with one link; the
// analysis itself is read on GitHub.
func TestNoticeBugAnalysisReviewIsShort(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 11, 22, 55, 0, 0, time.UTC)
	analysis := "Bug work is waiting at conversation.\n\n- **What goes wrong:** the notice pastes everything.\n" + strings.Repeat("- **Why:** a long cause. ", 400)
	r := store.NoticeRecord{ID: 9, TaskRef: "github:acme/repo#690", Workflow: "bug", Key: "review-key", Kind: "question", StopOrigin: "conversation", StopCause: "diagnosis_review", StopReason: analysis, Candidate: []byte(`{"cause":"diagnosis_review","reason":"What goes wrong"}`), CommentID: 93, State: "waiting", Published: true, Ready: true, At: at}
	events, err := messaging.NoticeEvents("owner_waits", r)
	if err != nil || len(events) != 1 || events[0].Kind != "waiting for you" || events[0].Question != "" {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	events[0].Title = "Discord notices are too long"
	text := messaging.NoticeText(events[0])
	if lines := strings.Split(text, "\n"); len(lines) > 3 {
		t.Fatalf("%d lines: %s", len(lines), text)
	}
	if !strings.Contains(text, "Bug analysis ready for your approval") || strings.Count(text, "https://") != 1 || !strings.Contains(text, "#issuecomment-93") {
		t.Fatal(text)
	}
	for _, body := range []string{"What goes wrong", "Why", "waiting at conversation"} {
		if strings.Contains(text, body) {
			t.Fatalf("analysis body %q in notice: %s", body, text)
		}
	}
	// A Bug analysis question names itself plainly.
	r.StopCause = "diagnosis_question"
	r.Candidate = []byte(`{"questions":[{"text":"Which release?"}]}`)
	events, err = messaging.NoticeEvents("owner_waits", r)
	if err != nil || len(events) != 1 || events[0].Asked != "Bug analysis question" || events[0].Question != "Which release?" {
		t.Fatalf("events=%+v err=%v", events, err)
	}
}

// A question wait shows only one short sentence of the first question.
func TestNoticeQuestionIsOneShortSentence(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 11, 22, 55, 0, 0, time.UTC)
	first := "Should the release target the stable channel? " + strings.Repeat("More context follows here. ", 40)
	r := store.NoticeRecord{ID: 10, TaskRef: "github:acme/repo#1", Workflow: "research", Key: "q", Kind: "question", CommentID: 94, State: "waiting", Published: true, Ready: true, At: at}
	r.Candidate = []byte(`{"questions":[{"text":"` + first + `"},{"text":"` + strings.Repeat("Second question text. ", 40) + `"}]}`)
	events, err := messaging.NoticeEvents("owner_waits", r)
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	text := messaging.NoticeText(events[0])
	lines := strings.Split(text, "\n")
	if len(lines) != 2 || !strings.HasSuffix(lines[0], "Should the release target the stable channel?") || !strings.Contains(lines[0], "has a question for you:") || strings.Count(text, "https://") != 1 {
		t.Fatalf("%q", lines)
	}
	long := strings.Repeat("x", 500)
	text = messaging.NoticeText(messaging.NoticeEvent{Kind: "waiting for you", Question: long})
	lines = strings.Split(text, "\n")
	if q := strings.Split(lines[0], ": ")[1]; len(utf16.Encode([]rune(q))) > 161 || !strings.HasSuffix(q, "….") {
		t.Fatalf("question line %q", q)
	}
}

// A successful run that only leads to an owner wait posts nothing itself; a
// failed one still posts.
func TestNoticeIgnoredRunFinishPostsNothing(t *testing.T) {
	t.Parallel()
	r := store.NoticeRecord{ID: 11, TaskRef: "github:acme/repo#690", Workflow: "bug", Ready: true, Ignore: true, Run: store.Run{ID: 11, Status: store.RunSucceeded}}
	if events, err := messaging.NoticeEvents("run_finishes", r); err != nil || len(events) != 0 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	r.Ignore = false
	r.Run = store.Run{ID: 11, Status: store.RunFailed, Error: "controlled failure"}
	if events, err := messaging.NoticeEvents("run_finishes", r); err != nil || len(events) != 1 || events[0].Kind != "failed" {
		t.Fatalf("events=%+v err=%v", events, err)
	}
}

func TestNoticeTitleAndCostBounds(t *testing.T) {
	t.Parallel()
	ref, _ := naming.ParseRef("github:acme/repo#1")
	for _, title := range []string{strings.Repeat("readable words ", 400), strings.Repeat("😀 café ", 400), strings.Repeat("x", 4000), "  normalize\tthese\nwords  "} {
		e := messaging.NoticeEvent{Task: ref, Kind: "waiting for you", Workflow: "plan", Title: title, Asked: "Plan review"}
		text := messaging.NoticeText(e)
		if len(utf16.Encode([]rune(text))) > 1900 || !strings.HasSuffix(text, ref.IssueURL()) || !strings.Contains(text, "is ready for your review.") || strings.Contains(text, "\t") || strings.Count(text, "\n") != 1 {
			t.Fatal(text)
		}
		if strings.HasPrefix(title, "readable") && !strings.Contains(text, "words…)") && !strings.Contains(text, "readable…)") {
			t.Fatal(text)
		}
		if strings.HasPrefix(title, "  ") && !strings.Contains(text, "(normalize these words)") {
			t.Fatal(text)
		}
	}
	for _, tc := range []struct{ value, want string }{{"0.79", "≈ $0.79"}, {"0", "≈ $0.00"}, {"0.9591808", "≈ $0.96"}} {
		text := messaging.NoticeText(messaging.NoticeEvent{Task: ref, Kind: "completed", Usage: &report.MeterSummary{EstimatedUSD: &tc.value}})
		if !strings.Contains(text, tc.want) || strings.Contains(text, "about") || strings.Contains(text, "review") {
			t.Fatal(text)
		}
	}
}

type publicNoticeIssues struct {
	issue github.Issue
	calls int
}

func (f *publicNoticeIssues) GetIssue(context.Context, github.Repo, int) (github.Issue, error) {
	f.calls++
	return f.issue, nil
}

func TestNoticeCollectorPublicIdentityEveryKind(t *testing.T) {
	t.Parallel()
	for _, provider := range []string{"github", "linear"} {
		t.Run(provider, func(t *testing.T) {
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
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			must(db.Migrate(ctx))
			number := 42
			key := "#42"
			url := "https://github.com/acme/repo/issues/42"
			issue := github.Issue{Title: "Readable task", HTMLURL: url}
			if provider == "linear" {
				number = 4503599627580010
				key = "MAR-12"
				url = "https://linear.app/acme/issue/MAR-12/readable-task"
				issue.QualifiedSnapshot = &ticket.Snapshot{DisplayKey: key, Title: "Readable task", BrowserURL: url}
			}
			reader := &publicNoticeIssues{issue: issue}
			repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: provider, Owner: "acme", Name: "repo", RemoteURL: "git@github.com:acme/repo.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
			must(err)
			session, err := db.GetOrCreateSession(ctx, repo, number)
			must(err)
			segment, err := db.GetOrCreateSegment(ctx, session.ID, 1, "session", "codex")
			must(err)
			at := time.Now().UTC()
			c := &messaging.NoticeCollector{DB: db, Transport: "test", Channel: "3", Issues: reader}
			must(c.Collect(ctx, at))
			keys := map[string]string{}
			for i, kind := range []string{"completed", "failed", "stopped"} {
				r, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "research", Agent: "codex", TriggerCommentID: int64(i + 1), StartedAt: at, LogPath: func(int) string { return "/logs/test" }})
				must(err)
				if i == 0 {
					keys["started"] = fmt.Sprintf("run:%d:start", r.ID)
				}
				outcome := store.RunOutcome{Status: store.RunSucceeded, FinishedAt: at}
				if kind != "completed" {
					outcome.Status = store.RunFailed
					outcome.Error = "controlled failure"
				}
				if kind == "stopped" {
					_, err = db.RequestOwnerStop(ctx, session.TaskRef)
					must(err)
				}
				_, err = db.FinishRun(ctx, r.ID, outcome)
				must(err)
				keys[kind] = fmt.Sprintf("run:%d:finish", r.ID)
			}
			definition, err := db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "notices", Mode: "implement", ProfileVersion: 1, Path: "research.dot", Digest: strings.Repeat("a", 64), CreatedAt: at})
			must(err)
			instance, err := db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: session.ID, DefinitionID: definition.ID, CurrentNode: "work", StartedAt: at})
			must(err)
			attempt, err := db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, Node: "work", StartedAt: at})
			must(err)
			must(db.FinishGraphNodeAttempt(ctx, attempt.ID, store.GraphSucceeded, "ask", at))
			must(db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: "public-wait", InstanceID: instance.ID, SourceAttemptID: attempt.ID, Node: "work", Kind: "question", Candidate: []byte(`{"questions":[{"text":"Which release?"}]}`)}))
			_, err = db.EnsureReportDelivery(ctx, store.ReportDelivery{Key: "public-wait", SessionID: session.ID, Task: session.TaskRef, Destination: "acme/repo#42", TriggerNamespace: "issue-comment", Purpose: "research-question", BotUserID: 99, Body: "Question", Marker: "public-marker"})
			must(err)
			_, err = db.MarkReportDeliverySent(ctx, "public-wait")
			must(err)
			_, err = db.ConfirmReportDelivery(ctx, "public-wait", 91)
			must(err)
			_, err = db.CompleteReportDelivery(ctx, "public-wait")
			must(err)
			keys["waiting for you"] = "wait:public-wait@" + store.FormatTime(at) + "@" + session.TaskRef
			must(c.Collect(ctx, at))
			if reader.calls != 1 {
				t.Fatalf("reads=%d", reader.calls)
			}
			for kind, eventKey := range keys {
				notice, err := db.ReadChatNotice(ctx, "test", eventKey)
				must(err)
				text := notice.ReplyText
				if notice.Command != kind || !strings.Contains(text, key+" (Readable task)") || !strings.Contains(text, url) || strings.Contains(text, "(research)") || strings.Contains(text, "Mode:") || strings.Count(text, "\n") != 1 {
					t.Fatalf("%s: %s", kind, text)
				}
				if provider == "linear" && (strings.Contains(text, "4503599627580010") || strings.Contains(text, "#issuecomment")) {
					t.Fatal(text)
				}
				if provider == "github" && kind == "waiting for you" && !strings.HasSuffix(text, url+"#issuecomment-91") {
					t.Fatal(text)
				}
			}
			must(c.Collect(ctx, at.Add(time.Second)))
			if reader.calls != 1 {
				t.Fatal("recollection reread admitted events")
			}
		})
	}
}

func TestNoticeGraphContinuationSettlesOnce(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"research", "plan"} {
		for _, ending := range []string{"completed", "question", "failed", "stopped"} {
			t.Run(mode+"/"+ending, func(t *testing.T) {
				ctx := t.Context()
				db, err := store.Open(ctx, filepath.Join(t.TempDir(), "graph.db"))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := db.Close(); err != nil {
						t.Error(err)
					}
				})
				must := func(err error) {
					t.Helper()
					if err != nil {
						t.Fatal(err)
					}
				}
				must(db.Migrate(ctx))
				repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "repo", RemoteURL: "git@github.com:acme/repo.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
				must(err)
				session, err := db.GetOrCreateSession(ctx, repo, 42)
				must(err)
				segment, err := db.GetOrCreateSegment(ctx, session.ID, 1, "session", "codex")
				must(err)
				if mode == "plan" {
					_, err = db.AdmitPlanPath(ctx, session.ID)
				} else {
					_, err = db.AdmitResearchPath(ctx, session.ID, "graph")
				}
				must(err)
				at := time.Now().UTC()
				definition, err := db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "notices", Mode: mode, ProfileVersion: 1, Path: mode + ".dot", Digest: strings.Repeat("a", 64), CreatedAt: at})
				must(err)
				launch := store.WorkflowLaunch{Mode: mode, SessionID: session.ID, TriggerID: 10, DefinitionID: definition.ID, Start: "work", ContentDigest: strings.Repeat("b", 64), TopologyDigest: strings.Repeat("c", 64), Files: []byte(`{"role":"eA=="}`)}
				admitted, err := db.AdmitWorkflowLaunch(ctx, launch)
				must(err)
				c := &messaging.NoticeCollector{DB: db, Transport: "test", Channel: "3"}
				must(c.Collect(ctx, at))
				allocate := func(trigger int64) (store.Run, store.GraphNodeAttempt) {
					t.Helper()
					r, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: mode, Agent: "codex", TriggerCommentID: trigger, StartedAt: at, LogPath: func(int) string { return "/logs/test" }})
					must(err)
					a, err := db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: admitted.Instance.ID, LaunchID: admitted.LaunchID, Node: "work", RunID: r.ID, StartedAt: at})
					must(err)
					must(db.FinishGraphNodeAttempt(ctx, a.ID, store.GraphSucceeded, "done", at))
					return r, a
				}
				finish := func(r store.Run, status store.RunStatus) {
					t.Helper()
					_, err := db.FinishRun(ctx, r.ID, store.RunOutcome{Status: status, FinishedAt: at})
					must(err)
				}
				absent := func(r store.Run) {
					t.Helper()
					if _, err := db.ReadChatNotice(ctx, "test", fmt.Sprintf("run:%d:finish", r.ID)); !errors.Is(err, store.ErrNotFound) {
						t.Fatalf("intermediate completion: %v", err)
					}
				}
				first, _ := allocate(10)
				dispatch, err := db.ParkCapturedGraphInstanceAfter(ctx, admitted.Instance.ID, "work", at, at, 0, admitted.LaunchID)
				must(err)
				finish(first, store.RunSucceeded)
				must(c.Collect(ctx, at))
				absent(first)
				_, reserved, err := db.ReserveGraphDispatch(ctx, dispatch.ID, at)
				must(err)
				if !reserved {
					t.Fatal("not reserved")
				}
				second, source := allocate(dispatch.TriggerID())
				// Collection while the successor is running must not revive its predecessor.
				must(c.Collect(ctx, at))
				absent(first)
				if ending == "question" {
					must(db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: "settled", InstanceID: admitted.Instance.ID, SourceAttemptID: source.ID, Node: "work", Kind: "question", Candidate: []byte(`{"questions":[{"text":"Which release?"}]}`)}))
				}
				status := store.RunSucceeded
				if ending == "failed" || ending == "stopped" {
					status = store.RunFailed
				}
				if ending == "stopped" {
					_, err = db.RequestOwnerStop(ctx, session.TaskRef)
					must(err)
				}
				finish(second, status)
				must(db.FinishGraphDispatch(ctx, dispatch.ID, at))
				must(c.Collect(ctx, at))
				absent(first)
				if ending == "question" {
					absent(second)
					_, err = db.EnsureReportDelivery(ctx, store.ReportDelivery{Key: "settled", SessionID: session.ID, RunID: second.ID, TriggerID: second.TriggerCommentID, Task: session.TaskRef, Destination: "acme/repo#42", TriggerNamespace: "issue-comment", Purpose: mode + "-question", BotUserID: 99, Body: "Question", Marker: "marker"})
					must(err)
					_, err = db.MarkReportDeliverySent(ctx, "settled")
					must(err)
					_, err = db.ConfirmReportDelivery(ctx, "settled", 91)
					must(err)
					_, err = db.CompleteReportDelivery(ctx, "settled")
					must(err)
				}
				// Recreate the collector to exercise durable positions, then collect twice.
				c = &messaging.NoticeCollector{DB: db, Transport: "test", Channel: "3"}
				must(c.Collect(ctx, at))
				must(c.Collect(ctx, at))
				absent(first)
				key := fmt.Sprintf("run:%d:finish", second.ID)
				kind := ending
				if ending == "question" {
					key = "wait:settled@" + store.FormatTime(at) + "@" + session.TaskRef
					kind = "waiting for you"
					absent(second)
				}
				notice, err := db.ReadChatNotice(ctx, "test", key)
				must(err)
				if notice.Command != kind {
					t.Fatalf("notice=%+v", notice)
				}
				if ending == "completed" {
					// Fresh owner input in the same graph/mode has its own launch and outcome.
					launch.TriggerID = 11
					admitted, err = db.AdmitWorkflowLaunch(ctx, launch)
					must(err)
					later, _ := allocate(11)
					finish(later, store.RunSucceeded)
					must(c.Collect(ctx, at))
					n, err := db.ReadChatNotice(ctx, "test", fmt.Sprintf("run:%d:finish", later.ID))
					must(err)
					if n.Command != "completed" {
						t.Fatal(n)
					}
				}
			})
		}
	}
}
