package git_test

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
)

// TestCloneMethodSetIsTheAllowlist pins the exported surface of git.Clone.
//
// The product's central guarantee — litefactory never writes inside the
// repository you registered — is enforced by the shape of this type: the S-6
// allowlist is the whole method set, so a call to Commit or Checkout on a
// registered clone does not compile. That guarantee is only as durable as the
// method set, so adding a method here has to be a deliberate act with a failing
// test in front of it rather than an afternoon's convenience.
func TestCloneMethodSetIsTheAllowlist(t *testing.T) {
	t.Parallel()

	want := []string{
		// The S-6 command allowlist.
		"ConfigGet",
		"Fetch",
		"RemoteURL",
		"RevParse",
		"ShowRef",
		"WorktreeAdd",
		"WorktreeList",
		"WorktreeRemove",
		// Path reads a field rather than running git, so it is not a command.
		"Path",
	}
	slices.Sort(want)

	got := exportedMethods(reflect.TypeOf((*git.Clone)(nil)))
	if !slices.Equal(got, want) {
		t.Fatalf("git.Clone method set changed.\n got: %s\nwant: %s\n\n"+
			"The method set of Clone is the S-6 allowlist. If a new git command "+
			"really belongs against the registered clone, amend S-6 first.",
			strings.Join(got, ", "), strings.Join(want, ", "))
	}
}

// TestCloneCannotMutateTheRegisteredClone states the negative directly, so the
// failure message explains itself to whoever added the method.
func TestCloneCannotMutateTheRegisteredClone(t *testing.T) {
	t.Parallel()

	forbidden := []string{
		"Commit", "Checkout", "Switch", "Stash", "Reset", "Merge", "Pull",
		"Add", "Push", "Rebase", "CherryPick", "Clean", "Apply", "Restore",
	}
	methods := exportedMethods(reflect.TypeOf((*git.Clone)(nil)))
	for _, name := range forbidden {
		if slices.Contains(methods, name) {
			t.Errorf("git.Clone has a %s method. Mutating operations belong on "+
				"git.Worktree; the registered clone is never written to.", name)
		}
	}
}

// TestWorktreeMethodSetIsTheAllowlist pins the exported surface of
// git.Worktree, the same guarantee [TestCloneMethodSetIsTheAllowlist] pins
// for git.Clone.
//
// This test predates every mutator this list now carries: it was added
// against the read-only-plus-Commit/Push/MergeBase surface Worktree had
// before US-12, specifically so US-12's three new mutators
// (WriteResolvedFiles, StageResolvedFiles, CompleteMerge) would have to land
// on top of a failing test rather than a passing one. Every method below
// carries the reason it was allowed onto this list.
func TestWorktreeMethodSetIsTheAllowlist(t *testing.T) {
	t.Parallel()

	want := []string{
		// Read-only: record what happened, touch neither the working tree
		// nor a ref (ConflictBlob and MergePreview do write loose objects,
		// documented on MergePreview itself, but move nothing a ref points
		// at).
		"Path", "Branch", "ClonePath", "RevParse", "Status", "Divergence",
		"MergePreview", "ConflictBlob",
		// US-003 authoritative server observation; no local or remote mutation.
		"RemoteHead",
		// US-1 bounded immutable blob/mode collection, no index/worktree/ref mutations.
		"CollectConflictSnapshots",
		// Mutating, pre-US-12: the only three operations that write to a
		// repository or a remote before this story.
		"Commit", "Push", "MergeBase",
		// US-12's three new mutators. Each is deliberately narrow rather
		// than a general Apply/Checkout/Reset: WriteResolvedFiles starts the
		// merge a confirmed proposal resolves and writes exactly its
		// confirmed paths (S-13); StageResolvedFiles stages exactly those
		// paths; CompleteMerge finishes the merge with the identity prelude
		// and this story's trailers, reusing the same commit mechanism
		// [Worktree.Commit] already has. All three refuse a path outside
		// the confirmed list and roll back on any failure via
		// abortInProgressMerge (S-5).
		"WriteResolvedFiles", "StageResolvedFiles", "CompleteMerge",
	}
	slices.Sort(want)

	got := exportedMethods(reflect.TypeOf((*git.Worktree)(nil)))
	if !slices.Equal(got, want) {
		t.Fatalf("git.Worktree method set changed.\n got: %s\nwant: %s\n\n"+
			"The method set of Worktree is the whole mutating surface litefactory "+
			"has. If a new git command really belongs here, add it with a recorded "+
			"reason and amend this list deliberately.",
			strings.Join(got, ", "), strings.Join(want, ", "))
	}
}

