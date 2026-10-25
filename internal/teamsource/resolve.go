package teamsource

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
)

// baseManifest is teams/source/<base>/base.json. It marks a folder as a base.
type baseManifest struct {
	Variables []string `json:"variables"`
}

// inheritManifest is teams/source/<team>/inherit.json.
type inheritManifest struct {
	Base      string            `json:"base"`
	Replace   []string          `json:"replace"`
	Exclude   []string          `json:"exclude"`
	Variables map[string]string `json:"variables"`
}

var (
	variableName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	placeholder  = regexp.MustCompile(`\{\{([^{}\n]*)\}\}`)
)

// RefuseBase stops a base from being built as a team.
func RefuseBase(root *os.Root, source string) error {
	if _, err := root.Lstat(source + "/base.json"); err == nil {
		return fmt.Errorf("%s: %s is a base, not a team", source, path.Base(source))
	}
	return nil
}

// ResolveSource returns the files of one source (path to bytes): stack-profile.json
// and the workflow folders. A source without inherit.json resolves to its own
// folder. It only reads files; every inheritance problem is reported together,
// sorted, one per line.
func ResolveSource(ctx context.Context, root *os.Root, source, slug string) (map[string][]byte, error) {
	if _, err := root.Lstat(source + "/inherit.json"); errors.Is(err, fs.ErrNotExist) {
		return ownFiles(ctx, root, source, true)
	}
	manifestPath := source + "/inherit.json"
	raw, err := readFile(root, manifestPath)
	if err != nil {
		return nil, err
	}
	var m inheritManifest
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&m); err != nil {
		return nil, fmt.Errorf("%s: %w", manifestPath, err)
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: trailing JSON", manifestPath)
	}
	var faults []string
	fail := func(format string, args ...any) {
		faults = append(faults, manifestPath+": "+fmt.Sprintf(format, args...))
	}
	if m.Base == "" || !safePath(m.Base) || strings.Contains(m.Base, "/") {
		return nil, fmt.Errorf("%s: base %q: not a valid base name", manifestPath, m.Base)
	}
	baseDir := path.Dir(source) + "/" + m.Base
	braw, err := readFile(root, baseDir+"/base.json")
	if err != nil {
		return nil, fmt.Errorf("%s: base %s: missing or unreadable base (%s/base.json): %w", manifestPath, m.Base, baseDir, err)
	}
	var bm baseManifest
	d = json.NewDecoder(bytes.NewReader(braw))
	d.DisallowUnknownFields()
	if err := d.Decode(&bm); err != nil {
		return nil, fmt.Errorf("%s/base.json: %w", baseDir, err)
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s/base.json: trailing JSON", baseDir)
	}
	declared := map[string]bool{}
	for _, v := range bm.Variables {
		if !variableName.MatchString(v) || declared[v] {
			return nil, fmt.Errorf("%s/base.json: variable %q: invalid or duplicate name", baseDir, v)
		}
		declared[v] = true
	}
	if _, err := root.Lstat(baseDir + "/inherit.json"); err == nil {
		return nil, fmt.Errorf("%s/inherit.json: a base cannot name a base", baseDir)
	}
	baseFiles, err := ownFiles(ctx, root, baseDir, false)
	if err != nil {
		return nil, err
	}
	childFiles, err := ownFiles(ctx, root, source, false)
	if err != nil {
		return nil, err
	}

	listed := map[string]string{}
	check := func(field string, paths []string) {
		for _, p := range paths {
			if !inResolution(p) {
				fail("%s %s: path is not stack-profile.json or inside a workflow folder", field, p)
				continue
			}
			if prev, dup := listed[p]; dup {
				fail("%s %s: already listed under %s", field, p, prev)
				continue
			}
			listed[p] = field
		}
	}
	check("replace", m.Replace)
	check("exclude", m.Exclude)
	replaced, excluded := map[string]bool{}, map[string]bool{}
	for _, p := range m.Replace {
		if listed[p] != "replace" {
			continue
		}
		replaced[p] = true
		if _, ok := baseFiles[p]; !ok {
			fail("replace %s: base %s has no such file", p, m.Base)
		} else if _, ok := childFiles[p]; !ok {
			fail("replace %s: source provides no replacement file", p)
		}
	}
	for _, p := range m.Exclude {
		if listed[p] != "exclude" {
			continue
		}
		excluded[p] = true
		if _, ok := baseFiles[p]; !ok {
			fail("exclude %s: base %s has no such file", p, m.Base)
		}
		if _, ok := childFiles[p]; ok {
			fail("exclude %s: source also provides this file", p)
		}
	}
	for p := range childFiles {
		if _, ok := baseFiles[p]; ok && !replaced[p] && !excluded[p] {
			fail("add %s: collides with base %s file; list it under replace", p, m.Base)
		}
	}

	values := map[string]string{"team": slug}
	for name, value := range m.Variables {
		switch {
		case name == "team":
			fail("variable team: filled from the release slug and cannot be set")
		case !declared[name]:
			fail("variable %s: not declared by base %s", name, m.Base)
		case strings.Contains(value, "\n") || strings.Contains(value, "\r") || strings.Contains(value, "{{"):
			fail("variable %s: value must be a single line without {{", name)
		default:
			values[name] = value
		}
	}
	for _, name := range bm.Variables {
		if _, set := m.Variables[name]; !set && name != "team" {
			fail("variable %s: declared by base %s but not set", name, m.Base)
		}
	}

	out := map[string][]byte{}
	for p, content := range baseFiles {
		if excluded[p] {
			continue
		}
		if replaced[p] {
			if own, ok := childFiles[p]; ok {
				out[p] = own
			}
			continue
		}
		filled := placeholder.ReplaceAllFunc(content, func(match []byte) []byte {
			name := string(placeholder.FindSubmatch(match)[1])
			if !declared[name] {
				fail("variable %s: used in base file %s but not declared by base %s", name, p, m.Base)
				return match
			}
			value, ok := values[name]
			if !ok {
				return match
			}
			return []byte(value)
		})
		if bytes.Count(content, []byte("{{")) != len(placeholder.FindAll(content, -1)) {
			fail("variable placeholder: base file %s has a malformed {{ }}", p)
		}
		out[p] = filled
	}
	for p, content := range childFiles {
		if _, ok := baseFiles[p]; !ok {
			out[p] = content
		}
	}
	if len(faults) > 0 {
		slices.Sort(faults)
		faults = slices.Compact(faults)
		return nil, errors.New(strings.Join(faults, "\n"))
	}
	return out, nil
}

