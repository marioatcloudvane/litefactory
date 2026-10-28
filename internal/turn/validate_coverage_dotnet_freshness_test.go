package turn

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/coverage"
	"github.com/cloudvane/litefactory/internal/store"
)

// These fixtures run actual dotnet test applications. The launcher only adapts
// the executor's sandbox argv in the injected runner seam; it is not isolation
// evidence. Native SDKs and all NuGet dependencies must already be installed.
func TestValidateVSTestNativeFullRequestFreshness(t *testing.T) {
	nativeDotnetFullRequestFreshness(t, coverage.RunnerVSTest)
}

func TestValidateMTPNativeFullRequestFreshness(t *testing.T) {
	nativeDotnetFullRequestFreshness(t, coverage.RunnerMTP)
}

func nativeDotnetFullRequestFreshness(t *testing.T, runner string) {
	t.Helper()
	dotnet, err := exec.LookPath("dotnet")
	if err != nil {
		nativeFreshnessUnavailable(t, "native .NET freshness unavailable: dotnet SDK is not installed")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	module := filepath.Join(root, "project")
	feed := filepath.Join(root, "offline-feed")
	for _, dir := range []string{module, feed, filepath.Join(root, "cli-home")} {
		if err = os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	packages := os.Getenv("NUGET_PACKAGES")
	if packages == "" {
		home, e := os.UserHomeDir()
		if e != nil {
			t.Fatal(e)
		}
		packages = filepath.Join(home, ".nuget", "packages")
	}
	packages, err = filepath.Abs(packages)
	if err != nil {
		t.Fatal(err)
	}
	settings := map[string]string{
		"BODY_EVENTS":                               filepath.Join(root, "native-body-events"),
		"DOTNET_CLI_HOME":                           filepath.Join(root, "cli-home"),
		"DOTNET_CLI_TELEMETRY_OPTOUT":               "true",
		"DOTNET_GENERATE_ASPNET_CERTIFICATE":        "false",
		"DOTNET_SKIP_FIRST_TIME_EXPERIENCE":         "true",
		"DOTNET_CLI_WORKLOAD_UPDATE_NOTIFY_DISABLE": "true",
		"NUGET_PACKAGES":                            packages,
	}
	env := os.Environ()
	names := make([]string, 0, len(settings))
	for key, value := range settings {
		names = append(names, key)
		env = append(env, key+"="+value)
	}
	sort.Strings(names)
	sdkOutput, err := nativeDotnetCommand(t, module, env, dotnet, "--list-sdks")
	if err != nil {
		nativeFreshnessUnavailable(t, "native .NET freshness unavailable: SDK enumeration failed: %s", sdkOutput)
	}
	minimumMajor := 8
	if runner == coverage.RunnerMTP {
		minimumMajor = 10
	}
	sdk := nativeDotnetSDK(sdkOutput, minimumMajor)
	if sdk == "" {
		nativeFreshnessUnavailable(t, "native %s freshness requires an installed stable .NET SDK %d or later", runner, minimumMajor)
	}
	major, _ := strconv.Atoi(strings.Split(sdk, ".")[0])
	framework := fmt.Sprintf("net%d.0", major)
	global := map[string]any{"sdk": map[string]string{"version": sdk, "rollForward": "disable"}}
	if major >= 10 {
		name := "VSTest"
		if runner == coverage.RunnerMTP {
			name = "Microsoft.Testing.Platform"
		}
		global["test"] = map[string]string{"runner": name}
	}
	var project string
	if runner == coverage.RunnerVSTest {
		mstest := nativeCachedMSTestPair(t, packages)
		testSDK := nativeCachedNuGet(t, packages, "microsoft.net.test.sdk", 17)
		project = fmt.Sprintf(`<Project Sdk="Microsoft.NET.Sdk"><PropertyGroup><TargetFramework>%s</TargetFramework><IsTestProject>true</IsTestProject><IsPackable>false</IsPackable><EnableMSTestRunner>false</EnableMSTestRunner><NuGetAudit>false</NuGetAudit></PropertyGroup><ItemGroup><PackageReference Include="Microsoft.NET.Test.Sdk" Version="%s"/><PackageReference Include="MSTest.TestFramework" Version="%s"/><PackageReference Include="MSTest.TestAdapter" Version="%s"/></ItemGroup></Project>`, framework, testSDK, mstest, mstest)
	} else {
		mstestSDK := nativeCachedNuGet(t, packages, "mstest.sdk", 4)
		// MSTest.Sdk's None profile plus this extension avoids enabling unrelated
		// code coverage/reporters. Its pinned dependency versions must be cached.
		_ = nativeCachedNuGet(t, packages, "microsoft.testing.extensions.trxreport", 1)
		project = fmt.Sprintf(`<Project Sdk="MSTest.Sdk/%s"><PropertyGroup><TargetFramework>%s</TargetFramework><TestingExtensionsProfile>None</TestingExtensionsProfile><EnableMicrosoftTestingExtensionsTrxReport>true</EnableMicrosoftTestingExtensionsTrxReport><NuGetAudit>false</NuGetAudit></PropertyGroup></Project>`, mstestSDK, framework)
	}
	files := map[string][]byte{
		"NativeFreshness.csproj": []byte(project),
		"global.json":            mustNativeDotnetJSON(t, global),
		"NuGet.Config":           []byte(`<configuration><packageSources><clear/><add key="offline-fixture" value="` + nativeDotnetXML(t, feed) + `"/></packageSources></configuration>`),
		"BodyTests.cs": []byte(`using System;
using System.IO;
using Microsoft.VisualStudio.TestTools.UnitTesting;

[TestClass]
public sealed class NativeFreshness
{
    [TestMethod]
    public void ExecutesBody()
    {
        var path = Environment.GetEnvironmentVariable("BODY_EVENTS");
        Assert.IsFalse(String.IsNullOrEmpty(path));
        File.AppendAllText(path, "body\n");
    }
}
`),
	}
	for name, body := range files {
		if err = os.WriteFile(filepath.Join(module, name), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	output, err := nativeDotnetCommand(t, module, env, dotnet, "restore", "NativeFreshness.csproj", "--configfile", filepath.Join(module, "NuGet.Config"), "--disable-parallel", "--ignore-failed-sources", "-p:NuGetAudit=false")
	if err != nil {
		for _, code := range []string{"NU1100", "NU1101", "NU1102", "NU1301", "MSB4236", "NU1605", "NU1202"} {
			if strings.Contains(output, code) {
				nativeFreshnessUnavailable(t, "native %s freshness unavailable: cached SDK/package dependencies cannot restore from the empty local feed (%s)", runner, code)
			}
		}
		t.Fatalf("offline native fixture restore failed: %v\n%s", err, output)
	}
	// This launcher never simulates dotnet or its reports. Every test body and TRX
	// artifact below comes from the installed native framework and runner.
	launcher := filepath.Join(root, "native-launcher-fixture")
	if err = os.WriteFile(launcher, []byte("#!/bin/sh\nwhile [ \"$#\" -gt 0 ] && [ \"$1\" != '--' ]; do shift; done\nshift\nexec \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	argv := []string{"dotnet", "test", "NativeFreshness.csproj", "--no-restore"}
	if runner == coverage.RunnerMTP {
		argv = []string{"dotnet", "test", "--project", "NativeFreshness.csproj", "--no-restore"}
	}
	command := config.BugCheckCommand{Argv: argv, CWD: ".", Env: names, TimeoutSeconds: 120}
	facts := coverage.Facts{Runner: runner, Version: sdk, Structured: true, Dependencies: true, Fresh: true, Framework: "MSTest", FilterFlag: "--filter", FilterProperty: "FullyQualifiedName"}
	full, err := coverage.FullCommand(command, facts)
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	isolation := bugchecks.Isolation{Binary: launcher, PATH: filepath.Dir(dotnet) + string(os.PathListSeparator) + os.Getenv("PATH"), Home: home, Environment: settings, WritableRoots: []string{root}, Revision: "native-dotnet-fixture-v1"}
	execute := func(ctx context.Context, path string, c config.BugCheckCommand, started func(bugchecks.CommandResult) error) bugchecks.CommandResult {
		result := isolation.RunFresh(ctx, path, c, started)
		if result.Outcome == "blocked" && strings.Contains(result.Reason, "retain command identity: operation not permitted") {
			nativeFreshnessUnavailable(t, "native .NET executor prerequisite unavailable: host refuses process identity observation")
		}
		return result
	}
	// Warm the real build outputs and runner, then prove it executed a body. No
	// cache directory is cleared between requests.
	warm := execute(t.Context(), module, full, func(bugchecks.CommandResult) error { return nil })
	if warm.Outcome != "passed" {
		t.Fatalf("native %s warm run failed: %+v", runner, warm)
	}
	observed, complete, err := coverage.ParseResults(runner, "native-dotnet", []byte(warm.Structured))
	if err != nil || !complete || len(observed) != 1 || observed[0].Status != "pass" {
		t.Fatalf("native %s TRX observation incomplete: complete=%v err=%v observations=%+v", runner, complete, err, observed)
	}
	if got := nativeDotnetBodyCount(t, settings["BODY_EVENTS"]); got != 1 {
		t.Fatalf("warm native test bodies=%d want=1", got)
	}
	recipe := []byte("version: 1\nphases:\n  regression:\n    commands:\n      - argv: [fixture-regression]\n        timeout_seconds: 30\n  focused:\n    commands:\n      - argv: [fixture-focused]\n        timeout_seconds: 30\n  full:\n    commands:\n      - argv: " + string(mustNativeDotnetJSON(t, argv)) + "\n        cwd: .\n        env: " + string(mustNativeDotnetJSON(t, names)) + "\n        timeout_seconds: 120\n")
	f := newSuiteFixture(t)
	f.b.Repo = freshnessRepo{freshnessScratch{module, recipe}}
	f.b.Run = execute
	f.b.Coverage = &ValidateCoverageBindings{Conditions: "native-" + runner + "/" + sdk, Discover: func(context.Context, ValidateSuiteScratch, []config.BugCheckCommand, coverage.DiscoveryRunner) (coverage.Inventory, map[int]coverage.Facts, error) {
		return coverage.Inventory{Commit: suiteCommit, Complete: true, Units: []coverage.Unit{{Key: "native-dotnet", Command: 0, Runner: runner, Selector: "NativeFreshness.csproj", Files: []string{"BodyTests.cs"}}}}, map[int]coverage.Facts{0: facts}, nil
	}}
	requestID := int64(0)
	f.b.Bind = func(context.Context, Options, ActivityRequest) (ValidateSuiteAuthority, error) {
		return ValidateSuiteAuthority{Commit: suiteCommit, RequestID: requestID, Lineage: "native-" + runner, Policy: "full", AssessmentVersion: coverage.Version}, nil
	}
	var firstRun int64
	for i := 0; i < 2; i++ {
		request, err := f.db.EnsureValidationRequest(t.Context(), store.ValidationRequest{Lineage: "native-" + runner, SourceKey: fmt.Sprintf("native-full-comment:%d", i), Policy: "full", Commit: suiteCommit})
		if err != nil {
			t.Fatal(err)
		}
		requestID = request.ID
		result, out, err := f.exec(int64(980 + i))
		if err != nil || result.Outcome != "passed" {
			t.Fatalf("native %s full request %d failed: outcome=%s err=%v output=%+v", runner, i, result.Outcome, err, out)
		}
		if i == 0 {
			firstRun = out.RunID
		} else if out.RunID == firstRun {
			t.Fatal("distinct full request reused the prior suite run")
		}
		want := i + 2
		if got := nativeDotnetBodyCount(t, settings["BODY_EVENTS"]); got != want {
			t.Fatalf("native %s full request %d body count=%d want=%d", runner, i, got, want)
		}
		_, replay, err := f.exec(int64(980 + i))
		if err != nil || replay.RunID != out.RunID {
			t.Fatalf("native request replay changed run: %+v %v", replay, err)
		}
		if got := nativeDotnetBodyCount(t, settings["BODY_EVENTS"]); got != want {
			t.Fatalf("native %s replay executed a test body: count=%d want=%d", runner, got, want)
		}
	}
	for name, expected := range files {
		actual, e := os.ReadFile(filepath.Join(module, name))
		if e != nil || !bytes.Equal(actual, expected) {
			t.Fatalf("native fixture source %s changed: %v", name, e)
		}
	}
}

func nativeDotnetCommand(t *testing.T, dir string, env []string, binary string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, binary, args...)
	command.Dir = dir
	command.Env = env
	out, err := command.CombinedOutput()
	return string(out), err
}
func nativeDotnetSDK(output string, minimumMajor int) string {
	versions := []string{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.Contains(fields[0], "-") {
			continue
		}
		major, err := strconv.Atoi(strings.Split(fields[0], ".")[0])
		if err == nil && major >= minimumMajor {
			versions = append(versions, fields[0])
		}
	}
	sort.Slice(versions, func(i, j int) bool { return nativeDotnetVersionLess(versions[j], versions[i]) })
	if len(versions) == 0 {
		return ""
	}
	return versions[0]
}
func nativeDotnetVersionLess(a, b string) bool {
	aa, bb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < max(len(aa), len(bb)); i++ {
		av, bv := 0, 0
		if i < len(aa) {
			av, _ = strconv.Atoi(aa[i])
		}
		if i < len(bb) {
			bv, _ = strconv.Atoi(bb[i])
		}
		if av != bv {
			return av < bv
		}
	}
	return false
}
func nativeCachedNuGet(t *testing.T, root, id string, minimumMajor int) string {
	t.Helper()
	versions := nativeCachedNuGetVersions(root, id, minimumMajor)
	if len(versions) == 0 {
		nativeFreshnessUnavailable(t, "native .NET freshness unavailable: no cached stable %s package version %d or later", id, minimumMajor)
	}
	return versions[0]
}
func nativeCachedMSTestPair(t *testing.T, root string) string {
	t.Helper()
	framework := nativeCachedNuGetVersions(root, "mstest.testframework", 3)
	adapter := nativeCachedNuGetVersions(root, "mstest.testadapter", 3)
	for _, version := range framework {
		if slices.Contains(adapter, version) {
			return version
		}
	}
	nativeFreshnessUnavailable(t, "native VSTest freshness unavailable: matching cached MSTest framework and adapter packages are missing")
	return ""
}
func nativeCachedNuGetVersions(root, id string, minimumMajor int) []string {
	entries, err := os.ReadDir(filepath.Join(root, id))
	if err != nil {
		return nil
	}
	versions := []string{}
	for _, entry := range entries {
		version := entry.Name()
		if !entry.IsDir() || strings.Contains(version, "-") {
			continue
		}
		major, err := strconv.Atoi(strings.Split(version, ".")[0])
		if err != nil || major < minimumMajor {
			continue
		}
		if info, err := os.Stat(filepath.Join(root, id, version, id+".nuspec")); err != nil || !info.Mode().IsRegular() {
			continue
		}
		versions = append(versions, version)
	}
	sort.Slice(versions, func(i, j int) bool { return nativeDotnetVersionLess(versions[j], versions[i]) })
	return versions
}
func nativeDotnetBodyCount(t *testing.T, path string) int {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(body), "body\n")
}
func mustNativeDotnetJSON(t *testing.T, value any) []byte {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
func nativeDotnetXML(t *testing.T, value string) string {
	t.Helper()
	var b bytes.Buffer
	if err := xml.EscapeText(&b, []byte(value)); err != nil {
		t.Fatal(err)
	}
	return b.String()
}
