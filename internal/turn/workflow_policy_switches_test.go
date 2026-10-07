package turn

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestResearchNodeSizeMeasuresKeptEncoding(t *testing.T) {
	small := agentresult.Result{Outcome: "completed", Comment: "ok"}
	if d := researchNodeSize(context.Background(), store.ResultBinding{}, small, agentresult.Context{}); !d.Valid || !d.Complete {
		t.Fatalf("small result refused: %+v", d)
	}
	big := agentresult.Result{Outcome: "completed", Comment: strings.Repeat("x", agentresult.MaxResearchNodeBytes)}
	d := researchNodeSize(context.Background(), store.ResultBinding{}, big, agentresult.Context{})
	if d.Valid || len(d.Diagnostics) != 1 || !strings.Contains(d.Diagnostics[0].Message, "1 MiB") {
		t.Fatalf("oversized result accepted or unclear: %+v", d)
	}
}

// The Research size hook runs inside the shared service at preflight and submit;
// without the hook (Plan) the same service adds no size check.
func TestResearchSizeHookRefusesOversizedAtPreflightAndSubmit(t *testing.T) {
	big := `{"result":{"outcome":"completed","comment":"` + strings.Repeat("x", agentresult.MaxResearchNodeBytes) + `","payload":{"research":{"request":"Inspect boundaries"}}}}`
	small := `{"result":` + retainedResearchResult + `}`

	f := newRetainedFixture(t, "research")
	f.service.validateCompletion = ResearchWorkflowPolicy().ValidateCompletion
	if d, err := f.service.Preflight(t.Context(), []byte(big)); err != nil || d.Valid {
		t.Fatalf("oversized preflight = %+v, %v; want refused", d, err)
	}
	if got, err := f.service.Submit(t.Context(), "big", []byte(big)); err != nil || got.Validation.Valid || got.Snapshot != nil {
		t.Fatalf("oversized submit accepted: %+v, %v", got, err)
	}
	if d, err := f.service.Preflight(t.Context(), []byte(small)); err != nil || !d.Valid {
		t.Fatalf("small preflight = %+v, %v; want valid", d, err)
	}
	if got, err := f.service.Submit(t.Context(), "small", []byte(small)); err != nil || !got.Validation.Valid {
		t.Fatalf("small submit = %+v, %v; want accepted", got, err)
	}

	p := newRetainedFixture(t, "plan")
	if p.service.validateCompletion != nil {
		t.Fatal("plan service must carry no completion hook")
	}
}
