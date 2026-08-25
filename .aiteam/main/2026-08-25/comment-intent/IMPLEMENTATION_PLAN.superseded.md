# Implementation Plan — comment-intent
# Branch: main
# Date: 2026-08-25

Scope: litefactory reads the **meaning** of an owner's issue comment — not just a `/spike` prefix — infers the aiteam mode (Research / Plan / Implement / Spike), runs the matching orchestrator against the task's frozen session directory, and never leaves a comment queue wedged when it cannot decide.

Inputs: `RESEARCH_BRIEF.md` (incl. owner decisions D-CI-1, D-CI-2), `feature-spec.md`, architect review (2026-08-25).

---

## Settled Decisions

Do not re-open these during implementation.

| # | Decision |
|---|---|
| D-CI-1 | The session path's `<date>` is pinned to the task's `created_at`. One task = one session directory, for ever. The gate check is a plain filesystem test in the worktree. No cross-date look-back anywhere. *(owner)* |
| D-CI-2 | All four modes are inferrable in the first cut — deliberately against the research recommendation, taken knowingly after the risk was stated. *(owner)* |
| D-CI-3 | D-X (no rate limiting until LF-11) is superseded. This feature ships a cost bound. *(consequence of D-CI-2)* |
| D-CI-4 | The frozen date is **UTC**, derived from `created_at`. *(architect)* Rationale: `naming` must stay clock-free and dependency-free; the daemon's TZ is not the operator's shell TZ (`TZ` is not in the SD-4 env allowlist); and UTC's cost is cosmetic and bounded (an evening task gets a "tomorrow" directory, then never moves) whereas local's cost is unbounded (DST/timezone changes re-derive the *same stored instant* differently — the exact day-two failure D-CI-1 exists to prevent). |
| D-CI-5 | `<feature>` in the session path is the **bare title-derived slug** (`add-a-widget`), not `<issue>-<slug>`. `naming.MaxSlugLength`'s own doc states the issue number "is never counted against this" and is a branch-level concern. Full path: `.aiteam/lf-<issue>-<slug>/<utc-date>/<slug>/`. |
| D-CI-6 | `Bash(make:*)` does **not** ship. The Go toolchain commands are enumerated individually. *(architect — see S7 notes)* |
| D-CI-7 | The classifier is a second short-lived `claude -p` invocation, not a direct API call. Per SD-4 this machine authenticates via macOS Keychain with `apiKeySource: "none"`; a direct HTTP call needs a credential the product does not have, and a stale one overrides Keychain auth and causes the SD-3 indefinite hang. |

**Reaffirmed, not re-opened:** the D-I authorship filter (`turn/classify.go` must not be touched at all), D-4/S-6 slug derivation and freezing, `UNIQUE (session_id, trigger_comment_id)`, SD-1..SD-9, D-A..D-I.

**Amended:** A-8 (`lf-setup/feature-spec.md:412`) is reversed — litefactory now creates and reads `.aiteam/<branch>/<date>/<feature>/`. D-X is superseded by D-CI-3. Both are recorded in-tree by S9.

---

## Global Invariants

Inherited from `lf-setup/IMPLEMENTATION_PLAN.md:31-70`. Violations are review blockers.

- **No new package.** The package list is enumerated in the invariants; one decision function does not earn an entry. `pkg/` must not be created.
- **`internal/turn/classify.go` is untouched.** It is the safety boundary (D-I). Inference does not widen it.
- **Goroutine inventory stays four.** The classifier's two stream readers are bounded by its own call and exit via the existing close-and-`ioGrace` mechanism. No fifth long-lived goroutine — in particular, no background budget refresher.
- **No `context.Background()` in any request path.** The classifier's timeout derives from the tick's context.
- **Exactly one `recover` in the tree**, in `internal/redact`. Do not add a second.
- **`internal/naming` and `internal/config` validation stay at zero third-party imports** (AST-enforced by `config/purity_test.go`). `naming` also stays clock-free and I/O-free.
- **`--dangerously-skip-permissions` is never constructible.** `validateNoForbiddenFlag` (`config/policy.go:131-151`) must be extended to cover `inference.model`, which is new argv surface.
- **`go test -race` is mandatory.** Table-driven for `naming` and the command parser. Fakes: `claude` as a shell script on a test-controlled PATH; real temp git repo fixtures.
- **Regression budget:** M0 was exercised end to end against real GitHub on 2026-08-25. The `/spike` path must stay byte-identical, pinned by a golden test (AC-CI-5.7).

---

## Build Order

The spec's stated order is wrong in one place: **S3 must follow S4**, because S3 produces a `Command` carrying the typed `Mode`/`Source` that only S4 defines.

| Wave | Lanes (parallel within a wave) |
|---|---|
| 0 | **S1** ‖ **S2** ‖ **S9** — no file overlap; S1 and S9 touch no production code |
| 1 | **S4** ‖ **S7** — S4 defines the types everything downstream needs; S7's files are disjoint |
| 2 | **S5** — needs S2's naming function and S4's `Mode`; owns `prepare.go` |
| 3 | **S3** — needs S4's types; takes `tick.go` |
| 4 | **S6**, then **S8** — both touch `tick.go` *and* `prepare.go`; strictly serial |

**Serial spine: S4 → S5 → S3 → S6 → S8.** S1, S2, S7, S9 hang off it.

**S1 does not gate S5.** It gates exactly two things: AC-CI-7.4 (whether `Task`/`TodoWrite` need `allowed_tools` entries) and the choice of one per-mode sentence, which S5 implements as a single flippable constant.

**File ownership** (the mitigation for the two prior `internal/cli` collisions — each engineer edits only its own files):

| Story | Owns exclusively |
|---|---|
| S1 | *(none)* — `$session/SPIKE_DISPATCH.md` |
| S2 | `store/migrations/0002_*.sql`, `store/sessions.go`, `store/comments.go`, `naming/paths.go` |
| S3 | `turn/infer.go` **(new)**, `harness/classify.go` **(new)**, `daemon/tick.go` |
| S4 | `turn/command.go`, `report/reply.go` |
| S5 | `harness/prompt.go`, `turn/execute.go`, `turn/prepare.go`, `report/run.go` |
| S6 | `turn/report.go` — shares `prepare.go`, `tick.go` |
| S7 | `config/policy.go`, `config/template.go`, `turn/harvest.go`, `git/commit.go` |
| S8 | `store/runs.go` — shares `tick.go`, `config` |
| S9 | *(docs only)* |

---

## User Story 1 — Measure the claude child's dispatch surface

> As the owner, I want an empirical measurement of what the `claude` child can actually dispatch, so the routing design is measured rather than assumed.

Establishes whether `Task` fires, whether `--tools` bounds sub-agents, and whether the project's `PreToolUse` hook runs inside a worktree — before the design commits to orchestrator dispatch.

