package daemon_test

import (
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/store/creationfixture"
	"github.com/cloudvane/litefactory/internal/turn"
	"strings"
	"testing"
)

func TestCreationRejectionQueueIO_BacklogReleasesLaterWork(t *testing.T) {
	for _, body := range []string{"/mcp-create invalid", "/mcp-create prepare {invalid"} {
		t.Run(body, func(t *testing.T) {
			f := newFixture(t)
			f.plantCursor()
			f.issue("Creation backlog")
			f.settle()
			f.forge.addComment(testIssue, 501, owner(), body)
			f.forge.addComment(testIssue, 502, owner(), "/spike explain this task")
			f.settle()
			if f.watermark() != 502 {
				t.Fatalf("queue stuck at %d", f.watermark())
			}
			calls := f.turns.calls()
			if len(calls) != 1 || calls[0].Trigger.CommentID != 502 {
				t.Fatalf("wrong execution: %+v", calls)
			}
			row, err := f.db.GetCreationRejection(t.Context(), f.session().ID, 501)
			if err != nil {
				t.Fatal(err)
			}
			delivery, err := f.db.GetReportDelivery(t.Context(), row.Delivery.Key)
			if err != nil || !delivery.Completed || delivery.CommentID == 0 || !strings.Contains(delivery.Body, "disabled") {
				t.Fatalf("missing confirmed guidance %+v %v", delivery, err)
			}
			f.settle()
			if len(f.turns.calls()) != 1 {
				t.Fatal("consumed backlog replayed")
			}
		})
	}
}

func TestCreationRejectionQueueIO_EnabledResearchMalformedThenWork(t *testing.T) {
	for _, tc := range []struct{ body, reason string }{{"/mcp-create invalid", "syntax"}, {"/mcp-create prepare {invalid", "payload"}} {
		t.Run(tc.reason, func(t *testing.T) {
			f := newFixture(t)
			cs, err := store.NewCreationStore(f.db, f.clock.Now)
			if err != nil {
				t.Fatal(err)
			}
			authorizer, err := turn.NewCreationAuthorizer(turn.CreationAuthorizerOptions{Store: cs, Enabled: true, OwnerID: ownerID, BotID: botID})
			if err != nil {
				t.Fatal(err)
			}
			runtime := &turn.MCPRuntime{CreationAuthorizer: authorizer, BotID: botID}
			f.daemon = f.newDaemon(daemon.Options{Turn: daemon.NewExecutor(turn.Options{MCP: runtime})})
			f.plantCursor()
			f.issueWithLabels("Creation backlog", "Mode: Research")
			f.settle()
			f.forge.addComment(testIssue, 501, owner(), tc.body)
			f.settle()
			row, err := f.db.GetCreationRejection(t.Context(), f.session().ID, 501)
			if err != nil {
				t.Fatal(err)
			}
			if row.Reason != tc.reason {
				t.Fatalf("reason=%s want %s", row.Reason, tc.reason)
			}
			delivery, err := f.db.GetReportDelivery(t.Context(), row.Delivery.Key)
			if err != nil || !delivery.Completed || delivery.CommentID == 0 || f.watermark() != 501 {
				t.Fatalf("source not confirmed and consumed: %+v %v watermark %d", delivery, err, f.watermark())
			}
			creationfixture.NoCreationIntents(t, store.DSN(f.db.Path()), f.session().TaskRef)
			if len(f.runs()) != 0 {
				t.Fatal("rejected command ran work")
			}
			// With the rejection settled, replace only expensive work execution with
			// the standard fake; queue, real DB, filter and reporter remain unchanged.
			f.daemon = f.newDaemon(daemon.Options{})
			f.forge.addComment(testIssue, 502, owner(), "inspect the next instruction")
			f.settle()
			calls := f.turns.calls()
			if len(calls) != 1 || calls[0].Trigger.CommentID != 502 || f.watermark() != 502 {
				t.Fatalf("later work blocked: calls=%+v watermark=%d", calls, f.watermark())
			}
		})
	}
}
