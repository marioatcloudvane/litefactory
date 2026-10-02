package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func validationFixture() (*ValidationContext, ValidationOutput) {
	s := ValidationStory{Story: ApprovedStory{Key: "US-1", IssueID: 12, Revision: strings.Repeat("a", 64)}, Criteria: []string{"AC-1"}, Snapshot: strings.Repeat("b", 64), EvidenceStatus: "satisfied"}
	v := &ValidationContext{Version: 1, Parent: "github:o/r#1", Generation: 1, Stories: []ValidationStory{s}}
	r := ValidationResult{Key: s.Story.Key, IssueID: 12, Revision: s.Story.Revision, Snapshot: s.Snapshot, Criteria: []ValidationFinding{{Name: "AC-1", Status: "satisfied", Classification: "blocking", Evidence: "retained test #1 and source"}}}
	for _, name := range []string{"code", "architecture", "plan"} {
		r.Reviews = append(r.Reviews, ValidationFinding{Name: name, Status: "satisfied", Classification: "blocking", Evidence: "reviewed retained source"})
	}
	return v, ValidationOutput{Version: 1, Parent: v.Parent, Generation: 1, Stories: []ValidationResult{r}}
}
func TestValidationRejectsIncompleteOrForgedResults(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*ValidationOutput)
	}{
		{"identity", func(o *ValidationOutput) { o.Stories[0].IssueID++ }},
		{"revision", func(o *ValidationOutput) { o.Stories[0].Revision = "other" }},
		{"snapshot", func(o *ValidationOutput) { o.Stories[0].Snapshot = "other" }},
		{"generation", func(o *ValidationOutput) { o.Generation++ }},
		{"unknown story", func(o *ValidationOutput) { o.Stories[0].Key = "US-X" }},
		{"missing criterion", func(o *ValidationOutput) { o.Stories[0].Criteria = nil }},
		{"duplicate criterion", func(o *ValidationOutput) {
			o.Stories[0].Criteria = append(o.Stories[0].Criteria, o.Stories[0].Criteria[0])
		}},
		{"missing review", func(o *ValidationOutput) { o.Stories[0].Reviews = nil }},
		{"waiver", func(o *ValidationOutput) { o.Stories[0].Criteria[0].Status = "waived" }},
		{"missing proof", func(o *ValidationOutput) { o.Stories[0].Criteria[0].Evidence = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, o := validationFixture()
			tc.mutate(&o)
			b, _ := json.Marshal(o)
			if _, err := ParseValidationOutput(b, v); err == nil {
				t.Fatal("accepted invalid output")
			}
		})
	}
	v, o := validationFixture()
	b, _ := json.Marshal(o)
	for _, bad := range [][]byte{append(b, []byte(" {}")...), []byte(`{"version":1,"version":1}`), []byte(strings.Repeat("x", MaxValidationBytes+1))} {
		if _, err := ParseValidationOutput(bad, v); err == nil {
			t.Fatal("accepted malformed output")
		}
	}
}
func TestValidationEvidenceFloorsAndSuggestions(t *testing.T) {
	for _, status := range []string{"satisfied", "unverified", "failed"} {
		t.Run(status, func(t *testing.T) {
			v, o := validationFixture()
			v.Stories[0].EvidenceStatus = status
			v.Stories[0].EvidenceReason = "retained check attribution"
			b, _ := json.Marshal(o)
			out, err := ParseValidationOutput(b, v)
			if err != nil {
				t.Fatal(err)
			}
			want := status
			if status == "failed" {
				want = "unmet"
			}
			if out.Stories[0].Criteria[0].Status != want {
				t.Fatal(out)
			}
			if status != "failed" && out.ImplementSuggestion() != "" {
				t.Fatal("speculative suggestion")
			}
		})
	}
	v, o := validationFixture()
	o.Stories[0].Reviews[0].Status = "unmet"
	o.Stories[0].Reviews[0].Classification = "advisory"
	b, _ := json.Marshal(o)
	out, err := ParseValidationOutput(b, v)
	if err != nil || out.ImplementSuggestion() != "" {
		t.Fatal("advisory blocks", err)
	}
}
func TestValidationContextCloneOwnsNestedEvidence(t *testing.T) {
	v, _ := validationFixture()
	v.Stories[0].Story.Dependencies = []string{"US-0"}
	v.Stories[0].Evidence = &StoryEvidence{Files: []StorySnapshotFile{{Content: []byte("original")}}}
	c := v.Clone()
	c.Stories[0].Criteria[0] = "changed"
	c.Stories[0].Story.Dependencies[0] = "changed"
	c.Stories[0].Evidence.Files[0].Content[0] = 'X'
	if v.Stories[0].Criteria[0] != "AC-1" || v.Stories[0].Story.Dependencies[0] != "US-0" || string(v.Stories[0].Evidence.Files[0].Content) != "original" {
		t.Fatal("shared mutable context")
	}
}

func TestValidationRejectsUnknownOrAmbiguousObligations(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*ValidationOutput)
	}{
		{"unknown criterion", func(o *ValidationOutput) { o.Stories[0].Criteria[0].Name = "AC-unapproved" }},
		{"unknown review", func(o *ValidationOutput) { o.Stories[0].Reviews[0].Name = "security-override" }},
		{"duplicate review", func(o *ValidationOutput) { o.Stories[0].Reviews[1] = o.Stories[0].Reviews[0] }},
		{"duplicate story", func(o *ValidationOutput) { o.Stories = append(o.Stories, o.Stories[0]) }},
		{"missing story", func(o *ValidationOutput) { o.Stories = nil }},
		{"unsupported status", func(o *ValidationOutput) { o.Stories[0].Criteria[0].Status = "approved" }},
		{"unsupported classification", func(o *ValidationOutput) { o.Stories[0].Reviews[0].Classification = "optional" }},
		{"wrong parent", func(o *ValidationOutput) { o.Parent = "github:other/repo#2" }},
		{"wrong version", func(o *ValidationOutput) { o.Version++ }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v, o := validationFixture()
			tc.mutate(&o)
			data, err := json.Marshal(o)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ParseValidationOutput(data, v); err == nil {
				t.Fatal("unapproved or ambiguous obligation accepted")
			}
		})
	}
}

