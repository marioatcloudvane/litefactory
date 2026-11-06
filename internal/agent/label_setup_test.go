package agent

import (
	"reflect"
	"testing"
)

func names(ws []WantedLabel) []string {
	var out []string
	for _, w := range ws {
		out = append(out, w.Name)
	}
	return out
}

func TestPlanLabels(t *testing.T) {
	wanted := WantedLabels([]string{"claude", "codex"})
	all := names(wanted)
	tests := []struct {
		name     string
		existing []string
		adopted  []LabelAdoption
		create   []string
		warnings int
	}{
		{"adopt exact", []string{"Mode: Plan", "bug"}, []LabelAdoption{{"Mode: Plan", "Mode: Plan"}, {"bug", "bug"}}, without(all, "Mode: Plan", "bug"), 0},
		{"case-different adopted unchanged", []string{"mode: plan"}, []LabelAdoption{{"Mode: Plan", "mode: plan"}}, without(all, "Mode: Plan"), 1},
		{"nothing existing creates all in order", nil, nil, all, 0},
		{"nothing missing", all, adoptAll(all), nil, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan := PlanLabels(wanted, tc.existing)
			if !reflect.DeepEqual(plan.Adopted, tc.adopted) {
				t.Fatalf("adopted = %v, want %v", plan.Adopted, tc.adopted)
			}
			if got := names(plan.Create); !reflect.DeepEqual(got, tc.create) {
				t.Fatalf("create = %v, want %v", got, tc.create)
			}
			if len(plan.Warnings) != tc.warnings {
				t.Fatalf("warnings = %v, want %d", plan.Warnings, tc.warnings)
			}
			for _, c := range plan.Create {
				for _, a := range plan.Adopted {
					if a.Wanted == c.Name {
						t.Fatalf("%q both adopted and created", c.Name)
					}
				}
			}
		})
	}
}

func without(all []string, drop ...string) []string {
	var out []string
	for _, a := range all {
		keep := true
		for _, d := range drop {
			if a == d {
				keep = false
			}
		}
		if keep {
			out = append(out, a)
		}
	}
	return out
}

func adoptAll(all []string) []LabelAdoption {
	var out []LabelAdoption
	for _, a := range all {
		out = append(out, LabelAdoption{a, a})
	}
	return out
}

func TestReaderAcceptsLabel(t *testing.T) {
	claude := WantedLabel{Name: "Coding Agent: Claude", Family: LabelFamilyAgent}
	plan := WantedLabel{Name: "Mode: Plan", Family: LabelFamilyMode}
	bug := WantedLabel{Name: "bug", Family: LabelFamilyBug}
	tests := []struct {
		name     string
		w        WantedLabel
		existing string
		want     bool
	}{
		{"mode exact", plan, "Mode: Plan", true},
		{"mode lowercase refused", plan, "mode: plan", false},
		{"bug exact", bug, "bug", true},
		{"bug capitalised refused", bug, "Bug", false},
		{"agent exact", claude, "Coding Agent: Claude", true},
		{"agent upper value accepted", claude, "Coding Agent: CLAUDE", true},
		{"agent lowercase value refused", claude, "Coding Agent: claude", false},
		{"agent lowercase prefix refused", claude, "coding agent: Claude", false},
		{"agent other runner refused", claude, "Coding Agent: Codex", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ok, reason := ReaderAcceptsLabel(tc.w, tc.existing)
			if ok != tc.want {
				t.Fatalf("accepts = %v (%s), want %v", ok, reason, tc.want)
			}
			if !ok && reason == "" {
				t.Fatal("refusal has no reason")
			}
		})
	}
}
