package teambuild

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/teampackage"
)

const swiftTeam = "swift-ios"

// These tests prove offline package contracts, never live Apple execution.
func TestSwiftIOSBuildSelectionAndReproducibility(t *testing.T) {
	t.Parallel()
	source := repoSource()
	version := sourceVersion(t, swiftTeam)
	files, release, err := Build(t.Context(), source, swiftTeam)
	if err != nil {
		t.Fatal(err)
	}
	again, second, err := Build(t.Context(), source, swiftTeam)
	if err != nil || second.Checksum != release.Checksum || !reflect.DeepEqual(again, files) {
		t.Fatalf("Swift build is not reproducible: %v", err)
	}
	m := newManager(t)
	installed, _, err := m.InstallBuilt(t.Context(), identity(source), files, release, teampackage.BuildInfo{})
	if err != nil || installed.Identity.Slug != swiftTeam || installed.Identity.Version != version || installed.Identity.Digest != release.Checksum {
		t.Fatal("exact Swift selection failed", err)
	}
	corrupt := map[string][]byte{}
	for name, raw := range files {
		corrupt[name] = raw
	}
	corrupt["stack-profile.json"] = []byte(`{"version":99}`)
	if _, _, err := newManager(t).InstallBuilt(t.Context(), identity(source), corrupt, release, teampackage.BuildInfo{}); err == nil {
		t.Fatal("checksum corruption accepted")
	}
}

func TestSwiftIOSInvalidSourceRejected(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		edit func(*testing.T, string)
	}{
		{"malformed profile", func(t *testing.T, dir string) {
			put(t, filepath.Join(dir, swiftTeam, "stack-profile.json"), []byte(`{"version":99}`))
		}},
		{"missing guidance", func(t *testing.T, dir string) {
			if err := os.Remove(filepath.Join(dir, swiftTeam, "plan/skills/swift-ios-guidance/compatibility.md")); err != nil {
				t.Fatal(err)
			}
		}},
		{"unsupported workflow", func(t *testing.T, dir string) {
			// Swift inherits its graphs; break the base graph its build resolves.
			name := filepath.Join(dir, "base-team", "implement/implement.dot")
			raw, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			put(t, name, bytes.ReplaceAll(raw, []byte("lf_implement_"), []byte("lf_unsupported_")))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := copySource(t)
			tc.edit(t, dir)
			if _, _, err := Build(t.Context(), dir, swiftTeam); err == nil {
				t.Fatal("invalid Swift source accepted")
			}
		})
	}
	// Keep schema decoding closed rather than inventing Swift-only host fields.
	var invalid map[string]any
	raw, err := os.ReadFile("../../teams/source/swift-ios/stack-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &invalid); err != nil {
		t.Fatal(err)
	}
	invalid["toolchain_versions"] = map[string]string{"xcode": "26.3"}
	raw, err = json.Marshal(invalid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.DecodeStackProfile(raw); err == nil {
		t.Fatal("unsupported host-version fields accepted")
	}
}

func TestSwiftIOSProfileGuidanceAndMissingInputs(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../teams/source/swift-ios/stack-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := config.ResolveStackProfile(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.Effective.Requirements.OS, []string{"darwin"}) || len(p.Effective.Checks) != 2 || len(p.Effective.Setup) != 1 {
		t.Fatal("missing Swift host or recipes")
	}
	for _, c := range p.Effective.Checks {
		if c.CWD != "." || c.TimeoutSeconds != 3600 || !strings.Contains(c.Argv[2], "CODE_SIGNING_ALLOWED=NO") || !strings.Contains(c.Argv[2], "SWIFT_VERSION=6") {
			t.Fatal("unbounded or signed simulator recipe")
		}
		for _, tc := range []struct {
			inputs []string
			want   string
		}{
			{[]string{"", "", "", ""}, "project/workspace"},
			{[]string{"-project", "App.xcodeproj", "", ""}, "shared scheme"},
			{[]string{"-project", "App.xcodeproj", "App", ""}, "simulator destination"},
		} {
			argv := append(append([]string{}, c.Argv[1:4]...), tc.inputs...)
			cmd := exec.CommandContext(t.Context(), "sh", argv...)
			cmd.Dir = t.TempDir()
			out, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(out), tc.want) {
				t.Fatalf("missing inputs did not fail actionably: %s: %v", out, err)
			}
		}
	}
	if _, err := config.ResolveStackProfile(raw, []byte(`{"version":1,"checks":[]}`)); err == nil {
		t.Fatal("required checks cleared")
	}
	if _, err := config.ResolveStackProfile(raw, []byte(`{"version":1,"setup":[]}`)); err == nil {
		t.Fatal("non-overridable preflight cleared")
	}
}

func TestSwiftIOSToolchainPreflightWithOfflineHostFacts(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../teams/source/swift-ios/stack-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := config.DecodeStackProfile(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, osVersion, xcode, swift, sdk, runtime, developer string
		want                                                   string
	}{
		{"compatible", "15.6", "26.3", "6.2.3", "26.2", "iOS 26.2 (available)", "/Applications/Xcode.app/Contents/Developer", ""},
		{"macOS", "15.5", "26.3", "6.2.3", "26.2", "iOS 26.2 (available)", "/Applications/Xcode.app/Contents/Developer", "macOS"},
		{"Xcode", "15.6", "26.2", "6.2.3", "26.2", "iOS 26.2 (available)", "/Applications/Xcode.app/Contents/Developer", "Xcode 26.3"},
		{"compiler", "15.6", "26.3", "6.2.2", "26.2", "iOS 26.2 (available)", "/Applications/Xcode.app/Contents/Developer", "Swift compiler"},
		{"SDK", "15.6", "26.3", "6.2.3", "26.1", "iOS 26.2 (available)", "/Applications/Xcode.app/Contents/Developer", "SDK"},
		{"runtime", "15.6", "26.3", "6.2.3", "26.2", "iOS 26.2 (unavailable)", "/Applications/Xcode.app/Contents/Developer", "simulator runtime"},
		{"command line tools", "15.6", "26.3", "6.2.3", "26.2", "iOS 26.2 (available)", "/Library/Developer/CommandLineTools", "full Xcode"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin := t.TempDir()
			// Only fixtures execute. No installed Xcode or simulator is queried.
			fixtures := map[string]string{
				"uname":        "echo Darwin",
				"sw_vers":      "echo " + tc.osVersion,
				"xcode-select": "echo " + tc.developer,
				"xcodebuild":   "[ \"$1\" = -version ] || exit 91\nprintf 'Xcode " + tc.xcode + "\\nBuild version fixture\\n'",
				"xcrun": "case \"$*\" in\n" +
					"'--find xcodebuild') echo /Applications/Xcode.app/Contents/Developer/usr/bin/xcodebuild;;\n" +
					"'swift --version') echo 'Apple Swift version " + tc.swift + " (fixture)' ;;\n" +
					"'simctl list runtimes') echo '" + tc.runtime + "';;\n" +
					"*) echo " + tc.sdk + ";;\nesac",
			}
			for name, body := range fixtures {
				if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			c := p.Setup[0]
			cmd := exec.CommandContext(t.Context(), "sh", c.Argv[1:]...)
			cmd.Env = []string{"PATH=" + bin + ":/usr/bin:/bin"}
			out, err := cmd.CombinedOutput()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("compatible offline host facts refused: %s: %v", out, err)
				}
			} else if err == nil || !strings.Contains(string(out), tc.want) {
				t.Fatalf("incompatible host facts accepted: %s: %v", out, err)
			}
		})
	}
}
