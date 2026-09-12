package turn

import (
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
)

// The two sentences that say which paragraph a delivered prompt ended up with.
// They are fragments of internal/harness's own constants rather than copies of
// them: matching the whole paragraph here would make every wording change in
// that package a failure in this one, and matching nothing would make this file
// assert that a prompt exists.
const (
	promptGrantsAsking  = "permission to ask"
	promptForbidsAsking = "may not ask clarifying questions"
	promptRevokesAsking = "may no longer ask questions"
)

// askingRunner is the claude double every test in this file uses: it succeeds,
// and the request it recorded is where the prompt is read back from.
func askingRunner(f **fixture) *stubRunner {
	return recordingRunner(func(req harness.Request) (*harness.RunResult, error) {
		return succeeded(req, (*f).paths.Home()), nil
	})
}

// TestTheTagIsHonouredOnlyFromTheTriggeringComment is AC-A1.5, asserted where
// the prompt is actually delivered rather than at the parser.
//
// The tag is permission, and permission is granted by the comment that gave the
// instruction — read by Go, from that comment, and from nothing else. The three
// cases here are the three places a tag can appear without having been written
// by the person triggering this run.
func TestTheTagIsHonouredOnlyFromTheTriggeringComment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// seed writes the thread this turn reads back, before the turn runs.
		seed func(f *fixture)
		// mutate adjusts the triggering request itself.
		mutate func(req *Request)
	}{
		{
			name: "a third party wrote the tag",
			seed: func(f *fixture) {
				f.seedComment(historyThirdPartyComment, "carol",
					"I think you should ask about this\n"+AskTag,
					store.ClassificationThirdParty, time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC))
			},
		},
		{
			name: "the tag is in replayed history, from an earlier turn",
			seed: func(f *fixture) {
				f.seedComment(historyOwnerComment, "marioatcloudvane",
					"look at the retry path\n"+AskTag,
					store.ClassificationInstruction, time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC))
			},
		},
		{
			name: "the tag is in the issue body, which triggered nothing",
			seed: func(*fixture) {},
			mutate: func(req *Request) {
				req.Issue.Body = "the health endpoint is wrong\n" + AskTag
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var f *fixture
			runner := askingRunner(&f)
			f = newFixture(t, runner)
			tc.seed(f)

			// The triggering comment is untagged free text, so this run has no
			// permission from anything it wrote itself.
			req := f.request()
			req.Trigger.Body = "have another look at the retry path"
			req.Command = ParseCommand(req.Trigger.Body)
			req.Mode = ""
			if tc.mutate != nil {
				tc.mutate(&req)
			}
			if result := f.execute(req); !result.Succeeded() {
				t.Fatalf("turn did not succeed: %+v\nlogs:\n%s", result, f.logs)
			}

			prompt := runner.requests[0].Prompt
			if strings.Contains(prompt, promptGrantsAsking) {
				t.Errorf("a tag outside the triggering comment granted permission:\n%s", prompt)
			}
			if !strings.Contains(prompt, promptForbidsAsking) {
				t.Errorf("the run was not told to proceed headless:\n%s", prompt)
			}
		})
	}
}

// A tagged comment naming no command reaches claude with permission and without
// the tag: the whole of A1's happy path, through prepare, in one turn.
func TestATaggedCommentGrantsPermissionAndLosesItsTag(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := askingRunner(&f)
	f = newFixture(t, runner)

	req := f.request()
	req.Trigger.Body = "have another look at the retry path\n" + AskTag
	req.Command = ParseCommand(req.Trigger.Body)
	req.Mode = ""
	if result := f.execute(req); !result.Succeeded() {
		t.Fatalf("turn did not succeed: %+v\nlogs:\n%s", result, f.logs)
	}

	sent := runner.requests[0]
	if sent.Mode != harness.InferMode {
		t.Fatalf("mode = %q, want %q", sent.Mode, harness.InferMode)
	}
	if !strings.Contains(sent.Prompt, promptGrantsAsking) {
		t.Errorf("the tagged run was not given permission:\n%s", sent.Prompt)
	}
	if strings.Contains(sent.Prompt, promptForbidsAsking) {
		t.Errorf("the tagged run was told both things at once:\n%s", sent.Prompt)
	}

	// AC-A1.8 where it matters: the instruction is the uncapped, trusted field.
	// The tag is still in the comment note, because that note is the comment
	// verbatim and editing untrusted material would be its own kind of lie.
	instruction, _, found := strings.Cut(sent.Prompt, "\n## Context")
	if !found {
		t.Fatalf("prompt has no context section:\n%s", sent.Prompt)
	}
	_, instruction, found = strings.Cut(instruction, "## Instruction")
	if !found {
		t.Fatalf("prompt has no instruction section:\n%s", sent.Prompt)
	}
	if strings.Contains(instruction, AskTag) {
		t.Errorf("the tag reached the instruction:\n%s", instruction)
	}
}

