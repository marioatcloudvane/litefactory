package turn

import (
	"context"
	"errors"
	"github.com/cloudvane/litefactory/internal/testutil"
	"html"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestImplementStatusActivityIsCurrentAndAdvisory(t *testing.T) {
	f := newImplementFixture(t, implementStories(5, nil)...)
	rows := []store.ImplementStory{
		{Key: "story-001", State: store.ImplementRunning, Attempts: 1, StateAt: implementClock},
		{Key: "story-002", State: store.ImplementMerged},
		{Key: "story-003", State: store.ImplementDone},
		{Key: "story-004", State: store.ImplementParked},
		{Key: "story-005", State: store.ImplementWaiting},
	}
	var progress []store.ImplementProgressReport
	for _, row := range rows {
		for i, kind := range []string{"implementation", "testing", "repair"} {
			progress = append(progress, store.ImplementProgressReport{ID: int64(i + 1), Parent: implementParent.String(), Generation: 1, StoryKey: row.Key, StoryAttempts: row.Attempts, Kind: kind, Message: "done but only activity", RecordedAt: implementClock.Add(time.Minute)})
		}
	}
	progress = append(progress,
		store.ImplementProgressReport{ID: 100, Parent: implementParent.String(), Generation: 1, StoryKey: "story-001", StoryAttempts: 0, Kind: "testing", Message: "old attempt", RecordedAt: implementClock.Add(time.Hour)},
		store.ImplementProgressReport{ID: 101, Parent: implementParent.String(), Generation: 1, StoryKey: "story-001", StoryAttempts: 1, Kind: "testing", Message: "before resume", RecordedAt: implementClock.Add(-time.Minute)},
		store.ImplementProgressReport{ID: 102, Parent: implementParent.String(), Generation: 1, StoryKey: "story-001", StoryAttempts: 1, Kind: "implementation", Message: "newest activity", RecordedAt: implementClock.Add(2 * time.Minute)},
	)
	body, counts := renderImplementHandoffProgress(f.auth, rows, progress)
	_, without := renderImplementHandoff(f.auth, rows)
	if counts != without || counts != (implementHandoffCounts{1, 1, 3}) {
		t.Fatalf("activity changed counts: %+v %+v", counts, without)
	}
	if strings.Count(body, "Latest activity (advisory, not completion)") != 1 {
		t.Fatal(body)
	}
	for _, unwanted := range []string{"old attempt", "before resume", "implementation at 2026-03-04T05:07:07Z"} {
		if strings.Contains(html.UnescapeString(body), unwanted) {
			t.Errorf("unexpected %q in %s", unwanted, body)
		}
	}
	for _, want := range []string{"being built now.", "newest activity", "implementation at 2026-03-04T05:08:07Z, attempt 2", "| Implementing |"} {
		if !strings.Contains(html.UnescapeString(body), want) {
			t.Errorf("missing %q in %s", want, body)
		}
	}

}

func TestImplementStatusProgressSafety(t *testing.T) {
	hostile := "\n# heading\r[link](https://example.com) @owner <!-- hidden --> & `code`\t" + strings.Repeat("界", 400)
	safe := safeImplementProgress(hostile)
	if strings.ContainsAny(safe, "\n\r\t@<>[]()`") || strings.Contains(safe, "https://") || !utf8.ValidString(safe) {
		t.Fatal(safe)
	}
	if strings.Count(safe, "界") >= 300 {
		t.Fatal("activity was not clipped")
	}
	if !strings.Contains(safe, "&#64;owner") {
		t.Fatal("mention was not escaped")
	}
}

func TestImplementStatusRedactsBeforeClippingAndEscaping(t *testing.T) {
	f := newImplementFixture(t, implementStories(1, nil)...)
	f.ready()
	secret := "secret-with-@punctuation-value"
	f.bindings.Redactor = redact.New(redact.Options{Environ: []string{"API_TOKEN=" + secret}})
	f.bindings.ProgressRead = func(context.Context, string, int64) ([]store.ImplementProgressReport, error) {
		return []store.ImplementProgressReport{{Parent: implementParent.String(), Generation: 1, StoryKey: "story-001", Kind: "testing", Message: strings.Repeat("x", 285) + secret, RecordedAt: implementClock}}, nil
	}
	_, _, err := refreshImplementHandoff(t.Context(), f.bindings, Options{}, f.req())
	if err != nil {
		t.Fatal(err)
	}
	body := f.forge.bodies(1)[0]
	if strings.Contains(html.UnescapeString(body), "secret-with") || strings.Contains(html.UnescapeString(body), "punctuation-value") {
		t.Fatal("secret leaked: " + body)
	}
	if !strings.Contains(html.UnescapeString(body), "Latest activity") {
		t.Fatal(body)
	}
	if strings.Count(body, report.ImplementHandoffMarker(implementParent)) != 1 || len(body) > implementHandoffLimit {
		t.Fatal("invalid public body")
	}
}

func TestImplementStatusMissingProgressStillRendersLedger(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "failed"}[fail], func(t *testing.T) {
			f := newImplementFixture(t, implementStories(1, nil)...)
			f.ready()
			f.bindings.ProgressRead = func(context.Context, string, int64) ([]store.ImplementProgressReport, error) {
				if fail {
					return nil, errors.New("progress unavailable")
				}
				return nil, nil
			}
			_, _, err := refreshImplementHandoff(t.Context(), f.bindings, Options{}, f.req())
			if err != nil {
				t.Fatal(err)
			}
			body := f.forge.bodies(1)[0]
			if !strings.Contains(html.UnescapeString(body), "being built now.") || strings.Contains(html.UnescapeString(body), "Latest activity") {
				t.Fatal(body)
			}
		})
	}
}

