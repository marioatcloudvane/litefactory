# Feature Spec: Coding-agent switching by label
# Status: APPROVED
# Date: 2026-09-12

## Goal
Changing an issue's `Coding Agent:` label must actually move that issue's work to
the named tool on the next dispatched turn — replacing today's refusal
(`internal/turn/prepare.go:228-237` → `report.ReplyAgentSwitchRefused`) and the
manual `switch-agent.sh` + SSH procedure it forces the owner into.

## User Stories
| ID | Story |
|----|-------|
| US-1 | As the issue owner, I want to change `Coding Agent: claude` to `Coding Agent: codex` (or back) on a GitHub issue and have my next comment run under the new tool, so I never ssh into the daemon host or restart litefactory |
| US-2 | As the issue owner, I want a turn that arrives while work is already running to be unaffected, and the switch to apply from the next dispatched task, so a label edit never kills an in-flight run |
| US-3 | As the daemon operator, I want `segments.agent` and every `runs.agent` row to keep agreeing after a switch, so run history still says which tool actually ran each run |
| US-4 | As a maintainer, I want the manual `switch-agent.sh` escape hatch deleted once the automatic path exists, so there is one mechanism, not two |

## Decisions (settled by the owner — not reopened here)
| # | Decision | Source |
|---|----------|--------|
| D-1 | Overwrite the existing `segments` row in place (`agent`, `resume_token`); do **not** rotate to a new segment | RESEARCH_BRIEF Q5 ("overwriting!") |
| D-2 | In-flight work finishes under the old agent; the switch applies from the next dispatched task. No subprocess cancellation | Q6 ("let the work finish") |
| D-3 | No new label-watching/polling/reconciliation plumbing; the existing per-turn `resolveAgent` read is the trigger | Q7 ("no new work") |
| D-4 | Delete `switch-agent.sh` in this change; it is not kept as a fallback | Q8 ("no, we delete it!") |
| D-5 | `.codex/AGENT_ROSTER.md` naming coincidence is ignored | Q9 |
| D-6 | Small change; no new abstractions, no new config, no new machinery | Owner, twice |

## Data Model
No migration. `segments.agent` and `segments.resume_token` already exist
(`internal/store/migrations/0003_agent.sql`); only their mutability changes.

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| `segments` | `agent` | TEXT | yes | Was write-once at INSERT (`store/segments.go:62-100`). Becomes rewritable to any `agent.Kind` present in `turn.RunnerSet`; never a hardcoded `claude|codex` list |
| `segments` | `resume_token` | TEXT | no | Cleared to empty in the same statement as an `agent` rewrite (a token from the old tool names a conversation the new tool never issued) |
| `segments` | `ordinal`, `session_ref` | INT / TEXT | yes | **Unchanged by a switch** — same row, same ordinal, same derived id (`naming.SessionRef(ref, ordinal)`) |
| `runs` | `agent` | TEXT | yes | Per-run historical snapshot, copied from `segment.Agent` at allocation (`prepare.go:1540`). Rows written before a switch keep the old value |

### Relationships
- `sessions` → `segments` via `session_id` (+ `ordinal`); `segments` → `runs` via `runs.segment_id`.
- The pin read on every turn is the **last** segment of the session
  (`turn/delivery.go:328-345`), compared against `req.Agent`, which the daemon
  resolved fresh from the label this tick (`daemon/agent.go:94-96`, call sites
  `daemon/tick.go:1754`, `daemon/continuation_dispatch.go:107`).

