package store

import (
	"encoding/json"
	"testing"

	"github.com/cloudvane/litefactory/internal/ticket"
)

func linearVersion(bug string) ConnectionVersion {
	return ConnectionVersion{Provider: ticket.Linear, Origin: "https://api.linear.app", Destination: "team/project", Linear: LinearConfiguration{WorkspaceID: "w", TeamID: "team", ProjectID: "project", AuthKind: "api_key", ModeLabelsJSON: `{"plan":"label-plan"}`, LifecycleStatesJSON: `{"completed":"done","retired":"cancelled"}`, BugLabelID: bug}}
}

func TestLinearBugLabelStorageAndValidation(t *testing.T) {
	raw, err := json.Marshal(linearVersion("").Linear)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"WorkspaceID":"w","TeamID":"team","ProjectID":"project","AuthKind":"api_key","ModeLabelsJSON":"{\"plan\":\"label-plan\"}","LifecycleStatesJSON":"{\"completed\":\"done\",\"retired\":\"cancelled\"}"}`
	if string(raw) != want {
		t.Fatalf("unset config bytes changed: %s", raw)
	}
	for _, tt := range []struct {
		name, id string
		ok       bool
	}{{"unset", "", true}, {"set", "bug-label", true}, {"equals mode label", "label-plan", false}, {"leading space", " bug", false}, {"inner space", "bu g", false}, {"newline", "bug\n", false}, {"nul", "b\x00g", false}} {
		if err := validateLinearConnection(linearVersion(tt.id)); (err == nil) != tt.ok {
			t.Errorf("%s: err=%v", tt.name, err)
		}
	}
}
