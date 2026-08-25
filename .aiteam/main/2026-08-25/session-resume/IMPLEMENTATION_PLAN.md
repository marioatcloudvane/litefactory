# Implementation Plan — session-resume
# Branch: main
# Date: 2026-08-25

Scope: make run four of a task continue run one's conversation instead of starting cold, and give
the cold-start case a compacted history to start from.

Inputs: `RESEARCH_BRIEF.md` (spike-promoted, 2026-08-25), an empirical probe of the `claude` CLI run
during planning, and a citation re-verification pass against the tree at `e30257c`. No
`feature-spec.md` — the brief carries the owner's four directives verbatim and there were no gaps to
close, so the requirements conversation was skipped. Same precedent as `fix-comments-pickup`.

The owner's instruction was "don't over-complicate this". Three stories. No tiers, no options.

---

## The brief's two unknowns are answered

Both were probed empirically during planning. Neither is a branch point any more.

**1. Reusing `--session-id` does not resume — it errors.** Against `claude 2.1.245` on this machine:

```
$ claude -p --session-id <fresh-uuid> "Remember this magic word: PLATYPUS42."   # exit 0
$ claude -p --session-id <same-uuid>  "What was the magic word?"                # exit 1
Error: Session ID <same-uuid> is already in use.
```

The check is against the on-disk transcript, not a live process lock — the first process had fully
exited. `--resume <uuid>` composes with `-p` and with **every** flag in `Runner.args`
(`runner.go:302-318`) except `--session-id`, which conflicts:

```
Error: --session-id can only be used with --continue or --resume if --fork-session is also specified.
```

