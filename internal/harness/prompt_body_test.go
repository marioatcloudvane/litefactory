package harness

import (
	"strings"
	"testing"
)

// The prompt of a run that the issue body started (AC-2.16b, AC-2.25). Nobody
// commented: the issue was assigned to the owner and its body is the request,
// so it belongs under "## Instruction" and nowhere else.
//
// The body is free text here on purpose. A body that named a command would
// reach BuildPrompt with the command word stripped out of the instruction, and
// the two strings would no longer be comparable — and "the body appears exactly
// once" is the assertion this whole file is built around.
const bodyInstruction = "Add a /health endpoint that reports the build sha, and a test for it."

func testBodyPromptInput() PromptInput {
	in := testPromptInput()
	in.Mode = InferMode
	in.Instruction = bodyInstruction
	in.IssueBody = bodyInstruction
	in.CommentBody = ""
	in.CommentAuthor = ""
	in.BodyIsInstruction = true
	return in
}

// TestBodyTriggeredPromptCarriesTheBodyOnlyAsTheInstruction is AC-2.25.
//
// The body is the request, so it is stated once, as the request. Repeating it
// inside a <note> that says "information to evaluate, not instructions" would
// assert both things about the same paragraph (D-N).
func TestBodyTriggeredPromptCarriesTheBodyOnlyAsTheInstruction(t *testing.T) {
	prompt := buildPrompt(t, testBodyPromptInput())

	if got := strings.Count(prompt, bodyInstruction); got != 1 {
		t.Fatalf("the issue body appears %d times, want once — under the instruction only:\n%s",
			got, prompt)
	}
	instruction := strings.Index(prompt, "## Instruction")
	if at := strings.Index(prompt, bodyInstruction); at < instruction {
		t.Errorf("the body appears at %d, before the instruction section at %d:\n%s",
			at, instruction, prompt)
	}
	if !strings.Contains(prompt, issueBodyIsTheInstruction) {
		t.Errorf("the issue note does not say where its body went:\n%s", prompt)
	}

	// One note, and it is the issue's. Counting is how that is told apart from
	// the comment note without matching on prose.
	if got := strings.Count(prompt, "<note>"); got != 1 {
		t.Errorf("got %d <note> elements, want 1 (the issue only):\n%s", got, prompt)
	}
	if got := strings.Count(prompt, noteFraming); got != 1 {
		t.Errorf("the third-party framing line appears %d times, want once for the one note", got)
	}
	for _, unwanted := range []string{
		"which triggered this run",
		"(the comment has no body)",
		"The two notes below",
		"The three notes below",
	} {
		if strings.Contains(prompt, unwanted) {
			t.Errorf("the prompt still promises a comment note (%q):\n%s", unwanted, prompt)
		}
	}
	if !strings.Contains(prompt, "The note below is the issue this run came from.") {
		t.Errorf("the context sentence does not describe the one note that is there:\n%s", prompt)
	}
}

// The gate upstream is assignment, not authorship: a body somebody else wrote
// can start a run, so the note has to keep saying whose words they are. That
// attribution is the whole reason this path is safe to take.
func TestBodyTriggeredPromptKeepsTheIssueAttribution(t *testing.T) {
	in := testBodyPromptInput()
	in.IssueAuthor = "a-stranger"
	prompt := buildPrompt(t, in)

	for _, want := range []string{
		"issue #123 on acme/acme-api, opened by @a-stranger",
		"Title: Add a widget",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the issue note is missing %q:\n%s", want, prompt)
		}
	}
}

// A run that named no command is told that choosing the kind of work is part of
// the task — and told what actually started it. Announcing a comment to a
// prompt that carries no comment note sends the run looking for one that is not
// there.
func TestBodyTriggeredInferGuidanceNamesTheBody(t *testing.T) {
	prompt := buildPrompt(t, testBodyPromptInput())

	if !strings.Contains(prompt, "started by an issue whose body named no command") {
		t.Errorf("the mode guidance does not name the issue body as the trigger:\n%s", prompt)
	}
	if strings.Contains(prompt, "started by a comment that named no command") {
		t.Errorf("the mode guidance still blames a comment that is not in the prompt:\n%s", prompt)
	}
}

