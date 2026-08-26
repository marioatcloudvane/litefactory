# Notes — session-resume (Implement mode)

## Owner decisions applied as settled

- **OD-1 — ACCEPT AS-IS.** The history block copies stored comment bodies verbatim into cold-start
  `NNNN.meta.json`. No redaction pass was added. The accepted exposure is recorded in a code comment
  on `historyNote` in `internal/harness/prompt.go`: bodies are unredacted because S-7 redaction is
  outbound-only, the files are `0600` and local, and running redaction here was rejected because the
  model would see a redacted version of text it was previously shown verbatim.
- **OD-2 — `MaxHistoryBytes = 32 << 10`** (32 KiB), in `internal/harness/prompt.go`.

## Correction made to the plan during implementation

The plan's AC-SR-2.7 proposes detecting a resume refusal with
`Model == "" && Events == 0 && ExitCode != 0`. **That rule would never have fired.** The probe against
the real binary shows `--resume` on a missing session emits **one** stdout event — a terminal
`result` event with `subtype: "error_during_execution"`, `is_error: true`, `num_turns: 0` — so
`Events == 1` and `HasResult == true`.

The implemented rule is the plan's *intent* rather than its literal text:

```
resuming && Outcome == OutcomeFailed && Model == "" && ExitCode > 0
```

`> 0` and not `!= 0`, because `SignalExitCode` is `-1`. Evidence and rationale are in
`CLAUDE_CLI_CONTRACT.md`; `TestResumeRefusedClauses` pins every clause, and
`TestRunReportsARefusedResume` asserts the refusal arrives with `Events == 1 && HasResult`, so the
plan's original rule is provably dead.

## Version discrepancy

The plan was probed against `claude 2.1.245`. The operator's installed binary is **`claude 2.1.246`**,
which is what the contract was re-verified against. Every observed behaviour is identical, including
the verbatim stderr strings.

## Code Review Advisories

From `CODE_REVIEW.md`, verdict **ADVISORY_ONLY** — no blocking findings.

1. **Doc-comment typo** `missing---verbase` at `internal/harness/runner.go:596`. **Fixed** by the
   orchestrator during Phase 1.5 (now `missing-`` --verbose ``).
2. **Neighbouring doc comments slightly out of step.** `internal/config/config.go` and
   `internal/store/segments.go` still describe only the cold-path invocation, whereas
   `internal/harness/doc.go` now documents both. Both files are outside this story's scope and were
   left alone. Worth a one-line touch-up next time either file is opened.
3. **Two full-table reads per prepared turn.** `previousRun` and `commentHistory` in
   `internal/turn/prepare.go` each call a `List…` accessor and filter in Go. This is deliberate and
   documented — the plan explicitly rejected a new column and a new store accessor (F-3, AC-SR-2.3) —
   and is worth revisiting only if a task's run or comment history grows large.

## Scope held

- Prompt-injection hardening was **not** touched, per the owner's explicit directive. The `<note>`
  framing and `</note>` neutralisation in `internal/harness/prompt.go` are reused exactly as they
  were; nothing was added, strengthened or renamed. The `old_owner` relabelling that *was* built is
  instruction-precedence, not injection hardening.
- `internal/daemon/tick.go` ends byte-identical: SHA-256
  `9a308a98bbe613d40c0f00fdd4af046102f13403d86ca8d7f5e0f9aec51e3ef7`.
- No migration, no new table, no new column, no new store accessor. `store.FirstSegmentOrdinal`
  untouched at `1`.
- Nothing committed, nothing pushed — the work is in the working tree for the owner to commit.

## Environment

The daemon was never started. No writes to the live store, no GitHub calls, no comments posted, no
cursor changes. The only `claude` invocations were five read-only contract probes in the session
scratchpad, outside the repository.
