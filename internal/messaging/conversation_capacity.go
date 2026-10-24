package messaging

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// conversationCapacity stops growth before launching another native turn. It
// checks metadata only, never reads session bodies or follows private symlinks.
// The fixed 1 GiB state ceiling and 64 MiB free reserve bound this initial route;
// capacity failures retain the queued turn and publish a visible health reason.
func conversationCapacity(root string) error {
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(root, &stat); err != nil {
		return err
	}
	if freeBlockBytes(stat.Bavail, stat.Bsize) < 64<<20 {
		return errors.New("conversation free disk reserve exhausted")
	}
	var total int64
	return filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		if total > 1<<30 {
			return errors.New("conversation native state limit exceeded")
		}
		return nil
	})
}

// freeBlockBytes multiplies a free block count by the volume's block size.
// Bsize is signed on some platforms and unsigned on others; a size that is not
// positive yields 0 rather than wrapping.
func freeBlockBytes[T int32 | int64 | uint32 | uint64](blocks uint64, size T) uint64 {
	if size <= 0 {
		return 0
	}
	return blocks * uint64(size)
}
