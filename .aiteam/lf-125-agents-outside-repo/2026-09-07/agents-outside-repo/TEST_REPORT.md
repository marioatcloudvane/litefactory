# Acceptance test report

## Run 0015 — remote distribution and CLI delivery

The independent test engineers added 25 scoped test functions derived from the approved acceptance criteria. All pass across their focused race-enabled executions after one production bug fix (BT-001: override provenance participates in delivery identity). No unresolved functional failure was reported. Detailed cases, coverage and untested branches are in [TEST_DISTRIBUTION.md](TEST_DISTRIBUTION.md) and [TEST_DELIVERY.md](TEST_DELIVERY.md).

### Distribution verification

14 functions cover configuration default/override, strict protocol/SemVer/URLs, TLS and redirect restrictions, deterministic publisher install-A/observe-B/update-B, mutation/pin/rollback/concurrent-edit failure protection, exact rosters, next-lock crash recovery and corporate-alias/local-directory collision. Obsolete embedded/default-network fixtures were replaced with explicit local sources or injected TLS transport.

Reported race coverage: config 84.5%, teamremote 49.5%, teampublish 59.0%, teamsource 73.1%, team 62.7%; focused CLI cases 18.2%. These figures have different suite scopes and are not a whole-feature acceptance percentage.

### Delivery verification

11 functions cover selected identity/model/evidence changes, retained-resource tamper/missing/extra/symlink rejection, scratch-store segment reuse/rotation, concurrent disjoint bundles, file-backed native CLI arguments, catalog-free classifier setup, copied evidence isolation and durable metadata independent of bundle reads. BT-001 was independently reproduced, analyzed, task-planned, fixed, re-reviewed and re-tested successfully in one bug cycle.

Reported coverage: render 93.3%, agent 56.0%, codex 43.7%, harness 82.9%, store 74.7%. Earlier broad turn coverage was 78.7% before the one failing identity case was fixed; the focused post-fix identity matrix passes. Do not treat that earlier broad result as a final full-suite pass.

### Verification boundaries

- Real CLI delegation/authentication/permission/isolation/resume acceptance and comparative billed-token/latency measurements are not verified here. CR-B-005 remains an open release gate (AC-3.7 and measured AC-5.2), not an unsupported-Codex finding.
- TEST_BASE_URL and TEST_DATABASE_URL are unset; no external integration environment was used. Ordinary local TLS/scratch-root/fake-runner/store fixtures follow this project's plan and use no invented integration tags.
- Earlier overlapping broad checks encountered importer/cache errors and a linter timeout; duplicate test runs were stopped without claiming results. A bounded 120-second turn/daemon run timed out because existing full fixtures run longer; it is not recorded as a product regression. Final serial verification follows below.
- Public default availability is not established by local tests. Generated release content is unpushed; obsolete public-source fixtures received HTTP 404. No production publishing or paid CLI probe was performed.

### Final serial verification

- `GOMAXPROCS=2 GOLANGCI_LINT_CACHE=/private/tmp/lf-task125-final-lint make check`: formatting and vet passed; lint stopped the command with 41 findings. The reviewer verified all 41 diagnostic files are entirely unchanged from starting HEAD `9bb0fbeccad30fcc4ae1761e6014cb6cf758fb2b`. This establishes inherited source lines, not a reproduced baseline lint run or a green check gate. Introduced lint findings were corrected.
- `GOMAXPROCS=2 go test -race -p 2 ./...`: PASS, every package, exit 0. The final turn package completed in 309.621 seconds. This supersedes the earlier incomplete/timeout broad checks. The reduced build/test parallelism avoids overlapping compiler resource contention; no tests were excluded.
- Final `go build ./...` and `git diff --check`: PASS. No writes to `dist/` or live runtime state.
- Code review is CLEAN after the alias-collision and override-identity fixes; release evidence remains incomplete. Verification finished after midnight on 2026-09-08 and remains in the run's original 2026-09-07 session directory.

## Historical run 0011 report

Scope: run 0011, delivered US-1/US-2 and safe runtime refusal guard. Tests were derived from the supplied acceptance criteria, using established stdlib assertions and isolated temporary filesystem fixtures. No production code changed in this test pass.

## Newly covered acceptance cases

- `TestModeRosterPreservesDeclaredOrder`: mixed inline/shared entries retain declaration order, including IDs deliberately out of alphabetical order.
- `TestSnapshotModeIsolationIgnoresUnselectedSources`: corrupting an unselected implementation skill and removing an unused shared orchestrator leaves the research snapshot's files and digest unchanged; no implementation source or skill enters that snapshot.
- `TestSnapshotRejectsTransitiveResourceEscape/relative`: a skill resource resolving outside the canonical catalog root is rejected.
- `TestSnapshotRejectsTransitiveResourceEscape/symlink`: a contained resource filename pointing outside through a symlink is rejected.

Result: 4 newly tested cases passed, 0 failed. Both complete touched-package suites passed with race detection:

```text
go test -race -cover ./internal/team ./internal/teamsource
internal/team        coverage: 76.5% of statements
internal/teamsource  coverage: 77.7% of statements
```

Existing tests already cover strict schema cutover, duplicate effective IDs, cross-mode inline identity agreement, source metadata/cycles/limits/cancellation/locking, central import independence, shared conflict preflight, dependency and override drift, forced updates, absolute path relocation, root-only missing origins and multi-roster override round trips. This pass avoided duplicating those regressions. Coverage is statement coverage for these two packages only, not a percentage of story acceptance criteria.

## Remaining acceptance gaps

US-3 runtime bundle materialization/delivery, conversation and CLI isolation/parity, US-4 finished workflow bodies, and US-5 runtime token telemetry are explicitly deferred by the implementation handoff following the Codex app-server EPERM probe. These tests do not demonstrate those capabilities or token savings. This pass did not independently exercise real Codex/Claude processes, external CLI authentication, or concurrent mutation during snapshot reading. Existing turn/daemon refusal-guard tests and other package suites were reported passing by the parent but were not rerun here; no full-repository gate is claimed by this report.
