package agentresult_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"unicode/utf8"

	ar "github.com/cloudvane/litefactory/internal/agentresult"
)

type exchangeStep struct{ Direction, Kind, Path, SHA256 string }
type exchangeScenario struct {
	Name           string
	Revision       int64
	Baseline       string
	ExpectedResult string `json:"expected_result"`
	Full, Scoped   []exchangeStep
}
type exchangeCorpus struct {
	Version   int
	Unit      string
	Scenarios []exchangeScenario
}
type byteTotals struct {
	Input  int `json:"input"`
	Output int `json:"output"`
	Total  int `json:"total"`
}
type scenarioTotals struct {
	Name   string     `json:"name"`
	Full   byteTotals `json:"full"`
	Scoped byteTotals `json:"scoped"`
}
type exchangeReport struct {
	Version   int                   `json:"version"`
	Unit      string                `json:"unit"`
	Scenarios []scenarioTotals      `json:"scenarios"`
	Aggregate map[string]byteTotals `json:"aggregate"`
}

func exchangeRead(t *testing.T, p string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("../..", p))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func exchangeDecode(t *testing.T, raw []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatal(err)
	}
}

func TestExchangeCosts_PinnedTotalsAndIdenticalResults(t *testing.T) {
	var corpus exchangeCorpus
	exchangeDecode(t, exchangeRead(t, "internal/agentresult/testdata/v1/exchanges/corpus.json"), &corpus)
	if corpus.Version != 1 || corpus.Unit != "UTF-8 bytes" || len(corpus.Scenarios) != 5 {
		t.Fatal("wrong pinned corpus")
	}
	report := exchangeReport{Version: 1, Unit: corpus.Unit, Aggregate: map[string]byteTotals{"full": {}, "scoped": {}}}
	seen := map[string]bool{}
	for _, scenario := range corpus.Scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			seen[scenario.Name] = true
			row := scenarioTotals{Name: scenario.Name}
			var desired ar.CreateInput
			exchangeDecode(t, exchangeRead(t, scenario.ExpectedResult), &desired)
			_, ctx := createFixture("research", "incomplete")
			if d := ar.ValidateResult(desired.Result, ctx); !d.Valid {
				t.Fatalf("expected final result invalid: %+v", d)
			}
			for approach, steps := range map[string][]exchangeStep{"full": scenario.Full, "scoped": scenario.Scoped} {
				totals := byteTotals{}
				failures, contributions := 0, 0
				var baseline ar.Snapshot
				if scenario.Baseline != "" {
					exchangeDecode(t, exchangeRead(t, scenario.Baseline), &baseline)
				}
				var result ar.Result
				for _, step := range steps {
					raw := exchangeRead(t, step.Path)
					if !utf8.Valid(raw) || fmt.Sprintf("%x", sha256.Sum256(raw)) != step.SHA256 {
						t.Fatalf("pinned bytes drift: %s", step.Path)
					}
					switch step.Direction {
					case "input":
						totals.Input += len(raw)
					case "output":
						totals.Output += len(raw)
					default:
						t.Fatal("unknown direction")
					}
					totals.Total += len(raw)
					if step.Kind != "contribution" {
						continue
					}
					contributions++
					if scenario.Name == "cold" {
						var d ar.Diagnostics
						result, d = ar.Create(raw, ctx)
						if !d.Valid {
							t.Fatalf("cold create invalid: %+v", d)
						}
						continue
					}
					input, d := ar.DecodeUpdate(raw)
					if !d.Valid {
						t.Fatalf("bad exchange update: %+v", d)
					}
					prepared, diagnostics := ar.ApplyUpdate(baseline, nil, input, ctx, ar.EditTargets("research"))
					if !diagnostics.Valid {
						if scenario.Name != "stale-retry" || diagnostics.ExpectedRevision != 1 || diagnostics.CurrentRevision != 2 {
							t.Fatalf("unexpected rejection: %+v", diagnostics)
						}
						failures++
						continue
					}
					result = prepared.Snapshot.Result
				}
				if !reflect.DeepEqual(result, desired.Result) {
					t.Fatalf("%s changed task meaning or untouched content", approach)
				}
				if scenario.Name == "stale-retry" && (failures != 1 || contributions != 2) {
					t.Fatal("stale retry omitted")
				}
				if scenario.Name != "stale-retry" && (failures != 0 || contributions != 1) {
					t.Fatal("unexpected correction count")
				}
				if approach == "full" {
					row.Full = totals
				} else {
					row.Scoped = totals
				}
				aggregate := report.Aggregate[approach]
				aggregate.Input += totals.Input
				aggregate.Output += totals.Output
				aggregate.Total += totals.Total
				report.Aggregate[approach] = aggregate
			}
			report.Scenarios = append(report.Scenarios, row)
		})
	}
	for _, name := range []string{"cold", "continuation", "stale-retry", "incomplete", "blocked"} {
		if !seen[name] {
			t.Fatalf("missing scenario %s", name)
		}
	}
	var expected exchangeReport
	exchangeDecode(t, exchangeRead(t, "internal/agentresult/testdata/v1/exchanges/totals.json"), &expected)
	if !reflect.DeepEqual(report, expected) {
		t.Fatalf("recomputed exchange totals differ: got %+v want %+v", report, expected)
	}
}
