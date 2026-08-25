# Feature Spec: comment-intent — mode inference from issue comments
# Status: APPROVED
# Date: 2026-08-25

## Goal

Let litefactory read the *meaning* of an owner's issue comment — not just a `/spike` prefix — infer the aiteam mode (Research / Plan / Implement / Spike), run the matching orchestrator against the task's frozen session directory, and never leave a comment queue wedged when it cannot decide.

## Binding inputs

| Ref | Decision | Source |
|-----|----------|--------|
| D-CI-1 | The session path's `<date>` is pinned to the task's `created_at`. One task = one session directory, for ever. Gate check stays a filesystem test in the worktree. No cross-date look-back anywhere. | owner, 2026-08-25 |
| D-CI-2 | All four modes are inferrable in the first cut (against the research recommendation, taken knowingly). | owner, 2026-08-25 |
| D-CI-3 | D-X (no rate limiting until LF-11) is re-opened; this feature ships a cost bound. | owner, consequence of D-CI-2 |
| R1 | Inference must have a terminal outcome. "Could not classify" **consumes** and goes quiet. Never defers. | `RESEARCH_BRIEF.md:154-171` |
| Reaffirmed | D-I authorship filter, D-4/S-6 slug derivation + freezing, `UNIQUE (session_id, trigger_comment_id)`, SD-1..SD-9, D-A..D-I. Not re-opened. | `SPIKE_LF3.0_DECISIONS.md`, `IMPLEMENTATION_PLAN.md:15-31` |

## Constraints

- **M0 was exercised end to end against real GitHub on 2026-08-25** (reaction → worktree → branch → `claude -p` → commit → push → report as the bot). This feature changes the trigger path. Every story below must leave the proven `/spike` path byte-identical unless the story says otherwise; a golden test pins the `/spike` prompt.
- `ParseCommand` (`internal/turn/command.go:132`) is pure, total, has no `ctx` and no error. It cannot absorb a model call. The classifier is a **sibling**, not a replacement.
- `AdvanceWatermark` is a monotonic `MAX()`. `drain` (`internal/daemon/tick.go:274-303`) stops a task's queue at the first comment the watermark did not pass. Any new "log it and leave it" path on the trigger route wedges an issue for ever.
- `Filter.Admit` (`internal/turn/classify.go:207-215`) runs first and already restricts triggers to the owner's numeric id. Inference does not touch it and does not widen it.
- `internal/config` is stdlib-only except the YAML decode (`purity_test.go`). Anything new there must stay stdlib-only.
- The classifier reads untrusted text. It gets the same `<note>` framing and neutralisation as `BuildPrompt` (D-N), and its output is constrained to four mode values.

## User Stories

| ID | Story |
|----|-------|
| US-1 | As the owner, I want an empirical measurement of what the `claude` child can actually dispatch (`Task`, sub-agent tool surface, nested `Task`) before the design commits to orchestrator dispatch, so the routing design is measured rather than assumed. |
| US-2 | As the owner, I want a task's aiteam session directory to be the same directory on day two as on day one, so a mode gate does not fail on a task that has genuinely passed it. |
| US-3 | As the owner, I want a plain-English comment on my own issue to be understood and acted on, so I do not have to remember a command prefix. |
| US-4 | As the owner, I want a comment litefactory cannot classify to be consumed and answered with silence, so one ambiguous sentence never wedges an issue's queue. |
| US-5 | As the owner, I want to see at a glance whether a comment was accepted or declined, so a wrong decline is visible without reading a log. |
| US-6 | As the owner, I want `/research`, `/plan` and `/implement` to start real runs instead of replying "not supported yet", so the explicit path and the inferred path do the same thing. |
| US-7 | As the owner, I want the run to be told exactly which aiteam mode it is in, where the session directory is, and where its approval gate is, so the orchestrator neither invents my approval nor computes a different date. |
| US-8 | As the owner, I want a mode whose prerequisite artifact is missing to be declined by name before a run is spent, so an inferred Implement with no plan costs one comment, not one run. |
| US-9 | As the owner, I want an Implement-mode run to be able to commit its own work and still have its files reported, so the mode D-CI-2 unlocked is actually usable. |
| US-10 | As the owner, I want a daily ceiling on what inference can spend, so a misread conversation cannot run the machine all night. |
| US-11 | As a future reader of the M0 spec, I want A-8 and D-X recorded as amended in-tree, so the contradiction is documented rather than silent. |

