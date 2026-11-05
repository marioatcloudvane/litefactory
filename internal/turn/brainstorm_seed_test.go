package turn

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
)

type fakeSeedStore struct {
	seed      store.BrainstormPlanLaunchSeed
	ledger    []byte
	missing   bool
	recorded  int
	finished  []string
	recordErr error
}

func (f *fakeSeedStore) BrainstormSeedForLaunch(_ context.Context, launchID int64) (store.BrainstormPlanLaunchSeed, error) {
	if launchID != f.seed.LaunchID {
		return store.BrainstormPlanLaunchSeed{}, store.ErrNotFound
	}
	return f.seed, nil
}

func (f *fakeSeedStore) BrainstormSeedLedger(context.Context, string) ([]byte, string, error) {
	if f.missing {
		return nil, "", store.ErrNotFound
	}
	return f.ledger, digestBytes(f.ledger), nil
}

func (f *fakeSeedStore) RecordBrainstormSeedDigests(_ context.Context, _ int64, dir, brief, technical string) (store.BrainstormPlanLaunchSeed, error) {
	if f.recordErr != nil {
		return store.BrainstormPlanLaunchSeed{}, f.recordErr
	}
	f.recorded++
	f.seed.State, f.seed.SessionDir, f.seed.BriefDigest, f.seed.TechnicalDigest = store.BrainstormSeedWriting, dir, brief, technical
	return f.seed, nil
}

func (f *fakeSeedStore) FinishBrainstormSeed(_ context.Context, _ int64, state, _ string, _ time.Time) error {
	f.finished = append(f.finished, state)
	f.seed.State = state
	return nil
}

const seedTestDir = "aiteam/branch/2026-11-04-idea"

func newFakeSeed(t *testing.T) *fakeSeedStore {
	t.Helper()
	ledger := []byte(`{"decisions":[{"id":"O-01","title":"Goal","decision":"Ship the idea"}]}`)
	return &fakeSeedStore{seed: store.BrainstormPlanLaunchSeed{LaunchID: 5, ItemID: "item", State: store.BrainstormSeedPending, LedgerDigest: digestBytes(ledger)}, ledger: ledger}
}

func expectedSeedFiles(t *testing.T, f *fakeSeedStore) (string, string) {
	t.Helper()
	var research agentresult.Research
	if err := json.Unmarshal(f.ledger, &research); err != nil {
		t.Fatal(err)
	}
	result := agentresult.Result{Payload: agentresult.Payload{Research: &research}}
	return renderResearchTechnical(result), renderResearchBrief(seedTestDir, result)
}