Verified with litefactory's exact argv shape — `-p --output-format stream-json --verbose --resume
<uuid> --tools … --settings … --allowedTools … --strict-mcp-config` — exit 0, terminal event
`"subtype":"success"`, and the resumed id echoed on every stream event.

**This means litefactory's second run of any task is broken today.** `prepare.go:108` derives the
session id from `store.FirstSegmentOrdinal` (hardcoded `1`, `segments.go:14`), so every run of a
task passes the same UUID; the second one exits 1 before emitting a single event. It has never
fired because **no task has ever had a second run** — `~/.litefactory/logs/runs/` holds exactly two
metadata documents, `.../1/0001.meta.json` and `.../2/0001.meta.json`. This feature is the fix for a
latent defect, not only an improvement.

**2. The transcript survives, and survives more than the brief assumed.** Sessions live at
`~/.claude/projects/<cwd-slug>/<session-uuid>.jsonl` — outside the worktree; nothing is written into
the working directory at all. Both of litefactory's real runs still have theirs:

```
~/.claude/projects/-Users-mariomh--lf-worktrees-litefactory-task-1/26b2fd12-ac0e-5ac3-ae55-cbaaaea9f4ad.jsonl
~/.claude/projects/-Users-mariomh--lf-worktrees-litefactory-task-2/a990e05f-fa37-53e8-a787-b69c51916aae.jsonl
```

Those are exactly the deterministic ids `naming.ClaudeSessionID` derives, matching
`claude_session_id` in the meta documents — so claude never drifted and `runner.go:452-459`'s warning
never fired. A worktree deleted and recreated at the same path **keeps** its transcript (probed
directly: `rm -rf` the cwd, recreate, `--resume` → exit 0). Resume does not even require the cwd to
match. The real cold-start cases are therefore narrower than the brief supposed: a different machine,
a changed `worktree_root` on the repos row, or a hand-deleted transcript.

**Also verified:** `--resume` on an unknown id fails **closed** (exit 1, `No conversation found with
session ID: …`). `--continue` fails **open** — in an empty directory it silently starts a fresh
conversation and exits 0. `--resume` is the safe primitive for a harness; `--continue` must not be
used.

*Caveat: all of the above is `claude 2.1.245` on this machine. It is a contract with an external
binary and S1 exists to pin it.*

## Settled — do not re-open during implementation

| # | Fact |
|---|---|
| F-1 | Resume is `--resume <stored id>` with `--session-id` **dropped**, not `--resume … --session-id … --fork-session`. Forking writes a second transcript per run and needs a new id per run, i.e. segment ordinals; plain resume grows one transcript in place and keeps the id claude already saw. |
| F-2 | `segments.ordinal` stays at `1`. Incrementing it means a new session id means a deliberately cold conversation, which is the thing this feature exists to stop. LF-8's problem, not this one. |
| F-3 | No migration. No new table, no new column. The resume decision comes from `runs.segment_id` + `runs.status`, which are already written before every subprocess. |
| F-4 | This feature does not touch `internal/daemon/tick.go`. **No collision with the sibling `fix-comments-pickup` session**, which owns that file. |
| F-5 | The corrected citation: the worktree directory is `task-<issue>`, not `lf-<issue>` (`naming.WorktreeDirPrefix = "task-"`, `naming/paths.go:12`). |

## Build order

S1 → S2 → S3, strictly serial. S2 and S3 both change `PromptInput` and `prepare.go` at overlapping
lines, so they do not parallelise. S2's fallback task (T-SR-2.4) depends on S3's renderer existing,
so land S3's `BuildPrompt` work before closing S2 — or land T-SR-2.4 last, after S3.

---

## User Story 1 — The invocation contract is pinned and the session id has one source

> As the owner, I want litefactory's claude invocation to be verified against the real binary and
> the session id to come from one place, so the resume work is built on a checked contract rather
> than on an assumption.

Turns the probe findings into something the tree asserts, and removes the second, redundant
derivation of the session id.

### Architecture Notes
- **The probe is not the deliverable; a test in the tree is.** T-SR-1.1 re-runs the probe against
  whatever `claude` the operator has and records the verdict in-tree, so a version bump that changes
  the flag semantics is caught by a human reading a document rather than by a wedged task.
- **One derivation site.** `runner.go:230` derives the session id via `naming.ClaudeSessionID(req.Ref,
  req.SegmentOrdinal)`; `prepare.go:108` derives the identical value independently to create the
  segments row. Delete the runner's derivation and add `ClaudeSessionID string` to `harness.Request`
  (`runner.go:150-172`), populated from `segment.ClaudeSessionID`. `GetOrCreateSegment`'s own doc
  already settles this: "if they ever disagree, the stored value is the one claude has already seen
  and therefore the one that wins" (`segments.go:41-43`). Resume makes that hypothetical real.
- `naming.ClaudeSessionID` stays; it is simply called once, from `prepare.go`.
- **The tool boundary must survive resume.** `--tools`, `--settings`, `--allowedTools` and
  `--strict-mcp-config` were all confirmed to compose with `--resume`, but a resumed session must
  take its boundary from `r.args()` and never inherit it from the stored transcript. T-SR-1.3 asserts
  this against the real binary, because it is a security property and not a convenience.
- Platform: Error handling — `Request.validate` (`runner.go:176-199`) gains a check that the session
  id is a non-empty canonical UUID, alongside the existing ordinal checks. `errorlint`/`nilerr` are
  on in `.golangci.yml`.
- Platform: Database — no new query, no migration.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-SR-1.1 | Re-run the probe against the operator's `claude` and record the verdict, version and exact stderr strings in `$session/CLAUDE_CLI_CONTRACT.md` | infra | — | S |
| T-SR-1.2 | `harness.Request.ClaudeSessionID` field; `prepare.go` populates it from `segment.ClaudeSessionID`; delete the derivation at `runner.go:230` | logic | — | S |
| T-SR-1.3 | Verify against the real binary that `--tools`/`--settings`/`--allowedTools`/`--strict-mcp-config` still bind on a resumed run; record the evidence | infra | T-SR-1.1 | S |
| T-SR-1.4 | `Request.validate` rejects an empty or non-UUID session id | logic | T-SR-1.2 | XS |
| T-SR-1.5 | Tests: validation table for the new field; `prepare` passes the **stored** id through even when it differs from the derived one | test | T-SR-1.4 | S |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-SR-1.1 | `harness.Request` carries the claude session id and `internal/harness` no longer derives it. `naming.ClaudeSessionID` is called from exactly one place in the production tree. |
| AC-SR-1.2 | A test seeds a segment whose stored `claude_session_id` differs from the derived value and asserts the **stored** one reaches the child's argv. |
| AC-SR-1.3 | `Request.validate` rejects an empty or malformed session id with `ErrInvalidRequest`. |
| AC-SR-1.4 | `$session/CLAUDE_CLI_CONTRACT.md` records the claude version tested, the observed behaviour of a reused `--session-id`, of `--resume`, and of `--resume` combined with `--session-id`, with verbatim stderr. |
| AC-SR-1.5 | The tool-boundary flags are shown to still apply on a resumed run, with the evidence recorded. |
| AC-SR-1.6 | `make test` and `make check` are green. |

---

## User Story 2 — A run continues the previous conversation

> As the owner, I want run four of a task to continue the conversation runs one to three had, so
> litefactory does not lose what it already reasoned, tried and said.

The primary path, and the fix for the latent second-run failure.

### Architecture Notes
- **`Runner.args` branches on one boolean.** First run of a segment: `--session-id <id>`, as today.
  Every later run: `--resume <id>`, with `--session-id` omitted. Everything else in
  `runner.go:302-318` is unchanged. The flag rationale block at `runner.go:277-301` is load-bearing
  prose and must be updated in the same commit, including the sentence about `--session-id` being the
  deterministic UUID v5 — it is now the *first* run's flag only.
- **"Has this conversation started?" comes from the runs table.** A prior run on this segment in a
  terminal status (`succeeded`/`failed`/`reported`) means claude was invoked with this id and a
  transcript should exist. `store.ListRuns` exists (`runs.go:379`) and `runs.segment_id` is already on
  the row (`0001_init.sql:87`), so this is a read and a filter in Go — no migration, no new accessor.
  Rejected in one line each: a new `segments` column needs a migration for a fact the runs table
  already implies; probing `~/.claude/projects/<slug>/<uuid>.jsonl` means reverse-engineering an
  undocumented path layout; always-resume pays a wasted process start on every first run.
- **The store is the *intent* signal, not ground truth, so the fallback is mandatory — not a nicety.**
  A machine move or a changed `worktree_root` leaves runs rows behind with no transcript. Without a
  fallback the task is **wedged permanently**: the store says "resume", `--resume` exits 1, and every
  subsequent run repeats it forever. This is the single most important correctness point in the
  story.
- **Detect the refusal structurally, not by string-matching stderr.** `--resume` on a missing session
  exits non-zero having emitted **no init event**, so `RunResult.Model == "" && Events == 0 &&
  ExitCode != 0` is the test. Distinguishable from the SD-7 missing-`--verbose` failure, which is zero
  events with a *clean* exit, and from a process that never started, which already comes back as
  `ErrStartFailed` on a different path (`result.go:69-72`). Surface it as a `harness` sentinel —
  `ErrSessionNotResumable` — checked with `errors.Is`; log `RunResult.Stderr` as evidence but do not
  branch on it.
- **On refusal, rerun once with the cold prompt, in the same turn.** Exactly one retry, and only on
  the sentinel. It reuses the same run row and the same ordinal — the retry is the same run, not a
  new one — and the second attempt uses `--session-id`, which succeeds because the transcript that
  would have collided is gone.
- **The resumed prompt is the delta.** A resumed session has already seen the issue note, the
  feature-slug paragraph, the headless-guidance paragraph and the `Task`/`Branch`/`Working directory`
  bullets. A resumed run emits: the `# litefactory run NNNN of <ref>` header line, the `- Mode:` and
  `- Feature slug:` bullets, `## Instruction`, and the triggering comment's `<note>`. Keep the slug
  bullet — one line of insurance against claude's own internal compaction having dropped D-3's answer.
  Re-emit `writeInferGuidance` when this run's mode differs from the previous run's: a run-one
  `/spike` followed by a run-four infer still needs it.
