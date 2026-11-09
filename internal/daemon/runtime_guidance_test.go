package daemon

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

func TestReportRecoveryLostResponseNamesRetainedExecution(t *testing.T) {
	db, _, session := recoveryDatabase(t)
	ctx := t.Context()
	segment, err := db.GetOrCreateSegment(ctx, session.ID, 1, "implement", store.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "implement", Agent: store.AgentClaude, LogPath: func(int) string { return "fixture.log" }})
	if err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	if _, err := db.FinishRun(ctx, run.ID, store.RunOutcome{Status: store.RunSucceeded, ExitCode: &zero, SemanticOutcome: "succeeded"}); err != nil {
		t.Fatal(err)
	}
	d, logs, source := recoveryUnitFixture()
	d.db = db
	source.SessionID = session.ID
	source.RunID = run.ID
	source.Task = session.TaskRef
	source.Key = recoveryKey(session.ID, source.TriggerID)
	d.recoveryFailure(ctx, source, time.Now(), &turn.DeliveryError{Key: "retained-report", State: "uncertain", Cause: errors.New("response lost")})
	body := logs.String()
	for _, want := range []string{"Run 1", "retained succeeded outcome", "uncertain", "automatic reconciliation", "no owner action", "retained-report", session.TaskRef} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in diagnostic: %s", want, body)
		}
	}
	saved, err := db.GetRun(ctx, run.ID)
	if err != nil || !saved.Succeeded() {
		t.Fatalf("execution changed: %+v %v", saved, err)
	}
	t.Log(body)
}
