# Architect: Go Architect
# Created: 2026-09-12
# Feature: coding-agent switching by label (issue #228)
# Input: feature-spec.md (APPROVED), APP_CONSTRAINTS.md, APP_CONTEXT.md,
#        .claude/skills/go/platform-constraints.md

## Verdict

The scope as specified is **proportionate, not over-engineered** — it is one
store accessor, one restructured branch in `internal/turn/delivery.go`, one
narrowed condition in `internal/turn/prepare.go`, one resume predicate, rewritten
tests, one deleted script. No new package, no new interface, no new config key,
no goroutine, no polling, no migration. D-6 is honoured.

Two corrections to the spec's own arithmetic, both found by reading the code:

1. **The delivery pin is immutable too.** `store.SetSegmentDelivery`
   (`internal/store/segment_delivery.go:12-28`) upserts only when the new
   `(identity, root)` equals the stored pair, and errors otherwise
   ("segment delivery is absent or already pinned differently"). A switch
   necessarily changes both (the identity hash covers `Kind`,
   `turn/delivery.go:139-153`; `writeDelivery` allocates a fresh directory).
   **BR-6 cannot be satisfied by adding `SetSegmentAgent` alone.** The plan needs
   the pin retracted as part of the switch. See D-B below. This is the one real
   omission in the spec's API table and in A-5's line count.
2. **A-3 overstates the danger.** The "fatal cold Claude" case is not reachable
   once the switch updates the in-memory `store.Segment` (D-C): today's Claude arm
   returns `true` on `found`, `--resume` fails closed, and `runClaude`'s cold
   rerun (`turn/harvest.go:201-263`) recovers. The refinement is still worth
   doing — it removes a guaranteed wasted subprocess and makes the predicate state
   its own precondition — but it should be planned as *hygiene plus one saved
   subprocess*, not as a fix for a wedge. Planning it as a wedge fix invites
   over-building it.

Nothing else in the plan is architecturally unresolved. The daemon needs no
change at all.

## System Overview

### How This Feature Fits

The pipeline is unchanged end to end:
`daemon/tick.go` (fresh label snapshot) → `daemon.resolveAgent`
(`daemon/agent.go:96`) → `req.Agent` → `turn.Execute` → `turn.prepare` →
`prepareDeliverySegment` → `allocateRun` → runner (`internal/harness` or
`internal/codex`) → `turn/harvest.go` → `internal/report`.

The feature touches exactly three points on that line, all inside
`internal/turn` and `internal/store`:

- `internal/turn/delivery.go` — `prepareDeliverySegment` stops handing back a
  stale-agent segment and instead rewrites it in place, at the same ordinal, with
  a bundle rendered for the new tool.
- `internal/turn/prepare.go` — the refusal at `:228-237` narrows to
  `req.Continuation != nil`; `resumeDecision` (`:1064-1074`) gains the
  `runs.agent` fact.
- `internal/store/segments.go` — one new mutating accessor.

Everything upstream of `req.Agent` already works: the label is re-read every tick,
`resolveAgent` already refuses ambiguous/unregistered values, and
`daemon/continuation_dispatch.go:107-114` already cancels a pending automatic
continuation whose label changed. **Do not add anything to `internal/daemon`.**

### New Packages / Components

None. No new file is required; `SetSegmentAgent` belongs in the existing
`internal/store/segments.go` next to `SetSegmentResumeToken`, which it is the
deliberate mirror image of.

### Key Boundaries

- `internal/turn` → `internal/store`: in-process method call on the concrete
  `*store.DB` (`turn.Options.DB`, `turn/execute.go:91`). `turn` does **not**
  declare a store interface; TEST-5's point-of-consumption rule is already
  satisfied by the concrete type here, so no interface is to be introduced for
  the new accessor.
- `internal/turn` → runner: `agent.Request` (`prepare.go:~470-495`) carries
  `Delivery`, `Session.Ref` and `Session.Resume`. These three fields are the whole
  observable surface of a switch as far as the subprocess is concerned, and are
  therefore where the tests should look.
- `internal/store` internal boundary: `segments` and `segment_deliveries` are two
  tables in one package. The switch writes both; that is a single transaction's
  worth of work (D-B), not a cross-package concern.

### Data Flow

Ordinary turn (unchanged): label → `req.Agent` → `planDelivery` renders a bundle
**already keyed to `req.Agent`** (`delivery.go:67,79,99,107,149`) → last segment's
agent matches → identity matches → `verifyDelivery` → run.