- **Shape it as `PromptInput.Resume bool`, branched on inside `BuildPrompt`** — not a second exported
  builder. One function, one persisted-verbatim prompt, one place to read. `BuildPrompt` stays pure
  (`prompt.go:11-16`).
- **The meta document records less on a resumed run, by design.** `Meta.Prompt` (`meta.go:59-60`) is
  what was actually sent, which on a resumed run is the delta; the rest of the conversation is in the
  transcript. Record which path the run took so the two can be joined — add `resumed bool` to `Meta`
  beside `ClaudeSessionID` (`meta.go:50-51`).
- Platform: Logging — add a structured `path` field (`resume` | `cold` | `cold_after_resume_refused`)
  to the run logger at `runner.go:265-268`, so the fallback is auditable from `daemon.log`. `log/slog`
  only; `forbidigo` is on.
- Platform: Testing — hand-written stubs (`stubRunner`, `internal/turn/fixture_test.go:244`), plain
  `if got != want`, table-driven by default, prompt assertions on substrings and structure as
  `prompt_test.go` already does. No goldens, no testify. `-race` via `make test`.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-SR-2.1 | `Runner.args`: `--resume <id>` instead of `--session-id <id>` when resuming; update the flag rationale block at `runner.go:277-301` | logic | T-SR-1.2 | S |