## API / Interface Changes
| Surface | Change | Notes |
|---------|--------|-------|
| `store.DB` | **New** accessor, e.g. `SetSegmentAgent(ctx, segmentID int64, kind string) error` | `UPDATE segments SET agent = ?, resume_token = '' WHERE id = ?` inside `withTx`; reject empty kind/zero id; `ErrNotFound` for a missing row. Mirrors `switch-agent.sh`'s statement. Contrast `SetSegmentResumeToken` (`store/segments.go:102-134`), which deliberately never overwrites — this one deliberately does |
| `turn.prepareDeliverySegment` (`turn/delivery.go:293-380`) | The vendor-mismatch early return (`delivery.go:343-345`) performs the switch instead of handing the stale segment back | After the switch, continue at **`last.Ordinal` / `last.SessionRef`** — do not take the rotation branch (`delivery.go:353-357`) even though the delivery identity necessarily differs (the hash covers `Kind`, `delivery.go:139-153`) |
| `turn.prepareDeliverySegment` (`delivery.go:318-326`) | The `p == nil && !hadDelivery` branch must apply the same switch to the row `GetOrCreateSegment` returns | `GetOrCreateSegment` ignores its `kind` argument on the reuse path (`store/segments.go:53-61`), so this path can also return a stale-agent row |
| `turn.prepare` (`prepare.go:228-237`) | Refusal narrowed to the automatic-recovery path only: `req.Continuation != nil && segment.Agent != string(resolvedKind)` | Ordinary turns can no longer reach it. Kept as defence in depth behind `daemon/continuation_dispatch.go:112-114`, which already cancels a pending continuation when the agent changed |
| `turn.resumeDecision` / `previousRun` (`prepare.go:1024-1074`) | Claude's arm additionally requires a prior terminal run **on this segment whose `runs.agent` equals the resolved kind** | See BR-5; today Claude resumes on `found` alone |
| `report.ReplyAgentSwitchRefused` (`report/reply.go:51-58, 799-806`) | Reply text and doc comment rewritten for the narrowed meaning | The ReplyKind, `DispositionAgentSwitchRefused` (`turn/result.go:52-59`), `agentSwitchRefusal` (`turn/errors.go:68-86`), `agentSwitchRefused` (`turn/execute.go:686-698`) and `reportAgentSwitchRefusal` (`turn/report.go:375-376, 409-428`) all stay — removing the kind would churn the pinned inventories in `report/replykind_test.go` and `report/confirm_reply_test.go` for no gain |
| `switch-agent.sh` (repo root) | Deleted | No in-tree references outside `.aiteam/` history |

No change to: `internal/daemon` label resolution, `internal/github`, `internal/harness`,
`internal/codex`, `internal/render`, the CLI, or any config file.

## Business Rules
- **BR-1 (trigger)** — the switch is decided inside `prepare`, from the `req.Agent`
  the daemon resolved from this turn's fresh label snapshot. Nothing new watches
  labels; a bare label edit with no comment does nothing until the next dispatched
  turn.
- **BR-2 (in place)** — a switch rewrites `segments.agent` and clears
  `segments.resume_token` on the existing row. It creates no segment, does not
  change `ordinal` or `session_ref`, and never deletes a `runs` row.
- **BR-3 (serialisation)** — the mutation happens inside `prepare`, which runs
  under `AcquireSessionLock` (`turn/execute.go:548`) and inside the daemon's
  per-session in-flight set (`daemon/inflight.go`). An in-flight run therefore
  holds the session for its whole turn, so a switch can never land underneath it:
  this is what replaces `switch-agent.sh`'s "refuse while the daemon is alive"
  precondition, with no new locking. D-2 follows from it for free.
- **BR-4 (history stays honest)** — `allocateRun` copies `segment.Agent` into
  `runs.agent` after the switch (`prepare.go:1530-1540`), so the run's row names
  the tool that actually ran it; earlier rows keep the old value.
- **BR-5 (conversation continuity after a switch)** — a switched turn must never
  hand one tool a conversation handle it did not issue, and must never pass Claude
  a `--session-id` that already has a transcript:
  - switching **away** from a tool ⇒ the new tool starts cold (`resume_token` is
    cleared, so Codex is cold; Claude is cold because no prior run on this segment
    used Claude);
  - switching **back** to Claude on a segment Claude already ran ⇒ resume that
    earlier Claude conversation. A cold run would pass `--session-id` for an
    id that already exists and exit 1 with "Session ID … is already in use"
    (`harness/runner.go:545-559`, pinned by `harness/runner_test.go:1147` and
    `turn/harness_test.go:238`), which the resume-refused cold rerun
    (`turn/harvest.go:185-262`) does not cover.
  This is the direct consequence of D-1: the segment's `session_ref` is
  `uuidv5("<ref>/segment/<ordinal>")` (`naming/session.go:44-76`) and an in-place
  switch reuses it.
- **BR-6 (instruction bundle follows the tool)** — after a switch the segment's
  recorded delivery identity/root must be a bundle rendered for the **new** agent
  (`SetSegmentDelivery`, `turn/delivery.go:371-380`), and `agent.Request.Delivery`
  must be non-nil whenever a team snapshot is configured. Today the mismatch
  branch returns a `nil` delivery, which would run the new tool with no team
  instructions and leave the old tool's bundle identity recorded.
- **BR-7 (registry-driven)** — the set of switchable kinds is whatever
  `turn.RunnerSet` holds (`prepare.go:238-248` already refuses an unregistered
  kind before allocating a run). No `claude|codex` literal is introduced.
