package turn

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestStandardsLaunchRetainsUnavailableAndNewOwnerBindsCurrent(t *testing.T) {
	db, session, o := agentResultFixture(t, "research")
	previous, err := db.GetRun(t.Context(), o.Attempt.RunID)
	if err != nil {
		t.Fatal(err)
	}
	allocate := func(trigger int64) store.Run {
		t.Helper()
		r, e := db.AllocateRun(t.Context(), store.AllocateRunParams{Prelaunch: true, SessionID: session.ID, SegmentID: previous.SegmentID, Mode: "research", Agent: store.AgentClaude, TriggerCommentID: trigger, LogPath: func(n int) string { return fmt.Sprintf("standards-%d.log", n) }})
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	// An existing launched run missing its book can never capture a new one.
	root, _, guidance := prepareStandards(t.Context(), db, nil, previous, "analyst", "research")
	if !strings.Contains(guidance, "integrity=unavailable") {
		t.Fatalf("missing binding not visible: %s", guidance)
	}
	missing, e := db.GetStandardsRunBinding(t.Context(), root)
	if e != nil || missing.Outcome != "unavailable" || missing.RevisionID != 0 || len(missing.SnapshotBytes) != 0 {
		t.Fatalf("missing invocation did not retain an explicit unavailable outcome: %+v %v", missing, e)
	}
	first := allocate(2)
	root, consumer, guidance := prepareStandards(t.Context(), db, nil, first, "analyst", "research")
	if !strings.Contains(guidance, "delivery unavailable") || !strings.Contains(guidance, "integrity=unavailable") {
		t.Fatalf("disabled MCP/no book not visible: %s", guidance)
	}
	writer := standards.Service{Store: db, OwnerID: 7}
	p, err := writer.Create(t.Context(), "launch-book", 0, []standards.Operation{{Kind: "category.add", Category: &standards.Category{ID: "go", Name: "Go"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = writer.Approve(t.Context(), p.ProposalID, p.Version, p.ChangeDigest); err != nil {
		t.Fatal(err)
	}
	published, err := writer.Publish(t.Context(), p.ProposalID, p.Version, p.ChangeDigest)
	if err != nil {
		t.Fatal(err)
	}
	started, err := db.StartRun(t.Context(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	repeatedRoot, repeatedConsumer, guidance := prepareStandards(t.Context(), db, &MCPRuntime{}, started, "analyst", "research")
	if repeatedRoot != root || repeatedConsumer != consumer || !strings.Contains(guidance, "integrity=unavailable") {
		t.Fatalf("same invocation changed root/consumer/status: %s", guidance)
	}
	next := allocate(3)
	newRoot, _, guidance := prepareStandards(t.Context(), db, &MCPRuntime{}, next, "analyst", "research")
	binding, err := db.GetStandardsRunBinding(t.Context(), newRoot)
	if err != nil {
		t.Fatal(err)
	}
	if newRoot == root || binding.RevisionID != published.ID || !strings.Contains(guidance, "integrity=verified") {
		t.Fatalf("new owner failed current binding: %#v %s", binding, guidance)
	}
	if standardsDeliveryGuidance([]string{"communication_submit"}) == "" || standardsDeliveryGuidance([]string{"standards_categories_list"}) != "" {
		t.Fatal("delivery guidance misstates actual catalog")
	}
}

func TestStandardsEachProviderLaunchRefreshesRetainedStatus(t *testing.T) {
	prep, db := lifecycleMCPPreparation(t)
	root := fmt.Sprint(prep.run.ID)
	consumer := standardsConsumerKey("research", "")
	publishRunStandards(t, db, "refresh-launch", 0, []standards.Operation{{Kind: "category.add", Category: &standards.Category{ID: "go", Name: "Go"}}})
	retained := standards.Runtime{Store: db, RootRunID: root, ConsumerKey: consumer}
	initial, err := retained.Bind(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	prep.standardsRoot, prep.standardsConsumer = root, consumer
	prep.standardsGuidance = renderStandardsGuidance(root, consumer, prep.mcpRuntime, initial)
	// A native bound attempt refreshes even without any tool call.
	first, _, err := prepareMCP(t.Context(), prep, agent.Request{Mode: "research"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	firstAttempt := first.service.Binding().Attempt
	if err := first.close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = retained.Select(t.Context(), 0, []string{}, "No standards apply"); err != nil {
		t.Fatal(err)
	}
	next, _, err := prepareMCP(t.Context(), prep, agent.Request{Mode: "research"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(next.standardsGuidance, "selection=selected-empty") || strings.Contains(next.standardsGuidance, "selection=not-selected") {
		t.Fatalf("attempt reused stale bootstrap: %s", next.standardsGuidance)
	}
	nextAttempt := next.service.Binding().Attempt
	if err := next.close(t.Context()); err != nil {
		t.Fatal(err)
	}
	obs, err := db.ListStandardsRunObservations(t.Context(), root, consumer)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, o := range obs {
		if o.Kind == "verification" && o.Outcome == "verified" {
			seen[o.AttemptID] = true
		}
	}
	if !seen[firstAttempt] || !seen[nextAttempt] {
		t.Fatalf("missing attempt verification observations: %#v", obs)
	}
	// Legacy/runtime-disabled fallback also refreshes retained state immediately.
	prep.mcpRuntime = nil
	prep.meterDB = db
	calls := 0
	prep.runner = lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		if !strings.Contains(req.Prompt, "selection=selected-empty") || strings.Contains(req.Prompt, "selection=not-selected") {
			t.Fatalf("legacy provider got stale guidance: %s", req.Prompt)
		}
		return nil, nil
	})
	for range 2 {
		if _, err := runMCPBound(t.Context(), prep, agent.Request{Mode: "research"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("provider launches=%d", calls)
	}
}
