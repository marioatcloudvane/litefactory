package turn

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/harness"
)

func TestExternalTeamRefusesBeforeRunnerAndCloneChanges(t *testing.T) {
	t.Parallel()
	runner := newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		t.Error("runner invoked for unavailable external team transport")
		return nil, nil
	})
	f := newFixture(t, runner)
	repo := f.repoRow()
	repo.ExternalTeam = "go-team"
	if _, err := f.db.UpsertRepo(t.Context(), repo); err != nil {
		t.Fatal(err)
	}
	gittest.AssertUnchanged(t, f.repo.Dir, func() {
		result := f.execute(f.request())
		if result.Disposition != DispositionPrepareFailed {
			t.Fatalf("got %s", result.Disposition)
		}
		if !strings.Contains(result.Reason, "external team") {
			t.Fatalf("failure not actionable: %#v", result)
		}
	})
	if len(f.runs()) != 0 {
		t.Fatal("allocated a run for unavailable transport")
	}
}
