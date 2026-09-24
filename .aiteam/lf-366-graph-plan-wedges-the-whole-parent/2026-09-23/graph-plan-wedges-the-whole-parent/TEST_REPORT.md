# Engineering tests — run 0006

All three stories implemented and scoped race regressions passed on final snapshot digest 6cf2b6aaed38d385634bee8a3efa06a4f1b7fa90aeebae97b7a03800490a8782. Acceptance has not been reviewed.

Final scoped test output:

```text
ok internal/planbrief 2.179s
ok internal/report 2.734s
ok internal/teamsource 3.545s
ok internal/turn 78.374s
ok internal/daemon 155.981s
```

Covered lifecycle reasons/reopening/holds, dependency policy/readiness, exact retained retirement authority, both restart persistence gaps, create/attach/update readback, durable fairness, original request preservation, multiple holds, notice response loss/deduplication, stale cursor protection, healthy replies/parent discussion and actual Tick rate-limit/backoff behavior.

Additional aggregate build, gofmt and whitespace checks passed. Initial make check stopped at 27 pinned-linter findings; one coordinated fix cycle corrected them and rerun lint reported zero issues. The first full race execution was interrupted after prolonged silence, not counted as a pass. A bounded full daemon non-race diagnostic passed in 219.841s. A separate isolated race diagnostic made progress without test failure but was interrupted after 111.618s to avoid duplicate concurrent work; its package failure is interruption, not a pass. Full make check is now running serial packages with verbose progress (GOFLAGS=-p=1 -v), the same required race suite and pinned linter, using a writable temporary lint cache. Aggregate completion is pending.

No live deployment/forge behavior or acceptance is claimed.

## Final parity repair and retest

The first completed full race run passed all packages except two pre-existing canonical/runtime Plan package consistency tests. Canonical source was copied unchanged to its runtime mirror; targeted tests pass. Final scoped checkpoints now use final3 snapshot/check IDs (see notes.md). An overly broad post-repair scoped run hit its eight-minute suite limit during ordinary SQLite setup; narrowed changed-story plus parity checks passed as shown above.

Last aggregate retest is executing the complete make check target with the same pinned linter and full race suite. Build/format/whitespace checks already pass on these final bytes; aggregate completion remains pending.

## Run 0007 — aggregate execution

`go build ./...`, `gofmt -l .` (empty output) and `git diff --check` passed. `make check` passed formatting, vet and pinned lint (zero issues); lint emitted sandbox cache-write warnings without failing. Full race suite is currently running; completion is not yet claimed. Raw output: `/private/tmp/lf366-run7-make-check.log`.

### Completed aggregate result

`make check` exited 0: formatting, vet, pinned lint (zero issues) and the complete race suite passed. `internal/turn` passed in 3659.846s, daemon in 861.519s, store in 429.315s and CLI in 236.492s. Cached passing package results are identified in `aggregate-run7.log`. The long race run completed without interruption; temporary test-directory timestamps confirmed progress during buffered output. Lint cache-write warnings did not fail lint. Exact implementation manifest remains unchanged. No acceptance or live-deployment claim is made.