---

## Data Model

Schema changes ship as one new migration, `internal/store/migrations/0002_comment_intent.sql`. The migration runner is version-ordered and idempotent (`internal/store/migrate.go:41`); no other mechanism is needed.

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| `sessions` | `session_date` | TEXT | yes for new rows | UTC `YYYY-MM-DD`. Written in the same INSERT as `created_at` (`GetOrCreateSession`), from `created_at`. Never updated. Backfilled by 0002 as `substr(created_at,1,10)` for every existing row. |
| `comments` | `intent` | TEXT | no | NULL for comments the classifier never saw. Otherwise one of `research`, `plan`, `implement`, `spike`, `declined`. Written after classification, before the run starts. |
| `comments` | `intent_confidence` | REAL | no | NULL when unknown. 0.0–1.0 as reported by the classifier. |
| `comments` | `intent_reason` | TEXT | no | ≤ 200 chars, classifier's own words, for "why did nothing happen?". Untrusted; never rendered into a forge comment unescaped. |
| `runs` | `mode` | TEXT | yes | Existing column, no CHECK constraint. Value set widens from `spike` to `research \| plan \| implement \| spike`. No migration needed. |
| `runs` | `trigger_source` | TEXT | yes | New. `explicit` (a slash command) or `inferred` (the classifier). Backfilled to `explicit` by 0002. Makes false-positive runs auditable and drives one line of the report body. |

### In-memory values (no persistence)

| Value | Field | Type | Required | Constraints |
|-------|-------|------|----------|-------------|
| `turn.Mode` | — | enum | yes | `ModeResearch \| ModePlan \| ModeImplement \| ModeSpike`. Zero value is invalid and must fail `validate()`, not panic. |
| `turn.Command` | `Mode` | `turn.Mode` | yes when `Spawns()` | Replaces the `Name[len("/"):]` derivation at `execute.go:159-161` (R12). |
| `turn.Command` | `Source` | enum | yes when `Spawns()` | `explicit \| inferred`. |
| Classifier verdict | `Decision` | enum | yes | `act \| decline`. |
| Classifier verdict | `Mode` | `turn.Mode` | yes when `act` | Any other value ⇒ decline. |
| Classifier verdict | `Confidence` | float | yes | Below `inference.min_confidence` ⇒ decline. |
| Classifier verdict | `Reason` | string | no | ≤ 200 chars, truncated on a rune boundary. |

### Relationships

- `sessions` → session directory via `(branch, session_date, feature_slug)`, all three frozen.
- `comments.intent` → `runs.mode` for the run that comment triggered, via `runs.trigger_comment_id`.

### Derived path (single spelling, in `internal/naming`)

- Session directory: `<worktree>/.aiteam/<flattened-branch>/<session_date>/<feature_slug>/`
- Flattened branch: `lf/<issue>-<slug>` → `lf-<issue>-<slug>` (slashes → hyphens, lowercased, per CLAUDE.md).
- One function produces this path. The gate check (US-8) and the prompt (US-7) both call it; they cannot drift.

---

## API / Interface Changes

### Comment vocabulary

| Token | Today | After |
|-------|-------|-------|
| `/spike <text>` | spawns a run | unchanged, byte-identical prompt |
| `/spike` (bare) | reply asking for text (AC-2.20) | unchanged |
| `/research`, `/plan`, `/implement` | `KindUnsupported` → "not supported yet" reply | spawn a run in that mode; removed from `knownCommands` |
| `/research` etc. (bare, no text) | — | spawns; the instruction becomes a litefactory-authored sentence naming the mode and the issue. The orchestrator reads the issue itself, so there is nothing to ask for. |
| `/approve`, `/include`, `/reset`, `/retry`, `/status` | `KindUnsupported` | unchanged |
| `/note <anything>` | `KindUnknown` → reply | **new**: recorded, consumed, silent. The documented way to write a comment inference must not touch. |
| any other `/word` | `KindUnknown` → reply | unchanged |
| free text (owner) | `KindNone`: silent, consumed | goes to the classifier |
| free text (third party / self) | recorded, consumed, silent | unchanged — never classified |

