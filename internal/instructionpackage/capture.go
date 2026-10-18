package instructionpackage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/skillclosure"
)

// CapturedFile owns exact selected bytes and their origin and SHA-256 hash.
type CapturedFile struct {
	Path    string `json:"path"`
	Content []byte `json:"content"`
	Source  Source `json:"source"`
	Hash    string `json:"hash"`
}

// CaptureData is the serializable retention contract. Integrity covers every
// field except itself, including provenance and diagnostics. Hashes detect
// corruption; they do not authenticate data supplied by an untrusted writer.
type CaptureData struct {
	Schema          int            `json:"schema"`
	Role            string         `json:"role"`
	ShippedVersion  string         `json:"shipped_version"`
	ShippedDigest   string         `json:"shipped_digest"`
	Files           []CapturedFile `json:"files"`
	EffectiveDigest string         `json:"effective_digest"`
	Diagnostics     []Diagnostic   `json:"diagnostics"`
	Integrity       string         `json:"integrity"`
}

// RoleCapture retains instructions independently of live sources. Its private
// storage is immutable; every public byte or collection boundary returns copies.
type RoleCapture struct{ data CaptureData }

// Capture selects and validates a known role before launch. Callers must retain
// the encoded capture through their durable state before launching if replay is
// required. Cancellation fails explicitly and never selects fallback instructions.
func (p *Package) Capture(ctx context.Context, role string) (*RoleCapture, []Diagnostic, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if !slices.Contains(p.roles, role) {
		return nil, nil, unknownRole(role)
	}
	selectFiles := p.selectFiles
	if len(selectedSkills(role)) != 0 {
		selectFiles = p.selectSkillFiles
	}
	files, sources, diagnostics, err := selectFiles(ctx, role)
	if err != nil {
		return nil, nil, err
	}
	data := CaptureData{Schema: SchemaVersion, Role: role, ShippedVersion: p.version, ShippedDigest: p.digest, EffectiveDigest: contentDigest(files), Diagnostics: slices.Clone(diagnostics)}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		data.Files = append(data.Files, CapturedFile{name, bytes.Clone(files[name]), sources[name], hashBytes(files[name])})
	}
	data.Integrity = captureIntegrity(data)
	if err := validateCapture(data); err != nil {
		return nil, nil, fmt.Errorf("capture instructions: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return &RoleCapture{data}, slices.Clone(diagnostics), nil
}

// Snapshot returns a fully owned copy of the retained data for inspection.
func (c *RoleCapture) Snapshot() CaptureData {
	data := c.data
	data.Files = slices.Clone(data.Files)
	for i := range data.Files {
		data.Files[i].Content = bytes.Clone(data.Files[i].Content)
	}
	data.Diagnostics = slices.Clone(data.Diagnostics)
	return data
}

// Instructions returns an owned copy of the exact selected role text.
func (c *RoleCapture) Instructions() []byte {
	for _, file := range c.data.Files {
		if file.Path == rolePath(c.data.Role) {
			return bytes.Clone(file.Content)
		}
	}
	return nil
}

// Encode validates and encodes a capture for caller-owned durable retention.
func (c *RoleCapture) Encode() ([]byte, error) {
	if err := validateCapture(c.data); err != nil {
		return nil, fmt.Errorf("encode instruction capture: %w", err)
	}
	return json.Marshal(c.data)
}

// Restore uses only retained data. Invalid retained content fails closed; it
// never consults current shipped instructions or the owner's filesystem.
func Restore(raw []byte) (*RoleCapture, error) {
	// JSON base64 plus metadata must fit a bound before decoding allocates content.
	if len(raw) > 2*MaxCaptureBytes {
		return nil, fmt.Errorf("retained instruction capture exceeds encoding bound")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var data CaptureData
	if err := dec.Decode(&data); err != nil {
		return nil, fmt.Errorf("decode instruction capture: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, fmt.Errorf("decode trailing instruction capture: %w", err)
		}
		return nil, fmt.Errorf("instruction capture has trailing data")
	}
	if err := validateCapture(data); err != nil {
		return nil, fmt.Errorf("restore instruction capture: %w", err)
	}
	return &RoleCapture{data}, nil
}

func hashBytes(raw []byte) string { digest := sha256.Sum256(raw); return hex.EncodeToString(digest[:]) }
func captureIntegrity(data CaptureData) string {
	data.Integrity = ""
	raw, _ := json.Marshal(data)
	return hashBytes(raw)
}
func validDigest(s string) bool {
	raw, err := hex.DecodeString(s)
	return err == nil && len(raw) == sha256.Size && strings.ToLower(s) == s
}

func validateCapture(data CaptureData) error {
	if data.Schema != SchemaVersion {
		return fmt.Errorf("unsupported capture schema %d", data.Schema)
	}
	if !roleNamePattern.MatchString(data.Role) {
		return fmt.Errorf("invalid captured role")
	}
	if strings.TrimSpace(data.ShippedVersion) == "" || strings.TrimSpace(data.ShippedVersion) != data.ShippedVersion || len(data.ShippedVersion) > MaxFileBytes {
		return fmt.Errorf("invalid shipped version")
	}
	if !validDigest(data.ShippedDigest) || !validDigest(data.EffectiveDigest) || !validDigest(data.Integrity) {
		return fmt.Errorf("invalid capture digest")
	}
	expectedPaths := capturePaths(data.Role)
	hasSkills := len(selectedSkills(data.Role)) != 0
	if len(data.Files) < 2 || len(data.Files) > MaxFiles {
		return fmt.Errorf("invalid capture file count")
	}
	if !hasSkills && len(data.Files) != len(expectedPaths) {
		return fmt.Errorf("capture must contain complete role resource closure")
	}
	files := make(map[string][]byte, 2)
	sources := make(map[string]Source, 2)
	total := 0
	previous := ""
	for _, file := range data.Files {
		if !skillclosure.SafePath(file.Path) {
			return fmt.Errorf("unsafe captured file")
		}
		if !hasSkills && !slices.Contains(expectedPaths, file.Path) {
			return fmt.Errorf("unexpected captured file")
		}
		if file.Path <= previous {
			return fmt.Errorf("captured files must be unique and sorted")
		}
		previous = file.Path
		if file.Source != SourceOwner && file.Source != SourceShipped {
			return fmt.Errorf("invalid captured source")
		}
		total += len(file.Content)
		if len(file.Content) > MaxFileBytes || total > MaxCaptureBytes {
			return fmt.Errorf("captured content exceeds bounds")
		}
		if hashBytes(file.Content) != file.Hash {
			return fmt.Errorf("captured file hash mismatch")
		}
		files[file.Path] = file.Content
		sources[file.Path] = file.Source
	}
	roles, err := parseCatalogue(files[CataloguePath])
	if err != nil {
		return fmt.Errorf("captured catalogue: %w", err)
	}
	if !slices.Contains(roles, data.Role) {
		return fmt.Errorf("captured role absent from catalogue")
	}
	for _, name := range expectedPaths {
		if name == CataloguePath {
			continue
		}
		if err := validateText(files[name]); err != nil {
			return fmt.Errorf("captured resource %s: %w", name, err)
		}
		if sources[CataloguePath] == SourceShipped && sources[name] == SourceOwner {
			return fmt.Errorf("owner resource requires validated owner catalogue")
		}
	}
	if hasSkills {
		closure, err := roleClosure(data.Role, func(name string) ([]byte, error) {
			raw, ok := files[name]
			if !ok {
				return nil, fmt.Errorf("missing captured closure file %s", name)
			}
			return raw, nil
		})
		if err != nil {
			return fmt.Errorf("captured closure: %w", err)
		}
		if len(closure) != len(files) {
			return fmt.Errorf("unexpected captured file")
		}
		for _, source := range sources {
			if source != sources[CataloguePath] {
				return fmt.Errorf("mixed-source skill closure")
			}
		}
	}
	maxDiagnostics := len(expectedPaths)
	if hasSkills {
		maxDiagnostics = 2
	}
	if len(data.Diagnostics) > maxDiagnostics {
		return fmt.Errorf("too many capture diagnostics")
	}
	seen := map[string]bool{}
	for _, diagnostic := range data.Diagnostics {
		if diagnostic.Role != data.Role || diagnostic.SelectedSource != SourceShipped || sources[diagnostic.File] != SourceShipped || seen[diagnostic.File] {
			return fmt.Errorf("invalid fallback diagnostic")
		}
		switch diagnostic.Category {
		case "missing", "unreadable", "unsafe", "bounds", "changed", "invalid", "catalogue-mismatch":
		default:
			return fmt.Errorf("invalid fallback category")
		}
		seen[diagnostic.File] = true
	}
	if contentDigest(files) != data.EffectiveDigest {
		return fmt.Errorf("effective capture digest mismatch")
	}
	if captureIntegrity(data) != data.Integrity {
		return fmt.Errorf("capture integrity mismatch")
	}
	return nil
}

// Role returns the captured catalogue role.
func (c *RoleCapture) Role() string { return c.data.Role }

// Resource returns an owned copy of a selected resource, or nil if absent.
func (c *RoleCapture) Resource(path string) []byte {
	for _, file := range c.data.Files {
		if file.Path == path {
			return bytes.Clone(file.Content)
		}
	}
	return nil
}
