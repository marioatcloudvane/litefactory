# Implementation Notes — modes-should-stay

## Run 0003

- Execute only the four factory-selected frozen revisions supplied for generation 1. No local story bodies or forge reads.
- US-001 passed scoped review and daemon/store race coverage. Event 000001 published its immutable snapshot immediately; event 000002 invalidated that snapshot before US-002 shared-file edits. Retention checks will be repeated with the next stable snapshot.
- All infrastructure checks use existing temporary SQLite/httptest/fake executor fixtures, requiring no external environment or integration tags.
- Implementation base is synchronized at 592131d, superseding the planning run's stale-base warning.
- US-002: scoped review found parent-watermark retirement of incomplete child refinements (CR-B-001); fixed by independent child completion predicates and passed re-review. New fixtures preserve label capabilities for mandatory confirmation; no production change for the resulting fixture-only backlog failures.
- Events 000003 and 000004 reverified US-001 and verified US-002 after daemon/store/report race coverage (78.0%/65.3%/83.1%) plus turn mode/prompt/report checks.
- US-003 passed scoped review and acceptance. CR-B-001 moved authorized readiness invalidation before repository/team capture failures; deterministic pinned-artifact identity/content tests cover SEC-21. Event 000005 verified US-003; events 000006–000008 invalidated all three prior snapshots before US-004 shared-file changes.
- US-003 protocol: .litefactory-plan-outcome.json with factory run and per-subprocess attempt binding; PLAN_CONTEXT.json holds identity-bound unfinished scope/decisions/assumptions/questions. Full proposal remains v1.
- US-004 targeted runtime tests passed; aggregate review found no new runtime blockers and US-001–003 production revalidation is CLEAN. US-004 AC-004-4/5 remain blocked because sandbox rejected .codex writes. Exact CODEX_SKILLS.patch passed read-only applicability check; never applied through a permission bypass.
- All final scoped acceptance tests passed. Events 000010/000011/000012 reverified US-001/002/003 and their snapshots matched current bytes; event 000013 records US-004 runtime success but AC-004-4/5 protected-copy block.
- Initial aggregate build passed and make check format/vet/pinned lint passed (0 lint issues). Concurrent aggregate race/coverage runs reached the default 10-minute package timeout (CLI/daemon/turn); stacks showed recent progressing tests rather than a demonstrated deadlock. Retrying one suite at a time with GOMAXPROCS=2 and GOFLAGS=-p=1 -parallel=2 -timeout=30m, without repository configuration changes.
- Final make check PASS with GOMAXPROCS=2, GOFLAGS=-p=1 -parallel=2 -timeout=30m, writable lint cache. All format/vet/pinned-lint/race gates green; CLI116.699s, daemon244.787s, git183.600s, store155.010s, turn441.213s. Separate full race/coverage rerun now runs alone with package concurrency2 and the same test parallelism/timeout.
- Final full go test -race -cover ./... PASS with GOMAXPROCS=2, GOFLAGS=-p=2 -parallel=2 -timeout=30m, running alone. 29 tested packages passed; no failure/panic/race output. Initial default-timeout attempts remain documented above.
  - ok   github.com/cloudvane/litefactory/internal/cli 119.912s coverage: 76.6% of statements
  - ok   github.com/cloudvane/litefactory/internal/codex 43.849s coverage: 78.7% of statements
  - ok   github.com/cloudvane/litefactory/internal/daemon 188.236s coverage: 78.0% of statements
  - ok   github.com/cloudvane/litefactory/internal/harness 15.156s coverage: 85.0% of statements
  - ok   github.com/cloudvane/litefactory/internal/planbrief 1.593s coverage: 90.1% of statements
  - ok   github.com/cloudvane/litefactory/internal/store 47.470s coverage: 65.3% of statements
  - ok   github.com/cloudvane/litefactory/internal/turn 432.088s coverage: 79.6% of statements

- Run 0004 correction: prior reports said US-001–003 were verified, but only local checks and event submission succeeded. Actual factory logs show every verified event rejected because the 346-byte Evidence contract paragraph was parsed as an acceptance criterion despite the 256-byte evidence-name cap. All supplied states are blocked. Successful implementation/save does not imply accepted completion; closure also does not modify body checkboxes/TODO tables. Current run records explicit blocked events and COMPLETION_DIAGNOSIS.md. No production changes or test rerun.

## Run 0014 decisions

Followed the explicitly supplied verified-event protocol for this legacy feature; did not infer a switch to separated workflow from the merged main branch. Semantic instruction agreement satisfies AC-004-5; harness-specific workflow links need not match byte for byte. Corrected only the stale lookup wording, with no protected-tree writes. Completed dependencies were not reopened or reworked. All selected and aggregate gates passed; event ingestion and external delivery remain factory-owned.
