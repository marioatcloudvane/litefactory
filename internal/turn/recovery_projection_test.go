package turn

import (
	"context"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/report"
	"strings"
	"testing"
)

func TestRecoveryProjectionUsesAffectedRunOrdinal(t *testing.T) {
	var f *fixture
	f = newFixture(t, newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		return succeeded(req, f.paths.Home()), nil
	}))
	f.execute(f.request())
	runs := f.runs()
	out := &report.StopRecovery{}
	if err := projectRecoveryRun(t.Context(), f.db, f.session.ID, runs[0].ID, out); err != nil {
		t.Fatal(err)
	}
	if out.Route != "inspect-run" || !strings.Contains(out.NextStep, "--run 1") || !strings.Contains(out.NextStep, f.session.TaskRef) {
		t.Fatalf("projection = %+v", out)
	}
	out = &report.StopRecovery{NextStep: "inspect missing evidence"}
	if err := projectRecoveryRun(t.Context(), f.db, f.session.ID, runs[0].ID+999, out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Unresolved, "unknown") || out.NextStep != "inspect missing evidence" {
		t.Fatalf("missing projection = %+v", out)
	}
}
