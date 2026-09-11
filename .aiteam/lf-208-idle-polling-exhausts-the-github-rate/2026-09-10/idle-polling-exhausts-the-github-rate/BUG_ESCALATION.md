# Remaining aggregate engineering failure — run 0007

## Where

US-002 aggregate verification after the second fix/retest cycle.

## Remaining failure

`GOFLAGS=-p=1 GOLANGCI_LINT_CACHE=/private/tmp/lf208-golangci-cache make check` passed format, vet and pinned lint, then daemon tests timed out after 20 minutes. `TestABackoffArmedMidTickStopsFurtherDispatch` was blocked for 17m37s. Trace: `gatedForge.GetIssue` at `internal/daemon/dispatch_test.go:247`, called through discovery admission/materialisation and `fixture.settle`, from `dispatch_test.go:113`. Full trace: `evidence/final-aggregate-timeout.log`. The failed check process was explicitly cancelled after this result; no service process was touched.

## Root cause as currently evidenced

The test is waiting on its gated Forge callback during bootstrap settlement. The trace proves that wait; a likely remaining fixture assumption is that settlement needs no later admitted materialisation tick. This inference needs a focused engineering reproduction before changing the test. It is not evidence of production correctness or an acceptance review.

## What was attempted

Cycle 1 corrected cache-test ownership/status lint and budget status/switch style diagnostics. Scoped cache/policy/HTTP race checks passed. A broad earlier run timed out in internal/turn under parallel package load. Package execution was serialized without changing the race suite or 20-minute deadline.

Cycle 2 addressed demonstrated daemon fixture assumptions: quota reset rollover after large injected-clock jumps; quota probes intercepted by candidate-only spies; bootstrap concurrency under pessimistic list reservations. Backlog/cross-repository/refill race retests passed 19.112s, confirm/recovery passed 20.317s, and HTTP/store/delivery/backoff passed 15.967s. Production code did not require changes in that cycle. The final broad run exposed the gated bootstrap wait above.

## Completed work retained

Both story implementations and focused tests remain uncommitted in the worktree. US-001 scoped snapshot remains recorded. US-002 corrected scoped snapshot is retained, but final invalidation request `8f8ee053ce704f340bd422b6ba142b60` has valid input and rejected engineering state; effects remain pending. No final aggregate pass, acceptance, publication/save or commit/push is claimed.

## Next step

Continue Implement with a focused reproduction of `TestABackoffArmedMidTickStopsFurtherDispatch`, make its bootstrap/gate sequencing compatible with admitted subsequent ticks while preserving the mid-tick real-backoff assertion, then rerun scoped and complete aggregate gates and refresh engineering evidence. Do not weaken the production admission policy or omit the test.

## Why this run stops

The separated workflow in `.codex/skills/implement-orchestrator/SKILL.md` requires “at most two fix/retest cycles, then report failures.” Both cycles were used. No third cycle or automatic mode change was performed.
