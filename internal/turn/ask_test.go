package turn

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
)

// TestHasAskTag is AC-A1.4: the tag is matched line-anchored and whole-line,
// never as a substring, and anything that fails to be a tag is simply not one.
//
// The absent cases are the load-bearing half. Permission that could be turned on
// by quoting a document is not permission, and the documents in question — this
// feature's own plan, spec and review — contain the literal dozens of times.
func TestHasAskTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want bool
	}{
		// Present.
		{name: "the tag alone", body: AskTag, want: true},
		{name: "the tag on its own line under an instruction",
			body: "have another look at the retry path\n\n" + AskTag, want: true},
		{name: "the tag above the instruction",
			body: AskTag + "\nhave another look at the retry path", want: true},
		{name: "the tag in the middle of a body",
			body: "first paragraph\n" + AskTag + "\nsecond paragraph", want: true},
		{name: "a trailing carriage return", body: "do the thing\n" + AskTag + "\r\nmore", want: true},
		{name: "trailing spaces", body: AskTag + "   ", want: true},
		{name: "under a /spike", body: "/spike look at the retry path\n" + AskTag, want: true},

		// Absent.
		{name: "an empty body"},
		{name: "an ordinary instruction", body: "have another look at the retry path"},
		{name: "mid-line, which is prose about the tag",
			body: "write " + AskTag + " on its own line to be asked"},
		{name: "a quote reply, which is GitHub quoting somebody else",
			body: "> " + AskTag + "\n\nno, do not do that"},
		{name: "an indented line, which is a code block or a list continuation",
			body: "  " + AskTag},
		{name: "a tab-indented line", body: "\t" + AskTag},
		{name: "inside a fenced code block on its own line",
			body: "put this in the comment:\n\n```\n  " + AskTag + "\n```"},
		{name: "in backticks, which is how every document names it",
			body: "The literal is `" + AskTag + "`, line-anchored."},
		{name: "a trailing word makes it a sentence", body: AskTag + " me about the schema"},
		{name: "a trailing colon", body: AskTag + ":"},
		{name: "mis-cased, which fails closed", body: "Lf: ask"},
		{name: "shouted, which also fails closed", body: "LF: ASK"},
		{name: "the wrong spacing", body: "lf:ask"},
		{name: "the wrong separator", body: "lf-ask"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := HasAskTag(tc.body); got != tc.want {
				t.Errorf("HasAskTag(%q) = %t, want %t", tc.body, got, tc.want)
			}
		})
	}
}

// AC-A1.4a. The two literals are one character apart in the eye and nothing
// alike in purpose: [MarkerPrefix] says "litefactory wrote this" and [AskTag]
// says "you may ask". A future simplification that merged them would let every
// comment litefactory has ever posted grant permission to the next run, so the
// non-matching is asserted in both directions rather than left to the reader.
func TestTheAskTagAndTheSelfMarkerDoNotMatchEachOther(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		MarkerPrefix,
		MarkerPrefix + "status task=github:acme/acme-api#123 run=2 -->",
		"<!-- " + AskTag + " -->",
	} {
		if HasAskTag(body) {
			t.Errorf("the self-marker %q was read as a permission tag", body)
		}
	}
	for _, body := range []string{
		AskTag,
		AskTag + "\nplease look at the retry path",
	} {
		if HasMarker(body) {
			t.Errorf("the permission tag %q was read as litefactory's own comment", body)
		}
	}
}

