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
