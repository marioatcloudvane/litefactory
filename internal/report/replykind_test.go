package report_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/report"
)

// everyReplyKind is every named [report.ReplyKind] this package defines,
// [report.ReplyInvalid] excluded — the zero value, which is deliberately
// never a real kind. It is a hand-maintained list rather than a reflective
// enumeration (Go gives no way to enumerate a typed-int const block), so
// adding a new ReplyKind and forgetting to add it here is a real gap this
// test cannot catch on its own — but every kind that exists today, including
// [report.ReplyRefusesSync], is exercised by it.
var everyReplyKind = []report.ReplyKind{
	report.ReplyUnknownCommand,
	report.ReplyEmptySpike,
	report.ReplyAmbiguousAgentLabels,
	report.ReplyMalformedAgentLabel,
	report.ReplyUnknownAgent,
	report.ReplyAgentSwitchRefused,
	report.ReplyRefusesSync,
	report.ReplyMalformedModeLabel,
	report.ReplyAmbiguousModeLabels,
	report.ReplyConflictingModeSelection,
	report.ReplyLegacyToolLabel,
	report.ReplyScheduleInvalid,
	report.ReplyScheduleConfirmed,
	report.ReplyScheduleBlocked,
	report.ReplyScheduleProposalReady,
	report.ReplyScheduleProposalAccepted,
	report.ReplyScheduleProposalRejected,
	report.ReplyScheduleProposalFailed,
}

// TestReplyKindStringIsDefinedAndDistinctForEveryKind is AC-4.2's
// enum-exhaustiveness check, made concrete: every real [report.ReplyKind] —
// [report.ReplyRefusesSync] included — renders a real, non-"invalid" label
// from [report.ReplyKind.String], and no two kinds collide on it. Only
// [report.ReplyInvalid], the zero value, is allowed to render "invalid".
func TestReplyKindStringIsDefinedAndDistinctForEveryKind(t *testing.T) {
	t.Parallel()

	seen := make(map[string]report.ReplyKind, len(everyReplyKind))
	for _, kind := range everyReplyKind {
		got := kind.String()
		if got == "" || got == "invalid" {
			t.Errorf("%v.String() = %q, want a real, non-invalid label", kind, got)
			continue
		}
		if other, ok := seen[got]; ok {
			t.Errorf("%v and %v both render String() = %q", kind, other, got)
		}
		seen[got] = kind
	}

	if got := report.ReplyInvalid.String(); got != "invalid" {
		t.Errorf("ReplyInvalid.String() = %q, want %q: the zero value is not a real reply", got, "invalid")
	}
}

// TestReplyRefusesSyncStringIsSyncRefused pins the exact label AC-4.2 asks
// for by name, independent of the exhaustiveness sweep above.
func TestReplyRefusesSyncStringIsSyncRefused(t *testing.T) {
	t.Parallel()

	if got := report.ReplyRefusesSync.String(); got != "sync_refused" {
		t.Errorf("ReplyRefusesSync.String() = %q, want %q", got, "sync_refused")
	}
}

// TestReplyRefusesSyncRefusesSyncAndNothingElse is the predicate half of
// AC-4.2: [report.ReplyKind.RefusesSync] is true for
// [report.ReplyRefusesSync] and false for the other refusal families, and
// ReplyRefusesSync itself claims none of the other three predicates —
// [ReplyRequest.kind] and [ReplyRequest.validate] in internal/turn OR all
// four together, so a kind answering yes to more than one would silently
// widen what request shape it is accepted under.
func TestReplyRefusesSyncRefusesSyncAndNothingElse(t *testing.T) {
	t.Parallel()

	if !report.ReplyRefusesSync.RefusesSync() {
		t.Error("ReplyRefusesSync.RefusesSync() = false, want true")
	}
	if report.ReplyRefusesSync.RefusesTools() || report.ReplyRefusesSync.RefusesAgent() ||
		report.ReplyRefusesSync.RefusesSwitch() || report.ReplyRefusesSync.RefusesMode() {
		t.Error("ReplyRefusesSync answers yes to another refusal predicate too; the four must be mutually exclusive")
	}

	others := []report.ReplyKind{
		report.ReplyUnknownCommand, report.ReplyEmptySpike, report.ReplyAgentSwitchRefused,
		report.ReplyLegacyToolLabel, report.ReplyMalformedModeLabel, report.ReplyAmbiguousAgentLabels,
		report.ReplyMalformedAgentLabel, report.ReplyUnknownAgent,
	}
	for _, other := range others {
		if other.RefusesSync() {
			t.Errorf("%v.RefusesSync() = true, want false", other)
		}
	}
}

// TestReplyRefusesSyncRendersTheRefusalAndTheConflictingPaths is AC-4.1's
// rendering half and US-4's own architecture note taken literally: the
// refusal shell (the fact of refusal, the reason, the sentence) plus the
// conflicting paths [git.Worktree.MergePreview] found, carried through
// exactly as syncBase recorded them.
func TestReplyRefusesSyncRendersTheRefusalAndTheConflictingPaths(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	reply := report.Reply{
		Ref:           testRef,
		Kind:          report.ReplyRefusesSync,
		ConflictPaths: []string{"internal/cli/task.go", "docs/readme.md"},
	}
	body, err := rep.RenderReply(t.Context(), reply)
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	text := body.String()

	if !strings.HasPrefix(text, report.ReplyMarker(testRef)) {
		t.Errorf("reply does not begin with the reply marker:\n%s", text)
	}
	if strings.Contains(text, "run=") {
		t.Error("a run-less reply carries a run attribute")
	}
	containsAll(t, "the sync refusal reply", text,
		"could not be merged with the base branch cleanly",
		"stopped before starting the agent",
		"Nothing was run",
		"Conflicting path(s) (2)",
		"internal/cli/task.go",
		"docs/readme.md",
	)
	checkGolden(t, "reply_refuses_sync", text)
}

// TestReplyRefusesSyncConflictingPathCannotBreakOutOfItsFence is S-2, checked
// against this package's Reply rendering path specifically — a different call
// site than [Run.Sync]'s equivalent list, which
// TestSyncDisclosure/"a conflicting path cannot break out of its fence"
// already covers. Both call [conflictsBlock], but nothing here assumes that;
// the fence is proven at each call site conflicting paths reach.
func TestReplyRefusesSyncConflictingPathCannotBreakOutOfItsFence(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	evil := "internal/```not-a-fence```/file.go"
	body, err := rep.RenderReply(t.Context(), report.Reply{
		Ref: testRef, Kind: report.ReplyRefusesSync, ConflictPaths: []string{evil},
	})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	text := body.String()
	if !strings.Contains(text, "````\n"+evil+"\n````") {
		t.Errorf("conflicting path is not enclosed in a fence longer than its own backtick run:\n%s", text)
	}
}

// TestRenderReplyRejectsASyncRefusalWithNoConflictingPaths pins reporter.go's
// own invariant check: a blocked outcome always carries at least one
// conflicting path, and a caller that got that wrong is refused rather than
// rendering a refusal that names nothing.
func TestRenderReplyRejectsASyncRefusalWithNoConflictingPaths(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	_, err := rep.RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: report.ReplyRefusesSync})
	if !errors.Is(err, report.ErrInvalidArgument) {
		t.Errorf("RenderReply with no ConflictPaths: error = %v, want ErrInvalidArgument", err)
	}
}
