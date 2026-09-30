package doctor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

func digestBytes(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func fileIdentity(info os.FileInfo) string {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino)
}
func repairableSessionPath(path string) bool {
	if path == "" || filepath.IsAbs(path) || filepath.Clean(path) != path || strings.Contains(path, "\\") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		lower := strings.ToLower(part)
		if strings.HasPrefix(part, ".") {
			return false
		}
		for _, denied := range []string{"secret", "credential", "token", "agent", "skill", "config", "workflow", "instruction", "prompt", "plan", "approval", "manifest", "result", "story"} {
			if strings.Contains(lower, denied) {
				return false
			}
		}
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".txt", ".json", ".log":
		return true
	default:
		return false
	}
}

// repairFile opens only a captured regular session artifact, never factory source.
func (s *Service) repairFile(ctx context.Context, ref naming.Ref, step store.DoctorRepairStep) (root, parent, file *os.File, err error) {
	relative, rootPath, err := s.repairFileRoot(ctx, ref, step)
	if err != nil {
		return nil, nil, nil, err
	}
	root, err = openDoctorFile(rootPath, true)
	if err != nil {
		return nil, nil, nil, err
	}
	cleanup := func(cause error) (*os.File, *os.File, *os.File, error) {
		if file != nil {
			cause = errors.Join(cause, file.Close())
		}
		if parent != nil {
			cause = errors.Join(cause, parent.Close())
		}
		return nil, nil, nil, errors.Join(cause, root.Close())
	}
	rootInfo, err := root.Stat()
	if err != nil {
		return cleanup(err)
	}
	parent, err = openRelativeDoctorDir(root, filepath.Dir(relative))
	if err != nil {
		return cleanup(err)
	}
	fd, err := unix.Openat(int(parent.Fd()), filepath.Base(relative), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	parentInfo, statErr := parent.Stat()
	if statErr != nil {
		return cleanup(statErr)
	}
	if errors.Is(err, unix.ENOENT) && step.Before == absentFile && step.SourceIdentity == fileIdentity(rootInfo)+":"+fileIdentity(parentInfo)+":absent" {
		return root, parent, nil, nil
	}
	if err != nil {
		return cleanup(err)
	}
	file = os.NewFile(uintptr(fd), relative)
	info, err := file.Stat()
	if err != nil {
		return cleanup(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Nlink != 1 || info.Size() > 32768 || (step.SourceIdentity != fileIdentity(rootInfo)+":"+fileIdentity(info) && step.SourceIdentity != fileIdentity(rootInfo)+":"+fileIdentity(parentInfo)+":"+fileIdentity(info)) {
		return cleanup(errors.New("doctor file identity drift or unsafe alias"))
	}
	return root, parent, file, nil
}

// backupRepairFile creates a durable restorable copy before the first approved write.
func (s *Service) backupRepairFile(ctx context.Context, ref naming.Ref, id string, step store.DoctorRepairStep) (err error) {
	root, parent, file, err := s.repairFile(ctx, ref, step)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, closeDoctorHandles(file, parent, root)) }()
	var data []byte
	var mode os.FileMode
	existed := file != nil
	if existed {
		info, statErr := file.Stat()
		if statErr != nil {
			return statErr
		}
		mode = info.Mode().Perm()
		data, err = io.ReadAll(io.LimitReader(file, 32769))
		if err != nil {
			return err
		}
		if len(data) > 32768 || digestBytes(data) != step.Before {
			return errors.New("doctor file precondition changed")
		}
	} else if step.Before != absentFile {
		return errors.New("doctor original unexpectedly absent")
	}
	dir := filepath.Join(s.Paths.DoctorBackupsDir(), digestBytes([]byte(id)))
	if err = ensureDoctorPrivateDir(s.Paths.Home(), dir); err != nil {
		return err
	}
	backupDir, err := openDoctorFile(dir, true)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, backupDir.Close()) }()
	name := fmt.Sprintf("step-%d.data", step.Number)
	if err = writePrivateCopy(backupDir, name, data); err != nil {
		return err
	}
	manifest, err := json.Marshal(struct {
		Target, Identity, Digest string
		AfterDigest              string
		Restoration              string
		Mode                     uint32
		Existed                  bool
	}{step.Target, step.SourceIdentity, step.Before, digestBytes([]byte(step.After)), "Restore original bytes and mode when Existed=true; otherwise remove only the approved created file after verifying AfterDigest and root/parent identity", uint32(mode), existed})
	if err != nil {
		return err
	}
	if err = writePrivateCopy(backupDir, fmt.Sprintf("step-%d.manifest.json", step.Number), manifest); err != nil {
		return err
	}
	if err = backupDir.Sync(); err != nil {
		return err
	}
	return s.DB.RecordDoctorFileBackup(ctx, id, step.Number, filepath.Join(dir, name), step.Before)
}
func writePrivateCopy(dir *os.File, name string, data []byte) (err error) {
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if errors.Is(err, unix.EEXIST) {
		fd, err = unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
		if err != nil {
			return err
		}
		f := os.NewFile(uintptr(fd), name)
		defer func() { err = errors.Join(err, f.Close()) }()
		info, e := f.Stat()
		if e != nil {
			return e
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Nlink != 1 || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() != int64(len(data)) {
			return errors.New("doctor backup collision")
		}
		actual, e := io.ReadAll(io.LimitReader(f, int64(len(data))+1))
		if e != nil {
			return e
		}
		if digestBytes(actual) != digestBytes(data) {
			return errors.New("doctor backup contents differ")
		}
		return nil
	}
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), name)
	defer func() { err = errors.Join(err, f.Close()) }()
	if _, err = f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}

