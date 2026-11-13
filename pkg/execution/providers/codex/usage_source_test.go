package codex

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/pkg/execution/agent"
)

func TestCanonicalRolloutSource(t *testing.T) {
	t.Parallel()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	filesystemEvidenceWrite(t, home, "root", filesystemEvidenceEnvelope("root", "", "old-root"))
	source := filepath.Join(home, ".codex", "sessions")
	var got []agent.UsageObservation
	c := NewUsageCollector(t.Context(), &agent.UsageCapture{Sink: func(_ context.Context, e agent.UsageEvent) error {
		if e.Observation != nil {
			got = append(got, e.Observation.Clone())
		}
		return nil
	}})
	c.PrepareRolloutsFromRoot(source)
	if c.rollouts == nil || c.rollouts.root == nil {
		t.Fatalf("canonical inventory unavailable: %+v", c.coverage)
	}
	defer func() { _ = c.rollouts.root.Close() }()
	c.thread = "root"
	c.invocation("root", "")
	filesystemEvidenceWrite(t, home, "child", filesystemEvidenceEnvelope("child", "root", "new-child"))
	c.collectRollouts(true)
	if len(got) != 1 || got[0].InvocationID != "child" || c.parents["child"] != "root" {
		t.Fatalf("canonical baseline or hierarchy lost: %+v, parents=%v", got, c.parents)
	}
	from := time.Date(2026, 9, 9, 18, 0, 0, 0, time.UTC)
	evidence, err := CollectHistoricalUsageFromRoot(t.Context(), source, "root", HistoricalWindow{From: from, To: from.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence.Observations) != 1 || evidence.Observations[0].InvocationID != "child" {
		t.Fatalf("historical attribution lost: %+v", evidence)
	}
	if err := evidence.Revalidate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(source, source+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source+"-original", source); err != nil {
		t.Fatal(err)
	}
	if err := evidence.Revalidate(t.Context()); err == nil {
		t.Fatal("revalidation followed substituted source symlink")
	}
}

func TestCanonicalRolloutSourceRefusesLinks(t *testing.T) {
	t.Parallel()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	filesystemEvidenceWrite(t, home, "root", filesystemEvidenceEnvelope("root", "", "old-root"))
	if err := os.Symlink(filepath.Join(home, ".codex"), filepath.Join(home, "linked")); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"relative/sessions", filepath.Join(home, "linked", "sessions"), filepath.Join(home, ".codex", "sessions") + "/../sessions"} {
		t.Run(source, func(t *testing.T) {
			c := NewUsageCollector(t.Context(), &agent.UsageCapture{})
			c.PrepareRolloutsFromRoot(source)
			if c.rollouts != nil || !slices.Contains(c.coverage.Diagnostics, "usage_source_unavailable") {
				t.Fatalf("unsafe source accepted: %+v", c.coverage)
			}
		})
	}
}

func TestDefaultRolloutSourceIgnoresConfigurationHome(t *testing.T) {
	t.Parallel()
	home, other := t.TempDir(), t.TempDir()
	filesystemEvidenceWrite(t, home, "root", filesystemEvidenceEnvelope("root", "", "old-root"))
	c := NewUsageCollector(t.Context(), &agent.UsageCapture{})
	c.PrepareRollouts([]string{"HOME=" + home, "CODEX_HOME=" + other})
	if c.rollouts == nil || len(c.rollouts.baseline) != 1 {
		t.Fatalf("default source changed: %+v", c.coverage)
	}
	defer func() { _ = c.rollouts.root.Close() }()
}
