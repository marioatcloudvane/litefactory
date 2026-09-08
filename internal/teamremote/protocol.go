// Package teamremote validates the independently versioned HTTPS team protocol.
package teamremote

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/cloudvane/litefactory/internal/team"
)

const (
	// SchemaVersion is the supported distribution protocol, not a content version.
	SchemaVersion = 1
	// MaxIndexBytes bounds one discovery document.
	MaxIndexBytes = 1 << 20
	// MaxManifestBytes bounds one immutable release manifest.
	MaxManifestBytes = 1 << 20
	// MaxFileBytes bounds one decoded payload file or cached blob.
	MaxFileBytes = 1 << 20
	// MaxReleaseBytes bounds the aggregate decoded payload bytes.
	MaxReleaseBytes = 16 << 20
	// MaxFiles bounds the number of files declared by one release.
	MaxFiles = 1024
)

// Discovery maps team identifiers to independently published releases.
type Discovery struct {
	SchemaVersion int                     `json:"schema_version"`
	Teams         map[string]TeamReleases `json:"teams"`
}

// TeamReleases names an explicit stable release and its exact version catalog.
type TeamReleases struct {
	Stable   string             `json:"stable"`
	Releases map[string]Release `json:"releases"`
}

// Release identifies a manifest relative to the discovery document's directory.
type Release struct {
	Manifest string `json:"manifest"`
	SHA256   string `json:"sha256"`
}

// Manifest describes one team's complete immutable, hash-verified payload.
type Manifest struct {
	SchemaVersion        int    `json:"schema_version"`
	CatalogSchemaVersion int    `json:"catalog_schema_version"`
	Team                 string `json:"team"`
	Version              string `json:"version"`
	Files                []File `json:"files"`
}