// writeRepairFile journals intent, atomically replaces exact approved bytes and rereads them.
// Caller holds the process-shared effect lock throughout; no blind replay is allowed.
func (s *Service) writeRepairFile(ctx context.Context, ref naming.Ref, id string, step store.DoctorRepairStep) (err error) {
	root, parent, file, err := s.repairFile(ctx, ref, step)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, closeDoctorHandles(file, parent, root)) }()
	mode := os.FileMode(0600)
	if file != nil {
		info, statErr := file.Stat()
		if statErr != nil {
			return statErr
		}
		mode = info.Mode().Perm()
		data, readErr := io.ReadAll(io.LimitReader(file, 32769))
		if readErr != nil {
			return readErr
		}
		if digestBytes(data) != step.Before {
			return errors.New("doctor file precondition changed")
		}
	} else if step.Before != absentFile {
		return errors.New("doctor original unexpectedly absent")
	}
	if err = verifyDoctorBackup(step); err != nil {
		return err
	}

	if file != nil {
		if err = verifyDoctorManifest(step, uint32(mode), true); err != nil {
			return err
		}
	}
	if err = s.DB.BeginDoctorFileWrite(ctx, id, step.Number); err != nil {
		return err
	}
	temporary := ".doctor-replacement-" + randomID()
	fd, err := unix.Openat(int(parent.Fd()), temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, uint32(mode))
	if err != nil {
		return err
	}
	replacement := os.NewFile(uintptr(fd), temporary)
	// Do not remove uncertain artifacts; the operation journal retains the incomplete write.
	if _, err = replacement.Write([]byte(step.After)); err != nil {
		return errors.Join(err, replacement.Close())
	}
	if err = replacement.Chmod(mode); err != nil {
		return errors.Join(err, replacement.Close())
	}
	if err = errors.Join(replacement.Sync(), replacement.Close()); err != nil {
		return err
	}
	name := filepath.Base(strings.TrimPrefix(step.Target, step.Kind+":"))

	// Re-resolve the captured root, parent and original immediately before the
	// atomic effect; drift during backup/temp preparation must not be overwritten.
	checkRoot, checkParent, checkFile, checkErr := s.repairFile(ctx, ref, step)
	if checkErr != nil {
		return checkErr
	}
	if checkFile != nil {
		current, readErr := io.ReadAll(io.LimitReader(checkFile, 32769))
		if readErr != nil || digestBytes(current) != step.Before {
			return errors.Join(errors.New("doctor original drift before replacement"), readErr, closeDoctorHandles(checkFile, checkParent, checkRoot))
		}
	}
	if err = closeDoctorHandles(checkFile, checkParent, checkRoot); err != nil {
		return err
	}
	if step.Before == absentFile {
		// Atomic exclusive creation: a late competing name is never overwritten.
		if err = unix.Linkat(int(parent.Fd()), temporary, int(parent.Fd()), name, 0); err != nil {
			return err
		}
		if err = unix.Unlinkat(int(parent.Fd()), temporary, 0); err != nil {
			return err
		}
	} else if err = unix.Renameat(int(parent.Fd()), temporary, int(parent.Fd()), name); err != nil {
		return err
	}

	if err = parent.Sync(); err != nil {
		return err
	}
	fd, err = unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	observed := os.NewFile(uintptr(fd), name)
	actual, readErr := io.ReadAll(io.LimitReader(observed, 32769))
	if err = errors.Join(readErr, observed.Close()); err != nil {
		return err
	}
	return s.DB.FinishDoctorFileWrite(ctx, id, step.Number, digestBytes(actual))
}

