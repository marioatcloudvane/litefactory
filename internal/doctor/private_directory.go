package doctor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// ensureDoctorPrivateDir creates only private descendants of the recorded home,
// anchoring each component and syncing its parent before relying on persistence.
func ensureDoctorPrivateDir(home, path string) (err error) {
	relative, err := filepath.Rel(home, path)
	if err != nil || !filepath.IsLocal(relative) {
		return errors.New("doctor backup directory escapes installation")
	}
	root, err := openDoctorFile(home, true)
	if err != nil {
		return err
	}
	current := root
	defer func() { err = errors.Join(err, current.Close()) }()
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		if part == "." {
			continue
		}
		fd, openErr := unix.Openat(int(current.Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(openErr, unix.ENOENT) {
			if err = unix.Mkdirat(int(current.Fd()), part, 0700); err != nil && !errors.Is(err, unix.EEXIST) {
				return err
			}
			if err = current.Sync(); err != nil {
				return err
			}
			fd, openErr = unix.Openat(int(current.Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		}
		if openErr != nil {
			return openErr
		}
		next := os.NewFile(uintptr(fd), part)
		info, statErr := next.Stat()
		if statErr != nil {
			return errors.Join(statErr, next.Close())
		}
		if !info.IsDir() || info.Mode().Perm() != 0700 {
			return errors.Join(errors.New("doctor backup directory is not private"), next.Close())
		}
		if err = current.Close(); err != nil {
			return errors.Join(err, next.Close())
		}
		current = next
	}
	return nil
}