### Architecture Notes
- **No production code, and no gate on S5.** S1 blocks only AC-CI-7.4 and the choice of one per-mode sentence. Build S5 with both variants behind a single package-level constant in `harness/prompt.go`; S1 flips it.
- **AC-CI-1.4 is the highest-value measurement, not AC-CI-1.1.** `--settings` is documented as *additive* (`harness/settings.go:24-27`) and `.claude/hooks/check-test-env.sh` sits at a **relative** path inside the committed `.claude/`, so the `PreToolUse` Bash hook very likely fires on every Bash call in a worktree run. Latent today (it exits 0 for plain `go test`; no `//go:build integration` files exist), but it is the hardest failure to diagnose from a run log.
- **Partial prior art exists:** `harness/doc.go:44-47` already records *"Verified: a run with `--settings` still reported the operator's agents in its init event."* The spike must therefore distinguish whether an orchestrator resolves from the **worktree's** `.claude/agents/` or the **operator's** `~/.claude/` — otherwise the feature will appear to work on a non-self repo for the wrong reason.
- **If AC-CI-1.2 finds sub-agent frontmatter widens the surface past `--tools`, that is an amendment to SD-2**, not a note in a spike file. SD-2 is the measured basis for calling `--tools` the real boundary; escalate rather than file.
- Platform: Testing conventions — record **exact argv and observed event streams**, not summaries, per the T-LF3.0 precedent that produced nine binding amendments.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-CI-1 | Measure `Task` dispatch, sub-agent tool surface, nested `Task` under the litefactory argv | infra | — | M |
| T-CI-2 | Measure `--settings` additivity; whether `.claude/hooks/check-test-env.sh` fires in a worktree run; whether agents resolve from the worktree or `~/.claude` | infra | — | S |
| T-CI-3 | Write `$session/SPIKE_DISPATCH.md`: exact argv, observed event streams, named fallback per negative result | infra | T-CI-1, T-CI-2 | S |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-CI-1.1 | A written measurement of whether the child can call `Task`, given `--tools` includes it, `--allowedTools` omits it, and settings set `defaultMode: "default"` with an empty `allow`. |
| AC-CI-1.2 | A written measurement of whether `--tools` bounds sub-agents, or whether frontmatter (`research-orchestrator` declares WebFetch/WebSearch; `go-architect` declares "All tools") grants a wider surface. |
| AC-CI-1.3 | A written measurement of whether a sub-agent can itself call `Task` (nested dispatch), which `implement-orchestrator` requires. |
| AC-CI-1.4 | A written measurement of whether `--settings` suppresses the project's own `.claude/settings.json`, specifically whether the `PreToolUse` Bash hook fires inside a worktree run. |
| AC-CI-1.5 | Findings land in `$session/SPIKE_DISPATCH.md`, each with exact argv and the observed event stream. |
| AC-CI-1.6 | A named fallback is recorded per negative result. If `Task` is denied: add it to `allowed_tools`; if still denied, the prompt names the orchestrator file for the child to `Read` (inline routing). |
| AC-CI-1.7 | No production code changes in this story. |
| AC-CI-1.8 | The report distinguishes whether an orchestrator definition resolved from the worktree or from `~/.claude`. |

---

## User Story 2 — Frozen session date

> As the owner, I want a task's session directory to be the same directory on day two as on day one, so a mode gate does not fail on a task that has genuinely passed it.

Pins the `<date>` component to the session's creation instant (D-CI-1) and gives the whole feature one authoritative path spelling.

### Architecture Notes
- **Keep `session_date` as a column — but not for the reason the spec gives.** A hand-edited `session_date` moves the path just as a hand-edited `created_at` would. The real reasons: (1) the codebase has already decided this exact question the other way — `feature_slug` is *deterministically derivable* (D-4: "recomputable after DB loss") and is nonetheless frozen in a column, because a derived-on-read identity component drifts when its source changes; `session_date` joins the same identity tuple. (2) `substr(created_at,1,10)` hardcodes knowledge of `store.FormatTime`'s layout (`store.go:187-194`) in a second place.
- **Cut AC-CI-2.5** (the conditional repair-while-empty path). `FreezeSessionSlug` needs that shape because the slug is computed at *first run*, after the row exists, so two daemons can race. `session_date` is written **in the same INSERT as `created_at`**, and 0002 backfills every pre-existing row inside the migration transaction. There is no legitimate empty window. Two mechanisms covering one impossible state is worse than one — and the repair mechanism is the one that can silently produce a *wrong* path. AC-CI-2.6 is the better handler.
- **SQLite cannot `ALTER TABLE ADD COLUMN … NOT NULL` without a `DEFAULT`.** The spec's data model as written would fail on first run. Use `runs.trigger_source TEXT NOT NULL DEFAULT 'explicit'` (a true default *and* the backfill value) and `sessions.session_date TEXT NOT NULL DEFAULT ''` — the `''` is a lie about the domain, so state in the migration comment that the real enforcement is the Go-side post-condition of AC-CI-2.6. **No `CHECK` constraints** — SQLite needs a full table rebuild for those.
- **0002 is the repo's first-ever second migration.** `0001_init.sql` is currently the only file; the `applied[version]` skip path, `SchemaVersion() > 1`, and applying to a *populated* database have never executed. That is untested infrastructure, not untested feature code, and it gets its own test task. The applier itself is safe: `//go:embed migrations/*.sql` picks the file up automatically, and each migration runs in one transaction (`migrate.go:112-129`) with SQLite's transactional DDL.
- **Add an index on `runs(started_at)`.** S8's rolling-24h budget aggregate would otherwise table-scan, and `SetMaxOpenConns(1)` means it serialises against every other database operation.
- Platform: Error handling — an invalid `session_date` is a **new condition callers branch on**; it needs a sentinel/typed error beside `naming.ErrInvalidPath`, checked with `errors.Is`, never a string match.
- Platform: Dependency management — `naming.SessionDir` takes strings and joins them. It must not reach for `time`.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-CI-4 | Migration `0002_comment_intent.sql`: five columns with legal SQLite defaults, backfills, and an index on `runs(started_at)` | data | — | M |
| T-CI-5 | `GetOrCreateSession` writes `session_date`; store read/write for `intent`/`intent_confidence`/`intent_reason`/`trigger_source` (`SetCommentIntent` — `RecordComment` returns existing rows unchanged and cannot carry it) | data | T-CI-4 | S |
| T-CI-6 | `naming.SessionDir(branch, sessionDate, slug)` + `ValidateSessionDate`, zero-dependency and clock-free | logic | — | S |
| T-CI-7 | Test: the applier's second-migration path — populated DB, backfill values, idempotent re-run | test | T-CI-4 | M |
| T-CI-8 | Test: `naming.SessionDir` table-driven + fuzz; frozen date across calendar days | test | T-CI-6, T-CI-5 | S |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-CI-2.1 | Migration `0002` adds `sessions.session_date`, `comments.intent`, `comments.intent_confidence`, `comments.intent_reason`, `runs.trigger_source`, plus an index on `runs(started_at)`. |
| AC-CI-2.2 | Backfill sets `session_date = substr(created_at,1,10)` and `trigger_source = 'explicit'` for every pre-existing row. No required column is left NULL or empty. |
| AC-CI-2.3 | `GetOrCreateSession` writes `session_date` in the creating INSERT, from the same UTC instant as `created_at`. No code path ever updates it. |
| AC-CI-2.4 | A run started on a later calendar day than the session was created uses the stored date. Test: a session with a past `created_at`, run, assert the artifact path. |
| AC-CI-2.6 | A `session_date` that is not `YYYY-MM-DD` fails the turn as a reportable failure and never builds a path — the same post-condition discipline as `naming.ValidateSlug` on the reuse path (`prepare.go:250`). |
| AC-CI-2.7 | **One** function produces the session directory from `(branch, session_date, feature_slug)`. The gate check and the prompt both call it; neither derives a date. |
| AC-CI-2.8 | Applying `0002` to a database populated under `0001` preserves every existing row, and a second `Migrate` is a no-op returning nil. |