### Forge-visible surface

| Action | When | Auth | Notes |
|--------|------|------|-------|
| 👀 `eyes` reaction | a comment was accepted (explicit or inferred) | bot | existing `AddCommentReaction`; ordering moves so it fires only after acceptance |
| 😕 `confused` reaction | an inferred comment was declined | bot | `github.ReactionConfused` already exists. One API call, no thread noise. Failure is logged, never fatal, never blocks the consume. |
| comment | accepts that produce a report, gate declines, budget declines, unsupported/unknown commands | bot | comments are reserved for accepts and gates; declines carry no comment |
| (nothing) | `/note`, third party, self, budget-declined *inference* below threshold | — | silence stays the default for anything the classifier simply did not want |

### Configuration (new keys, `claude`/`inference`/`budget` sections)

| Key | Default | Notes |
|-----|---------|-------|
| `inference.enabled` | `true` | Kill switch. Takes effect on daemon restart. `false` restores today's `KindNone` silence exactly. |
| `inference.min_confidence` | `0.6` | Below this ⇒ decline. |
| `inference.model` | `haiku` | Passed to the classifier invocation only. Never affects the run. |
| `inference.timeout_seconds` | `60` | Hard wall clock for one classification. Expiry ⇒ decline. |
| `budget.daily_usd` | `25` | Rolling 24 h sum of `runs.cost_usd`. `0` = unlimited, permitted but warned at startup. |
| `budget.daily_runs` | `20` | Rolling 24 h count of runs across all repos. |
| `budget.task_daily_runs` | `6` | Rolling 24 h count of runs on one session. |

### Tool policy (`internal/config/policy.go`)

| List | Additions |
|------|-----------|
| `defaultTools()` | none |
| `defaultAllowedTools()` | `Bash(make:*)`, `Bash(go run:*)`, `Bash(go mod:*)`, `Bash(golangci-lint:*)`, `Bash(gofumpt:*)`, `Bash(git add:*)`, `Bash(git commit:*)`, plus `Task` and `TodoWrite` **iff** US-1 measures them as denied without an entry |
| deliberately absent | `Bash(git push:*)`, `Bash(git checkout:*)`, `Bash(git reset:*)`, `Bash(git rebase:*)`, `Bash(git clean:*)`, `WebFetch`, `WebSearch` |

---

## Stories, in build order

### S1 — Empirical spike on the claude child's dispatch surface (US-1) — **gates S5 and S7**

Precedent: T-LF3.0 measured the child rather than reasoning about it, and produced nine binding amendments.

- [ ] **AC-CI-1.1** A written measurement of whether the child can call `Task` given `--tools` includes it, `--allowedTools` omits it, and `settings.json` sets `defaultMode: "default"` with an empty `allow`.
- [ ] **AC-CI-1.2** A written measurement of whether `--tools` bounds sub-agents, or whether a sub-agent's frontmatter (`research-orchestrator` declares WebFetch/WebSearch; `go-architect` declares "All tools") gives it a wider surface.
- [ ] **AC-CI-1.3** A written measurement of whether a sub-agent can itself call `Task` (nested dispatch), which `implement-orchestrator` requires.
- [ ] **AC-CI-1.4** A written measurement of whether `--settings <file>` suppresses the project's own `.claude/settings.json`, specifically whether `.claude/hooks/check-test-env.sh` (`PreToolUse` on Bash) fires inside a worktree run.
- [ ] **AC-CI-1.5** Findings land in `$session/SPIKE_DISPATCH.md`, each with the exact argv and the observed event stream, not a summary.
- [ ] **AC-CI-1.6** A named fallback is recorded for each negative result. If `Task` is denied: add `Task` to `allowed_tools`; if it is still denied, the prompt routes the orchestrator's *instructions* inline instead of dispatching, and that becomes S5's shape.
- [ ] **AC-CI-1.7** No production code changes in this story. It is measurement only.

### S2 — Frozen session date (US-2, D-CI-1)

