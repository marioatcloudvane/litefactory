// Package skillclosure validates selected package skills and their complete
// dependency/resource closure without choosing skills or granting authority.
package skillclosure

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// Header declares the supported skill metadata and captured dependencies.
type Header struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Requires    []string `yaml:"requires"`
	Resources   []string `yaml:"resources"`
}

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

// ValidName accepts a bounded skill name or shared skill reference.
func ValidName(name string) bool {
	name = strings.TrimPrefix(name, "shared/")
	return len(name) <= 64 && namePattern.MatchString(name)
}

// SafePath confines a resource to its skill directory.
func SafePath(name string) bool {
	return name != "." && fs.ValidPath(name) && !strings.ContainsAny(name, "\\:\x00")
}

// Parse preserves the exact body bytes while validating the supported header.
func Parse(raw []byte, id string) (Header, []byte, error) {
	var header Header
	if !ValidName(id) || !utf8.Valid(raw) || !bytes.HasPrefix(raw, []byte("---\n")) {
		return header, nil, fmt.Errorf("invalid skill %q", id)
	}
	rest := raw[4:]
	end := bytes.Index(rest, []byte("\n---"))
	if end < 0 {
		return header, nil, fmt.Errorf("invalid skill header %q", id)
	}
	after := rest[end+4:]
	if len(after) != 0 && after[0] != '\n' {
		return header, nil, fmt.Errorf("invalid skill delimiter %q", id)
	}
	body := bytes.TrimPrefix(after, []byte("\n"))
	dec := yaml.NewDecoder(bytes.NewReader(rest[:end]))
	dec.KnownFields(true)
	if err := dec.Decode(&header); err != nil {
		return header, nil, fmt.Errorf("skill %s: %w", id, err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return header, nil, fmt.Errorf("multiple skill headers %q", id)
	}
	if header.Name != path.Base(id) || strings.TrimSpace(header.Description) == "" || len(bytes.TrimSpace(body)) == 0 {
		return header, nil, fmt.Errorf("invalid skill metadata/body %q", id)
	}
	return header, body, nil
}

// Load reads every selected skill, required skill and declared resource once.
// read owns confinement, cancellation, regular-file and aggregate size limits.
// Skills are returned in dependency order; files retain exact bytes.
func Load(selected []string, read func(string) ([]byte, error)) (skills []string, files map[string][]byte, err error) {
	files = map[string][]byte{}
	states := map[string]int{}
	native := map[string]string{}
	var visit func(string) error
	get := func(name string) ([]byte, error) {
		if raw, ok := files[name]; ok {
			return raw, nil
		}
		raw, err := read(name)
		if err != nil {
			return nil, err
		}
		files[name] = raw
		return raw, nil
	}
	visit = func(id string) error {
		if !ValidName(id) {
			return fmt.Errorf("invalid skill %q", id)
		}
		if states[id] == 1 {
			return fmt.Errorf("skill dependency cycle at %s", id)
		}
		if states[id] == 2 {
			return nil
		}
		short := path.Base(id)
		if previous, ok := native[short]; ok && previous != id {
			return fmt.Errorf("skills %s and %s collide as %s", previous, id, short)
		}
		native[short] = id
		states[id] = 1
		name := "skills/" + id + "/SKILL.md"
		raw, err := get(name)
		if err != nil {
			return err
		}
		header, _, err := Parse(raw, id)
		if err != nil {
			return err
		}

		var faults []error
		seen := map[string]bool{}
		for _, dep := range header.Requires {
			if seen[dep] {
				faults = append(faults, fmt.Errorf("duplicate skill dependency %q", dep))
				continue
			}
			seen[dep] = true
			faults = append(faults, visit(dep))
		}
		seen = map[string]bool{}
		for _, resource := range header.Resources {
			if !SafePath(resource) || seen[resource] {
				faults = append(faults, fmt.Errorf("unsafe or duplicate skill resource %q", resource))
				continue
			}
			seen[resource] = true
			_, err := get(path.Join(path.Dir(name), resource))
			faults = append(faults, err)
		}
		if err := errors.Join(faults...); err != nil {
			return err
		}
		states[id] = 2
		skills = append(skills, id)
		return nil
	}

	var faults []error
	for _, id := range selected {
		faults = append(faults, visit(id))
	}
	if err := errors.Join(faults...); err != nil {
		return nil, nil, err
	}
	return skills, files, nil
}
