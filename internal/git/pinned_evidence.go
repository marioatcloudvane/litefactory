package git

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// ErrPinnedSourceUnavailable refuses sources that could implicitly fetch.
var ErrPinnedSourceUnavailable = errors.New("git: partial or promisor source unavailable for pinned reads")

func pinnedSafety(ctx context.Context, c *Clone) error {
	if c == nil || c.runner.binary == "" {
		return ErrUninitialized
	}
	out, err := pinnedRunner(c).runRawLimit(ctx, readTimeout, 64<<10, "--no-optional-locks", "--no-replace-objects", "config", "--get-regexp", `^(extensions\.partialclone|remote\..*\.promisor)$`)
	if err != nil {
		var command *CommandError
		if errors.As(err, &command) && command.ExitCode == 1 {
			return nil
		}
		return err
	}
	if strings.TrimSpace(out) != "" {
		return ErrPinnedSourceUnavailable
	}
	return nil
}

// ResolvePinnedRef observes a fully qualified local ref without network access.
func ResolvePinnedRef(ctx context.Context, c *Clone, ref string) (string, error) {
	if !strings.HasPrefix(ref, "refs/heads/") && !strings.HasPrefix(ref, "refs/remotes/origin/") {
		return "", ErrInvalidArgument
	}
	if err := checkName("ref", ref); err != nil {
		return "", err
	}
	if strings.ContainsAny(ref, " ~^:?*[\\") || strings.Contains(ref, "..") || strings.Contains(ref, "@{") {
		return "", ErrInvalidArgument
	}
	if err := pinnedSafety(ctx, c); err != nil {
		return "", err
	}
	out, err := c.boundedRead(ctx, 256, nil, "--no-optional-locks", "--no-replace-objects", "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	out = strings.TrimSpace(out)
	if !fullBugObject(out) {
		return "", ErrRevisionNotFound
	}
	return out, nil
}

// PinnedCommit carries complete committed identity and first-class parents.
type PinnedCommit struct {
	ID            string
	Parents       []string
	Date, Subject string
}

// PinnedHistory lists bounded committed evidence reachable from one full head.
func PinnedHistory(ctx context.Context, c *Clone, head, path, query string, limit, byteLimit int) ([]PinnedCommit, error) {
	if err := c.pinned(head, byteLimit); err != nil {
		return nil, err
	}
	path, err := pinnedPath(path)
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 || len(query) > 200 || strings.ContainsAny(query, "\x00\n\r") {
		return nil, ErrInvalidArgument
	}
	args := []string{"--no-optional-locks", "--no-replace-objects", "log", "--no-color", "--no-show-signature", "--no-notes", "--no-decorate", "--no-ext-diff", "--no-textconv", "--no-mailmap", "-n", strconv.Itoa(limit), "--format=%H%x00%P%x00%aI%x00%s%x00"}
	if query != "" {
		args = append(args, "-i", "--fixed-strings", "--grep="+query)
	}
	args = append(args, "--end-of-options", head, "--")
	if path != "" {
		args = append(args, path)
	}
	out, err := c.boundedRead(ctx, byteLimit, nil, args...)
	if err != nil {
		return nil, err
	}
	fields := strings.Split(out, "\x00")
	result := []PinnedCommit{}
	for i := 0; i+3 < len(fields); i += 4 {
		id := strings.TrimSpace(fields[i])
		if !fullBugObject(id) {
			return nil, ErrInvalidArgument
		}
		result = append(result, PinnedCommit{ID: id, Parents: strings.Fields(fields[i+1]), Date: fields[i+2], Subject: fields[i+3]})
	}
	return result, nil
}

// PinnedChange describes direct endpoints or one reachable first-parent change.
type PinnedChange struct {
	Entries         []PinnedChangedEntry
	From, To        string
	Parents         []string
	Semantics, Text string
}

// PinnedDiff compares direct authorized endpoints when from is provided. When
// from is empty, to must be reachable from head and is compared to its first
// parent (or the empty tree for a root commit).
func PinnedDiff(ctx context.Context, c *Clone, head, from, to, path string, byteLimit int) (PinnedChange, error) {
	var result PinnedChange
	if err := c.pinned(head, byteLimit); err != nil {
		return result, err
	}
	if !fullBugObject(to) || (from != "" && !fullBugObject(from)) {
		return result, ErrInvalidArgument
	}
	path, err := pinnedPath(path)
	if err != nil {
		return result, err
	}
	args := []string{"--no-optional-locks", "--no-replace-objects"}
	result.From, result.To, result.Semantics = from, to, "direct tree endpoints"
	if from == "" {
		_, err = c.boundedRead(ctx, 256, nil, "--no-replace-objects", "merge-base", "--is-ancestor", to, head)
		if err != nil {
			return result, fmt.Errorf("commit not reachable from authorized head: %w", ErrRevisionNotFound)
		}
		history, err := PinnedHistory(ctx, c, to, "", "", 1, 64<<10)
		if err != nil {
			return result, err
		}
		if len(history) != 1 {
			return result, ErrRevisionNotFound
		}
		result.Parents = history[0].Parents
		result.Semantics = "first-parent committed change"
		args = append(args, "diff-tree", "--root", "--no-commit-id", "-p")
		if len(result.Parents) > 0 {
			result.From = result.Parents[0]
			args = append(args, result.From, to)
		} else {
			empty, emptyErr := c.boundedRead(ctx, 256, nil, "--no-replace-objects", "hash-object", "-t", "tree", "--stdin")
			if emptyErr != nil {
				return result, emptyErr
			}
			result.From = strings.TrimSpace(empty)
			result.Semantics = "root commit against empty tree"
			args = append(args, to)
		}
	} else {
		args = append(args, "diff", from, to)
	}
	entryArgs := []string{"--no-optional-locks", "--no-replace-objects"}
	if from == "" && len(result.Parents) == 0 {
		entryArgs = append(entryArgs, "diff-tree", "--root", "--no-commit-id", "-r", to)
	} else {
		entryArgs = append(entryArgs, "diff", result.From, to)
	}
	entryArgs = append(entryArgs, "--raw", "-z", "--no-abbrev", "--no-color", "--no-ext-diff", "--no-textconv", "--no-renames", "--no-relative", "-O/dev/null", "--ignore-submodules=all", "--")
	if path != "" {
		entryArgs = append(entryArgs, path)
	}
	raw, entryErr := c.boundedRead(ctx, byteLimit, nil, entryArgs...)
	if entryErr != nil {
		return result, entryErr
	}
	result.Entries, entryErr = parsePinnedChangedEntries(raw)
	if entryErr != nil {
		return result, entryErr
	}
	if len(raw) >= byteLimit {
		return result, ErrPinnedReadTooLarge
	}
	args = append(args, "--no-color", "--no-ext-diff", "--no-textconv", "--no-renames", "--full-index", "--unified=3", "--diff-algorithm=myers", "--no-indent-heuristic", "--no-relative", "-O/dev/null", "--ignore-submodules=all", "--src-prefix=a/", "--dst-prefix=b/", "--")
	if path != "" {
		args = append(args, path)
	}
	result.Text, err = c.boundedRead(ctx, byteLimit-len(raw), nil, args...)
	return result, err
}

// ErrPinnedUnsupported names committed entries whose contents are outside this
// repository's object evidence, such as a submodule gitlink.
var ErrPinnedUnsupported = errors.New("git: committed submodule entry is unsupported")

func pinnedEntry(ctx context.Context, c *Clone, commit, path string) error {
	if path == "" {
		return nil
	}
	out, err := c.boundedRead(ctx, 16<<10, nil, "--no-replace-objects", "ls-tree", "-z", commit, "--", path)
	if err != nil {
		return err
	}
	if strings.HasPrefix(out, "160000 ") {
		return ErrPinnedUnsupported
	}
	return nil
}

func pinnedRunner(c *Clone) runner { r := c.runner; r.noLazyFetch = true; return r }

// PinnedEntryInfo describes the committed entry without following symbolic links
// or entering a submodule repository. Empty path names the root tree.
type PinnedEntryInfo struct{ Mode, Kind, Object string }

// ReadPinnedEntry returns bounded literal tree metadata for one committed path.
func ReadPinnedEntry(ctx context.Context, c *Clone, commit, path string) (PinnedEntryInfo, error) {
	if err := c.pinned(commit, 16<<10); err != nil {
		return PinnedEntryInfo{}, err
	}
	path, err := pinnedPath(path)
	if err != nil {
		return PinnedEntryInfo{}, err
	}
	if path == "" {
		return PinnedEntryInfo{Mode: "040000", Kind: "tree"}, nil
	}
	out, err := c.boundedRead(ctx, 16<<10, nil, "--no-replace-objects", "ls-tree", "-z", commit, "--", path)
	if err != nil {
		return PinnedEntryInfo{}, err
	}
	record := strings.TrimSuffix(out, "\x00")
	meta, name, ok := strings.Cut(record, "\t")
	fields := strings.Fields(meta)
	if !ok || name != path || len(fields) != 3 {
		return PinnedEntryInfo{}, ErrRevisionNotFound
	}
	return PinnedEntryInfo{Mode: fields[0], Kind: fields[1], Object: fields[2]}, nil
}

// PinnedChangedEntry identifies complete committed source units for one changed
// path. An absent side has mode 000000 and the full all-zero object identity.
// Renames are represented as separate deletion and addition entries.
type PinnedChangedEntry struct {
	Path, Before, After, BeforeMode, AfterMode string
}

func parsePinnedChangedEntries(raw string) ([]PinnedChangedEntry, error) {
	entries := []PinnedChangedEntry{}
	if raw == "" {
		return entries, nil
	}
	records := strings.Split(raw, "\x00")
	if len(records)%2 != 1 || records[len(records)-1] != "" {
		return nil, ErrInvalidArgument
	}
	for i := 0; i+1 < len(records); i += 2 {
		fields := strings.Fields(records[i])
		if len(fields) != 5 || !strings.HasPrefix(fields[0], ":") || records[i+1] == "" || !fullBugObject(fields[2]) || !fullBugObject(fields[3]) {
			return nil, ErrInvalidArgument
		}
		beforeMode := strings.TrimPrefix(fields[0], ":")
		if !pinnedMode(beforeMode) || !pinnedMode(fields[1]) || !slices.Contains([]string{"A", "D", "M", "T"}, fields[4]) {
			return nil, ErrInvalidArgument
		}
		entries = append(entries, PinnedChangedEntry{Path: records[i+1], BeforeMode: beforeMode, AfterMode: fields[1], Before: fields[2], After: fields[3]})
	}
	return entries, nil
}

func pinnedMode(mode string) bool {
	if len(mode) != 6 {
		return false
	}
	for _, r := range mode {
		if r < '0' || r > '7' {
			return false
		}
	}
	return true
}

// ReadPinnedBlob reads one complete bounded immutable blob without replacement
// objects, fetching, text conversion or working-tree access. Callers derive the
// object identity from trusted committed entries, never client-selected IDs.
func ReadPinnedBlob(ctx context.Context, c *Clone, blob string, byteLimit int) (string, error) {
	if err := c.pinned(blob, byteLimit); err != nil {
		return "", err
	}
	size, err := c.boundedRead(ctx, 256, nil, "--no-replace-objects", "cat-file", "-s", blob)
	if err != nil {
		return "", err
	}
	n, err := strconv.ParseInt(strings.TrimSpace(size), 10, 64)
	if err != nil || n < 0 {
		return "", ErrInvalidArgument
	}
	if n > int64(byteLimit) {
		return "", ErrPinnedReadTooLarge
	}
	return c.boundedRead(ctx, byteLimit, nil, "--no-replace-objects", "cat-file", "blob", blob)
}

// PinnedMatch contains only the literal committed path and matching line
// positions. Source excerpts remain private acquisition data.
type PinnedMatch struct {
	Path  string
	Lines []int
}

// PinnedSearchPositions preserves Git's extended-regexp behavior while keeping
// matching source text out of its result. acquiredBytes accounts for all raw
// output, including the privately discarded excerpts, against caller budgets.
func PinnedSearchPositions(ctx context.Context, c *Clone, commit, pattern, path string, ignoreCase bool, perFile, byteLimit int) ([]PinnedMatch, int, error) {
	if err := c.pinned(commit, byteLimit); err != nil {
		return nil, 0, err
	}
	path, err := pinnedPath(path)
	if err != nil {
		return nil, 0, err
	}
	if pattern == "" || len(pattern) > 500 || strings.ContainsAny(pattern, "\x00\n\r") || perFile < 1 || perFile > 100 {
		return nil, 0, ErrInvalidArgument
	}
	args := []string{"--no-optional-locks", "--no-replace-objects", "grep", "--no-color", "--no-heading", "--no-break", "--no-column", "--no-textconv", "--no-recurse-submodules", "-I", "-n", "-z", "-E", "--max-count=" + strconv.Itoa(perFile)}
	if ignoreCase {
		args = append(args, "-i")
	}
	args = append(args, "-e", pattern, commit, "--")
	if path != "" {
		args = append(args, path)
	}
	out, err := c.boundedRead(ctx, byteLimit, []int{1}, args...)
	if err != nil {
		return nil, 0, err
	}
	matches, err := parsePinnedSearchPositions(commit, out, perFile)
	return matches, len(out), err
}

func parsePinnedSearchPositions(commit, raw string, perFile int) ([]PinnedMatch, error) {
	matches := []PinnedMatch{}
	positions := map[string]int{}
	for raw != "" {
		label, rest, ok := strings.Cut(raw, "\x00")
		if !ok {
			return nil, ErrInvalidArgument
		}
		number, body, ok := strings.Cut(rest, "\x00")
		if !ok {
			return nil, ErrInvalidArgument
		}
		lineEnd := strings.IndexByte(body, '\n')
		if lineEnd < 0 {
			return nil, ErrInvalidArgument
		}
		raw = body[lineEnd+1:]
		name, ok := strings.CutPrefix(label, commit+":")
		if !ok || name == "" {
			return nil, ErrInvalidArgument
		}
		if _, err := pinnedPath(name); err != nil {
			return nil, ErrInvalidArgument
		}
		line, err := strconv.Atoi(number)
		if err != nil || line < 1 {
			return nil, ErrInvalidArgument
		}
		index, exists := positions[name]
		if !exists {
			index = len(matches)
			positions[name] = index
			matches = append(matches, PinnedMatch{Path: name, Lines: []int{}})
		}
		prior := matches[index].Lines
		if len(prior) >= perFile || (len(prior) > 0 && line <= prior[len(prior)-1]) {
			return nil, ErrInvalidArgument
		}
		matches[index].Lines = append(matches[index].Lines, line)
	}
	return matches, nil
}