- [ ] **AC-CI-2.1** Migration `0002` adds `sessions.session_date`, `comments.intent`, `comments.intent_confidence`, `comments.intent_reason`, `runs.trigger_source`, and backfills all of them.
- [ ] **AC-CI-2.2** Backfill sets `session_date = substr(created_at,1,10)` and `runs.trigger_source = 'explicit'` for every pre-existing row. No row is left NULL where the column is required.
- [ ] **AC-CI-2.3** `GetOrCreateSession` writes `session_date` in the creating INSERT, derived from the same UTC instant as `created_at`. No code path ever updates it.
- [ ] **AC-CI-2.4** A run started on a later calendar day than the session was created uses the session's stored date, not today's. Test: create a session with a `created_at` in the past, run, assert the artifact path.
- [ ] **AC-CI-2.5** A session whose `session_date` is empty (a restored or hand-edited row) has it derived from `created_at` and written once, conditionally, at preparation time — same "fill only while empty, use the returned row" shape as `FreezeSessionSlug` (`sessions.go:197-220`), so two instances converge.
- [ ] **AC-CI-2.6** A `session_date` that is not `YYYY-MM-DD` fails the turn as a reportable failure, never builds a path from it — the same post-condition discipline as `naming.ValidateSlug` on the reuse path (`prepare.go:250`).
- [ ] **AC-CI-2.7** One naming function produces the session directory from `(branch, session_date, feature_slug)`. Both the gate check and the prompt call it.

### S3 — The classifier seam, with a terminal decline (US-3, US-4, US-5, R1)

- [ ] **AC-CI-3.1** Classification runs at exactly one place: where `tick.go:349-355` produces `KindNone` today. Nothing else on the trigger path changes.
- [ ] **AC-CI-3.2** `ParseCommand` stays pure, total, `ctx`-free and error-free. The classifier is a separate call with `ctx` and an error, and it returns one value that the daemon and the reply path cannot disagree about — the `Command` invariant of `command.go:73-77` survives.
- [ ] **AC-CI-3.3** The classifier is one short-lived `claude` invocation: the configured binary, `inference.model`, a minimal tool surface, cwd an empty scratch directory under `$LF_HOME` (never the worktree, never the clone), the litefactory settings file, `--strict-mcp-config`, and `inference.timeout_seconds` as a hard wall clock with the same pgid sweep the runner uses.
- [ ] **AC-CI-3.4** The comment body reaches the classifier wrapped in the same `<note>` framing and tag neutralisation as `BuildPrompt` (D-N, `prompt.go:229-253`). A comment that contains `</note>` cannot escape the frame.
- [ ] **AC-CI-3.5** The classifier's output contract is a single JSON object: `{"decision","mode","confidence","reason"}`. `mode` must be one of the four. Anything else — extra prose, missing field, unknown mode — is a decline.
- [ ] **AC-CI-3.6 (the one that will silently break the daemon if missed)** Every decline path **consumes the comment**: timeout, non-zero exit, unparsable output, unknown mode, confidence below threshold, classifier disabled, binary missing, context cancelled. Terminal, always. There is no `leave()` on this path.
- [ ] **AC-CI-3.7** Test: a task with three comments where the middle one cannot be classified. Assert all three are consumed, the watermark passes all three, and a second tick lists nothing. This is the R1 regression test and it is named as such.
- [ ] **AC-CI-3.8** Test: a classifier that returns an error on every call does not stop the tick, does not fail the task, and does not prevent a later `/spike` on the same issue from running.
- [ ] **AC-CI-3.9** A decline adds the 😕 reaction and posts no comment. A reaction failure is logged and the comment is still consumed.
- [ ] **AC-CI-3.10** An accept adds 👀 before anything that can start a run, preserving the AC-2.17 ordering guarantee.
- [ ] **AC-CI-3.11** The verdict is recorded on the `comments` row (`intent`, `intent_confidence`, `intent_reason`) whether it accepted or declined. Failure to record is logged and does **not** prevent the consume.
- [ ] **AC-CI-3.12** `inference.enabled: false` restores today's behaviour exactly: no model call, no reaction, silent consume. Test asserts zero classifier invocations.
- [ ] **AC-CI-3.13** An accepted inference produces `Command{Kind: <spawning>, Mode: <inferred>, Source: inferred, Text: <the whole trimmed comment body>}`. The instruction is the owner's own words, uncapped, as `PromptInput.Instruction` already is.
- [ ] **AC-CI-3.14** Classification never runs for a comment the filter did not classify as `instruction`. Third-party and self comments never reach it — asserted by a test, since this is the cost and safety boundary.
- [ ] **AC-CI-3.15** A crash between classify and run leaves the comment unconsumed and re-classifies on the next tick. That is acceptable; `UNIQUE (session_id, trigger_comment_id)` still bounds it to at most one run.

