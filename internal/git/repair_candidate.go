package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// RepairPathChange describes one complete, non-renamed tree delta. Object
// identities and modes retain binary, deletion and symlink changes exactly.
type RepairPathChange struct {
	Path      string
	OldMode   string
	NewMode   string
	OldObject string
	NewObject string
}

// RepairSnapshot is immutable evidence retained before a repair is reset.
// Patch is the full binary-capable patch, never a display-limited preview.
type RepairSnapshot struct {
	Base    string
	Tree    string
	Commit  string
	Ref     string
	Patch   string
	Changes []RepairPathChange
}

// CaptureRepairCandidate captures HEAD plus staged, unstaged and nonignored
// untracked content using a private index. It never changes the live index,
// worktree or branch. The retained ref keeps every captured object reachable.
// The caller must hold task exclusion until the verified snapshot is durable.
func (w *Worktree) CaptureRepairCandidate(ctx context.Context, base, key string) (RepairSnapshot, error) {
	var empty RepairSnapshot
	if err := w.check(); err != nil {
		return empty, err
	}
	if !fullBugObject(base) || key == "" {
		return empty, errors.New("repair capture requires exact base and candidate key")
	}
	actual, err := w.runner.run(ctx, readTimeout, "--no-replace-objects", "rev-parse", "--verify", base+"^{commit}")
	if err != nil || actual != base {
		return empty, fmt.Errorf("verify repair base: %w", errors.Join(err, errors.New("base must resolve to its exact commit")))
	}
	tree, err := w.repairWorkingTree(ctx)
	if err != nil {
		return empty, err
	}
	s := RepairSnapshot{Base: base, Tree: tree}
	if err = w.readRepairDelta(ctx, &s); err != nil {
		return empty, err
	}
	s.Commit, err = w.repairCommitTree(ctx, tree, base, Author{Name: "litefactory retained repair", Email: "retained-repair@localhost"}, "Retained repair candidate "+key)
	if err != nil {
		return empty, err
	}
	refKey := sha256.Sum256([]byte(w.branch + "\x00" + key))
	s.Ref = "refs/litefactory/validate-repairs/" + hex.EncodeToString(refKey[:])
	// Ref creation uses an expected missing value. A key cannot silently be
	// rebound to a second candidate after an interrupted capture.
	if _, err = w.runner.run(ctx, commitTimeout, "--no-replace-objects", "update-ref", s.Ref, s.Commit, strings.Repeat("0", len(s.Commit))); err != nil {
		previous, readErr := w.runner.run(ctx, readTimeout, "--no-replace-objects", "rev-parse", "--verify", s.Ref)
		if readErr != nil {
			return empty, err
		}
		s.Commit = previous
	}
	if err = w.VerifyRepairCandidate(ctx, s); err != nil {
		return empty, err
	}
	return s, nil
}

