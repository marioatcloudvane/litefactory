# Implementation notes — run 244

## Decisions and assumptions

- Used only the three factory-selected frozen revisions in the run input as story specifications. Existing issue identities and dependencies were retained; the factory owns forge publication and story closure.
- Applied the approved tradeoff: an eligible normal recovery can run against the current task base while dirty synchronization is disclosed as deferred. Up-to-date, disabled and over-cap decisions retain precedence. Ordinary retained work is not attributed to the factory or automatically committed.
- Confirmations remain guarded; a refused dirty confirmation stays pending and does not launch an agent. Recovery may make its recorded revisions stale, requiring a new proposal and confirmation.
- All tests use existing fake runners/forge with isolated local git repositories and migrated temporary SQLite. No external integration targets or build tags were needed.

## Git preservation probes

Git 2.50.1 (Apple Git-155) can rewrite raw index stat metadata during an ordinary status inspection. In an isolated temporary repository, commit a file, capture `.git/index`, advance the unchanged file's mtime by ten seconds with `os.utime`, then run `git status --porcelain -z --untracked-files=all --ignored=no`: index bytes change. Advancing the mtime again and running the same command with global `--no-optional-locks` preserves bytes. `Worktree.Status` now uses that flag; the regression checks raw index preservation.

The same flag was insufficient for refused merges involving staged/tracked reserved transport. A narrow read-only preflight now rejects these unsafe cases before raw merge mutation. The full reproduction and sixteen-case matrix are in GIT_TRANSPORT_PROBE.md and the US-3 review.

## Incremental verification

- Event 000001: US-1 attempt 1 verified after AC-171-1 through AC-171-4 acceptance, review and scoped race tests.
- Event 000002: US-1 invalidated before editing its shared `merge.go` snapshot in response to the reserved-transport counterexample.
- Event 000003: US-1 attempt 2 verified after shared-guard review and renewed scoped checks.
- Event 000004: US-2 attempt 1 verified immediately after its own acceptance, review and tests passed against settled shared code.
- Event 000005: US-3 attempt 1 verified immediately after its own acceptance, review and tests passed.

Events are immutable transient transport under `.litefactory-story-events/244`; they must not be committed or deleted by the coding agent. Review artifacts contain exact commands and acceptance mapping. Latest story snapshots are checked again at finalization.

## Validation and environment

Scoped race/coverage checks passed for all acceptance criteria. The final combined US-1/US-2 turn check passed in 78.534 seconds (44.2% package coverage); US-3 git checks passed in 38.964 seconds (25.5%) and turn checks in 14.136 seconds (24.1%). Adapter, raw-index and store checks also passed. Aggregate code review is CLEAN.

The baseline `make check` passed formatting, vet and lint, but its full race run reached Go's default ten-minute timeout in CLI, daemon and turn while SQLite/test work was still progressing; no assertion failure was observed in that baseline. Final aggregate validation used an external temporary GO wrapper that adds only `-timeout=30m` to `go test`, preserving `make check`, race detection and all packages. Its lint cache was redirected to writable temporary storage. No repository test policy was changed.

Final `make check` PASSED (exit 0): formatting, vet, pinned lint with zero issues, and the complete race suite. CLI passed in 264.944 seconds, daemon in 279.378 seconds, and turn in 385.186 seconds. `go build ./...` then PASSED (exit 0). The dispatcher records these aggregate outcomes in MANIFEST.md. The final evidence audit validated five consecutive events, 4,119,464 total bytes, all expected acceptance identifiers and digests, and exact current bytes for each latest story snapshot.

Two test-fixture corrections were made before verification: the sync report assertion checks that the existing retained-work path is not repeated, and the Codex resume/fallback lifecycle first establishes a successful no-edit conversation because Codex persists its resume token only after success. Neither weakened retained-work assertions or changed production save semantics.

## Documentation and workflow

APP_CONTEXT.md now describes the recovery lookup points. APP_CONSTRAINTS.md records approved deferral, disclosure and guarded confirmation/transport behavior with enforcement references; duplicate enforcement rows were consolidated without dropping unrelated rules. The constraints document remains above its approximate size target, and no unrelated rewrite was undertaken.

No agent skills or Claude counterparts changed. Their stale local-story/all-stories-first orchestration wording was overridden by the explicit managed-run instructions; this run did not attempt an unrelated parity rewrite. No commits, pushes, production daemon operations or runtime-state edits were performed.