// TestParseCommandStripsTheAskTag is AC-A1.8 and AC-A1.9 together, because they
// are the same mechanism seen from two sides: the tag is taken off the body
// before the body is classified, so it can neither reach the instruction nor
// leave an empty one behind.
func TestParseCommandStripsTheAskTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want Command
	}{
		{
			name: "free text with the tag under it",
			body: "have another look at the retry path\n" + AskTag,
			want: Command{Kind: KindFreeText, Text: "have another look at the retry path", Tagged: true},
		},
		{
			name: "free text with the tag above it",
			body: AskTag + "\nhave another look at the retry path",
			want: Command{Kind: KindFreeText, Text: "have another look at the retry path", Tagged: true},
		},
		{
			name: "the tag between two paragraphs keeps both",
			body: "first\n" + AskTag + "\nsecond",
			want: Command{Kind: KindFreeText, Text: "first\nsecond", Tagged: true},
		},
		{
			// F-16. Left in, this became free text with an empty instruction,
			// and BuildPrompt refused it with ErrInvalidPrompt — a failed run
			// naming nothing the owner could act on.
			name: "the tag alone starts nothing",
			body: AskTag,
			want: Command{Kind: KindNone, Tagged: true},
		},
		{
			name: "the tag alone with surrounding blank lines starts nothing",
			body: "\n\n" + AskTag + "\n\n",
			want: Command{Kind: KindNone, Tagged: true},
		},
		{
			// F-15: free text is not the only path that carries text.
			name: "a spike carrying the tag keeps its argument",
			body: "/spike look at the retry path\n" + AskTag,
			want: Command{Kind: KindSpike, Name: "/spike", Text: "look at the retry path", Tagged: true},
		},
		{
			name: "a spike whose whole argument was the tag asks for text",
			body: "/spike\n" + AskTag,
			want: Command{Kind: KindSpikeEmpty, Name: "/spike", Tagged: true},
		},
		{
			name: "a tag above a command still leaves the command first",
			body: AskTag + "\n/spike look at the retry path",
			want: Command{Kind: KindSpike, Name: "/spike", Text: "look at the retry path", Tagged: true},
		},
		{
			name: "a tagged mode command is actionable",
			body: "/implement the plan\n" + AskTag,
			want: Command{Kind: KindSpike, Name: "/implement", Text: "the plan", Tagged: true},
		},
		{
			name: "an untagged body is untouched",
			body: "have another look at the retry path",
			want: Command{Kind: KindFreeText, Text: "have another look at the retry path"},
		},
		{
			// The tag is on the same line as the command, so it is not a tag:
			// line-anchoring is what stops a document quoting the literal from
			// granting permission (F-22), and it does not get to be relaxed for
			// the shape that happens to look convenient. Nothing was stripped
			// because nothing was a tag, and the words the owner typed reach the
			// instruction exactly as typed. The spike path is still covered —
			// see the two /spike cases above, where the tag is on its own line
			// and does come off (AC-A1.8, F-15).
			name: "a tag on the command's own line is prose, not permission",
			body: "/spike " + AskTag + " do the thing",
			want: Command{Kind: KindSpike, Name: "/spike", Text: AskTag + " do the thing"},
		},
		{
			name: "a body merely quoting the tag is untagged and keeps every word",
			body: "does writing " + AskTag + " on a line really work?",
			want: Command{
				Kind: KindFreeText,
				Text: "does writing " + AskTag + " on a line really work?",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := ParseCommand(tc.body)
			if got != tc.want {
				t.Errorf("ParseCommand(%q) = %+v, want %+v", tc.body, got, tc.want)
			}
			// AC-A1.8 stated as the property rather than as a set of examples.
			// Text becomes harness.PromptInput.Instruction, which is the one
			// field with no size cap and the one documented as trusted.
			if got.Tagged && strings.Contains(got.Text, AskTag) {
				t.Errorf("ParseCommand(%q) left the tag in the instruction: %q", tc.body, got.Text)
			}
		})
	}
}

// TestParseCommandNeverFails is the platform constraint stated as a test.
// ParseCommand's contract is that every body classifies as something; a tag
// parser that could fail underneath it would be a second answer to a question
// that already has one, so a malformed tag is absent and never an error.
func TestParseCommandNeverFails(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		"", " ", "\n\n\n", "lf:", "lf: ", "lf: as", "lf: askk", AskTag, "\x00" + AskTag,
		"/spike " + AskTag, "/spike\n" + AskTag, "\r\n" + AskTag + "\r\n",
		strings.Repeat(AskTag+"\n", 100),
		strings.Repeat(AskTag+"\n", 100) + "do the thing",
	} {
		// There is no error return to check — that is the point, and the
		// signature is where it is enforced. What is left to assert is that
		// every input still classifies as something disposable, which is the
		// contract the daemon relies on to consume the comment.
		got := ParseCommand(body)
		if got.Kind.String() == "invalid" {
			t.Errorf("ParseCommand(%q) = %+v: no kind at all", body, got)
		}
		// The invariant command_test.go pins for untagged bodies has to survive
		// the tag being taken off first: a kind that spawns carries text, and
		// one that does not carries none. An empty instruction is the one shape
		// BuildPrompt refuses (F-16).
		if (got.Text != "") != got.Spawns() {
			t.Errorf("ParseCommand(%q) = %+v: text is set exactly when the command spawns", body, got)
		}
		// AC-A1.8, scoped to the bodies it is about: a body that *carried* a tag
		// hands on an instruction with no tag line in it.
		//
		// It is deliberately not asserted for untagged bodies, because there it
		// is not true and should not be. "lf: askk" and "/spike lf: ask do X"
		// never carried a tag — the literal is mid-line, and line-anchoring is
		// the whole of what stops a document quoting it from granting permission
		// (F-22) — so there was nothing to strip, and the owner's words go
		// through as written. "/spike lf: ask" is the sharpest of these: cutting
		// the command word off leaves an instruction that is now, by itself, a
		// tag-shaped line. It grants nothing, and cannot: permission is read
		// once, from the original body, and is never re-derived from Text.
		// Stripping it anyway would mean silently editing an instruction that
		// was never a tag, which is the worse of the two failures.
		if got.Tagged {
			for line := range strings.SplitSeq(got.Text, "\n") {
				if isAskTagLine(line) {
					t.Errorf("ParseCommand(%q) left a tag line in the instruction: %q", body, got.Text)
				}
			}
		}
	}
}

