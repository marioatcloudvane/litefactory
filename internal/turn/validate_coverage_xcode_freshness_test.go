package turn

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/coverage"
	"github.com/cloudvane/litefactory/internal/store"
)

// This fixture runs a real macOS XCTest bundle and the production xcresult
// observer. It needs a prepared full Xcode installation; Command Line Tools
// cannot supply XCTest execution or native result evidence.
func TestValidateFreshFullRequestsExecuteWarmXcodeBodies(t *testing.T) {
	if runtime.GOOS != "darwin" {
		nativeFreshnessUnavailable(t, "real xcodebuild freshness requires macOS with full Xcode")
	}
	version, err := exec.CommandContext(t.Context(), "/usr/bin/xcodebuild", "-version").CombinedOutput()
	if err != nil || !strings.HasPrefix(string(version), "Xcode ") {
		nativeFreshnessUnavailable(t, "full Xcode prerequisite unavailable: %s", strings.TrimSpace(string(version)))
	}
	fields := strings.Fields(string(version))
	major, err := strconv.Atoi(strings.Split(fields[1], ".")[0])
	if err != nil || major < 16 {
		nativeFreshnessUnavailable(t, "real xcresult observer requires Xcode 16 or later")
	}
	if output, e := exec.CommandContext(t.Context(), "/usr/bin/xcrun", "xcresulttool", "get", "test-results", "tests", "--help").CombinedOutput(); e != nil || !strings.Contains(string(output), "--path") {
		nativeFreshnessUnavailable(t, "installed Xcode lacks the required xcresult test-results schema")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	projectRoot := filepath.Join(root, "source")
	events := filepath.Join(root, "body-events")
	files := map[string]string{
		"Fixture.xcodeproj/project.pbxproj":                                xcodeFreshnessProject,
		"Fixture.xcodeproj/xcshareddata/xcschemes/FreshnessTests.xcscheme": xcodeFreshnessScheme,
		"BodyTests.m": fmt.Sprintf(`#import <XCTest/XCTest.h>
@interface BodyTests : XCTestCase
@end
@implementation BodyTests
- (void)testBody {
 NSString *path = @%s;
 NSData *event = [@"body\n" dataUsingEncoding:NSUTF8StringEncoding];
 if (![[NSFileManager defaultManager] fileExistsAtPath:path]) {
  XCTAssertTrue([[NSFileManager defaultManager] createFileAtPath:path contents:[NSData data] attributes:nil]);
 }
 NSFileHandle *file = [NSFileHandle fileHandleForWritingAtPath:path];
 XCTAssertNotNil(file);
 [file seekToEndOfFile];
 [file writeData:event];
 [file closeFile];
}
@end
`, strconv.Quote(events)),
	}
	for name, body := range files {
		target := filepath.Join(projectRoot, name)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	original := config.BugCheckCommand{Argv: []string{"/usr/bin/xcodebuild", "-project", "Fixture.xcodeproj", "-scheme", "FreshnessTests", "-destination", "platform=macOS", "-derivedDataPath", filepath.Join(root, "DerivedData"), "-parallel-testing-enabled", "NO", "CODE_SIGNING_ALLOWED=NO", "test"}, CWD: ".", TimeoutSeconds: 180}
	// Reusing DerivedData warms the native build cache. xcodebuild has no test
	// result cache to assert: every control invocation must execute the body.
	for i := 0; i < 2; i++ {
		command := exec.CommandContext(t.Context(), original.Argv[0], original.Argv[1:]...)
		command.Dir = projectRoot
		if output, e := command.CombinedOutput(); e != nil {
			t.Fatalf("native warm control %d: %v\n%s", i, e, output)
		}
		if got := freshnessBodyCount(t, events); got != i+1 {
			t.Fatalf("native control body events=%d want=%d", got, i+1)
		}
	}
	launcher := filepath.Join(root, "fixture-launcher")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\nwhile [ \"$#\" -gt 0 ] && [ \"$1\" != '--' ]; do shift; done\nshift\nexec \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	isolation := bugchecks.Isolation{Binary: launcher, PATH: os.Getenv("PATH"), Home: home, Revision: "native-xcode-freshness-fixture"}
	full, err := coverage.FullCommand(original, coverage.Facts{Runner: coverage.RunnerXcode, Version: strings.TrimSpace(string(version)), Structured: true, Dependencies: true, Fresh: true})
	if err != nil {
		t.Fatal(err)
	}
	recipe := []byte("version: 1\nphases:\n  regression:\n    commands:\n      - argv: [fixture-regression]\n        timeout_seconds: 30\n  focused:\n    commands:\n      - argv: [fixture-focused]\n        timeout_seconds: 30\n  full:\n    commands:\n      - argv: [xcodebuild, test]\n        timeout_seconds: 180\n")
	f := newSuiteFixture(t)
	f.b.Repo = freshnessRepo{freshnessScratch{projectRoot, recipe}}
	f.b.Run = func(ctx context.Context, path string, _ config.BugCheckCommand, started func(bugchecks.CommandResult) error) bugchecks.CommandResult {
		result := isolation.RunFresh(ctx, path, full, started)
		if result.Outcome == "passed" {
			observations, complete, parseErr := coverage.ParseResults(coverage.RunnerXcode, "native-xcode", []byte(result.Structured))
			if parseErr != nil || !complete || len(observations) != 1 || observations[0].Status != "pass" {
				t.Fatalf("real xcresult evidence incomplete: observations=%+v complete=%v err=%v", observations, complete, parseErr)
			}
		}
		return result
	}
	f.b.Coverage = &ValidateCoverageBindings{Conditions: "real-native-xcode-fixture", Discover: func(context.Context, ValidateSuiteScratch, []config.BugCheckCommand, coverage.DiscoveryRunner) (coverage.Inventory, map[int]coverage.Facts, error) {
		return coverage.Inventory{Complete: false, Reason: "native freshness control executes the unchanged complete project"}, nil, nil
	}}
	var requestID int64
	f.b.Bind = func(context.Context, Options, ActivityRequest) (ValidateSuiteAuthority, error) {
		return ValidateSuiteAuthority{Commit: suiteCommit, RequestID: requestID, Lineage: "native-xcode-freshness", Policy: "full", AssessmentVersion: coverage.Version}, nil
	}
	for i := 0; i < 2; i++ {
		request, e := f.db.EnsureValidationRequest(t.Context(), store.ValidationRequest{Lineage: "native-xcode-freshness", SourceKey: fmt.Sprintf("native-xcode-comment:%d", i), Policy: "full", Commit: suiteCommit})
		if e != nil {
			t.Fatal(e)
		}
		requestID = request.ID
		result, out, e := f.exec(int64(1800 + i))
		if e != nil || result.Outcome != "passed" {
			commands, _ := f.db.SuiteCommands(t.Context(), out.RunID)
			for _, command := range commands {
				if strings.Contains(command.Reason, "retain command identity: operation not permitted") {
					nativeFreshnessUnavailable(t, "host refuses process identity observation required by native execution")
				}
			}
			t.Fatalf("native full request %d: outcome=%s err=%v output=%+v commands=%+v", i, result.Outcome, e, out, commands)
		}
		expected := 3 + i
		if got := freshnessBodyCount(t, events); got != expected {
			t.Fatalf("native fresh request body events=%d want=%d", got, expected)
		}
		if _, _, e := f.exec(int64(1800 + i)); e != nil {
			t.Fatal(e)
		}
		if got := freshnessBodyCount(t, events); got != expected {
			t.Fatalf("native replay executed a body: events=%d want=%d", got, expected)
		}
	}
}

const xcodeFreshnessProject = `// !$*UTF8*$!
{
 archiveVersion = 1;
 classes = {};
 objectVersion = 56;
 objects = {
  000000000000000000000001 = {isa = PBXProject; attributes = {LastUpgradeCheck = 1600;}; buildConfigurationList = 000000000000000000000010; compatibilityVersion = "Xcode 14.0"; developmentRegion = en; knownRegions = (en, Base); mainGroup = 000000000000000000000002; productRefGroup = 000000000000000000000003; projectDirPath = ""; projectRoot = ""; targets = (000000000000000000000004);};
  000000000000000000000002 = {isa = PBXGroup; children = (000000000000000000000005, 000000000000000000000003, 000000000000000000000007); sourceTree = "<group>";};
  000000000000000000000003 = {isa = PBXGroup; children = (000000000000000000000006); name = Products; sourceTree = "<group>";};
  000000000000000000000004 = {isa = PBXNativeTarget; buildConfigurationList = 000000000000000000000011; buildPhases = (000000000000000000000008, 000000000000000000000009); buildRules = (); dependencies = (); name = FreshnessTests; productName = FreshnessTests; productReference = 000000000000000000000006; productType = "com.apple.product-type.bundle.unit-test";};
  000000000000000000000005 = {isa = PBXFileReference; lastKnownFileType = sourcecode.c.objc; path = BodyTests.m; sourceTree = "<group>";};
  000000000000000000000006 = {isa = PBXFileReference; explicitFileType = wrapper.cfbundle; includeInIndex = 0; path = FreshnessTests.xctest; sourceTree = BUILT_PRODUCTS_DIR;};
  000000000000000000000007 = {isa = PBXFileReference; lastKnownFileType = wrapper.framework; path = Platforms/MacOSX.platform/Developer/Library/Frameworks/XCTest.framework; sourceTree = DEVELOPER_DIR;};
  000000000000000000000008 = {isa = PBXSourcesBuildPhase; buildActionMask = 2147483647; files = (000000000000000000000012); runOnlyForDeploymentPostprocessing = 0;};
  000000000000000000000009 = {isa = PBXFrameworksBuildPhase; buildActionMask = 2147483647; files = (000000000000000000000013); runOnlyForDeploymentPostprocessing = 0;};
  000000000000000000000010 = {isa = XCConfigurationList; buildConfigurations = (000000000000000000000014); defaultConfigurationIsVisible = 0; defaultConfigurationName = Debug;};
  000000000000000000000011 = {isa = XCConfigurationList; buildConfigurations = (000000000000000000000015); defaultConfigurationIsVisible = 0; defaultConfigurationName = Debug;};
  000000000000000000000012 = {isa = PBXBuildFile; fileRef = 000000000000000000000005;};
  000000000000000000000013 = {isa = PBXBuildFile; fileRef = 000000000000000000000007;};
  000000000000000000000014 = {isa = XCBuildConfiguration; buildSettings = {CLANG_ENABLE_MODULES = YES; SDKROOT = macosx; MACOSX_DEPLOYMENT_TARGET = 13.0;}; name = Debug;};
  000000000000000000000015 = {isa = XCBuildConfiguration; buildSettings = {CODE_SIGNING_ALLOWED = NO; GENERATE_INFOPLIST_FILE = YES; PRODUCT_BUNDLE_IDENTIFIER = example.litefactory.FreshnessTests; PRODUCT_NAME = "$(TARGET_NAME)"; SDKROOT = macosx; SUPPORTED_PLATFORMS = macosx; MACOSX_DEPLOYMENT_TARGET = 13.0; CLANG_ENABLE_MODULES = YES; FRAMEWORK_SEARCH_PATHS = ("$(inherited)", "$(PLATFORM_DIR)/Developer/Library/Frameworks"); LD_RUNPATH_SEARCH_PATHS = ("$(inherited)", "@loader_path/../Frameworks", "@executable_path/../Frameworks", "$(PLATFORM_DIR)/Developer/Library/Frameworks");}; name = Debug;};
 };
 rootObject = 000000000000000000000001;
}
`
const xcodeFreshnessScheme = `<?xml version="1.0" encoding="UTF-8"?>
<Scheme LastUpgradeVersion="1600" version="1.3">
 <BuildAction parallelizeBuildables="NO" buildImplicitDependencies="YES"><BuildActionEntries><BuildActionEntry buildForTesting="YES" buildForRunning="NO" buildForProfiling="NO" buildForArchiving="NO" buildForAnalyzing="YES"><BuildableReference BuildableIdentifier="primary" BlueprintIdentifier="000000000000000000000004" BuildableName="FreshnessTests.xctest" BlueprintName="FreshnessTests" ReferencedContainer="container:Fixture.xcodeproj"/></BuildActionEntry></BuildActionEntries></BuildAction>
 <TestAction buildConfiguration="Debug" selectedDebuggerIdentifier="Xcode.DebuggerFoundation.Debugger.LLDB" selectedLauncherIdentifier="Xcode.IDEFoundation.Launcher.LLDB" shouldUseLaunchSchemeArgsEnv="YES"><Testables><TestableReference skipped="NO" parallelizable="NO"><BuildableReference BuildableIdentifier="primary" BlueprintIdentifier="000000000000000000000004" BuildableName="FreshnessTests.xctest" BlueprintName="FreshnessTests" ReferencedContainer="container:Fixture.xcodeproj"/></TestableReference></Testables></TestAction>
</Scheme>
`
