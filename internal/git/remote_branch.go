package git

import (
	"context"
	"fmt"
	"strings"
)

// RemoteBranchExists asks the clone's origin whether refs/heads/<branch>
// exists. An empty answer means it does not; an error means the remote could
// not be read, which the caller must not treat as "missing".
func (c *Clone) RemoteBranchExists(ctx context.Context, branch string) (bool, error) {
	if c == nil {
		return false, ErrUninitialized
	}
	if err := checkName("branch", branch); err != nil {
		return false, err
	}
	out, err := c.runner.run(ctx, fetchTimeout, "ls-remote", "--refs", "--", "origin", "refs/heads/"+branch)
	if err != nil {
		return false, fmt.Errorf("ls-remote origin: %w", err)
	}
	return strings.TrimSpace(out) != "", nil
}
