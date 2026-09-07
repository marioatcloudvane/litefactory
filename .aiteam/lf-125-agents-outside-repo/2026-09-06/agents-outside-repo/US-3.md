# US-3 — Post-classification private mode bundles

> As a coding-agent runner, I want only the selected team’s mode bundle after classification so that unrelated specialist instructions are not available to the run.

This gives each work run a private, minimal instruction set while retaining existing security controls.

## Why This Matters

Loading a broad specialist catalog can waste context and expose irrelevant workflow behavior. The difficult part is preserving vendor isolation and failing safely when an installed CLI cannot load the external bundle.

## Architecture Notes

- `turn` resolves `(stored repository team, selected mode)` after classification and before work launch through a consumer-owned minimal resolver interface.
- Extend the vendor-neutral request with an immutable resolved bundle; classifiers receive no such field or vendor arguments.
- `harness` and `codex` render only their own private external bundles and argv. Probe and version-pin the actual installed CLI mechanisms before finalizing argv.
- The bundle stays outside the worktree and is not a writable sandbox root; retain stdin prompts, Claude tools/env controls and Codex user-config isolation.
- The shipped Go roster keeps implementation/test authoring in Implement, review/evidence/drift in strict Validate, and excludes app-context updating from both.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|------------|-----|
| T1 | Add post-classification resolver wiring and immutable request bundle | implementation | US-1, US-2, US-4 | L |
| T2 | Perform version-pinned, read-only CLI bundle-loading probes | spike/contract | T1 | M |
| T3 | Implement Claude private-bundle rendering and controlled argv | implementation | T2 | M |
| T4 | Implement Codex isolated private-bundle rendering and controlled argv | implementation | T2 | M |
| T5 | Define strict Validate roster/verdict contract and bundle fixtures | implementation | T1 | M |
| T6 | Add runner/turn failure, isolation and no-worktree-write tests | test | T1–T5 | L |
| T7 | Record baseline-versus-bundle benchmark evidence | benchmark | T3–T6 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC1 | Classifier command construction contains no team lookup, roster, external specialist path or bundle argument. |
| AC2 | Work resolution loads exactly one selected team/mode roster; missing team/mode/source prevents CLI launch and names alternatives without fallback. |
| AC3 | No bundle content is copied into the task worktree’s agent/skill directories or made writable to the child. |
| AC4 | Vendor tests prove only the selected private bundle is passed through a version-probed mechanism while ambient user configuration remains controlled. |
| AC5 | Go Implement exposes only implementation/test-authoring roles; strict Validate is inline/read-only and excludes context updating. |
| AC6 | Benchmark records fixed prompt, vendor/version, team/mode, elapsed time and available token counters for baseline and selected-bundle runs without unsupported saving claims. |

