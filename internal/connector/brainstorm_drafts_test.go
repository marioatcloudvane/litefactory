package connector

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/owneraction"
)

func TestBrainstormDraftToolsScopes(t *testing.T) {
	t.Parallel()
	b := newBrainstormFixture(t)
	newTools := []string{"brainstorm_draft", "brainstorm_drafts_get", "brainstorm_tasks_get"}
	old := b.token(t, DiscussionScope+" "+AdvisoryScope)
	for _, name := range b.list(t, old) {
		if slices.Contains(newTools, name) {
			t.Fatalf("grant without repository:brainstorm lists %s", name)
		}
	}
	brainstormOnly := b.list(t, b.token(t, BrainstormScope))
	if !slices.Contains(brainstormOnly, "brainstorm_draft") || !slices.Contains(brainstormOnly, "brainstorm_drafts_get") || slices.Contains(brainstormOnly, "brainstorm_tasks_get") {
		t.Fatalf("brainstorm-only grant lists %v", brainstormOnly)
	}
	token := b.token(t, BrainstormScope)
	response, _ := b.call(t, token, "brainstorm_tasks_get", map[string]any{"batch": "batch-1"})
	if response.Code != http.StatusForbidden || !strings.Contains(response.Header().Get("WWW-Authenticate"), RepositoryActionsScope) {
		t.Fatalf("tasks_get without repository:actions: HTTP %d %q", response.Code, response.Header().Get("WWW-Authenticate"))
	}
}

func TestBrainstormDraftArguments(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, raw string }{
		{"brainstorm_draft", `{"brainstorm":"b-1","request_key":"k"}`},
		{"brainstorm_draft", `{"brainstorm":"b-1","request_key":"k","note":"focus on voice"}`},
		{"brainstorm_drafts_get", `{"brainstorm":"b-1"}`},
		{"brainstorm_tasks_get", `{"batch":"batch-1"}`},
	} {
		if _, err := parseBrainstormArguments(tc.name, json.RawMessage(tc.raw)); err != nil {
			t.Errorf("%s rejected %s: %v", tc.name, tc.raw, err)
		}
	}
	for _, tc := range []struct{ name, raw string }{
		{"brainstorm_draft", `{"brainstorm":"b-1"}`},
		{"brainstorm_drafts_get", `{"brainstorm":"b-1","batch":"x"}`},
		{"brainstorm_tasks_get", `{}`},
	} {
		if _, err := parseBrainstormArguments(tc.name, json.RawMessage(tc.raw)); err == nil {
			t.Errorf("%s accepted %s", tc.name, tc.raw)
		}
	}
}

type brainstormCatalogActions struct{ OwnerActions }

func (brainstormCatalogActions) Catalog() []owneraction.ActionDefinition {
	return []owneraction.ActionDefinition{{Command: "draft"}, {Command: "brainstorm_create_tasks"}, {Command: "brainstorm_task_recreate"}}
}

func TestActionSchemaHidesBrainstormActionsFromOldGrants(t *testing.T) {
	server := &Server{}
	server.SetActions(brainstormCatalogActions{})
	count := func(brainstorm bool) int {
		variants, _ := server.actionSchema("action_propose", brainstorm)["oneOf"].([]any)
		return len(variants)
	}
	if got := count(false); got != 1 {
		t.Fatalf("old grant sees %d action variants, want 1", got)
	}
	if got := count(true); got != 3 {
		t.Fatalf("brainstorm grant sees %d action variants, want 3", got)
	}
}
