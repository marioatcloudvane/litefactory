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
		_, err := Execute(t.Context(), f.opts, f.request())
		if err == nil || !strings.Contains(err.Error(), "external team") {
			t.Fatalf("retryable failure not actionable: %v", err)
		}
	})
	if len(f.runs()) != 0 {
		t.Fatal("allocated a run for unavailable transport")
	}
}
