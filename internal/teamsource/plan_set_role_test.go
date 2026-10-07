package teamsource

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agentresult"
)

func planEmbeddedText(t *testing.T, name string) string {
	t.Helper()
	raw, err := planAssets.ReadFile("plan/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// setBody builds a done result over n entries whose names, ids and intents can be
// adjusted, judged by the same validator the examples run through.
func setBody(t *testing.T, entries []map[string]any) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{"result": map[string]any{
		"outcome": "completed", "comment": "A proposed set.",
		"payload": map[string]any{"plan": map[string]any{"storyset": map[string]any{"entries": entries}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func setEntries(n int) []map[string]any {
	out := make([]map[string]any, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, map[string]any{"id": fmt.Sprintf("s%d", i), "name": fmt.Sprintf("Story %d", i), "intent": "Deliver one outcome."})
	}
	return out
}

// The documented limits are the validator's: the boundary value passes and one
// beyond it is refused with the code the contract names.
func TestPlanSetContractLimitsMatchTheValidator(t *testing.T) {
	t.Parallel()
	const base = "$.result.payload.plan.storyset.entries"
	cases := []struct {
		name   string
		mutate func([]map[string]any) []map[string]any
		want   string // empty: accepted
	}{
		{"one entry", func([]map[string]any) []map[string]any { return setEntries(1) }, ""},
		{"twelve entries", func([]map[string]any) []map[string]any { return setEntries(12) }, ""},
		{"thirteen entries", func([]map[string]any) []map[string]any { return setEntries(13) }, "limit@" + base},
		{"intent of 280 characters", func(e []map[string]any) []map[string]any {
			e[0]["intent"] = strings.Repeat("a", 280)
			return e
		}, ""},
		{"intent of 281 characters", func(e []map[string]any) []map[string]any {
			e[0]["intent"] = strings.Repeat("a", 281)
			return e
		}, "intent@" + base + "[0].intent"},
		{"name of 80 characters", func(e []map[string]any) []map[string]any {
			e[0]["name"] = strings.Repeat("a", 80)
			return e
		}, ""},
		{"name of 81 characters", func(e []map[string]any) []map[string]any {
			e[0]["name"] = strings.Repeat("a", 81)
			return e
		}, "name@" + base + "[0].name"},
		{"multi-line intent", func(e []map[string]any) []map[string]any {
			e[0]["intent"] = "one\ntwo"
			return e
		}, "intent@" + base + "[0].intent"},
		{"dependency on an earlier entry", func(e []map[string]any) []map[string]any {
			e[1]["depends_on"] = []string{"s1"}
			return e
		}, ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, d := agentresult.Create(setBody(t, tt.mutate(setEntries(2))), factoryContext("set", ""))
			if tt.want == "" {
				if !d.Valid {
					t.Fatalf("refused: %s", diagList(d))
				}
				return
			}
			code, path, _ := strings.Cut(tt.want, "@")
			if d.Valid || !hasDiag(d, code, path) {
				t.Fatalf("want %s, valid=%v got %s", tt.want, d.Valid, diagList(d))
			}
		})
	}
}

// The extended stage vocabulary is closed and the set pair rule is exact: a set
// example has operation none, and no other stage may claim it for a done.
func TestContractMarkersAdmitTheSetStageOnly(t *testing.T) {
	t.Parallel()
	fence := "\n```json\n{}\n```\n"
	cases := []struct {
		name, marker, want string
	}{
		{"set ask", "<!-- example: positive id=a stage=set response=ask operation=none -->", ""},
		{"set done", "<!-- example: positive id=a stage=set response=done operation=none -->", ""},
		{"set done with an operation", "<!-- example: positive id=a stage=set response=done operation=create -->", "is not a valid pair"},
		{"story done with none", "<!-- example: positive id=a stage=story response=done operation=none -->", "is not a valid pair"},
		{"negative set", "<!-- example: negative id=a stage=set expect=cycle@$.x -->", ""},
		{"unknown stage", "<!-- example: negative id=a stage=sets expect=cycle@$.x -->", `stage "sets"`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, problems := extractContractExamples(tt.marker + fence)
			got := strings.Join(problems, "; ")
			if tt.want == "" && got != "" || tt.want != "" && !strings.Contains(got, tt.want) {
				t.Fatalf("problems %q, want %q", got, tt.want)
			}
		})
	}
}