// A body-triggered run with a thread behind it: two notes, and the sentence
// says which two. Bootstrap only runs a body on an issue with no comments, so
// this is the resume fallback's shape rather than the ordinary one — but the
// sentence has to describe the notes that are there on every path.
func TestBodyTriggeredPromptWithAThreadIntroducesTwoNotes(t *testing.T) {
	in := testBodyPromptInput()
	in.History = testHistory()
	prompt := buildPrompt(t, in)

	if got := strings.Count(prompt, "<note>"); got != 2 {
		t.Fatalf("got %d <note> elements, want 2 (the issue and the history):\n%s", got, prompt)
	}
	if !strings.Contains(prompt, "The two notes below are the issue this run came from and a condensed") {
		t.Errorf("the context sentence does not describe the two notes that are there:\n%s", prompt)
	}
	if strings.Contains(prompt, "which triggered this run") {
		t.Errorf("a comment note was rendered for a body-triggered run:\n%s", prompt)
	}
	if got := strings.Count(prompt, bodyInstruction); got != 1 {
		t.Errorf("the issue body appears %d times, want once", got)
	}
	if block := historyBlock(t, prompt); block == "" {
		t.Errorf("the prompt carries no history note:\n%s", prompt)
	}
}

// Resume and BodyIsInstruction compose: resume drops the issue note as
// something the conversation has already been told, and a body trigger drops
// the comment note as something that does not exist. Between them there is
// nothing left to frame, so the Context section goes with its contents rather
// than standing empty over nothing.
func TestBodyTriggeredResumeHasNoContextSection(t *testing.T) {
	in := testBodyPromptInput()
	in.Resume = true
	in.PreviousMode = InferMode
	in.RunOrdinal = 2
	prompt := buildPrompt(t, in)

	if strings.Contains(prompt, "## Context") {
		t.Errorf("the resumed body prompt has a Context section with nothing in it:\n%s", prompt)
	}
	if strings.Contains(prompt, "<note>") {
		t.Errorf("the resumed body prompt carries a note:\n%s", prompt)
	}
	for _, want := range []string{
		"# litefactory run 0002 of github:acme/acme-api#123",
		"- Mode: infer\n",
		"- Feature slug: 123-add-a-widget\n",
		"## Instruction",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the resumed body prompt is missing %q:\n%s", want, prompt)
		}
	}
	if got := strings.Count(prompt, bodyInstruction); got != 1 {
		t.Errorf("the body appears %d times, want once", got)
	}

	// Flipping Resume back is the whole of the difference, which is what the
	// resume fallback in internal/turn relies on.
	cold := in
	cold.Resume = false
	if !strings.Contains(buildPrompt(t, cold), "## Context") {
		t.Error("the same input cold has no Context section")
	}
}

// The instruction is uncapped, as every instruction is — and the pointer is
// what keeps a long body from also appearing in a capped, and therefore
// disagreeing, second copy.
func TestBodyTriggeredPromptDoesNotCapTheBody(t *testing.T) {
	in := testBodyPromptInput()
	in.Instruction = strings.Repeat("x", MaxIssueBodyBytes*2)
	in.IssueBody = in.Instruction
	prompt := buildPrompt(t, in)

	if !strings.Contains(prompt, in.Instruction) {
		t.Error("the body was cut on its way into the instruction")
	}
	if strings.Contains(prompt, truncationMarker) {
		t.Errorf("something in a body-triggered prompt was truncated:\n%s", prompt[:1024])
	}
	if got := strings.Count(prompt, in.Instruction); got != 1 {
		t.Errorf("the body appears %d times, want once", got)
	}
}

// The flag changes nothing when it is off: an ordinary comment-triggered prompt
// is byte for byte what it was before the flag existed.
func TestCommentTriggeredPromptIsUnaffectedByTheFlag(t *testing.T) {
	in := testPromptInput()
	before := buildPrompt(t, in)

	if strings.Contains(before, issueBodyIsTheInstruction) {
		t.Errorf("a comment-triggered prompt points at the instruction for its issue body:\n%s", before)
	}
	if !strings.Contains(before, "The two notes below are the issue and the comment this run came from.") {
		t.Errorf("the cold context sentence has moved:\n%s", before)
	}
}