### S4 — Explicit slash commands become spawning kinds (US-6)

- [ ] **AC-CI-4.1** `/research`, `/plan` and `/implement` are removed from `knownCommands` and parse to a spawning kind carrying the matching `Mode` and `Source: explicit`.
- [ ] **AC-CI-4.2** Explicit commands never invoke the classifier. Zero model calls, zero added latency — asserted by a test.
- [ ] **AC-CI-4.3** A bare `/research` / `/plan` / `/implement` spawns, with a litefactory-authored instruction naming the mode and the issue. Rationale: the orchestrator reads the issue itself; only `/spike` needs ad-hoc text.
- [ ] **AC-CI-4.4** A bare `/spike` keeps drawing the AC-2.20 reply. Unchanged.
- [ ] **AC-CI-4.5** `/note` is added as a silent, consumed, reaction-free no-op and is documented in the unsupported-command reply text as the way to opt out of inference.
- [ ] **AC-CI-4.6** `/approve`, `/include`, `/reset`, `/retry`, `/status` still reply "not supported yet". Unchanged.
- [ ] **AC-CI-4.7** Case-insensitivity, first-token-only and the `isCommandToken` character restriction are unchanged for every token.

### S5 — Typed Mode and prompt reconstruction (US-7, R7, R12)

- [ ] **AC-CI-5.1** `Request.mode()` no longer slices `Command.Name` (`execute.go:159-161`). Mode is the typed value; an invalid mode is refused by `validate()` with an error, never a panic. Test: a `Command` with an empty `Name` returns an invalid-request error.
- [ ] **AC-CI-5.2** `runs.mode` carries the four modes; `runs.trigger_source` carries `explicit` or `inferred`. The commit trailer and the metadata document say the same thing, from the same value.
- [ ] **AC-CI-5.3** For the three orchestrator modes, the prompt's instruction section **begins with the aiteam slash command**, `/<mode> <feature-slug>`, so the child routes via CLAUDE.md rule 1 rather than improvising through rule 3.
- [ ] **AC-CI-5.4** The prompt states the **absolute session directory** and instructs that it is to be used verbatim — the child must not derive `<date>` itself. Without this, CLAUDE.md's `date=$(date +%Y-%m-%d)` defeats D-CI-1 on day two.
- [ ] **AC-CI-5.5** The headless preamble is replaced, **per mode**, for the three orchestrator modes: the issue comment thread is the approval checkpoint. The orchestrator runs to its gate, writes the artifact, then ends its turn with a summary of exactly what needs confirming. It must not invent the user's approval and must not proceed past a gate.
- [ ] **AC-CI-5.6** Per-mode commit framing: Research and Plan do not commit (litefactory does). Implement may commit its own work and must not push. Every mode is told which.
- [ ] **AC-CI-5.7** The `/spike` prompt is unchanged, byte for byte, including the existing "do not stop for approval / do not commit or push" paragraph. A golden test pins it. This is the M0 regression guard.
- [ ] **AC-CI-5.8** `BuildPrompt` stays pure: no clock, no filesystem, no environment. The session path arrives as an input, already derived.
- [ ] **AC-CI-5.9** `BuildPrompt` validates the new inputs the way it validates the old ones — an empty session path or an invalid mode is an error, not a prompt with a hole in it.
- [ ] **AC-CI-5.10** The report body names the mode and, when `trigger_source = inferred`, says so and quotes the classifier's reason. A false positive must be visible in the thread without opening a log.

### S6 — Pre-spawn gate check (US-8)