func TestValidationRejectsMutationAuthorityAndNestedDuplicateKeys(t *testing.T) {
	v, o := validationFixture()
	data, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, data string }{
		{"target mutation", strings.Replace(string(data), `"version":1`, `"close_issue":true,"version":1`, 1)},
		{"owner waiver", strings.Replace(string(data), `"status":"satisfied"`, `"owner_waiver":true,"status":"satisfied"`, 1)},
		{"duplicate finding status", strings.Replace(string(data), `"status":"satisfied"`, `"status":"unmet","status":"satisfied"`, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseValidationOutput([]byte(tc.data), v); err == nil {
				t.Fatal("untrusted authority or ambiguity accepted")
			}
		})
	}
}

func TestValidationConcreteBlockingFindingSuggestsAffectedScope(t *testing.T) {
	v, o := validationFixture()
	o.Stories[0].Reviews[0].Status = "unmet"
	o.Stories[0].Reviews[0].Evidence = "retained source omits required cancellation handling"
	data, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	out, err := ParseValidationOutput(data, v)
	if err != nil {
		t.Fatal(err)
	}
	got := out.ImplementSuggestion()
	for _, want := range []string{"US-1", "code", "retained source omits required cancellation handling"} {
		if !strings.Contains(got, want) {
			t.Fatalf("suggestion %q omits concrete scope/finding %q", got, want)
		}
	}
}

func TestValidationAcceptsAWrappedObjectButNotASecondOne(t *testing.T) {
	v, o := validationFixture()
	object, _ := json.Marshal(o)
	for _, tc := range []struct {
		name string
		text string
	}{
		{"bare, as the contract asks", string(object)},
		{"leading and trailing whitespace", "\n\n  " + string(object) + "\n"},
		{"the run 20 shape: prose then a fenced block",
			"Based on direct inspection of the repository code against each story's frozen acceptance criteria, here is the structured validation result.\n\n```json\n" + string(object) + "\n```"},
		{"an unlabelled fence", "```\n" + string(object) + "\n```"},
		{"a trailing remark after the fence",
			"```json\n" + string(object) + "\n```\n\nHappy to expand on any finding."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := ParseValidationOutput([]byte(tc.text), v)
			if err != nil {
				t.Fatalf("rejected a wrapped assessment: %v", err)
			}
			if out.Parent != v.Parent || len(out.Stories) != 1 || out.Stories[0].Key != "US-1" {
				t.Fatalf("decoded the wrong object: %+v", out)
			}
		})
	}
	// Tolerating the envelope must not tolerate ambiguity about which object is
	// the assessment, nor weaken any check applied to the object itself.
	for _, tc := range []struct {
		name string
		text string
	}{
		{"two objects in one message", "Here it is:\n\n```json\n" + string(object) + "\n```\n\nAnd again:\n\n```json\n" + string(object) + "\n```"},
		{"prose with no object at all", "I could not complete the review; the retained evidence was unreadable."},
		{"an object that is not an assessment", "Result:\n\n```json\n{\"note\":\"unfinished\"}\n```"},
		{"trailing data inside the fence", "```json\n" + string(object) + " {}\n```"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseValidationOutput([]byte(tc.text), v); err == nil {
				t.Fatal("accepted an ambiguous or invalid assessment")
			}
		})
	}
}

func TestValidationGapsPreserveFacts(t *testing.T) {
	t.Parallel()
	for _, classification := range []string{"blocking", "advisory", "gap"} {
		t.Run(classification, func(t *testing.T) {
			v, o := validationFixture()
			o.Stories[0].Criteria[0].Status = "unverified"
			o.Stories[0].Criteria[0].Classification = classification
			o.Stories[0].Reviews[0].Status = "unverified"
			data, err := json.Marshal(o)
			if err != nil {
				t.Fatal(err)
			}
			got, err := ParseValidationOutput(data, v)
			if err != nil {
				t.Fatal(err)
			}
			if got.Stories[0].Criteria[0].Classification != "gap" || got.Stories[0].Reviews[0].Classification != "gap" || got.ImplementSuggestion() != "" {
				t.Fatalf("missing evidence treated as a defect: %+v", got)
			}
		})
	}
	v, o := validationFixture()
	v.Stories[0].EvidenceStatus = "unverified"
	o.Stories[0].Criteria[0].Status = "unmet"
	o.Stories[0].Criteria[0].Evidence = "required cancellation is absent in retained source"
	data, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseValidationOutput(data, v)
	if err != nil {
		t.Fatal(err)
	}
	if got.Stories[0].Criteria[0].Status != "unmet" || got.ImplementSuggestion() == "" {
		t.Fatal("missing tests erased concrete defect")
	}
	for _, status := range []string{"satisfied", "unmet"} {
		o.Stories[0].Criteria[0].Status = status
		o.Stories[0].Criteria[0].Classification = "gap"
		data, err := json.Marshal(o)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ParseValidationOutput(data, v); err == nil {
			t.Fatalf("%s disguised as gap", status)
		}
	}
}
