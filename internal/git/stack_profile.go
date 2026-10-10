package git

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

// ReadStackOverrides reads the optional bounded regular override blob from an
// exact commit. Dirty files, symlinks and mutable revision names are never used.
func (c *Clone) ReadStackOverrides(ctx context.Context, commit string) ([]byte, error) {
	if c == nil || c.runner.binary == "" || !fullBugObject(commit) {
		return nil, errors.New("stack.revision: requires initialized clone and full commit identity")
	}
	actual, err := c.runner.run(ctx, readTimeout, "rev-parse", "--verify", commit+"^{commit}")
	if err != nil {
		return nil, err
	}
	if actual != commit {
		return nil, errors.New("stack.revision: object is not an exact commit")
	}
	parent, err := c.runner.run(ctx, readTimeout, "ls-tree", commit, "--", ".litefactory")
	if err != nil {
		return nil, err
	}
	if parent == "" {
		return nil, nil
	}
	fields := strings.Fields(parent)
	if len(fields) != 4 || fields[0] != "040000" || fields[1] != "tree" || fields[3] != ".litefactory" {
		return nil, errors.New("stack.overrides: parent must be a directory")
	}
	const filename = ".litefactory/stack.overrides.json"
	entry, err := c.runner.run(ctx, readTimeout, "ls-tree", commit, "--", filename)
	if err != nil {
		return nil, err
	}
	if entry == "" {
		return nil, nil
	}
	fields = strings.Fields(entry)
	if len(fields) != 4 || (fields[0] != "100644" && fields[0] != "100755") || fields[1] != "blob" || fields[3] != filename || !fullBugObject(fields[2]) {
		return nil, errors.New("stack.overrides: requires regular file")
	}
	sizeText, err := c.runner.run(ctx, readTimeout, "cat-file", "-s", fields[2])
	if err != nil {
		return nil, err
	}
	size, err := strconv.Atoi(sizeText)
	if err != nil || size < 0 || size > 1<<20 {
		return nil, errors.New("stack.overrides: blob exceeds bounds")
	}
	data, err := c.runner.runRaw(ctx, readTimeout, "cat-file", "blob", fields[2])
	if err != nil {
		return nil, err
	}
	if len(data) != size {
		return nil, errors.New("stack.overrides: blob size mismatch")
	}
	return []byte(data), nil
}
