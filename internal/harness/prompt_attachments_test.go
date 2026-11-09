package harness

import (
	"strings"
	"testing"
)

// TestPromptWithoutAttachmentsIsUnchanged is AC-3.3, the highest-risk
// regression US-3's architecture note names.
//
// The pinned prompt tests elsewhere in this package already fail loudly if
// the bytes move, but they would not catch an empty section — a heading with
// nothing under it is still a well-formed prompt, and every other assertion
// would keep passing while every turn on the system gained three stray lines.
// This states the invariant directly: with no attachments, the word does not
// appear at all.
func TestPromptWithoutAttachmentsIsUnchanged(t *testing.T) {
	prompt := buildPrompt(t, testPromptInput())

	if strings.Contains(prompt, "## Attachments") {
		t.Errorf("a turn with no attachments rendered an Attachments heading:\n%s", prompt)
	}
	if strings.Contains(prompt, "Attachments") {
		t.Errorf("a turn with no attachments mentioned attachments at all:\n%s", prompt)
	}
}

// TestAttachmentsAddOnlyTheirOwnSection is the other half of AC-3.3: turning
// the feature on must be purely additive. Deleting the section from the
// rendered prompt has to leave exactly the prompt that would have been built
// without it — so nothing above or below shifted, and no separator changed.
func TestAttachmentsAddOnlyTheirOwnSection(t *testing.T) {
	without := buildPrompt(t, testPromptInput())

	in := testPromptInput()
	in.Attachments = "- /lf/runs/0001.attachments/01-mockup.png — `mockup` (image/png, 2048 bytes)"
	with := buildPrompt(t, in)

	if with == without {
		t.Fatal("the attachments section was not rendered at all")
	}
	start := strings.Index(with, "\n## Attachments\n")
	if start < 0 {
		t.Fatalf("no Attachments section in:\n%s", with)
	}
	// The section runs to the start of whatever heading follows it, or to the
	// end of the prompt when it is last.
	rest := with[start+1:]
	end := len(with)
	if next := strings.Index(rest, "\n## "); next >= 0 {
		// next indexes the newline that opens the following heading, and that
		// newline belongs to the following section, not to this one.
		end = start + 1 + next
	}
	stripped := with[:start] + with[end:]
	if stripped != without {
		t.Errorf("removing the Attachments section did not restore the original prompt.\ngot:\n%q\nwant:\n%q", stripped, without)
	}
}

func TestAttachmentsSectionCarriesThePathsAndTheirFraming(t *testing.T) {
	in := testPromptInput()
	in.Attachments = "- /lf/runs/0001.attachments/01-mockup.png — `mockup` (image/png, 2048 bytes)"
	prompt := buildPrompt(t, in)

	if !strings.Contains(prompt, "/lf/runs/0001.attachments/01-mockup.png") {
		t.Errorf("the prompt does not name the file the agent is meant to open:\n%s", prompt)
	}
	// The section sits under its own heading, which the run-rules skill names
	// as third-party material; the prompt states no rule of its own.
	if !strings.Contains(prompt, "\n## Attachments\n\n- /lf/runs/0001.attachments/01-mockup.png") {
		t.Errorf("the attachments section does not carry the files under its heading:\n%s", prompt)
	}
}

// TestAttachmentsAreRenderedOnTheResumePath covers the case the section's
// placement exists for: a resumed run triggered by the issue body returns
// early, before the Context section, and an attachment on that trigger would
// be silently dropped if the section sat any lower.
func TestAttachmentsAreRenderedOnTheResumePath(t *testing.T) {
	for _, bodyIsInstruction := range []bool{false, true} {
		in := testPromptInput()
		in.Resume = true
		in.BodyIsInstruction = bodyIsInstruction
		in.Attachments = "- /lf/runs/0002.attachments/01-shot.png — `shot` (image/png, 12 bytes)"

		prompt := buildPrompt(t, in)
		if !strings.Contains(prompt, "## Attachments") {
			t.Errorf("resume path (bodyIsInstruction=%v) dropped the attachments section:\n%s", bodyIsInstruction, prompt)
		}
	}
}

// TestBlankAttachmentsRenderNothing guards the boundary between "no
// attachments" and "a section that happens to be whitespace". A caller that
// hands over a string of spaces gets the no-attachments prompt, not a heading
// over a blank.
func TestBlankAttachmentsRenderNothing(t *testing.T) {
	in := testPromptInput()
	in.Attachments = "   \n\t  \n"

	if got, want := buildPrompt(t, in), buildPrompt(t, testPromptInput()); got != want {
		t.Errorf("a whitespace-only attachments value changed the prompt:\n%q", got)
	}
}

// TestRejectedAttachmentsReachTheAgent covers the Linear HEIC case: a section
// holding only refused files is still rendered, so the agent learns why the
// file is missing instead of trying the raw link.
func TestRejectedAttachmentsReachTheAgent(t *testing.T) {
	in := testPromptInput()
	in.Attachments = "These attachments are not available to you:\n- `IMG_0001.heic` is an unsupported file type."
	prompt := buildPrompt(t, in)

	if !strings.Contains(prompt, "## Attachments") || !strings.Contains(prompt, "`IMG_0001.heic` is an unsupported file type.") {
		t.Errorf("a rejection-only attachments section was not rendered:\n%s", prompt)
	}
}
