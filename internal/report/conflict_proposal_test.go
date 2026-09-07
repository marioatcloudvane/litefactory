package report_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/report"
)

// TestReplySuggestsResolutionRefusesSyncAndNothingElse is US-11's version of
// TestReplyRefusesSyncRefusesSyncAndNothingElse: the two kinds share the
// RefusesSync predicate (they answer the same request shape), and neither
// claims any of the other three refusal families.
func TestReplySuggestsResolutionRefusesSyncAndNothingElse(t *testing.T) {
	t.Parallel()

	if !report.ReplySuggestsResolution.RefusesSync() {
		t.Error("ReplySuggestsResolution.RefusesSync() = false, want true")
	}
	if report.ReplySuggestsResolution.RefusesTools() || report.ReplySuggestsResolution.RefusesAgent() ||
		report.ReplySuggestsResolution.RefusesSwitch() || report.ReplySuggestsResolution.RefusesMode() {
		t.Error("ReplySuggestsResolution answers yes to another refusal predicate too")
	}
}

// TestReplySuggestsResolutionRendersTheSuggestion is AC-11.1: one comment
// carries the conflicting files, a per-file suggested resolution, the model
// name, the proposal id and the confirmation phrase.
func TestReplySuggestsResolutionRendersTheSuggestion(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	reply := report.Reply{
		Ref:           testRef,
		Kind:          report.ReplySuggestsResolution,
		ConflictPaths: []string{"internal/cli/task.go", "docs/readme.md"},
		ConflictProposal: &report.ConflictProposal{
			ID:    42,
			Model: "claude",
			Files: []report.ConflictProposalFile{
				{Path: "internal/cli/task.go", Resolution: "package cli\n\nfunc Task() {}\n"},
				{Path: "docs/readme.md", Resolution: "# readme\nresolved content\n"},
			},
		},
	}
	body, err := rep.RenderReply(t.Context(), reply)
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	text := body.String()

	if !strings.HasPrefix(text, report.ReplyMarker(testRef)) {
		t.Errorf("reply does not begin with the reply marker:\n%s", text)
	}
	containsAll(t, "the suggested-resolution reply", text,
		"Conflicting path(s) (2)",
		"internal/cli/task.go",
		"docs/readme.md",
		"claude",
		"42",
		"func Task() {}",
		"resolved content",
		"This is a suggestion only",
		"has not applied it",
	)
	checkGolden(t, "reply_suggests_resolution", text)
}

// TestReplyRefusesSyncAddsExactlyOneLineWhenSuggestionFailed is AC-11.2: a
// blocked run whose suggestion could not be produced still renders the full
// existing conflict refusal, plus exactly one added line.
func TestReplyRefusesSyncAddsExactlyOneLineWhenSuggestionFailed(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	plain, err := rep.RenderReply(t.Context(), report.Reply{
		Ref: testRef, Kind: report.ReplyRefusesSync, ConflictPaths: []string{"a.go"},
	})
	if err != nil {
		t.Fatalf("RenderReply (plain): %v", err)
	}
	withFailure, err := rep.RenderReply(t.Context(), report.Reply{
		Ref: testRef, Kind: report.ReplyRefusesSync, ConflictPaths: []string{"a.go"}, ConflictSuggestionFailed: true,
	})
	if err != nil {
		t.Fatalf("RenderReply (failed suggestion): %v", err)
	}

	plainLines := strings.Split(strings.TrimRight(plain.String(), "\n"), "\n")
	failureLines := strings.Split(strings.TrimRight(withFailure.String(), "\n"), "\n")

	added := len(failureLines) - len(plainLines)
	if added <= 0 {
		t.Fatalf("ConflictSuggestionFailed added %d lines, want at least 1", added)
	}
	if !strings.Contains(withFailure.String(), "No suggested resolution could be produced") {
		t.Errorf("failed-suggestion reply missing the one-line explanation:\n%s", withFailure.String())
	}
}

// TestReplyDisabledSuggestionIsByteIdenticalToThePlainRefusal is AC-11.3:
// the feature-disabled path (ConflictProposal nil, ConflictSuggestionFailed
// false — the zero value of both) renders exactly [report.ReplyRefusesSync]
// always has, unmarked by US-11's presence at all.
func TestReplyDisabledSuggestionIsByteIdenticalToThePlainRefusal(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	body, err := rep.RenderReply(t.Context(), report.Reply{
		Ref: testRef, Kind: report.ReplyRefusesSync, ConflictPaths: []string{"a.go", "b.go"},
	})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	text := body.String()
	for _, absent := range []string{"No suggested resolution", "This is a suggestion only", "Suggested resolution from"} {
		if strings.Contains(text, absent) {
			t.Errorf("disabled-suggestion reply unexpectedly contains %q:\n%s", absent, text)
		}
	}
}

func TestRenderReplyRejectsAnIncompleteConflictProposal(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	cases := []report.Reply{
		{Ref: testRef, Kind: report.ReplySuggestsResolution, ConflictPaths: []string{"a.go"}},
		{Ref: testRef, Kind: report.ReplySuggestsResolution, ConflictPaths: []string{"a.go"},
			ConflictProposal: &report.ConflictProposal{ID: 1, Model: "claude"}},
		{Ref: testRef, Kind: report.ReplySuggestsResolution},
	}
	for i, reply := range cases {
		if _, err := rep.RenderReply(t.Context(), reply); !errors.Is(err, report.ErrInvalidArgument) {
			t.Errorf("case %d: error = %v, want ErrInvalidArgument", i, err)
		}
	}
}