| T-SR-2.2 | `prepare.go`: decide resume-vs-cold from prior terminal-status runs on this segment via `ListRuns` | logic | T-SR-1.2 | S |
| T-SR-2.3 | `PromptInput.Resume` + the delta header in `BuildPrompt`; re-emit mode guidance on a mode change | logic | T-SR-2.2 | M |
| T-SR-2.4 | `ErrSessionNotResumable` sentinel + exactly one cold rerun on refusal, reusing the run row | logic | T-SR-2.1, T-SR-3.2 | M |
| T-SR-2.5 | `Meta.resumed` + the `path` log field | logic | T-SR-2.1 | XS |
| T-SR-2.6 | Test: the second run of a segment passes `--resume` and **not** `--session-id` — the regression that is broken today | test | T-SR-2.1 | S |
| T-SR-2.7 | Tests: prompt shape resumed vs cold; the resume decision across run counts and statuses | test | T-SR-2.3 | M |
| T-SR-2.8 | Tests: the fallback fires once on the sentinel, does not fire on other non-zero exits, and does not loop | test | T-SR-2.4 | S |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-SR-2.1 | The first run of a segment passes `--session-id` and no `--resume`. Every later run passes `--resume` and no `--session-id`. Asserted on the child's argv. |
| AC-SR-2.2 | A second run of a task starts successfully. A test that fails against `e30257c` covers this — it is the latent defect made executable. |
| AC-SR-2.3 | The resume decision is derived from prior terminal-status runs on the segment. No schema change, no new store accessor. |
| AC-SR-2.4 | A resumed run's prompt omits the issue note, the feature-slug paragraph, the headless-guidance paragraph and the location bullets, and carries the run header, mode, feature slug, instruction and the triggering comment's note. |
| AC-SR-2.5 | A resumed run whose mode differs from the previous run's re-emits the mode guidance. |
| AC-SR-2.6 | `BuildPrompt` remains pure and remains the only builder. No second exported entry point. |
| AC-SR-2.7 | A resume refusal is classified structurally (no init event + non-zero exit) into `ErrSessionNotResumable`, never by matching stderr text. The stderr tail is logged as evidence. |
| AC-SR-2.8 | On refusal the turn reruns **once**, cold, with the compacted history, reusing the same run row and ordinal. It never reruns twice, and never reruns on any other failure. |
| AC-SR-2.9 | The tool boundary is identical on the resume and cold paths — same `--tools`, `--settings`, `--allowedTools`, `--strict-mcp-config`. |
| AC-SR-2.10 | `Meta` records whether the run resumed; the run log line names the path taken. |
| AC-SR-2.11 | `make test` and `make check` are green. |

---

## User Story 3 — A cold start gets the thread, compacted, with prior owner comments defused

> As the owner, I want a run that cannot resume to be given a condensed record of the thread with
> historic owner comments marked as historic, so it starts with context and does not read an old
> `/spike …` as a live command.

The fallback path. It is only reached when the transcript is genuinely gone — and it is the whole
answer to the owner's fourth directive.