Switched turn (new): label changed → `req.Agent` is the new kind → `planDelivery`
has already produced a new-agent bundle plan → `prepareDeliverySegment` sees
`last.Agent != string(req.Agent)` → keeps `last.Ordinal` / `last.SessionRef`
(no rotation, D-1) → `writeDelivery` writes the new bundle → the segment row's
`agent` is rewritten and `resume_token` cleared, the old delivery pin retracted →
`SetSegmentDelivery` pins the new bundle → `allocateRun` copies the *new*
`segment.Agent` into `runs.agent` (`prepare.go:1540`) → the new runner runs.

### Concurrency & Context

No new concurrency. BR-3 is correct and is the whole argument for D-2: `prepare`
runs under `AcquireSessionLock` (`turn/execute.go:548`) inside the daemon's
per-session in-flight set (`daemon/inflight.go`), so the mutation cannot land
under a running turn. CONC-2's goroutine budget is untouched. CONC-5 applies to
the new accessor: `ctx` first. No `time.Sleep` or polling in any new test
(TEST-8).

### Shared State and Side Effects

- `segments.agent` stops being immutable. Every doc comment asserting otherwise
  must change in this commit (STYLE-1, AC-10) — notably
  `store/segments.go:41-43`, `delivery.go:293-294`, `prepare.go:215-237,262-269`,
  `resumeDecision`'s own comment at `:1056-1063`, `allocateRun`'s at `:1535-1539`,
  `report/reply.go:51-58,799-806`, `turn/errors.go:68-77`,
  `turn/execute.go:686-697`, `turn/report.go:394-409`.
- `segment_deliveries` rows become replaceable *at a switch only*. Say so where
  the rule lives (`store/segment_delivery.go:10-11`).
- Orphaned bundle directories: each switch calls `writeDelivery`, which allocates
  a new `run-*` directory under `$LF_HOME/bundles/...`; the previous agent's
  directory stays on disk, unreferenced once the pin moves. This is the same
  behaviour a same-agent rotation already has, bounded by the number of switches.
  **No cleanup is to be added** (the comment at `delivery.go:188-189` forbids
  age-based deletion of retained bundles for good reason).
- Consequence of D-1 worth writing into a comment, not code: switching back to
  Claude resumes Claude's earlier conversation while handing it a *new* bundle
  root path. When the team snapshot has not changed, the bytes are identical and
  only the path differs. Accepted (R-1).

## Cross-story architectural decisions

**D-A — one funnel, not an early return.** Express the switch by *choosing*
`ordinal`/`sessionRef` before the tail of `prepareDeliverySegment`, then
reconciling the row the tail's `GetOrCreateSegment` returns. Concretely: the
mismatch case sets `ordinal, sessionRef = last.Ordinal, last.SessionRef` and falls
through, where the same-agent-different-identity case still sets
`last.Ordinal + 1`. Do not bolt a second copy of the write/pin sequence onto the
mismatch branch.

**D-B — the switch retracts the old delivery pin.** `SetSegmentDelivery` refuses
a differing re-pin by design. Make `SetSegmentAgent` do all three writes in one
`withTx`: set `agent`, clear `resume_token`, and delete this segment's row from
`segment_deliveries`. The existing pin accessor then takes its ordinary INSERT
path and stays immutable-per-tool. Rejected alternative: a separate
`RepinSegmentDelivery`, or relaxing `SetSegmentDelivery`'s `WHERE` clause — both
weaken the immutability guarantee for the ordinary caller to serve the exceptional
one. The retraction is the honest statement: the old pin named the *old tool's*
bundle, exactly as the old `resume_token` named the old tool's conversation.

**D-C — the returned `store.Segment` must carry the new values.** Four
downstream readers depend on the in-memory struct, not on a re-read:
`resumeDecision` (`prepare.go:269`), `allocateRun`'s `Agent: segment.Agent`
(`:1540`), `agent.Request.Session.Resume = segment.ResumeToken` (`:491`), and
`harvest.go:139`'s `prep.segment.ResumeToken == ""` guard, which is what lets the
new tool's first reported token be recorded at all under
`SetSegmentResumeToken`'s never-overwrite rule. Returning a stale struct is the
single most damaging mistake available in this change.

**D-D — two predicates, not one narrowed predicate.** `previousRun`
(`prepare.go:1024-1045`) answers "what ran last on this conversation" and feeds
prompt context that is deliberately tool-agnostic (`commentHistory` exclusion,
`previousMayAsk`, `PreviousMode`, `PreviousFailed`, `PreviousReason`). The Claude
resume arm asks a different question: "has **any** terminal run on this segment
already used this `session_ref` under Claude" — *any*, not *the last*, because
`session_ref` is fixed by D-1 and one earlier Claude run is enough to make a cold
`--session-id` collide. Do not narrow `previousRun` by agent; compute the
agent-match fact alongside it.

