package factorylib

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/skillclosure"
)

const captureSchema = 2

// legacyCaptureSchema is the format of the retired embedded instruction
// package. Retained captures of it still restore so in-flight work replays.
const legacyCaptureSchema = 1

// Capture holds one role's exact bytes, taken before launch and retained by the
// caller so replay never reads the live library.
type Capture struct{ data captureData }

type captureData struct {
	Schema  int            `json:"schema"`
	Role    Role           `json:"role"`
	Library string         `json:"library"`
	Digest  string         `json:"digest"`
	Files   []capturedFile `json:"files"`
}

type capturedFile struct {
	Path    string `json:"path"`
	Content []byte `json:"content"`
	Hash    string `json:"hash"`
}

// wireCapture accepts both formats; legacy-only fields map onto the new ones.
type wireCapture struct {
	captureData
	ShippedVersion  string `json:"shipped_version"`
	EffectiveDigest string `json:"effective_digest"`
}

// CaptureRole captures role from source's current library.
func CaptureRole(ctx context.Context, source Source, role Role) (*Capture, error) {
	if source == nil {
		return nil, errors.New("factory library source is missing")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lib, err := source.Library(ctx)
	if err != nil {
		return nil, err
	}
	return lib.Capture(ctx, role)
}

// Capture selects one role with its registered resources and skill closure.
func (l *Library) Capture(ctx context.Context, role Role) (*Capture, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	spec, ok := roles[role]
	if !ok {
		return nil, fmt.Errorf("factory library: unknown role %q", role)
	}
	files := map[string][]byte{}
	for _, name := range append([]string{spec.path}, spec.resources...) {
		raw, err := l.Read(name)
		if err != nil {
			return nil, err
		}
		files[name] = raw
	}
	_, skills, err := skillclosure.Load(spec.skills, func(name string) ([]byte, error) { return l.Read(name) })
	if err != nil {
		return nil, fmt.Errorf("factory library: role %s skills: %w", role, err)
	}
	for name, raw := range skills {
		files[name] = raw
	}
	data := captureData{Schema: captureSchema, Role: role, Library: l.digest, Digest: filesDigest(files)}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		data.Files = append(data.Files, capturedFile{Path: name, Content: files[name], Hash: hashBytes(files[name])})
	}
	return &Capture{data: data}, nil
}

// Restore decodes a retained capture without reading the library.
func Restore(raw []byte) (*Capture, error) {
	if len(raw) > 2*MaxLibraryBytes {
		return nil, errors.New("retained capture exceeds its encoding bound")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	var wire wireCapture
	if err := dec.Decode(&wire); err != nil {
		return nil, fmt.Errorf("decode retained capture: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("retained capture has trailing data")
	}
	data := wire.captureData
	switch data.Schema {
	case captureSchema:
	case legacyCaptureSchema:
		data = captureData{Schema: captureSchema, Role: legacyRole(string(wire.Role)), Library: wire.ShippedVersion, Digest: wire.EffectiveDigest}
		for _, file := range wire.Files {
			if name := legacyPath(file.Path); name != "" {
				data.Files = append(data.Files, capturedFile{Path: name, Content: file.Content, Hash: file.Hash})
			}
		}
		slices.SortFunc(data.Files, func(a, b capturedFile) int { return strings.Compare(a.Path, b.Path) })
	default:
		return nil, fmt.Errorf("retained capture has unsupported schema %d", data.Schema)
	}
	if err := checkCapture(data); err != nil {
		return nil, fmt.Errorf("retained capture: %w", err)
	}
	return &Capture{data: data}, nil
}

func checkCapture(data captureData) error {
	if !data.Role.Valid() {
		return fmt.Errorf("unknown role %q", data.Role)
	}
	if strings.TrimSpace(data.Library) == "" || strings.TrimSpace(data.Digest) == "" {
		return errors.New("missing library identity")
	}
	registered := Files()
	previous := ""
	hasRole := false
	for _, file := range data.Files {
		if !slices.Contains(registered, file.Path) || file.Path <= previous {
			return fmt.Errorf("unexpected or unsorted file %q", file.Path)
		}
		previous = file.Path
		if hashBytes(file.Content) != file.Hash {
			return fmt.Errorf("file %s hash mismatch", file.Path)
		}
		hasRole = hasRole || file.Path == data.Role.Path()
	}
	if !hasRole {
		return errors.New("role file is missing")
	}
	return nil
}

// Encode returns the capture for the caller's durable retention.
func (c *Capture) Encode() ([]byte, error) {
	if err := checkCapture(c.data); err != nil {
		return nil, fmt.Errorf("encode capture: %w", err)
	}
	return json.Marshal(c.data)
}

// Role is the captured role.
func (c *Capture) Role() Role { return c.data.Role }

// Library identifies the library the capture was taken from.
func (c *Capture) Library() string { return c.data.Library }

// Digest identifies the captured bytes.
func (c *Capture) Digest() string { return c.data.Digest }

// Instructions returns an owned copy of the role's own file.
func (c *Capture) Instructions() []byte { return c.Resource(c.data.Role.Path()) }

// Resource returns an owned copy of one captured file, or nil when absent.
func (c *Capture) Resource(name string) []byte {
	for _, file := range c.data.Files {
		if file.Path == name {
			return bytes.Clone(file.Content)
		}
	}
	return nil
}

// SkillInstructions returns one of the role's registered skills: its body,
// then its dependency bodies and resources in path order, all from the capture.
// A capture retained before the skill existed returns nil.
func (c *Capture) SkillInstructions(name string) []byte {
	if !slices.Contains(roles[c.data.Role].skills, name) {
		return nil
	}
	files := map[string][]byte{}
	for _, file := range c.data.Files {
		files[file.Path] = file.Content
	}
	_, closure, err := skillclosure.Load([]string{name}, func(p string) ([]byte, error) {
		raw, ok := files[p]
		if !ok {
			return nil, errors.New("missing captured skill")
		}
		return raw, nil
	})
	if err != nil {
		return nil
	}
	root := "skills/" + name + "/SKILL.md"
	_, body, err := skillclosure.Parse(closure[root], name)
	if err != nil {
		return nil
	}
	result := bytes.Clone(body)
	names := make([]string, 0, len(closure))
	for p := range closure {
		if p != root {
			names = append(names, p)
		}
	}
	slices.Sort(names)
	for _, p := range names {
		content := closure[p]
		if strings.HasSuffix(p, "/SKILL.md") {
			id := strings.TrimSuffix(strings.TrimPrefix(p, "skills/"), "/SKILL.md")
			if _, content, err = skillclosure.Parse(content, id); err != nil {
				return nil
			}
		}
		result = append(result, []byte("\n\nCaptured skill resource "+p+":\n")...)
		result = append(result, content...)
	}
	return result
}

func filesDigest(files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	var b bytes.Buffer
	var framing [8]byte
	for _, name := range names {
		binary.BigEndian.PutUint64(framing[:], uint64(len(name)))
		b.Write(framing[:])
		b.WriteString(name)
		binary.BigEndian.PutUint64(framing[:], uint64(len(files[name])))
		b.Write(framing[:])
		b.Write(files[name])
	}
	return hashBytes(b.Bytes())
}
