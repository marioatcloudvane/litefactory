package git

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

// ErrBugCheckRecipe marks an absent or structurally invalid configured recipe.
var ErrBugCheckRecipe = errors.New("bug target recipe is unavailable or invalid")

// ReadBugCheckRecipe reads only a regular pinned recipe blob. Checking object
// size before cat-file bounds allocation because content-addressed git objects
// cannot change size between these reads.
func (w *Worktree) ReadBugCheckRecipe(ctx context.Context, commit string) ([]byte, error) {
	if !fullBugObject(commit) {
		return nil, errors.New("bug recipe requires full commit object identity")
	}
	actual, err := w.runner.run(ctx, readTimeout, "rev-parse", "--verify", commit+"^{commit}")
	if err != nil {
		return nil, err
	}
	if actual != commit {
		return nil, errors.New("bug recipe object is not exact commit")
	}
	const path = ".litefactory/bug-checks.yaml"
	entry, err := w.runner.run(ctx, readTimeout, "ls-tree", commit, "--", path)
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(entry)
	if len(fields) != 4 || (fields[0] != "100644" && fields[0] != "100755") || fields[1] != "blob" || fields[3] != path || !fullBugObject(fields[2]) {
		return nil, ErrBugCheckRecipe
	}
	sizeText, err := w.runner.run(ctx, readTimeout, "cat-file", "-s", fields[2])
	if err != nil {
		return nil, err
	}
	size, err := strconv.Atoi(sizeText)
	if err != nil || size < 1 || size > 1<<20 {
		return nil, ErrBugCheckRecipe
	}
	data, err := w.runner.runRaw(ctx, readTimeout, "cat-file", "blob", fields[2])
	if err != nil {
		return nil, err
	}
	if len(data) != size {
		return nil, errors.New("bug recipe blob size mismatch")
	}
	return []byte(data), nil
}
func fullBugObject(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, c := range value {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
