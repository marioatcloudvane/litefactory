package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func cacheFixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	return path
}
func cacheFile(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.WriteFile(path, make([]byte, n), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestCacheSamplerLargeTreeContinues(t *testing.T) {
	main, reviewer := cacheFixture(t, "main"), cacheFixture(t, "reviewer")
	// A genuinely wide directory exceeds the production limit, not an injected
	// budget. Every file has known nonzero logical length.
	const count = cacheSliceEntries + 257
	for i := 0; i < count; i++ {
		cacheFile(t, filepath.Join(main, fmt.Sprintf("%06d", i)), 3)
	}
	cacheFile(t, filepath.Join(reviewer, "small"), 7)
	s := newCacheSampler(main, reviewer)
	defer s.close()
	// Entry-count boundary is deterministic independent of CI disk speed.
	s.durationBudget = time.Hour
	s.round(context.Background())
	first := s.Snapshot()
	if o := first["main"]; o.State != "partial" || o.PartialBytes != 3*cacheSliceEntries || o.EntriesExamined != cacheSliceEntries || o.CompleteBytes != nil {
		t.Fatalf("first slice: %+v", o)
	}
	if o := first["reviewer"]; o.State != "complete" || o.CompleteBytes == nil || *o.CompleteBytes != 7 {
		t.Fatalf("reviewer was starved: %+v", o)
	}
	assertCacheSampleHealth(t, s, main, "partial", 3*cacheSliceEntries)
	s.round(context.Background())
	full := s.Snapshot()["main"]
	if full.State != "complete" || full.EntriesExamined != count || full.CompleteBytes == nil || *full.CompleteBytes != 3*count {
		t.Fatalf("continued total: %+v", full)
	}
	assertCacheSampleHealth(t, s, main, "complete", 3*count)
	if !full.PassStartedAt.Equal(first["main"].PassStartedAt) {
		t.Fatal("pass restarted instead of continuing")
	}
}

func TestCacheSamplerRetainsCompleteSeparatelyAndDoesNotFollowSymlink(t *testing.T) {
	main, reviewer := cacheFixture(t, "main"), cacheFixture(t, "reviewer")
	cacheFile(t, filepath.Join(main, "one"), 11)
	outside := filepath.Join(t.TempDir(), "outside")
	cacheFile(t, outside, 123)
	if err := os.Symlink(outside, filepath.Join(main, "link")); err != nil {
		t.Fatal(err)
	}
	s := newCacheSampler(main, reviewer)
	defer s.close()
	s.round(context.Background())
	first := s.Snapshot()["main"]
	if first.CompleteBytes == nil || *first.CompleteBytes != 11 {
		t.Fatalf("symlink counted: %+v", first)
	}
	cacheFile(t, filepath.Join(main, "two"), 5)
	s.entryBudget = 1
	s.round(context.Background())
	next := s.Snapshot()["main"]
	if next.State != "partial" || next.CompleteBytes == nil || *next.CompleteBytes != 11 || !next.CompleteAt.Equal(first.CompleteAt) {
		t.Fatalf("lost complete reading: %+v", next)
	}
	// Snapshot pointers must not expose mutable published state.
	*next.CompleteBytes = 999
	if *s.Snapshot()["main"].CompleteBytes != 11 {
		t.Fatal("snapshot aliased published value")
	}
}

func TestCacheSamplerSnapshotDoesNotWaitForReader(t *testing.T) {
	main, reviewer := cacheFixture(t, "main"), cacheFixture(t, "reviewer")
	cacheFile(t, filepath.Join(main, "one"), 1)
	s := newCacheSampler(main, reviewer)
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	calls := 0
	s.beforeRead = func() {
		calls++
		if calls == 1 {
			close(entered)
			<-release
		}
	}
	go func() { defer close(finished); s.round(context.Background()) }()
	<-entered
	read := make(chan struct{})
	go func() {
		d := &Daemon{cacheSampler: s, diskPath: main, minFreeDiskBytes: 100, freeDiskBytes: func(string) (uint64, error) { return 101, nil }, log: slog.New(slog.DiscardHandler)}
		d.now = time.Now
		if d.lowDisk(t.Context()) {
			t.Error("sampler changed disk admission")
		}
		_ = d.diskStatus()
		close(read)
	}()
	select {
	case <-read:
	case <-time.After(time.Second):
		close(release)
		<-finished
		t.Fatal("snapshot waited for filesystem")
	}
	close(release)
	<-finished
	s.close()
}

func TestCacheSamplerDeadlineKeepsUnreadBatchAndCancellationCloses(t *testing.T) {
	main, reviewer := cacheFixture(t, "main"), cacheFixture(t, "reviewer")
	for i := 0; i < 4; i++ {
		cacheFile(t, filepath.Join(main, fmt.Sprint(i)), 2)
	}
	s := newCacheSampler(main, reviewer)
	defer s.close()
	now := time.Unix(100, 0)
	s.now = func() time.Time { return now }
	s.durationBudget = time.Second
	once := true
	s.beforeRead = func() {
		if once {
			once = false
			now = now.Add(2 * time.Second)
		}
	}
	scan := s.scans["main"]
	s.slice(context.Background(), scan)
	if scan.observation.EntriesExamined != 0 || len(scan.stack) == 0 || len(scan.stack[0].batch) != 4 {
		t.Fatalf("slow read lost batch: %+v", scan.observation)
	}
	s.beforeRead = nil
	s.slice(context.Background(), scan)
	if scan.observation.CompleteBytes == nil || *scan.observation.CompleteBytes != 8 {
		t.Fatalf("batch recounted/lost: %+v", scan.observation)
	}
	s.entryBudget = 1
	s.slice(context.Background(), scan)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.slice(ctx, scan)
	if scan.observation.State != "interrupted" || len(scan.stack) != 0 {
		t.Fatalf("cancel did not close scan: %+v", scan.observation)
	}
}

func TestCacheSamplerReplacementAndDepthErrors(t *testing.T) {
	main, reviewer := cacheFixture(t, "main"), cacheFixture(t, "reviewer")
	for i := 0; i < 3; i++ {
		cacheFile(t, filepath.Join(main, fmt.Sprint(i)), 10)
	}
	s := newCacheSampler(main, reviewer)
	defer s.close()
	s.entryBudget = 1
	s.round(context.Background())
	if err := os.Rename(main, main+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(main, 0700); err != nil {
		t.Fatal(err)
	}
	cacheFile(t, filepath.Join(main, "new"), 2)
	s.entryBudget = cacheSliceEntries
	s.round(context.Background())
	if o := s.Snapshot()["main"]; o.CompleteBytes == nil || *o.CompleteBytes != 2 || o.EntriesExamined != 1 {
		t.Fatalf("replacement retained prefix: %+v", o)
	}
	deep := main
	for i := 0; i < cacheScanDepth; i++ {
		deep = filepath.Join(deep, "d")
		if err := os.Mkdir(deep, 0700); err != nil {
			t.Fatal(err)
		}
	}
	s.round(context.Background())
	if o := s.Snapshot()["main"]; o.State != "incomplete" || o.Error == "" || o.CompleteBytes == nil || *o.CompleteBytes != 2 {
		t.Fatalf("depth silently omitted: %+v", o)
	}
}

func TestCacheSamplerDisappearingEntryDoesNotRestartPass(t *testing.T) {
	main, reviewer := cacheFixture(t, "main"), cacheFixture(t, "reviewer")
	for i := 0; i < 5; i++ {
		cacheFile(t, filepath.Join(main, fmt.Sprint(i)), 3)
	}
	s := newCacheSampler(main, reviewer)
	defer s.close()
	s.entryBudget = 1
	s.round(context.Background())
	scan := s.scans["main"]
	started := scan.observation.PassStartedAt
	victim := filepath.Join(scan.stack[0].path, scan.stack[0].batch[0].Name())
	if err := os.Remove(victim); err != nil {
		t.Fatal(err)
	}
	s.entryBudget = cacheSliceEntries
	s.round(context.Background())
	o := s.Snapshot()["main"]
	if o.State != "incomplete" || o.Error == "" || o.PartialBytes != 12 || o.EntriesExamined != 5 || o.CompleteBytes != nil || !o.PassStartedAt.Equal(started) {
		t.Fatalf("missing entry restarted/lost progress: %+v", o)
	}
}

func TestCacheSamplerDoesNotCrossVolumes(t *testing.T) {
	main, reviewer := cacheFixture(t, "main"), cacheFixture(t, "reviewer")
	nested := filepath.Join(main, "other-volume")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	cacheFile(t, filepath.Join(nested, "hidden"), 99)
	cacheFile(t, filepath.Join(main, "local"), 2)
	s := newCacheSampler(main, reviewer)
	defer s.close()
	s.device = func(info os.FileInfo) (string, bool) {
		if info.Name() == "other-volume" {
			return "2", true
		}
		return "1", true
	}
	s.round(context.Background())
	if o := s.Snapshot()["main"]; o.State != "incomplete" || o.PartialBytes != 2 || o.Error == "" || o.CompleteBytes != nil {
		t.Fatalf("crossed volume or silently completed: %+v", o)
	}
}

func TestCacheSamplerRestoreKeepsTimesButDiscardsTraversal(t *testing.T) {
	main, reviewer := cacheFixture(t, "main"), cacheFixture(t, "reviewer")
	cacheFile(t, filepath.Join(main, "fresh"), 9)
	previousTime := time.Unix(50, 0)
	complete := int64(40)
	s := newCacheSampler(main, reviewer)
	defer s.close()
	s.Restore(map[string]cacheSizeObservation{
		"main":     {Path: main, State: "partial", PartialBytes: 30, EntriesExamined: 12, CompleteBytes: &complete, CompleteAt: previousTime, ProgressAt: previousTime},
		"reviewer": {Path: "different-path", State: "complete", CompleteBytes: &complete},
	})
	restored := s.Snapshot()
	if o := restored["main"]; o.State != "interrupted" || !o.CompleteStale || o.PartialBytes != 30 || !o.CompleteAt.Equal(previousTime) || !o.ProgressAt.Equal(previousTime) {
		t.Fatalf("restore changed evidence: %+v", o)
	}
	if restored["reviewer"].State != "unavailable" {
		t.Fatal("restored different configuration")
	}
	s.entryBudget = 1
	s.round(context.Background())
	partial := s.Snapshot()["main"]
	if partial.PartialBytes != 9 || partial.EntriesExamined != 1 || !partial.CompleteStale || *partial.CompleteBytes != 40 {
		t.Fatalf("restored cursor/combined passes: %+v", partial)
	}
	s.round(context.Background())
	full := s.Snapshot()["main"]
	if full.State != "complete" || full.CompleteStale || *full.CompleteBytes != 9 {
		t.Fatalf("new complete remains stale: %+v", full)
	}
}

func TestCacheSamplerEachFilesystemStageYieldsAtDeadline(t *testing.T) {
	for _, stage := range []string{"root-lstat", "root-open", "root-stat", "child-lstat", "child-open", "child-stat"} {
		t.Run(stage, func(t *testing.T) {
			main, reviewer := cacheFixture(t, "main"), cacheFixture(t, "reviewer")
			child := filepath.Join(main, "child")
			if err := os.Mkdir(child, 0700); err != nil {
				t.Fatal(err)
			}
			cacheFile(t, filepath.Join(child, "file"), 7)
			s := newCacheSampler(main, reviewer)
			defer s.close()
			now := time.Unix(100, 0)
			s.now = func() time.Time { return now }
			s.durationBudget = time.Second
			var calls []string
			yielded := false
			record := func(call string) {
				calls = append(calls, call)
				if call == stage && !yielded {
					now = now.Add(2 * time.Second)
					yielded = true
				}
			}
			lstats, opens, stats := 0, 0, 0
			s.afterLstat = func() {
				lstats++
				if lstats == 1 {
					record("root-lstat")
				} else {
					record("child-lstat")
				}
			}
			s.afterOpen = func() {
				opens++
				if opens == 1 {
					record("root-open")
				} else {
					record("child-open")
				}
			}
			s.afterStat = func() {
				stats++
				if stats == 1 {
					record("root-stat")
				} else {
					record("child-stat")
				}
			}
			s.beforeRead = func() { record("read") }
			scan := s.scans["main"]
			s.slice(t.Context(), scan)
			if !yielded || calls[len(calls)-1] != stage {
				t.Fatalf("issued more filesystem calls after %s: %v", stage, calls)
			}
			s.afterLstat = nil
			s.afterOpen = nil
			s.afterStat = nil
			s.beforeRead = nil
			for i := 0; i < 5 && scan.observation.State != "complete"; i++ {
				s.slice(t.Context(), scan)
			}
			o := scan.observation
			if o.State != "complete" || o.CompleteBytes == nil || *o.CompleteBytes != 7 || o.EntriesExamined != 2 {
				t.Fatalf("pending stage lost/recounted entry: %+v", o)
			}
		})
	}
}

func TestCacheSamplerRepeatedSlowReturningCallsStillComplete(t *testing.T) {
	for _, allStages := range []bool{false, true} {
		t.Run(fmt.Sprintf("all_stages=%t", allStages), func(t *testing.T) {
			main, reviewer := cacheFixture(t, "main"), cacheFixture(t, "reviewer")
			nested := filepath.Join(main, "nested")
			if err := os.Mkdir(nested, 0700); err != nil {
				t.Fatal(err)
			}
			cacheFile(t, filepath.Join(main, "one"), 3)
			cacheFile(t, filepath.Join(nested, "two"), 7)
			s := newCacheSampler(main, reviewer)
			defer s.close()
			now := time.Unix(100, 0)
			s.now = func() time.Time { return now }
			s.durationBudget = time.Second
			calls := 0
			slow := func() { calls++; now = now.Add(2 * time.Second) }
			// Every metadata call, including every repeated root probe, exceeds the
			// slice budget but returns. Retained root/entry stages must still advance.
			s.afterLstat = slow
			if allStages {
				s.afterOpen = slow
				s.afterStat = slow
				s.beforeRead = slow
			}
			scan := s.scans["main"]
			partialSeen := false
			for i := 0; i < 100 && scan.observation.State != "complete"; i++ {
				s.slice(t.Context(), scan)
				if scan.observation.State == "partial" && scan.observation.PartialBytes > 0 {
					partialSeen = true
				}
			}
			o := scan.observation
			if calls < 4 || !partialSeen || o.State != "complete" || o.CompleteBytes == nil || *o.CompleteBytes != 10 || o.EntriesExamined != 3 {
				t.Fatalf("slow returning calls prevented progress: calls=%d observation=%+v", calls, o)
			}
		})
	}
}
