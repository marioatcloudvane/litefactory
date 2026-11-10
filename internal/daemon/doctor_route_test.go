package daemon_test

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

// The owner's /doctor reaches a Doctor turn, beside the task's own workflow,
// with the owner's guidance as the instruction.
func TestOwnerDoctorCommandStartsTheDoctor(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle()

	f.forge.addComment(testIssue, 90, owner(), "/doctor why did the last run stop?")
	f.settle()

	calls := f.turns.calls()
	if len(calls) != 1 || calls[0].Mode != agent.ModeDoctor || calls[0].Workflow != agent.WorkflowDoctor || calls[0].Command.Text != "why did the last run stop?" {
		t.Fatalf("doctor turn: %+v", calls)
	}
}

// Anyone but the owner cannot start the Doctor.
func TestStrangerDoctorCommandStartsNothing(t *testing.T) {
	f := newFixture(t)
	f.plantCursor()
	f.issue("Add a /health endpoint")
	f.settle()

	f.forge.addComment(testIssue, 90, stranger(), "/doctor reset everything")
	f.settle()

	if calls := f.turns.calls(); len(calls) != 0 {
		t.Fatalf("a stranger started %d turns", len(calls))
	}
}
