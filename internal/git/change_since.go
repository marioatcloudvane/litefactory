package git

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// maxChangedSince bounds the paths one measurement will list.
const maxChangedSince = 1024

// ChangedSince lists, once each and sorted, every path that differs from base:
// what was committed after it, what is changed or staged in the worktree and
// what is untracked. A rename counts both of its paths. It changes nothing and
// takes no index lock. A base that is not a full commit object, or a listing
// beyond the bound, is an error rather than a partial answer.
func (w *Worktree) ChangedSince(ctx context.Context, base string) ([]string, error) {
	if err := w.check(); err != nil {
		return nil, err
	}
	if !fullBugObject(base) {
		return nil, errors.New("changed-since baseline must be a full commit object")
	}
	actual, err := w.runner.run(ctx, readTimeout, "rev-parse", "--verify", base+"^{commit}")
	if err != nil {
		return nil, err
	}
	if actual != base {
		return nil, errors.New("changed-since baseline is not the exact commit")
	}
	names, err := w.runner.runRaw(ctx, readTimeout, "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--name-only", "-z", base, "HEAD", "--")
	if err != nil {
		return nil, err
	}
	unique := map[string]bool{}
	for _, path := range strings.Split(names, "\x00") {
		if path != "" {
			unique[path] = true
		}
	}
	changes, err := w.Status(ctx)
	if err != nil {
		return nil, err
	}
	for _, change := range changes {
		unique[change.Path] = true
		if change.From != "" {
			unique[change.From] = true
		}
	}
	if len(unique) > maxChangedSince {
		return nil, fmt.Errorf("changed-since lists more than %d paths", maxChangedSince)
	}
	out := make([]string, 0, len(unique))
	for path := range unique {
		out = append(out, path)
	}
	sort.Strings(out)
	return out, nil
}
