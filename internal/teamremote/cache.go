package teamremote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
)

// Cache stores only verified, content-addressed bytes. Call after validating the
// complete catalog dependency closure. It is disposable, unlike digest history.
func Cache(ctx context.Context, root string, p *Package) error {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	if info, err := os.Lstat(root); err != nil || !info.IsDir() {
		return fmt.Errorf("cache root must be a real directory")
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer r.Close()
	if p == nil || p.Manifest == nil || Hash(p.ManifestBytes) != p.ManifestDigest {
		return fmt.Errorf("invalid verified package")
	}
	data := map[string][]byte{p.ManifestDigest: p.ManifestBytes}
	for _, f := range p.Manifest.Files {
		b, ok := p.Files[f.Path]
		if !ok || Hash(b) != f.SHA256 || int64(len(b)) != f.Size {
			return fmt.Errorf("invalid verified cache input")
		}
		data[f.SHA256] = b
	}
	for digest, b := range data {
		if err := ctx.Err(); err != nil {
			return err
		}
		if info, err := r.Lstat(digest); err == nil {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("cache entry is not regular")
			}
			existing, err := readBlob(r, digest)
			if err != nil {
				return err
			}
			if Hash(existing) != digest {
				return fmt.Errorf("cached blob SHA-256 mismatch")
			}
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		// Unique temporary names plus Root prevent traversing cache links. A
		// concurrent writer publishes identical content for this digest.
		tmp, err := os.CreateTemp(root, ".blob-")
		if err != nil {
			return err
		}
		name := tmp.Name()
		_, writeErr := tmp.Write(b)
		syncErr := tmp.Sync()
		closeErr := tmp.Close()
		if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
			_ = os.Remove(name)
			return err
		}
		if err := os.Rename(name, root+string(os.PathSeparator)+digest); err != nil {
			_ = os.Remove(name)
			return err
		}
	}
	return nil
}

func readBlob(r *os.Root, digest string) ([]byte, error) {
	f, err := r.Open(digest)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > MaxFileBytes {
		return nil, fmt.Errorf("cache blob exceeds size limit")
	}
	return b, nil
}

func cachedBlob(ctx context.Context, root, digest string, size int64) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if root == "" {
		return nil, false, nil
	}
	info, err := os.Lstat(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.IsDir() {
		return nil, false, fmt.Errorf("cache root must be a real directory")
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, false, err
	}
	defer r.Close()
	info, err = r.Lstat(digest)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("cache blob must be regular")
	}
	b, err := readBlob(r, digest)
	if err != nil {
		return nil, false, err
	}
	if Hash(b) != digest || int64(len(b)) != size {
		return nil, false, fmt.Errorf("cached blob size or SHA-256 mismatch")
	}
	return b, true, nil
}
