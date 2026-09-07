package team

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadLockfileMissingReturnsErrNoLockfile(t *testing.T) {
	root := t.TempDir()
	lf, err := loadLockfile(root)
	if !errors.Is(err, ErrNoLockfile) {
		t.Fatalf("error = %v, want ErrNoLockfile", err)
	}
	if lf == nil || lf.Version != lockfileSchemaVersion || len(lf.Records) != 0 {
		t.Fatalf("lf = %+v, want empty lockfile at current schema version", lf)
	}
}

func TestLoadLockfileCorruptJSON(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, litefactoryDir)
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, lockfileName), []byte("{not valid json"), 0o666); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := loadLockfile(root)
	if !errors.Is(err, ErrLockfileCorrupt) {
		t.Fatalf("error = %v, want ErrLockfileCorrupt", err)
	}
}

func TestLockfileSaveAndLoadRoundTrip(t *testing.T) {
	root := t.TempDir()
	lf := &Lockfile{
		Records: []LockRecord{
			{
				AgentKey:     "go-architect",
				Harness:      "claude",
				DestPath:     ".claude/agents/go-architect.md",
				SourcePath:   "src/go-architect.md",
				SourceHash:   "abc123",
				RenderedHash: "def456",
			},
		},
	}
	if err := lf.save(root); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := loadLockfile(root)
	if err != nil {
		t.Fatalf("loadLockfile after save: %v", err)
	}
	if loaded.Version != lockfileSchemaVersion {
		t.Errorf("Version = %d, want %d", loaded.Version, lockfileSchemaVersion)
	}
	if len(loaded.Records) != 1 {
		t.Fatalf("Records = %d, want 1", len(loaded.Records))
	}
	if loaded.Records[0] != lf.Records[0] {
		t.Errorf("round-tripped record = %+v, want %+v", loaded.Records[0], lf.Records[0])
	}
}

func TestLockRecordMatchesRequiresBothHashes(t *testing.T) {
	base := LockRecord{
		AgentKey: "go-architect", Harness: "claude", DestPath: ".claude/agents/go-architect.md",
		SourcePath: "src/go-architect.md", SourceHash: "src-hash", RenderedHash: "rendered-hash",
	}

	sameEverything := base
	if !base.matches(sameEverything) {
		t.Error("matches() = false for an identical record")
	}

	diffSource := base
	diffSource.SourceHash = "different"
	if base.matches(diffSource) {
		t.Error("matches() = true despite a different SourceHash")
	}

	diffRendered := base
	diffRendered.RenderedHash = "different"
	if base.matches(diffRendered) {
		t.Error("matches() = true despite a different RenderedHash")
	}
}

func TestLockfileFindAndUpsertRecord(t *testing.T) {
	lf := &Lockfile{}
	rec := LockRecord{
		AgentKey: "go-architect", Harness: "claude", DestPath: ".claude/agents/go-architect.md",
		SourcePath: "src/go-architect.md", SourceHash: "h1", RenderedHash: "h2",
	}

	if _, found := lf.findRecord(rec); found {
		t.Fatal("findRecord found a record in an empty lockfile")
	}

	lf.upsertRecord(rec)
	got, found := lf.findRecord(rec)
	if !found {
		t.Fatal("findRecord did not find the record after upsertRecord")
	}
	if got != rec {
		t.Errorf("found record = %+v, want %+v", got, rec)
	}

	updated := rec
	updated.RenderedHash = "h3"
	lf.upsertRecord(updated)
	if len(lf.Records) != 1 {
		t.Fatalf("Records = %d after upserting same key, want 1 (update in place, not append)", len(lf.Records))
	}
	got2, found := lf.findRecord(rec)
	if !found || got2.RenderedHash != "h3" {
		t.Errorf("record after upsert = %+v, want RenderedHash h3", got2)
	}
}