*(AC-CI-2.5 cut — see Architecture Notes.)*

---

## User Story 3 — The classifier seam, with a terminal decline

> As the owner, I want a plain-English comment on my own issue to be understood and acted on, and one I cannot be understood to be consumed silently, so no ambiguous sentence ever wedges an issue's queue.

Adds the model call where `KindNone` is produced today, and guarantees every outcome is terminal.

### Architecture Notes
- **`turn.Infer` returns an error *only* for `ctx.Err()`.** Timeout, non-zero exit, unparsable output, unknown mode, low confidence, disabled, missing binary — all fold into `Verdict{Decision: Decline}` **inside** `Infer`. This makes AC-CI-3.6 a property of the type signature rather than of the caller's discipline. It is the single highest-value structural decision in the feature: `leave()` (`tick.go:470-485`) *is* the "comment stays unconsumed" primitive — that is its documented job — so a classifier error reaching it is precisely the R1 wedge. Document the contract on the function.
- **Ordering is the whole game: record intent → react → consume, consume last.** The intent row is the only durable answer to "why did nothing happen", so it is written first; the reaction is cosmetic and best-effort; the consume is the point of no return. **The reaction must never be attempted after the consume** — a `RateLimitError` arriving afterwards would be handed to `leave()`, which correctly propagates rate limits to end the tick, but with the comment already gone. Everything between classification and the consume is log-and-continue; the only permitted early return is `ctx.Err() != nil`.
- **Package split, mirroring the existing `turn.Runner` / `harness.Runner` seam.** Decision logic in `internal/turn/infer.go` beside `ParseCommand`; subprocess mechanics in a new concrete `harness.Classifier`; a **one-method** interface declared in `turn` at the point of consumption (`execute.go:19-27` is the template: *"the harness exports a concrete struct, and the one narrow interface a turn needs belongs to the turn"*). No `internal/intent` — `turn` has no subprocess code by design and `harness` is documented as "the only place in litefactory that executes the claude binary, and the real security boundary of the product".
- **Name it to avoid a collision.** `daemon.Classifier` **already exists** (`daemon.go:58-62`) and means the *authorship* filter (`Admit`). The intent seam must not reuse that name, or the two "classifiers" become permanently confusable in a codebase where that distinction is the safety boundary.
- **`harness.Classifier` is a separately constructed type, not a `Runner` mode.** `Runner` holds `timeout` as a construction-time field (`runner.go:128-138`) and derives a run-log path and a `--session-id` from a `RunOrdinal` the classifier does not have. It reuses `env.go` (SD-4 allowlist — never a second allowlist), `kill.go` (SD-1 subtree pgid sweep) and `stream.go` (SD-6 `bufio.Reader`, never `Scanner`). Omit `--session-id` entirely; if one proves necessary, derive it from a distinct namespace (`"<ref>/classify/<comment-id>"`) so it can never collide with the run's segment id.
- **The classifier's `--tools` is a separate config key holding one inert tool.** Two traps: omitting `--tools` means **allow-all** (`policy.go:67-69`), so "no tools" is not expressible by omission; and reusing `claude.tools` would hand the classifier `Bash`, `Edit`, `Write` and `Task`. `TodoWrite` is the obvious single entry. Its cwd is a dedicated `$LF_HOME/classify/<comment-id>/` at 0700 — **never `$LF_HOME` itself**, which holds `litefactory.db` (every issue body and comment, unencrypted).
- **`inference.timeout_seconds` needs its own range**, not `claude.timeout_seconds`' 60–86400 floor (`config/validate.go:22-25`), which would make "cheap and fast" bottom out at 60s. Recommend 5–120.
- Platform: Concurrency — the two stream readers are bounded by the call and exit via the existing close-and-`ioGrace` mechanism. No fifth long-lived goroutine.
- Platform: Logging — `log/slog` with structured fields. The classifier's **raw stdout is logged at DEBUG only, never posted**: it is untrusted text and `daemon.log` is unredacted and unrotated (accepted M0 debt; do not add `lumberjack`, explicitly rejected).
- Security: the untrusted body **selects a mode**, so injection here escalates rather than exfiltrates. Mitigations: the `<note>` framing and `neutraliseNoteTags` (already built and tested, `prompt.go:236-250`), plus a closed output contract. **`encoding/json` silently ignores unknown fields**, so "extra prose ⇒ decline" is not free — the parser must reject on any unexpected top-level key.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-CI-22 | `internal/config` `inference` block: enabled, min_confidence, model (charset-restricted, added to `validateNoForbiddenFlag`), own timeout range, separate classifier tool key | infra | — | S |
| T-CI-23 | `harness.Classifier`: argv, own timeout, scratch cwd, SD-4 env, SD-1 pgid sweep, SD-6 stream read | logic | T-CI-22 | L |
| T-CI-24 | `turn.Infer` + `turn.Verdict`: note framing, strict JSON contract, every failure folds to Decline | logic | T-CI-23, T-CI-9 | M |
| T-CI-25 | `daemon` inference branch: record → react → consume ordering, terminal on every decline; `TickResult` counters | logic | T-CI-24, T-CI-5 | M |
| T-CI-26 | **Test (R1 regression, named as such):** three comments, middle unclassifiable — watermark past all three, `Deferred == 0`, second tick lists nothing | test | T-CI-25 | M |
| T-CI-27 | Test: always-erroring classifier does not stop the tick, does not fail the task, later `/spike` still runs | test | T-CI-25 | S |
| T-CI-28 | Test: `inference.enabled: false` ⇒ zero invocations; third-party/self never classified; explicit never classified | test | T-CI-25, T-CI-10 | S |
| T-CI-29 | Test: `harness.Classifier` against a fake `claude` script — timeout, prose output, non-zero exit, missing binary | test | T-CI-23 | M |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-CI-3.1 | Classification runs at exactly one place: where `tick.go:349-355` produces `KindNone` today. Nothing else on the trigger path changes. |
| AC-CI-3.2 | `ParseCommand` stays pure, total, `ctx`-free and error-free. The classifier returns the same `Command` value type, so the daemon and the reply path cannot disagree (`command.go:73-77`). |
| AC-CI-3.3 | The classifier is one short-lived `claude` invocation: configured binary, `inference.model`, a minimal non-empty tool surface, cwd an empty scratch dir under `$LF_HOME` at 0700, the litefactory settings file, `--strict-mcp-config`, its own hard wall clock with the SD-1 pgid sweep. |
| AC-CI-3.4 | The comment body reaches the classifier wrapped in the same `<note>` framing and tag neutralisation as `BuildPrompt` (D-N). A body containing `</note>` cannot escape the frame. |
| AC-CI-3.5 | The output contract is a single JSON object `{"decision","mode","confidence","reason"}`; `mode` ∈ the four. Extra prose, a missing field, an unknown mode, **or any unexpected top-level key** ⇒ decline. |
| AC-CI-3.6 | **(the one that will silently break the daemon if missed)** Every decline path consumes the comment: timeout, non-zero exit, unparsable output, unknown mode, low confidence, disabled, binary missing. Terminal, always. There is no `leave()` on this path. |
| AC-CI-3.7 | Test: three comments, the middle unclassifiable. All three consumed, watermark past all three, `Deferred == 0`, a second tick lists nothing. Asserted against the **watermark read back from the store**, never a return value. |
| AC-CI-3.8 | Test: a classifier erroring on every call does not stop the tick, does not fail the task, and does not prevent a later `/spike` on the same issue. |
| AC-CI-3.9 | A decline adds 😕 and posts no comment. A reaction failure is logged and the comment is still consumed. |
| AC-CI-3.10 | An accept adds 👀 before anything that can start a run, preserving AC-2.17 ordering. |
| AC-CI-3.11 | The verdict is recorded on the `comments` row whether it accepted or declined. Failure to record is logged and does **not** prevent the consume. |
| AC-CI-3.12 | `inference.enabled: false` restores today's behaviour exactly — no model call, no reaction, silent consume. Asserted by an invocation counter reading zero. |
| AC-CI-3.13 | **(amended)** An accepted inference produces `Command{Kind: <spawning>, Mode: <inferred>, Source: inferred}` whose `Text` is a **litefactory-authored sentence** naming the mode and pointing at the comment note — not the raw body. See the security note in S5. |
| AC-CI-3.14 | Classification never runs for a comment the filter did not classify as `instruction`. Asserted by test — this is the cost and safety boundary. |
| AC-CI-3.15 | A crash between classify and run leaves the comment unconsumed and re-classifies next tick. Accepted: `UNIQUE (session_id, trigger_comment_id)` still bounds it to one run. No durable "classification started" marker — that would trade a bounded cost for an unbounded one. |
| AC-CI-3.16 | The intent seam does not reuse the name `Classifier` at the `daemon` boundary, which already means the authorship filter. |