### Architecture Notes
- **Deterministic Go, not a second `claude -p` call.** Recommendation, and the reason: a summariser
  call adds cost, latency, a new failure mode, and non-determinism to a prompt this codebase keeps
  byte-reproducible on purpose (`prompt.go:11-16`). Bodies are already capped at 8 KiB each; the real
  problem is the block's *total* size, and a total budget with an explicit truncation marker solves it
  honestly in a way a summary does not. If threads ever grow long enough that elision loses real
  content, a summariser can be added later behind the same `PromptInput` field without moving anything
  else.
- **`[]HistoryEntry` rendered by `BuildPrompt`, not a pre-rendered string from `turn`.** The `<note>`
  framing, `capText` and `neutraliseNoteTags` are `harness`'s single responsibility; a pre-rendered
  block hands framing to `turn`, where it will drift. `HistoryEntry` carries the author handle, a
  harness-defined role constant, the body and the timestamp. `prepare.go` maps
  `store.Classification` → role and does nothing else.
- **The relabelling, which is directive 2.** `instruction` → `old_owner`, `third_party` →
  `old_third_party`, `self` → `litefactory`. The role is what is rendered; the raw handle is rendered
  beside it for traceability. The point is that a historic `/spike …` in a body reads as a record of
  what was asked before, not as this run's instruction — the live instruction is the one and only
  thing under `## Instruction`.
- **One `<note>`, not N.** The whole history is a single note whose `Source:` line says it is a
  condensed record of earlier comments on the issue, each entry prefixed by role and timestamp. N
  sibling notes would triple the framing overhead and blur which note is the triggering one.
- **`MaxHistoryBytes` total budget** on top of the existing per-entry `MaxCommentBytes`, so one giant
  comment cannot eat the block. Fill **newest-first** until the budget is spent, then emit the kept
  entries in **chronological** order with a single `[truncated] N earlier comments omitted` line at
  the top. Reuses `truncationMarker` (`prompt.go:57`); adds no new mechanism.
- **The latest comment is not in the history block.** It is already the triggering comment's own note,
  verbatim and uncapped-by-history. That is the owner's "only take the full latest comment", and it
  falls out of the existing structure rather than needing new code.
- **The block is best-effort over what `comments` holds.** `bootstrap` (`daemon/tick.go:225-251`)
  fetches pre-existing comments, takes the max id and discards the payloads, so comments predating
  litefactory's first sight of the issue are absent. The history says so in its `Source:` line rather
  than silently implying completeness. Fixing the bootstrap gap is out of scope — and would mean
  touching `tick.go`, which the sibling session owns.
- `store.ListComments` (`comments.go:151`) already returns everything needed in forge-id order, which
  is chronological order. Litefactory's own replies are recorded as `self` and carry the run's final
  message via the report comment (`report/run.go:502-517`), so prior runs' output comes back for free.
- Platform: Data handling — the history puts every stored comment body into the prompt, and the prompt
  is persisted verbatim into `NNNN.meta.json`. Bodies are stored unredacted (S-7 is outbound-only), so
  a secret pasted into a comment would land in every subsequent cold-start meta file rather than only
  its own. `metaFileMode` is `0600` (`meta.go:35`). See OD-1.
- Platform: Testing — table-driven for the role mapping and the budget eviction; substring/structure
  assertions for the rendered note.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-SR-3.1 | `harness.HistoryEntry` + the role constants `old_owner` / `old_third_party` / `litefactory` | data | T-SR-1.2 | S |