func TestImplementStatusRestartDeletionDuplicatesAndConcurrentRefresh(t *testing.T) {
	f := newImplementFixture(t, implementStories(2, nil)...)
	f.ready()
	refresh := func() {
		t.Helper()
		if _, _, err := refreshImplementHandoff(t.Context(), f.bindings, Options{}, f.req()); err != nil {
			t.Fatal(err)
		}
	}
	refresh()
	first := f.forge.comments[1][0].ID
	// A fresh binding has no cached identity. It still edits the same comment.
	f.drive("story-001", store.ImplementDone, implementClock)
	fresh := f.bindings
	if _, _, err := refreshImplementHandoff(t.Context(), fresh, Options{}, f.req()); err != nil {
		t.Fatal(err)
	}
	if f.forge.posts != 1 || f.forge.edits != 1 || f.forge.comments[1][0].ID != first {
		t.Fatal("restart duplicated the comment")
	}
	refresh()
	if f.forge.edits != 1 {
		t.Fatal("unchanged body edited")
	}
	f.forge.comments[1] = nil
	refresh()
	if f.forge.posts != 2 {
		t.Fatal("deleted comment not recreated")
	}
	duplicate := github.Comment{ID: 999, Body: report.ImplementHandoffMarker(implementParent) + "\nduplicate"}
	f.forge.comments[1] = append(f.forge.comments[1], duplicate)
	f.drive("story-001", store.ImplementMerged, implementClock)
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for range 20 {
		wg.Go(func() { _, _, err := refreshImplementHandoff(t.Context(), f.bindings, Options{}, f.req()); errs <- err })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if f.forge.posts != 2 || f.forge.edits != 2 || f.forge.comments[1][1].Body != duplicate.Body {
		t.Fatalf("posts=%d edits=%d comments=%+v", f.forge.posts, f.forge.edits, f.forge.comments[1])
	}
}

type failingStatusForge struct {
	*implementForgeFake
	fail bool
}

func (f *failingStatusForge) EditComment(ctx context.Context, repo github.Repo, id int64, body redact.Body) error {
	if f.fail {
		return errors.New("forge unavailable")
	}
	return f.implementForgeFake.EditComment(ctx, repo, id, body)
}

func TestImplementStatusFailureCatchUpAndFinalError(t *testing.T) {
	f := newImplementFixture(t, implementStories(1, nil)...)
	f.ready()
	if _, _, err := refreshImplementHandoff(t.Context(), f.bindings, Options{}, f.req()); err != nil {
		t.Fatal(err)
	}
	forge := &failingStatusForge{implementForgeFake: f.forge, fail: true}
	f.bindings.Forge = forge
	status, err := NewImplementStatusService(t.Context(), ImplementStatusOptions{Now: func() time.Time { return implementClock }, Wait: func(context.Context, time.Duration) error { return nil }, Refresh: func(ctx context.Context, _ string, _ int64) (bool, error) {
		_, changed, err := refreshImplementHandoff(ctx, f.bindings, Options{}, f.req())
		return changed, err
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(status.Close)
	f.bindings.Status = status
	f.drive("story-001", store.ImplementDone, implementClock)
	// A coordinator pass preserves its outcome despite a failing live edit.
	res, _, err := f.merge()
	if err != nil || res.Outcome != "merged" {
		t.Fatalf("merge=%+v error=%v", res, err)
	}
	if _, err := (ImplementHandoffActivity{f.bindings}).Execute(t.Context(), Options{}, f.req()); err == nil {
		t.Fatal("final handoff hid the error")
	}
	forge.fail = false
	if err := status.RefreshImplementStatus(t.Context(), implementParent.String(), 1, ImplementStatusState); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.forge.bodies(1)[0], "merged as commit") {
		t.Fatal("next refresh did not catch up")
	}
}

func TestImplementStatusAlreadyComplete(t *testing.T) {
	f := newImplementFixture(t, implementStories(2, nil)...)
	f.auth.Stories[0].AlreadyComplete = true
	f.auth.Stories[1].Dependencies = []string{"story-001"}
	body, counts := renderImplementHandoff(f.auth, nil)
	if counts.merged != 1 || counts.waiting != 1 || !strings.Contains(html.UnescapeString(body), "already complete before this run.") || strings.Contains(html.UnescapeString(body), "unknown story") {
		t.Fatal(body)
	}
	// A ledger row remains authoritative even if the approved revision is complete.
	body, counts = renderImplementHandoff(f.auth, []store.ImplementStory{{Key: "story-001", State: store.ImplementRunning}})
	if counts.merged != 0 || !strings.Contains(html.UnescapeString(body), "being built now.") {
		t.Fatal(body)
	}
}

func receiveStatus[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(testutil.Wait):
		t.Fatal("status operation timed out")
		var zero T
		return zero
	}
}

func TestImplementStatusCoalescesTrailingAndStateSkipsGap(t *testing.T) {
	var now atomic.Int64
	now.Store(implementClock.UnixNano())
	var calls, active, maxActive atomic.Int64
	entered := make(chan int64, 10)
	release := make(chan struct{})
	waits := make(chan time.Duration, 10)
	ticks := make(chan struct{}, 10)
	status, err := NewImplementStatusService(t.Context(), ImplementStatusOptions{
		Now: func() time.Time { return time.Unix(0, now.Load()) },
		Wait: func(ctx context.Context, d time.Duration) error {
			waits <- d
			select {
			case <-ticks:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
		Refresh: func(ctx context.Context, _ string, _ int64) (bool, error) {
			n := active.Add(1)
			for old := maxActive.Load(); n > old && !maxActive.CompareAndSwap(old, n); old = maxActive.Load() {
			}
			defer active.Add(-1)
			call := calls.Add(1)
			entered <- call
			if call == 1 {
				select {
				case <-release:
				case <-ctx.Done():
					return false, ctx.Err()
				}
			}
			return true, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(status.Close)
	if err := status.RefreshImplementStatus(t.Context(), implementParent.String(), 1, ImplementStatusProgress); err != nil {
		t.Fatal(err)
	}
	if receiveStatus(t, entered) != 1 {
		t.Fatal("first progress not refreshed")
	}
	for range 100 {
		if err := status.RefreshImplementStatus(t.Context(), implementParent.String(), 1, ImplementStatusProgress); err != nil {
			t.Fatal(err)
		}
	}
	close(release)
	if gap := receiveStatus(t, waits); gap != DefaultImplementStatusGap {
		t.Fatalf("gap=%s", gap)
	}
	// State refresh proceeds while progress is waiting, and shares the writer.
	if err := status.RefreshImplementStatus(t.Context(), implementParent.String(), 1, ImplementStatusState); err != nil {
		t.Fatal(err)
	}
	if receiveStatus(t, entered) != 2 {
		t.Fatal("state waited for progress gap")
	}
	now.Add(int64(DefaultImplementStatusGap))
	ticks <- struct{}{}
	if receiveStatus(t, entered) != 3 {
		t.Fatal("missing trailing refresh")
	}
	status.Close()
	if calls.Load() != 3 || maxActive.Load() != 1 {
		t.Fatalf("calls=%d concurrent=%d", calls.Load(), maxActive.Load())
	}
}

type recordingStatus struct {
	mu         sync.Mutex
	calls      int
	trigger    ImplementStatusTrigger
	parent     string
	generation int64
}

func (s *recordingStatus) RefreshImplementStatus(_ context.Context, p string, g int64, tr ImplementStatusTrigger) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.parent = p
	s.generation = g
	s.trigger = tr
	return errors.New("display failed")
}
func TestImplementStatusAcceptedProgressDoesNotFailOrWakeGraph(t *testing.T) {
	activity, client, db, _, _ := durableActivityFixture(t)
	status := &recordingStatus{}
	activity.status = status
	before, err := db.ListImplementStories(t.Context(), "github:example/results#507", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, seq := range []int{1, 1, 2} {
		got := activityResponse(t, mcpToolCall(t, client, "activity_update", durableActivityArgs(seq, "testing", "done")))
		if got.Disposition == "rejected" {
			t.Fatal(got)
		}
	}
	status.mu.Lock()
	defer status.mu.Unlock()
	if status.calls != 2 || status.trigger != ImplementStatusProgress || status.parent != "github:example/results#507" || status.generation != 1 {
		t.Fatalf("refresh=%+v", status)
	}
	after, err := db.ListImplementStories(t.Context(), "github:example/results#507", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 || after[0].State != before[0].State || after[0].Revision != before[0].Revision {
		t.Fatal("advisory reports changed factory status")
	}
}

func TestImplementStatusWorkerChangesRemainSuccessfulOnDisplayFailure(t *testing.T) {
	f := newImplementFixture(t, implementStories(1, nil)...)
	f.ready()
	var states []store.ImplementStory
	status, err := NewImplementStatusService(t.Context(), ImplementStatusOptions{
		Now: func() time.Time { return implementClock }, Wait: func(context.Context, time.Duration) error { return nil },
		Refresh: func(ctx context.Context, _ string, _ int64) (bool, error) {
			row, err := f.db.GetImplementStory(ctx, implementParent.String(), 1, "story-001")
			if err != nil {
				return false, err
			}
			states = append(states, row)
			return false, errors.New("forge failed")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(status.Close)
	opts := Options{DB: f.db, MCP: &MCPRuntime{ImplementStatus: status}}
	row := f.row("story-001")
	rt := &implementRuntime{parentRef: implementParent, generation: 1, worker: &row, opts: opts, save: func(context.Context) (Result, error) { return Result{}, errors.New("save failed") }}
	res, err := rt.storyDone(t.Context(), opts, ActivityRequest{})
	if err != nil || res.Outcome != "retry" || len(states) != 1 || states[0].Attempts != 1 {
		t.Fatalf("failed attempt: %+v %v states=%+v", res, err, states)
	}
	rt.save = func(context.Context) (Result, error) { return Result{}, nil }
	res, err = rt.storyDone(t.Context(), opts, ActivityRequest{})
	if err != nil || res.Outcome != "done" || len(states) != 2 || states[1].State != store.ImplementDone {
		t.Fatalf("done: %+v %v states=%+v", res, err, states)
	}
}

func TestImplementStatusProgressBodyBudget(t *testing.T) {
	f := newImplementFixture(t, implementStories(1000, nil)...)
	var rows []store.ImplementStory
	var progress []store.ImplementProgressReport
	for _, story := range f.auth.Stories {
		rows = append(rows, store.ImplementStory{Key: story.Key, State: store.ImplementRunning})
		for _, kind := range []string{"implementation", "testing", "repair"} {
			progress = append(progress, store.ImplementProgressReport{Parent: implementParent.String(), Generation: 1, StoryKey: story.Key, Kind: kind, RecordedAt: implementClock, Message: strings.Repeat("@", 2048)})
		}
	}
	body, _ := renderImplementHandoffProgress(f.auth, rows, progress)
	if len(body) <= implementHandoffBudget || strings.Contains(body, " more.") || strings.Count(body, "| Implementing |") != 1000 {
		t.Fatalf("body length=%d", len(body))
	}
}

func TestImplementStatusReadyStartsLiveCommentAndRebuildsLatestProgress(t *testing.T) {
	f := newImplementFixture(t, implementStories(2, nil)...)
	var latest []store.ImplementProgressReport
	f.bindings.ProgressRead = func(context.Context, string, int64) ([]store.ImplementProgressReport, error) { return latest, nil }
	status, err := NewImplementStatusService(t.Context(), ImplementStatusOptions{
		Now: func() time.Time { return implementClock }, Wait: func(context.Context, time.Duration) error { return nil },
		Refresh: func(ctx context.Context, _ string, _ int64) (bool, error) {
			_, changed, err := refreshImplementHandoff(ctx, f.bindings, Options{}, f.req())
			return changed, err
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(status.Close)
	f.bindings.Status = status
	f.ready()
	if f.forge.posts != 1 || !strings.Contains(html.UnescapeString(f.forge.bodies(1)[0]), "being built now.") {
		t.Fatal("ready did not post a live comment")
	}
	latest = []store.ImplementProgressReport{{ID: 1, Parent: implementParent.String(), Generation: 1, StoryKey: "story-001", Kind: "testing", RecordedAt: implementClock, Message: "done"}}
	if err := status.RefreshImplementStatus(t.Context(), implementParent.String(), 1, ImplementStatusState); err != nil {
		t.Fatal(err)
	}
	body := f.forge.bodies(1)[0]
	if f.forge.posts != 1 || f.forge.edits != 1 || !strings.Contains(html.UnescapeString(body), "Latest activity") || !strings.Contains(html.UnescapeString(body), "being built now.") {
		t.Fatal(body)
	}
	latest[0].ID = 2
	latest[0].Message = "newer report"
	if err := status.RefreshImplementStatus(t.Context(), implementParent.String(), 1, ImplementStatusState); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.forge.bodies(1)[0], "newer report") || f.forge.edits != 2 {
		t.Fatal("refresh did not rebuild from latest reports")
	}
}

func TestImplementStatusProductionBindingIncludesApprovedWithoutScheduling(t *testing.T) {
	f, _, _ := assembledImplementFixture(t, nil)
	parent := seedImplementParent(t, f, store.ManagedStory{Key: "story-001", IssueNumber: 401, IssueID: 4401}, store.ManagedStory{Key: "story-002", IssueNumber: 402, IssueID: 4402})
	b, err := implementStatusBindings(f.opts, parent, 1)
	mustProgress(t, err)
	auth, err := b.bind(t.Context(), f.opts, ActivityRequest{Mode: "implement"})
	mustProgress(t, err)
	if len(auth.Stories) != 2 {
		t.Fatalf("authority=%+v", auth)
	}
	rows, err := f.db.ListImplementStories(t.Context(), parent, 1)
	mustProgress(t, err)
	if len(rows) != 0 {
		t.Fatal("status binding created ledger rows")
	}
	body, _ := renderImplementHandoff(auth, rows)
	if !strings.Contains(html.UnescapeString(body), "story-001") || !strings.Contains(html.UnescapeString(body), "story-002") {
		t.Fatal(body)
	}
	_, err = RefreshImplementParentStatus(t.Context(), f.opts, parent, 2)
	if err == nil {
		t.Fatal("accepted a stale generation")
	}
}

func TestImplementHandoffAllStatesAndLatestKind(t *testing.T) {
	f := newImplementFixture(t, implementStories(10, map[string][]string{"story-009": {"story-006"}})...)
	f.auth.Stories[9].AlreadyComplete = true
	rows := []store.ImplementStory{
		{Key: "story-001", State: store.ImplementWaiting},
		{Key: "story-002", State: store.ImplementRunning, StateAt: implementClock},
		{Key: "story-003", State: store.ImplementRunning, StateAt: implementClock},
		{Key: "story-004", State: store.ImplementDone},
		{Key: "story-005", State: store.ImplementMerged},
		{Key: "story-006", State: store.ImplementParked},
		{Key: "story-007", State: store.ImplementParked},
		{Key: "story-008", State: store.ImplementParked, ParkOrigin: store.ImplementOriginWorker, Attempts: store.MaxImplementAttempts},
	}
	var progress []store.ImplementProgressReport
	for _, row := range rows {
		progress = append(progress, store.ImplementProgressReport{ID: 1, Parent: f.auth.Parent.String(), Generation: 1, StoryKey: row.Key, Kind: "testing", RecordedAt: implementClock})
	}
	progress = append(progress, store.ImplementProgressReport{ID: 2, Parent: f.auth.Parent.String(), Generation: 1, StoryKey: "story-002", Kind: "repair", RecordedAt: implementClock})
	progress = append(progress, []store.ImplementProgressReport{
		{ID: 100, Parent: "foreign", Generation: 1, StoryKey: "story-003", Kind: "repair", RecordedAt: implementClock},
		{ID: 101, Parent: f.auth.Parent.String(), Generation: 2, StoryKey: "story-003", Kind: "repair", RecordedAt: implementClock},
	}...)
	notes := map[string]store.ReportDelivery{"story-007": {CommentID: 123, Body: "**Story parked**\n\nWorker response: blocked\n"}}
	body, counts := renderImplementHandoffNotes(f.auth, rows, progress, notes)
	body = html.UnescapeString(body)
	expected := []string{"Waiting", "Implementing", "Testing", "Merging", "Finished", "Parked", "Blocked", "Failed", "Blocked", "Finished"}
	var storyRows []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "| story-") {
			storyRows = append(storyRows, line)
		}
	}
	if len(storyRows) != len(expected) {
		t.Fatal(body)
	}
	for i, want := range expected {
		if !strings.Contains(storyRows[i], "| "+want+" |") {
			t.Fatalf("story %d: %s", i, storyRows[i])
		}
	}
	if counts != (implementHandoffCounts{2, 3, 5}) {
		t.Fatal(counts)
	}
}

func TestImplementHandoffTwelveEscapedRowsKeepLinksAndFooter(t *testing.T) {
	f := newImplementFixture(t, implementStories(12, nil)...)
	var rows []store.ImplementStory
	notes := map[string]store.ReportDelivery{}
	for i := range f.auth.Stories {
		story := &f.auth.Stories[i]
		story.Name = strings.Repeat("|界@", 100)
		rows = append(rows, store.ImplementStory{Key: story.Key, State: store.ImplementParked, Reason: strings.Repeat("|@界", 2000)})
		notes[story.Key] = store.ReportDelivery{CommentID: int64(100 + i)}
	}
	for _, auto := range []bool{false, true} {
		body, _ := renderImplementHandoffNotes(f.auth, rows, nil, notes)
		body += implementValidateNextStep(auto)
		if len(body) > implementHandoffBudget || strings.Count(body, "[Full answer]") != 12 || strings.Count(body, "| Parked |") != 12 || !utf8.ValidString(body) {
			t.Fatal("table lost rows, links or exceeded budget")
		}
		if !strings.HasSuffix(body, implementValidateNextStep(auto)) || strings.Contains(body, "more.") {
			t.Fatal(body)
		}
		decoded := html.UnescapeString(body)
		last := -1
		for _, story := range f.auth.Stories {
			at := strings.Index(decoded, "| "+story.Key+" ")
			if at <= last || strings.Count(decoded, "| "+story.Key+" ") != 1 {
				t.Fatal("story order or uniqueness changed")
			}
			last = at
		}
	}
}
