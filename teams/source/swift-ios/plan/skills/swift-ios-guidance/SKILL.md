---
name: swift-ios-guidance
description: Swift 6 iOS planning, implementation and review guidance.
resources:
  - compatibility.md
---
# Swift iOS guidance

Use the existing SwiftUI or UIKit structure and repository architecture. Do not impose a UI migration, dependency framework or new app architecture. Respect all shared workflow gates and the admitted repository revision.

Use Swift 6 language mode with the pinned Swift 6.2.3 compiler. Review concurrency, actor isolation, MainActor boundaries, Sendable values and cancellation. Preserve existing isolation instead of suppressing diagnostics or introducing unchecked conformance without evidence.

Use Swift Package Manager as the repository already does. Preserve its pinned Package.resolved and dependency policy. Resolve dependencies only within admitted network and filesystem permissions. Standalone swift test applies only to explicitly selected host-compatible package targets; it cannot prove an iOS application or package target requiring an iOS simulator.

Extend the existing XCTest or Swift Testing suites. iOS application and iOS-only package checks use the existing project/workspace, shared scheme and explicit iOS 26.2 simulator destination. Keep test scope appropriate to the story, record build/test output and simulator evidence, and retain failures and cancellation. Use unsigned simulator checks with CODE_SIGNING_ALLOWED=NO; signing, physical devices and App Store distribution are separate operator work.

Read compatibility.md for the pinned baseline and repository override example. Missing project/workspace, scheme or simulator inputs fail before Xcode checks. The base recipes intentionally contain empty repository inputs; operators supply explicit bounded replacements at the admitted repository revision. Do not infer a universal app scheme. Bug and Spike keep existing pinned recipes.

Use the repository’s pinned formatter/linter when present. Do not prescribe Go checks or Go race flags. Do not change global Xcode selection, install host software, import credentials or expand writable roots. A checksummed profile identifies content and grants no permission.

Package validation is offline package/contract evidence. Live macOS workflow execution is deferred and must never be claimed from these checks.