func inResolution(p string) bool {
	if !safePath(p) {
		return false
	}
	if p == "stack-profile.json" {
		return true
	}
	first, rest, ok := strings.Cut(p, "/")
	return ok && rest != "" && slices.Contains(sourceWorkflows, first)
}

// ownFiles reads stack-profile.json and the workflow folders of one folder.
// A strict source must hold them (as before); a manifest side may lack them.
func ownFiles(ctx context.Context, root *os.Root, dir string, strict bool) (map[string][]byte, error) {
	files := map[string][]byte{}
	profile, err := readFile(root, dir+"/stack-profile.json")
	switch {
	case err == nil:
		files["stack-profile.json"] = profile
	case strict:
		return nil, fmt.Errorf("stack-profile.json: %w", err)
	case !errors.Is(err, fs.ErrNotExist):
		return nil, err
	}
	for _, w := range sourceWorkflows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !strict {
			if _, err := root.Lstat(dir + "/" + w); errors.Is(err, fs.ErrNotExist) {
				continue
			}
		}
		source, err := sourceTree(root, dir+"/"+w)
		if err != nil {
			return nil, err
		}
		for name, raw := range source {
			files[w+"/"+name] = raw
		}
	}
	return files, nil
}

var sourceWorkflows = []string{"bug", "implement", "plan", "research", "spike", "validate"}

// SafePath reports whether name is a clean, relative, portable path.
func SafePath(name string) bool { return safePath(name) }

// ReadSourceFile reads one regular source file under root, refusing symlinks and oversize files.
func ReadSourceFile(root *os.Root, name string) ([]byte, error) { return readFile(root, name) }

// SourceTree reads every file under dir (relative path to bytes), refusing symlinks.
func SourceTree(root *os.Root, dir string) (map[string][]byte, error) { return sourceTree(root, dir) }

// SafeAncestors refuses a name whose existing ancestors are symlinks or not directories.
func SafeAncestors(root *os.Root, name string) error { return safeAncestors(root, name) }

func safePath(name string) bool {
	return name != "." && fs.ValidPath(name) && path.Clean(name) == name && !strings.ContainsAny(name, "\\:\x00\r\n%?#")
}

func safeAncestors(root *os.Root, name string) error {
	if !safePath(name) {
		return fmt.Errorf("%s: unsafe path", name)
	}
	parts := strings.Split(name, "/")
	for i := range parts {
		p := strings.Join(parts[:i+1], "/")
		info, err := root.Lstat(p)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 || (i < len(parts)-1 && !info.IsDir()) {
			return fmt.Errorf("%s: symlink or invalid directory", p)
		}
	}
	return nil
}

func readFile(root *os.Root, name string) ([]byte, error) {
	if err := safeAncestors(root, name); err != nil {
		return nil, err
	}
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxSourceBytes {
		return nil, fmt.Errorf("%s: nonregular or oversized source", name)
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	after, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, after) {
		return nil, fmt.Errorf("%s: source changed while opening", name)
	}
	raw, err := io.ReadAll(io.LimitReader(f, MaxSourceBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > MaxSourceBytes {
		return nil, fmt.Errorf("%s: oversized source", name)
	}
	return raw, nil
}

func sourceTree(root *os.Root, dir string) (map[string][]byte, error) {
	if err := safeAncestors(root, dir); err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	err := fs.WalkDir(root.FS(), dir, func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(name, dir+"/")
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s: symlink source/output", name)
		}
		if d.IsDir() {
			return nil
		}
		if !safePath(rel) {
			return fmt.Errorf("%s: unsafe source/output path", name)
		}
		raw, err := readFile(root, name)
		if err != nil {
			return err
		}
		files[rel] = raw
		return nil
	})
	return files, err
}
