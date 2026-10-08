package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
)

// Native output may include bootstrap text in addition to captured resources.
const maxRenderedFileBytes int64 = 4 << 20
const maxRenderedBundleBytes int64 = 32 << 20
const maxRenderedEntries = 4096

// VerifyDelivery checks the complete retained native output without changing it.
// Callers must repeat this check immediately before every provider launch.
func VerifyDelivery(ctx context.Context, d Delivery) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := d.validate(); err != nil {
		return err
	}
	if err := validateRenderedManifest(d.RenderedFiles); err != nil {
		return err
	}
	info, err := os.Lstat(d.Root)
	if err != nil {
		return fmt.Errorf("retained bundle: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("retained bundle is not a regular directory")
	}
	root, err := os.OpenRoot(d.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	seen := make(map[string]bool, len(d.RenderedFiles))
	directories := map[string]bool{".": true}
	for name := range d.RenderedFiles {
		for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
			directories[dir] = true
		}
	}
	var total int64
	entries := 0
	err = fs.WalkDir(&boundedDeliveryFS{FS: root.FS(), ctx: ctx, remaining: maxRenderedEntries - 1}, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		entries++
		if entries > maxRenderedEntries {
			return fmt.Errorf("retained bundle entry limit exceeded")
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinked retained resource: %s", path)
		}
		if entry.IsDir() {
			if !directories[path] {
				return fmt.Errorf("undeclared retained directory: %s", path)
			}
			return nil
		}
		expected, ok := d.RenderedFiles[path]
		if !ok {
			return fmt.Errorf("undeclared retained resource: %s", path)
		}
		info, err := root.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("nonregular retained resource: %s", path)
		}
		if info.Size() > maxRenderedFileBytes {
			return fmt.Errorf("retained resource read limit exceeded: %s", path)
		}
		file, err := root.Open(path)
		if err != nil {
			return err
		}
		hash := sha256.New()
		var size int64
		buf := make([]byte, 32<<10)
		for {
			if err := ctx.Err(); err != nil {
				_ = file.Close()
				return err
			}
			n, readErr := file.Read(buf)
			size += int64(n)
			total += int64(n)
			if size > maxRenderedFileBytes || total > maxRenderedBundleBytes {
				_ = file.Close()
				return fmt.Errorf("retained resource read limit exceeded: %s", path)
			}
			_, _ = hash.Write(buf[:n])
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				_ = file.Close()
				return fmt.Errorf("read retained resource %s: %w", path, readErr)
			}
		}
		if err := file.Close(); err != nil {
			return err
		}
		if hex.EncodeToString(hash.Sum(nil)) != expected {
			return fmt.Errorf("retained resource was modified: %s", path)
		}
		seen[path] = true
		return nil
	})
	if err != nil {
		return err
	}
	for _, path := range sortedRenderedPaths(d.RenderedFiles) {
		if !seen[path] {
			return fmt.Errorf("missing retained resource: %s", path)
		}
	}
	return nil
}

// WalkDir otherwise reads an entire directory before invoking its callback.
// Bound those allocations too, including undeclared retained entries.
type boundedDeliveryFS struct {
	fs.FS
	ctx       context.Context
	remaining int
}

func (f *boundedDeliveryFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if err := f.ctx.Err(); err != nil {
		return nil, err
	}
	dir, err := f.Open(name)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	reader, ok := dir.(fs.ReadDirFile)
	if !ok {
		return nil, fmt.Errorf("retained directory cannot be read: %s", name)
	}
	entries, err := reader.ReadDir(f.remaining + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(entries) > f.remaining {
		return nil, fmt.Errorf("retained bundle entry limit exceeded: %s", name)
	}
	f.remaining -= len(entries)
	if err := f.ctx.Err(); err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, nil
}

func sortedRenderedPaths(files map[string]string) []string {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func validateRenderedManifest(files map[string]string) error {
	if len(files) == 0 || len(files) > maxRenderedEntries {
		return fmt.Errorf("delivery requires a bounded rendered-file manifest")
	}
	for _, path := range sortedRenderedPaths(files) {
		if !fs.ValidPath(path) || path == "." || filepath.ToSlash(path) != path {
			return fmt.Errorf("unsafe rendered resource: %s", path)
		}
		if len(files[path]) != sha256.Size*2 {
			return fmt.Errorf("invalid rendered resource hash: %s", path)
		}
		decoded, err := hex.DecodeString(files[path])
		if err != nil || len(decoded) != sha256.Size {
			return fmt.Errorf("invalid rendered resource hash: %s", path)
		}
	}
	return nil
}
