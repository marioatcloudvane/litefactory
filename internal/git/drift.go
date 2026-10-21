package git

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
)

// DriftChange describes one changed entry between two pinned trees.
type DriftChange struct{ Path, OldMode, NewMode, OldBlob, NewBlob, Status string }

// DriftChangeSet is a bounded net tree comparison. UnknownRemainder names a
// limit rather than claiming enumeration of the whole requested scope.
type DriftChangeSet struct {
	Base, Head       string
	Changes          []DriftChange
	Excluded         []string
	Bytes            int
	UnknownRemainder bool
}

// DriftChanges resolves endpoints once and reads only local immutable objects.
// It never fetches, checks out, traverses submodules or invokes external diff.
func (c *Clone) DriftChanges(ctx context.Context, base, head string, prefixes []string, maxPaths, byteLimit int) (DriftChangeSet, error) {
	var out DriftChangeSet
	if c == nil || maxPaths < 1 || maxPaths > 200 || byteLimit < 1 || byteLimit > 8<<20 {
		return out, errors.New("invalid drift comparison bounds")
	}
	for _, ref := range []string{base, head} {
		if err := checkName("drift endpoint", ref); err != nil {
			return out, err
		}
	}
	for _, prefix := range prefixes {
		if prefix == "" || strings.HasPrefix(prefix, "/") || path.Clean(prefix) != prefix || prefix == ".." || strings.HasPrefix(prefix, "../") || strings.ContainsAny(prefix, "\x00\r\n") {
			return out, errors.New("invalid literal drift path prefix")
		}
	}
	run := func(limit int, args ...string) (string, error) {
		return c.runner.runRawLimit(ctx, readTimeout, limit, append([]string{"--no-optional-locks", "--no-replace-objects", "-c", "core.fsmonitor=false", "-c", "diff.external=", "-c", "core.quotePath=false"}, args...)...)
	}
	pin := func(ref string) (string, error) {
		remaining := byteLimit - out.Bytes
		if remaining < 1 {
			return "", errors.New("source allowance cannot accommodate pinned endpoints")
		}
		bound := min(65, remaining)
		v, e := run(bound, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
		if e != nil {
			// The runner suppresses failed command output. Charge its whole
			// allowance because the actual observed size is uncertain.
			out.Bytes += bound
		} else {
			out.Bytes += len(v)
		}
		return strings.TrimSpace(v), e
	}
	var err error
	out.Base, err = pin(base)
	if err != nil {
		out.UnknownRemainder = true
		return out, err
	}
	out.Head, err = pin(head)
	if err != nil {
		out.UnknownRemainder = true
		return out, err
	}
	if !fullBugObject(out.Base) || !fullBugObject(out.Head) {
		return out, errors.New("invalid resolved drift endpoint")
	}
	args := []string{"diff", "--raw", "--no-abbrev", "--no-renames", "--no-ext-diff", "--no-textconv", "--ignore-submodules=none", "-z", out.Base, out.Head, "--"}
	// Literal pathspecs restrict enumeration before output allocation and
	// prevent wildcard expansion in owner-selected file or directory prefixes.
	for _, prefix := range prefixes {
		args = append(args, ":(literal)"+prefix)
	}
	if out.Bytes >= byteLimit {
		out.UnknownRemainder = true
		return out, errors.New("source allowance exhausted by pinned endpoints")
	}
	raw, err := c.runner.runRawRecordLimit(ctx, readTimeout, byteLimit-out.Bytes, 2*(maxPaths+1), append([]string{"--no-optional-locks", "--no-replace-objects", "-c", "core.fsmonitor=false", "-c", "diff.external=", "-c", "core.quotePath=false"}, args...)...)
	out.Bytes += len(raw)
	if err != nil {
		out.UnknownRemainder = true
		out.Bytes = byteLimit
		return out, err
	}
	parts := strings.Split(raw, "\x00")
	for i := 0; i+1 < len(parts); i += 2 {
		if parts[i] == "" {
			break
		}
		f := strings.Fields(parts[i])
		if len(f) != 5 || !strings.HasPrefix(f[0], ":") {
			return out, errors.New("malformed drift tree comparison")
		}
		name := parts[i+1]
		if len(out.Changes) >= maxPaths {
			out.UnknownRemainder = true
			break
		}
		out.Changes = append(out.Changes, DriftChange{Path: name, OldMode: strings.TrimPrefix(f[0], ":"), NewMode: f[1], OldBlob: f[2], NewBlob: f[3], Status: f[4]})
	}
	return out, nil
}

// DriftBlob reads one bounded blob by full object identity; gitlinks and LFS
// pointers are never expanded by this operation.
func (c *Clone) DriftBlob(ctx context.Context, oid string, limit int) (string, error) {
	if c == nil || !fullBugObject(oid) || limit < 1 || limit > 256<<10 {
		return "", errors.New("invalid drift blob request")
	}
	data, err := c.runner.runRawLimit(ctx, showBlobTimeout, limit, "--no-replace-objects", "cat-file", "blob", oid)
	if err != nil {
		return "", fmt.Errorf("drift blob unavailable or over limit: %w", err)
	}
	return data, nil
}
