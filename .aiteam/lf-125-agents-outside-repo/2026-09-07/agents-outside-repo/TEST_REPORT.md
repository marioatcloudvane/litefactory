# Acceptance test report

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
