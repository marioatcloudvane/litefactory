package graphexec_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/graphexec"
)

// The remaining request faults each name the member to correct, and the section
// rule is per stage: the story stage may author only intent, dependencies and
// conversation, the technical stage may not author acceptance sections, and the
// acceptance stage is unrestricted.
func TestPlanStoryRequest_FaultsNameTheMemberToCorrect(t *testing.T) {
	t.Parallel()
	rev := strings.Repeat("a", 64)
	base := graphexec.PlanStoryRequest{RequestID: "r", Operation: "update", Stage: "technical", Candidate: "c", SharedRevision: "s", ExpectedRevision: rev}
	cases := []struct {
		name   string
		mutate func(*graphexec.PlanStoryRequest)
		field  string
		advice string // empty means no fault
	}{
		{"update is valid", func(*graphexec.PlanStoryRequest) {}, "", ""},
		{"update without a revision", func(r *graphexec.PlanStoryRequest) { r.ExpectedRevision = "" }, "expected_revision", "set expected_revision"},
		{"retire with sections", func(r *graphexec.PlanStoryRequest) { r.Operation, r.ChangedSections = "retire", []string{"intent"} }, "changed_sections", "remove changed_sections"},
		{"retire with replacements is valid", func(r *graphexec.PlanStoryRequest) { r.Operation, r.Replacements = "retire", []string{"story-002"} }, "", ""},
		{"replacements on update", func(r *graphexec.PlanStoryRequest) { r.Replacements = []string{"story-002"} }, "replacements", "use operation retire"},
		{"blank replacement", func(r *graphexec.PlanStoryRequest) { r.Operation, r.Replacements = "retire", []string{" "} }, "replacements", ""},
		{"unknown operation", func(r *graphexec.PlanStoryRequest) { r.Operation = "promote" }, "operation", "create, update or retire"},
		{"unknown stage", func(r *graphexec.PlanStoryRequest) { r.Stage = "review" }, "stage", "story, technical or acceptance"},
		{"blank shared revision", func(r *graphexec.PlanStoryRequest) { r.SharedRevision = "" }, "shared_revision", ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := base
			tt.mutate(&r)
			faults := r.Faults()
			if tt.field == "" {
				if len(faults) != 0 {
					t.Fatalf("valid request refused: %v", faults)
				}
				return
			}
			for _, f := range faults {
				if f.Field == tt.field && strings.Contains(f.Message, tt.advice) {
					return
				}
			}
			t.Fatalf("no fault for %q containing %q: %v", tt.field, tt.advice, faults)
		})
	}
}

func TestPlanSectionFaults_ArePerStage(t *testing.T) {
	t.Parallel()
	cases := []struct {
		stage    string
		sections []string
		want     []string // sections named in a fault, in order
	}{
		{"story", []string{"intent", "dependencies", "conversation"}, nil},
		{"story", []string{"intent", "architecture", "criteria"}, []string{"architecture", "criteria"}},
		{"technical", []string{"architecture", "guidance", "design", "conversation"}, nil},
		{"technical", []string{"criteria", "architecture", "tasks"}, []string{"criteria", "tasks"}},
		{"acceptance", []string{"criteria", "tasks", "architecture"}, nil},
		{"story", nil, nil},
	}
	for _, tt := range cases {
		t.Run(tt.stage+"/"+strings.Join(tt.sections, ","), func(t *testing.T) {
			t.Parallel()
			faults := graphexec.PlanSectionFaults(tt.stage, tt.sections)
			if len(faults) != len(tt.want) {
				t.Fatalf("faults = %v, want sections %v", faults, tt.want)
			}
			for i, f := range faults {
				if f.Field != "changed_sections" || !strings.Contains(f.Message, `"`+tt.want[i]+`"`) || !strings.Contains(f.Message, "remove") {
					t.Errorf("fault %d = %+v, want it to tell what to remove and name %q", i, f, tt.want[i])
				}
			}
		})
	}
}

func TestPlanSectionFaults_UnknownSectionListsValidNames(t *testing.T) {
	t.Parallel()
	faults := graphexec.PlanSectionFaults("technical", []string{"bogus", "design"})
	if len(faults) != 1 || faults[0].Field != "changed_sections" || !strings.Contains(faults[0].Message, `"bogus"`) {
		t.Fatalf("faults = %+v, want one fault naming bogus", faults)
	}
	for _, name := range []string{"intent", "architecture", "criteria", "tasks", "dependencies", "design", "guidance", "conversation"} {
		if !strings.Contains(faults[0].Message, name) {
			t.Errorf("message %q does not list %q", faults[0].Message, name)
		}
	}
}
