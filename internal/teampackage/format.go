package teampackage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// MaxCatalogueBytes bounds catalogue and team.config reads before allocation.
const MaxCatalogueBytes = 1 << 20

// MaxReleaseBytes bounds the complete inventory content.
const MaxReleaseBytes = 64 << 20

// MaxFiles bounds the number of downloadable ordinary files.
const MaxFiles = 4096

// ErrInvalidPackage marks invalid format, inventory or digest data.
var ErrInvalidPackage = errors.New("invalid team package")

// ErrConflict marks an immutable identity collision or installed drift.
var ErrConflict = errors.New("installed release conflict")

// ErrNotFound marks a missing selected release.
var ErrNotFound = errors.New("team release not found")

// ErrIncompatibleWorkflow marks rejection by factory workflow policy.
var ErrIncompatibleWorkflow = errors.New("incompatible workflow")

// ErrTransport marks bounded anonymous fetching failures.
var ErrTransport = errors.New("team transport failed")

// File identifies the exact bytes of a package-relative ordinary file.
type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// Release is the source's immutable release inventory.
type Release struct {
	Name        string   `json:"name"`
	Slug        string   `json:"slug"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Download    string   `json:"download"`
	Files       []File   `json:"files"`
	Checksum    string   `json:"checksum"`
}

// Catalogue is the versioned anonymous release discovery format.
type Catalogue struct {
	Schema   int       `json:"schema"`
	Releases []Release `json:"releases"`
}

// Identity identifies exact verified local bytes, independent of graph team names.
type Identity struct {
	Source  string
	Slug    string
	Version string
	Digest  string
}

// Installed reports a locally verified release and its supported workflows.
type Installed struct {
	Identity  Identity
	Release   Release
	Path      string
	Workflows map[string]string
	Integrity bool
}

type receipt struct {
	Schema   int     `json:"schema"`
	Source   string  `json:"source"`
	Release  Release `json:"release"`
	Checksum string  `json:"checksum"`
}

var slugPattern = regexp.MustCompile(`^[a-z](?:[a-z0-9]*|[a-z0-9]*(?:-[a-z0-9]+)+)$`)
var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)
var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func version(v string) ([]string, error) {
	m := versionPattern.FindStringSubmatch(v)
	if m == nil {
		return nil, fmt.Errorf("%w: noncanonical SemVer", ErrInvalidPackage)
	}
	for _, part := range strings.Split(m[4], ".") {
		if len(part) > 1 && part[0] == '0' {
			if strings.Trim(part, "0123456789") == "" {
				return nil, fmt.Errorf("%w: prerelease leading zero", ErrInvalidPackage)
			}
		}
	}
	return m, nil
}
func compare(a, b string) int {
	am, _ := version(a)
	bm, _ := version(b)
	for i := 1; i <= 3; i++ {
		if len(am[i]) != len(bm[i]) {
			return len(am[i]) - len(bm[i])
		}
		if c := strings.Compare(am[i], bm[i]); c != 0 {
			return c
		}
	}
	return 0 // automatic selection considers stable releases only
}
func safePath(p string) bool {
	return p != "." && fs.ValidPath(p) && path.Clean(p) == p && !strings.ContainsAny(p, "\\:\x00\r\n%?#")
}
func digest(files []File) string {
	sorted := slices.Clone(files)
	slices.SortFunc(sorted, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	h := sha256.New()
	for _, f := range sorted {
		_, _ = io.WriteString(h, f.Path+"\x00"+f.SHA256+"\n")
	}
	return hex.EncodeToString(h.Sum(nil))
}
func validateRelease(r Release) error {
	var faults []error
	if strings.TrimSpace(r.Name) == "" {
		faults = append(faults, errors.New("empty display name"))
	}
	if !slugPattern.MatchString(r.Slug) {
		faults = append(faults, errors.New("invalid slug"))
	}
	if _, err := version(r.Version); err != nil {
		faults = append(faults, err)
	}
	if !safePath(r.Download) {
		faults = append(faults, errors.New("unsafe download path"))
	}
	if len(r.Files) == 0 || len(r.Files) > MaxFiles {
		faults = append(faults, errors.New("invalid inventory size"))
	}
	seen := map[string]bool{}
	for _, f := range r.Files {
		if !safePath(f.Path) || !hashPattern.MatchString(f.SHA256) || seen[f.Path] {
			faults = append(faults, fmt.Errorf("invalid inventory path/hash: %q", f.Path))
		}
		seen[f.Path] = true
	}
	for _, f := range r.Files {
		for parent := path.Dir(f.Path); parent != "."; parent = path.Dir(parent) {
			if seen[parent] {
				faults = append(faults, fmt.Errorf("file/directory collision: %q", f.Path))
			}
		}
	}
	if !seen["team.config"] {
		faults = append(faults, errors.New("missing team.config"))
	}
	if !hashPattern.MatchString(r.Checksum) || digest(r.Files) != r.Checksum {
		faults = append(faults, errors.New("release checksum mismatch"))
	}
	if err := errors.Join(faults...); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidPackage, err)
	}
	return nil
}

// strictJSON rejects duplicate keys at every depth, unknown fields and trailing values.
func strictJSON(raw []byte, dst any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	var value func(reflect.Type) error
	value = func(expected reflect.Type) error {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		if tok == nil {
			return errors.New("null JSON field")
		}
		delim, composite := tok.(json.Delim)
		if !composite {
			return nil
		}
		switch delim {
		case '{':
			if expected.Kind() != reflect.Struct {
				return errors.New("unexpected JSON object")
			}
			fields := map[string]reflect.Type{}
			for i := 0; i < expected.NumField(); i++ {
				field := expected.Field(i)
				fields[field.Tag.Get("json")] = field.Type
			}
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return errors.New("duplicate JSON key")
				}
				field, ok := fields[name]
				if !ok {
					return fmt.Errorf("unknown JSON key %q", name)
				}
				seen[name] = true
				if err := value(field); err != nil {
					return err
				}
			}
			for name := range fields {
				if !seen[name] {
					return errors.New("missing required JSON field")
				}
			}
		case '[':
			if expected.Kind() != reflect.Slice {
				return errors.New("unexpected JSON array")
			}
			for d.More() {
				if err := value(expected.Elem()); err != nil {
					return err
				}
			}
		default:
			return errors.New("invalid JSON delimiter")
		}
		_, err = d.Token()
		return err
	}
	if err := value(reflect.TypeOf(dst).Elem()); err != nil {
		return err
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON value")
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(dst)
}

func parseCatalogue(raw []byte) (Catalogue, error) {
	var c Catalogue
	if len(raw) > MaxCatalogueBytes {
		return c, fmt.Errorf("%w: catalogue too large", ErrInvalidPackage)
	}
	if err := strictJSON(raw, &c); err != nil {
		return c, fmt.Errorf("%w: catalogue: %w", ErrInvalidPackage, err)
	}
	var faults []error
	if c.Schema != 1 || c.Releases == nil {
		faults = append(faults, errors.New("unsupported catalogue schema"))
	}
	seen := map[string]bool{}
	for _, r := range c.Releases {
		key := r.Slug + "/" + r.Version
		if seen[key] {
			faults = append(faults, fmt.Errorf("duplicate release %s", key))
		}
		seen[key] = true
		if err := validateRelease(r); err != nil {
			faults = append(faults, fmt.Errorf("%s: %w", key, err))
		}
	}
	if err := errors.Join(faults...); err != nil {
		return c, fmt.Errorf("%w: %w", ErrInvalidPackage, err)
	}
	return c, nil
}
func parseConfig(raw []byte) (map[string]string, error) {
	if len(raw) > MaxCatalogueBytes {
		return nil, fmt.Errorf("%w: team.config too large", ErrInvalidPackage)
	}
	var n yaml.Node
	d := yaml.NewDecoder(bytes.NewReader(raw))
	if err := d.Decode(&n); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidPackage, err)
	}
	var tail yaml.Node
	if err := d.Decode(&tail); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: multiple YAML documents", ErrInvalidPackage)
	}
	if len(n.Content) != 1 || n.Content[0].Kind != yaml.MappingNode || n.Content[0].Tag != "!!map" || len(n.Content[0].Content) == 0 || n.Content[0].Anchor != "" {
		return nil, fmt.Errorf("%w: team.config must be a nonempty mode mapping", ErrInvalidPackage)
	}
	allowed := map[string]bool{"research": true, "plan": true, "implement": true, "validate": true, "spike": true, "bug": true}
	result := map[string]string{}
	var faults []error
	nodes := n.Content[0].Content
	for i := 0; i < len(nodes); i += 2 {
		k, v := nodes[i], nodes[i+1]
		if k.Kind != yaml.ScalarNode || k.Tag != "!!str" || !allowed[k.Value] || result[k.Value] != "" || v.Kind != yaml.ScalarNode || v.Tag != "!!str" || v.Anchor != "" || k.Anchor != "" || !safePath(v.Value) {
			faults = append(faults, fmt.Errorf("invalid workflow entry %q", k.Value))
			continue
		}
		result[k.Value] = v.Value
	}
	if err := errors.Join(faults...); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidPackage, err)
	}
	return result, nil
}
