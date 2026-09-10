package submission

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func randomID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

// openPrivateRoot pins the final directory after refusing all symlink components.
// Ancestors may be shared; the attempt directory itself must be owner-only.
func openPrivateRoot(dir string) (*os.Root, fs.FileInfo, error) {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return nil, nil, errors.New("submission: directory must be an absolute clean path")
	}
	current := string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(dir, current), string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, nil, errors.New("submission: unsafe directory component")
		}
	}
	info, err := os.Lstat(dir)
	if err != nil || !privateOwner(info) {
		return nil, nil, errors.New("submission: directory must be owner-only")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, nil, errors.New("submission: cannot open directory")
	}
	pinned, err := root.Stat(".")
	if err != nil || !os.SameFile(info, pinned) {
		_ = root.Close()
		return nil, nil, errors.New("submission: directory replaced")
	}
	return root, pinned, nil
}
func privateOwner(info fs.FileInfo) bool {
	if info == nil || info.Mode().Perm()&0077 != 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int64(stat.Uid) == int64(os.Getuid())
}
func stableDirectory(root *os.Root, dir string, pinned fs.FileInfo) bool {
	current := string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(dir, current), string(filepath.Separator)) {
		current = filepath.Join(current, part)
		entry, err := os.Lstat(current)
		if err != nil || !entry.IsDir() || entry.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}

	info, err := os.Lstat(dir)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, pinned) || !privateOwner(info) {
		return false
	}
	held, err := root.Stat(".")
	return err == nil && os.SameFile(info, held)
}
func privateRead(root *os.Root, name string, maxBytes int) ([]byte, fs.FileInfo, error) {
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || !privateOwner(info) || info.Size() > int64(maxBytes) {
		return nil, nil, errors.New("submission: unsafe private file")
	}
	// O_NONBLOCK prevents a replacement FIFO from trapping the process before its
	// descriptor type and identity can be checked; O_NOFOLLOW refuses symlinks.
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, errors.New("submission: cannot open private file")
	}
	defer file.Close()
	actual, err := file.Stat()
	if err != nil || !os.SameFile(info, actual) || !actual.Mode().IsRegular() {
		return nil, nil, errors.New("submission: private file replaced")
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(maxBytes)+1))
	if err != nil || len(data) > maxBytes {
		return nil, nil, errors.New("submission: private file exceeds limit")
	}
	after, err := root.Lstat(name)
	if err != nil || !os.SameFile(info, after) || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return nil, nil, errors.New("submission: private file changed")
	}
	return data, info, nil
}
func privateCreate(root *os.Root, name string, data []byte) error {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	return errors.Join(writeErr, syncErr, closeErr)
}

func descriptorSocketMatches(d descriptor, info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && d.SocketDevice == int64(stat.Dev) && d.SocketInode == stat.Ino
}
func socketDescriptor(info fs.FileInfo, capability string) (descriptor, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return descriptor{}, errors.New("submission: socket identity unavailable")
	}
	return descriptor{Version: 1, Socket: "s", Capability: capability, SocketDevice: int64(stat.Dev), SocketInode: stat.Ino}, nil
}