---

## User Story 4 — Explicit slash commands become spawning kinds

> As the owner, I want `/research`, `/plan` and `/implement` to start real runs instead of replying "not supported yet", so the explicit path and the inferred path do the same thing.

Defines the typed `Mode` and `Source` the rest of the feature depends on.

### Architecture Notes
- **Build S4 before S3.** S3 produces a `Command` carrying a spawning `Kind` plus typed `Mode` and `Source`, and only S4 defines them. The spec's stated order would have the types invented twice and reconciled.
- **`ModeInvalid` is the zero value (`iota`).** A struct literal that forgets the field must fail `validate()` rather than silently defaulting to Research. Note `validate()` (`execute.go:147-150`) **also** requires non-empty `Command.Text` — a fact the brief missed — so a bare `/research` with no trailing text would be rejected as *invalid* rather than handled; AC-CI-4.3's authored sentence is what prevents that.
- **`Command` stays one value with one meaning.** `Source` rides on the same struct; do not thread it separately, or the two callers can disagree.
- **`report/reply.go:170`'s `onlyCommandSentence` becomes false the moment four modes ship.** It hardcodes *"The only command it acts on at the moment is `/spike <what you want tried>`."* and is deliberately a single constant so all three replies change together. Fix it in the story that makes it false. `reply_unsupported.golden` also uses `/plan` as its example — a token that becomes *spawning* — so the fixture must switch to a still-unsupported one (`/approve`).
- Platform: Testing conventions — the command parser is named in the Global Invariants as table-driven. Extend the existing table; do not add parallel test functions. `knownCommands` is documented as sorted — keep it sorted after the removals.
- Platform: Error handling — `ParseCommand` stays total and error-free. Nothing here may introduce an error return.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-CI-9 | `turn.Mode` and `turn.Source` typed enums with `Valid()`/`String()`, invalid zero value | logic | — | S |
| T-CI-10 | `ParseCommand`: `/research`/`/plan`/`/implement` spawn; `/note` silent no-op; shrink `knownCommands` | logic | T-CI-9 | M |
| T-CI-11 | Fix `onlyCommandSentence`; document `/note` as the inference opt-out; re-point the `/plan` reply fixtures at `/approve` | logic | T-CI-10 | S |
| T-CI-12 | Test: command parser table extended — all tokens, bare forms, case-insensitivity, `isCommandToken` rejections | test | T-CI-10 | S |
| T-CI-13 | Test: golden bodies for the unsupported/unknown/bare-spike replies | test | T-CI-11 | XS |
| T-CI-44 | Rewrite `TestRunlessCommandsReplyAndAdvanceTheWatermark` (`daemon/tick_test.go:472-490`) — this feature inverts both behaviours it pins | test | T-CI-10, T-CI-25 | S |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-CI-4.1 | `/research`, `/plan`, `/implement` leave `knownCommands` and parse to a spawning kind carrying the matching `Mode` and `Source: explicit`. |
| AC-CI-4.2 | Explicit commands never invoke the classifier. Zero model calls, zero added latency — asserted by the invocation counter. |
| AC-CI-4.3 | A bare `/research` / `/plan` / `/implement` spawns, with a litefactory-authored instruction naming the mode and the issue. The orchestrator reads the issue itself; only `/spike` needs ad-hoc text. |
| AC-CI-4.4 | A bare `/spike` keeps drawing the AC-2.20 reply. Unchanged. |
| AC-CI-4.5 | `/note` is a silent, consumed, reaction-free no-op, documented in the reply copy as the way to opt out of inference. |
| AC-CI-4.6 | `/approve`, `/include`, `/reset`, `/retry`, `/status` still reply "not supported yet". |
| AC-CI-4.7 | Case-insensitivity, first-token-only and the `isCommandToken` character restriction are unchanged for every token — including that a leading path (`/usr/bin/security …`) is still not a command, and now routes to the classifier. |
| AC-CI-4.8 | No user-facing copy claims `/spike` is the only actionable command. |

---

## User Story 5 — Typed Mode and prompt reconstruction

> As the owner, I want the run told exactly which aiteam mode it is in, where the session directory is, and where its approval gate is, so the orchestrator neither invents my approval nor computes a different date.

Resolves R7 and R12, and makes D-CI-1 actually take effect in the child.

