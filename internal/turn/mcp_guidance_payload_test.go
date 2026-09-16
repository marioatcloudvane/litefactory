package turn

import (
	"encoding/json"
	"testing"

	"github.com/cloudvane/litefactory/internal/agentresult"
)

func TestMCPApplicationGuidancePayloadContract(t *testing.T) {
	for _, mode := range []string{"research", "spike", "plan", "implement"} {
		t.Run(mode, func(t *testing.T) {
			_, _, options := agentResultFixture(t, mode)
			service, err := BindAgentResultService(t.Context(), options)
			if err != nil {
				t.Fatal(err)
			}
			data, baseline, err := mcpApplicationExample(t.Context(), service)
			if err != nil {
				t.Fatal(err)
			}
			if len(baseline) != 0 {
				t.Fatalf("new application unexpectedly has baseline: %s", baseline)
			}
			diagnostics, err := service.Preflight(t.Context(), data)
			if err != nil || !diagnostics.Valid || !diagnostics.Complete {
				t.Fatalf("create example preflight: %+v %v payload=%s", diagnostics, err, data)
			}
			var initial agentresult.CreateInput
			if err := json.Unmarshal(data, &initial); err != nil {
				t.Fatal(err)
			}
			initial.Result.Comment = "Actual retained result: quotes \" and Unicode café\nsecond line"
			data, err = json.Marshal(initial)
			if err != nil {
				t.Fatal(err)
			}
			submission, err := service.Submit(t.Context(), "guidance-create", data)
			if err != nil || !submission.Validation.Valid || submission.Snapshot == nil {
				t.Fatalf("seed result: %+v %v", submission, err)
			}
			data, baseline, err = mcpApplicationExample(t.Context(), service)
			if err != nil {
				t.Fatal(err)
			}
			var comment string
			if err := json.Unmarshal(baseline, &comment); err != nil {
				t.Fatal(err)
			}
			if comment != initial.Result.Comment {
				t.Fatalf("trusted baseline changed: %q", comment)
			}
			var update agentresult.UpdateInput
			if err := json.Unmarshal(data, &update); err != nil {
				t.Fatal(err)
			}
			if update.ExpectedRevision != submission.Snapshot.Meta.ResultRevision || len(update.Operations) != 1 || update.Operations[0].Target.Field != "comment" {
				t.Fatalf("incorrect update scope: %+v", update)
			}
			diagnostics, err = service.Preflight(t.Context(), data)
			if err != nil || !diagnostics.Valid || !diagnostics.Complete {
				t.Fatalf("update example preflight: %+v %v payload=%s", diagnostics, err, data)
			}
			updated, err := service.Submit(t.Context(), "guidance-update", data)
			if err != nil || !updated.Validation.Valid || updated.Snapshot == nil {
				t.Fatalf("update example submit: %+v %v", updated, err)
			}
			next, nextBaseline, err := mcpApplicationExample(t.Context(), service)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(next, &update); err != nil {
				t.Fatal(err)
			}
			if update.ExpectedRevision != updated.Snapshot.Meta.ResultRevision {
				t.Fatal("guidance reused stale revision after update")
			}
			if string(nextBaseline) != string(update.Operations[0].Value) {
				t.Fatalf("guidance omitted latest baseline: %s", nextBaseline)
			}
		})
	}
}