**D-E — the refusal machinery survives, narrowed.** `agentSwitchRefusal`,
`DispositionAgentSwitchRefused`, `ReplyAgentSwitchRefused` and
`reportAgentSwitchRefusal` all stay (A-2). Removing the ReplyKind churns
`report/replykind_test.go` and `report/confirm_reply_test.go` for nothing, and the
kind is still genuinely reachable on the continuation path.

**D-F — the crash window is self-healing; add no compensation.** If `prepare`
fails between the agent rewrite and `SetSegmentDelivery`, the next turn sees
`segment.Agent == req.Agent` with no pin, takes the *rotation* branch, and gets a
correct cold segment at `ordinal + 1`. That is a coherent outcome, no run was
allocated, and CONC-7 guarantees `sessions.state` was never touched. Record it in
a comment; do not write reconciliation logic for it (the rule against compensating
state logic in CONC-7 points the same way).

**D-G — do not touch `allocateRun`'s prelaunch guard** (`prepare.go:1520`). R-3 is
a pre-existing class (the same guard already fires on a mode change) and is
explicitly out of scope. Relaxing `existing.Agent != segment.Agent` here would let
a crashed attempt's run row be reused under the wrong tool.

## Per-story hints

### US-1.1: `store.SetSegmentAgent` — rewrite a segment's pinned tool
**Agent:** go-implementation-engineer

- **Pattern:** a `withTx` mutating accessor in the shape of
  `SetSegmentResumeToken` (`internal/store/segments.go:109-134`) — read first so a
  missing row reports `ErrNotFound` rather than being indistinguishable from a
  no-op write, then `ExecContext` with bound parameters.
- **Package placement:** `internal/store/segments.go`, immediately after
  `SetSegmentResumeToken`. It is that function's deliberate opposite, and the two
  doc comments should read as a pair: one never overwrites, this one always does,
  each says why.
- **Key interfaces:** none. Concrete method on `*store.DB`; `ctx` first (CONC-5).
  Reject a zero id and an empty kind the way the neighbours do; no `claude|codex`
  literal anywhere in it (BR-7) — the kind is an opaque string the caller has
  already validated against `turn.RunnerSet`.
- **Watch out for:** the transaction must also `DELETE FROM segment_deliveries
  WHERE segment_id = ?` (D-B). Without it the caller's `SetSegmentDelivery` fails
  with "already pinned differently" and every switch on a team-configured session
  returns a preparation failure. Clear `resume_token` in the same statement as the
  `agent` write, never as a second round trip.
- **Not in scope:** no migration (the columns exist, `0003_agent.sql`); no change
  to `GetOrCreateSegment`'s stored-row-wins semantics; no new sentinel error.

### US-1.2: switch in place inside `prepareDeliverySegment`
**Agent:** go-implementation-engineer

- **Pattern:** restructure the existing branch, per D-A. In
  `internal/turn/delivery.go:337-358`, the `last.Agent != string(req.Agent)` case
  stops returning `(last, nil, nil)` and instead selects `last.Ordinal` /
  `last.SessionRef`, letting the existing tail (`:359-381`) write the bundle,
  create-or-get the row, and pin the delivery.
- **Package placement:** `internal/turn/delivery.go` only. `planDelivery` already
  renders for `req.Agent` — the new agent — so **no bundle-rendering code changes**
  (`:67,79,99,107,131,149`). BR-6 is satisfied by routing the switch through the
  tail, not by adding a render step.
- **Key interfaces:** `opts.DB.SetSegmentAgent`, then the existing
  `opts.DB.SetSegmentDelivery`. Order matters: retract-then-pin.
- **Integration points:** the second `GetOrCreateSegment` call site
  (`delivery.go:318-326`, the `p == nil && !hadDelivery` path) returns a stored row
  whose `kind` argument was ignored (`store/segments.go:53-61`) and can therefore
  also be stale. Route it through the same reconciliation — a small unexported
  helper in this file taking `(ctx, opts, segment, kind)` is the right size. One
  helper, not a type.
- **Watch out for:** (a) D-C — after `SetSegmentAgent` succeeds, set
  `segment.Agent = string(req.Agent)` and `segment.ResumeToken = ""` on the struct
  you return; (b) the `req.Continuation != nil` block at the top (`:295-317`) must
  keep returning without any switch (BR-8) — do not hoist the reconciliation above
  it; (c) when `p == nil` there is no delivery to pin, but the agent still must
  switch; (d) the function's doc comment at `:293-294` currently states the
  opposite rule and must be rewritten (AC-10).
