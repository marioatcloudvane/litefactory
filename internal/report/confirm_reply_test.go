package report_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/report"
)

// TestReplyConfirmKindsRefuseSyncAndEveryOtherPredicateAlike is US-12's
// version of TestReplyRefusesSyncRefusesSyncAndNothingElse (CR-A-011): none
// of the eight confirmation replies is a tool, agent, switch, mode or sync
// refusal — [ReplyKind.IsConfirmationReply] is their own, disjoint predicate,
// and [ReplyRequest.kind]/[ReplyRequest.validate] in internal/turn rely on
// that disjointness to keep the confirmation request shape from silently
// widening to admit one of the other four families.
func TestReplyConfirmKindsRefuseSyncAndEveryOtherPredicateAlike(t *testing.T) {
	t.Parallel()

	for _, kind := range []report.ReplyKind{
		report.ReplyConfirmInvalid, report.ReplyConfirmNotOwner, report.ReplyConfirmUnknownProposal,
		report.ReplyConfirmExpiredProposal, report.ReplyConfirmSupersededProposal,
		report.ReplyConfirmAlreadyApplied, report.ReplyConfirmFailed, report.ReplyAppliesResolution,
	} {
		if !kind.IsConfirmationReply() {
			t.Errorf("%v.IsConfirmationReply() = false, want true", kind)
		}
		if kind.RefusesTools() || kind.RefusesAgent() || kind.RefusesSwitch() ||
			kind.RefusesMode() || kind.RefusesSync() {
			t.Errorf("%v answers yes to a non-confirmation refusal predicate too", kind)
		}
	}

	for _, other := range []report.ReplyKind{
		report.ReplyUnknownCommand, report.ReplyRefusesSync, report.ReplyAgentSwitchRefused,
		report.ReplyLegacyToolLabel, report.ReplyScheduleConfirmed,
	} {
		if other.IsConfirmationReply() {
			t.Errorf("%v.IsConfirmationReply() = true, want false", other)
		}
	}
}

// TestReplyConfirmInvalidNamesTheGrammarAndNothingElse is AC-12.3's grammar
// case (S-14): the reply never echoes the comment's own text, only the exact
// phrase it required.
func TestReplyConfirmInvalidNamesTheGrammarAndNothingElse(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	body, err := rep.RenderReply(t.Context(), report.Reply{
		Ref: testRef, Kind: report.ReplyConfirmInvalid,
	})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	text := body.String()
	containsAll(t, "the invalid-confirmation reply", text,
		"I could not accept that confirmation",
		"apply this resolution",
		"Nothing was applied",
	)
}

// TestReplyConfirmNotOwnerLeavesTheStatusUnsaid is AC-12.2: the reply
// discloses the refusal without naming the commenter (S-16 screens the
// commenting handle only for [ReplyAppliesResolution]; a non-owner refusal
// never even reaches a handle worth quoting back).
func TestReplyConfirmNotOwnerLeavesTheStatusUnsaid(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	body, err := rep.RenderReply(t.Context(), report.Reply{
		Ref: testRef, Kind: report.ReplyConfirmNotOwner,
	})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	text := body.String()
	containsAll(t, "the not-owner reply", text,
		"Only this task's issue owner may confirm a proposal",
		"Nothing was applied",
		"status is unchanged",
	)
}

// TestReplyConfirmUnknownProposalNamesTheIDWhenKnown is AC-12.3's first case:
// the id is named whenever the request carried one, and the reply still
// answers sensibly when it did not.
func TestReplyConfirmUnknownProposalNamesTheIDWhenKnown(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	withID, err := rep.RenderReply(t.Context(), report.Reply{
		Ref: testRef, Kind: report.ReplyConfirmUnknownProposal,
		Confirm: report.ConfirmReply{ProposalID: 77},
	})
	if err != nil {
		t.Fatalf("RenderReply (with id): %v", err)
	}
	containsAll(t, "the unknown-proposal reply", withID.String(),
		"I have no pending proposal `77`", "Nothing was applied",
	)

	withoutID, err := rep.RenderReply(t.Context(), report.Reply{
		Ref: testRef, Kind: report.ReplyConfirmUnknownProposal,
	})
	if err != nil {
		t.Fatalf("RenderReply (without id): %v", err)
	}
	if strings.Contains(withoutID.String(), "proposal `0`") {
		t.Errorf("a zero proposal id was rendered as though it were real:\n%s", withoutID.String())
	}
	containsAll(t, "the unknown-proposal reply with no id", withoutID.String(),
		"I have no pending proposal with that id", "Nothing was applied",
	)
}

// TestReplyConfirmExpiredProposalNamesTheIDAndAsksForAFreshSuggestion is
// AC-12.3's second case.
func TestReplyConfirmExpiredProposalNamesTheIDAndAsksForAFreshSuggestion(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	body, err := rep.RenderReply(t.Context(), report.Reply{
		Ref: testRef, Kind: report.ReplyConfirmExpiredProposal,
		Confirm: report.ConfirmReply{ProposalID: 12},
	})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	containsAll(t, "the expired-proposal reply", body.String(),
		"Proposal `12` has expired",
		"can no longer be confirmed",
		"Nothing was applied",
		"Comment again to ask for a fresh suggestion",
	)
}

// TestReplyConfirmSupersededProposalNamesTheIDAndAsksForAFreshSuggestion is
// AC-12.4's reply.
func TestReplyConfirmSupersededProposalNamesTheIDAndAsksForAFreshSuggestion(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	body, err := rep.RenderReply(t.Context(), report.Reply{
		Ref: testRef, Kind: report.ReplyConfirmSupersededProposal,
		Confirm: report.ConfirmReply{ProposalID: 13},
	})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	containsAll(t, "the superseded-proposal reply", body.String(),
		"Proposal `13`",
		"no longer matches this task's current branch or base",
		"Nothing was applied",
		"Comment again to ask for a fresh suggestion",
	)
}

