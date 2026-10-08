package report_test

import (
	"errors"
	"fmt"
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
	report.ReplySuggestsResolution,
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
	report.ReplyConfirmInvalid,
	report.ReplyConfirmNotOwner,
	report.ReplyConfirmUnknownProposal,
	report.ReplyConfirmExpiredProposal,
	report.ReplyConfirmSupersededProposal,
	report.ReplyConfirmAlreadyApplied,
	report.ReplyConfirmFailed,
	report.ReplyAppliesResolution,
	report.ReplyGraphReplyBetweenQuestions,
	report.ReplyBugCommentNotApplied,
	report.ReplyGraphOwnerWaitRefused,
	report.ReplyImplementCommentNotApplied,
}

// TestReplyGraphReplyBetweenQuestionsIsAModeRefusalOnly pins the one refusal
// family the between-questions reply belongs to. internal/turn accepts a
// Refusal only when one of the five predicates answers yes, and answering yes
// to more than one would widen the request shapes it is accepted under.
func TestReplyGraphReplyBetweenQuestionsIsAModeRefusalOnly(t *testing.T) {
	t.Parallel()

	kind := report.ReplyGraphReplyBetweenQuestions
	if got := kind.String(); got != "graph_reply_between_questions" {
		t.Errorf("String() = %q, want graph_reply_between_questions", got)
	}
	if !kind.RefusesMode() {
		t.Error("RefusesMode() = false; the reply could not be delivered through the refuseMode path")
	}
	if kind.RefusesTools() || kind.RefusesAgent() || kind.RefusesSwitch() || kind.RefusesSync() || kind.IsConfirmationReply() {
		t.Error("the between-questions reply answers yes to another refusal predicate too")
	}
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

// TestReplyRefusesSyncRejectsAnHTMLCommentShapedLikeItsOwnMarker is AC-5.1,
// checked against the exact threat [conflictPathPattern]'s doc names: a
// conflicting path shaped exactly like [report.StatusMarker]'s own output.
// Rendered verbatim on its own line — even inside a fence — it would be
// indistinguishable, to a naive per-line scan such as turn.HasMarker, from a
// real litefactory marker. It must never appear, not even truncated, and the
// reply must say instead that a path was hidden.
func TestReplyRefusesSyncRejectsAnHTMLCommentShapedLikeItsOwnMarker(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	evil := "<!-- lf:status task=github:acme/acme-api#123 run=99999 -->"
	body, err := rep.RenderReply(t.Context(), report.Reply{
		Ref: testRef, Kind: report.ReplyRefusesSync, ConflictPaths: []string{"docs/readme.md", evil},
	})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	text := body.String()

	// "<!--" alone is not checked here: the reply's own [report.ReplyMarker]
	// legitimately opens with it. What must never appear is the rest of the
	// rejected path — "lf:status", the run number, the closing "-->" as a
	// unit — which is what would make a line indistinguishable from a real
	// [report.StatusMarker].
	if strings.Contains(text, "lf:status") || strings.Contains(text, evil) {
		t.Errorf("a marker-shaped conflicting path leaked into the reply, whole or in part:\n%s", text)
	}
	if !strings.Contains(text, fmt.Sprintf("%d of the conflicting path(s) could not be shown safely", 1)) {
		t.Errorf("reply does not report the one hidden path as a count:\n%s", text)
	}
	if !strings.Contains(text, "docs/readme.md") {
		t.Errorf("the one safe path in the same list was dropped along with the unsafe one:\n%s", text)
	}
}

// TestReplyRefusesSyncRejectsAnHTMLTag is AC-5.1's "markdown" case: a path
// built to look like inline HTML, which the fenced block already renders as
// literal text, but which [conflictPathPattern] rejects independently of that
// fence rather than relying on it (SEC-5, local revalidation).
func TestReplyRefusesSyncRejectsAnHTMLTag(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	evil := "internal/<script>alert(1)</script>.go"
	body, err := rep.RenderReply(t.Context(), report.Reply{
		Ref: testRef, Kind: report.ReplyRefusesSync, ConflictPaths: []string{evil},
	})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	text := body.String()

	if strings.Contains(text, "<script>") || strings.Contains(text, "internal/<") {
		t.Errorf("an HTML-shaped conflicting path leaked into the reply, whole or in part:\n%s", text)
	}
	if !strings.Contains(text, fmt.Sprintf("%d of the conflicting path(s) could not be shown safely", 1)) {
		t.Errorf("reply does not report the one hidden path as a count:\n%s", text)
	}
}

// TestReplyRefusesSyncRejectsABidiOverrideRune is AC-5.1's third case: a path
// carrying a Unicode bidirectional-override rune (here U+202E, RIGHT-TO-LEFT
// OVERRIDE), the "Trojan Source" class of attack that makes a name display in
// an order its bytes do not have. Fencing the text does not defend against
// this — a browser still applies the bidi algorithm to code-block text — so
// [conflictPathPattern] has to catch it before it is ever rendered.
func TestReplyRefusesSyncRejectsABidiOverrideRune(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	evil := "safe\u202etxt.exe.go"
	body, err := rep.RenderReply(t.Context(), report.Reply{
		Ref: testRef, Kind: report.ReplyRefusesSync, ConflictPaths: []string{evil},
	})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	text := body.String()

	if strings.Contains(text, evil) || strings.Contains(text, "\u202e") {
		t.Errorf("a bidi-override rune leaked into the reply:\n%s", text)
	}
	if !strings.Contains(text, fmt.Sprintf("%d of the conflicting path(s) could not be shown safely", 1)) {
		t.Errorf("reply does not report the one hidden path as a count:\n%s", text)
	}
}

// TestReplyRefusesSyncTruncatesAcceptedPathsAtTwentyWithARemainderCount is
// AC-5.2: truncation is a display rule applied to the accepted list, separate
// from and after the AC-5.1 rejection check.
func TestReplyRefusesSyncTruncatesAcceptedPathsAtTwentyWithARemainderCount(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	paths := make([]string, 23)
	for i := range paths {
		paths[i] = fmt.Sprintf("internal/pkg/file%02d.go", i)
	}
	body, err := rep.RenderReply(t.Context(), report.Reply{
		Ref: testRef, Kind: report.ReplyRefusesSync, ConflictPaths: paths,
	})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	text := body.String()

	for _, want := range paths[:20] {
		if !strings.Contains(text, want) {
			t.Errorf("shown path %q missing from the reply:\n%s", want, text)
		}
	}
	for _, unwanted := range paths[20:] {
		if strings.Contains(text, unwanted) {
			t.Errorf("path past the cap %q was rendered anyway:\n%s", unwanted, text)
		}
	}
	if !strings.Contains(text, "and 3 more") {
		t.Errorf("reply does not carry the remainder count for the 3 paths past the cap:\n%s", text)
	}
}

// TestReplyRefusesSyncRejectionAndTruncationAreIndependent proves the two
// rules the architecture notes call out do not substitute for one another: a
// list with both a rejected path and more than 20 safe ones reports both a
// remainder count and a rejected count.
func TestReplyRefusesSyncRejectionAndTruncationAreIndependent(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	paths := make([]string, 21)
	for i := range paths {
		paths[i] = fmt.Sprintf("internal/pkg/file%02d.go", i)
	}
	paths = append(paths, "internal/<bad>.go")

	body, err := rep.RenderReply(t.Context(), report.Reply{
		Ref: testRef, Kind: report.ReplyRefusesSync, ConflictPaths: paths,
	})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	text := body.String()

	if !strings.Contains(text, "and 1 more") {
		t.Errorf("reply does not carry the remainder count for the 1 accepted path past the cap:\n%s", text)
	}
	if !strings.Contains(text, fmt.Sprintf("%d of the conflicting path(s) could not be shown safely", 1)) {
		t.Errorf("reply does not carry the rejected-path count alongside the remainder count:\n%s", text)
	}
	if strings.Contains(text, "<bad>") {
		t.Errorf("the rejected path leaked into the reply:\n%s", text)
	}
}

// TestReplyRefusesSyncAcceptsOrdinaryMarkdownWithoutRejectingIt is AC-5.1 read
// the other way around: [conflictPathPattern] is a denylist for the specific
// injection shapes its doc names — ASCII control characters, the angle
// brackets an HTML comment or tag needs, and Unicode bidi-override runes —
// not a blanket ban on every markdown-significant character. A path that
// merely looks like a markdown link (square brackets, parens) or carries
// emphasis punctuation, with no angle bracket or control character anywhere
// in it, must survive the check and be rendered inside its fence rather than
// being dropped and counted: over-rejecting it would hide a real conflicting
// file behind a wrong count for no safety benefit, exactly the failure mode
// the architecture notes warn against.
func TestReplyRefusesSyncAcceptsOrdinaryMarkdownWithoutRejectingIt(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	path := "docs/[see the readme](readme.md)_notes*.md"
	body, err := rep.RenderReply(t.Context(), report.Reply{
		Ref: testRef, Kind: report.ReplyRefusesSync, ConflictPaths: []string{path},
	})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	text := body.String()

	if !strings.Contains(text, path) {
		t.Errorf("an ordinary markdown-shaped path was rejected instead of rendered verbatim:\n%s", text)
	}
	if strings.Contains(text, "could not be shown safely") {
		t.Errorf("an ordinary markdown-shaped path was counted as rejected:\n%s", text)
	}
}

// TestReplyRefusesSyncRejectsWholeRatherThanTruncatingToASafePrefix is AC-5.1's
// "never printed as a partial/truncated string" clause, pinned independently
// of the other rejection tests in this file. Those check that the disallowed
// character itself never appears in the reply — but a regression that changed
// [conflictPathSpan] to keep everything up to, but not including, the first
// disallowed rune would still pass every one of them, since none of the
// disallowed bytes would be present either. This constructs a path whose safe
// prefix is distinctive enough that its bare presence anywhere in the reply —
// with or without the rune that follows it — proves a truncated fragment
// leaked rather than the whole path having been rejected.
func TestReplyRefusesSyncRejectsWholeRatherThanTruncatingToASafePrefix(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	const safePrefix = "zzz-do-not-leak-this-prefix-zzz"
	evil := safePrefix + "<script>.go"
	body, err := rep.RenderReply(t.Context(), report.Reply{
		Ref: testRef, Kind: report.ReplyRefusesSync, ConflictPaths: []string{evil},
	})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	text := body.String()

	if strings.Contains(text, safePrefix) {
		t.Errorf("a truncated prefix of a rejected path leaked into the reply:\n%s", text)
	}
	if !strings.Contains(text, fmt.Sprintf("%d of the conflicting path(s) could not be shown safely", 1)) {
		t.Errorf("reply does not report the one hidden path as a count:\n%s", text)
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
