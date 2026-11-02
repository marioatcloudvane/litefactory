package messaging

import (
	"encoding/json"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestFactoryDiagnosisPreservesPendingPayloadAndTargetFence(t *testing.T) {
	run, at := factoryUnitRun(t, &factoryUnitProvider{}, "fedcba0987654321")
	s := run.s
	s.EnableResearch(&factoryUnitDiagnosis{}, nil)
	repo, err := fixtureDB(s).UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := fixtureDB(s).GetOrCreateSession(t.Context(), repo, 42)
	if err != nil {
		t.Fatal(err)
	}
	principal := owneraction.Principal{Transport: "mcp", GrantID: "grant"}
	domain := OwnerActionDomain{Service: s}
	facts, err := domain.Resolve(t.Context(), principal, owneraction.Invocation{Command: "diagnose", Arguments: map[string]string{"project": "acme/app", "issue": "42", "note": "Inspect saved work"}}, at)
	if err != nil {
		t.Fatal(err)
	}
	// Reproduce the previous pending payload using its retained serializer.
	legacy := ownerActionPayload{Natural: naturalPayload{Invocation: Invocation{Command: facts.Invocation.Command, Arguments: maps.Clone(facts.Invocation.Arguments)}, RepoID: repo.ID, Project: repo.Slug(), Channel: "mcp:grant", Transport: "mcp"}}
	encoded, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if facts.Payload != string(encoded) {
		t.Fatalf("pending payload encoding changed:\ngot %s\nwant %s", facts.Payload, encoded)
	}
	if facts.TargetID != session.TaskRef || facts.TargetRevision != store.OwnerActionSessionRevision(session) {
		t.Fatalf("wrong frozen task: %+v", facts)
	}
	if err = domain.Recheck(t.Context(), principal, facts, at); err != nil {
		t.Fatal(err)
	}
	changed := facts
	changed.TargetRevision = "replacement"
	if err = domain.Recheck(t.Context(), principal, changed, at); err == nil || !strings.Contains(err.Error(), "reviewed diagnosis target changed") {
		t.Fatalf("changed target admitted: %v", err)
	}
}

func TestFactoryStandardsPreservesPendingPayload(t *testing.T) {
	s, ops, _ := standardsGateFixture(t)
	principal := owneraction.Principal{Transport: "discord", ChannelID: "3", OwnerID: "42"}
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	invocation := owneraction.Invocation{Command: "standards-author", Arguments: map[string]string{"action": "start", "intent": "Require bounded recovery tests"}}
	domain := OwnerActionDomain{Service: s}
	facts, err := domain.Resolve(t.Context(), principal, invocation, at)
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := ops.ResolveFrozen(t.Context(), principal, StandardsRequest{Operation: "author.start", Arguments: maps.Clone(invocation.Arguments), Transport: "discord", OwnerID: "42", ChannelID: "3", At: at})
	if err != nil {
		t.Fatal(err)
	}
	previous := ownerActionPayload{Natural: naturalPayload{Invocation: Invocation{Command: frozen.Invocation.Command, Arguments: maps.Clone(frozen.Invocation.Arguments)}, Channel: "3", Transport: "discord"}, Domain: frozen, DomainPayload: frozen.Payload}
	encoded, err := json.Marshal(previous)
	if err != nil {
		t.Fatal(err)
	}
	if facts.Payload != string(encoded) {
		t.Fatalf("pending standards encoding changed:\ngot %s\nwant %s", facts.Payload, encoded)
	}
	if err = domain.Recheck(t.Context(), principal, facts, at); err != nil {
		t.Fatal(err)
	}
}
