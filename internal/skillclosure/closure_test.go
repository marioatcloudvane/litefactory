package skillclosure

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestClosureRejectsCyclesCollisionsAndUnsafeResources(t *testing.T) {
	skill := func(name, header string) []byte {
		return []byte("---\nname: " + name + "\ndescription: Test\n" + header + "\n---\nExact body\n")
	}
	for _, tc := range []struct {
		name     string
		selected []string
		files    map[string][]byte
		want     string
	}{
		{"cycle", []string{"first"}, map[string][]byte{"skills/first/SKILL.md": skill("first", "requires: [second]"), "skills/second/SKILL.md": skill("second", "requires: [first]")}, "cycle"},
		{"collision", []string{"evidence", "shared/evidence"}, map[string][]byte{"skills/evidence/SKILL.md": skill("evidence", "")}, "collide"},
		{"unsafe resource", []string{"first"}, map[string][]byte{"skills/first/SKILL.md": skill("first", "resources: [../secret]")}, "unsafe"},
		{"duplicate dependency", []string{"first"}, map[string][]byte{"skills/first/SKILL.md": skill("first", "requires: [second, second]"), "skills/second/SKILL.md": skill("second", "")}, "duplicate"},
		{"invalid header", []string{"first"}, map[string][]byte{"skills/first/SKILL.md": skill("first", "unknown: true")}, "field unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := Load(tc.selected, func(path string) ([]byte, error) {
				raw, ok := tc.files[path]
				if !ok {
					return nil, fmt.Errorf("missing %s", path)
				}
				return raw, nil
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %s", err, tc.want)
			}
		})
	}
	_, body, err := Parse(skill("first", ""), "first")
	if err != nil || string(body) != "Exact body\n" {
		t.Fatalf("body changed: %q %v", body, err)
	}
	failure := errors.New("canceled reader")
	_, _, err = Load([]string{"first"}, func(string) ([]byte, error) { return nil, failure })
	if !errors.Is(err, failure) {
		t.Fatalf("reader error lost: %v", err)
	}
}
