package teamrelease

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
	"github.com/cloudvane/litefactory/internal/turn"
)

const swiftSource = "teams/source/swift-ios"

func swiftFixture(t *testing.T) string {
	t.Helper()
	root := fixture(t)
	for name, raw := range snapshot(t, "../../teams/source/base-team") {
		put(t, filepath.Join(root, "teams/source/base-team", filepath.FromSlash(name)), raw)
	}
	for name, raw := range snapshot(t, "../../teams/source/swift-ios") {
		put(t, filepath.Join(root, swiftSource, filepath.FromSlash(name)), raw)
	}
	return root
}

// swiftRelease is the published directory of the current swift-ios release.
func swiftRelease(t *testing.T) string {
	t.Helper()
	return "../../teams/swift-ios/v" + sourceVersion(t, "../../teams/source/swift-ios")
}

// These tests prove offline package contracts, never live Apple execution.
func TestSwiftIOSReleaseSelectionAndReproducibility(t *testing.T) {
	t.Parallel()
	root := swiftFixture(t)
	generate(t, root)
	before := snapshot(t, filepath.Join(root, "teams"))
	prior := releaseCatalogue(t, root)
	if err := GenerateSources(t.Context(), root, false, []string{swiftSource}); err != nil {
		t.Fatal(err)
	}
	for name, raw := range before {
		if name == "teams.json" {
			continue
		}
		got, err := os.ReadFile(filepath.Join(root, "teams", filepath.FromSlash(name)))
		if err != nil || !bytes.Equal(got, raw) {
			t.Fatalf("prior release changed: %s: %v", name, err)
		}
	}
	catalogue := releaseCatalogue(t, root)
	if len(catalogue.Releases) != len(prior.Releases)+1 {
		t.Fatal("catalogue did not retain prior entries")
	}
	for _, old := range prior.Releases {
		found := false
		for _, got := range catalogue.Releases {
			found = found || reflect.DeepEqual(old, got)
		}
		if !found {
			t.Fatal("prior catalogue entry changed")
		}
	}
	manager, err := teampackage.New(teampackage.Options{Paths: config.NewPaths(canonicalTemp(t)), Validate: turn.ValidateTeamWorkflows})
	if err != nil {
		t.Fatal(err)
	}
	version := sourceVersion(t, "../../teams/source/swift-ios")
	installed, _, err := manager.Install(t.Context(), filepath.Join(root, "teams/teams.json"), "swift-ios", version)
	if err != nil || installed.Identity.Slug != "swift-ios" || installed.Identity.Version != version {
		t.Fatal("exact Swift selection failed", err)
	}
	all := snapshot(t, filepath.Join(root, "teams"))
	if err := GenerateSources(t.Context(), root, true, []string{swiftSource}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(all, snapshot(t, filepath.Join(root, "teams"))) {
		t.Fatal("check mode wrote files")
	}
	put(t, filepath.Join(root, "teams/swift-ios/v"+version+"/stack-profile.json"), []byte(`{"version":99}`))
	manager, err = teampackage.New(teampackage.Options{Paths: config.NewPaths(canonicalTemp(t)), Validate: turn.ValidateTeamWorkflows})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.Install(t.Context(), filepath.Join(root, "teams/teams.json"), "swift-ios", version); err == nil {
		t.Fatal("checksum corruption accepted")
	}
}

func TestSwiftIOSProfileGuidanceAndMissingInputs(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(swiftRelease(t) + "/stack-profile.json")
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

func TestSwiftIOSInvalidSourceRejected(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		edit func(*testing.T, string)
	}{
		{"malformed profile", func(t *testing.T, root string) {
			put(t, filepath.Join(root, swiftSource, "stack-profile.json"), []byte(`{"version":99}`))
		}},
		{"missing guidance", func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, swiftSource, "plan/skills/swift-ios-guidance/compatibility.md")); err != nil {
				t.Fatal(err)
			}
		}},
		{"unsupported workflow", func(t *testing.T, root string) {
			// Swift inherits its graphs; break the base graph its build resolves.
			name := filepath.Join(root, "teams/source/base-team", "implement/implement.dot")
			raw, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			put(t, name, bytes.ReplaceAll(raw, []byte("lf_implement_"), []byte("lf_unsupported_")))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := swiftFixture(t)
			tc.edit(t, root)
			if err := GenerateSources(t.Context(), root, false, []string{swiftSource}); err == nil {
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

func TestSwiftIOSToolchainPreflightWithOfflineHostFacts(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(swiftRelease(t) + "/stack-profile.json")
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