### Architecture Notes
- **`Request.mode()` is deleted, not guarded.** `Command.Name[len("/"):]` (`execute.go:159-161`) panics on an empty `Name` and is called at **three** sites (`prepare.go:130`, `:166`, `:344`). One spelling — `Mode.String()` — then feeds `runs.mode`, the commit trailer (`git.TrailerMode`, via `runs.mode` at `harvest.go:137`) and `NNNN.meta.json` (`harness/meta.go:52-53,106`), which is exactly what the deleted function's own comment intended. Never `panic` for a bad input value.
- **Pinning the date in the database is worthless without the prompt half.** CLAUDE.md:55-60 has the child compute `date=$(date +%Y-%m-%d)` itself, at runtime, in local time — and CLAUDE.md:42 tells the dispatcher to *ask the user* for the feature name, a blocking question in headless mode. The prompt must state the **absolute session directory**, say it is UTC-derived, and instruct that it be used verbatim. Without this, D-CI-1 is defeated on day two.
- **`BuildPrompt` stays pure** — no clock, no filesystem, no environment (its own doc comment; this is what makes the goldens byte-exact). The session directory arrives pre-derived. Consequently the inline-orchestrator fallback **names a path for the child to `Read`**, it never embeds file contents — which is what lets the dispatch and inline variants differ by exactly one constant sentence, and is why S1 does not gate this story.
- **Security — amend AC-CI-3.13.** The spec routed the raw comment body into `PromptInput.Instruction`, the one field `BuildPrompt` documents as **trusted, uncapped and un-neutralised** (`prompt.go:38-42`). For `/spike` that is right — the owner typed a deliberate instruction. For an *inferred* comment it is conversational text arriving verbatim in **both** the trusted slot and the untrusted `<note>`, with the trusted copy bypassing `</note>` neutralisation and the 8 KB cap. Fix: `Instruction` becomes a litefactory-authored sentence pointing at the comment note. The owner's words still reach the model where they already do today. This also makes the inferred and bare-explicit paths structurally identical — one prompt shape to pin, not two.
- **Gate the orchestrator sentence on a `.claude/agents/` probe.** `repos.DetectAiteam` (`repos/aiteam.go:106+`) checks `CLAUDE.md` and `.aiteam/` but never `.claude/agents/`, so it cannot tell whether a repo carries the definitions this feature depends on. ~15 lines converts R10 from a confusing run against a non-self repo into a clean fall-back to plain Spike framing.
- **The `/spike` golden is byte-for-byte** (AC-CI-5.7) and is the M0 regression guard. `TestBuildPromptIsDeterministic` already establishes the property; `internal/report` already has a golden harness with `-update` to copy. A substring assertion is not a guard. Note `TestBuildPromptStatesTheFeatureSlugAndForbidsQuestions` asserts the very "do not ask clarifying questions" preamble that changes per mode — it must be split, not deleted.
- **The prompt has "three jobs" per its doc comment**; this feature adds a fourth (state the session path and the gate contract). Document it in the same place so the rationale does not rot.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-CI-14 | Delete `Request.mode()`; thread typed `Mode`+`Source` through `validate`/`prepare`/`AllocateRun`/`harness.Request` | logic | T-CI-9, T-CI-5 | M |
| T-CI-15 | `PromptInput` gains session dir + typed `Mode`; validation rejects an empty path or invalid mode | logic | T-CI-14, T-CI-6 | M |
| T-CI-16 | Per-mode preamble and commit framing (gate-as-comment; Research/Plan do not commit; Implement may commit but never push) | logic | T-CI-15 | L |
| T-CI-17 | Orchestrator-invocation sentence as one per-mode constant covering both S1 outcomes | logic | T-CI-16 | S |
| T-CI-45 | `repos.DetectAiteam` probes `.claude/agents/`; the orchestrator sentence is emitted only when present | logic | T-CI-17 | S |
| T-CI-18 | Run report body names mode, `trigger_source`, and the classifier reason via `redact.Apply` | logic | T-CI-14 | S |
| T-CI-19 | Test: `/spike` prompt golden, byte-for-byte (M0 regression guard) | test | T-CI-16 | S |
| T-CI-20 | Test: per-mode prompt goldens + `BuildPrompt` validation errors; split the "forbids questions" assertion per mode | test | T-CI-16, T-CI-17 | M |
| T-CI-21 | Test: report body at volume — dozens of files against the 65,536-byte `redact.Budget` cap | test | T-CI-18 | S |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-CI-5.1 | `Request.mode()` no longer slices `Command.Name`. Mode is the typed value; an invalid mode is refused by `validate()` with an error, never a panic. |
| AC-CI-5.2 | `runs.mode` carries the four modes and `runs.trigger_source` carries `explicit`/`inferred`. The commit trailer and the metadata document say the same thing, from the same value. |
| AC-CI-5.3 | For the three orchestrator modes, the instruction section **begins with the aiteam slash command** `/<mode> <feature-slug>`, so the child routes via CLAUDE.md rule 1 rather than improvising through rule 3. |
| AC-CI-5.4 | The prompt states the **absolute session directory**, notes it is UTC-derived, and instructs that it be used verbatim — the child must not derive `<date>` itself. |
| AC-CI-5.5 | The headless preamble is replaced **per mode** for the three orchestrator modes: the issue comment thread is the approval checkpoint. The orchestrator runs to its gate, writes the artifact, and ends its turn summarising exactly what needs confirming. It must not invent the user's approval or proceed past a gate. |
| AC-CI-5.6 | Per-mode commit framing: Research and Plan do not commit (litefactory does); Implement may commit its own work and must never push. Every mode is told which. |
| AC-CI-5.7 | The `/spike` prompt is unchanged byte for byte, including the existing preamble. A golden test pins it. This is the M0 regression guard. |
| AC-CI-5.8 | `BuildPrompt` stays pure: no clock, no filesystem, no environment. The session path arrives as an input. |
| AC-CI-5.9 | `BuildPrompt` validates the new inputs as it validates the old — an empty session path or invalid mode is an error, not a prompt with a hole in it. |
| AC-CI-5.10 | The report names the mode and, when `trigger_source = inferred`, says so and quotes the classifier's reason (through `redact.Apply`). A false positive must be visible in the thread without opening a log. **Security-relevant, not cosmetic.** |
| AC-CI-5.11 | The orchestrator sentence is emitted only when `.claude/agents/` is present in the worktree; otherwise the run falls back to plain Spike framing. |

---

## User Story 6 — Pre-spawn gate check

> As the owner, I want a mode whose prerequisite artifact is missing to be declined by name before a run is spent, so an inferred Implement with no plan costs one comment, not one run.

