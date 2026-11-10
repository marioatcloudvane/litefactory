package factorylib

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing/fstest"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/skillclosure"
)

const (
	// MaxFileBytes bounds every library file.
	MaxFileBytes = 1 << 20
	// MaxLibraryBytes bounds the whole library.
	MaxLibraryBytes = 16 << 20
	manifestSchema  = 1
)

// ErrNotInstalled means no factory library was installed in this factory home.
var ErrNotInstalled = errors.New("factory library is not installed: run `litefactory factory install --source <repo>/teams/source/factory`")

// Library is one validated, immutable copy of the factory library.
type Library struct {
	digest string
	files  map[string][]byte
}

type manifest struct {
	Schema int            `json:"schema"`
	Digest string         `json:"digest"`
	Files  []manifestFile `json:"files"`
}

type manifestFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// Digest identifies the exact library bytes; it names the installed directory.
func (l *Library) Digest() string { return l.digest }

// Read returns an owned copy of one registered file. It fails for any path
// outside the registry, so no caller can look up free-form names.
func (l *Library) Read(name string) ([]byte, error) {
	if !slices.Contains(Files(), name) {
		return nil, fmt.Errorf("factory library: %s is not a registered file", name)
	}
	raw, ok := l.files[name]
	if !ok {
		return nil, fmt.Errorf("factory library: %s is missing", name)
	}
	return bytes.Clone(raw), nil
}

// RoleText returns one role's own file.
func (l *Library) RoleText(role Role) ([]byte, error) {
	if !role.Valid() {
		return nil, fmt.Errorf("factory library: unknown role %q", role)
	}
	return l.Read(role.Path())
}

// FS exposes the files below one registered directory (DoctorDir or
// ConnectorDir) as a read-only filesystem rooted at that directory.
func (l *Library) FS(dir string) (fs.FS, error) {
	if dir != DoctorDir && dir != ConnectorDir {
		return nil, fmt.Errorf("factory library: %s is not a registered directory", dir)
	}
	out := fstest.MapFS{}
	for name, raw := range l.files {
		if rest, ok := strings.CutPrefix(name, dir+"/"); ok {
			out[rest] = &fstest.MapFile{Data: bytes.Clone(raw), Mode: 0o444}
		}
	}
	return out, nil
}

// fromFiles validates a complete file set and computes its digest.
func fromFiles(files map[string][]byte) (*Library, error) {
	if err := validate(files); err != nil {
		return nil, err
	}
	return &Library{digest: manifestDigest(files), files: files}, nil
}

// FromFiles validates a complete in-memory file set, keyed by library path,
// against the registry. Tests use it to build a library with edited roles.
func FromFiles(files map[string][]byte) (*Library, error) {
	owned := make(map[string][]byte, len(files))
	for name, raw := range files {
		owned[name] = bytes.Clone(raw)
	}
	return fromFiles(owned)
}

// Files returns an owned copy of every file, keyed by library path.
func (l *Library) Files() map[string][]byte {
	out := make(map[string][]byte, len(l.files))
	for name, raw := range l.files {
		out[name] = bytes.Clone(raw)
	}
	return out
}

func validate(files map[string][]byte) error {
	var faults []error
	want := Files()
	for _, name := range want {
		if _, ok := files[name]; !ok {
			faults = append(faults, fmt.Errorf("factory library: missing %s", name))
		}
	}
	names := make([]string, 0, len(files))
	total := 0
	for name, raw := range files {
		names = append(names, name)
		total += len(raw)
	}
	slices.Sort(names)
	for _, name := range names {
		if !slices.Contains(want, name) {
			faults = append(faults, fmt.Errorf("factory library: unregistered file %s", name))
			continue
		}
		if err := validText(files[name]); err != nil {
			faults = append(faults, fmt.Errorf("factory library: %s: %w", name, err))
		}
	}
	if total > MaxLibraryBytes {
		faults = append(faults, errors.New("factory library exceeds its size limit"))
	}
	if err := errors.Join(faults...); err != nil {
		return err
	}
	read := func(name string) ([]byte, error) {
		raw, ok := files[name]
		if !ok {
			return nil, fmt.Errorf("factory library: skill file %s is not registered", name)
		}
		return raw, nil
	}
	for _, role := range Roles() {
		if _, _, err := skillclosure.Load(roles[role].skills, read); err != nil {
			faults = append(faults, fmt.Errorf("factory library: role %s skills: %w", role, err))
		}
	}
	if _, err := graphexec.ParseProfile(files[DoctorDir+"/"+DoctorGraph]); err != nil {
		faults = append(faults, fmt.Errorf("factory library: %s/%s: %w", DoctorDir, DoctorGraph, err))
	}
	doctorSkill := DoctorDir + "/skills/doctor-result/SKILL.md"
	if _, _, err := skillclosure.Parse(files[doctorSkill], "doctor-result"); err != nil {
		faults = append(faults, fmt.Errorf("factory library: %s: %w", doctorSkill, err))
	}
	return errors.Join(faults...)
}

func validText(raw []byte) error {
	if len(raw) > MaxFileBytes {
		return fmt.Errorf("exceeds %d bytes", MaxFileBytes)
	}
	if !utf8.Valid(raw) {
		return errors.New("is not UTF-8")
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return errors.New("is blank")
	}
	return nil
}

func hashBytes(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

func manifestOf(files map[string][]byte) manifest {
	m := manifest{Schema: manifestSchema}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		m.Files = append(m.Files, manifestFile{Path: name, SHA256: hashBytes(files[name])})
	}
	m.Digest = digestOf(m.Files)
	return m
}

func manifestDigest(files map[string][]byte) string { return manifestOf(files).Digest }

func digestOf(entries []manifestFile) string {
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e.Path + "\x00" + e.SHA256 + "\n")
	}
	return hashBytes([]byte(b.String()))
}

// readTree reads every regular file below root, refusing links and oversize files.
func readTree(ctx context.Context, root *os.Root, skip string) (map[string][]byte, error) {
	files := map[string][]byte{}
	total := 0
	err := fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() || name == skip {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("factory library: %s is not a regular file", name)
		}
		f, err := root.Open(name)
		if err != nil {
			return err
		}
		raw, readErr := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
		closeErr := f.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return fmt.Errorf("factory library: read %s: %w", name, err)
		}
		if len(raw) > MaxFileBytes {
			return fmt.Errorf("factory library: %s exceeds %d bytes", name, MaxFileBytes)
		}
		total += len(raw)
		if total > MaxLibraryBytes {
			return errors.New("factory library exceeds its size limit")
		}
		files[path.Clean(name)] = raw
		return nil
	})
	return files, err
}

// LoadDir reads and verifies one installed library directory against its
// manifest and the registry.
func LoadDir(ctx context.Context, dir string) (*Library, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w (missing %s)", ErrNotInstalled, dir)
		}
		return nil, err
	}
	defer root.Close()
	raw, err := root.ReadFile(ManifestPath)
	if err != nil {
		return nil, fmt.Errorf("factory library %s: read manifest: %w", dir, err)
	}
	var m manifest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil || m.Schema != manifestSchema {
		return nil, fmt.Errorf("factory library %s: invalid manifest", dir)
	}
	files, err := readTree(ctx, root, ManifestPath)
	if err != nil {
		return nil, err
	}
	got := manifestOf(files)
	if !slices.Equal(got.Files, m.Files) || got.Digest != m.Digest || m.Digest != filepath.Base(dir) {
		return nil, fmt.Errorf("factory library %s: files differ from the manifest; reinstall it", dir)
	}
	return fromFiles(files)
}