- **Not in scope:** rotation on a switch; deleting the old bundle directory;
  touching `writeDelivery`/`verifyDelivery`/`planDelivery`; anything in
  `internal/render` or `internal/teamsource`.

### US-1.3: narrow the refusal in `turn.prepare`
**Agent:** go-implementation-engineer

- **Pattern:** guard the existing `return nil, &agentSwitchRefusal{...}`
  (`prepare.go:228-237`) with `req.Continuation != nil`. Nothing else about the
  error type, the disposition or the reply changes (D-E).
- **Package placement:** `internal/turn/prepare.go`; comment rewrites in
  `internal/turn/errors.go`, `execute.go`, `report.go` and
  `internal/report/reply.go`.
- **Integration points:** the upstream cancellation at
  `daemon/continuation_dispatch.go:112-114` is the real defence; this is defence
  in depth. Say that in the comment rather than implying the branch is dead.
- **Watch out for:** `report.ReplyAgentSwitchRefused`'s **rendered text**
  (`report/reply.go:799-806`) now describes a narrower situation — an automatic
  recovery that cannot follow a label change, not a general prohibition. An owner
  reading the old wording after a successful switch would be actively misled.
- **Not in scope:** removing the ReplyKind, the disposition, `agentSwitchRefusal`
  or `reportAgentSwitchRefusal`; changing `daemon.resolveAgent`; adding any new
  reply for a successful switch (BR-9).

### US-2.1: in-flight work finishes naturally
**Agent:** go-integration-test-engineer

- **Pattern:** this story requires **no production code**. It is a property of
  `AcquireSessionLock` (`turn/execute.go:548`) plus `daemon/inflight.go`, and the
  correct deliverable is a pin, not a mechanism.
- **Package placement:** `internal/turn` (in-package, TEST-4) using the existing
  fixture (`turn/fixture_test.go`, `newStubRunner`) per TEST-3.
- **Watch out for:** TEST-8 — no `time.Sleep`, no `Eventually`, no wall-clock
  polling. If a rendezvous is needed, use channels with a timeout that names what
  it waited for; if the property can be pinned sequentially (turn A completes under
  the old agent, then turn B switches), prefer that and skip concurrency entirely.
  A sequential pin is the honest test here: nothing in the change is concurrent.
- **Not in scope:** subprocess cancellation, a new observable for lock release
  (TEST-9 forbids inventing one), any daemon-level test.

### US-3.1: `runs.agent`-aware resume decision
**Agent:** go-implementation-engineer

- **Pattern:** per D-D, compute "a terminal, non-`refused` run exists on this
  segment whose `agent` equals the resolved kind" alongside `previousRun`'s
  existing result, from the same `ListRuns` pass, and feed that fact into
  `resumeDecision`'s Claude arm. Leave `previousRun`'s returned `store.Run`
  tool-agnostic.
- **Package placement:** `internal/turn/prepare.go:983-1074`.
- **Key interfaces:** `store.Run.Agent` already exists and is already written per
  run (`prepare.go:1540`); no store change.
- **Watch out for:** (a) the Claude arm's question is "*any* prior Claude run",
  not "the *last* run was Claude" — a claude→codex→claude segment has a Codex run
  as its last (`store/runs.go`, `ListRuns` is ordinal-ordered and the loop keeps
  the last match); (b) the `SemanticOutcome != "refused"` filter is correct to keep
  — a refused run never launched a subprocess (`turn/execute.go:790`) and so never
  consumed the session id; (c) the Codex arm needs no new condition because the
  switch clears `resume_token`; (d) `resumeDecision`'s doc comment
  (`:1047-1063`) argues from an invariant this change deletes and must be rewritten.
- **Not in scope:** changing `previousRun`'s signature semantics for the prompt
  fields; touching `runClaude`'s cold rerun (`harvest.go:201-263`); anything in
  `internal/harness` or `internal/codex`; migrating historical `runs` rows.

### US-3.2: rewrite the pinned invariant tests
**Agent:** go-unit-test-engineer

- **Pattern:** `internal/turn/execute_test.go:304-378` currently asserts the
  opposite of the new behaviour. Rewrite both functions (TEST-10: a pinned
  invariant failing is a design change, made deliberately) — one becomes the
  switch assertion (AC-1/AC-2/AC-3), one becomes the narrowed continuation
  refusal (AC-8). Keep a same-agent regression pin (AC-4).
