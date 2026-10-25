package git

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// The pinned reads below let an analyst look at a registered clone as of one
// exact commit: its history, a search over its files and one file or
// directory. They read immutable objects only, so the working tree, index,
// refs and dirty files are never touched, and each one is byte bounded.

// ErrPinnedReadTooLarge reports output beyond the caller's byte bound; the
// caller narrows the request instead of receiving a silent cut.
var ErrPinnedReadTooLarge = errors.New("git: pinned read exceeds its byte bound")

func (c *Clone) pinned(commit string, byteLimit int) error {
	if c == nil || c.runner.binary == "" || !fullBugObject(commit) || byteLimit < 1 || byteLimit > 1<<20 {
		return fmt.Errorf("pinned read: %w: requires an initialized clone, a full commit and a bound", ErrInvalidArgument)
	}
	return nil
}

func pinnedPath(path string) (string, error) {
	path = strings.Trim(path, "/")
	if path == "" {
		return "", nil
	}
	if err := checkName("path", path); err != nil {
		return "", err
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("path %q: %w", path, ErrInvalidArgument)
		}
	}
	return path, nil
}

func (c *Clone) boundedRead(ctx context.Context, byteLimit int, allowed []int, args ...string) (string, error) {
	out, err := c.runner.runRawLimit(ctx, readTimeout, byteLimit, args...)
	if err != nil {
		var command *CommandError
		if errors.As(err, &command) {
			for _, code := range allowed {
				if command.ExitCode == code {
					return "", nil
				}
			}
		}
		if strings.Contains(err.Error(), "byte budget") {
			return "", ErrPinnedReadTooLarge
		}
		return "", err
	}
	return out, nil
}

// PinnedLog lists at most limit commits reachable from commit, newest first:
// short hash, date, subject and changed paths. path narrows to commits touching
// it; query narrows to commit messages containing it (case-insensitive).
func (c *Clone) PinnedLog(ctx context.Context, commit, path, query string, limit, byteLimit int) (string, error) {
	if err := c.pinned(commit, byteLimit); err != nil {
		return "", err
	}
	path, err := pinnedPath(path)
	if err != nil {
		return "", err
	}
	if limit < 1 || limit > 100 || strings.ContainsAny(query, "\x00\n\r") || len(query) > 200 {
		return "", fmt.Errorf("pinned log: %w", ErrInvalidArgument)
	}
	args := []string{"--no-optional-locks", "--no-replace-objects", "log", "--no-color", "--no-ext-diff", "--no-mailmap", "-n", strconv.Itoa(limit), "--date=short", "--format=%h %ad %s", "--name-only"}
	if query != "" {
		args = append(args, "-i", "--fixed-strings", "--grep="+query)
	}
	args = append(args, "--end-of-options", commit, "--")
	if path != "" {
		args = append(args, path)
	}
	return c.boundedRead(ctx, byteLimit, nil, args...)
}

// PinnedGrep searches the files of commit for an extended regular expression
// and returns path:line:text matches, at most perFile per file. No match is an
// empty result.
func (c *Clone) PinnedGrep(ctx context.Context, commit, pattern, path string, ignoreCase bool, perFile, byteLimit int) (string, error) {
	if err := c.pinned(commit, byteLimit); err != nil {
		return "", err
	}
	path, err := pinnedPath(path)
	if err != nil {
		return "", err
	}
	if pattern == "" || len(pattern) > 500 || strings.ContainsAny(pattern, "\x00\n\r") || perFile < 1 || perFile > 100 {
		return "", fmt.Errorf("pinned grep: %w", ErrInvalidArgument)
	}
	args := []string{"--no-optional-locks", "--no-replace-objects", "grep", "--no-color", "-I", "-n", "-E", "--max-count=" + strconv.Itoa(perFile)}
	if ignoreCase {
		args = append(args, "-i")
	}
	args = append(args, "-e", pattern, commit, "--")
	if path != "" {
		args = append(args, path)
	}
	out, err := c.boundedRead(ctx, byteLimit, []int{1}, args...)
	// Matches are printed as <commit>:<path>:<line>:<text>; the commit is known.
	return strings.ReplaceAll(out, commit+":", ""), err
}

// PinnedRead returns one file's bytes at commit, or a directory's entry names
// (directories end in "/"). A missing path is [ErrRevisionNotFound].
func (c *Clone) PinnedRead(ctx context.Context, commit, path string, byteLimit int) (content string, directory bool, err error) {
	if err = c.pinned(commit, byteLimit); err != nil {
		return "", false, err
	}
	if path, err = pinnedPath(path); err != nil {
		return "", false, err
	}
	object := commit + ":" + path
	kind, err := c.runner.run(ctx, readTimeout, "--no-replace-objects", "cat-file", "-t", object)
	if err != nil {
		return "", false, fmt.Errorf("%s: %w", path, ErrRevisionNotFound)
	}
	switch kind {
	case "tree":
		out, err := c.boundedRead(ctx, byteLimit, nil, "--no-optional-locks", "--no-replace-objects", "ls-tree", "-z", object)
		if err != nil {
			return "", true, err
		}
		var names []string
		for _, entry := range strings.Split(out, "\x00") {
			meta, name, ok := strings.Cut(entry, "\t")
			if !ok {
				continue
			}
			if strings.Contains(meta, " tree ") {
				name += "/"
			}
			names = append(names, name)
		}
		return strings.Join(names, "\n"), true, nil
	case "blob":
		size, err := c.runner.run(ctx, readTimeout, "--no-replace-objects", "cat-file", "-s", object)
		if err != nil {
			return "", false, err
		}
		if n, e := strconv.Atoi(size); e != nil || n > byteLimit {
			return "", false, ErrPinnedReadTooLarge
		}
		out, err := c.boundedRead(ctx, byteLimit, nil, "--no-replace-objects", "cat-file", "blob", object)
		return out, false, err
	default:
		return "", false, fmt.Errorf("%s is a %s, not a file or directory: %w", path, kind, ErrInvalidArgument)
	}
}

// OpenPinnedRead opens an immutable blob or tree listing for incremental private
// preparation. The caller must close it, including on cancellation.
func OpenPinnedRead(ctx context.Context, c *Clone, commit, path string) (io.ReadCloser, bool, error) {
	if err := c.pinned(commit, 1); err != nil {
		return nil, false, err
	}
	path, err := pinnedPath(path)
	if err != nil {
		return nil, false, err
	}
	object := commit + ":" + path
	kind, err := c.runner.run(ctx, readTimeout, "--no-replace-objects", "cat-file", "-t", object)
	if err != nil {
		return nil, false, ErrRevisionNotFound
	}
	switch kind {
	case "blob":
		stream, err := c.runner.openRead(ctx, "--no-replace-objects", "cat-file", "blob", object)
		return stream, false, err
	case "tree":
		stream, err := c.runner.openRead(ctx, "--no-optional-locks", "--no-replace-objects", "ls-tree", "-z", object)
		return stream, true, err
	default:
		return nil, false, ErrInvalidArgument
	}
}