- **BR-8 (automatic recovery)** — an auto-resume continuation (`req.Continuation
  != nil`) never switches: it is cancelled upstream when the label changed, and if
  one reaches `prepare` anyway it is refused exactly as today.
- **BR-9 (no new comment surface)** — a switched turn posts its ordinary run
  report. No new reply kind, no extra "agent switched" comment.

## Edge Cases & Error Handling
- Label unchanged → no mutation, no extra write, current behaviour byte for byte.
- Session with no segment yet → `GetOrCreateSegment` creates it under the resolved
  kind; no switch path involved.
- Label change **while a run is in flight** → the running turn finishes under the
  old agent (it holds the lock); the next dispatched turn switches (D-2, BR-3).
- Label changed to an unregistered/malformed/ambiguous value → refused by
  `daemon.resolveAgent` before `prepare`; no mutation. Unchanged behaviour.
- Switch **and** an instruction-bundle change in the same turn → one switch, same
  ordinal, new bundle written for the new agent (BR-6). No rotation.
- Repeated A→B→A switching → each turn rewrites the same row; `runs` keeps the
  full alternating history; BR-5 decides cold vs resume each time.
- Auto-resume continuation whose label changed → cancelled by
  `daemon/continuation_dispatch.go:112-114`; `prepare`'s refusal remains as the
  unreachable-in-practice backstop.
- A `prelaunch` runs row exists for this trigger comment from a crashed earlier
  attempt **and** the label changed since → `allocateRun`'s guard
  (`prepare.go:1520`) errors with "prelaunch allocation no longer matches this
  request". Pre-existing behaviour class (the same guard already fires on a mode
  change); not fixed here, recorded as R-3.
- Store write fails mid-turn → ordinary error return from `prepare`; the turn
  reports a preparation failure and `sessions.state` is untouched, because the
  mutation sits above `SetSessionState` (`prepare.go:427-440`) like every other
  early return.

## Out of Scope
- Rotating to a new segment on a switch, killing an in-flight subprocess, any new
  polling/reconciliation pass, keeping `switch-agent.sh` as a fallback (D-1/D-2/D-3/D-4).
- Adding a third coding-agent tool; any CLI flag or non-label selection surface.
- `internal/render` harness rendering; `.codex/AGENT_ROSTER.md` (D-5).
- Migrating or rewriting historical `runs.agent` rows.
- Announcing the switch in a dedicated issue comment.
- Any change to `daemon.resolveAgent`'s label ladder or its refusal replies.

## Assumptions
| # | Assumption | Based on |
|---|------------|----------|
| A-1 | Mutation site is `prepareDeliverySegment`, not the refusal line itself, because that function is the only place the stored segment, the requested kind and the delivery decision are all in hand (BR-6) | `turn/delivery.go:293-380`; brief is silent on placement |
| A-2 | The refusal type/disposition/ReplyKind survive, narrowed to continuations, rather than being deleted | Smaller diff than removing a pinned enum member (`report/replykind_test.go`, `confirm_reply_test.go`); keeps BR-8 enforceable |
| A-3 | BR-5's Claude arm is refined via `runs.agent` on the segment's prior runs | `store.Run.Agent` already records it; avoids a guaranteed wasted refused subprocess on every codex→claude switch and the fatal cold case |
| A-4 | No owner-facing documentation exists to update (no `docs/**` or README mention of the refusal or the script) | Repo-wide search: only `internal/**` and `.aiteam/**` history mention them |
| A-5 | "A few lines" is read as: one new store accessor, one branch rewritten in `delivery.go`, one condition narrowed in `prepare.go`, one resume-decision predicate, two tests rewritten, one script deleted | Owner's D-6 plus the code above |

## Risks
- **R-1 (contradicts nothing, but worth the owner knowing)** — under D-1, switching
  back to Claude resumes Claude's own earlier conversation on that task rather than
  starting a fresh one (BR-5). Truly-fresh-every-switch is only reachable by
  rotating the segment (new ordinal ⇒ new `session_ref`), which the owner declined.
  Non-blocking; the behaviour is coherent and no run fails.
- **R-2** — `execute_test.go:304-378` currently pins the opposite behaviour and
  must be rewritten (not merely added to), together with the rationale comments at
  `report/reply.go:51-57`, `turn/errors.go:68-77`, `turn/execute.go:686-697`,
  `turn/report.go:394-409`, `prepare.go:215-237, 262-269`, `turn/delivery.go:293-294`.
