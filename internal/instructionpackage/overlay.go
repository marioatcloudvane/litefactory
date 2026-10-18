package instructionpackage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
)

// Source identifies the origin of a captured file.
type Source string

const (
	// SourceShipped selects the validated embedded baseline.
	SourceShipped Source = "shipped"
	// SourceOwner selects a validated owner override.
	SourceOwner Source = "owner"
)

// Diagnostic explains a fallback without disclosing instruction bodies.
type Diagnostic struct {
	Role           string `json:"role"`
	File           string `json:"file"`
	Category       string `json:"category"`
	SelectedSource Source `json:"selected_source"`
}

type ownerFailure struct {
	category string
	cause    error
}

func (e *ownerFailure) Error() string             { return e.category }
func (e *ownerFailure) Unwrap() error             { return e.cause }
func ownerError(category string, err error) error { return &ownerFailure{category, err} }
func failureCategory(err error) string {
	var failure *ownerFailure
	if errors.As(err, &failure) {
		return failure.category
	}
	if errors.Is(err, fs.ErrNotExist) {
		return "missing"
	}
	if errors.Is(err, fs.ErrPermission) {
		return "unreadable"
	}
	return "unreadable"
}

var errOwnerAbsent = errors.New("owner instruction directory absent")

type ownerReader struct {
	root *os.Root
	path string
	info fs.FileInfo
}

func openOwner(dir string) (*ownerReader, error) {
	info, err := os.Lstat(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, errors.Join(errOwnerAbsent, err)
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, ownerError("unsafe", nil)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, ownerError("changed", err)
	}
	pinned, err := root.Stat(".")
	if err != nil || !os.SameFile(info, pinned) {
		_ = root.Close()
		return nil, ownerError("changed", err)
	}
	return &ownerReader{root, dir, info}, nil
}

func (r *ownerReader) checkRoot() error {
	info, err := os.Lstat(r.path)
	if err != nil {
		return ownerError("changed", err)
	}
	if !info.IsDir() || !os.SameFile(r.info, info) {
		return ownerError("changed", nil)
	}
	return nil
}

func sameState(a, b fs.FileInfo) bool {
	return os.SameFile(a, b) && a.Size() == b.Size() && a.Mode() == b.Mode() && a.ModTime().Equal(b.ModTime())
}

func (r *ownerReader) read(ctx context.Context, name string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !fs.ValidPath(name) || strings.Contains(name, "\\") {
		return nil, ownerError("unsafe", nil)
	}
	if err := r.checkRoot(); err != nil {
		return nil, err
	}
	components := strings.Split(name, "/")
	dirs := make([]fs.FileInfo, 0, len(components)-1)
	for i := 0; i < len(components)-1; i++ {
		info, err := r.root.Lstat(strings.Join(components[:i+1], "/"))
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, ownerError("unsafe", nil)
		}
		dirs = append(dirs, info)
	}
	before, err := r.root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, ownerError("unsafe", nil)
	}
	if before.Size() > MaxFileBytes {
		return nil, ownerError("bounds", nil)
	}
	// Nonblocking, no-follow open also refuses a replacement FIFO or final symlink.
	file, err := r.root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() || !sameState(before, opened) {
		return nil, ownerError("changed", nil)
	}
	raw, err := io.ReadAll(io.LimitReader(contextReader{ctx, file}, MaxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxFileBytes {
		return nil, ownerError("bounds", nil)
	}
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	current, err := r.root.Lstat(name)
	if err != nil {
		return nil, ownerError("changed", err)
	}
	if !sameState(opened, after) || !sameState(after, current) {
		return nil, ownerError("changed", nil)
	}
	for i, info := range dirs {
		now, err := r.root.Lstat(strings.Join(components[:i+1], "/"))
		if err != nil || !now.IsDir() || !os.SameFile(info, now) {
			return nil, ownerError("changed", err)
		}
	}
	if err := r.checkRoot(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return raw, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func (p *Package) selectFiles(ctx context.Context, role string) (map[string][]byte, map[string]Source, []Diagnostic, error) {
	name := rolePath(role)
	files := map[string][]byte{CataloguePath: bytes.Clone(p.files[CataloguePath]), name: bytes.Clone(p.files[name])}
	sources := map[string]Source{CataloguePath: SourceShipped, name: SourceShipped}
	var diagnostics []Diagnostic
	fallback := func(file string, err error) {
		diagnostics = append(diagnostics, Diagnostic{role, file, failureCategory(err), SourceShipped})
	}
	if p.ownerDir == "" {
		return files, sources, diagnostics, ctx.Err()
	}
	owner, err := openOwner(filepath.Clean(p.ownerDir))
	if err != nil {
		if errors.Is(err, errOwnerAbsent) {
			return files, sources, diagnostics, ctx.Err()
		}
		fallback(CataloguePath, err)
		return files, sources, diagnostics, ctx.Err()
	}
	defer owner.root.Close()
	catalogue, err := owner.read(ctx, CataloguePath)
	if err == nil {
		var roles []string
		roles, err = parseCatalogue(catalogue)
		if err != nil {
			err = ownerError("invalid", err)
		} else if !sameRoles(roles, p.roles) {
			err = ownerError("catalogue-mismatch", nil)
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, nil, ctx.Err()
		}
		fallback(CataloguePath, err)
		return files, sources, diagnostics, nil
	}
	files[CataloguePath] = catalogue
	sources[CataloguePath] = SourceOwner
	raw, err := owner.read(ctx, name)
	if err == nil {
		if validation := validateText(raw); validation != nil {
			err = ownerError("invalid", validation)
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, nil, ctx.Err()
		}
		fallback(name, err)
	} else {
		files[name] = raw
		sources[name] = SourceOwner
	}
	// Recheck all selected owner bytes before accepting the capture. Each file is
	// individually stable; this does not promise an atomic edit across files.
	for _, file := range []string{CataloguePath, name} {
		if sources[file] != SourceOwner {
			continue
		}
		check, readErr := owner.read(ctx, file)
		if ctx.Err() != nil {
			return nil, nil, nil, ctx.Err()
		}
		if readErr == nil && bytes.Equal(check, files[file]) {
			continue
		}
		if readErr == nil {
			readErr = ownerError("changed", nil)
		}
		if file == CataloguePath {
			for _, key := range []string{CataloguePath, name} {
				files[key] = bytes.Clone(p.files[key])
				sources[key] = SourceShipped
			}
			diagnostics = nil
			fallback(file, readErr)
			break
		}
		files[file] = bytes.Clone(p.files[file])
		sources[file] = SourceShipped
		fallback(file, readErr)
	}
	return files, sources, diagnostics, ctx.Err()
}

func sameRoles(a, b []string) bool {
	a = slices.Clone(a)
	b = slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

func unknownRole(role string) error { return fmt.Errorf("unknown instruction role %q", role) }
