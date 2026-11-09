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

// headerFacts returns the "- Key: value" lines of a header as a map. What the
// facts mean for the agent lives in the run-rules skill; these tests only
// check that the right facts are stated.
func headerFacts(header string) map[string]string {
	facts := map[string]string{}
	for _, line := range strings.Split(header, "\n") {
		if key, value, ok := strings.Cut(strings.TrimPrefix(line, "- "), ": "); ok && strings.HasPrefix(line, "- ") {
			facts[key] = value
		}
	}
	return facts
}

// The resume header is the delta: run, mode, slug and the question
// permission, and nothing the conversation was already told.
func TestResumeHeaderStatesOnlyTheDelta(t *testing.T) {
	t.Parallel()

	in := testPromptInput()
	in.Mode = "spike"
	in.PreviousMode = "spike"
	header := resumeHeader(t, in)
	if !strings.HasPrefix(header, "# litefactory run 0001 of github:acme/acme-api#123\n") {
		t.Fatalf("resume header lost its run line:\n%s", header)
	}
	facts := headerFacts(header)
	for key, want := range map[string]string{"Mode": "spike", "Feature slug": "123-add-a-widget", "Questions": "not allowed"} {
		if facts[key] != want {
			t.Errorf("%s = %q, want %q:\n%s", key, facts[key], want, header)
		}
	}
	for _, absent := range []string{"Task", "Working directory", "Questions changed", "Previous run", "Request"} {
		if _, ok := facts[absent]; ok {
			t.Errorf("resume header repeats %q:\n%s", absent, header)
		}
	}
}

// AC-A1.12: on the resume path silence is not neutral, so a permission change
// is named in both directions, and an unchanged permission is not.
func TestResumeHeaderNamesAPermissionChange(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		mayAsk, previousMayAsk bool
	}{{true, false}, {false, true}, {true, true}, {false, false}} {
		in := testPromptInput()
		in.Mode = "research"
		in.PreviousMode = "research"
		in.MayAsk = tc.mayAsk
		in.PreviousMayAsk = tc.previousMayAsk
		facts := headerFacts(resumeHeader(t, in))

		want := "not allowed"
		if tc.mayAsk {
			want = "allowed"
		}
		if facts["Questions"] != want {
			t.Errorf("MayAsk=%t: Questions = %q", tc.mayAsk, facts["Questions"])
		}
		if _, changed := facts["Questions changed"]; changed != (tc.mayAsk != tc.previousMayAsk) {
			t.Errorf("MayAsk=%t PreviousMayAsk=%t: change named = %t", tc.mayAsk, tc.previousMayAsk, changed)
		}
	}
}

// Every cold prompt states the question permission as a fact.
func TestColdHeaderStatesQuestionPermission(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"spike", "research", "plan", "implement", InferMode} {
		for _, mayAsk := range []bool{false, true} {
			in := testPromptInput()
			in.Mode = mode
			in.MayAsk = mayAsk
			head, _, _ := strings.Cut(buildPrompt(t, in), "\n## Instruction")
			want := "not allowed"
			if mayAsk {
				want = "allowed"
			}
			if got := headerFacts(head)["Questions"]; got != want {
				t.Errorf("mode %q, MayAsk=%t: Questions = %q", mode, mayAsk, got)
			}
		}
	}
}

// Infer mode names what started the run: on the cold path always, on the
// resume path only when the conversation has just switched to inferring.
func TestInferModeNamesItsRequest(t *testing.T) {
	t.Parallel()

	in := testPromptInput()
	in.Mode = InferMode
	head, _, _ := strings.Cut(buildPrompt(t, in), "\n## Instruction")
	if headerFacts(head)["Request"] == "" {
		t.Errorf("cold infer header names no request:\n%s", head)
	}

	in.PreviousMode = "spike"
	if headerFacts(resumeHeader(t, in))["Request"] == "" {
		t.Error("resume header after a switch to infer names no request")
	}
	in.PreviousMode = InferMode
	if _, ok := headerFacts(resumeHeader(t, in))["Request"]; ok {
		t.Error("resume header repeats the infer request it already stated")
	}

	in = testPromptInput()
	in.Mode = "research"
	head, _, _ = strings.Cut(buildPrompt(t, in), "\n## Instruction")
	if _, ok := headerFacts(head)["Request"]; ok {
		t.Errorf("a resolved mode names an infer request:\n%s", head)
	}
}
