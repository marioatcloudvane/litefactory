package agent

import (
	"testing"
	"time"
)

func TestInterruptedResultClone(t *testing.T) {
	original := &RunResult{Outcome: OutcomeInterrupted, ExitCode: 0, HasResult: true, Interruption: &Interruption{DueAt: time.Unix(2000000000, 0), Precision: ResetExact}}
	clone := original.Clone()
	clone.Interruption.Reason = "changed"
	if original.Interruption.Reason != "" {
		t.Fatal("Clone aliases interruption")
	}
	if original.Succeeded() || clone.Succeeded() {
		t.Fatal("exit-zero interrupted result classified successful")
	}
}
