# Implementation notes — run 0011

The owner authorized implementation of the five local approved stories. Their local bodies are authoritative; stale remote issue synchronization does not block this run. Starting checkout: `10b5bb26031b0e007a1c574fcd8e774128c5a2b7`.

## Progress

- US-1 central schema and source snapshots: implemented; review cycle 1 clear.
- US-2 central management and repository selection: implemented; review cycle 1 clear.
- US-3 capability gate: blocked by Codex app-server initialization permission error before inference. Runtime transport remains disabled/unimplemented pending evidence.
- US-4 focused workflows: deferred behind the runtime transport gate; converting bootstrap before delivery would break the coordinated cutover.
- US-5 provenance and benchmark: reproducible baseline inventory captured; runtime-dependent metadata and comparative benchmark deferred.
- Code review: CLEAN after cycle 1 resolved override dependency drift, conflicting cross-mode inline identity, absolute-path relocation, and root-only origin visibility. Independent acceptance test assessment complete: four additional cases passed.

## Decisions and boundaries

- Follow the approved dependency order, overlapping only independent capability investigation and baseline capture.
- The planned removal of the retired context updater overrides the stale copied orchestrator exit step; do not invoke or restore it.
- Work remains uncommitted for litefactory. No live runtime state or `dist/` writes.
- Paid CLI experiments are manual proof gates, distinct from normal automated tests. Unsupported or unproven transport cannot silently enable a full catalog fallback.

## Capability evidence

`RUNNER_CAPABILITIES.md` records three bounded authenticated Claude 2.1.263 probes ($0.037020 total) and a Codex 0.153.4 launch refused before inference with an app-server initialization permission error. No bypass was attempted. Claude demonstrated selected native-agent/plugin-skill injection with ambient fixture exclusion and explicit project-instruction append; delegated tier/authority and concurrency remain unproven. This does not satisfy AC-3.1 or vendor parity. `BASELINE_CATALOG.md` preserves the original commit/blob inventory.

## Implementation micro-decisions

- Keep the existing configuration tier vocabulary (`fast`, `balanced`, `flagship`).
- Extend the existing `team` write owner with narrowly validated central catalog batches sharing its atomic write primitive. Do not broaden legacy harness-owned path authorization to arbitrary paths.
- Replace obsolete repository-installation CLI fixtures with central-operation acceptance tests while preserving lower-level safe-write invariants; no compatibility shims.
- Explicitly assigned external-team runs fail before classifier/work CLI execution while runtime transport is unavailable. Existing unassigned behavior is retained. This is the approved fail-closed AC-3.5 boundary, not a claim that bundles are delivered.

## Verification evidence so far

Root verified the full `go test -race ./...` suite, `go build ./...`, and `go vet ./...` after review fixes. A subsequent boolean-equivalent lint simplification in `override.go` received a focused source race-test pass. Formatting and whitespace checks passed.

`make check` stops at the pinned linter, so it is not green. Starting HEAD archived into `/private/tmp/lf125-baseline.fyTHkQ` produced 33 pre-existing lint findings with the same pinned tool; the final tree has 28 inherited findings and no task-introduced findings. Scratch lint cache: `/private/tmp/lf125-lint.q3ebhA`. Unrelated schedule/log/turn/team warnings were not changed. The full race suite was run separately because `make check` stops before its test target.

Independent acceptance testing added mixed shared/inline roster order, unselected-mode invalid-source isolation, and transitive resource traversal/symlink-escape cases. All four passed with `go test -race -cover ./internal/team ./internal/teamsource`; coverage was 76.5% and 77.7% respectively. These were test-only additions after the full gates. `TEST_REPORT.md` records coverage and the unverified downstream criteria.
