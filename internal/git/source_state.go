package git

import (
	"context"
	"path/filepath"
	"strings"
)

// FolderState reports the HEAD commit of the clone and whether the folder dir
// inside it has uncommitted changes, untracked files included. A team install
// records both so an installed team names the source it was built from. It
// runs only local plumbing.
func (c *Clone) FolderState(ctx context.Context, dir string) (head string, dirty bool, err error) {
	head, err = c.RevParse(ctx, "HEAD", RevParseOptions{})
	if err != nil {
		return "", false, err
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", false, err
	}
	rel, err := filepath.Rel(c.path, resolved)
	if err != nil {
		return "", false, err
	}
	rel = filepath.ToSlash(rel)
	out, err := c.runner.run(ctx, readTimeout, statusArgs...)
	if err != nil {
		return "", false, err
	}
	changes, err := parseStatus(out)
	if err != nil {
		return "", false, err
	}
	for _, change := range changes {
		for _, p := range []string{change.Path, change.From} {
			if p != "" && (rel == "." || p == rel || strings.HasPrefix(p, rel+"/")) {
				return head, true, nil
			}
		}
	}
	return head, false, nil
}
