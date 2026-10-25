package turn

import (
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestOwnerInputPromptUsesSavedAssignmentWithoutRepeatingTrigger(t *testing.T) {
	f := newFixture(t, nil)
	at := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		id   int64
		body string
	}{{31, "  Preserve these exact queued bytes.  "}, {32, "This is already the current instruction."}} {
		if _, err := f.db.RecordComment(t.Context(), store.Comment{SessionID: f.session.ID, ForgeCommentID: c.id, AuthorUserID: 42, Body: c.body, CreatedAt: at, Classification: store.ClassificationInstruction}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.RetainOwnerInstruction(t.Context(), f.session.ID, c.id, 42, c.body, store.OwnerInputTarget{}); err != nil {
			t.Fatal(err)
		}
	}
	definition, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "core", Mode: "implement", ProfileVersion: 1, Path: "implement/implement.dot", Digest: strings.Repeat("a", 64), CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: definition.ID, CurrentNode: "worker", StartedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	segment, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, store.FirstSegmentOrdinal, "test", store.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: segment.ID, Mode: "implement", Agent: store.AgentClaude, TriggerCommentID: 32, LogPath: func(int) string { return "/logs/owner-input" }})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: "worker", RunID: run.ID, AcceptOwnerInput: true, StartedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	section, err := graphOwnerInputSection(t.Context(), f.db, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(section, "  Preserve these exact queued bytes.  ") != 1 || strings.Count(section, "This is already the current instruction.") != 1 {
		t.Fatalf("wrong owner input section: %s", section)
	}
	resumed, err := graphOwnerInputSection(t.Context(), f.db, attempt.ID)
	if err != nil || resumed != section {
		t.Fatalf("replay changed saved input %q %v", resumed, err)
	}
	req := Request{researchInstance: instance.ID, Trigger: Trigger{CommentID: 32, Body: "This is already the current instruction."}, Command: Command{Kind: KindFreeText, Text: "This is already the current instruction."}}
	instruction, err := ownerInstructionForStep(t.Context(), f.db, req, f.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	in := harness.PromptInput{Ref: naming.Ref{Forge: naming.ForgeGitHub, Owner: "acme", Repo: "acme-api", Issue: 123}, FeatureSlug: "123-owner-input", Branch: "lf/123-owner-input", Mode: "implement", RunOrdinal: 1, Instruction: instruction}
	prompt, err := harness.BuildPrompt(in)
	if err != nil {
		t.Fatal(err)
	}
	prompt += section
	if strings.Count(prompt, "This is already the current instruction.") != 1 {
		t.Fatalf("first step repeated trigger as current instruction: %s", prompt)
	}
	if err = f.db.CompleteGraphNodeAttempt(t.Context(), attempt.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "next", FinishedAt: at, Transition: &store.GraphTransition{InstanceID: instance.ID, FromNode: "worker", ToNode: "reviewer", OnOutcome: "next", At: at}}); err != nil {
		t.Fatal(err)
	}
	second, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: "reviewer", RunID: run.ID, AcceptOwnerInput: true, StartedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	secondSection, err := graphOwnerInputSection(t.Context(), f.db, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	secondInstruction, err := ownerInstructionForStep(t.Context(), f.db, req, f.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	in.Instruction = secondInstruction
	in.RunOrdinal = 2
	in.Resume = true
	secondPrompt, err := harness.BuildPrompt(in)
	if err != nil {
		t.Fatal(err)
	}
	secondPrompt += secondSection
	if strings.Contains(secondPrompt, "This is already the current instruction.") || strings.Contains(secondPrompt, "Preserve these exact queued bytes.") {
		t.Fatalf("later agent repeated one-time input as current direction: %s", secondPrompt)
	}

}