// TestWorktreeCannotRunGeneralPurposeMutators states the negative directly,
// mirroring [TestCloneCannotMutateTheRegisteredClone]: none of these ever
// land on Worktree, however convenient a future caller might find one.
// Reset in particular is watch-out #2 of US-12 by name — it must never
// appear here even as a self-heal for a failed rollback; every mutator in
// this package rolls back with "git merge --abort" and a re-verify instead
// (S-5).
func TestWorktreeCannotRunGeneralPurposeMutators(t *testing.T) {
	t.Parallel()

	forbidden := []string{
		"Reset", "Rebase", "CherryPick", "Clean", "Stash", "Checkout", "Switch",
		"Apply", "Restore", "Pull",
	}
	methods := exportedMethods(reflect.TypeOf((*git.Worktree)(nil)))
	for _, name := range forbidden {
		if slices.Contains(methods, name) {
			t.Errorf("git.Worktree has a %s method; this package only ever rolls "+
				"back with \"git merge --abort\" plus a re-verify, never a general-"+
				"purpose reset or checkout", name)
		}
	}
}

// TestWorktreeHasNoExportedConstructionPath checks the other half of the
// mechanism. Worktree carries the mutating operations, so it must be reachable
// only through Clone.WorktreeAdd — which refuses any path overlapping the
// registered clone. Every field is unexported, so a composite literal built
// outside this package cannot be given a directory to act on.
func TestWorktreeHasNoExportedConstructionPath(t *testing.T) {
	t.Parallel()

	typ := reflect.TypeOf(git.Worktree{})
	for i := range typ.NumField() {
		if field := typ.Field(i); field.IsExported() {
			t.Errorf("git.Worktree.%s is exported; a caller could build a "+
				"usable Worktree without going through Clone.WorktreeAdd", field.Name)
		}
	}
}

// TestZeroValuesRefuseToRunGit covers what the type system cannot: a
// git.Worktree{} or git.Clone{} literal still compiles. It must be inert rather
// than running git in whatever directory the process happens to be in — which,
// for the daemon, could be the registered clone.
func TestZeroValuesRefuseToRunGit(t *testing.T) {
	t.Parallel()

	var clone git.Clone
	if err := clone.Fetch(t.Context(), "origin"); !errors.Is(err, git.ErrUninitialized) {
		t.Errorf("zero Clone.Fetch error = %v, want ErrUninitialized", err)
	}
	if _, err := clone.RevParse(t.Context(), "HEAD", git.RevParseOptions{}); !errors.Is(err, git.ErrUninitialized) {
		t.Errorf("zero Clone.RevParse error = %v, want ErrUninitialized", err)
	}

	var worktree git.Worktree
	if _, err := worktree.RevParse(t.Context(), "HEAD", git.RevParseOptions{}); !errors.Is(err, git.ErrUninitialized) {
		t.Errorf("zero Worktree.RevParse error = %v, want ErrUninitialized", err)
	}
	if got := worktree.Path(); got != "" {
		t.Errorf("zero Worktree.Path() = %q, want empty", got)
	}
}

func exportedMethods(typ reflect.Type) []string {
	names := make([]string, 0, typ.NumMethod())
	for i := range typ.NumMethod() {
		names = append(names, typ.Method(i).Name)
	}
	slices.Sort(names)
	return names
}