### Architecture Notes
- **A decline is a `Disposition`, not an error.** It rides the existing `RunID == 0` reporting path (`report.go:301-309`: post, then `AdvanceWatermark`), which a preparation failure already uses — no new reporting machinery, no new transaction shape. It must **not** reuse `prepareFailed`, which sets `SessionFailed` (`execute.go:305-320`): a task that simply needs a brief first is not broken. The `Disposition` enum (`result.go:23-43`) is the established extension point.
- **Ordering inside the accept branch: budget → gate → `AllocateRun`.** The gate precedes the runs-row insert so a decline burns no ordinal; `prepare.go:89` (after `ensureWorktree`) to `prepare.go:341` (`AllocateRun`) is the exact window, and `TestExecuteAllocatesTheRunRowBeforeTheSubprocess` documents the ordering it must respect. The budget precedes the gate so an exhausted budget does not pay for a filesystem walk. This is why S6 lands before S8 — inserting a check *ahead* of an existing one is a smaller edit than the reverse.
- **A `Stat` error that is not `NotExist` declines; it does not fail.** An unreadable worktree is not an approved gate, and turning it into a session failure would mark the task `failed` for a transient filesystem condition.
- **No `FileChecker` interface.** One `os.Stat`, one implementation, no test fake requiring substitution — abstracting it would be premature under the platform constraints. AC-CI-6.6 needs a real temp git repo fixture anyway, which is the in-tree convention.
- `task clean` removes the worktree but the branch survives, so `ensureWorktree` restores `.aiteam/` artifacts from the branch — AC-CI-6.6 is satisfiable as written.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-CI-30 | Gate check + `DispositionGateDeclined` on the `RunID == 0` report path, before `AllocateRun` | logic | T-CI-25, T-CI-6 | M |
| T-CI-31 | Gate-decline comment renderer: missing artifact, full path, producing mode; never auto-promotes | logic | T-CI-30 | S |
| T-CI-32 | Test: gate decline for inferred and explicit; worktree recreated after `task clean` restores the gate | test | T-CI-30, T-CI-31 | M |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-CI-6.1 | Research and Spike have no prerequisite; Plan requires `RESEARCH_BRIEF.md`; Implement requires `IMPLEMENTATION_PLAN.md` — both in the frozen session directory, checked as a plain filesystem test in the worktree. |
| AC-CI-6.2 | The check runs after the worktree exists and **before the runs row is allocated**, so a decline burns no ordinal and no run. |
| AC-CI-6.3 | A missing prerequisite is a *decline*, not a failure: a distinct disposition that posts a comment, consumes the comment, and does **not** mark the session `failed`. |
| AC-CI-6.4 | The decline comment names the missing artifact, its full path, and the mode that would produce it. It never auto-promotes — CLAUDE.md's own rule is "never promote silently". |
| AC-CI-6.5 | The rule is identical for explicit and inferred triggers. An explicit `/plan` with no brief declines the same way. |
| AC-CI-6.6 | A worktree recreated after `task clean` restores the artifacts from the branch, so a cleaned task does not lose a gate it had passed. |
| AC-CI-6.7 | A gate decline is terminal. If the post fails in a way that leaves the comment unconsumed, the existing AC-4.10 retry ordering applies unchanged — the comment is retried, never skipped. |
| AC-CI-6.8 | A `Stat` error other than `NotExist` declines rather than failing the session. |

---

## User Story 7 — Tool policy for orchestrators, and the existing-install upgrade

> As the owner, I want an Implement-mode run to be able to commit its own work and still have its files reported, so the mode D-CI-2 unlocked is actually usable.

### Architecture Notes
- **`Bash(make:*)` does not ship (D-CI-6).** Not because it grants more than `Bash(go test:*)` — it does not; that entry plus `Write` already permits running agent-authored code, and SD-2 already ruled `--allowedTools` is defence-in-depth rather than the boundary (the real containment is the worktree, the task branch, and nothing auto-merging). The reason is **legibility of the allow-list as an operator artifact**, which matters here precisely because AC-CI-7.6 tells the operator to read and hand-edit it. The test: *can an operator know what an entry permits, from the entry alone?* `Bash(golangci-lint:*)` — yes. `Bash(make:*)` — no: it permits whatever the `Makefile` says, and the `Makefile` is an `Edit`-able file inside the untrusted work area, so it can change meaning without the config changing. It also buys nothing — `make test` is `go test ./...`, and the feature-level "make test is green" AC is the *human's* gate, not the agent's.
- **Ship:** `Bash(go build:*)`, `Bash(go vet:*)`, `Bash(go run:*)`, `Bash(go mod:*)`, `Bash(gofmt:*)`, `Bash(gofumpt:*)`, `Bash(golangci-lint:*)`, `Bash(git add:*)`, `Bash(git commit:*)`. **Do not ship** `Bash(git push:*)`, `git checkout`, `git reset`, `git rebase`, `git clean`, `WebFetch`, `WebSearch` — nor any `Bash(git:*)` or `Bash(go:*)` catch-all, which would re-admit `git push` and `go get` through the back door. All proposed entries pass `toolRuleRE` and `validateAllowedTools`.
- **This contradicts an AC-anchored design comment and is a recorded amendment, not a list edit.** `policy.go:31-33` states the read-only git surface is deliberate "because litefactory itself performs the commit and the push (AC-3.21, AC-3.22)".
- **AC-CI-7.2 and AC-CI-7.8 must ship together, ideally in one commit.** Granting `git commit` is exactly what makes `harvest.go`'s pre-staging `Status` read go blank. Landing the permission without the diff-based file list produces runs reporting zero changes — the confusing failure this story exists to remove.
- **R8 is narrower than the brief states.** `commit.go:119-133` already anticipates a self-committing run (AC-3.24) and `harvest.go:155` keys the push off `commit.Head != prep.shaBefore`, so self-committed work **is already pushed**. Only `result.Changes = commit.Files` goes blank. This is a reporting defect, not a functional break.
- **The upgrade story is on the critical path, because the owner's own install is affected.** `config.WriteDefault` uses `O_EXCL` and never overwrites (`template.go:150-176`) — deliberate, AC-0.7, and what makes `init` idempotent. So an existing install keeps its old `allowed_tools` **and** its 1800s `claude.timeout_seconds`, which an orchestrator driving sub-agents may well exceed. The warning **never edits the operator's file**: silently widening a deliberately narrowed permission list is worse than a warning. (`harness.WriteSettings` existing as an overwrite is not licence to reuse it here.)
- **Two output channels, one message:** `slog` on `daemon run` startup; `fmt.Fprintf(w, …)` on `init`, per the Global Invariants' explicit CLI carve-out. `Startup` (`daemon/setup.go:62-78`) already carries exactly this kind of "how am I configured and what's risky about it" reporting, with the SD-7 `FromPATH` warning as precedent. Share the construction, not the emission.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-CI-33 | `defaultAllowedTools()` additions + `config.yaml` template lockstep | infra | — | S |
| T-CI-34 | Recommended-set drift warning on `init` and on `daemon run` startup; never edits the operator's file | infra | T-CI-33 | S |
| T-CI-35 | R8: report file list from `diff(shaBefore, Head)` when the run self-committed | logic | — | M |
| T-CI-36 | Test: the whole default allowed-tools list passes `validateAllowedTools`; the template/`Defaults()` drift test stays green | test | T-CI-33 | S |
| T-CI-37 | Test: self-commit harvest — self-commit only, and self-commit plus leftover uncommitted files | test | T-CI-35 | M |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-CI-7.1 | `defaultAllowedTools()` gains the Go toolchain entries above so an orchestrator can verify its own work. `Bash(make:*)` is **not** among them. |
| AC-CI-7.2 | It gains `Bash(git add:*)` and `Bash(git commit:*)` so Implement's engineers do not report BLOCKED. It does not gain `git push`, `checkout`, `reset`, `rebase` or `clean`. |
| AC-CI-7.3 | Every new entry passes `validateAllowedTools` — qualified, never bare `Bash`, never `Bash(*)`, never a `Bash(git:*)`/`Bash(go:*)` catch-all. A test asserts the whole list validates. |
| AC-CI-7.4 | `Task` and `TodoWrite` are added to `allowed_tools` **iff** AC-CI-1.1 measured them as denied without an entry. |
| AC-CI-7.5 | The generated template and `Defaults()` stay in lockstep; the existing drift test passes unmodified. |
| AC-CI-7.6 | On `init` and on `daemon run` startup, litefactory compares configured `allowed_tools` against the recommended set and warns, naming exactly the missing rules and the file to edit. It never edits the operator's file. |
| AC-CI-7.7 | The warning is loudest about `Bash(git add:*)` / `Bash(git commit:*)`, whose absence turns an Implement run into a confusing BLOCKED report. |
| AC-CI-7.8 | When the run committed for itself (`Head != shaBefore` while `Committed` is false), the reported file list is derived from the `shaBefore..Head` diff instead of the pre-staging `Status`. A self-committing run no longer reports zero changes. |
| AC-CI-7.9 | Push behaviour is unchanged: the branch moving is the test, not the commit happening (AC-3.24). |
| AC-CI-7.10 | A run that both self-commits and leaves uncommitted files gets both: litefactory's commit on top, and a file list covering the whole span. |
| AC-CI-7.11 | The `claude.timeout_seconds` staleness on existing installs is named in the same warning. |

