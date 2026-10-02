package turn

import (
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
)

// The thread the tests in this file seed on the task, oldest first. The ids are
// below the triggering comment's, because the forge hands them out in order and
// [store.DB.ListComments] returns them in that order.
const (
	historyOwnerComment      = int64(7001)
	historyThirdPartyComment = int64(7002)
	historySelfComment       = int64(7003)
)

// The bodies are distinct sentences rather than filler so that a test can say
// which one it found in a prompt without matching on prose that appears
// elsewhere in it.
const (
	historyOwnerBody      = "/spike see whether the widget can be blue"
	historyThirdPartyBody = "it was green in the mockups"
	historySelfBody       = "Spiked it: blue is a one-line change."
)

// seedComment records a comment on the fixture's task the way the daemon's
// filter would have, with the forge's own timestamp.
func (f *fixture) seedComment(
	forgeCommentID int64,
	handle, body string,
	classification store.Classification,
	createdAt time.Time,
) store.Comment {
	f.t.Helper()

	comment, err := f.db.RecordComment(f.t.Context(), store.Comment{
		SessionID:      f.session.ID,
		ForgeCommentID: forgeCommentID,
		AuthorHandle:   handle,
		Body:           body,
		CreatedAt:      createdAt,
		Classification: classification,
	})
	if err != nil {
		f.t.Fatalf("seed comment %d: %v", forgeCommentID, err)
	}
	return comment
}

// seedThread records the three earlier comments plus the one the turn under
// test is triggered by. The triggering comment is seeded deliberately: the
// daemon records every comment it sees before it acts on any of them, so the
// row is always there by the time a turn reads the thread back.
func (f *fixture) seedThread() {
	f.t.Helper()

	base := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	f.seedComment(historyOwnerComment, "marioatcloudvane", historyOwnerBody,
		store.ClassificationInstruction, base)
	f.seedComment(historyThirdPartyComment, "carol", historyThirdPartyBody,
		store.ClassificationThirdParty, base.Add(time.Hour))
	f.seedComment(historySelfComment, "litefactory-bot", historySelfBody,
		store.ClassificationSelf, base.Add(2*time.Hour))
	f.seedComment(testComment, "marioatcloudvane", f.request().Trigger.Body,
		store.ClassificationInstruction, base.Add(3*time.Hour))
}

