package teamrelease

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

// refuseBase stops a base from being built as a team.
func refuseBase(root *os.Root, source string) error {
	if _, err := root.Lstat(source + "/base.json"); err == nil {
		return fmt.Errorf("%s: %s is a base, not a team", source, path.Base(source))
	}
	return nil
}

// resolveSource returns the files of one source (path to bytes): stack-profile.json
// and the workflow folders. A source without inherit.json resolves to its own
// folder. It only reads files; every inheritance problem is reported together,
// sorted, one per line.
func resolveSource(ctx context.Context, root *os.Root, source, slug string) (map[string][]byte, error) {
	if _, err := root.Lstat(source + "/inherit.json"); errors.Is(err, fs.ErrNotExist) {
		return ownFiles(ctx, root, source, true)
	}
	manifestPath := source + "/inherit.json"
	raw, err := read(root, manifestPath)
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
	if m.Base == "" || !safe(m.Base) || strings.Contains(m.Base, "/") {
		return nil, fmt.Errorf("%s: base %q: not a valid base name", manifestPath, m.Base)
	}
	baseDir := path.Dir(source) + "/" + m.Base
	braw, err := read(root, baseDir+"/base.json")
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
	if !safe(p) {
		return false
	}
	if p == "stack-profile.json" {
		return true
	}
	first, rest, ok := strings.Cut(p, "/")
	return ok && rest != "" && slices.Contains(workflows, first)
}

// ownFiles reads stack-profile.json and the workflow folders of one folder.
// A strict source must hold them (as before); a manifest side may lack them.
func ownFiles(ctx context.Context, root *os.Root, dir string, strict bool) (map[string][]byte, error) {
	files := map[string][]byte{}
	profile, err := read(root, dir+"/stack-profile.json")
	switch {
	case err == nil:
		files["stack-profile.json"] = profile
	case strict:
		return nil, fmt.Errorf("stack-profile.json: %w", err)
	case !errors.Is(err, fs.ErrNotExist):
		return nil, err
	}
	for _, w := range workflows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !strict {
			if _, err := root.Lstat(dir + "/" + w); errors.Is(err, fs.ErrNotExist) {
				continue
			}
		}
		source, err := tree(root, dir+"/"+w)
		if err != nil {
			return nil, err
		}
		for name, raw := range source {
			files[w+"/"+name] = raw
		}
	}
	return files, nil
}
