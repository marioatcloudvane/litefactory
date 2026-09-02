# Implementation Drift Report

## Delivered

- Added a shared, closed four-mode vocabulary and canonical GitHub label
  mapping.
- Added `default_agent` configuration, template defaults, validation, startup
  diagnostics, and registry checks.
- Added stateless Claude and Codex classifiers with strict terminal-result
  parsing and bounded execution.
- Added durable mode-selection state, targeted GitHub label mutation, and
  restart-safe label cleanup.
- Integrated label, slash-command, persisted-decision, and classifier routing
  before ordinary turn execution. Concrete mode is passed into the work turn;
  the work agent does not select it.
- Added sanitized refusal outcomes for malformed, ambiguous, and conflicting
  mode declarations.

## Deviations from the plan

- Classification currently uses the neutral four-mode rubric. Reading bounded
  repository-specific routing guidance through `internal/git` was deferred;
  this does not change the issue's requested label/default-harness behavior.
- The historical `infer` prompt compatibility seam remains for direct legacy
  callers and persisted historical runs. The production daemon always supplies
  a concrete mode and never enters that seam.
- Existing daemon fixtures that do not register classifiers retain their old
  behavior so package-level compatibility tests can exercise unrelated daemon
  concerns. Normal setup always registers the configured classifiers.

## Verification

- `go build ./...`: passed.
- `go vet ./...`: passed.
- Pinned `make lint`: passed with zero issues.
- `make check`: formatting, vet, lint, and all race-tested packages except
  `internal/harness` passed. Two unchanged process-tree tests there failed
  because `/bin/ps` returned no visible descendants/process groups inside this
  execution sandbox; the same failures reproduced when that package was run
  alone.