// TestPermissionIsRecoveredFromThePreviousTrigger is AC-A1.13 and AC-A1.12
// through a real turn, and the reason the derivation had to be re-parsed rather
// than looked up: nothing records permission, and no column was added for it.
//
// The revoke direction is the one that matters. A conversation that was told it
// may ask keeps believing that unless the next prompt says otherwise, because a
// resumed prompt repeats nothing — so an owner who replies in plain words to a
// question has to be able to end the asking by not re-tagging.
func TestPermissionIsRecoveredFromThePreviousTrigger(t *testing.T) {
	t.Parallel()

	const previousComment = int64(7500)

	tests := []struct {
		name string
		// previousBody is what triggered the run before this one, and
		// previousMode is the mode it ran in.
		previousBody string
		previousMode string
		// body is what triggers the run under test.
		body       string
		wantGrant  bool
		wantRevoke bool
	}{
		{
			name:         "tagged, then not: permission is withdrawn out loud",
			previousBody: "look at the retry path\n" + AskTag,
			previousMode: harness.InferMode,
			body:         "yes, use the second one",
			wantRevoke:   true,
		},
		{
			name:         "not tagged, then tagged: permission is granted",
			previousBody: "look at the retry path",
			previousMode: harness.InferMode,
			body:         "have another go\n" + AskTag,
			wantGrant:    true,
		},
		{
			// AC-A1.11. The conversation was told it may ask and still may; a
			// resumed prompt is the delta, and permission that did not move is
			// not part of it.
			name:         "tagged both times: nothing changed, nothing is said",
			previousBody: "look at the retry path\n" + AskTag,
			previousMode: harness.InferMode,
			body:         "and again\n" + AskTag,
		},
		{
			name:         "untagged both times: nothing changed, nothing is said",
			previousBody: "look at the retry path",
			previousMode: harness.InferMode,
			body:         "and again",
		},
		{
			// The previous run was a spike, so it never had permission however
			// its comment was written. Announcing a withdrawal here would be
			// litefactory telling a conversation about a state it was never in.
			name:         "a tagged spike never had permission to lose",
			previousBody: "/spike try it\n" + AskTag,
			previousMode: store.ModeSpike,
			body:         "yes, use the second one",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var f *fixture
			runner := askingRunner(&f)
			f = newFixture(t, runner)

			// The thread as the daemon would have left it: the comment that
			// triggered the previous run is recorded, and the run itself is a
			// finished row on this conversation.
			f.seedComment(previousComment, "marioatcloudvane", tc.previousBody,
				store.ClassificationInstruction, time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC))
			f.seedRun(store.FirstSegmentOrdinal, storedClaudeSessionID,
				previousComment, tc.previousMode, store.RunReported)

			req := f.request()
			req.Trigger.Body = tc.body
			req.Command = ParseCommand(tc.body)
			req.Mode = ""
			if result := f.execute(req); !result.Succeeded() {
				t.Fatalf("turn did not succeed: %+v\nlogs:\n%s", result, f.logs)
			}

			sent := runner.requests[0]
			// AC-A1.16: the owner's reply continues the same conversation.
			if sent.Path != harness.PathResume {
				t.Fatalf("run path = %s, want %s", sent.Path, harness.PathResume)
			}
			if got := strings.Contains(sent.Prompt, promptGrantsAsking); got != tc.wantGrant {
				t.Errorf("permission granted = %t, want %t:\n%s", got, tc.wantGrant, sent.Prompt)
			}
			if got := strings.Contains(sent.Prompt, promptRevokesAsking); got != tc.wantRevoke {
				t.Errorf("permission revoked = %t, want %t:\n%s", got, tc.wantRevoke, sent.Prompt)
			}
		})
	}
}

// T-A1.7 end to end: a previous run triggered by the issue body has no comment
// row to re-parse, and the body — which is still in memory on every later run —
// is what carried its permission.
func TestPermissionIsRecoveredFromTheIssueBody(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := askingRunner(&f)
	f = newFixture(t, runner)

	// Trigger id 0 is the issue body run as the first instruction (AC-2.16b).
	f.seedRun(store.FirstSegmentOrdinal, storedClaudeSessionID, 0, harness.InferMode, store.RunReported)

	req := f.request()
	req.Issue.Body = "the health endpoint is wrong\n" + AskTag
	req.Trigger.Body = "yes, the second one"
	req.Command = ParseCommand(req.Trigger.Body)
	req.Mode = ""
	if result := f.execute(req); !result.Succeeded() {
		t.Fatalf("turn did not succeed: %+v\nlogs:\n%s", result, f.logs)
	}

	sent := runner.requests[0]
	if sent.Path != harness.PathResume {
		t.Fatalf("run path = %s, want %s", sent.Path, harness.PathResume)
	}
	if !strings.Contains(sent.Prompt, promptRevokesAsking) {
		t.Errorf("the body-triggered run's permission was not recovered, so its "+
			"withdrawal was never stated:\n%s", sent.Prompt)
	}
}