- [ ] **AC-CI-6.1** Gate rules: Research and Spike have no prerequisite; Plan requires `RESEARCH_BRIEF.md`; Implement requires `IMPLEMENTATION_PLAN.md` — both in the frozen session directory, checked as a plain filesystem test in the worktree.
- [ ] **AC-CI-6.2** The check runs after the worktree exists and **before the runs row is allocated**, so a decline burns no ordinal and no run.
- [ ] **AC-CI-6.3** A missing prerequisite is a *decline*, not a failure: a distinct disposition that posts a comment, consumes the comment, and does **not** mark the session `failed`. It rides the existing no-run reporting path (`report.go:301-309`, `RunID == 0` ⇒ post then `AdvanceWatermark`).
- [ ] **AC-CI-6.4** The decline comment names the missing artifact, its full path, and the mode that would produce it. It never auto-promotes to the earlier mode — CLAUDE.md's own rule is "never promote silently".
- [ ] **AC-CI-6.5** The rule is identical for explicit and inferred triggers. An explicit `/plan` with no brief declines the same way.
- [ ] **AC-CI-6.6** The check reads the persistent worktree, and a worktree recreated after `task clean` restores the artifacts from the branch — so a cleaned task does not lose a gate it had passed. Test covers it.
- [ ] **AC-CI-6.7** A gate decline is terminal: the comment is consumed and the queue advances even if the post fails in a way that leaves the comment unconsumed — in which case the existing AC-4.10 retry ordering applies unchanged, and the comment is retried, not skipped.

### S7 — Tool policy for orchestrators, and the existing-install upgrade (US-9, R8, R14)

- [ ] **AC-CI-7.1** `defaultAllowedTools()` gains `Bash(make:*)`, `Bash(go run:*)`, `Bash(go mod:*)`, `Bash(golangci-lint:*)`, `Bash(gofumpt:*)` so an orchestrator can verify its own work.
- [ ] **AC-CI-7.2** It gains `Bash(git add:*)` and `Bash(git commit:*)` so Implement's engineers do not report BLOCKED. It does **not** gain `git push`, `git checkout`, `git reset`, `git rebase` or `git clean`.
- [ ] **AC-CI-7.3** Every new entry passes `validateAllowedTools` (`policy.go:88-119`) — qualified, never `Bash`, never `Bash(*)`. A test asserts the whole default list validates.
- [ ] **AC-CI-7.4** `Task` and `TodoWrite` are added to `allowed_tools` if and only if AC-CI-1.1 measured them as denied without an entry.
- [ ] **AC-CI-7.5** The generated `config.yaml` template and `Defaults()` stay in lockstep — the existing drift test still passes.
- [ ] **AC-CI-7.6 (R14)** `init` never overwrites an existing `config.yaml` (`template.go:150-153`), so an existing install keeps the old list. On `init` and on `daemon run` startup, litefactory compares the configured `allowed_tools` against the recommended set and prints/logs a warning naming **exactly** the missing rules and the file to edit. It never edits the operator's file: silently widening a deliberately narrowed permission list is worse than a warning.
- [ ] **AC-CI-7.7** The warning is loud specifically about `Bash(git add:*)` / `Bash(git commit:*)`, because their absence turns an Implement run into a confusing BLOCKED report rather than a clear error.
- [ ] **AC-CI-7.8 (R8)** When the run committed for itself — `commit.Head != shaBefore` while `Committed` is false (`git/commit.go:95-117`, `harvest.go:148-155`) — the reported file list is derived from the diff between `shaBefore` and `Head` instead of the pre-staging `Status`. A self-committing run no longer reports zero changes.
- [ ] **AC-CI-7.9** Push behaviour is unchanged: the branch moving is the test, not the commit happening (AC-3.24). Self-committed work is already pushed and must stay pushed.
- [ ] **AC-CI-7.10** A run that both self-commits and leaves uncommitted files still gets both: litefactory's commit on top, and a file list covering the whole span.

### S8 — Cost bound (US-10, D-CI-3, D-X re-opened)

