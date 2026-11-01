package turn

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/taskdiscussion"
)

func TestTaskAdvisoryResultRejectsWorkflowAuthority(t *testing.T) {
	valid := `{"answer":"The saved result supports this approach.","sources":["task.json"],"gaps":[]}`
	if _, err := decodeTaskAdvisoryAnswer([]byte(valid)); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"sources":["task.json"],"gaps":[]}`,
		`{"answer":null,"sources":["task.json"],"gaps":[]}`,
		`{"answer":"a","gaps":[]}`,
		`{"answer":"a","sources":null,"gaps":[]}`,
		`{"answer":"a","sources":["task.json"]}`,
		`{"answer":"a","sources":["task.json"],"gaps":null}`,
		`{"answer":"a","sources":["task.json"],"gaps":[],"summary_suggestion":null}`,

		`{"answer":"a","answer":"b","sources":["task.json"],"gaps":[]}`,
		`{"answer":"a","Answer":"b","sources":["task.json"],"gaps":[]}`,
		`{"answer":"a","sources":["task.json"],"gaps":[],"summary_suggestion":{"Question":"q","conclusion":"c","alternatives_or_uncertainty":"u","evidence_basis":"e","attribution":"a"}}`,
		`{"answer":"a","sources":["task.json"],"gaps":[],"approval":true}`,
		`{"answer":"a","sources":[],"gaps":[]}`,
		`{"answer":"a","sources":["task.json"],"gaps":[],"summary_suggestion":{"question":"q"}}`,
	} {
		if _, err := decodeTaskAdvisoryAnswer([]byte(raw)); err == nil {
			t.Fatalf("accepted unauthorized or incomplete result: %s", raw)
		}
	}
}
func TestTaskAdvisoryCapturedClosureAndWorkspace(t *testing.T) {
	raw, err := CaptureTaskAdvisoryGuidance(t.Context(), config.Paths{}, "Captured analytical facts", "")
	if err != nil {
		t.Fatal(err)
	}
	var guidance TaskAdvisoryGuidance
	if err = json.Unmarshal(raw, &guidance); err != nil {
		t.Fatal(err)
	}
	if len(guidance.Advisory) == 0 {
		t.Fatal("missing immutable advisory capture")
	}
	runner := &TaskAdvisoryRunner{}
	workspace, err := runner.openTaskAdvisoryWorkspace(t.Context(), taskdiscussion.RunRequest{Basis: taskdiscussion.Basis{Evidence: json.RawMessage(`{}`)}, Context: json.RawMessage(`[]`)}, guidance)
	if err != nil {
		t.Fatal(err)
	}
	if workspace.gap == "" {
		t.Fatal("missing repository gap")
	}
	if _, err = os.Stat(workspace.repo); err != nil {
		t.Fatal(err)
	}
	if err = workspace.close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(workspace.root); !os.IsNotExist(err) {
		t.Fatal("workspace was not removed")
	}
}
