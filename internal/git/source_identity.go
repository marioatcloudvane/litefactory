package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SourceEntry describes deliverable bytes independently of commit metadata.
type SourceEntry struct {
	Path   string `json:"path"`
	Mode   string `json:"mode"`
	SHA256 string `json:"sha256"`
	Target string `json:"target,omitempty"`
}

// SourceIdentity is shared by dirty validation and saved delivery. HEAD is an
// observation, never part of the content digest. Ignored inputs are included
// only when the factory explicitly identifies them; external inputs refuse.
type SourceIdentity struct {
	Head          string        `json:"head"`
	Digest        string        `json:"digest"`
	Entries       []SourceEntry `json:"entries"`
	IgnoredInputs []string      `json:"ignored_inputs"`
	NonInputs     []string      `json:"non_inputs"`
}

// SourceOptions binds explicitly excluded artifacts and relevant ignored inputs.
type SourceOptions struct {
	IgnoredInputs  []string
	NonInputs      []string
	ExternalInputs []string
}

// ObserveSource reads tracked and new nonignored files and contained symlinks without staging,
// writing objects, or changing the index. Deleted paths disappear from the full
// manifest; executable modes and new files therefore change the same digest as
// their later saved representation. Unsupported symlinks/submodules refuse.
func (w *Worktree) ObserveSource(ctx context.Context, options SourceOptions) (SourceIdentity, error) {
	first, err := w.observeSource(ctx, options)
	if err != nil {
		return SourceIdentity{}, err
	}
	second, err := w.observeSource(ctx, options)
	if err != nil {
		return SourceIdentity{}, err
	}
	if first.Head != second.Head || first.Digest != second.Digest {
		return SourceIdentity{}, errors.New("source changed across complete observations")
	}
	return second, nil
}

func (w *Worktree) observeSource(ctx context.Context, options SourceOptions) (SourceIdentity, error) {
	if err := w.check(); err != nil {
		return SourceIdentity{}, err
	}
	if len(options.ExternalInputs) != 0 {
		return SourceIdentity{}, errors.New("external validation inputs cannot be certified")
	}
	ignored, err := sourcePaths(options.IgnoredInputs)
	if err != nil {
		return SourceIdentity{}, err
	}
	nonInputs, err := sourcePaths(options.NonInputs)
	if err != nil {
		return SourceIdentity{}, err
	}
	for _, path := range ignored {
		if sourceExcluded(path, nonInputs) {
			return SourceIdentity{}, errors.New("validation input also excluded")
		}
	}
	before, err := w.runner.run(ctx, readTimeout, "rev-parse", "HEAD")
	if err != nil {
		return SourceIdentity{}, err
	}
	pathsRaw, err := w.runner.runRawLimit(ctx, readTimeout, 4<<20, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return SourceIdentity{}, err
	}
	paths := append(strings.Split(strings.TrimSuffix(pathsRaw, "\x00"), "\x00"), ignored...)
	paths, err = sourcePaths(paths)
	if err != nil {
		return SourceIdentity{}, err
	}
	if len(paths) > 100000 {
		return SourceIdentity{}, errors.New("source exceeds file bound")
	}
	root, rootInfo, err := openSourceRoot(w.path)
	if err != nil {
		return SourceIdentity{}, err
	}
	defer root.Close()
	out := SourceIdentity{Head: before, IgnoredInputs: ignored, NonInputs: nonInputs}
	total := int64(0)
	for _, path := range paths {
		if err = ctx.Err(); err != nil {
			return SourceIdentity{}, err
		}
		if sourceExcluded(path, nonInputs) || sourceTransport(path) {
			continue
		}
		entry, size, e := observeSourceFile(root, path)
		if errors.Is(e, fs.ErrNotExist) {
			if sort.SearchStrings(ignored, path) < len(ignored) && ignored[sort.SearchStrings(ignored, path)] == path {
				return SourceIdentity{}, fmt.Errorf("missing declared input %s: %w", path, e)
			}
			continue
		}
		if e != nil {
			return SourceIdentity{}, fmt.Errorf("source %s: %w", path, e)
		}
		total += size
		if total > 512<<20 {
			return SourceIdentity{}, errors.New("source exceeds byte bound")
		}
		out.Entries = append(out.Entries, entry)
	}
	encoded, err := json.Marshal(struct {
		Entries                  []SourceEntry
		IgnoredInputs, NonInputs []string
	}{out.Entries, ignored, nonInputs})
	if err != nil {
		return SourceIdentity{}, err
	}
	sum := sha256.Sum256(encoded)
	out.Digest = hex.EncodeToString(sum[:])
	after, err := w.runner.run(ctx, readTimeout, "rev-parse", "HEAD")
	if err != nil {
		return SourceIdentity{}, err
	}
	if before != after {
		return SourceIdentity{}, errors.New("source HEAD changed during observation")
	}
	if err = sourceRootUnchanged(w.path, rootInfo); err != nil {
		return SourceIdentity{}, err
	}
	return out, nil
}

