package doctor

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"golang.org/x/sys/unix"
)

const absentFile = "absent"

func repairableWorktreePath(path string) bool {
	if path == "" || filepath.IsAbs(path) || filepath.Clean(path) != path || strings.Contains(path, "\\") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		low := strings.ToLower(part)
		if strings.HasPrefix(part, ".") {
			return false
		}
		for _, denied := range []string{"secret", "credential", "token", "agent", "skill", "config", "workflow", "instruction", "prompt", "approval", "manifest", "migration"} {
			if strings.Contains(low, denied) {
				return false
			}
		}
		switch low {
		case "go.mod", "go.sum", "package.json", "package-lock.json", "makefile", "dockerfile", "cargo.toml", "cargo.lock", "requirements.txt", "pyproject.toml", "gemfile", "readme.instructions.md":
			return false
		}
	}
	return true
}

func factoryWorktree(ref naming.Ref, root string) bool {
	if strings.EqualFold(ref.Repo, "litefactory") {
		return true
	}
	file, err := openDoctorFile(filepath.Join(root, "go.mod"), false)
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	if err != nil {
		return true
	}
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() || info.Size() > 8192 {
		_ = file.Close()
		return true
	}
	data, readErr := io.ReadAll(io.LimitReader(file, 8193))
	closeErr := file.Close()
	return readErr != nil || closeErr != nil || len(data) > 8192 || strings.Contains(string(data), "github.com/cloudvane/litefactory") || strings.Contains(string(data), "github.com/marioatcloudvane/litefactory")
}

// collectWorktreeFiles accepts only paths identified as locally changed by the
// verified issue branch. Clean repository files and rename/copy ambiguities are
// not issue-owned repair handles. Deleted tracked paths retain explicit absence.
func (s *Service) collectWorktreeFiles(ctx context.Context, ref naming.Ref, source store.DoctorLocalSources, status string, e *store.DoctorEvidence) {
	factory := factoryWorktree(ref, source.WorktreePath)
	if factory {
		e.Gaps = append(e.Gaps, "Worktree file repair refused: factory source or uncertain repository identity")
	}
	sessionPrefix := ".aiteam/" + strings.ReplaceAll(source.Branch, "/", "-") + "/"
	parts := strings.Split(status, "\x00")
	count, total := 0, 0
	for i := 0; i < len(parts); i++ {
		entry := parts[i]
		if len(entry) < 4 || entry[2] != ' ' {
			continue
		}
		state, path := entry[:2], entry[3:]
		if strings.ContainsAny(state, "RC") {
			i++
			continue
		}
		sessionMissing := strings.HasPrefix(path, sessionPrefix) && strings.Contains(state, "D") && repairableSessionPath(strings.TrimPrefix(path, sessionPrefix))
		if strings.ContainsAny(state, "U!") || (!sessionMissing && (factory || !repairableWorktreePath(path))) {
			continue
		}
		if count >= 30 || total >= 65536 {
			e.Gaps = append(e.Gaps, "Worktree file evidence limited to 30 files and 64 KiB")
			break
		}
		if ctx.Err() != nil {
			return
		}
		captureRoot, capturePath := source.WorktreePath, path
		if sessionMissing {
			captureRoot = filepath.Join(source.WorktreePath, strings.TrimSuffix(sessionPrefix, "/"))
			capturePath = strings.TrimPrefix(path, sessionPrefix)
		}
		record, err := captureWorktreeFile(captureRoot, capturePath, strings.Contains(state, "D"))
		if sessionMissing {
			record.Kind = "session_file"
		}
		if err != nil {
			e.Gaps = append(e.Gaps, "Changed worktree file refused or changed during capture: "+path)
			continue
		}
		if total+len(record.Content) > 65536 {
			e.Gaps = append(e.Gaps, "Worktree file evidence exceeded 64 KiB; further files omitted")
			break
		}
		total += len(record.Content)
		count++
		e.Records = append(e.Records, record)
	}
}

func captureWorktreeFile(rootPath, path string, mayBeAbsent bool) (record store.DoctorEvidenceRecord, err error) {
	if !repairableWorktreePath(path) {
		return record, errors.New("unsafe worktree evidence path")
	}
	root, err := openDoctorFile(rootPath, true)
	if err != nil {
		return record, err
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	rootInfo, err := root.Stat()
	if err != nil {
		return record, err
	}
	parent, err := openRelativeDoctorDir(root, filepath.Dir(path))
	if err != nil {
		return record, err
	}
	defer func() { err = errors.Join(err, parent.Close()) }()
	parentInfo, err := parent.Stat()
	if err != nil {
		return record, err
	}
	identity := fileIdentity(rootInfo) + ":" + fileIdentity(parentInfo)
	record = store.DoctorEvidenceRecord{Kind: "worktree_file", ID: path, CapturedAt: time.Now().UTC()}
	fd, err := unix.Openat(int(parent.Fd()), filepath.Base(path), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if errors.Is(err, unix.ENOENT) && mayBeAbsent {
		record.State = absentFile
		record.Identity = identity + ":absent"
		record.Content = "Deleted path in the verified issue worktree; original is absent; approved creation uses mode 0600"
		return record, nil
	}
	if err != nil {
		return record, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer func() { err = errors.Join(err, file.Close()) }()
	info, err := file.Stat()
	if err != nil {
		return record, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Nlink != 1 || info.Size() > 32768 {
		return record, errors.New("unsafe or oversized worktree file")
	}
	data, err := io.ReadAll(io.LimitReader(file, 32769))
	if err != nil {
		return record, err
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(info, after) || info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) || len(data) > 32768 {
		return record, errors.New("worktree file changed during capture")
	}
	record.Identity = identity + ":" + fileIdentity(info)
	record.State = digestBytes(data)
	record.Content = string(data)
	return record, nil
}

func (s *Service) repairFileRoot(ctx context.Context, ref naming.Ref, step store.DoctorRepairStep) (string, string, error) {
	prefix := step.Kind + ":"
	if (step.Kind != "session_file" && step.Kind != "worktree_file") || !strings.HasPrefix(step.Target, prefix) {
		return "", "", errors.New("unsafe doctor file target kind")
	}
	relative := strings.TrimPrefix(step.Target, prefix)
	if (step.Kind == "session_file" && !repairableSessionPath(relative)) || (step.Kind == "worktree_file" && !repairableWorktreePath(relative)) {
		return "", "", errors.New("unsafe doctor file path")
	}
	source, err := s.DB.DoctorSources(ctx, ref)
	if err != nil {
		return "", "", err
	}
	expected, err := naming.WorktreePath(source.WorktreeRoot, ref.Issue)
	if err != nil || source.WorktreePath != expected || source.Branch == "" || strings.Contains(source.Branch, "..") || strings.HasPrefix(source.Branch, "/") {
		return "", "", errors.New("doctor file root identity refused")
	}
	if step.Kind == "worktree_file" {
		if factoryWorktree(ref, source.WorktreePath) {
			return "", "", errors.New("factory worktree file repair refused")
		}
		return relative, source.WorktreePath, nil
	}
	return relative, filepath.Join(source.WorktreePath, ".aiteam", strings.ReplaceAll(source.Branch, "/", "-")), nil
}

func closeDoctorHandles(files ...*os.File) error {
	var errs []error
	for _, f := range files {
		if f != nil {
			errs = append(errs, f.Close())
		}
	}
	return errors.Join(errs...)
}
