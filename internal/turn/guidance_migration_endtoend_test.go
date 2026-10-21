package turn

import (
	"encoding/json"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// This offline main-path fixture publishes the reviewable migration input and
// exercises the real generated release, retained store, broker and MCP adapter.
// It does not assert provider loading, model compliance or owner deployment.
func TestGuidanceMigrationEndToEnd(t *testing.T) {
	db, session, options := agentResultFixture(t, "research")
	proposalBytes, err := os.ReadFile("../../docs/standards-migration/proposal.json")
	mustProgress(t, err)
	var input struct {
		Operations []standards.Operation `json:"operations"`
	}
	mustProgress(t, json.Unmarshal(proposalBytes, &input))
	var voice *standards.Standard
	for _, operation := range input.Operations {
		if operation.Standard != nil && operation.Standard.ID == "factory-voice-options" {
			voice = operation.Standard
		}
	}
	if voice == nil || voice.Body == "" {
		t.Fatal("migration proposal lacks the research role voice")
	}
	svc := &standards.Service{Store: db, OwnerID: 71}
	proposal, err := svc.Create(t.Context(), "guidance-migration-fixture", 0, input.Operations)
	mustProgress(t, err)
	reviewed, err := svc.Review(t.Context(), proposal.ProposalID, proposal.Version)
	mustProgress(t, err)
	_, err = svc.Approve(t.Context(), proposal.ProposalID, proposal.Version, reviewed.Proposal.ChangeDigest)
	mustProgress(t, err)
	first, err := svc.Publish(t.Context(), proposal.ProposalID, proposal.Version, reviewed.Proposal.ChangeDigest)
	mustProgress(t, err)

	specs, err := ResearchSpecs()
	mustProgress(t, err)
	definition, err := teamsource.CaptureWorkflowIdentity(t.Context(), os.DirFS("../../teams/go-development/v1.0.3/research"), "research.dot", "go-development", agent.WorkflowResearch, specs)
	mustProgress(t, err)
	opts, req, _ := deliveryFixture()
	opts.Tiers = config.Defaults().Tiers
	req.Agent, req.Mode, req.Workflow = "codex", agent.ModeResearch, agent.WorkflowResearch
	plan, err := planWorkflowDelivery(opts, req, definition, "investigate", ResearchWorkflowPolicy())
	mustProgress(t, err)
	delivery, err := writeDelivery(t.Context(), t.TempDir(), "bundle", plan)
	mustProgress(t, err)
	var delivered strings.Builder
	for name, raw := range plan.files {
		if strings.Contains(string(raw), "# Voice") || strings.Contains(string(raw), "You always respond in effective sentences") {
			t.Fatalf("migrated internal Voice copy delivered in %s", name)
		}
		if strings.Contains(name, "standards-migration") {
			t.Fatalf("proposal evidence delivered as %s", name)
		}
		delivered.Write(raw)
	}
	for _, retained := range []string{"communication preflight, submit and receipt", "do not publish directly", "factory"} {
		if !strings.Contains(delivered.String(), retained) {
			t.Fatalf("required procedure %q missing", retained)
		}
	}
	for _, path := range []agent.RunPath{agent.PathCold, agent.PathResume, agent.PathColdAfterResumeRefused} {
		mustProgress(t, agent.VerifyRequestDelivery(t.Context(), agent.Request{Invocation: agent.InvocationGraphNode, Graph: &agent.GraphInvocation{Node: "investigate"}, Delivery: delivery, Path: path}))
	}

	root := strconv.FormatInt(options.Attempt.RunID, 10)
	runtime := &standards.Runtime{Store: db, RootRunID: root, ConsumerKey: "story-011:research", AttemptID: options.Attempt.Attempt}
	initial, err := runtime.Bind(t.Context(), true)
	mustProgress(t, err)
	if initial.Integrity != "verified" || initial.Binding.RevisionID != first.ID {
		t.Fatalf("initial binding: %+v", initial)
	}
	launch := func(runtime *standards.Runtime) (*standardsWire, *AttemptGuard) {
		t.Helper()
		service, bindErr := BindAgentResultService(t.Context(), options)
		mustProgress(t, bindErr)
		family := familyResearchOptions(t, db, service)
		tools, toolsErr := NewStandardsToolService(runtime, service.Binding())
		mustProgress(t, toolsErr)
		family.Registrations = tools.Registrations()
		client, guard, _ := familySocketFixture(t, db, family)
		return newStandardsWire(t, client), guard
	}
	wire, guard := launch(runtime)
	var categories standards.CategoryPage
	wire.call(t, "standards_categories_list", map[string]any{}, &categories)
	found := false
	for _, category := range categories.Categories {
		if category.ID == "shared-working" {
			found = true
		}
	}
	if !found {
		t.Fatal("shared-working migration category is not discoverable")
	}
	var selected standards.RuntimeStatus
	wire.call(t, "standards_select", map[string]any{"expected_version": 0, "category_ids": []string{"shared-working"}, "reason": "Shared working guidance applies to this research role"}, &selected)
	if selected.Selection == nil {
		t.Fatal("selection was not retained")
	}
	read := func(wire *standardsWire) map[string]string {
		t.Helper()
		bodies := map[string]string{}
		cursor := ""
		for pages := 0; ; pages++ {
			if pages > 1000 {
				t.Fatal("migration read did not finish")
			}
			var page standards.ReadPage
			wire.call(t, "standards_read", map[string]any{"selection_version": selected.Selection.Version, "selection_digest": selected.Selection.Digest, "cursor": cursor}, &page)
			if page.Status.Binding.RevisionID != first.ID || page.Status.Binding.Digest != first.Digest {
				t.Fatal("read substituted another book")
			}
			bodies[page.StandardID] += page.Body
			if page.Complete {
				break
			}
			if page.NextCursor == "" || page.NextCursor == cursor {
				t.Fatal("migration read did not advance")
			}
			cursor = page.NextCursor
		}
		var acknowledged standards.ReadPage
		wire.call(t, "standards_read", map[string]any{"selection_version": selected.Selection.Version, "selection_digest": selected.Selection.Digest, "complete": true}, &acknowledged)
		if acknowledged.Status.RetrievalStatus != "retrieved" {
			t.Fatalf("retrieval not complete: %+v", acknowledged.Status)
		}
		return bodies
	}
	bodies := read(wire)
	if bodies[voice.ID] != voice.Body {
		t.Fatal("MCP did not retrieve exact migrated voice body")
	}
	replacement := *voice
	replacement.Body = "Later published fixture guidance."
	publishRunStandards(t, db, "later-guidance-fixture", first.ID, []standards.Operation{{Kind: "standard.replace", Standard: &replacement}})
	mustProgress(t, guard.Revoke(t.Context(), "replaced"))
	options.Attempt = agentResultRun(t, db, session, "research", "migration-attempt-2", 2)
	resumed := &standards.Runtime{Store: db, RootRunID: root, ConsumerKey: runtime.ConsumerKey, AttemptID: options.Attempt.Attempt}
	restored, err := resumed.Bind(t.Context(), false)
	mustProgress(t, err)
	if restored.Binding.RevisionID != first.ID || restored.Binding.Digest != first.Digest || restored.Selection == nil || restored.Selection.Version != selected.Selection.Version || restored.Selection.Digest != selected.Selection.Digest {
		t.Fatalf("resume replaced retained identity: %+v", restored)
	}
	resumedWire, _ := launch(resumed)
	if !reflect.DeepEqual(read(resumedWire), bodies) {
		t.Fatal("resume changed migrated guidance")
	}
	mustProgress(t, agent.VerifyRequestDelivery(t.Context(), agent.Request{Invocation: agent.InvocationGraphNode, Graph: &agent.GraphInvocation{Node: "investigate"}, Delivery: delivery, Path: agent.PathResume}))
}