// TestColdRunCarriesTheThreadAsHistory is AC-SR-3.1, AC-SR-3.2 and AC-SR-3.4
// through the turn: the mapping prepare does, and the block harness renders
// from it, asserted where the prompt is actually delivered.
func TestColdRunCarriesTheThreadAsHistory(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := recordingRunner(func(req harness.Request) (*harness.RunResult, error) {
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	f.seedThread()

	if result := f.execute(f.request()); !result.Succeeded() {
		t.Fatalf("turn did not succeed: %+v\nlogs:\n%s", result, f.logs)
	}
	if got := runner.requests[0].Path; got != harness.PathCold {
		t.Fatalf("run path = %s, want %s", got, harness.PathCold)
	}

	prompt := runner.requests[0].Prompt
	for _, want := range []string{
		"a condensed record of earlier comments on issue #123",
		"[old_owner] @marioatcloudvane at 2026-08-20T09:00:00Z:\n" + historyOwnerBody,
		"[old_third_party] @carol at 2026-08-20T10:00:00Z:\n" + historyThirdPartyBody,
		"[litefactory] @litefactory-bot at 2026-08-20T11:00:00Z:\n" + historySelfBody,
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the cold prompt is missing %q:\n%s", want, prompt)
		}
	}

	// AC-SR-3.4. The comment that triggered this run is a note of its own,
	// verbatim. Repeating it as history would leave two copies of the live
	// request in one prompt, one of them labelled as something that already
	// happened.
	trigger := f.request().Trigger.Body
	if got := strings.Count(prompt, trigger); got != 1 {
		t.Errorf("the triggering comment appears %d times, want once — in its own note:\n%s", got, prompt)
	}
	if strings.Contains(prompt, "[old_owner] @marioatcloudvane at 2026-08-20T12:00:00Z") {
		t.Errorf("the triggering comment was duplicated into the history block:\n%s", prompt)
	}

	// AC-3.11 still holds: the document records what was sent.
	if meta := f.readMeta(1); meta.Prompt != prompt {
		t.Errorf("the metadata document does not record the prompt that was sent:\n%s", meta.Prompt)
	}
}

// A task whose only comment is the one that triggered the run has no thread to
// replay, and gets the prompt litefactory sent before there was a history block
// at all.
func TestColdRunWithoutAThreadCarriesNoHistory(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := recordingRunner(func(req harness.Request) (*harness.RunResult, error) {
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	f.seedComment(testComment, "marioatcloudvane", f.request().Trigger.Body,
		store.ClassificationInstruction, time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC))

	f.execute(f.request())

	prompt := runner.requests[0].Prompt
	if strings.Contains(prompt, "a condensed record") {
		t.Errorf("a task with no earlier comments still got a history block:\n%s", prompt)
	}
	if got := strings.Count(prompt, "<note>"); got != 2 {
		t.Errorf("got %d <note> elements, want 2 (the issue and the triggering comment)", got)
	}
}

// TestResumedRunCarriesNoHistory is AC-SR-3.7 through the turn.
//
// The conversation being continued already contains the thread, one message at
// a time. Sending it again as a block would spend the context the resume exists
// to preserve.
func TestResumedRunCarriesNoHistory(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := recordingRunner(func(req harness.Request) (*harness.RunResult, error) {
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	f.seedRun(store.FirstSegmentOrdinal, storedClaudeSessionID, seededComment, "implement", store.RunReported)
	f.seedThread()

	f.execute(f.request())

	prompt := runner.requests[0].Prompt
	if got := runner.requests[0].Path; got != harness.PathResume {
		t.Fatalf("run path = %s, want %s", got, harness.PathResume)
	}
	for _, unwanted := range []string{"a condensed record", historyOwnerBody, historyThirdPartyBody, historySelfBody} {
		if strings.Contains(prompt, unwanted) {
			t.Errorf("the resumed prompt replays %q, which the conversation has already seen:\n%s",
				unwanted, prompt)
		}
	}
}

// TestFallbackColdRerunCarriesTheHistory is the seam between the resume
// fallback and the history block, and it is the second half of AC-SR-2.8:
// "reruns once, cold, with the compacted history".
//
// The rerun is the case the history exists for. The conversation is gone —
// that is why it is being rerun — so the thread is the only context the cold
// prompt has left, and a fallback that silently dropped it would be a fallback
// to a run that knows nothing.
func TestFallbackColdRerunCarriesTheHistory(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := recordingRunner(func(req harness.Request) (*harness.RunResult, error) {
		if req.Path == harness.PathResume {
			return refused(req, f.paths.Home())
		}
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)
	f.seedRun(store.FirstSegmentOrdinal, storedClaudeSessionID, seededComment, "implement", store.RunReported)
	f.seedThread()

	if result := f.execute(f.request()); !result.Succeeded() {
		t.Fatalf("the cold rerun did not succeed: %+v\nlogs:\n%s", result, f.logs)
	}
	if runner.callCount() != 2 {
		t.Fatalf("claude ran %d times, want exactly 2: the resume and one cold rerun", runner.callCount())
	}

	first, second := runner.requests[0], runner.requests[1]
	if second.Path != harness.PathColdAfterResumeRefused {
		t.Fatalf("the rerun took the %s path, want %s", second.Path, harness.PathColdAfterResumeRefused)
	}
	if strings.Contains(first.Prompt, "a condensed record") {
		t.Errorf("the refused resume carried a history block:\n%s", first.Prompt)
	}
	for _, want := range []string{
		"a condensed record of earlier comments on issue #123",
		"[old_owner] @marioatcloudvane at 2026-08-20T09:00:00Z:\n" + historyOwnerBody,
		"[old_third_party] @carol at 2026-08-20T10:00:00Z:\n" + historyThirdPartyBody,
		"[litefactory] @litefactory-bot at 2026-08-20T11:00:00Z:\n" + historySelfBody,
	} {
		if !strings.Contains(second.Prompt, want) {
			t.Errorf("the cold rerun's prompt is missing %q:\n%s", want, second.Prompt)
		}
	}
	if got := strings.Count(second.Prompt, f.request().Trigger.Body); got != 1 {
		t.Errorf("the triggering comment appears %d times in the rerun's prompt, want once", got)
	}
	if meta := f.readMeta(second.RunOrdinal); meta.Prompt != second.Prompt {
		t.Errorf("the document records the abandoned prompt rather than what was sent:\n%s", meta.Prompt)
	}
}

// TestHistoryRoleMapping is the relabelling that is the whole point of the
// block (AC-SR-3.2): what the store recorded a comment as, and what the prompt
// is allowed to call it.
func TestHistoryRoleMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		classification store.Classification
		want           harness.HistoryRole
	}{
		{
			name:           "the owner's earlier instruction is historic, not live",
			classification: store.ClassificationInstruction,
			want:           harness.RoleOldOwner,
		},
		{
			name:           "a third party stays a third party",
			classification: store.ClassificationThirdParty,
			want:           harness.RoleOldThirdParty,
		},
		{
			name:           "litefactory's own reply is named as its own",
			classification: store.ClassificationSelf,
			want:           harness.RoleLitefactory,
		},
		{
			// Not reachable from the store today, which is exactly why it is
			// pinned: a classification added later must not default to being
			// read as the owner's words.
			name:           "an unknown classification claims the least it can",
			classification: store.Classification("something new"),
			want:           harness.RoleOldThirdParty,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := historyRole(tc.classification); got != tc.want {
				t.Errorf("historyRole(%q) = %q, want %q", tc.classification, got, tc.want)
			}
		})
	}
}

// The history is read from the comments table, oldest first, with the
// triggering comment left out. prepare maps and nothing else: no framing, no
// caps, no markup (AC-SR-3.8).
func TestCommentHistoryMapsTheStoredThread(t *testing.T) {
	t.Parallel()

	f := newFixture(t, newStubRunner(nil))
	f.seedThread()

	// No previous trigger: this test is about the history entries, and the
	// second return value has its own coverage in ask_test.go.
	history, _, err := commentHistory(t.Context(), f.opts, f.session, f.request(), 0)
	if err != nil {
		t.Fatalf("commentHistory: %v", err)
	}

	want := []harness.HistoryEntry{
		{
			Role:   harness.RoleOldOwner,
			Author: "marioatcloudvane",
			Body:   historyOwnerBody,
			At:     time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC),
		},
		{
			Role:   harness.RoleOldThirdParty,
			Author: "carol",
			Body:   historyThirdPartyBody,
			At:     time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC),
		},
		{
			Role:   harness.RoleLitefactory,
			Author: "litefactory-bot",
			Body:   historySelfBody,
			At:     time.Date(2026, 8, 20, 11, 0, 0, 0, time.UTC),
		},
	}
	if len(history) != len(want) {
		t.Fatalf("history has %d entries, want %d: %+v", len(history), len(want), history)
	}
	for i, entry := range history {
		if entry.Role != want[i].Role || entry.Author != want[i].Author || entry.Body != want[i].Body {
			t.Errorf("entry %d = %+v, want %+v", i, entry, want[i])
		}
		if !entry.At.Equal(want[i].At) {
			t.Errorf("entry %d is timestamped %s, want %s", i, entry.At, want[i].At)
		}
	}
}
