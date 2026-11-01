package connector

import (
	"context"
	"encoding/json"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cloudvane/litefactory/internal/stagedecision"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestStageDecisionArgumentsRejectAuthorityAndReplacementBindings(t *testing.T) {
	summary := `{"question":"Which remedy?","conclusion":"Revise the guard.","alternatives_or_uncertainty":"Replacement remains to be reviewed.","evidence_basis":"Current saved analysis.","attribution":"Owner decision after confirmation."}`
	proposal := `{"task_ref":"github:acme/api#7","stage":"analysis","client_request_key":"one","summary":` + summary + `}`
	for _, tc := range []struct{ name, raw string }{
		{"approve_stage", proposal},
		{"revise_task", strings.TrimSuffix(proposal, "}") + `,"change":"Preserve error context."}`},
		{"decision_get", `{"decision_ref":"decision-one"}`},
		{"decision_cancel", `{"decision_ref":"decision-one","target":"proposal"}`},
		{"decision_cancel", `{"decision_ref":"decision-one","target":"summary"}`},
	} {
		if _, err := parseDecisionArguments(tc.name, json.RawMessage(tc.raw)); err != nil {
			t.Errorf("valid %s refused: %v", tc.name, err)
		}
	}
	for _, tc := range []struct{ name, raw string }{
		{"approve_stage", strings.TrimSuffix(proposal, "}") + `,"owner_id":"7"}`},
		{"approve_stage", strings.TrimSuffix(proposal, "}") + `,"candidate_revision":"new"}`},
		{"approve_stage", strings.TrimSuffix(proposal, "}") + `,"change":"Approve after changing it."}`},
		{"approve_stage", strings.Replace(proposal, `"stage":"analysis"`, `"stage":"analysis","stage":"repair"`, 1)},
		{"approve_stage", strings.Replace(proposal, `"evidence_basis":"Current saved analysis."`, `"evidence_basis":null`, 1)},
		{"approve_stage", strings.Replace(proposal, `"attribution":"Owner decision after confirmation."`, `"attribution":""`, 1)},
		{"approve_stage", strings.Replace(proposal, `"summary":`+summary, `"summary":{}`, 1)},
		{"approve_stage", proposal + ` {}`},
		{"revise_task", proposal},
		{"decision_get", `{"decision_ref":"one","grant_id":"other"}`},
		{"decision_get", `{"decision_ref":null}`},
		{"decision_cancel", `{"decision_ref":"one","target":"workflow"}`},
		{"decision_cancel", `{"decision_ref":"one"}`},
	} {
		if _, err := parseDecisionArguments(tc.name, json.RawMessage(tc.raw)); err == nil {
			t.Errorf("unsafe %s admitted: %s", tc.name, tc.raw)
		}
	}
}

func TestStageDecisionGuidanceResourceClosure(t *testing.T) {
	if _, err := loadGuidance(guidanceFiles, decisionToolNames); err != nil {
		t.Fatal(err)
	}
	fixtures := fstest.MapFS{}
	for _, name := range decisionToolNames {
		fixtures["guidance/"+name+".md"] = &fstest.MapFile{Data: []byte("fixture guidance")}
		schema := decisionInputSchema(name)
		if schema["additionalProperties"] != false {
			t.Fatalf("%s accepts extra properties", name)
		}
	}
	if _, err := loadGuidance(fixtures, decisionToolNames); err != nil {
		t.Fatal(err)
	}
	delete(fixtures, "guidance/decision_get.md")
	if _, err := loadGuidance(fixtures, decisionToolNames); err == nil {
		t.Fatal("missing recovery guidance admitted")
	}
}

func TestStageDecisionDiscoveryRequiresReadyRepositoryActions(t *testing.T) {
	s := &Server{}
	s.SetStageDecisions(&stagedecision.Service{})
	for _, scope := range []string{DiscussionScope, AdvisoryScope, DiscussionPublishScope, StandardsManagementScope, RepositoryActionsScope} {
		names := s.availableTools(store.ConnectorGrant{Scope: scope})
		for _, name := range names {
			if isDecisionTool(name) {
				t.Fatalf("unready decision exposed for %s", scope)
			}
		}
		expected := scope == RepositoryActionsScope
		if decisionGrant(store.ConnectorGrant{Scope: scope}) != expected {
			t.Fatalf("scope %s stage authority differs", scope)
		}
	}
}

type decisionDiscoveryPublisher struct{}

func (decisionDiscoveryPublisher) Publish(context.Context, store.DiscussionConclusion) (string, error) {
	return "", nil
}
func (decisionDiscoveryPublisher) Recover(context.Context, store.DiscussionConclusion) (string, bool, error) {
	return "", false, nil
}

func TestStageDecisionReadyDiscoveryScopeSeparation(t *testing.T) {
	fixture := newConnectorFixture(t, nil)
	service, err := stagedecision.New(stagedecision.Options{DB: fixture.db, Now: fixture.options.Now, Protect: func(_ context.Context, v string) (string, error) { return v, nil }, Publisher: decisionDiscoveryPublisher{}})
	if err != nil {
		t.Fatal(err)
	}
	actions := owneraction.New(fixture.db, actionContractDomain{}, fixture.options.Now)
	service.BindActions(actions)
	s := &Server{}
	s.SetActions(actions)
	s.SetStageDecisions(service)
	for _, scope := range []string{DiscussionScope, AdvisoryScope, DiscussionPublishScope, StandardsManagementScope, RepositoryActionsScope} {
		names := s.availableTools(store.ConnectorGrant{Scope: scope})
		for _, name := range decisionToolNames {
			found := false
			for _, actual := range names {
				if actual == name {
					found = true
				}
			}
			if found != (scope == RepositoryActionsScope) {
				t.Fatalf("scope %s exposes %s: %v", scope, name, found)
			}
		}
	}
}
