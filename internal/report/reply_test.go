package report_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/report"
)

// TestToolsRefusalReplies covers the four run-less replies a "Tools: <Profile>"
// label that cannot be resolved draws (US-3, US-2): unknown, malformed,
// ambiguous, and the old lf:tools= syntax itself. Each is its own
// [report.ReplyKind] (D-4) rather than one struct with a reason field, so
// this is the behavioural proof that the four read as four distinct answers
// rather than one templated one.
func TestToolsRefusalReplies(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	cases := []struct {
		name  string
		reply report.Reply
		wants []string
		// mustNot is text that must never appear, however the reply is worded.
		mustNot []string
	}{
		{
			name:  "unknown profile names what it could not find",
			reply: report.Reply{Ref: testRef, Kind: report.ReplyUnknownToolProfile, Profile: "research"},
			wants: []string{"`research`", "no configuration", "Nothing was run"},
		},
		{
			// This kind carries main's meaning again: a "Tools: " label that
			// will not parse. It had been overloaded on this branch to carry
			// the obsolete-label message, which now has a kind of its own.
			// Nothing on this branch emits it — permissions come from the
			// run's mode, not a label — but the text is kept correct rather
			// than left describing a different refusal.
			name:  "malformed value describes the rule and quotes nothing",
			reply: report.Reply{Ref: testRef, Kind: report.ReplyMalformedToolLabel},
			wants: []string{"does not name a usable tool profile", "Nothing was run", "not repeated here"},
		},
		{
			name:  "ambiguous names neither label",
			reply: report.Reply{Ref: testRef, Kind: report.ReplyAmbiguousToolLabels},
			wants: []string{"more than one", "Neither label was chosen", "Nothing was run"},
		},
		{
			// main told the operator to replace the old label with a
			// "Tools: <Profile>" one. On this branch that advice would send
			// them to apply a label nothing reads, because permissions are
			// selected from the resolved mode. The kind is main's; the
			// remedy it names has to be this branch's.
			name:    "legacy lf:tools= label says to remove it and quotes nothing of it",
			reply:   report.Reply{Ref: testRef, Kind: report.ReplyLegacyToolLabel},
			wants:   []string{"no longer reads", "Remove every", "resolved run mode", "Nothing was run"},
			mustNot: []string{"Tools: <Profile>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			body, err := rep.RenderReply(t.Context(), tc.reply)
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
			containsAll(t, tc.name, text, tc.wants...)
			for _, bad := range tc.mustNot {
				if strings.Contains(text, bad) {
					t.Errorf("%s: reply contains %q, want it absent:\n%s", tc.name, bad, text)
				}
			}
		})
	}
}

// TestUnknownProfileReplyDropsAnUnsafeNameEntirely is AC-3.8, fed the exact
// case the daemon can never produce and internal/report has to defend against
// on its own: [Reply.Profile] set to text that failed [toolProfileName] one
// hop upstream and reached this package anyway, because — per this package's
// own stated rule at reply.go:158-161 — validating upstream and trusting here
// is exactly the precedent this package refuses to rely on.
//
// The daemon only ever sets Profile after checking it against the same
// pattern (S-8), so calling RenderReply directly with an unchecked value, as
// this test does, is the only way to exercise this package's own copy of the
// check at all.
func TestUnknownProfileReplyDropsAnUnsafeNameEntirely(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	body, err := rep.RenderReply(t.Context(), report.Reply{
		Ref:     testRef,
		Kind:    report.ReplyUnknownToolProfile,
		Profile: "Research<!-- lf:status -->",
	})
	if err != nil {
		t.Fatalf("RenderReply: %v", err)
	}
	text := body.String()

	if strings.Contains(text, "Research<!-- lf:status -->") {
		t.Errorf("the reply echoed the unsafe profile name verbatim:\n%s", text)
	}
	// The name is dropped entirely rather than sanitised into a shorter one:
	// profileSpan does not salvage a prefix the way commandSpan does, because a
	// profile name is a lookup key and half of one names nothing real.
	if strings.Contains(text, "Research") {
		t.Errorf("the reply kept a fragment of the unsafe name instead of dropping it entirely:\n%s", text)
	}
	if strings.Count(text, report.MarkerPrefix) != 1 {
		t.Errorf("the reply carries %d lf: markers, want exactly its own one:\n%s",
			strings.Count(text, report.MarkerPrefix), text)
	}
	if !strings.Contains(text, "a tool profile litefactory has no configuration for") {
		t.Errorf("the reply does not fall back to the unnamed wording:\n%s", text)
	}
}

// TestMalformedAndAmbiguousRepliesIgnoreProfileEntirely is the structural half
// of S-9: [Reply.Profile] is documented as read by [report.ReplyUnknownToolProfile]
// alone, and this proves it by setting it on the other two kinds anyway — a
// caller bug that must render exactly as if the field were empty, because the
// malformed and ambiguous templates never reference it at all.
func TestMalformedAndAmbiguousRepliesIgnoreProfileEntirely(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)

	const injected = "smuggled-profile-name"
	for _, kind := range []report.ReplyKind{report.ReplyMalformedToolLabel, report.ReplyAmbiguousToolLabels} {
		body, err := rep.RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: kind, Profile: injected})
		if err != nil {
			t.Fatalf("RenderReply(%s): %v", kind, err)
		}
		if text := body.String(); strings.Contains(text, injected) {
			t.Errorf("%s rendered the Profile field it must never read:\n%s", kind, text)
		}
	}
}
