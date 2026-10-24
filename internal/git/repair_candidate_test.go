package git_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
)

func TestRepairCandidateRetainsCompleteTreeWithoutChangingLiveIndex(t *testing.T) {
	w, dir, base, repo := repairWorktree(t)
	put(t, dir, "committed.go", "package a\nvar Committed = 1\n")
	repo.GitIn(dir, "add", "committed.go")
	repo.GitIn(dir, "commit", "-m", "agent commit")
	head := repo.GitIn(dir, "rev-parse", "HEAD")
	put(t, dir, "edit.go", "package a\nvar Staged = 2\n")
	repo.GitIn(dir, "add", "edit.go")
	put(t, dir, "edit.go", "package a\nvar Unstaged = 3\n")
	put(t, dir, "large.txt", strings.Repeat("complete review content\n", 110000))
	put(t, dir, "binary.dat", "\x00\xff\x01binary\x00")
	put(t, dir, ".gitignore", "ignored.txt\n")
	put(t, dir, "ignored.txt", "excluded\n")
	if err := os.Symlink("does-not-exist", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "keep.go")); err != nil {
		t.Fatal(err)
	}
	indexBefore := repo.GitIn(dir, "ls-files", "--stage")
	statusBefore := repo.GitIn(dir, "status", "--porcelain=v1", "--untracked-files=all")
	snapshot, err := w.CaptureRepairCandidate(t.Context(), base, "mixed-candidate")
	if err != nil {
		t.Fatal(err)
	}
	if got := repo.GitIn(dir, "ls-files", "--stage"); got != indexBefore {
		t.Fatalf("capture modified live index: %q != %q", got, indexBefore)
	}
	if got := repo.GitIn(dir, "status", "--porcelain=v1", "--untracked-files=all"); got != statusBefore {
		t.Fatal("capture modified working files")
	}
	if got := repo.GitIn(dir, "rev-parse", "HEAD"); got != head {
		t.Fatal("capture moved branch")
	}
	if len(snapshot.Patch) <= 2<<20 || !strings.Contains(snapshot.Patch, "GIT binary patch") || !strings.Contains(snapshot.Patch, "+does-not-exist") {
		t.Fatal("capture omitted large, binary or symlink content")
	}
	paths := map[string]string{}
	for _, change := range snapshot.Changes {
		paths[change.Path] = change.NewMode
	}
	if paths["link"] != "120000" || paths["keep.go"] != "000000" || len(paths) != 7 {
		t.Fatalf("incomplete path/mode manifest: %v", paths)
	}
	if err = w.ResetHard(t.Context(), base); err != nil {
		t.Fatal(err)
	}
	// The preexisting reset leaves ignored data alone; it is unrelated to the
	// proposed tree and is not allowed to obscure adoption's clean precondition.
	if err = os.Remove(filepath.Join(dir, "ignored.txt")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err = w.VerifyRepairCandidate(t.Context(), snapshot); err != nil {
		t.Fatalf("snapshot did not survive reset: %v", err)
	}
	result, err := w.PrepareRepairAdoption(t.Context(), snapshot, git.Author{Name: "Test Owner", Email: "owner@example.invalid"})
	if err != nil {
		t.Fatal(err)
	}
	if replay, err := w.PrepareRepairAdoption(t.Context(), snapshot, git.Author{Name: "Changed Profile", Email: "changed@example.invalid"}); err != nil || replay != result {
		t.Fatalf("prepare did not recover its retained intent: %s %v", replay, err)
	}
	// Simulate a restart after the conditional branch update but before checkout.
	repo.GitIn(dir, "update-ref", "refs/heads/"+w.Branch(), result, base)
	if err = w.ApplyRepairAdoption(t.Context(), snapshot, result); err != nil {
		t.Fatalf("%v\nstatus: %s\nbase: %s\ncached: %s", err, repo.GitIn(dir, "status", "--porcelain=v1", "--untracked-files=all"), repo.GitIn(dir, "diff", "--raw", base), repo.GitIn(dir, "diff", "--cached", "--raw", base))
	}
	if got := repo.GitIn(dir, "rev-parse", "HEAD^{tree}"); got != snapshot.Tree {
		t.Fatalf("adopted tree %s != %s", got, snapshot.Tree)
	}
	if err = w.ApplyRepairAdoption(t.Context(), snapshot, result); err != nil {
		t.Fatalf("replayed application failed: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "binary.dat"))
	if err != nil || string(content) != "\x00\xff\x01binary\x00" {
		t.Fatalf("binary content lost: %q %v", content, err)
	}
	if got := repo.GitIn(dir, "show", "HEAD:edit.go"); got != "package a\nvar Unstaged = 3" {
		t.Fatalf("unstaged content lost: %q", got)
	}
}

func TestRepairCandidateRejectsChangedEvidenceAndDrift(t *testing.T) {
	w, dir, base, repo := repairWorktree(t)
	put(t, dir, "edit.go", "package a\nvar Repair = 1\n")
	snapshot, err := w.CaptureRepairCandidate(t.Context(), base, "drift-candidate")
	if err != nil {
		t.Fatal(err)
	}
	altered := snapshot
	altered.Patch += "hidden change"
	if err = w.VerifyRepairCandidate(t.Context(), altered); err == nil {
		t.Fatal("changed patch accepted")
	}
	altered = snapshot
	altered.Changes = append([]git.RepairPathChange(nil), snapshot.Changes...)
	altered.Changes[0].Path = "other.go"
	if err = w.VerifyRepairCandidate(t.Context(), altered); err == nil {
		t.Fatal("changed manifest accepted")
	}
	if err = w.ResetHard(t.Context(), base); err != nil {
		t.Fatal(err)
	}
	result, err := w.PrepareRepairAdoption(t.Context(), snapshot, git.Author{Name: "Owner", Email: "owner@example.invalid"})
	if err != nil {
		t.Fatal(err)
	}
	put(t, dir, "edit.go", "package a\nvar Other = 2\n")
	if err = w.ApplyRepairAdoption(t.Context(), snapshot, result); err == nil {
		t.Fatal("dirty checkout accepted")
	}
	if got := repo.GitIn(dir, "rev-parse", "HEAD"); got != base {
		t.Fatal("ref moved despite checkout drift")
	}
	repo.GitIn(dir, "add", "edit.go")
	repo.GitIn(dir, "commit", "-m", "new base")
	if err = w.ApplyRepairAdoption(t.Context(), snapshot, result); err == nil {
		t.Fatal("changed branch accepted")
	}
	// Reusing a candidate identity with different content must not rebind its ref.
	if _, err = w.CaptureRepairCandidate(t.Context(), base, "drift-candidate"); err == nil {
		t.Fatal("candidate identity rebound")
	}
	if err = w.VerifyRepairCandidate(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot.Changes, []git.RepairPathChange{{Path: "edit.go", OldMode: "100644", NewMode: "100644", OldObject: snapshot.Changes[0].OldObject, NewObject: snapshot.Changes[0].NewObject}}) {
		t.Fatal("unexpected manifest")
	}
}

func TestRepairCandidateRefusesDirtySubmoduleBeforeReset(t *testing.T) {
	w, dir, _, repo := repairWorktree(t)
	nested := gittest.New(t)
	repo.GitIn(dir, "-c", "protocol.file.allow=always", "submodule", "add", nested.Dir, "nested")
	repo.GitIn(dir, "commit", "-m", "track nested repository")
	base := repo.GitIn(dir, "rev-parse", "HEAD")
	put(t, dir, "nested/untracked.txt", "must survive\n")
	if _, err := w.CaptureRepairCandidate(t.Context(), base, "dirty-submodule"); err == nil {
		t.Fatal("incomplete submodule content captured")
	}
	content, err := os.ReadFile(filepath.Join(dir, "nested", "untracked.txt"))
	if err != nil || string(content) != "must survive\n" {
		t.Fatalf("capture failure damaged source: %q %v", content, err)
	}
	if got := repo.GitIn(dir, "rev-parse", "HEAD"); got != base {
		t.Fatal("capture failure moved branch")
	}
}

func TestRepairCandidateResetRefusesDriftAndRecoversExactEndpoints(t *testing.T) {
	w, dir, base, repo := repairWorktree(t)
	put(t, dir, "edit.go", "package a\nvar Captured = 1\n")
	put(t, dir, "new.go", "package a\n")
	snapshot, err := w.CaptureRepairCandidate(t.Context(), base, "guarded-reset")
	if err != nil {
		t.Fatal(err)
	}
	put(t, dir, "edit.go", "package a\nvar Drift = 2\n")
	if err = w.ResetRepairCandidate(t.Context(), snapshot); err == nil {
		t.Fatal("reset destroyed uncaptured edits")
	}
	if got, err := os.ReadFile(filepath.Join(dir, "edit.go")); err != nil || !strings.Contains(string(got), "Drift") {
		t.Fatal("reset refusal changed source")
	}
	put(t, dir, "edit.go", "package a\nvar Captured = 1\n")
	if err = w.ResetRepairCandidate(t.Context(), snapshot); err != nil {
		t.Fatalf("exact captured endpoint refused: %v", err)
	}
	if err = w.ResetRepairCandidate(t.Context(), snapshot); err != nil {
		t.Fatalf("clean base replay refused: %v", err)
	}
	if got := repo.GitIn(dir, "rev-parse", "HEAD"); got != base {
		t.Fatal("reset did not reach its saved base")
	}
	if _, err := os.Stat(filepath.Join(dir, "new.go")); !os.IsNotExist(err) {
		t.Fatal("captured untracked source survived reset")
	}
	// A reset interrupted after only some files were restored is ambiguous.
	put(t, dir, "new.go", "package a\n")
	if err = w.ResetRepairCandidate(t.Context(), snapshot); err == nil {
		t.Fatal("mixed reset endpoint accepted")
	}
}

func TestRepairCandidateResetRemovesAdditionsHiddenByBaseIgnoreRules(t *testing.T) {
	w, dir, _, repo := repairWorktree(t)
	put(t, dir, ".gitignore", "hidden.txt\n")
	put(t, dir, "restored/child.txt", "base bytes\n")
	repo.GitIn(dir, "add", ".gitignore", "restored/child.txt")
	repo.GitIn(dir, "commit", "-m", "base ignores candidate addition")
	base := repo.GitIn(dir, "rev-parse", "HEAD")
	put(t, dir, ".gitignore", "# reveal repair file\n")
	put(t, dir, "hidden.txt", "retained candidate\n")
	if err := os.RemoveAll(filepath.Join(dir, "restored")); err != nil {
		t.Fatal(err)
	}
	put(t, dir, "restored", "candidate replaces directory\n")
	snapshot, err := w.CaptureRepairCandidate(t.Context(), base, "ignored-reset")
	if err != nil {
		t.Fatal(err)
	}
	if err = w.ResetRepairCandidate(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "hidden.txt")); !os.IsNotExist(err) {
		t.Fatal("base ignore rules hid retained candidate content from cleanup")
	}
	if got, err := os.ReadFile(filepath.Join(dir, "restored", "child.txt")); err != nil || string(got) != "base bytes\n" {
		t.Fatalf("cleanup removed restored base directory: %q %v", got, err)
	}
	if err = w.ResetRepairCandidate(t.Context(), snapshot); err != nil {
		t.Fatalf("clean base replay failed: %v", err)
	}
	result, err := w.PrepareRepairAdoption(t.Context(), snapshot, git.Author{Name: "Owner", Email: "owner@example.invalid"})
	if err != nil {
		t.Fatal(err)
	}
	// Ignored local bytes created after reset must not be overwritten by accept.
	put(t, dir, "hidden.txt", "local bytes\n")
	if err = w.ApplyRepairAdoption(t.Context(), snapshot, result); err == nil {
		t.Fatal("adoption overwrote ignored local overlap")
	}
	if got, err := os.ReadFile(filepath.Join(dir, "hidden.txt")); err != nil || string(got) != "local bytes\n" {
		t.Fatal("refused adoption changed ignored content")
	}
	if err = w.ResetRepairCandidate(t.Context(), snapshot); err == nil {
		t.Fatal("hidden reset remnant was treated as clean base")
	}
	if err = os.Remove(filepath.Join(dir, "hidden.txt")); err != nil {
		t.Fatal(err)
	}
	if err = w.ApplyRepairAdoption(t.Context(), snapshot, result); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "hidden.txt")); err != nil || string(got) != "retained candidate\n" {
		t.Fatalf("adoption lost retained hidden addition: %q %v", got, err)
	}
}

