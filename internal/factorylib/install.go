package factorylib

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudvane/litefactory/internal/config"
)

// currentFile names the installed digest the factory reads.
const currentFile = "current"

// ReadSource validates a library source folder (teams/source/factory) in memory.
func ReadSource(ctx context.Context, dir string) (*Library, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("factory library source: %w", err)
	}
	defer root.Close()
	files, err := readTree(ctx, root, "")
	if err != nil {
		return nil, err
	}
	return fromFiles(files)
}

// Install validates the source folder and installs it as one unit into
// <home>/factory/<digest>/ with a manifest of per-file sha256, then points
// <home>/factory/current at it. Installing the same bytes again only repoints.
// Work already running keeps the bytes it captured.
func Install(ctx context.Context, paths config.Paths, source string) (lib *Library, already bool, err error) {
	lib, err = ReadSource(ctx, source)
	if err != nil {
		return nil, false, err
	}
	base := paths.FactoryLibraryDir()
	if err := os.MkdirAll(base, 0o700); err != nil {
		return nil, false, err
	}
	target := filepath.Join(base, lib.digest)
	if _, err := LoadDir(ctx, target); err == nil {
		already = true
	} else {
		if _, statErr := os.Lstat(target); statErr == nil {
			return nil, false, fmt.Errorf("installed factory library %s is damaged; remove it and install again: %w", target, err)
		}
		if err := writeLibrary(base, target, lib); err != nil {
			return nil, false, err
		}
	}
	if err := writeAtomic(filepath.Join(base, currentFile), []byte(lib.digest+"\n")); err != nil {
		return nil, false, err
	}
	return lib, already, nil
}

func writeLibrary(base, target string, lib *Library) (err error) {
	staging, err := os.MkdirTemp(base, ".install-")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.RemoveAll(staging))
		}
	}()
	for name, raw := range lib.files {
		file := filepath.Join(staging, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(file, raw, 0o400); err != nil {
			return err
		}
	}
	m, err := json.MarshalIndent(manifestOf(lib.files), "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(staging, ManifestPath), append(m, '\n'), 0o400); err != nil {
		return err
	}
	return os.Rename(staging, target)
}

func writeAtomic(name string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(name), ".current-")
	if err != nil {
		return err
	}
	_, writeErr := tmp.Write(data)
	if err := errors.Join(writeErr, tmp.Sync(), tmp.Close()); err != nil {
		return errors.Join(err, os.Remove(tmp.Name()))
	}
	return os.Rename(tmp.Name(), name)
}

// currentDigest reads the installed digest the factory should use.
func currentDigest(paths config.Paths) (string, error) {
	raw, err := os.ReadFile(filepath.Join(paths.FactoryLibraryDir(), currentFile))
	if errors.Is(err, fs.ErrNotExist) {
		return "", ErrNotInstalled
	}
	if err != nil {
		return "", err
	}
	digest := strings.TrimSpace(string(raw))
	if len(digest) != 64 || strings.Trim(digest, "0123456789abcdef") != "" {
		return "", fmt.Errorf("factory library: %s names no digest; install the library again", filepath.Join(paths.FactoryLibraryDir(), currentFile))
	}
	return digest, nil
}
