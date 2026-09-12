package harness

import (
	"strings"
	"testing"
)

// resumeHeader returns everything a resumed prompt says before "## Instruction",
// which is the whole of what [writeResumeHeader] produces.
func resumeHeader(t *testing.T, in PromptInput) string {
	t.Helper()

	in.Resume = true
	head, _, found := strings.Cut(buildPrompt(t, in), "\n## Instruction")
	if !found {
		t.Fatalf("resumed prompt has no instruction section:\n%s", buildPrompt(t, in))
	}
	return head
}

// TestResumeHeaderIsPinned is the characterization test required by T-A1.8
// (F-7): before A1, writeResumeHeader had no test of its own at all — nothing
// in this package set Resume at header level and asserted the bytes, on either
// of its two branches.
//
// It was written and run green against the unmodified function, so what it
// pins is the output as it stood before the permission paragraph existed. That
// is what makes AC-A1.11 assertable: when permission did not change between two
// runs, the resume header must still be exactly this.
func TestResumeHeaderIsPinned(t *testing.T) {
	t.Parallel()

	const runAndMode = `# litefactory run 0001 of github:acme/acme-api#123

- Mode: %s
- Feature slug: 123-add-a-widget
`

	tests := []struct {
		name         string
		mode         string
		previousMode string
		want         string
	}{
		{
			name:         "a mode the conversation is already in",
			mode:         "spike",
			previousMode: "spike",
			want:         strings.Replace(runAndMode, "%s", "spike", 1),
		},
		{
			name:         "a conversation that has just switched to inferring",
			mode:         InferMode,
			previousMode: "spike",
			want: strings.Replace(runAndMode, "%s", InferMode, 1) + `
This run was started by a comment that named no command, so working out what kind of work it is forms part of the task. If the supplied project instructions define modes, a routing scheme or a way of classifying a request, follow it and choose the one this request calls for. If it does not, simply do what is asked. Put any documents you produce under the feature slug named above, wherever this repository keeps them. Say which mode you chose in your final message.
`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			in := testPromptInput()
			in.Mode = tc.mode
			in.PreviousMode = tc.previousMode

			if got := resumeHeader(t, in); got != tc.want {
				t.Errorf("the resume header changed.\n--- got ---\n%s\n--- want ---\n%s", got, tc.want)
			}
		})
	}
}

// AC-A1.11. The pinned bytes above are what an unchanged permission still
// produces, in both of the two ways permission can be unchanged. This is the
// assertion that MayAsk exists without costing a byte on the path where it did
// not move — the resume prompt is the delta, and permission that did not change
// is not a delta.
func TestResumeHeaderIsSilentWhenPermissionDidNotChange(t *testing.T) {
	t.Parallel()

	const want = `# litefactory run 0001 of github:acme/acme-api#123

- Mode: research
- Feature slug: 123-add-a-widget
`

	for _, permission := range []bool{false, true} {
		in := testPromptInput()
		in.Mode = "research"
		in.PreviousMode = "research"
		in.MayAsk = permission
		in.PreviousMayAsk = permission

		if got := resumeHeader(t, in); got != want {
			t.Errorf("MayAsk = PreviousMayAsk = %t moved the resume header.\n--- got ---\n%s\n--- want ---\n%s",
				permission, got, want)
		}
	}
}

// AC-A1.12, all four combinations of the two bits.
//
// The two that differ each get a statement, and the direction matters: a
// conversation that gains permission is told it may ask, and one that loses it
// is told it may not — because on the resume path silence is not neutral. The
// prompt repeats nothing the transcript already holds, so an unstated change
// leaves the old permission standing, which fails open in the revoke direction.
func TestResumeHeaderStatesPermissionWhenItChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		mayAsk         bool
		previousMayAsk bool
		wantGrant      bool
		wantRevoke     bool
	}{
		{name: "granted", mayAsk: true, wantGrant: true},
		{name: "revoked", previousMayAsk: true, wantRevoke: true},
		{name: "still allowed", mayAsk: true, previousMayAsk: true},
		{name: "still not allowed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			in := testPromptInput()
			in.Mode = "research"
			in.PreviousMode = "research"
			in.MayAsk = tc.mayAsk
			in.PreviousMayAsk = tc.previousMayAsk
			header := resumeHeader(t, in)

			if got := strings.Contains(header, askingRule); got != tc.wantGrant {
				t.Errorf("permission granted = %t, want %t:\n%s", got, tc.wantGrant, header)
			}
			if got := strings.Contains(header, askingRevokedRule); got != tc.wantRevoke {
				t.Errorf("permission revoked = %t, want %t:\n%s", got, tc.wantRevoke, header)
			}
			// Q9(b): the answer framing rides with the revocation and nowhere
			// else. It is the case where the owner replied in plain words,
			// without re-tagging, to a run that had asked.
			answerFraming := "treat the new comment as the answer"
			if got := strings.Contains(header, answerFraming); got != tc.wantRevoke {
				t.Errorf("the hedged answer framing is present = %t, want %t:\n%s",
					got, tc.wantRevoke, header)
			}
		})
	}
}

