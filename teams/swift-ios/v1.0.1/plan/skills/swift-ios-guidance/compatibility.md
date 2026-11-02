# Swift iOS 1.0.0 compatibility

This is a pinned initial baseline, not a latest-version claim or live execution evidence.

| Setting | Required baseline |
| --- | --- |
| Xcode | 26.3, full Xcode application |
| Swift compiler | 6.2.3 bundled with Xcode 26.3 |
| Swift language mode | 6 |
| iOS device and simulator SDK | 26.2 |
| Simulator runtime | iOS 26.2, available and explicitly selected |
| iOS deployment targets | 17.0–26.2 (team policy) |
| macOS | Sequoia 15.6–Tahoe 26.x |

The compiler version, language mode, SDK, deployment target and simulator version are distinct settings. The deployment range is a team policy narrower than the toolchain range. Keep the repository's deployment target within 17.0–26.2; do not silently raise it. The recipes enforce Swift 6 language mode but do not inspect every target's deployment setting.

Baseline reference: [Apple Xcode requirements](https://developer.apple.com/xcode/system-requirements/). Other combinations need a later release decision.

## Operator preparation

Prepare full Xcode 26.3, its license/first-launch prerequisites and the iOS 26.2 simulator runtime before admitting engineering work. Command-line tools alone are insufficient. The selected developer directory must point to that full Xcode and agree with xcrun's resolved xcodebuild. Agents must not change global Xcode selection or set a different developer directory. Host software/runtime installation, signing credentials and physical-device/App Store distribution are outside these recipes.

The existing profile schema admits macOS, architectures and executable presence. It cannot encode toolchain version constraints as host requirements. Versions are encoded in the bounded read-only preflight recipes. Setup preflight is enforced only for eligible Implement workers; it does not establish version-aware admission for all six workflows. This release candidate therefore does not yet satisfy the approved all-workflow toolchain admission guarantee. Offline validation does not prove live macOS workflows. Live provider, Xcode and simulator execution remains deferred to a macOS host.

## Repository inputs and permission boundaries

The base simulator build/test recipes have empty repository inputs and fail with an actionable configuration error before Xcode executes. At the admitted revision, supply .litefactory/stack.overrides.json with the existing project or workspace, shared scheme and explicit simulator destination. Choose -project and a repository-relative .xcodeproj, or -workspace and a repository-relative .xcworkspace. The following example is illustrative: replace App and the simulator with this repository's actual shared scheme and prepared simulator. Retain both required build and test recipes, including their input and toolchain validation. A replaced check list is operator-reviewed configuration; the shared schema does not protect individual command contents against replacement.

Build/test output uses .litefactory/swift-ios/DerivedData inside the repository. Xcode and CoreSimulator may also require operator-admitted system/cache/runtime paths. This profile grants no writable roots or network access; do not expand them from team instructions. If required paths are unavailable, report a configuration failure rather than a passed or skipped check. Unsigned simulator checks set CODE_SIGNING_ALLOWED=NO. Record exact destination, toolchain, commands, results and simulator evidence.

```json
{
  "version": 1,
  "checks": [
    {
      "name": "simulator-build",
      "argv": [
        "sh",
        "-c",
        "set -eu\nfail() { printf '%s\\n' \"Swift iOS configuration: $*\" >&2; exit 2; }\n[ -n \"$1\" ] && [ -n \"$2\" ] || fail 'supply explicit -project/-workspace and repository-relative project/workspace path in .litefactory/stack.overrides.json'\n[ -n \"$3\" ] || fail 'supply the repository shared scheme in .litefactory/stack.overrides.json'\n[ -n \"$4\" ] || fail 'supply an explicit iOS 26.2 simulator destination in .litefactory/stack.overrides.json'\ncase \"$1:$2\" in -project:*.xcodeproj|-workspace:*.xcworkspace) ;; *) fail 'expected -project with .xcodeproj or -workspace with .xcworkspace';; esac\ncase \"$2\" in /*|../*|*/../*|*/..|..) fail 'project/workspace path must stay inside the repository';; esac\n[ -d \"$2\" ] || fail 'project/workspace directory is missing'\ncase \"$3\" in -*) fail 'shared scheme cannot begin with a dash';; esac\ncase \"$4\" in 'platform=iOS Simulator,'*'OS=26.2'*) ;; *) fail 'destination must select iOS Simulator and OS=26.2';; esac\nset -eu\nfail() { printf '%s\\n' \"Swift iOS configuration: $*\" >&2; exit 2; }\n[ \"$(uname -s)\" = Darwin ] || fail 'requires macOS 15.6\u201326.x'\ncase \"$(sw_vers -productVersion)\" in 15.[6-9]|15.[6-9].*|15.[1-9][0-9]*|26|26.*) ;; *) fail 'requires macOS 15.6\u201326.x';; esac\ndeveloper=$(xcode-select -p)\ncase \"$developer\" in */Xcode*.app/Contents/Developer) ;; *) fail 'requires full Xcode and matching selected developer directory';; esac\n[ \"$(xcrun --find xcodebuild)\" = \"$developer/usr/bin/xcodebuild\" ] || fail 'selected developer directory does not match xcrun xcodebuild'\ncase \"$(xcodebuild -version)\" in 'Xcode 26.3\n'*) ;; *) fail 'requires Xcode 26.3';; esac\ncase \"$(xcrun swift --version)\" in *'Swift version 6.2.3 ('*) ;; *) fail 'requires Swift compiler 6.2.3';; esac\n[ \"$(xcrun --sdk iphonesimulator --show-sdk-version)\" = 26.2 ] || fail 'requires iOS 26.2 simulator SDK'\n[ \"$(xcrun --sdk iphoneos --show-sdk-version)\" = 26.2 ] || fail 'requires iOS 26.2 device SDK'\nxcrun simctl list runtimes | grep 'iOS 26.2 ' | grep -v 'unavailable' >/dev/null || fail 'operator must prepare the iOS 26.2 simulator runtime'\nexec xcodebuild \"$1\" \"$2\" -scheme \"$3\" -destination \"$4\" -sdk iphonesimulator -derivedDataPath .litefactory/swift-ios/DerivedData CODE_SIGNING_ALLOWED=NO SWIFT_VERSION=6 build\n",
        "swift-ios",
        "-workspace",
        "App.xcworkspace",
        "App",
        "platform=iOS Simulator,name=iPhone 17,OS=26.2"
      ],
      "cwd": ".",
      "timeout_seconds": 3600
    },
    {
      "name": "simulator-test",
      "argv": [
        "sh",
        "-c",
        "set -eu\nfail() { printf '%s\\n' \"Swift iOS configuration: $*\" >&2; exit 2; }\n[ -n \"$1\" ] && [ -n \"$2\" ] || fail 'supply explicit -project/-workspace and repository-relative project/workspace path in .litefactory/stack.overrides.json'\n[ -n \"$3\" ] || fail 'supply the repository shared scheme in .litefactory/stack.overrides.json'\n[ -n \"$4\" ] || fail 'supply an explicit iOS 26.2 simulator destination in .litefactory/stack.overrides.json'\ncase \"$1:$2\" in -project:*.xcodeproj|-workspace:*.xcworkspace) ;; *) fail 'expected -project with .xcodeproj or -workspace with .xcworkspace';; esac\ncase \"$2\" in /*|../*|*/../*|*/..|..) fail 'project/workspace path must stay inside the repository';; esac\n[ -d \"$2\" ] || fail 'project/workspace directory is missing'\ncase \"$3\" in -*) fail 'shared scheme cannot begin with a dash';; esac\ncase \"$4\" in 'platform=iOS Simulator,'*'OS=26.2'*) ;; *) fail 'destination must select iOS Simulator and OS=26.2';; esac\nset -eu\nfail() { printf '%s\\n' \"Swift iOS configuration: $*\" >&2; exit 2; }\n[ \"$(uname -s)\" = Darwin ] || fail 'requires macOS 15.6\u201326.x'\ncase \"$(sw_vers -productVersion)\" in 15.[6-9]|15.[6-9].*|15.[1-9][0-9]*|26|26.*) ;; *) fail 'requires macOS 15.6\u201326.x';; esac\ndeveloper=$(xcode-select -p)\ncase \"$developer\" in */Xcode*.app/Contents/Developer) ;; *) fail 'requires full Xcode and matching selected developer directory';; esac\n[ \"$(xcrun --find xcodebuild)\" = \"$developer/usr/bin/xcodebuild\" ] || fail 'selected developer directory does not match xcrun xcodebuild'\ncase \"$(xcodebuild -version)\" in 'Xcode 26.3\n'*) ;; *) fail 'requires Xcode 26.3';; esac\ncase \"$(xcrun swift --version)\" in *'Swift version 6.2.3 ('*) ;; *) fail 'requires Swift compiler 6.2.3';; esac\n[ \"$(xcrun --sdk iphonesimulator --show-sdk-version)\" = 26.2 ] || fail 'requires iOS 26.2 simulator SDK'\n[ \"$(xcrun --sdk iphoneos --show-sdk-version)\" = 26.2 ] || fail 'requires iOS 26.2 device SDK'\nxcrun simctl list runtimes | grep 'iOS 26.2 ' | grep -v 'unavailable' >/dev/null || fail 'operator must prepare the iOS 26.2 simulator runtime'\nexec xcodebuild \"$1\" \"$2\" -scheme \"$3\" -destination \"$4\" -sdk iphonesimulator -derivedDataPath .litefactory/swift-ios/DerivedData CODE_SIGNING_ALLOWED=NO SWIFT_VERSION=6 test\n",
        "swift-ios",
        "-workspace",
        "App.xcworkspace",
        "App",
        "platform=iOS Simulator,name=iPhone 17,OS=26.2"
      ],
      "cwd": ".",
      "timeout_seconds": 3600
    }
  ]
}
```

Standalone `swift test` is limited to explicitly selected host-compatible Swift Package Manager targets, with repository-relative working directory and bounded timeout. It is optional additional evidence, never a substitute for iOS simulator build/test or iOS-only package validation. Use existing XCTest/Swift Testing suites and preserve Package.resolved. Bug and Spike retain their already pinned recipes.

## Offline regeneration

Run `go run ./cmd/teamrelease -source internal/teamsource/swift-ios` to generate this factory package, and append `-check` for reproducibility verification. These are factory maintainer packaging commands; Swift application workers use the captured Swift recipes. Existing release bytes and catalogue entries remain immutable. Package validation establishes inventory, graph, resource and checksum consistency only.