// verifyReplacedFile permits the new inode but still requires the original root and exact approved bytes.
func (s *Service) verifyReplacedFile(ctx context.Context, ref naming.Ref, step store.DoctorRepairStep) (err error) {
	relative, rootPath, err := s.repairFileRoot(ctx, ref, step)
	if err != nil {
		return err
	}
	root, err := openDoctorFile(rootPath, true)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	info, err := root.Stat()
	if err != nil {
		return err
	}
	parts := strings.Split(step.SourceIdentity, ":")
	if (len(parts) != 4 && len(parts) != 5 && len(parts) != 6) || fileIdentity(info) != strings.Join(parts[:2], ":") {
		return errors.New("doctor root identity changed")
	}
	parent, err := openRelativeDoctorDir(root, filepath.ToSlash(filepath.Dir(relative)))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, parent.Close()) }()
	if len(parts) >= 5 {
		parentInfo, statErr := parent.Stat()
		if statErr != nil {
			return statErr
		}
		if fileIdentity(parentInfo) != strings.Join(parts[2:4], ":") {
			return errors.New("doctor parent identity changed")
		}
	}

	fd, err := unix.Openat(int(parent.Fd()), filepath.Base(relative), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), relative)
	defer func() { err = errors.Join(err, file.Close()) }()
	info, err = file.Stat()
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Nlink != 1 {
		return errors.New("doctor repaired file has unsafe type or aliases")
	}
	data, err := io.ReadAll(io.LimitReader(file, 32769))
	if err != nil {
		return err
	}
	if digestBytes(data) != digestBytes([]byte(step.After)) {
		return errors.New("doctor repaired bytes changed")
	}
	return nil
}

func openRelativeDoctorDir(root *os.File, path string) (*os.File, error) {
	fd, err := unix.Dup(int(root.Fd()))
	if err != nil {
		return nil, err
	}
	if path != "." {
		for _, part := range strings.Split(path, "/") {
			if part == "" || part == "." || part == ".." {
				return nil, errors.Join(errors.New("unsafe relative directory"), unix.Close(fd))
			}
			next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			closeErr := unix.Close(fd)
			if e != nil {
				return nil, errors.Join(e, closeErr)
			}
			if closeErr != nil {
				return nil, errors.Join(closeErr, unix.Close(next))
			}
			fd = next
		}
	}
	return os.NewFile(uintptr(fd), path), nil
}
func verifyDoctorBackup(step store.DoctorRepairStep) (err error) {
	if step.BackupReference == "" || step.BackupDigest != step.Before {
		return errors.New("doctor backup is incomplete")
	}
	file, err := openDoctorFile(step.BackupReference, false)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || stat.Nlink != 1 || info.Size() > 32768 {
		return errors.New("unsafe doctor backup")
	}
	data, err := io.ReadAll(io.LimitReader(file, 32769))
	if err != nil {
		return err
	}
	if step.Before == absentFile {
		if len(data) != 0 {
			return errors.New("absent-original backup contains bytes")
		}
		return verifyAbsentManifest(step)
	}
	if digestBytes(data) != step.Before {
		return errors.New("doctor backup drift")
	}
	return nil
}