// File identifies a payload file relative to the manifest directory.
type File struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// ValidSHA256 requires a canonical lowercase digest of exact bytes.
func ValidSHA256(s string) bool {
	if len(s) != 64 || s != strings.ToLower(s) {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// DecodeDiscovery rejects malformed, duplicate, unknown and unsupported data.
func DecodeDiscovery(data []byte) (*Discovery, error) {
	var d Discovery
	if len(data) > MaxIndexBytes {
		return nil, fmt.Errorf("discovery exceeds size limit")
	}
	if err := decodeStrict(data, &d); err != nil {
		return nil, err
	}
	if err := discoveryShape(data); err != nil {
		return nil, err
	}
	if d.SchemaVersion != SchemaVersion || len(d.Teams) == 0 {
		return nil, fmt.Errorf("unsupported discovery schema or empty teams")
	}
	for name, t := range d.Teams {
		if !team.KeyPattern.MatchString(name) || len(t.Releases) == 0 {
			return nil, fmt.Errorf("invalid team or empty releases")
		}
		stable, err := versionParts(t.Stable)
		if err != nil || stable[4] != "" {
			return nil, fmt.Errorf("stable must name a non-prerelease exact version")
		}
		if _, ok := t.Releases[t.Stable]; !ok {
			return nil, fmt.Errorf("stable version is not listed")
		}
		versions := make([]string, 0, len(t.Releases))
		for v, r := range t.Releases {
			if err := ValidateVersion(v); err != nil {
				return nil, err
			}
			if err := ValidateRelativePath(r.Manifest); err != nil {
				return nil, err
			}
			if !ValidSHA256(r.SHA256) {
				return nil, fmt.Errorf("invalid manifest digest")
			}
			versions = append(versions, v)
		}
		sort.Slice(versions, func(i, j int) bool { c, _ := CompareVersions(versions[i], versions[j]); return c < 0 })
		for i := 1; i < len(versions); i++ {
			c, _ := CompareVersions(versions[i-1], versions[i])
			if c == 0 {
				return nil, fmt.Errorf("ambiguous equal-precedence releases")
			}
		}
	}
	return &d, nil
}

// DecodeManifest validates immutable metadata; the consumer additionally proves
// that the downloaded catalog names exactly this team and its complete closure.
func DecodeManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if len(data) > MaxManifestBytes {
		return nil, fmt.Errorf("manifest exceeds size limit")
	}
	if err := decodeStrict(data, &m); err != nil {
		return nil, err
	}
	fields, err := exactObject(data, "schema_version", "catalog_schema_version", "team", "version", "files")
	if err != nil {
		return nil, err
	}
	var files []json.RawMessage
	if err := json.Unmarshal(fields["files"], &files); err != nil {
		return nil, err
	}
	for _, file := range files {
		if _, err := exactObject(file, "path", "size", "sha256"); err != nil {
			return nil, err
		}
	}
	if m.SchemaVersion != SchemaVersion || m.CatalogSchemaVersion != 2 || !team.KeyPattern.MatchString(m.Team) {
		return nil, fmt.Errorf("unsupported manifest schema or invalid team")
	}
	if err := ValidateVersion(m.Version); err != nil {
		return nil, err
	}
	if len(m.Files) == 0 || len(m.Files) > MaxFiles {
		return nil, fmt.Errorf("invalid manifest file count")
	}
	seen := map[string]bool{}
	var total int64
	for _, f := range m.Files {
		if err := ValidateRelativePath(f.Path); err != nil {
			return nil, err
		}
		if seen[f.Path] {
			return nil, fmt.Errorf("duplicate manifest file")
		}
		seen[f.Path] = true
		if f.Size < 0 || f.Size > MaxFileBytes || !ValidSHA256(f.SHA256) {
			return nil, fmt.Errorf("invalid manifest file size or digest")
		}
		total += f.Size
	}
	if total > MaxReleaseBytes || !seen["index.json"] {
		return nil, fmt.Errorf("release too large or missing index.json")
	}
	return &m, nil
}

// Select resolves stable (empty version) or an exact pin without silently falling back.
func Select(d *Discovery, name, version string) (Release, string, error) {
	if d == nil {
		return Release{}, "", fmt.Errorf("missing discovery")
	}
	t, ok := d.Teams[name]
	if !ok {
		return Release{}, "", fmt.Errorf("team not listed")
	}
	if version == "" {
		version = t.Stable
	}
	if err := ValidateVersion(version); err != nil {
		return Release{}, "", err
	}
	r, ok := t.Releases[version]
	if !ok {
		return Release{}, "", fmt.Errorf("requested release not listed")
	}
	return r, version, nil
}

func decodeStrict[T any](data []byte, out *T) error {
	d := json.NewDecoder(bytes.NewReader(data))
	if err := uniqueValue(d); err != nil {
		return fmt.Errorf("decode distribution: %w", err)
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing distribution data")
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return fmt.Errorf("decode distribution: %w", err)
	}
	return nil
}

func uniqueValue(d *json.Decoder) error {
	t, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	for d.More() {
		if delim == '{' {
			token, err := d.Token()
			if err != nil {
				return err
			}
			key, ok := token.(string)
			if !ok || seen[key] {
				return fmt.Errorf("duplicate or invalid JSON key")
			}
			seen[key] = true
		}
		if err := uniqueValue(d); err != nil {
			return err
		}
	}
	_, err = d.Token()
	return err
}

// exactObject also rejects case-insensitive aliases accepted by encoding/json,
// omitted required fields and null values (including otherwise-valid zero sizes).
func exactObject(data []byte, names ...string) (map[string]json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, err
	}
	if len(obj) != len(names) {
		return nil, fmt.Errorf("distribution object has missing or unknown fields")
	}
	for _, name := range names {
		value, ok := obj[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, fmt.Errorf("distribution field %s is required", name)
		}
	}
	return obj, nil
}

func discoveryShape(data []byte) error {
	top, err := exactObject(data, "schema_version", "teams")
	if err != nil {
		return err
	}
	var teams map[string]json.RawMessage
	if err := json.Unmarshal(top["teams"], &teams); err != nil {
		return err
	}
	for _, raw := range teams {
		t, err := exactObject(raw, "stable", "releases")
		if err != nil {
			return err
		}
		var releases map[string]json.RawMessage
		if err := json.Unmarshal(t["releases"], &releases); err != nil {
			return err
		}
		for _, release := range releases {
			if _, err := exactObject(release, "manifest", "sha256"); err != nil {
				return err
			}
		}
	}
	return nil
}