- **Package placement:** `internal/turn`, in-package (TEST-4); reuse
  `newFixture`/`newStubRunner`/`f.db` (TEST-3). Table-driven for the both-directions
  cases (TEST-1). Store-level accessor tests go in `internal/store/store_test.go`
  beside `TestSetSegmentResumeTokenNeverOverwritesANonEmptyToken` against real
  SQLite (TEST-6).
- **Integration points:** assert against rows read back from the store (same
  segment `id`/`ordinal`/`session_ref`, exactly one segment for the session,
  `resume_token` empty, `runs.agent` history), and against what the stub runner
  received (`agent.Request.Delivery` non-nil, `Session.Ref`, `Session.Resume`,
  `Path`) for AC-5/AC-6.
- **Watch out for:** AC-6 needs the recorded identity compared with the identity
  `planDelivery` produces for the **new** kind — pin the change and the derivation,
  not a hardcoded hash. EXT-1: no test may spawn a real `claude` or `codex`.
- **Not in scope:** any test that still asserts an ordinary mismatch refuses;
  new fixtures or a second fake runner type; testing `daemon`.

### US-4.1: delete `switch-agent.sh`
**Agent:** go-implementation-engineer

- **Pattern:** plain deletion. Verified: the only occurrence of the name in the
  tree outside `.aiteam/` history is the file itself — no Makefile target, no
  docs, no test.
- **Watch out for:** deleting it in the same change that lands the automatic path,
  never earlier in the sequence; the script is the current escape hatch and D-4
  accepts no fallback only because the replacement exists.
- **Not in scope:** a replacement CLI command, a deprecation shim, or a
  documentation note (A-4: there is nothing to update).

## Architecture Notes (platform lens)

- Platform: project layout — no new top-level package; everything lands in
  existing `internal/` files.
- Platform: error handling — the new accessor wraps with `%w` in the shape of its
  neighbours and returns `ErrNotFound` for a missing row; no new sentinel is
  needed, and no caller branches on the error (STYLE-5 satisfied by reuse).
- Platform: database access — parameterized `UPDATE`/`DELETE`, `ctx` first
  (CONC-5), one `withTx`. No migration, no new column, no pool change.
- Platform: interfaces — none added. `turn.Options.DB` is the concrete `*store.DB`;
  adding an interface for one accessor would be premature (TEST-5).
- Platform: concurrency — no goroutine, no shared mutable state; serialisation is
  the pre-existing session lock (CONC-2 budget untouched).
- Platform: testing — stdlib `testing`, hand-written fakes only (TEST-2),
  table-driven where there are two directions (TEST-1), real SQLite for the
  transaction promise (TEST-6), no sleeps (TEST-8).
- Platform: logging — if a switch is logged at all, one `logger.Info` on the
  injected `*slog.Logger` with structured fields, in `prepare`/`delivery`, never
  `fmt.Print*` (STYLE-2). Optional; the run report already names the tool.
- Platform: HTTP layer — not applicable (none).
- Platform: dependencies — not applicable (none added; `go.mod` keeps its four
  direct requires).
- Platform: lint — `make check` (AC-12) is the gate; comment rewrites are the
  bulk of the diff and STYLE-1 makes them mandatory, not optional polish.

## Open risks recorded, not solved

- **R-A (new)** — the `SetSegmentDelivery` immutability collision above. Resolved
  by D-B; if an engineer instead relaxes `SetSegmentDelivery`, flag it in review:
  that weakens the guarantee for the ordinary path.
- **R-B** — orphaned bundle directories accumulate one per switch. Accepted; no
  cleanup, per the retention comment at `delivery.go:188-189`.
- **R-C** — a resumed Claude conversation sees a new bundle root path after a
  switch back. Direct consequence of D-1; accepted, comment it.
- **R-D (spec R-3)** — stale `prelaunch` row plus a label change wedges one
  trigger comment. Pre-existing class, out of scope, and D-G says do not "fix" it
  opportunistically.
- **Unsatisfied evidence obligation (carried, not dropped)** — end-to-end
  observation of a real `codex`/`claude` process picking up a switched label is
  unavailable in-run (`docs/decisions/nested-agent-evidence.md`, EXT-4). It stays
  an owner-operated post-merge check and MUST NOT become an implementation or CI
  requirement.

## Out of my domain

No architect is rostered for the operator/host side of this change (the owner's
manual SSH procedure that `switch-agent.sh` served). Nothing in the plan requires
it: the replacement is entirely in-process. If an operator-facing note is ever
wanted, the engineer should follow `.aiteam/APP_CONSTRAINTS.md` and their own
judgment, flagging assumptions in their task notes.