- **R-3** — the stale-`prelaunch` interaction above can wedge a single trigger
  comment; narrow window, pre-existing class, explicitly not fixed here.
- **R-4** — `segments.agent` stops being immutable. Any future reader that assumed
  "fixed at creation" (the phrase appears in `store/segments.go:41-43` and
  `report/reply.go:53-54`) must be corrected in this change.

## Evidence obligations
| Criterion | Proof method | Source availability | Version/context bounds | Missing prerequisite / disposition |
|---|---|---|---|---|
| AC-1, AC-2, AC-3, AC-4 | Offline Go tests through `turn.Execute` with the existing stub runner fixture (`internal/turn/fixture_test.go`, `newStubRunner`), asserting DB rows read back from the real SQLite store | In repository | Go 1.26, `go test ./...`; no network, no real agent binary | None |
| AC-5 (cold/resume after a switch) | Offline assertion on `agent.Request.Path` / `Session` handed to the stub runner, both directions | In repository | Same | None. The underlying "Session ID … is already in use" fact is documentation + existing tests (`harness/runner.go:545-559`, `harness/runner_test.go:1147`, `turn/harness_test.go:238`), not re-observed live |
| AC-6 (bundle rendered for the new agent) | Offline assertion that the segment's delivery identity changes and matches a `planDelivery` hash computed with the new `Kind` | In repository | Same | None |
| AC-7 (script deleted) | File absence + repo-wide grep | In repository | Same | None |
| End-to-end: a real `codex`/`claude` process picks up the switched label on the owner's host | Operator observation outside a managed run | Unavailable in-run | Live nested coding-agent observation is not supported under the current sandbox (`docs/decisions/nested-agent-evidence.md`) | **Unsatisfied obligation**, recorded, not dropped: owner-operated post-merge check. Must not become an implementation or CI requirement |

## Acceptance Criteria
- [ ] AC-1 — A session whose last segment is `codex` and whose turn resolves to
      `claude` (and the reverse) runs: `turn.Execute` returns `DispositionRan`,
      the configured runner for the newly resolved kind is invoked, and no
      `DispositionAgentSwitchRefused` is produced.
- [ ] AC-2 — After that turn the `segments` row is the **same row** (same `id`,
      `ordinal`, `session_ref`), with `agent` = the resolved kind and
      `resume_token` empty. No second segment exists for the session.
- [ ] AC-3 — The `runs` row allocated by the switched turn has `agent` = the new
      kind; `runs` rows from before the switch still carry the old kind.
- [ ] AC-4 — A turn whose label matches the pinned agent performs no segment write
      and behaves exactly as before (regression pin).
- [ ] AC-5 — Cold/resume after a switch: switching away yields a cold run for the
      new tool; switching back to Claude on a segment with a prior terminal Claude
      run yields a resume; switching to Claude on a segment that never ran Claude
      yields a cold run. No path passes `--session-id` for an id a prior Claude run
      on that segment already used.
- [ ] AC-6 — When a team snapshot is configured, the switched turn's
      `agent.Request.Delivery` is non-nil and the segment's recorded delivery
      identity equals the identity `planDelivery` produces for the **new** agent.
- [ ] AC-7 — `switch-agent.sh` no longer exists and nothing in the tree references
      it.
- [ ] AC-8 — An `req.Continuation != nil` turn whose segment agent differs from the
      resolved kind is still refused (`DispositionAgentSwitchRefused`), allocates
      no run and leaves `sessions.state` unchanged.
- [ ] AC-9 — `internal/turn/execute_test.go`'s two agent-switch invariant tests are
      rewritten to assert the switch (AC-1/AC-2) and the narrowed refusal (AC-8);
      no test in the tree still asserts that an ordinary agent mismatch refuses.
- [ ] AC-10 — Every doc comment that states a segment's agent is fixed at creation
      or that a conversation never moves tools (`store/segments.go:41-43`,
      `report/reply.go:51-58` incl. the rendered reply text at `:799-806`,
      `turn/errors.go:68-77`, `turn/execute.go:686-697`, `turn/report.go:394-409`,
      `turn/prepare.go:215-237, 262-269`, `turn/delivery.go:293-294`) describes the
      new behaviour.
- [ ] AC-11 — No `claude`/`codex` literal is introduced in the switch path; the
      switchable set is `turn.RunnerSet` membership.
- [ ] AC-12 — `make check` passes (fmt, vet, golangci-lint v2, race tests).