// repairWorkingTree builds and verifies a complete tree without changing any ref.
func (w *Worktree) repairWorkingTree(ctx context.Context) (string, error) {
	head, err := w.runner.run(ctx, readTimeout, "--no-replace-objects", "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", err
	}
	before, err := w.repairInputDigest(ctx)
	if err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp("", "litefactory-repair-index-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	r := w.runner
	r.indexFile = filepath.Join(tmp, "index")
	if _, err = r.run(ctx, commitTimeout, "--no-replace-objects", "read-tree", head); err != nil {
		return "", err
	}
	if _, err = r.run(ctx, commitTimeout, "--no-replace-objects", "add", "--all", "--", "."); err != nil {
		return "", fmt.Errorf("capture complete repair tree: %w", err)
	}
	// A force-added staged file remains part of the proposed tree even when
	// its path is ignored. Stage only paths known to the live index with force;
	// ignored files that were never staged remain outside the candidate.
	cached, err := w.runner.runRaw(ctx, readTimeout, "--no-replace-objects", "ls-files", "--cached", "-z")
	if err != nil {
		return "", err
	}
	if cached != "" {
		root, err := os.OpenRoot(w.path)
		if err != nil {
			return "", err
		}
		defer root.Close()
		var presentPaths strings.Builder
		for _, path := range strings.Split(cached, "\x00") {
			if path == "" {
				continue
			}
			present, err := repairSourcePathPresent(root, path)
			if err != nil {
				return "", err
			}
			if present {
				presentPaths.WriteString(path)
				presentPaths.WriteByte(0)
			}
		}
		pathspec := filepath.Join(tmp, "tracked-paths")
		if err = os.WriteFile(pathspec, []byte(presentPaths.String()), 0o600); err != nil {
			return "", err
		}
		if presentPaths.Len() > 0 {
			_, err = r.run(ctx, commitTimeout, "--no-replace-objects", "--literal-pathspecs", "add", "--all", "--force", "--pathspec-from-file="+pathspec, "--pathspec-file-nul")
		}
		if err != nil {
			return "", fmt.Errorf("capture staged ignored repair paths: %w", err)
		}
	}
	tree, err := r.run(ctx, commitTimeout, "--no-replace-objects", "write-tree")
	if err != nil {
		return "", err
	}
	after, err := w.repairInputDigest(ctx)
	if err != nil {
		return "", err
	}
	currentHead, err := w.runner.run(ctx, readTimeout, "--no-replace-objects", "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", err
	}
	if before != after || currentHead != head {
		return "", errors.New("repair changed during capture; source must not be reset")
	}
	return tree, nil
}

// repairInputDigest detects input changes across capture and rejects content
// Git cannot represent. It hashes symlink data without following the link.
func (w *Worktree) repairInputDigest(ctx context.Context) (string, error) {
	tracked, err := w.runner.runRaw(ctx, readTimeout, "--no-replace-objects", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return "", err
	}
	// Dirty submodules may hold changes outside the superproject tree.
	status, err := w.runner.runRaw(ctx, readTimeout, "--no-replace-objects", "status", "--porcelain=v2", "-z", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return "", err
	}
	entries, err := w.runner.runRaw(ctx, readTimeout, "--no-replace-objects", "ls-files", "--stage", "-z")
	if err != nil {
		return "", err
	}
	for _, record := range strings.Split(status, "\x00") {
		fields := strings.Fields(record)
		if len(fields) < 3 || (fields[0] != "1" && fields[0] != "2") {
			continue
		}
		// Porcelain v2's submodule field is S<c><m><u>. A changed commit
		// pointer can be retained; modified or untracked nested files cannot.
		sub := fields[2]
		if len(sub) == 4 && sub[0] == 'S' && (sub[2] != '.' || sub[3] != '.') {
			return "", errors.New("dirty submodule cannot be retained completely")
		}
	}
	root, err := os.OpenRoot(w.path)
	if err != nil {
		return "", err
	}
	defer root.Close()
	if err := w.repairUnsupportedInputs(ctx, root, entries, nil); err != nil {
		return "", err
	}
	h := sha256.New()
	_, _ = io.WriteString(h, entries)
	seen := map[string]bool{}
	for _, name := range strings.Split(tracked, "\x00") {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if !fs.ValidPath(name) {
			return "", errors.New("repair contains an invalid path")
		}
		// A tracked child is absent when its directory has been replaced by
		// a file or symlink. Never follow that new ancestor into another tree.
		present, e := repairSourcePathPresent(root, name)
		if e != nil {
			return "", e
		}
		if !present {
			fmt.Fprintf(h, "%q missing\n", name)
			continue
		}
		info, e := root.Lstat(name)
		if e != nil {
			return "", e
		}
		fmt.Fprintf(h, "%q %v %d\n", name, info.Mode(), info.Size())
		switch {
		case info.Mode().IsRegular():
			f, e := root.Open(name)
			if e != nil {
				return "", e
			}
			_, e = io.Copy(h, f)
			closeErr := f.Close()
			if e != nil || closeErr != nil {
				return "", errors.Join(e, closeErr)
			}
		case info.Mode()&os.ModeSymlink != 0:
			target, e := root.Readlink(name)
			if e != nil {
				return "", e
			}
			_, _ = io.WriteString(h, target)
		case info.IsDir(): // clean tracked gitlinks only
			if !strings.Contains(entries, "\t"+name+"\x00") {
				return "", fmt.Errorf("unsupported repair directory %q", name)
			}
		default:
			return "", fmt.Errorf("unsupported repair file type %q", name)
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Git silently omits untracked FIFOs, sockets and devices from status and
// ls-files. Walk without following links so such source work blocks reset
// rather than disappearing from an apparently complete retained candidate.
func (w *Worktree) repairUnsupportedInputs(ctx context.Context, root *os.Root, entries string, changes []RepairPathChange) error {
	ignored, err := w.runner.runRaw(ctx, readTimeout, "--no-replace-objects", "ls-files", "--others", "--ignored", "--exclude-standard", "--directory", "-z")
	if err != nil {
		return err
	}
	skipDirs := map[string]bool{".git": true}
	gitlinks := map[string]bool{}
	for _, path := range strings.Split(ignored, "\x00") {
		if strings.HasSuffix(path, "/") {
			skipDirs[strings.TrimSuffix(path, "/")] = true
		}
	}
	for _, entry := range strings.Split(entries, "\x00") {
		if strings.HasPrefix(entry, "160000 ") {
			_, path, ok := strings.Cut(entry, "\t")
			if ok {
				skipDirs[path] = true
				gitlinks[path] = true
			}
		}
	}
	return fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return fmt.Errorf("inspect complete repair filesystem: %w", walkErr)
		}
		if entry.IsDir() && skipDirs[path] {
			overlaps := false
			for _, change := range changes {
				if path == change.Path || strings.HasPrefix(path, change.Path+"/") || strings.HasPrefix(change.Path, path+"/") {
					overlaps = true
				}
			}
			if path == ".git" || gitlinks[path] || !overlaps {
				return fs.SkipDir
			}
		}
		kind := entry.Type()
		if path == ".git" || kind.IsDir() || kind.IsRegular() || kind&os.ModeSymlink != 0 {
			return nil
		}
		for _, change := range changes {
			if path == change.Path || strings.HasPrefix(path, change.Path+"/") || strings.HasPrefix(change.Path, path+"/") {
				return errors.New("repair adoption overlaps unsupported local content")
			}
		}
		// check-ignore checks policy even for file types ls-files omits. A
		// failed query is uncertainty, never permission to discard the source.
		_, code, err := w.runner.runAllowingExit(ctx, readTimeout, []int{0, 1}, "--no-replace-objects", "check-ignore", "--quiet", "--", path)
		if err != nil {
			return err
		}
		if code == 0 {
			return nil
		}
		return fmt.Errorf("unsupported repair file type %q", path)
	})
}

func (w *Worktree) checkRepairUnsupportedInputs(ctx context.Context, changes []RepairPathChange) error {
	entries, err := w.runner.runRaw(ctx, readTimeout, "--no-replace-objects", "ls-files", "--stage", "-z")
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(w.path)
	if err != nil {
		return err
	}
	defer root.Close()
	return w.repairUnsupportedInputs(ctx, root, entries, changes)
}

func (w *Worktree) readRepairDelta(ctx context.Context, s *RepairSnapshot) error {
	patch, err := w.runner.runRaw(ctx, commitTimeout, "--no-replace-objects", "-c", "core.quotePath=false", "diff", "--binary", "--full-index", "--no-ext-diff", "--no-textconv", "--no-renames", "--no-color", s.Base, s.Tree, "--")
	if err != nil {
		return err
	}
	raw, err := w.runner.runRaw(ctx, readTimeout, "--no-replace-objects", "diff", "--raw", "-z", "--no-abbrev", "--no-renames", s.Base, s.Tree, "--")
	if err != nil {
		return err
	}
	s.Patch = patch
	s.Changes = nil
	parts := strings.Split(strings.TrimSuffix(raw, "\x00"), "\x00")
	if raw == "" {
		return nil
	}
	if len(parts)%2 != 0 {
		return errors.New("invalid repair change manifest")
	}
	for i := 0; i < len(parts); i += 2 {
		fields := strings.Fields(strings.TrimPrefix(parts[i], ":"))
		if len(fields) != 5 || !fs.ValidPath(parts[i+1]) {
			return errors.New("invalid repair change entry")
		}
		s.Changes = append(s.Changes, RepairPathChange{Path: parts[i+1], OldMode: fields[0], NewMode: fields[1], OldObject: fields[2], NewObject: fields[3]})
	}
	return nil
}

func (w *Worktree) repairCommitTree(ctx context.Context, tree, base string, author Author, message string) (string, error) {
	if err := checkConfigValue("author name", author.Name); err != nil {
		return "", err
	}
	if err := checkConfigValue("author email", author.Email); err != nil {
		return "", err
	}
	return w.runner.run(ctx, commitTimeout, "--no-replace-objects", "-c", "user.name="+author.Name, "-c", "user.email="+author.Email, "-c", "commit.gpgsign=false", "commit-tree", tree, "-p", base, "-m", message)
}

// VerifyRepairCandidate checks retained reachability, exact parent and tree,
// complete patch and the full path/mode/object manifest against saved evidence.
func (w *Worktree) VerifyRepairCandidate(ctx context.Context, s RepairSnapshot) error {
	if err := w.check(); err != nil {
		return err
	}
	const prefix = "refs/litefactory/validate-repairs/"
	refDigest, refErr := hex.DecodeString(strings.TrimPrefix(s.Ref, prefix))
	if !fullBugObject(s.Base) || !fullBugObject(s.Tree) || !fullBugObject(s.Commit) || !strings.HasPrefix(s.Ref, prefix) || refErr != nil || len(refDigest) != sha256.Size || s.Ref != prefix+hex.EncodeToString(refDigest) {
		return errors.New("invalid retained repair identities")
	}
	if err := checkName("retained repair ref", s.Ref); err != nil {
		return err
	}
	ref, err := w.runner.run(ctx, readTimeout, "--no-replace-objects", "rev-parse", "--verify", s.Ref)
	if err != nil || ref != s.Commit {
		return errors.New("retained repair ref changed or is unavailable")
	}
	if err = w.verifyRepairCommit(ctx, s, s.Commit); err != nil {
		return err
	}
	observed := RepairSnapshot{Base: s.Base, Tree: s.Tree}
	if err = w.readRepairDelta(ctx, &observed); err != nil {
		return err
	}
	if observed.Patch != s.Patch || !reflect.DeepEqual(observed.Changes, s.Changes) {
		return errors.New("retained repair evidence does not match saved objects")
	}
	return nil
}

func (w *Worktree) verifyRepairCommit(ctx context.Context, s RepairSnapshot, commit string) error {
	if !fullBugObject(commit) {
		return errors.New("repair result must be an exact commit")
	}
	meta, err := w.runner.run(ctx, readTimeout, "--no-replace-objects", "show", "--no-patch", "--format=%T %P", commit, "--")
	if err != nil {
		return err
	}
	if meta != s.Tree+" "+s.Base {
		return errors.New("retained repair commit tree or parent changed")
	}
	return nil
}

// ResetRepairCandidate resets a verified retained candidate only while the
// checkout still represents its exact captured tree, or is already clean at
// its base. Partial reset and external edits are uncertain and left untouched.
func (w *Worktree) ResetRepairCandidate(ctx context.Context, s RepairSnapshot, keep ...string) error {
	if err := w.VerifyRepairCandidate(ctx, s); err != nil {
		return err
	}
	if err := w.checkRepairUnsupportedInputs(ctx, nil); err != nil {
		return err
	}
	branch, err := w.runner.run(ctx, readTimeout, "--no-replace-objects", "symbolic-ref", "HEAD")
	if err != nil || branch != "refs/heads/"+w.branch {
		return errors.New("repair reset requires the unchanged task branch")
	}
	head, err := w.runner.run(ctx, readTimeout, "--no-replace-objects", "rev-parse", "--verify", "HEAD")
	if err != nil {
		return err
	}
	added, err := w.repairAddedSourcePaths(ctx, s)
	if err != nil {
		return err
	}
	if head == s.Base {
		clean, err := w.repairCheckoutMatches(ctx, s.Base, keep)
		if err != nil {
			return err
		}
		if clean {
			root, err := os.OpenRoot(w.path)
			if err != nil {
				return err
			}
			defer root.Close()
			for _, path := range added {
				if keptPath(path, keep) {
					continue
				}
				present, err := repairSourcePathPresent(root, path)
				if err != nil {
					return err
				}
				if present {
					return errors.New("repair reset left candidate-added content; preserving uncertain worktree")
				}
			}
			return nil
		}
	}
	tree, err := w.repairWorkingTree(ctx)
	if err != nil {
		return fmt.Errorf("reconcile retained repair source before reset: %w", err)
	}
	if tree != s.Tree {
		return errors.New("repair source has drifted or reset is incomplete; preserving worktree")
	}
	if _, err = w.runner.run(ctx, commitTimeout, "--no-replace-objects", "reset", "--hard", "--quiet", s.Base); err != nil {
		return err
	}
	untracked, err := w.runner.runRaw(ctx, readTimeout, "--no-replace-objects", "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(w.path)
	if err != nil {
		return err
	}
	defer root.Close()
	// Base ignore rules may hide additions that were visible in the candidate.
	// Their immutable manifest remains authoritative after checkout changes.
	paths := append(strings.Split(untracked, "\x00"), added...)
	for _, path := range paths {
		if path == "" || keptPath(path, keep) {
			continue
		}
		if !fs.ValidPath(path) {
			return errors.New("invalid untracked repair path after reset")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		present, err := repairSourcePathPresent(root, path)
		if err != nil {
			return err
		}
		if !present {
			continue
		}
		if err := root.RemoveAll(path); err != nil {
			return fmt.Errorf("remove retained repair source path: %w", err)
		}
	}
	clean, err := w.repairCheckoutMatches(ctx, s.Base, keep)
	if err != nil || !clean {
		return errors.Join(err, errors.New("repair reset endpoint did not verify"))
	}
	return nil
}

// repairAddedSourcePaths excludes restored base directories as well as files.
// In a directory-to-file change the flattened diff calls the new file an
// addition, but removing that path after reset would delete restored base files.
func (w *Worktree) repairAddedSourcePaths(ctx context.Context, s RepairSnapshot) ([]string, error) {
	raw, err := w.runner.runRaw(ctx, readTimeout, "--no-replace-objects", "ls-tree", "-r", "-t", "-z", s.Base)
	if err != nil {
		return nil, err
	}
	basePaths := map[string]bool{}
	for _, entry := range strings.Split(raw, "\x00") {
		_, path, ok := strings.Cut(entry, "\t")
		if ok {
			basePaths[path] = true
		}
	}
	var added []string
	for _, change := range s.Changes {
		if change.OldMode == "000000" && !basePaths[change.Path] {
			added = append(added, change.Path)
		}
	}
	return added, nil
}

// repairSourcePathPresent does not follow restored symlink ancestors. An added
// child beneath a restored base file or symlink is already absent at this path.
func repairSourcePathPresent(root *os.Root, path string) (bool, error) {
	parts := strings.Split(path, "/")
	for i := range parts {
		info, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		if os.IsNotExist(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if i < len(parts)-1 && !info.IsDir() {
			return false, nil
		}
	}
	return true, nil
}

// PrepareRepairAdoption creates the intended exact-tree commit without moving
// any branch or changing files. A factory-owned intent ref keeps it reachable;
// persist its returned identity before applying.
func (w *Worktree) PrepareRepairAdoption(ctx context.Context, s RepairSnapshot, author Author) (string, error) {
	if err := w.VerifyRepairCandidate(ctx, s); err != nil {
		return "", err
	}
	commit, err := w.repairCommitTree(ctx, s.Tree, s.Base, author, "Adopt reviewed Validate repair\n\nRetained-candidate: "+s.Ref)
	if err != nil {
		return "", err
	}
	intentRef := strings.Replace(s.Ref, "/validate-repairs/", "/validate-adoptions/", 1)
	if _, err = w.runner.run(ctx, commitTimeout, "--no-replace-objects", "update-ref", intentRef, commit, strings.Repeat("0", len(commit))); err != nil {
		previous, readErr := w.runner.run(ctx, readTimeout, "--no-replace-objects", "rev-parse", "--verify", intentRef)
		if readErr != nil {
			return "", errors.Join(err, readErr)
		}
		if err = w.verifyRepairCommit(ctx, s, previous); err != nil {
			return "", err
		}
		commit = previous
	}
	return commit, nil
}

// ApplyRepairAdoption installs only a previously prepared exact tree. A saved
// intent can resume when the branch already has that result; a divergent ref or
// checkout blocks recovery. The caller holds exclusion and journals completion.
func (w *Worktree) ApplyRepairAdoption(ctx context.Context, s RepairSnapshot, result string, keep ...string) error {
	if err := w.VerifyRepairCandidate(ctx, s); err != nil {
		return err
	}
	if err := w.verifyRepairCommit(ctx, s, result); err != nil {
		return err
	}
	if err := w.checkRepairUnsupportedInputs(ctx, s.Changes); err != nil {
		return err
	}
	branch, err := w.runner.run(ctx, readTimeout, "--no-replace-objects", "symbolic-ref", "HEAD")
	if err != nil || branch != "refs/heads/"+w.branch {
		return errors.New("repair adoption requires the unchanged task branch")
	}
	head, err := w.runner.run(ctx, readTimeout, "--no-replace-objects", "rev-parse", "--verify", "HEAD")
	if err != nil {
		return err
	}
	if head != s.Base && head != result {
		return errors.New("repair adoption base has drifted")
	}
	if err := w.repairIgnoredOverlap(ctx, s); err != nil {
		return err
	}
	// Compare checkout and index against both states. Only the two endpoints
	// of our durable intent are recoverable; mixed or externally edited state
	// is left untouched for reconciliation.
	checkoutBase, err := w.repairCheckoutMatches(ctx, s.Base, keep)
	if err != nil {
		return err
	}
	checkoutResult, err := w.repairCheckoutMatches(ctx, result, keep)
	if err != nil {
		return err
	}
	if head == s.Base && !checkoutBase || head == result && !checkoutBase && !checkoutResult {
		return errors.New("repair adoption checkout has drifted or is uncertain")
	}
	if head == s.Base {
		if _, err = w.runner.run(ctx, commitTimeout, "--no-replace-objects", "update-ref", branch, result, s.Base); err != nil {
			return fmt.Errorf("compare-and-set repair branch: %w", err)
		}
	}
	if _, err = w.runner.run(ctx, commitTimeout, "--no-replace-objects", "reset", "--hard", "--quiet", result); err != nil {
		return fmt.Errorf("install exact repair tree: %w", err)
	}
	matched, err := w.repairCheckoutMatches(ctx, result, keep)
	if err != nil || !matched {
		return errors.Join(err, errors.New("repair result checkout did not verify"))
	}
	return nil
}

func (w *Worktree) repairIgnoredOverlap(ctx context.Context, s RepairSnapshot) error {
	raw, err := w.runner.runRaw(ctx, readTimeout, "--no-replace-objects", "ls-files", "--others", "--ignored", "--exclude-standard", "-z")
	if err != nil {
		return err
	}
	for _, path := range strings.Split(raw, "\x00") {
		if path == "" {
			continue
		}
		for _, change := range s.Changes {
			if path == change.Path || strings.HasPrefix(path, change.Path+"/") || strings.HasPrefix(change.Path, path+"/") {
				return errors.New("repair adoption would overwrite ignored local content")
			}
		}
	}
	return nil
}

func (w *Worktree) repairCheckoutMatches(ctx context.Context, commit string, keep []string) (bool, error) {
	diff, err := w.runner.runRaw(ctx, readTimeout, "--no-replace-objects", "diff", "--raw", "--no-ext-diff", "--no-textconv", commit, "--")
	if err != nil {
		return false, err
	}
	staged, err := w.runner.runRaw(ctx, readTimeout, "--no-replace-objects", "diff", "--cached", "--raw", "--no-ext-diff", "--no-textconv", commit, "--")
	if err != nil {
		return false, err
	}
	untracked, err := w.runner.runRaw(ctx, readTimeout, "--no-replace-objects", "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return false, err
	}
	for _, path := range strings.Split(untracked, "\x00") {
		if path != "" && !keptPath(path, keep) {
			return false, nil
		}
	}
	return diff == "" && staged == "", nil
}