| T-SR-3.2 | Render the history as one `<note>` in `BuildPrompt`, reusing `capText`/`neutraliseNoteTags`; `Source:` states it is condensed and possibly incomplete | logic | T-SR-3.1 | M |
| T-SR-3.3 | `MaxHistoryBytes`: newest-first fill, chronological emit, one omission marker naming the count | logic | T-SR-3.2 | S |
| T-SR-3.4 | `prepare.go`: read `ListComments`, map classification → role, exclude the triggering comment, populate history on the cold path only | logic | T-SR-3.2, T-SR-2.2 | S |
| T-SR-3.5 | Tests: role mapping table; a historic `/spike` in a body renders as `old_owner` history and is absent from `## Instruction`; `</note>` inside a history body is neutralised | test | T-SR-3.2 | M |
| T-SR-3.6 | Tests: budget eviction drops oldest first, keeps chronological order, emits the marker, and still applies the per-entry cap | test | T-SR-3.3 | S |
| T-SR-3.7 | Test: a resumed run carries **no** history block; a cold run with prior comments carries one | test | T-SR-3.4 | S |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-SR-3.1 | A cold-start run on a task with prior comments receives a single `<note>` holding a condensed record of them, oldest to newest, each entry prefixed by role and timestamp. |
| AC-SR-3.2 | Prior comments classified `instruction` render as `old_owner`; `third_party` as `old_third_party`; `self` as `litefactory`. The raw handle is shown beside the role. |
| AC-SR-3.3 | A historic `/spike …` in the history block appears only there and never under `## Instruction`. A test asserts this exactly. |
| AC-SR-3.4 | The triggering comment appears verbatim in its own note and is not duplicated into the history block. |
| AC-SR-3.5 | The block respects a total `MaxHistoryBytes` budget and the existing per-entry cap. When entries are dropped, the count is stated with the existing `[truncated]` marker. |
| AC-SR-3.6 | `<note>` and `</note>` inside a history body are neutralised by the existing mechanism. No new escaping is introduced. |
| AC-SR-3.7 | A resumed run carries no history block at all. |
| AC-SR-3.8 | `BuildPrompt` still takes structured input and renders it. `internal/turn` never assembles prompt markup. |
| AC-SR-3.9 | The `Source:` line states the record is condensed and may not include comments predating litefactory's first sight of the issue. |
| AC-SR-3.10 | `make test` and `make check` are green. |

---

## Feature-level Acceptance Criteria

- [ ] The second run of a task starts, and continues the first run's conversation.
- [ ] A test that fails against `e30257c` covers the reused-`--session-id` defect.
- [ ] A run whose transcript is gone falls back to a cold start with history, once, and the task is
      never wedged.
- [ ] A historic owner command in the thread cannot be read as a live instruction.
- [ ] No migration. `internal/daemon/tick.go` is byte-identical.
- [ ] The claude CLI contract is written down in-tree with the version it was verified against.
- [ ] `make test` and `make check` are green.

---

## Out of Scope

- **Prompt-injection hardening.** Deferred by the owner, explicitly. The `<note>` framing and the
  `</note>` neutralisation (`prompt.go:264-299`) stay exactly as they are; this plan reuses them
  unchanged and adds nothing.
- **The first-sight bootstrap gap** (`daemon/tick.go:225-251`), which discards pre-first-sight comment
  payloads. It bounds how complete the history block can be, and the block says so. Fixing it means
  touching `tick.go`, which the sibling `fix-comments-pickup` session owns.
- **Multiple segments per session.** `segments.ordinal` stays at `1`; deliberate conversation forking
  is LF-8.
- **`/include cN`** (`turn/classify.go:258-260`) — related, M1, not this.
- **Transcript lifecycle management** — nothing here prunes, rotates or bounds
  `~/.claude/projects/*.jsonl`. A long-lived task's transcript grows without limit and claude's own
  internal compaction is what manages it. Worth revisiting if a task ever runs long enough to matter.

---

## Open — Owner Decisions Required

Two. Both small; neither blocks starting S1.

**OD-1 — the history block puts stored comment bodies into every cold-start `NNNN.meta.json`.**
Bodies are stored unredacted because S-7 redaction is outbound-only, so a secret pasted into a comment
would be copied into later runs' metadata rather than staying in its own. The files are `0600` and
local. *Recommendation: accept as-is and note it in the code.* Running outbound redaction over the
history block is the mitigation if you would rather not — it is cheap, but it also means the model
sees a redacted version of what it was told before, which is its own kind of confusion.

**OD-2 — the value of `MaxHistoryBytes`.** *Recommendation: 32 KiB*, i.e. roughly four full-size
comments, against 8 KiB per entry and 8 KiB for the triggering comment. It is a judgement call, not a
measurement; the cost of it being too large is prompt bytes, and of it being too small is elided
history with the count stated.