// TestModeMayAsk is AC-A1.6, and the half of permission Go can always decide.
func TestModeMayAsk(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mode string
		want bool
	}{
		{mode: researchMode, want: true},
		{mode: planMode, want: true},
		// The unclassified mode may ask, and has to be able to: a comment naming
		// no command is the common path, and the Research-and-Plan-only rule is
		// carried in prompt text for it (AC-A1.7).
		{mode: harness.InferMode, want: true},
		// AC-A1.6: never, whatever the comment said.
		{mode: "implement"},
		// Deliberate, and a reading of Q6 rather than an oversight: Q6 named
		// Research and Plan and named nothing else.
		{mode: store.ModeSpike},
		{mode: "approve"},
		{mode: ""},
		{mode: "Research"}, // the mode string is the command word, already lowered
	}
	for _, tc := range tests {
		t.Run(tc.mode, func(t *testing.T) {
			t.Parallel()

			if got := modeMayAsk(tc.mode); got != tc.want {
				t.Errorf("modeMayAsk(%q) = %t, want %t", tc.mode, got, tc.want)
			}
		})
	}
}

// TestPermissionIsAConjunction walks the whole gate as prepare assembles it:
// the tag on this run's own trigger AND a mode that may ask. Neither half is
// sufficient, and that is the entire security property — a tagged implement run
// gets the gag (AC-A1.6) and an untagged research run gets it too.
func TestPermissionIsAConjunction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		mode string
		want bool
	}{
		{name: "tagged research", body: "look into it\n" + AskTag, mode: researchMode, want: true},
		{name: "tagged plan", body: "plan it\n" + AskTag, mode: planMode, want: true},
		{name: "tagged infer", body: "look into it\n" + AskTag, mode: harness.InferMode, want: true},
		{name: "tagged implement", body: "build it\n" + AskTag, mode: "implement"},
		{name: "tagged spike", body: "try it\n" + AskTag, mode: store.ModeSpike},
		{name: "untagged research", body: "look into it", mode: researchMode},
		{name: "quoted tag, research", body: "> " + AskTag + "\nlook into it", mode: researchMode},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			command := ParseCommand(tc.body)
			if got := command.Tagged && modeMayAsk(tc.mode); got != tc.want {
				t.Errorf("permission for %q in mode %q = %t, want %t", tc.body, tc.mode, got, tc.want)
			}
		})
	}
}

// TestPreviousMayAsk is AC-A1.13: the previous run's permission is recovered by
// re-parsing the text that triggered it, from state that already exists.
//
// The mode half uses the *previous* run's mode, and the spike case is why. A
// spike that carried the tag was never allowed to ask; reading it as though it
// had been would make the next run announce a withdrawal of permission that was
// never granted, into a conversation that never asked anything.
func TestPreviousMayAsk(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		previous            store.Run
		resume              bool
		previousTriggerBody string
		issueBody           string
		want                bool
	}{
		{
			name:                "the previous comment carried the tag in an asking mode",
			previous:            store.Run{TriggerCommentID: 9001, Mode: researchMode},
			resume:              true,
			previousTriggerBody: "look into it\n" + AskTag,
			want:                true,
		},
		{
			name:                "the previous comment carried no tag",
			previous:            store.Run{TriggerCommentID: 9001, Mode: researchMode},
			resume:              true,
			previousTriggerBody: "look into it",
		},
		{
			name:                "the previous run was a spike, tag or no tag",
			previous:            store.Run{TriggerCommentID: 9001, Mode: store.ModeSpike},
			resume:              true,
			previousTriggerBody: "/spike try it\n" + AskTag,
		},
		{
			name:                "the previous comment only quoted the tag",
			previous:            store.Run{TriggerCommentID: 9001, Mode: researchMode},
			resume:              true,
			previousTriggerBody: "> " + AskTag + "\nlook into it",
		},
		{
			// T-A1.7. Trigger id 0 is the issue body run as the first
			// instruction: there is no comment row, and the body is the text
			// that was parsed at the time.
			name:      "the previous run was triggered by the issue body",
			previous:  store.Run{TriggerCommentID: 0, Mode: harness.InferMode},
			resume:    true,
			issueBody: "the health endpoint is wrong\n" + AskTag,
			want:      true,
		},
		{
			name:      "the previous body-triggered run carried no tag",
			previous:  store.Run{TriggerCommentID: 0, Mode: harness.InferMode},
			resume:    true,
			issueBody: "the health endpoint is wrong",
		},
		{
			// The comment could not be found — it predates litefactory's first
			// sight of the issue, so no body was ever stored. No evidence is
			// read as no permission, which is the direction that fails closed.
			name:     "the previous trigger comment is not in the table",
			previous: store.Run{TriggerCommentID: 9001, Mode: researchMode},
			resume:   true,
		},
		{
			// A zero store.Run has trigger id 0 too, which without the resume
			// guard would send this down the issue-body path and read a tag on
			// an issue nobody has resumed anything about.
			name:      "there was no previous run at all",
			issueBody: "the health endpoint is wrong\n" + AskTag,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := previousMayAsk(tc.previous, tc.resume, tc.previousTriggerBody, tc.issueBody)
			if got != tc.want {
				t.Errorf("previousMayAsk = %t, want %t", got, tc.want)
			}
		})
	}
}
