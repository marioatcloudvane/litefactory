package linear

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/ticket"
)

type graphQLRequest struct {
	Query     string
	Variables map[string]any
}

func decodeRequest(t *testing.T, r *http.Request) graphQLRequest {
	t.Helper()
	var req graphQLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Error(err)
	}
	return req
}

func TestListSetupLabelsPagesAndFilters(t *testing.T) {
	var cursors []any
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		req := decodeRequest(t, r)
		cursors = append(cursors, req.Variables["after"])
		if req.Variables["after"] == nil {
			_, _ = fmt.Fprint(w, `{"data":{"issueLabels":{"nodes":[{"id":"g","name":"Mode","isGroup":true,"team":{"id":"team"}},{"id":"other","name":"Bug","team":{"id":"other-team"}},{"id":"old","name":"Old","archivedAt":"2026-01-01T00:00:00Z","team":{"id":"team"}}],"pageInfo":{"hasNextPage":true,"endCursor":"c1"}}}}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"data":{"issueLabels":{"nodes":[{"id":"p","name":"Plan","team":{"id":"team"},"parent":{"id":"g","name":"Mode"}},{"id":"ws","name":"Bug"}],"pageInfo":{"hasNextPage":false}}}}`)
	})
	got, err := c.ListSetupLabels(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := []SetupLabel{
		{ID: "g", Name: "Mode", IsGroup: true, TeamID: "team"},
		{ID: "p", Name: "Plan", TeamID: "team", ParentID: "g", ParentName: "Mode"},
		{ID: "ws", Name: "Bug"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v", got)
	}
	if len(cursors) != 2 || cursors[0] != nil || cursors[1] != "c1" {
		t.Fatalf("cursors=%v", cursors)
	}
}

func TestListSetupLabelsFailedSecondPageReturnsNoList(t *testing.T) {
	calls := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			_, _ = fmt.Fprint(w, `{"data":{"issueLabels":{"nodes":[{"id":"a","name":"A","team":{"id":"team"}}],"pageInfo":{"hasNextPage":true,"endCursor":"c1"}}}}`)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	})
	got, err := c.ListSetupLabels(t.Context())
	if err == nil || got != nil {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestListSetupLabelsRefusesRepeatedCursor(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"data":{"issueLabels":{"nodes":[],"pageInfo":{"hasNextPage":true,"endCursor":"same"}}}}`)
	})
	got, err := c.ListSetupLabels(t.Context())
	if err == nil || got != nil {
		t.Fatalf("got %v %v", got, err)
	}
}

func TestCreateLabelSendsTeamAndGrouping(t *testing.T) {
	var inputs []map[string]any
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		req := decodeRequest(t, r)
		if !strings.Contains(req.Query, "issueLabelCreate") {
			t.Errorf("unexpected query %s", req.Query)
		}
		input, _ := req.Variables["input"].(map[string]any)
		inputs = append(inputs, input)
		_, _ = fmt.Fprintf(w, `{"data":{"issueLabelCreate":{"success":true,"issueLabel":{"id":"new-%d"}}}}`, len(inputs))
	})
	id, err := c.CreateLabel(t.Context(), NewLabel{Name: "Mode", Color: "aabbcc", Description: "d", IsGroup: true})
	if err != nil || id != "new-1" {
		t.Fatalf("%q %v", id, err)
	}
	id, err = c.CreateLabel(t.Context(), NewLabel{Name: "Plan", Color: "112233", ParentID: "new-1"})
	if err != nil || id != "new-2" {
		t.Fatalf("%q %v", id, err)
	}
	group, child := inputs[0], inputs[1]
	if group["teamId"] != "team" || group["isGroup"] != true || group["color"] != "#aabbcc" || group["name"] != "Mode" || group["description"] != "d" {
		t.Fatalf("group input %v", group)
	}
	if _, ok := group["parentId"]; ok {
		t.Fatalf("group carries parent %v", group)
	}
	if child["teamId"] != "team" || child["parentId"] != "new-1" || child["color"] != "#112233" {
		t.Fatalf("child input %v", child)
	}
	if _, ok := child["isGroup"]; ok {
		t.Fatalf("child carries isGroup %v", child)
	}
}

func TestCreateLabelUnconfirmedIsRefused(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"data":{"issueLabelCreate":{"success":false,"issueLabel":{"id":"x"}}}}`)
	})
	id, err := c.CreateLabel(t.Context(), NewLabel{Name: "Plan", Color: "112233"})
	var f *ticket.Failure
	if id != "" || !errors.As(err, &f) || f.Kind != ticket.Refused {
		t.Fatalf("%q %v", id, err)
	}
}