func readSeedTestFile(t *testing.T, root, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, seedTestDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestBrainstormSeedWritesBriefBeforeFirstAgent(t *testing.T) {
	t.Parallel()
	root, f := t.TempDir(), newFakeSeed(t)
	got, err := seedBrainstormBrief(t.Context(), f, root, 5, seedTestDir, time.Now())
	if err != nil || got.Dir != seedTestDir || got.Brief != ResearchBriefName || got.Existing {
		t.Fatalf("seed: %+v %v", got, err)
	}
	technical, brief := expectedSeedFiles(t, f)
	if readSeedTestFile(t, root, ResearchTechnicalName) != technical || readSeedTestFile(t, root, ResearchBriefName) != brief {
		t.Fatal("seeded files differ from the rendered ledger")
	}
	if f.recorded != 1 || f.seed.State != store.BrainstormSeedComplete || f.seed.BriefDigest != digestBytes([]byte(brief)) {
		t.Fatalf("seed not recorded then completed: %+v", f)
	}
	// A fresh worktree after a restart gets the same bytes again.
	fresh := t.TempDir()
	if _, err = seedBrainstormBrief(t.Context(), f, fresh, 5, "ignored/default", time.Now()); err != nil {
		t.Fatal(err)
	}
	if readSeedTestFile(t, fresh, ResearchBriefName) != brief || f.recorded != 1 {
		t.Fatal("fresh worktree was not rewritten with the recorded bytes")
	}
}

func TestBrainstormSeedNoSeedWritesNothing(t *testing.T) {
	t.Parallel()
	root, f := t.TempDir(), newFakeSeed(t)
	got, err := seedBrainstormBrief(t.Context(), f, root, 99, seedTestDir, time.Now())
	if err != nil || got.Dir != "" {
		t.Fatalf("unseeded launch: %+v %v", got, err)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatal("a launch without a seed wrote files")
	}
	// The launch-only preparation has no admitted launch yet.
	if got, err = seedBrainstormBrief(t.Context(), f, root, 0, seedTestDir, time.Now()); err != nil || got.Dir != "" {
		t.Fatalf("launch-only preparation seeded: %+v %v", got, err)
	}
}

func TestBrainstormSeedKeepsUnrelatedBrief(t *testing.T) {
	t.Parallel()
	root, f := t.TempDir(), newFakeSeed(t)
	if err := os.MkdirAll(filepath.Join(root, seedTestDir), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, seedTestDir, ResearchBriefName), []byte("the owner's brief"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := seedBrainstormBrief(t.Context(), f, root, 5, seedTestDir, time.Now())
	if err != nil || !got.Existing || f.seed.State != store.BrainstormSeedSkippedExisting {
		t.Fatalf("unrelated brief: %+v %+v %v", got, f.seed, err)
	}
	if readSeedTestFile(t, root, ResearchBriefName) != "the owner's brief" {
		t.Fatal("unrelated brief overwritten")
	}
	if _, err := os.Stat(filepath.Join(root, seedTestDir, ResearchTechnicalName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("technical file written next to an unrelated brief")
	}
}

func TestBrainstormSeedDigestMismatchStops(t *testing.T) {
	t.Parallel()
	root, f := t.TempDir(), newFakeSeed(t)
	f.seed.LedgerDigest = strings.Repeat("0", 64)
	if _, err := seedBrainstormBrief(t.Context(), f, root, 5, seedTestDir, time.Now()); err == nil || !strings.Contains(err.Error(), "ledger changed") {
		t.Fatalf("mismatch: %v", err)
	}
	f = newFakeSeed(t)
	f.missing = true
	if _, err := seedBrainstormBrief(t.Context(), f, root, 5, seedTestDir, time.Now()); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing ledger: %v", err)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatal("a failed preparation wrote files")
	}
}

func TestBrainstormSeedRecoversInterruptedWrites(t *testing.T) {
	t.Parallel()
	cases := map[string]func(root string, technical, brief string){
		"technical before rename": func(root, technical, _ string) {
			writeSeedTest(root, brainstormSeedTemp(ResearchTechnicalName, 5), technical[:len(technical)/2])
		},
		"technical after rename": func(root, technical, _ string) {
			writeSeedTest(root, ResearchTechnicalName, technical)
		},
		"brief before rename": func(root, technical, brief string) {
			writeSeedTest(root, ResearchTechnicalName, technical)
			writeSeedTest(root, brainstormSeedTemp(ResearchBriefName, 5), brief[:1])
		},
		"brief after rename": func(root, technical, brief string) {
			writeSeedTest(root, ResearchTechnicalName, technical)
			writeSeedTest(root, ResearchBriefName, brief)
		},
	}
	for name, interrupt := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root, f := t.TempDir(), newFakeSeed(t)
			technical, brief := expectedSeedFiles(t, f)
			// The crash happened after the digests were recorded.
			if _, err := f.RecordBrainstormSeedDigests(t.Context(), 5, seedTestDir, digestBytes([]byte(brief)), digestBytes([]byte(technical))); err != nil {
				t.Fatal(err)
			}
			interrupt(root, technical, brief)
			got, err := seedBrainstormBrief(t.Context(), f, root, 5, seedTestDir, time.Now())
			if err != nil || got.Existing || f.seed.State != store.BrainstormSeedComplete {
				t.Fatalf("recovery: %+v %+v %v", got, f.seed, err)
			}
			if readSeedTestFile(t, root, ResearchTechnicalName) != technical || readSeedTestFile(t, root, ResearchBriefName) != brief {
				t.Fatal("recovered files differ from their recorded digests")
			}
		})
	}
}

func writeSeedTest(root, name, body string) {
	dir := filepath.Join(root, seedTestDir)
	_ = os.MkdirAll(dir, 0o750)
	_ = os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600)
}
