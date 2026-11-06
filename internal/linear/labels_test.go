package linear

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/ticket"
)

func grouped(id, group, name string) labelNode {
	return labelNode{ID: id, Name: name, Parent: &struct{ ID, Name string }{"g-" + group, group}}
}
func flat(id, name string) labelNode { return labelNode{ID: id, Name: name} }

func TestAgentKeyConversions(t *testing.T) {
	if KeyToLabel("Codex") != "Coding Agent: Codex" {
		t.Fatal("key to label")
	}
	for label, want := range map[string]string{"Coding Agent: Codex": "codex", "coding agent: Codex": "", "Coding Agent: codex": "", "Coding Agent: ": "", "Coding Agent: Bad Name": ""} {
		if got, ok := LabelToKind(label); got != want || ok != (want != "") {
			t.Fatalf("%q -> %q %v", label, got, ok)
		}
	}
	if k, ok := KeyForKind(map[string]string{"Codex": "a", "Claude": "b"}, "codex"); !ok || k != "Codex" {
		t.Fatal("match")
	}
	if _, ok := KeyForKind(map[string]string{"Claude": "b"}, "codex"); ok {
		t.Fatal("no match")
	}
	if _, ok := KeyForKind(map[string]string{"Codex": "a", "CODEX": "b"}, "codex"); ok {
		t.Fatal("two same-kind keys")
	}
}

func TestSnapshotLabelsFlatBehaviourUnchangedAndGroupedMerge(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {})
	c.config.ModeLabels = map[string]string{"plan": "plan-id"}
	c.config.CodingAgentLabels = map[string]string{"Codex": "codex-id"}
	cases := []struct {
		name  string
		nodes []labelNode
		want  []string
	}{
		{"flat only", []labelNode{flat("1", "Mode: Plan"), flat("2", "Coding Agent: Codex"), flat("3", "x")}, []string{"Mode: Plan", "Coding Agent: Codex", "x"}},
		{"two flat twins kept", []labelNode{flat("1", "Mode: Plan"), flat("2", "Mode: Plan")}, []string{"Mode: Plan", "Mode: Plan"}},
		{"grouped read", []labelNode{grouped("1", "Mode", "Plan"), grouped("2", "Coding Agent", "Codex")}, []string{"Mode: Plan", "Coding Agent: Codex"}},
		{"grouped plus flat twin is one", []labelNode{grouped("1", "Mode", "Plan"), flat("2", "Mode: Plan")}, []string{"Mode: Plan"}},
		{"mapped grouped plus flat twin is one", []labelNode{grouped("plan-id", "Mode", "Whatever"), flat("2", "Mode: Plan")}, []string{"Mode: Plan"}},
		{"two grouped plus flat keeps two", []labelNode{grouped("1", "Mode", "Plan"), grouped("2", "Mode", "Plan"), flat("3", "Mode: Plan")}, []string{"Mode: Plan", "Mode: Plan"}},
		{"mapped two grouped plus flat keeps two", []labelNode{grouped("plan-id", "Mode", "A"), grouped("plan-id", "Mode", "B"), flat("3", "Mode: Plan")}, []string{"Mode: Plan", "Mode: Plan"}},
		{"different values", []labelNode{flat("1", "Mode: Plan"), grouped("2", "Mode", "Implement")}, []string{"Mode: Plan", "Mode: Implement"}},
		{"stored agent id wins over name and group", []labelNode{grouped("codex-id", "Other", "Anything")}, []string{"Coding Agent: Codex"}},
		{"badly spelled child passes through", []labelNode{grouped("1", "Coding Agent", "codex")}, []string{"Coding Agent: codex"}},
		{"other group bare", []labelNode{grouped("1", "Type", "Plan"), flat("2", "Plan")}, []string{"Plan", "Plan"}},
		{"misspelled group bare", []labelNode{grouped("1", "mode", "Plan")}, []string{"Plan"}},
	}
	for _, tc := range cases {
		if got := c.snapshotLabels(tc.nodes); !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
	plain := testClient(t, func(w http.ResponseWriter, r *http.Request) {})
	if got := plain.snapshotLabels([]labelNode{flat("1", "Mode: Plan"), flat("2", "Mode: Plan")}); len(got) != 2 {
		t.Fatalf("no maps: %v", got)
	}
}

// End to end against a fake Linear: grouped issue reads like a flat one, the
// inherited Codex label is written by stored ID, and no catalogue call is made.
func TestGroupedReadAndInheritedAgentWrite(t *testing.T) {
	var written []string
	labels := `[{"id":"m","name":"Plan","parent":{"id":"g","name":"Mode"}},{"id":"other","name":"keep"}]`
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string
			Variables map[string]any
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if strings.Contains(req.Query, "issueLabels") || strings.Contains(req.Query, "issueLabelCreate") {
			t.Fatalf("label catalogue call: %s", req.Query)
		}
		if strings.Contains(req.Query, "mutation") {
			input, _ := req.Variables["input"].(map[string]any)
			for _, id := range input["labelIds"].([]any) {
				written = append(written, id.(string))
			}
			labels = `[{"id":"m","name":"Plan","parent":{"id":"g","name":"Mode"}},{"id":"other","name":"keep"},{"id":"codex-id","name":"x"}]`
			_, _ = fmt.Fprintf(w, `{"data":{"issueUpdate":{"success":true,"issue":%s}}}`, withLabels(labels))
			return
		}
		_, _ = fmt.Fprintf(w, `{"data":{"issue":%s}}`, withLabels(labels))
	})
	c.config.CodingAgentLabels = map[string]string{"Codex": "codex-id"}
	ref := ticket.Ref{Identity: c.identity("ticket"), Locator: c.locator("ticket")}
	s, err := c.Read(context.Background(), ref)
	if err != nil || !slices.Equal(s.Labels, []string{"Mode: Plan", "keep"}) {
		t.Fatalf("%v %v", s.Labels, err)
	}
	if err := c.AddLabel(context.Background(), ref, "Coding Agent: Codex"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(written, []string{"m", "other", "codex-id"}) {
		t.Fatalf("written=%v", written)
	}
}

func withLabels(labels string) string {
	return strings.Replace(issueJSON("ticket", "project"), `"nodes":[]`, `"nodes":`+labels, 1)
}

func TestAgentWriteWithoutStoredIDRefusesBeforeNetwork(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("network call") })
	ref := ticket.Ref{Identity: c.identity("ticket"), Locator: c.locator("ticket")}
	err := c.AddLabel(context.Background(), ref, "Coding Agent: Codex")
	if f, ok := err.(*ticket.Failure); !ok || f.Kind != ticket.Refused {
		t.Fatalf("err=%v", err)
	}
}