---

## User Story 8 — Cost bound

> As the owner, I want a daily ceiling on what inference can spend, so a misread conversation cannot run the machine all night.

Re-opens D-X, whose justification was that only an explicit `/spike` spends money.

### Architecture Notes
- **D-X's rejection was written as "No task or AC may reintroduce it"**, so this is a formal amendment recorded by S9, not merely a new feature.
- **One store method, one query, one struct.** All three ceilings come from a single rolling-24h aggregate over `runs`. Three separate queries can observe an inconsistent picture within a tick and cost three round trips per comment — and `SetMaxOpenConns(1)` means they serialise against everything else. `runs.cost_usd` already exists and is populated from the terminal result event, so no new telemetry is needed; S2 adds the `started_at` index this query requires.
- **`COALESCE(SUM(cost_usd), 0)`.** `cost_usd` is `REAL` and nullable (SD-3: no result event ⇒ no cost); a bare `SUM` over an all-NULL window returns NULL. AC-CI-8.3's documented one-run overshoot follows from the same fact and is correctly accepted rather than engineered around.
- **Fail open on a budget-lookup error** — log at ERROR and allow the run. Failing closed turns a transient database error into permanent silence, which AC-4.1 forbids.
- **No background refresher goroutine.** Computed on demand inside the tick.
- **OD-4 is open:** the spec contradicts itself on whether a budget decline speaks (AC-CI-8.2 posts; AC-CI-3.9 stays silent). See Owner Decisions.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-CI-38 | Store: single rolling-24h `BudgetUsage` aggregate with `COALESCE`, one query | data | T-CI-4 | S |
| T-CI-39 | `internal/config` `budget` block; zero-means-unlimited startup warning | infra | — | S |
| T-CI-40 | Budget check before `AllocateRun`; terminal decline naming the ceiling and its reset; fails open on lookup error | logic | T-CI-38, T-CI-39, T-CI-30 | M |
| T-CI-41 | Test: three ceilings decline correctly for explicit and inferred triggers; NULL `cost_usd` handled | test | T-CI-40 | S |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-CI-8.1 | Before a run is spawned, three ceilings are checked over a rolling 24 h window: `budget.daily_usd`, `budget.daily_runs`, `budget.task_daily_runs` — in one query. |
| AC-CI-8.2 | Over ceiling ⇒ terminal decline: the comment is consumed and no run row is created. Whether a comment is posted depends on OD-4. |
| AC-CI-8.3 | `cost_usd` is known only after a run and may be NULL, so the spend ceiling can overshoot by at most one run. Documented, not engineered around. |
| AC-CI-8.4 | A ceiling of `0` means unlimited, is permitted, and is warned about at startup. |
| AC-CI-8.5 | The ceilings apply to explicit commands too. A `/implement` loop costs the same as an inferred one. |
| AC-CI-8.6 | Classifier invocations are counted per tick and logged, so inference cost is observable even though it is not separately capped. |
| AC-CI-8.7 | A budget-lookup failure fails **open**: logged at ERROR, the run proceeds, the queue never wedges. |

---

## User Story 9 — Spec amendments recorded in-tree

> As a future reader of the M0 spec, I want A-8 and D-X recorded as amended, so the contradiction is documented rather than silent.

### Architecture Notes
- **The amendments file reaches the branch through the ordinary `git add --all -- .`** (`commit.go:148`) — `.aiteam/` is not gitignored, and `status.go` uses `--untracked-files=all` so files in a brand-new directory are seen individually. **That mechanism is now load-bearing for the whole feature** — it is how `RESEARCH_BRIEF.md` gets committed — so it deserves a cheap test guarding against a future `.gitignore` edit.
- **Record two things the spec does not list:** the UTC session-date decision (D-CI-4) and its visible consequence, so a future reader who notices the "tomorrow" directory does not "fix" it; and the `~/.claude` agent-discovery leak for non-self repos (`settings.go:24-27` — `--settings` is additive, and `harness/doc.go:44-47` records it as *verified*), as a named limitation rather than a surprise.
- **A pointer line, not a rewrite.** One "Amended by `<path>`" line under A-8 in the M0 spec. The historical spec stays historical — that is what makes the amendment trail readable.
- **AC-CI-9.4 is not cosmetic.** `CLAUDE.md`'s roster table says `agents/*.md` where the files are at `.claude/agents/*.md`. Anything that enumerates the roster from that table — including the child, under the inline-routing fallback — reads a path that does not exist.
- Also note: `*.out`/`*.test` are gitignored but ad-hoc `.md` is not, so stray agent scratch files ride along into the commit. Accepted, unchanged from M0; worth a line for the report reader.

### Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-CI-42 | `$session/AMENDMENTS.md`: A-8 reversed, D-X superseded, D-CI-1..7 restated, UTC decision, `~/.claude` leak, R10 | infra | — | S |
| T-CI-43 | "Amended by" pointer line in the M0 spec; `CLAUDE.md` roster path corrected to `.claude/agents/*.md` | infra | T-CI-42 | XS |
| T-CI-46 | Test: `.aiteam/` is not gitignored — guards the artifact-harvest mechanism | test | — | XS |

### Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-CI-9.1 | `$session/AMENDMENTS.md` records **A-8 reversed** (`lf-setup/feature-spec.md:412`), with reason and date. |
| AC-CI-9.2 | The same file records **D-X superseded** by D-CI-3, with the original justification and why inference removes it. |
| AC-CI-9.3 | A single "Amended by `<path>`" pointer is added under A-8 in the M0 spec. The historical spec is not rewritten. |
| AC-CI-9.4 | `CLAUDE.md`'s roster table is corrected from `agents/*.md` to `.claude/agents/*.md`. |
| AC-CI-9.5 | D-CI-1..D-CI-7 are restated as binding decisions with their consequences. |
| AC-CI-9.6 | The UTC date choice and the `~/.claude` agent-discovery limitation are both recorded. |
| AC-CI-9.7 | The tool-policy amendment is recorded against `policy.go:31-33`'s AC-3.21/AC-3.22 rationale. |

---

## Feature-level Acceptance Criteria

- [ ] A plain-English comment from the owner asking for research starts a Research-mode run, and `RESEARCH_BRIEF.md` lands in the task's session directory, is committed to the task branch, and is pushed before the report comment.
- [ ] The same comment on the following day lands in the **same** session directory.
- [ ] A comment the classifier cannot classify is consumed, gets 😕, gets no comment, and the next comment on that issue is processed in the same tick.
- [ ] `/spike <text>` behaves exactly as it did on 2026-08-25, prompt bytes included.
- [ ] `/plan` on a task with no research brief posts a comment naming the missing file and spends no run.
- [ ] An Implement-mode run can commit its own work, is pushed, and its report lists the files it changed.
- [ ] The daily ceilings decline a run and say which ceiling was hit.
- [ ] `inference.enabled: false` reproduces M0 behaviour with zero classifier invocations.
- [ ] `make test` and `make lint` are green; existing suites pass unmodified except where a story explicitly changes their expectations.

---

## Out of Scope

- Conversation resumption across comments (segments beyond ordinal 1); whether re-using `--session-id` resumes or resets stays unmeasured.
- The rest of the LF-6/LF-7 vocabulary: `/approve`, `/include`, `/status`, `/retry`, `/reset`.
- `lf:brief-ready` / `lf:plan-ready` labels; draft PR creation.
- Third-party comments as triggers (D-I, settled). Issue **bodies** as triggers (AC-2.16, settled).
- Materialising `.claude/agents/` into a non-self repo (R10). **Narrow exception:** the read-only *probe* is in scope (T-CI-45), because the inline-routing fallback makes its absence a live failure rather than a theoretical one.
- Multi-repo / per-repo team behaviour; `repos.aiteam_team` stays detected and unused.
- Any change to the D-I authorship filter, D-4/S-6 slug derivation, or `UNIQUE (session_id, trigger_comment_id)`.
- Hot config reload. New keys take effect on daemon restart.
- Log rotation for `daemon.log` (`lumberjack` explicitly rejected).

---

## Open — Owner Decisions Required

These block nothing structurally, but each is product-visible and none should be resolved silently.

**OD-1 — WebFetch / WebSearch on the `--tools` boundary.** `research-orchestrator` declares both in frontmatter; neither is in `defaultTools()`. Adding them opens a network egress channel from a worktree holding the repo's source. **Recommendation: leave them out** — the research for this very feature was done without them, and `--tools` is the one boundary SD-2 measured as real. If AC-CI-1.2 shows frontmatter grants them regardless, this stops being a choice and becomes an SD-2 amendment.

**OD-2 — Classifier model alias.** The mechanism is effectively settled (D-CI-7): per SD-4 this machine authenticates via Keychain with `apiKeySource: "none"`, so a direct Anthropic call needs a credential the product does not have and that SD-4 warns overrides Keychain auth when stale. **What needs confirming is the model alias** — it is the per-comment cost driver and it is written into `config.yaml` where you maintain it.

**OD-3 — The three budget ceilings.** `daily_usd: 25`, `daily_runs: 20`, `task_daily_runs: 6`. **Recommendation: ship these and revisit after a week** — they are config, not code, and AC-CI-8.6's logging gives the data to tune them.

**OD-4 — Does a budget decline post a comment on an *inferred* trigger?** The spec contradicts itself: AC-CI-8.2 posts a comment naming the ceiling; AC-CI-3.9 posts nothing for an inference decline. So a false-positive classification on a budget-exhausted night produces a comment about a request you never made. **Recommendation: post the ceiling comment only for *explicit* triggers; an inferred budget decline gets 😕 and silence like every other inferred decline.** The `intent_reason` row and the log still answer "why did nothing happen".

**OD-5 — If AC-CI-1.4 shows the project's `PreToolUse` Bash hook fires inside worktree runs, should litefactory's settings neutralise project hooks for headless runs?** Leaving it on risks an opaque hook denial mid-run — one of the hardest failures to diagnose from a run log. Turning it off means headless runs no longer honour a guard the repo deliberately installed. **No recommendation: this is a policy question about your own repo's hooks.** Only live if S1 measures the hook as firing.

---

## Owner Decisions Closed (2026-08-25)

All taken by the owner with the recommendation and its trade-off stated. Four of five went
against the plan's recommendation; recorded here so Implement does not re-litigate them.

**OD-1 — RESOLVED: add `WebFetch` and `WebSearch` to `defaultTools()`.**
Against recommendation. Research mode gets network access; the worktree holding repository
source gains a network egress channel. Accepted knowingly.

**OD-2 — RESOLVED: `claude-sonnet-5` is the classifier alias.**
Against recommendation (Haiku 4.5). Higher per-comment cost on a path that runs for every
free-text owner comment, bought for better judgement on ambiguous text. Interacts with OD-3:
this cost is currently unbounded.

**OD-3 — RESOLVED: no ceilings enforced at ship.**
Against recommendation. D-X stands in effect, but *not* by deferring S8.

**OD-3/OD-4 INTERACTION — the two answers are literally contradictory** (a budget decline
cannot be commented on when no budget exists). Resolution, taken by the assistant and stated
to the owner rather than chosen silently:

> Build S8's mechanism in full, but default `daily_usd`, `daily_runs` and `task_daily_runs`
> to **unlimited**. Nothing throttles at ship, satisfying OD-3. Setting any value activates
> the ceiling *and* OD-4's comment behaviour, with no code change. AC-CI-8.6's spend logging
> ships enabled regardless, so the data to choose a ceiling accumulates from day one.

S8 therefore stays in scope. Its ACs about *enforcement* must be tested against explicitly
configured ceilings, not against the shipped defaults.

**OD-4 — RESOLVED: a budget decline always posts a comment, inferred or explicit.**
Against recommendation. Accepted consequence: a false-positive classification on a
ceiling-exhausted day posts a comment about a request the owner never made. Only reachable
once a ceiling is configured (see OD-3 interaction).

**OD-5 — OPEN, contingent.** Live only if S1/AC-CI-1.4 measures the project's `PreToolUse`
Bash hook firing inside worktree runs. Route back to the owner if it does; do not decide it.
