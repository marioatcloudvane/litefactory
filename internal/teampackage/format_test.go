package teampackage

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestStrictCatalogueAndInventory(t *testing.T) {
	t.Parallel()
	r, _ := fixtureRelease("1.0.0")
	raw, err := json.Marshal(Catalogue{Schema: 1, Releases: []Release{r}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseCatalogue(raw); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name   string
		mutate func(*Release)
	}{
		{"traversal", func(r *Release) { r.Download = "../outside" }},
		{"decoded path", func(r *Release) { r.Files[0].Path = "%2e%2e/outside" }},
		{"duplicate file", func(r *Release) { r.Files = append(r.Files, r.Files[0]); r.Checksum = digest(r.Files) }},
		{"hash", func(r *Release) { r.Files[0].SHA256 = strings.Repeat("A", 64); r.Checksum = digest(r.Files) }},
		{"digest", func(r *Release) { r.Checksum = strings.Repeat("0", 64) }},
		{"config missing", func(r *Release) {
			r.Files = []File{{Path: "x", SHA256: strings.Repeat("0", 64)}}
			r.Checksum = digest(r.Files)
		}},
		{"file directory collision", func(r *Release) {
			r.Files = append(r.Files, File{Path: "research", SHA256: strings.Repeat("0", 64)})
			r.Checksum = digest(r.Files)
		}},
		{"leading v", func(r *Release) { r.Version = "v1.0.0" }},
		{"leading zero", func(r *Release) { r.Version = "01.0.0" }},
		{"prerelease leading zero", func(r *Release) { r.Version = "1.0.0-01" }},
		{"slug", func(r *Release) { r.Slug = "Example" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, _ := fixtureRelease("1.0.0")
			tt.mutate(&r)
			if err := validateRelease(r); !errors.Is(err, ErrInvalidPackage) {
				t.Fatalf("invalid release=%v", err)
			}
		})
	}
	for _, input := range []string{`{"schema":1,"schema":1,"releases":[]}`, `{"schema":2,"releases":[]}`, `{"schema":1,"releases":[],"extra":true}`, `{"schema":1,"releases":null}`, string(raw) + " {}", strings.Replace(string(raw), `"path":`, `"path":"duplicate","path":`, 1)} {
		if _, err := parseCatalogue([]byte(input)); err == nil {
			t.Errorf("invalid catalogue accepted: %s", input)
		}
	}
	duplicate, err := json.Marshal(Catalogue{Schema: 1, Releases: []Release{r, r}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseCatalogue(duplicate); err == nil {
		t.Fatal("duplicate releases accepted")
	}
	if _, err := parseCatalogue(make([]byte, MaxCatalogueBytes+1)); err == nil {
		t.Fatal("catalogue size bound lost")
	}
}
func TestStrictWorkflowMap(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"research: research/research.dot\n", "research: research/a.dot\nplan: plan/b.dot\nimplement: implement/c.dot\nvalidate: validate/d.dot\nspike: spike/e.dot\nbug: bug/f.dot\n"} {
		if _, err := parseConfig([]byte(raw)); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{"", "{}", "research: x\nresearch: y", "unknown: x", "research: 12", "research: [x]", "research: ../x", "research: /x", "research: a/%2e%2e/x", "research: &graph a.dot\nplan: *graph", "research: a.dot\n---\nplan: b.dot", "research: null", "&map\nresearch: x"} {
		if _, err := parseConfig([]byte(raw)); err == nil {
			t.Errorf("invalid YAML accepted: %q", raw)
		}
	}
	if _, err := parseConfig(make([]byte, MaxCatalogueBytes+1)); err == nil {
		t.Fatal("config size bound lost")
	}
}
func TestDigestAndSemVerSelection(t *testing.T) {
	t.Parallel()
	files := []File{{Path: "z", SHA256: strings.Repeat("0", 64)}, {Path: "team.config", SHA256: strings.Repeat("1", 64)}}
	// This fixed value comes from the specified sorted path/NUL/hash/newline bytes.
	if got := digest(files); got != "5ee0dbd6bf80c8c8dd672677ad07453ddfcbac820460cd991b852e4ca77287e8" {
		t.Fatalf("digest=%s", got)
	}
	reversed := []File{files[1], files[0]}
	if digest(files) != digest(reversed) {
		t.Fatal("digest depends on input ordering")
	}
	for _, v := range []string{"0.0.0", "1.0.0-rc.1", "1.0.0+build.001"} {
		if _, err := version(v); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []string{"1", "1.0", "v1.0.0", "1.0.0-00", "1.0.0-000000000000000000000000000000000001", "1.0.0+", "1.0.0/../2"} {
		if _, err := version(v); err == nil {
			t.Errorf("invalid version accepted %q", v)
		}
	}
	c := Catalogue{Schema: 1}
	for _, v := range []string{"1.9.0", "1.10.0", "2.0.0-rc.1", "1.10.0+build"} {
		r, _ := fixtureRelease(v)
		c.Releases = append(c.Releases, r)
	}
	r, err := selectRelease(c, "example", "")
	if err != nil || r.Version != "1.10.0+build" {
		t.Fatalf("selection=%s %v", r.Version, err)
	}
}
