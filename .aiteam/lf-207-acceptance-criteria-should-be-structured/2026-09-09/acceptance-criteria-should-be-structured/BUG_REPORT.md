# Run 0010 fixture diagnosis

Scope: unblock existing tests only; no story completion or acceptance determination.

## Root cause: legacy full-body Plan fixtures accidentally select the new contract

**Affected tests:** `TestPlanDiscussionThenCompletion` and other legacy transport tests using `setupOutcomeForge` without a persisted feature.

**Files:** `internal/turn/plan_outcome_test.go:42`, `internal/turn/plan_handoff.go:30`, `internal/turn/plan_retry_transport_test.go:84`.

**Explanation:** The fixture runner writes version-1 full-body proposal/outcome files. Fresh managed features now receive content/submission version 2, starting a broker before the runner. The default fixture home is long enough to trigger the broker socket guard on this host, hiding the protocol mismatch. Shortening only the path would not make the legacy fixture a valid new-contract submission.

**Suggested fix:** Explicitly seed the planning feature and bind content/submission version 1 in the legacy outcome fixture setup. Preserve its outcome, transport, readiness, failure and retry assertions. Follow the existing explicit binding in plan_retry_transport_test.go. Keep typed fixtures independently bound to version 2.

**Executed reproduction:** `go test ./internal/turn -run '^TestPlanDiscussionThenCompletion$' -count=1` failed before runner startup with `submission: configured socket path is too long`. Log: `/private/tmp/lf306-plan-diagnosis.log`.

## Root cause: broker fixtures require short private resolved temporary paths

**Files:** `internal/turn/fixture_test.go:71`, `internal/turn/submissions_test.go:27`, `internal/submission/broker.go:77`.

**Explanation:** The general fixture derives LF_HOME from a Go test directory containing a long test name and host temporary root. That is unsuitable for Unix sockets. Existing typedProgressScope already allocates a short private directory with MkdirTemp, resolves symlinks, and registers cleanup.

**Suggested fix:** Where a fixture actually exercises the version-2 broker, reuse that short-private-home setup through test-only helpers. Do not weaken the completed broker's length/containment guard, change production path policy, or silently dispatch typed cases as legacy. Callback-only typed Plan tests that never start a broker need no socket-path workaround.

The parent orchestrator/test engineer owns repair and fresh verification; this analysis changed no production or test files.

## Root cause: aggregate race suite exhausts the implicit package deadline

**Affected command:** `make check`, final `go test -race ./...` step.

**Files:** `Makefile:23`; captured result `/private/tmp/lf207-run306-make-check.log`.

**Observed evidence:** The aggregate log passes fmt, vet and pinned lint (0 issues), and all reported packages except turn. Turn panics after 10m0s and reports ten active tests aged only 0–9 seconds. Their stacks include ordinary local git execution and fixture SQLite migrations; many other goroutines are parked in testing's parallel-slot scheduler. This is not evidence of a single ten-minute test deadlock. The parent supplied a separately executed passing turn race/coverage result of 594.698s, already within six seconds of the package-wide default deadline. No broad test was rerun for this diagnosis.

**Suggested smallest fix:** Change only the Makefile test recipe from `$(GO) test -race $(PKGS)` to `$(GO) test -race -timeout=20m $(PKGS)`, with a short comment explaining package-wide allowance for the full real-git/SQLite race suite. Twenty minutes provides approximately twice the observed successful duration while retaining a finite watchdog. Preserve race detection, all packages, assertions and per-operation deadlines; do not add skips or change production timeouts.

**Verification:** Parent reruns `make check` with no competing broad test process and retains its exact final result. A new timeout or a specific failed assertion still requires diagnosis; raising the budget does not establish that tests pass.