- [ ] **AC-CI-8.1** Before a run is spawned, litefactory checks three ceilings over a rolling 24 h window: `budget.daily_usd` (sum of `runs.cost_usd`), `budget.daily_runs`, `budget.task_daily_runs`.
- [ ] **AC-CI-8.2** Over ceiling ⇒ terminal decline: a short reply comment naming which ceiling and when it resets, the comment consumed, no run row. Silence would be baffling here; this is rare enough to be worth a comment.
- [ ] **AC-CI-8.3** `cost_usd` is only known after a run and may be NULL (SD-3), so the spend ceiling can overshoot by at most one run. Documented, not engineered around.
- [ ] **AC-CI-8.4** A ceiling of `0` means unlimited, is permitted, and is warned about at startup.
- [ ] **AC-CI-8.5** The ceilings apply to explicit commands too. A `/implement` loop costs the same as an inferred one.
- [ ] **AC-CI-8.6** Classifier invocations are counted and logged per tick, so the inference cost is observable even though it is not separately capped.

### S9 — Spec amendments recorded in-tree (US-11)

- [ ] **AC-CI-9.1** `$session/AMENDMENTS.md` records: **A-8 reversed** (`.aiteam/main/2026-08-23/lf-setup/feature-spec.md:412` — litefactory now does create and read `.aiteam/<branch>/<date>/<feature>/`), with the reason and the date.
- [ ] **AC-CI-9.2** The same file records **D-X superseded** by D-CI-3, with the original justification ("only an explicit `/spike` spends money") and why inference removes it.
- [ ] **AC-CI-9.3** A single "Amended by `<path>`" pointer line is added under A-8 in the M0 spec. The historical spec is not rewritten.
- [ ] **AC-CI-9.4** `CLAUDE.md`'s roster table is corrected from `agents/*.md` to `.claude/agents/*.md` (Q10) — cosmetic interactively, misleading to anything that enumerates them.
- [ ] **AC-CI-9.5** D-CI-1 and D-CI-2 are restated in the amendments file as binding decisions with their consequences, so a future reader does not have to reconstruct them from a research brief.

---

## Business Rules

- Authorship is the safety boundary, not the prefix. `Filter.Admit` runs first and is untouched. A third party's comment is never classified, never triggers, and never costs a model call.
- The bot self-filter (numeric id · `author_is_bot` · `<!-- lf:` marker) and `NewFilter` refusing to construct when bot == owner are untouched. Inference cannot create a loop.
- One comment causes at most one run, ever. `UNIQUE (session_id, trigger_comment_id)` is untouched and is what bounds false-positive cost.
- Every comment on the trigger path ends consumed. The only exceptions remain the pre-existing ones: a contended task lock, and a post that failed before its recording transaction (AC-3.1, AC-4.10).
- The slug and branch are frozen per task (D-4 / AC-3.6). The date joins them. A comment cannot rename its own feature; `--feature` at first-run time only.
- Modes advance from artifacts on disk, not from self-report. `sessions.state` stays `idle | running | failed`; no persisted mode column.
- Explicit beats inferred. A leading slash command is never sent to the classifier.
- Decline is the default outcome of every uncertainty.
- The session directory rides along in the ordinary commit (`git add --all -- .`, `commit.go:148`), on the task branch, pushed before the report comment. No new harvest mechanism.

## Edge Cases & Error Handling