// TestReplyConfirmAlreadyAppliedNamesTheIDAsANoOp is AC-12.9: a second
// confirmation of an already-applied proposal is reported as a no-op, never
// as though something new happened.
func TestReplyConfirmAlreadyAppliedNamesTheIDAsANoOp(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	body, err := rep.RenderReply(t.Context(), report.Reply{
		Ref: testRef, Kind: report.ReplyConfirmAlreadyApplied,
		Confirm: report.ConfirmReply{ProposalID: 14},
	})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	containsAll(t, "the already-applied reply", body.String(),
		"Proposal `14` was already applied",
		"Nothing more was done",
	)
}

// TestReplyConfirmFailedDisclosesNothingWasWritten is AC-12.6: a failed
// application discloses that the branch, index and commits are unchanged and
// invites a retry.
func TestReplyConfirmFailedDisclosesNothingWasWritten(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	body, err := rep.RenderReply(t.Context(), report.Reply{
		Ref: testRef, Kind: report.ReplyConfirmFailed,
		Confirm: report.ConfirmReply{ProposalID: 15},
	})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	containsAll(t, "the failed-application reply", body.String(),
		"I could not apply proposal `15`",
		"branch, index and",
		"commits are unchanged",
		"Comment again once the underlying issue is resolved",
	)
}

// TestReplyAppliesResolutionNamesTheModelConfirmerFilesAndCommit is AC-12.5's
// rendering half: the one disclosure that actually happened names every one
// of the four required facts in a single reply.
func TestReplyAppliesResolutionNamesTheModelConfirmerFilesAndCommit(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	commitSHA := "9c1e0a4b7d2f8e6a5c3b1d09f8e7a6b5c4d3e2f1"
	body, err := rep.RenderReply(t.Context(), report.Reply{
		Ref: testRef, Kind: report.ReplyAppliesResolution,
		Confirm: report.ConfirmReply{
			ProposalID:  42,
			Model:       "claude",
			ConfirmedBy: "octo-reporter",
			Files: []report.ConflictProposalFile{
				{Path: "internal/cli/task.go", Resolution: "package cli\n\nfunc Task() {}\n"},
			},
			CommitSHA: commitSHA,
		},
	})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	text := body.String()
	containsAll(t, "the applied-resolution reply", text,
		"Applied proposal `42`",
		"claude",
		"octo-reporter",
		"internal/cli/task.go",
		"func Task() {}",
		commitSHA,
		"github.com/acme/acme-api/commit/"+commitSHA,
	)
	checkGolden(t, "reply_applies_resolution", text)
}

// TestReplyAppliesResolutionRendersWithNoFilesAndNoCommit covers the
// defensive corners of [Reply.body]'s ReplyAppliesResolution case: a
// [ConfirmReply] with none of the optional facts set still renders a plain,
// well-formed disclosure rather than an empty or malformed one.
func TestReplyAppliesResolutionRendersWithNoFilesAndNoCommit(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	body, err := rep.RenderReply(t.Context(), report.Reply{
		Ref: testRef, Kind: report.ReplyAppliesResolution,
		Confirm: report.ConfirmReply{ProposalID: 43},
	})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	text := body.String()
	if !strings.Contains(text, "Applied proposal `43`") {
		t.Errorf("reply does not name the applied proposal id:\n%s", text)
	}
	if strings.Contains(text, "Files applied:") {
		t.Errorf("reply names a files section with no files to show:\n%s", text)
	}
	if strings.Contains(text, "Commit:") {
		t.Errorf("reply names a commit with none supplied:\n%s", text)
	}
}

// TestReplyAppliesResolutionRejectsAMalformedConfirmedByHandle is S-16: a
// confirming handle that does not match GitHub's own login shape is never
// echoed into a comment litefactory signs its own name to — the same
// reject-don't-salvage discipline [agentSpan] and [commandSpan] already give
// their own untrusted strings.
func TestReplyAppliesResolutionRejectsAMalformedConfirmedByHandle(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	evil := "octo`;<script>evil()</script>"
	body, err := rep.RenderReply(t.Context(), report.Reply{
		Ref: testRef, Kind: report.ReplyAppliesResolution,
		Confirm: report.ConfirmReply{ProposalID: 44, ConfirmedBy: evil},
	})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	text := body.String()
	if strings.Contains(text, evil) || strings.Contains(text, "<script>") {
		t.Errorf("a malformed confirming handle leaked into the reply:\n%s", text)
	}
	if !strings.Contains(text, "confirmed.") {
		t.Errorf("reply does not fall back to the bare 'confirmed.' wording for a rejected handle:\n%s", text)
	}
}

// TestReplyAppliesResolutionRejectsAMalformedModelName mirrors the handle
// check above for the model name (S-8): [agentSpan]'s own pattern rejects
// anything that is not the shape of a config key, whole rather than
// truncated.
func TestReplyAppliesResolutionRejectsAMalformedModelName(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	evil := "claude <script>evil()</script>"
	body, err := rep.RenderReply(t.Context(), report.Reply{
		Ref: testRef, Kind: report.ReplyAppliesResolution,
		Confirm: report.ConfirmReply{ProposalID: 45, Model: evil, ConfirmedBy: "octocat"},
	})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	text := body.String()
	if strings.Contains(text, evil) || strings.Contains(text, "<script>") {
		t.Errorf("a malformed model name leaked into the reply:\n%s", text)
	}
	if !strings.Contains(text, "octocat") {
		t.Errorf("a valid confirming handle alongside a rejected model was dropped too:\n%s", text)
	}
}
