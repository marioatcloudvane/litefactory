# Offline Swift package engineering evidence — run 0006

This is package/contract evidence only. No Claude/Codex workflow, installed Xcode, iOS application build or simulator test was executed. The host-fact fixtures execute temporary shell tools with recorded output.

Implemented canonical source: internal/teamsource/swift-ios. Generated candidate: teams/swift-ios/v1.0.0. The catalogue adds exactly one release and retains every pre-existing entry unchanged. No prior release files changed. The candidate inventory contains 61 resources, including all six graphs, role/result closures, stack-profile.json, Swift guidance and compatibility.md.

Executed and passed:

- Focused Swift tests: go test ./internal/teamrelease -run SwiftIOS -count=1 (after correcting the new test filename so it was not excluded by the iOS platform suffix).
- Complete affected package run: go test -race -timeout=0 ./internal/teamrelease ./cmd/teamrelease; teamrelease passed in 22.078 seconds, command package has no tests. Output is retained in package-tests.log.
- go run ./cmd/teamrelease -source internal/teamsource/swift-ios -check: exit 0, no drift.
- go build ./...: exit 0.
- gofmt -l internal/teamrelease/swift_release_test.go and git diff --check: no findings.
- Pinned make lint for internal/teamrelease and cmd/teamrelease: exit 0, zero issues. Repeated with GOLANGCI_LINT_CACHE=/tmp/lf568-golangci-cache after the initial successful run emitted sandbox cache-write warnings. Clean output is retained in lint.log.

Tests cover exact Swift team/version installation through the shared installer, reproducibility, unchanged prior release bytes and catalogue entries, corrupted inventory rejection, malformed profile, missing guidance resource, unknown workflow activities, required-check clearing, empty project/scheme/simulator inputs and bounded unsigned recipes. Offline host-fact tests cover compatible baseline and incompatible macOS, Xcode, compiler, SDK/runtime and command-line-tools-only facts at recipe preflight.

Guidance review: plan/skills/swift-ios-guidance/SKILL.md covers Swift 6 concurrency/isolation, existing SwiftUI/UIKit, Swift Package Manager, Package.resolved, existing XCTest/Swift Testing, unsigned simulator evidence and repository architecture. Its compatibility.md records the pinned compiler/language-mode/SDK/runtime/deployment/macOS baseline, selected full-Xcode preparation, explicit inputs and operator permission boundaries. Shared team-guidance/boundaries.md preserves owner gates, graph ownership, result contracts and failure/cancellation honesty. Graphs differ from the prerequisite source only in team identity; registered activities and positive bounds remain. Swift role copies replace Go-only engineering commands and examples. No shared factory capability changed.

Unfinished approved requirements: shared version-aware admission across all workflows is unavailable; the candidate explicitly documents this and only offers bounded engineering preflight. Deployment target range remains a documented repository policy rather than target-by-target machine validation. Offline Claude/Codex/check adapter capture and full provider-profile sameness evidence remain outstanding. These checks do not establish completion of every approved criterion. Recommend a separately authorized shared admission repair, then complete the provider capture fixtures and regenerate this unpublished candidate. Alternatively, an explicit approved scope revision could accept the narrower engineering-preflight guarantee.

No subagents were started under the current delegation restriction. No commits, pushes, merges or publication were performed.