| Case | Expected behaviour |
|------|--------------------|
| Classifier times out | Decline. 😕. Consume. Log at WARN with the comment id. |
| Classifier returns prose instead of JSON | Decline. Consume. The raw output is logged, never posted. |
| Classifier returns a mode that is not one of the four | Decline. Consume. |
| Confidence below threshold | Decline. Consume. |
| `claude` binary missing or unexecutable at classify time | Decline. Consume. Startup validation already covers the run path; this path must not wedge the queue. |
| Context cancelled mid-classification (daemon shutting down) | Comment stays unconsumed, tick ends, next start re-lists it. This is the one non-terminal case and it is the existing shutdown contract, not a classification failure. |
| Two comments in one tick, the first unclassifiable | Both are consumed. The queue does not stop. (AC-CI-3.7) |
| Inferred Implement, no `IMPLEMENTATION_PLAN.md` | Gate decline. Comment names the missing file and says Plan mode produces it. No run. |
| Inferred Plan, no `RESEARCH_BRIEF.md` | Gate decline, same shape. |
| Task worked across two calendar days | Same session directory both days (D-CI-1). Gate passes. |
| Session row predates migration 0002 | `session_date` backfilled from `created_at`. Path unchanged for the life of the task. |
| Orchestrator writes scratch `.md` files in the worktree | They are committed with everything else. Accepted, unchanged from M0; noted for the report reader. |
| Implement run self-commits | Pushed (`Head != shaBefore`), and the file list comes from the diff, not from `Status`. |
| Implement run self-commits *and* leaves uncommitted files | litefactory commits the remainder on top; the report covers both. |
| Report body exceeds 65,536 bytes with an orchestrator touching dozens of files | `redact.Budget` truncates, as today. A test exercises the volume. |
| Budget exhausted | Reply naming the ceiling, consume, no run. |
| `inference.enabled: false` | Exactly today's behaviour: silent consume, no reaction, no model call. |
| Agent definitions absent (any repo that is not litefactory itself) | Out of scope. The orchestrator will not be found. Note it in the amendments file as a known limitation of running against a non-self repo. |

## Out of Scope

- Resuming the claude conversation across comments (segments beyond ordinal 1); whether re-using `--session-id` resumes or resets stays unmeasured.
- The rest of the LF-6/LF-7 vocabulary: `/approve`, `/include`, `/status`, `/retry`, `/reset`.
- `lf:brief-ready` / `lf:plan-ready` labels; draft PR creation.
- Third-party comments as triggers (D-I, settled). Issue **bodies** as triggers (AC-2.16, settled).
- Materialising `.claude/agents/` into a repo that is not litefactory itself (R10).
- Multi-repo / per-repo team behaviour; `repos.aiteam_team` stays detected and unused.
- Any change to the D-I authorship filter, D-4/S-6 slug derivation, or `UNIQUE (session_id, trigger_comment_id)`.
- Hot config reload. New keys take effect on daemon restart.

## Acceptance Criteria (feature-level)

- [ ] A plain-English comment from the owner asking for research starts a Research-mode run, and `RESEARCH_BRIEF.md` lands in the task's session directory, is committed to the task branch, and is pushed before the report comment.
- [ ] The same comment on the following day lands in the **same** session directory.
- [ ] A comment the classifier cannot classify is consumed, gets 😕, gets no comment, and the next comment on that issue is processed in the same tick.
- [ ] `/spike <text>` behaves exactly as it did on 2026-08-25, prompt bytes included.
- [ ] `/plan` on a task with no research brief posts a comment naming the missing file and spends no run.
- [ ] An Implement-mode run can commit its own work, is pushed, and its report lists the files it changed.
- [ ] The daily ceilings decline a run and say which ceiling was hit.
- [ ] `inference.enabled: false` reproduces M0 behaviour with zero classifier invocations.
- [ ] `make test` and `make lint` are green; the existing daemon, turn, store and harness suites pass unmodified except where a story explicitly changes their expectations.

---

## Open decisions for the owner

**OD-1 — WebFetch / WebSearch for Research mode.**
`research-orchestrator` declares `WebFetch` and `WebSearch` in its frontmatter; neither is in `defaultTools()`, so (pending AC-CI-1.2) it may partially fail. Either add them to the `--tools` boundary and accept a network egress channel from a worktree containing the repo's source, or leave them out and accept that inferred Research is codebase-only.
**Recommendation: leave them out.** The M0 research for this very feature was done without them, and `--tools` is the one boundary SD-2 measured as real.

**OD-2 — Classifier mechanism and model.**
The design above reuses the `claude` binary one-shot with `--model haiku`, which adds no new credential and inherits the existing settings and MCP lockdown. The alternative is a direct Anthropic API call, which is cheaper and faster to parse but introduces an API-key dependency the product does not have today.
**Recommendation: the `claude` binary.** Confirm the model alias, since it is the per-comment cost driver.

**OD-3 — The three budget ceilings.**
`daily_usd: 25`, `daily_runs: 20`, `task_daily_runs: 6` are placeholders chosen to be non-annoying for one person working on one repo.
**Recommendation: ship these, revisit after a week of real traffic.** They are config, not code.
