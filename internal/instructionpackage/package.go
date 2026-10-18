package instructionpackage

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"

	"github.com/cloudvane/litefactory/internal/config"
)

//go:embed embedded/*
var embedded embed.FS

// Package is a completely validated shipped baseline and optional read-only owner overlay.
// Its methods return defensive copies; callers may share a Package between captures.
type Package struct {
	version  string
	digest   string
	roles    []string
	files    map[string][]byte
	ownerDir string
}

// New loads the embedded baseline with the owner's configured overlay location.
// It neither creates nor modifies the owner directory.
func New(ctx context.Context, paths config.Paths) (*Package, error) {
	baseline, err := fs.Sub(embedded, "embedded")
	if err != nil {
		return nil, fmt.Errorf("open embedded instructions: %w", err)
	}
	return Load(ctx, baseline, paths.InternalInstructionsDir())
}

// Load validates the entire baseline before exposing any roles. ownerDir is optional;
// it is read only during capture. Tests may supply fixture filesystems and owner paths.
func Load(ctx context.Context, baseline fs.FS, ownerDir string) (*Package, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if baseline == nil {
		return nil, fmt.Errorf("instruction baseline is nil")
	}
	files := map[string][]byte{}
	total := 0
	err := fs.WalkDir(baseline, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("baseline file %q is not regular", name)
		}
		if len(files) >= MaxFiles {
			return fmt.Errorf("baseline exceeds %d files", MaxFiles)
		}
		file, err := baseline.Open(name)
		if err != nil {
			return fmt.Errorf("open baseline %q: %w", name, err)
		}
		raw, readErr := io.ReadAll(io.LimitReader(contextReader{ctx, file}, MaxFileBytes+1))
		closeErr := file.Close()
		if readErr != nil {
			return fmt.Errorf("read baseline %q: %w", name, readErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close baseline %q: %w", name, closeErr)
		}
		if len(raw) > MaxFileBytes {
			return fmt.Errorf("baseline file %q exceeds size limit", name)
		}
		total += len(raw)
		if total > MaxCaptureBytes {
			return fmt.Errorf("baseline exceeds total content limit")
		}
		files[name] = raw
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("load instruction baseline: %w", err)
	}
	meta, metaErr := parseMetadata(files[MetadataPath])
	var failures []error
	if metaErr != nil {
		failures = append(failures, fmt.Errorf("baseline %s: %w", MetadataPath, metaErr))
	}
	roles, catalogueErr := parseCatalogue(files[CataloguePath])
	if catalogueErr != nil {
		failures = append(failures, fmt.Errorf("baseline %s: %w", CataloguePath, catalogueErr))
	}
	if catalogueErr == nil {
		expected := map[string]bool{MetadataPath: true, CataloguePath: true}
		for _, role := range roles {
			name := rolePath(role)
			expected[name] = true
			raw, ok := files[name]
			if !ok {
				failures = append(failures, fmt.Errorf("baseline missing %q", name))
				continue
			}
			if err := validateText(raw); err != nil {
				failures = append(failures, fmt.Errorf("baseline %s: %w", name, err))
			}
		}
		names := make([]string, 0, len(files))
		for name := range files {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			if !expected[name] {
				failures = append(failures, fmt.Errorf("baseline contains unlisted file %q", name))
			}
		}
	}
	if err := errors.Join(failures...); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &Package{version: meta.Version, digest: contentDigest(files), roles: roles, files: files, ownerDir: ownerDir}, nil
}

// Roles lists only validated catalogue roles, preserving catalogue order.
func (p *Package) Roles() []string { return slices.Clone(p.roles) }

// Version identifies the shipped package release.
func (p *Package) Version() string { return p.version }

// Digest identifies the exact shipped inventory bytes, separately from effective captures.
func (p *Package) Digest() string { return p.digest }
