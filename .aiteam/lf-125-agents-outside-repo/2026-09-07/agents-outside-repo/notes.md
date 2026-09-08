# Implementation notes — run 0015

## Current run decisions and progress

- Owner authorizes implementing the approved remote-distribution/delivery solution with run 0014's default URL correction. `teams.index_url` defaults to the literal `https://github.com/marioatcloudvane/litefactory/teams/teams.json`; explicitly configured aliases may select corporate sources. This supersedes run 0013's unset-default clauses, not its integrity/offline boundaries.
- Treat the owner's GitHub URL as a repository-file locator: explicitly normalize the supported shorthand to raw GitHub content on `HEAD` (the default branch) before enforcing origin/root containment. Retain logical source identity; do not permit arbitrary cross-origin redirects or treat HTML as JSON. Publishing deployment is still not authorized or claimed by local code changes.
- US-1 contracts/configuration: implemented; focused existing tests/vet pass, independent acceptance tests pending.
- US-2 remote acquisition/central management: implementation reported complete, including explicit source switching; independent review/tests pending.
- US-3 CLI bundle delivery/conversations: implementation reported complete; existing focused suites and resume regression pass, new acceptance tests and real CLI evidence pending.
- US-4 focused content: canonical teams/source prepared with exact rosters and reusable skills; production embed removed, historical bytes preserved as test fixtures; selected runtime bootstrap supplied by delivery.
- US-5 provenance/evidence/docs: durable inline delivery.evidence, operator docs and reproducible byte benchmark implemented. Actual billed-token/latency/delegation evidence is explicitly unmeasured.
- US-6 deterministic independent publisher: generated teams/teams.json and immutable go-team/1.0.0 release; independent manual artifact workflow supplied. Actual hosted publication not performed.
- Independent code review cycle 1: CR-B-004 alias/local-directory collision resolved; re-review code verdict CLEAN. Test bug cycle 1 fixed BT-001 (override evidence omitted from identity); its regression and narrow re-review pass. CR-B-005 remains incomplete release acceptance: no real vendor delegation/isolation or comparative token evidence was collected. Production implementation and deterministic tests do not satisfy AC-3.7 or measured portions of AC-5.2; full acceptance is not marked done.
- Final verification, 2026-09-08: all 25 new scoped test functions pass; the entire `GOMAXPROCS=2 go test -race -p 2 ./...` suite passes. Final serial make check passes formatting/vet and stops on 41 lint findings in files unchanged from starting HEAD, verified by reviewer. No introduced lint issues remain. Test/drift reports are complete in the original session directory.
- External environment suites skipped: TEST_BASE_URL and TEST_DATABASE_URL are both unset. Approved ordinary Go tests use local TLS/temporary-root/fake-runner fixtures, not a substituted live environment or invented integration tags.
- Initial `go build ./...` and `go test ./...` passed. An early `make check` overlapped active edits and stopped on formatting; this is not a final verification result and will be rerun after integration.
- No TodoWrite tool is available; this list and commentary track progress instead.
- No paid nested capability probes, sandbox bypasses, live runtime/dist edits, commits or pushes. Owner-provided outside-sandbox Codex success removes the previous coding prerequisite, but does not replace end-to-end release evidence.
- `make check` with a writable temporary lint cache currently finds 57 issues; introduced lint issues are being corrected before final accounting. An overlapping lint retry was rejected by the linter lock; no linter-policy bypass was used.
- The approved removal of the retired app-context updater takes precedence over the stale orchestrator exit instruction; do not invoke or restore it.

## Historical run 0011 record

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
