package turn

import (
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestImplementTimeoutRedactsBeforeLedgerClipping(t *testing.T) {
	f := newImplementWorkerFixture(t)
	at := time.Now().UTC()
	def, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Mode: "implement", ProfileVersion: 1, Path: "implement.dot", Digest: strings.Repeat("f", 64), CreatedAt: at})
	mustProgress(t, err)
	instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: def.ID, CurrentNode: "worker_agent", StartedAt: at})
	mustProgress(t, err)
	attempt, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: "worker_agent", Limit: 1, FactoryStep: true, StartedAt: at})
	mustProgress(t, err)
	mustProgress(t, f.db.RecordAttemptTimeout(t.Context(), store.AttemptTimeout{NodeAttemptID: attempt.ID, Budget: time.Minute, TimedOutAt: at}))
	secret := "sensitive-timeout-token-" + strings.Repeat("z", 128)
	opts := Options{DB: f.db, MCP: &MCPRuntime{Redactor: redact.New(redact.Options{Environ: []string{"WORKER_API_KEY=" + secret}})}}
	diagnostic := strings.Repeat("x", maxImplementReason-5) + secret + " tail"
	out, err := implementAfterAgentFailure(t.Context(), opts, ActivityRequest{InstanceID: instance.ID, NodeAttemptID: attempt.ID}, ActivityResult{Diagnostic: diagnostic}, ErrAgentTimedOut)
	mustProgress(t, err)
	row, err := f.db.GetImplementStory(t.Context(), implementParent.String(), 1, "story-001")
	mustProgress(t, err)
	if out.Outcome != "failed" || row.TimeoutAttemptID != attempt.ID || strings.Contains(row.LastFailure, secret[:5]) || len(row.LastFailure) > maxImplementReason+3 {
		t.Fatalf("unsafe clipped timeout evidence: %+v", row)
	}
}
