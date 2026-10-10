package teamrelease

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This shim records bounded recipes offline. It is not a .NET application build.
func TestDotnetWebRecipes(t *testing.T) {
	p := dotnetProfile(t)
	for _, tc := range []struct {
		name, phase, runner, locked, sdk, isTest, mtp string
		missing, unset, failed                        bool
		want, avoid                                   string
	}{
		{name: "unset inputs", phase: "restore", unset: true, want: "set explicit", avoid: "<restore>"},
		{name: "missing target", phase: "build", missing: true, want: "missing target", avoid: "<build>"},
		{name: "wrong SDK", phase: "restore", sdk: "9.0.100", want: "differs from", avoid: "<restore>"},
		{name: "unlocked restore", phase: "restore", want: "<restore><App.sln>", avoid: "<--locked-mode>"},
		{name: "locked restore", phase: "restore", locked: "true", want: "<restore><Tests.csproj><--locked-mode>"},
		{name: "analyzer build", phase: "build", want: "<--configuration><Release><--framework><net10.0><--no-restore><-p:RunAnalyzersDuringBuild=true>"},
		{name: "format verify", phase: "format", want: "<format><App.sln><--verify-no-changes><--no-restore>"},
		{name: "VSTest", phase: "test", want: "<test><Tests.csproj><--configuration><Release><--framework><net10.0><--no-build><--><RunConfiguration.TreatNoTestsAsError=true>"},
		{name: "MTP", phase: "test", runner: "Microsoft.Testing.Platform", mtp: "true", want: "<test><--project><Tests.csproj><--configuration><Release><--framework><net10.0><--no-build><--minimum-expected-tests><1>"},
		{name: "non test project", phase: "test", isTest: "false", want: "not marked IsTestProject", avoid: "<test>"},
		{name: "MTP mismatch", phase: "test", runner: "Microsoft.Testing.Platform", want: "requires an MTP", avoid: "<test>"},
		{name: "VSTest mismatch", phase: "test", mtp: "true", want: "cannot use the VSTest", avoid: "<test>"},
		{name: "zero tests failed", phase: "test", failed: true, want: "<RunConfiguration.TreatNoTestsAsError=true>"},
		{name: "unknown runner", phase: "test", runner: "invalid", want: "unsupported test runner", avoid: "<test>"},
		{name: "unknown lock policy", phase: "restore", locked: "invalid", want: "declare locked", avoid: "<restore>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "bin")
			if err := os.Mkdir(bin, 0700); err != nil {
				t.Fatal(err)
			}
			log := filepath.Join(dir, "calls")
			shim := `#!/bin/sh
printf '<%s>' "$@" >> "$RECORD"
printf '\n' >> "$RECORD"
case "$1" in
 --version) echo "$FAKE_SDK";;
 --list-runtimes) echo 'Microsoft.NETCore.App 10.0.12 [/prepared]'; echo 'Microsoft.AspNetCore.App 10.0.12 [/prepared]';;
 msbuild) case "$3" in -getProperty:IsTestProject) echo "$FAKE_TEST";; -getProperty:IsTestingPlatformApplication) echo "$FAKE_MTP";; esac;;
 test) if [ "$FAIL_TEST" = true ]; then exit 9; fi;;
esac
`
			if err := os.WriteFile(filepath.Join(bin, "dotnet"), []byte(shim), 0700); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"App.sln", "Tests.csproj", "global.json", "NuGet.Config"} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte("retained repository bytes"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.missing {
				if err := os.Remove(filepath.Join(dir, "App.sln")); err != nil {
					t.Fatal(err)
				}
			}
			var argv []string
			for _, c := range p.Effective.Checks {
				if c.Name == tc.phase {
					argv = append([]string(nil), c.Argv...)
				}
			}
			if len(argv) == 0 {
				t.Fatal("phase not released")
			}
			argv[5] = "App.sln"
			argv[6] = "Tests.csproj"
			if tc.unset {
				argv[5] = "__REQUIRED_TARGET__"
			}
			if tc.runner != "" {
				argv[7] = tc.runner
			}
			if tc.locked != "" {
				argv[8] = tc.locked
			}
			sdk := tc.sdk
			if sdk == "" {
				sdk = "10.0.401"
			}
			isTest := tc.isTest
			if isTest == "" {
				isTest = "true"
			}
			mtp := tc.mtp
			if mtp == "" {
				mtp = "false"
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "RECORD="+log, "FAKE_SDK="+sdk, "FAKE_TEST="+isTest, "FAKE_MTP="+mtp, "FAIL_TEST="+map[bool]string{true: "true", false: "false"}[tc.failed])
			out, err := cmd.CombinedOutput()
			calls, readErr := os.ReadFile(log)
			if readErr != nil && !os.IsNotExist(readErr) {
				t.Fatal(readErr)
			}
			failed := tc.unset || tc.missing || tc.sdk != "" || tc.isTest == "false" || tc.name == "MTP mismatch" || tc.name == "VSTest mismatch" || tc.failed || tc.runner == "invalid" || tc.locked == "invalid"
			if (err != nil) != failed {
				t.Fatalf("failure=%v wanted=%v: %s", err, failed, out)
			}
			evidence := string(out) + string(calls)
			if !strings.Contains(evidence, tc.want) {
				t.Fatalf("missing %q in %s", tc.want, evidence)
			}
			if tc.avoid != "" && strings.Contains(string(calls), tc.avoid) {
				t.Fatalf("forbidden execution: %s", calls)
			}
			for _, name := range []string{"global.json", "NuGet.Config"} {
				raw, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil || string(raw) != "retained repository bytes" {
					t.Fatal("recipe rewrote repository configuration", name)
				}
			}
		})
	}
}