func TestRepairCandidateIncludesForceStagedIgnoredAddition(t *testing.T) {
	w, dir, _, repo := repairWorktree(t)
	put(t, dir, ".gitignore", "hidden.txt\nother.txt\n")
	repo.GitIn(dir, "add", ".gitignore")
	repo.GitIn(dir, "commit", "-m", "ignore fixture")
	base := repo.GitIn(dir, "rev-parse", "HEAD")
	put(t, dir, "hidden.txt", "force staged\n")
	put(t, dir, "other.txt", "never staged\n")
	repo.GitIn(dir, "add", "--force", "hidden.txt")
	snapshot, err := w.CaptureRepairCandidate(t.Context(), base, "force-staged")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Changes) != 1 || snapshot.Changes[0].Path != "hidden.txt" || !strings.Contains(snapshot.Patch, "+force staged") {
		t.Fatalf("staged ignored content omitted or unrelated ignored file captured: %+v", snapshot.Changes)
	}
}

func TestRepairCandidateRefusesNonignoredFIFOAndPreservesIgnoredFIFO(t *testing.T) {
	w, dir, base, repo := repairWorktree(t)
	pipe := filepath.Join(dir, "repair.pipe")
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Fatal(err)
	}
	put(t, dir, "edit.go", "package a\nvar Repair = 1\n")
	if _, err := w.CaptureRepairCandidate(t.Context(), base, "unsupported-pipe"); err == nil {
		t.Fatal("nonignored FIFO silently omitted from capture")
	}
	if info, err := os.Lstat(pipe); err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("capture failure damaged FIFO source: %v", err)
	}
	if got := repo.GitIn(dir, "rev-parse", "HEAD"); got != base {
		t.Fatal("unsupported-file capture moved branch")
	}
	put(t, dir, ".gitignore", "repair.pipe\n")
	snapshot, err := w.CaptureRepairCandidate(t.Context(), base, "ignored-pipe")
	if err != nil {
		t.Fatalf("unrelated ignored FIFO blocked complete capture: %v", err)
	}
	for _, change := range snapshot.Changes {
		if change.Path == "repair.pipe" {
			t.Fatal("ignored FIFO entered retained tree")
		}
	}
	if info, err := os.Lstat(pipe); err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatal("ignored FIFO changed during capture")
	}
}

func TestRepairCandidateResetAndAdoptionRefuseNewUnsupportedSource(t *testing.T) {
	w, dir, base, _ := repairWorktree(t)
	put(t, dir, "added.go", "package a\n")
	snapshot, err := w.CaptureRepairCandidate(t.Context(), base, "late-pipe")
	if err != nil {
		t.Fatal(err)
	}
	if err = w.ResetRepairCandidate(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	result, err := w.PrepareRepairAdoption(t.Context(), snapshot, git.Author{Name: "Owner", Email: "owner@example.invalid"})
	if err != nil {
		t.Fatal(err)
	}
	if err = syscall.Mkfifo(filepath.Join(dir, "added.go"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = w.ResetRepairCandidate(t.Context(), snapshot); err == nil {
		t.Fatal("clean-base reset ignored new unsupported source")
	}
	if err = w.ApplyRepairAdoption(t.Context(), snapshot, result); err == nil {
		t.Fatal("adoption overwrote unsupported source")
	}
	if info, err := os.Lstat(filepath.Join(dir, "added.go")); err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatal("refused reset or adoption damaged source FIFO")
	}
}