func sourcePaths(paths []string) ([]string, error) {
	seen := make(map[string]bool)
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		if !fs.ValidPath(path) || path == "." || strings.ContainsAny(path, "\\\x00\r\n") {
			return nil, errors.New("invalid source input path")
		}
		if !seen[path] {
			seen[path] = true
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out, nil
}
func sourceExcluded(path string, exclusions []string) bool {
	for _, prefix := range exclusions {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}
func sourceTransport(path string) bool {
	return path == ".litefactory-plan-proposal.json" || path == ".litefactory-plan-outcome.json" || path == ".litefactory-story-events" || strings.HasPrefix(path, ".litefactory-story-events/")
}
func observeSourceFile(root *os.Root, path string) (SourceEntry, int64, error) {
	parts := strings.Split(path, "/")
	for i := range parts {
		info, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		if err != nil {
			return SourceEntry{}, 0, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if i != len(parts)-1 {
				return SourceEntry{}, 0, &SourceSymlinkError{Path: path, Reason: "has a symlink source parent"}
			}
			entry, size, err := observeSourceSymlink(root, path, info)
			if errors.Is(err, fs.ErrNotExist) {
				return SourceEntry{}, 0, errors.New("source symlink disappeared during observation")
			}
			return entry, size, err
		}
	}
	info, err := root.Lstat(path)
	if err != nil {
		return SourceEntry{}, 0, err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<20 {
		return SourceEntry{}, 0, errors.New("unsupported or oversized source input")
	}
	f, err := root.Open(path)
	if err != nil {
		return SourceEntry{}, 0, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return SourceEntry{}, 0, err
	}
	if !os.SameFile(info, opened) {
		return SourceEntry{}, 0, errors.New("source replaced during observation")
	}
	h := sha256.New()
	size, err := io.Copy(h, io.LimitReader(f, (64<<20)+1))
	if err != nil {
		return SourceEntry{}, 0, err
	}
	after, err := f.Stat()
	if err != nil {
		return SourceEntry{}, 0, err
	}
	final, err := root.Lstat(path)
	if err != nil {
		return SourceEntry{}, 0, err
	}
	if size != info.Size() || size > 64<<20 || !os.SameFile(info, final) || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) || after.Mode() != info.Mode() {
		return SourceEntry{}, 0, errors.New("source changed during observation")
	}
	mode := "100644"
	if info.Mode()&0111 != 0 {
		mode = "100755"
	}
	return SourceEntry{Path: path, Mode: mode, SHA256: hex.EncodeToString(h.Sum(nil))}, size, nil
}

// SourceSymlinkError identifies a repository finding, rather than an I/O failure.
// It deliberately does not unwrap missing targets to fs.ErrNotExist: a dangling
// link is still source content, not a deleted entry.
type SourceSymlinkError struct {
	Path   string
	Reason string
}

func (e *SourceSymlinkError) Error() string { return e.Path + " " + e.Reason }

func observeSourceSymlink(root *os.Root, path string, info fs.FileInfo) (SourceEntry, int64, error) {
	target, err := root.Readlink(path)
	if err != nil {
		return SourceEntry{}, 0, err
	}
	if len(target) > 64<<20 {
		return SourceEntry{}, 0, errors.New("oversized source symlink")
	}
	if err := resolveSourceSymlink(root, path); err != nil {
		return SourceEntry{}, 0, err
	}
	final, err := root.Lstat(path)
	if err != nil {
		return SourceEntry{}, 0, err
	}
	after, err := root.Readlink(path)
	if err != nil {
		return SourceEntry{}, 0, err
	}
	if !os.SameFile(info, final) || info.Mode() != final.Mode() || !info.ModTime().Equal(final.ModTime()) || target != after {
		return SourceEntry{}, 0, errors.New("source symlink changed during observation")
	}
	sum := sha256.Sum256([]byte(target))
	return SourceEntry{Path: path, Mode: "120000", SHA256: hex.EncodeToString(sum[:]), Target: target}, int64(len(target)), nil
}

// Resolve components before cleaning '..': a preceding link can change its
// meaning. Every filesystem operation remains confined by the open os.Root,
// including if a directory is replaced while resolution is in progress.
func resolveSourceSymlink(root *os.Root, path string) error {
	pending := strings.Split(path, "/")
	var resolved []string
	links := 0
	refuse := func(reason string) error { return &SourceSymlinkError{Path: path, Reason: reason} }
	for len(pending) > 0 {
		part := pending[0]
		pending = pending[1:]
		switch part {
		case "", ".":
			continue
		case "..":
			if len(resolved) == 0 {
				return refuse("is a symlink outside the repository")
			}
			resolved = resolved[:len(resolved)-1]
			continue
		}
		name := strings.Join(append(append([]string{}, resolved...), part), "/")
		info, err := root.Lstat(name)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return refuse("is a symlink with an unresolved target")
			}
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			links++
			if links > 40 {
				return refuse("is a cyclic symlink or exceeds the link traversal limit")
			}
			target, err := root.Readlink(name)
			if err != nil {
				return err
			}
			if filepath.IsAbs(target) {
				return refuse("is a symlink outside the repository")
			}
			pending = append(strings.Split(target, "/"), pending...)
			continue
		}
		if len(pending) > 0 && !info.IsDir() {
			return refuse("is a symlink with a nondirectory target component")
		}
		resolved = append(resolved, part)
	}
	// Recheck the original chain through the kernel's confined resolver; the
	// component walk supplies precise findings but never replaces containment.
	info, err := root.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return refuse("is a symlink with an unresolved target")
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return refuse("is a symlink with an unsupported target type")
	}
	return nil
}

func openSourceRoot(path string) (*os.Root, fs.FileInfo, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, nil, errors.New("evidence/source root must be an absolute canonical path")
	}
	current := string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(path, current), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, e := os.Lstat(current)
		if e != nil {
			return nil, nil, e
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, nil, errors.New("symlink or nondirectory source root component refused")
		}
	}
	before, e := os.Lstat(path)
	if e != nil {
		return nil, nil, e
	}
	root, e := os.OpenRoot(path)
	if e != nil {
		return nil, nil, e
	}
	opened, e := root.Stat(".")
	if e != nil {
		closeErr := root.Close()
		return nil, nil, errors.Join(e, closeErr)
	}
	if !os.SameFile(before, opened) {
		closeErr := root.Close()
		return nil, nil, errors.Join(errors.New("source root replaced before observation"), closeErr)
	}
	return root, before, nil
}
func sourceRootUnchanged(path string, before fs.FileInfo) error {
	after, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if after.Mode()&os.ModeSymlink != 0 || !os.SameFile(before, after) {
		return errors.New("source root replaced during observation")
	}
	return nil
}
