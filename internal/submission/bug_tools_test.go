package submission_test

import (
	"encoding/json"
	"testing"

	"github.com/cloudvane/litefactory/internal/submission"
)

// Story-003 AC-010: the two self-check tools install only through
// NewBugFixerToolProfile, never through the plain bug workflow catalog the
// bug-analyst profile uses.
func TestNewWorkflowToolProfile_BugCatalogExcludesSelfCheckTools(t *testing.T) {
	t.Parallel()
	profile, err := submission.NewWorkflowToolProfile("bug", "")
	if err != nil {
		t.Fatal(err)
	}
	got := profile.List()
	names := make([]string, 0, len(got))
	for _, tool := range got {
		names = append(names, tool.Name)
	}
	want := []string{"communication_preflight", "communication_receipt", "communication_submit"}
	if len(names) != len(want) {
		t.Fatalf("bug workflow catalog=%v want=%v", names, want)
	}
	for _, n := range names {
		found := false
		for _, w := range want {
			if n == w {
				found = true
			}
		}
		if !found {
			t.Fatalf("unexpected tool %q in plain bug catalog", n)
		}
	}
	for _, forbidden := range []string{"bug_self_check_start", "bug_self_check_status"} {
		if profile.Allows(forbidden) {
			t.Fatalf("plain bug workflow profile allows %q", forbidden)
		}
	}
}

func TestNewBugFixerToolProfile_AddsSelfCheckTools(t *testing.T) {
	t.Parallel()
	profile := submission.NewBugFixerToolProfile()
	got := profile.List()
	names := make(map[string]submission.ToolSpec, len(got))
	for _, tool := range got {
		names[tool.Name] = tool
	}
	for _, base := range []string{"communication_preflight", "communication_receipt", "communication_submit"} {
		if _, ok := names[base]; !ok {
			t.Fatalf("bug-fixer profile lost base tool %q: %v", base, names)
		}
	}
	start, ok := names["bug_self_check_start"]
	if !ok {
		t.Fatal("bug-fixer profile missing bug_self_check_start")
	}
	if start.Effect != "input" {
		t.Fatalf("bug_self_check_start effect=%q want input", start.Effect)
	}
	if !json.Valid(start.InputSchema) || string(start.InputSchema) != `{"type":"object","properties":{},"additionalProperties":false}` {
		t.Fatalf("bug_self_check_start schema takes arguments: %s", start.InputSchema)
	}
	status, ok := names["bug_self_check_status"]
	if !ok {
		t.Fatal("bug-fixer profile missing bug_self_check_status")
	}
	if status.Effect != "read" {
		t.Fatalf("bug_self_check_status effect=%q want read", status.Effect)
	}
	var schema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(status.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "run_id" {
		t.Fatalf("bug_self_check_status does not require run_id: %+v", schema.Required)
	}
	if len(got) != 5 {
		t.Fatalf("bug-fixer profile has %d tools, want exactly 5: %v", len(got), got)
	}
	for _, name := range []string{"bug_self_check_start", "bug_self_check_status"} {
		if !profile.Allows(name) {
			t.Fatalf("bug-fixer profile does not allow its own advertised tool %q", name)
		}
	}
}

// The zero-value profile and an unrelated mode profile never gain the bug
// self-check tools by accident.
func TestNewBugFixerToolProfile_IsolatedFromOtherProfiles(t *testing.T) {
	t.Parallel()
	implement, err := submission.NewToolProfile("implement")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bug_self_check_start", "bug_self_check_status"} {
		if implement.Allows(name) {
			t.Fatalf("implement profile allows bug self-check tool %q", name)
		}
	}
}