// AC-A1.2. A tagged run is told it may ask, and is not simultaneously told it
// may not. A prompt carrying both would be settled by whichever half the model
// weighted, and litefactory would never see which.
func TestTaggedColdHeaderGrantsPermission(t *testing.T) {
	t.Parallel()

	in := testPromptInput()
	in.Mode = "research"
	in.MayAsk = true
	prompt := buildPrompt(t, in)

	for _, unwanted := range []string{
		"do not ask clarifying questions",
		"no one can answer a question",
		"Nobody will read a question",
	} {
		if strings.Contains(prompt, unwanted) {
			t.Errorf("a tagged prompt still says %q:\n%s", unwanted, prompt)
		}
	}
	for _, want := range []string{
		"permission to ask",
		// The bar, and the shape of asking. Asserted as prose because prose is
		// what the run reads; if the wording changes, whoever changed it should
		// look at whether the rule survived it.
		"cannot be settled by reading this repository",
		"Ask by stopping",
		"Batch your questions",
		// AC-A1.15 / T-A1.12: the standing invitation is on every asking prompt.
		// It is litefactory telling the owner what sentence opens the next mode,
		// which under One Run, One Mode only a human comment can do.
		"honor explicit instructions",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the tagged prompt is missing %q:\n%s", want, prompt)
		}
	}
}

// AC-A1.7, the one seam where the rule is prompt text rather than a Go branch.
//
// When the mode is infer, nothing in Go knows yet whether this run is research,
// plan or an implementation — the agent decides that from the target
// repository's CLAUDE.md, after this prompt exists. So the exception travels
// with the permission, and only when the mode is the one Go cannot resolve.
func TestInferSeamRidesWithPermission(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mode string
		// mayAsk is this run's permission. Every case here is a run that has
		// just gained it, so the paragraph is written on both paths: cold
		// because a cold prompt always states permission, resumed because
		// permission changed.
		mayAsk   bool
		wantSeam bool
	}{
		{name: "tagged and unclassified", mode: InferMode, mayAsk: true, wantSeam: true},
		{name: "tagged and already research", mode: "research", mayAsk: true},
		{name: "untagged and unclassified", mode: InferMode},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for _, resume := range []bool{false, true} {
				in := testPromptInput()
				in.Mode = tc.mode
				in.PreviousMode = tc.mode
				in.MayAsk = tc.mayAsk
				in.Resume = resume

				got := strings.Contains(buildPrompt(t, in), inferAskingSeam)
				if got != tc.wantSeam {
					t.Errorf("resume=%t: infer seam present = %t, want %t", resume, got, tc.wantSeam)
				}
			}
		})
	}
}

// AC-A1.10, and it is the reason the paragraph had to be split before it was
// branched (F-9). The gag and the commit prohibition were one paragraph;
// branching the whole of it on MayAsk would have dropped the prohibition from
// every tagged run, and litefactory commits after the agent finishes.
func TestEveryColdPromptForbidsCommitting(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"spike", "research", "plan", "implement", InferMode} {
		for _, mayAsk := range []bool{false, true} {
			in := testPromptInput()
			in.Mode = mode
			in.MayAsk = mayAsk
			if !strings.Contains(buildPrompt(t, in), worktreeRule) {
				t.Errorf("mode %q, MayAsk=%t: the cold prompt does not forbid committing or pushing", mode, mayAsk)
			}
		}
	}
}

// AC-A1.14. Q8 put the judgement of when questions are answered at agent level:
// the orchestrator decides, and must honour a direct instruction to move on. A
// Go-side round counter, cap or timeout is a *failing* outcome, not a missing
// feature, and nothing here implements one — the permission is a bit, and the
// prompt says how to use it.
func TestNothingCountsAskingRounds(t *testing.T) {
	t.Parallel()

	in := testPromptInput()
	in.Mode = "research"
	in.MayAsk = true
	prompt := buildPrompt(t, in)

	for _, unwanted := range []string{"at most", "no more than", "rounds", "questions left"} {
		if strings.Contains(strings.ToLower(prompt), unwanted) {
			t.Errorf("the asking prompt caps something with %q; Q8 puts that at agent level:\n%s",
				unwanted, prompt)
		}
	}
}
