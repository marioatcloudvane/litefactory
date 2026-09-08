package teamremote_test

import (
	"github.com/cloudvane/litefactory/internal/teamremote"
	"strings"
	"testing"
)

func TestVersionContracts(t *testing.T) {
	for _, v := range []string{"1.2.3", "1.0.0-alpha.1+build", "999999999999999999999999999999.0.0"} {
		if err := teamremote.ValidateVersion(v); err != nil {
			t.Errorf("valid %q: %v", v, err)
		}
	}
	for _, v := range []string{"v1.2.3", "^1.0.0", "01.2.3", "1.0", "1.0.0-01", "1.0.0-", "1.0.0+"} {
		if err := teamremote.ValidateVersion(v); err == nil {
			t.Errorf("invalid version accepted: %q", v)
		}
	}
	for _, tt := range []struct {
		a, b string
		want int
	}{{"1.10.0", "1.9.0", 1}, {"1.0.0-alpha.2", "1.0.0-alpha.10", -1}, {"1.0.0-rc.1", "1.0.0", -1}, {"1.0.0+a", "1.0.0+b", 0}, {"99999999999999999999999999.0.0", "9999999999999999999999999.0.0", 1}} {
		t.Run(tt.a+"/"+tt.b, func(t *testing.T) {
			got, err := teamremote.CompareVersions(tt.a, tt.b)
			if err != nil || got != tt.want {
				t.Fatalf("got %d,%v want %d", got, err, tt.want)
			}
		})
	}
}

func TestURLContracts(t *testing.T) {
	got, err := teamremote.NormalizeIndexURL("https://github.com/marioatcloudvane/litefactory/teams/teams.json")
	if err != nil || got != "https://raw.githubusercontent.com/marioatcloudvane/litefactory/HEAD/teams/teams.json" {
		t.Fatalf("default normalization: %q %v", got, err)
	}
	for _, p := range []string{"../secret", "/secret", "a/../b", "a\\b", "a%2fb", "a//b", "https://evil.example/a", "a?token=x", "a#x"} {
		t.Run(p, func(t *testing.T) {
			if err := teamremote.ValidateRelativePath(p); err == nil {
				t.Fatalf("unsafe path accepted %q", p)
			}
		})
	}
	got, err = teamremote.ResolveReference("https://example.org/catalog/releases/manifest.json", "agents/go.md")
	if err != nil || got != "https://example.org/catalog/releases/agents/go.md" {
		t.Fatalf("reference %q %v", got, err)
	}
	for _, u := range []string{"http://example.org/index.json", "https://user:secret@example.org/index.json", "https://example.org/index.json?token=x", "https://example.org/index.json#x"} {
		if _, err := teamremote.NormalizeIndexURL(u); err == nil {
			t.Errorf("unsafe URL accepted %s", u)
		}
	}
}

func TestDiscoveryStrictAndSelection(t *testing.T) {
	digest := strings.Repeat("a", 64)
	good := `{"schema_version":1,"teams":{"go-team":{"stable":"1.0.0","releases":{"1.0.0":{"manifest":"releases/1/manifest.json","sha256":"` + digest + `"},"2.0.0-rc.1":{"manifest":"releases/2/manifest.json","sha256":"` + digest + `"}}}}}`
	d, err := teamremote.DecodeDiscovery([]byte(good))
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ version, want string }{{"", "1.0.0"}, {"2.0.0-rc.1", "2.0.0-rc.1"}} {
		_, got, err := teamremote.Select(d, "go-team", tt.version)
		if err != nil || got != tt.want {
			t.Fatalf("select %q = %q %v", tt.version, got, err)
		}
	}
	for _, tt := range []struct{ name, body string }{
		{"duplicate", strings.Replace(good, `"schema_version":1`, `"schema_version":1,"schema_version":1`, 1)},
		{"unknown", strings.Replace(good, `"schema_version":1`, `"schema_version":1,"extra":true`, 1)},
		{"case alias", strings.Replace(good, `"schema_version"`, `"Schema_Version"`, 1)},
		{"schema", strings.Replace(good, `"schema_version":1`, `"schema_version":2`, 1)},
		{"prerelease stable", strings.Replace(good, `"stable":"1.0.0"`, `"stable":"2.0.0-rc.1"`, 1)},
		{"bad hash", strings.Replace(good, digest, "wrong", 1)},
		{"traversal", strings.Replace(good, "releases/1/manifest.json", "../manifest.json", 1)},
		{"trailing", good + ` {}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := teamremote.DecodeDiscovery([]byte(tt.body)); err == nil {
				t.Fatal("invalid discovery accepted")
			}
		})
	}
}

func TestManifestRejectsInvalidFiles(t *testing.T) {
	good := `{"schema_version":1,"catalog_schema_version":2,"team":"go-team","version":"1.0.0","files":[{"path":"index.json","size":1,"sha256":"` + strings.Repeat("a", 64) + `"}]}`
	if _, err := teamremote.DecodeManifest([]byte(good)); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ name, body string }{
		{"unknown", strings.Replace(good, `"size":1`, `"size":1,"extra":true`, 1)},
		{"negative size", strings.Replace(good, `"size":1`, `"size":-1`, 1)},
		{"oversize file", strings.Replace(good, `"size":1`, `"size":1048577`, 1)},
		{"unsupported payload schema", strings.Replace(good, `"catalog_schema_version":2`, `"catalog_schema_version":3`, 1)},
		{"no catalog", strings.Replace(good, `"index.json"`, `"agents/reader.md"`, 1)},
		{"traversal", strings.Replace(good, `"index.json"`, `"../index.json"`, 1)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := teamremote.DecodeManifest([]byte(tt.body)); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
}
