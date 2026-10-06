package planbrief

import (
	"slices"
	"testing"
)

func TestTaskOwnerScopeUsesBuiltinImplementSpecialists(t *testing.T) {
	t.Parallel()
	want := []string{"go-implementation-engineer", "go-unit-test-engineer", "go-integration-test-engineer"}
	got := TaskOwnerScope()
	if !slices.Equal(got.Roles, want) || got.IssueRefs == nil || len(got.IssueRefs) != 0 {
		t.Fatalf("task owner scope %+v, want built-in specialists %v with no external issues", got, want)
	}
	got.Roles[0] = "changed"
	if !slices.Equal(TaskOwnerScope().Roles, want) {
		t.Fatal("TaskOwnerScope shares its role list with callers")
	}
}
