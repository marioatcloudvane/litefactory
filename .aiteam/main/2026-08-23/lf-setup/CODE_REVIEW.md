# Code Review — Phase 1.5 Gate

**Branch:** main  
**Session:** 2026-08-23 (lf-setup)  
**Reviewer:** go-code-reviewer  
**Verdict: ADVISORY_ONLY — no blocking findings**

All 38 M0 tasks are eligible to proceed to Phase 2 (environment setup) and Phase 3 (testing).

---

## Sub-agent findings

### 1. Harness subprocess traps (SD-1 / SD-3 / SD-6)

**Result: CLEAN**

- BFS pgid sweep (`killSubtree`) correctly implements SD-1's descendant-reaping requirement.
- Outcome mapping branches only on `.is_error` (not `.subtype`) per SD-3; third "no result" outcome covered by `OutcomeNoResult` + `context.WithTimeout`.
- `--verbose` is always included in argv per SD-6; verified by `TestRunArgs`.
- `bufio.Reader.ReadBytes` used throughout (no `bufio.Scanner`), per SD-5.
- `cmd.Env` is always an explicit allowlist; `os.Environ()` never appears in harness code.

### 2. Cross-story consistency

**Result: 2 ADVISORY findings**

**A2.1 — Timestamp format drift in `harness/meta.go`** *(advisory)*  
`meta.go` writes `time.RFC3339Nano` timestamps into `meta.json`. `store/store.go`'s own doc comment ("must agree with the database") specifies `store.FormatTime()` (`"2006-01-02T15:04:05.000Z"`, millisecond precision, always UTC). These two formats do not round-trip identically. M1 tools that parse `meta.json` and join against store timestamps will see drift. Fix before any M1 story that reads both.

**A2.2 — Goroutine roster doc mismatch** *(advisory)*  
`daemon/daemon.go` header comment lists 4 goroutines (stdout reader, stderr reader, daemon tick loop, second-signal watcher). The second-signal watcher lives in `cmd/litefactory/main.go`, not `daemon.go`. The roster is correct for the binary; the comment's attribution is slightly off. Cosmetic.

### 3. Idiom, style, and resource management (~60 files)

**Result: CLEAN, 1 advisory**

All error paths close their resources. No goroutine leaks found. `context.Background()` appears only in `main.go`. `fmt.Print*` appears only in CLI command handlers via `env.Stdout/Stderr`. No `recover()` except the single sanctioned one in `internal/redact` (verified by `TestExactlyOneRecoverInThisPackage`).

**A3.1 — `internal/github` pagination cap** *(advisory)*  
100-page cap is correct as a safety ceiling. The constant is a bare integer literal rather than a named constant; naming it `maxPaginationPages` would make the intent reviewable at a glance. Cosmetic.

### 4. Watermark / retry state machine

**Result: MOSTLY CORRECT, 1 advisory**

The stop-on-failure guarantee (AC-4.10) is correctly implemented: `drain` in `daemon/tick.go` re-reads the watermark from DB after each comment via `d.consumed()`; stops the queue for that task at the first unconsumed entry; leaves the cursor at the deferred issue's `UpdatedAt`. The `MAX()`-based `AdvanceWatermark` never rewinds, and `CompleteRunReport` is a single atomic 3-write transaction.

**A4.1 — Rate-limit gap in `daemon.go reportAbandoned`** *(advisory)*  
`Daemon.Start`'s `reportAbandoned` path posts comments for orphaned runs on startup. It handles generic errors with a log-and-continue, but does not detect `*github.RateLimitError` and apply backoff. On a startup immediately after a heavy tick, all reconciliation posts could be lost silently (logged but not retried) rather than deferred. The normal tick path handles rate limits correctly (aborts the whole tick with the error). Risk: data loss only under a narrow startup-under-rate-limit race. Fix in M1.

### 5. Worktree-escape audit

**Result: CLEAN**

The guarantee that no usable `git.Worktree` pointing at the registered clone can be constructed outside the controlled path is mechanically enforced at three levels:

1. **Reflection test** (`TestWorktreeHasNoExportedConstructionPath`): asserts every `Worktree` field is unexported — external packages cannot build a working struct literal.
2. **AST test** (`TestWorktreeIsBuiltInExactlyOnePlace`): scans all non-test `.go` files in `internal/git`; the only permitted `Worktree{}` literal is inside `newWorktree` in `worktree.go`.
3. **Zero-value test** (`TestZeroValuesRefuseToRunGit`): a `git.Worktree{}` literal returns `ErrUninitialized` on every method call rather than operating on the process's working directory.

`Clone.WorktreeAdd` (the only caller of `newWorktree`) already refuses paths overlapping the registered clone (`TestWorktreeAddRefusesPathsOverlappingTheClone`). The chain is closed.

---

## Pre-existing advisories carried into this review

| ID | Source | Summary | Disposition |
|----|--------|---------|-------------|
| SD-9 | Spike decisions | Real GitHub SSH push deferred | Manual-verification-pending; mechanisms tested against local sshd |
| — | Wave 4b | Real-repo end-to-end deferred | Manual-verification-pending; mechanisms tested against httptest mock |
| — | Wave 4c | `files-touched` (AC-4.4) empty when Bash tool self-commits | Design gap noted; not blocking M0 |

---

## Gate verdict

**ADVISORY_ONLY.** The codebase is shippable for M0. No finding requires a code change before Phase 2.

Recommended fixes before M1 scope begins:
1. **A2.1** — align `harness/meta.go` timestamps to `store.FormatTime()`.
2. **A4.1** — add `*github.RateLimitError` detection + backoff in `reportAbandoned`.

Items A2.2, A3.1 can be addressed at any convenient point; they carry no correctness risk.
