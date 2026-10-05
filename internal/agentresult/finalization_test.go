package agentresult_test

import (
	"os"
	"path/filepath"
	"testing"

	ar "github.com/cloudvane/litefactory/internal/agentresult"
)

func TestFinalization_ExecutableExamples(t *testing.T) {
	root := "testdata/v1"
	for _, mode := range []string{"research", "plan", "implement", "validate"} {
		for _, outcome := range []string{"completed", "incomplete", "blocked"} {
			t.Run(mode+"/"+outcome, func(t *testing.T) {
				name := mode + "-" + outcome + ".json"
				supplied, err := os.ReadFile(filepath.Join(root, name))
				if err != nil {
					t.Fatal(err)
				}
				_, ctx := createFixture(mode, outcome)
				if mode == "validate" {
					story := ctx.Stories["US-001"]
					story.SnapshotID = ""
					story.SnapshotRetained = false
					story.Checks = nil
					ctx.Stories["US-001"] = story
				}
				if _, d := ar.Create(supplied, ctx); !d.Valid {
					t.Fatalf("invalid source example: %+v", d)
				}
			})
		}
		source, err := os.ReadFile(filepath.Join(root, "updates", mode+".json"))
		if err != nil {
			t.Fatal(err)
		}
		if _, d := ar.DecodeUpdate(source); !d.Valid {
			t.Fatalf("source update invalid: %+v", d)
		}
	}
	for _, tt := range []struct {
		name, mode       string
		permission, want bool
	}{
		{"research-question.json", "research", true, true}, {"research-question.json", "research", false, false}, {"invalid-implement-question.json", "implement", true, false},
		{"invalid-validate-question.json", "validate", true, false},
	} {
		source, err := os.ReadFile(filepath.Join(root, tt.name))
		if err != nil {
			t.Fatal(err)
		}
		_, ctx := createFixture(tt.mode, "incomplete")
		ctx.QuestionsAllowed = tt.permission
		if _, d := ar.Create(source, ctx); d.Valid != tt.want {
			t.Fatalf("question permission parity: %+v", d)
		}
	}
}

func TestFinalization_QuestionExampleAcrossModesAndExits(t *testing.T) {
	raw, err := os.ReadFile("testdata/v1/research-question.json")
	if err != nil {
		t.Fatal(err)
	}
	_, researchContext := createFixture("research", "incomplete")
	example, d := ar.Create(raw, researchContext)
	if !d.Valid {
		t.Fatal("question example invalid")
	}
	for _, mode := range []string{"research", "plan", "implement", "validate", "spike"} {
		for _, outcome := range []string{"completed", "incomplete", "blocked"} {
			for _, permitted := range []bool{false, true} {
				result, ctx := createFixture(mode, outcome)
				ctx.QuestionsAllowed = permitted
				result.Questions = example.Questions
				want := permitted && (mode == "research" || mode == "plan" || mode == "spike") && outcome != "completed"
				if got := ar.ValidateResult(result, ctx); got.Valid != want {
					t.Fatalf("source question shape: mode=%s outcome=%s permission=%t diagnostics=%+v", mode, outcome, permitted, got)
				}
			}
		}
	}
}