// inspectionFake answers publisher, destination, label and state lookups;
// deleted names a label ID that Linear no longer has.
func inspectionFake(t *testing.T, labelLookups *[]string, stateLookups *[]string, viewerOrg, deleted string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req := decodeRequest(t, r)
		id, _ := req.Variables["id"].(string)
		switch {
		case strings.Contains(req.Query, "viewer"):
			_, _ = fmt.Fprintf(w, `{"data":{"viewer":{"id":"bot","active":true,"organization":{"id":%q}}}}`, viewerOrg)
		case strings.Contains(req.Query, "project(id:"):
			_, _ = fmt.Fprint(w, `{"data":{"team":{"id":"team","organization":{"id":"workspace"}},"project":{"id":"project","teams":{"nodes":[{"id":"team"}],"pageInfo":{"hasNextPage":false}}}}}`)
		case strings.Contains(req.Query, "issueLabel("):
			*labelLookups = append(*labelLookups, id)
			if id == deleted {
				_, _ = fmt.Fprint(w, `{"data":null,"errors":[{"message":"Entity not found"}]}`)
				return
			}
			_, _ = fmt.Fprintf(w, `{"data":{"issueLabel":{"id":%q,"organization":{"id":"workspace"},"team":{"id":"team"}}}}`, id)
		case strings.Contains(req.Query, "workflowState("):
			*stateLookups = append(*stateLookups, id)
			_, _ = fmt.Fprintf(w, `{"data":{"workflowState":{"id":%q,"type":"unstarted","team":{"id":"team"}}}}`, id)
		default:
			_, _ = fmt.Fprint(w, `{"data":{"webhooks":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}`)
		}
	}
}

func TestVerifyForSaveLooksUpOnlyGivenLabels(t *testing.T) {
	var labels, states []string
	c := testClient(t, inspectionFake(t, &labels, &states, "workspace", ""))
	c.config.ModeLabels = map[string]string{"plan": "stored-plan"}
	c.config.BugLabel = "stored-bug"
	c.config.States = map[string]string{"open": "s-open"}
	if err := c.VerifyForSave(t.Context(), []string{"new-a", "new-b"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(labels, []string{"new-a", "new-b"}) || !slices.Equal(states, []string{"s-open"}) {
		t.Fatalf("labels=%v states=%v", labels, states)
	}
	labels = nil
	if err := c.VerifyForSave(t.Context(), []string{"new-a", "new-a"}); !IsMappingFailure(err) {
		t.Fatalf("duplicate accepted: %v", err)
	}
	if err := c.VerifyForSave(t.Context(), []string{""}); !IsMappingFailure(err) {
		t.Fatalf("empty accepted: %v", err)
	}
}

func TestIsMappingFailure(t *testing.T) {
	var labels, states []string
	c := testClient(t, inspectionFake(t, &labels, &states, "workspace", "gone"))
	c.config.ModeLabels = map[string]string{"plan": "gone"}
	_, err := c.Inspect(t.Context())
	if err == nil || !IsMappingFailure(err) {
		t.Fatalf("deleted label: %v", err)
	}
	other := testClient(t, inspectionFake(t, &labels, &states, "other-workspace", ""))
	_, err = other.Inspect(t.Context())
	if err == nil || IsMappingFailure(err) {
		t.Fatalf("publisher failure: %v", err)
	}
	if IsMappingFailure(nil) || IsMappingFailure(errors.New("x")) {
		t.Fatal("plain error")
	}
}
