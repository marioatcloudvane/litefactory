# Drift Report — switch-agent-by-label

Compared against `plan_brief.md` / `ARCH_REVIEW.md` and the approved story revision
(issue #252, revision `75fcd752b8f4b40986f1af3a9994afaaf59abab5f0599229559a45b801635054`).

## Delivered as planned

All 7 tasks landed exactly as specified:

- `store-set-segment-agent` — `store.DB.SetSegmentAgent` added in `internal/store/segments.go`
  immediately after `SetSegmentResumeToken`, single transaction, rejects zero id / empty kind,
  `ErrNotFound` for a missing row, retracts the segment's delivery pin.
- `delivery-switch-in-place` — `prepareDeliverySegment` in `internal/turn/delivery.go` performs
  the switch in place on a mismatch instead of refusing; both `GetOrCreateSegment` call sites
  routed through the same reconciliation; returned `*store.Segment` reflects the switch.
- `prepare-narrow-refusal` — refusal narrowed to the continuation-only path in
  `internal/turn/prepare.go`; rendered reply text and doc comments rewritten.
- `prepare-force-cold-on-switch` — `resumeDecision` forces cold unconditionally on a switch turn.
- `inflight-turn-finishes-under-old-agent` — sequential pin added, no production change.
- `rewrite-pinned-invariant-tests` — old refusal-pinning tests rewritten; new coverage for
  AC-013 through AC-018, AC-020; store-level test for the new accessor added.
- `delete-switch-agent-script` — `switch-agent.sh` deleted from the repo root, no replacement
  added, landed together with the narrowed refusal.

## Added scope (with why)

- **`Segment.AgentSwitched bool`** — a transient, non-DB-column field on `store.Segment`, added by
  the implementation engineer. Not named in the story's "Interfaces to add/change" list. Needed
  because `resumeDecision` must force cold *unconditionally* on "this turn just performed a
  switch," and neither `ResumeToken` emptiness nor `SessionRef` divergence from
  `naming.SessionRef` can distinguish "just switched this turn" from "continuing since an
  earlier switch" without a history lookup, which task `prepare-force-cold-on-switch` explicitly
  forbids. Modeled on the existing precedent of the same struct's `DeliveryIdentity`/
  `DeliveryRoot` transient fields. No DB migration, no new column — consistent with the story's
  "no migration" constraint.
- **`freshSegmentSessionRef()` helper** — mints the new `session_ref` in canonical-UUID shape
  (matching what `naming.ValidClaudeSessionID` and the harness's session-id validation expect)
  using `crypto/rand`. The story specified the *requirement* (a fresh value the row never held
  before) but not the exact generation mechanism; this satisfies AC-017's "never a used-before
  session id" and keeps a switch into Claude passing existing validation, without introducing
  any `claude`/`codex` string literal.

Both additions are internal implementation details required to satisfy the frozen acceptance
criteria as written; neither changes the story's external behavior, architecture, interfaces
list beyond what was already specified, or any ring-fenced area.

## Dropped or deferred

None. All 7 tasks and all in-scope acceptance criteria (AC-013 through AC-024) are addressed.

## Architectural deviations

None from the approved architecture (`internal/store/segments.go` mutating accessor +
`internal/turn/delivery.go` in-place switch + `internal/turn/prepare.go` narrowed refusal and
forced-cold resume decision). The two additions above are implementation-level, not
architectural — no new package, interface beyond what's documented, config key, goroutine, or
migration was introduced, matching the story's Constraints section.

## Verdict

**CLEAN** — implementation matches the approved plan and story revision; the two additions are
necessary, narrowly-scoped implementation details to satisfy AC-017's unconditional-cold
requirement and AC-023's no-hardcoded-literal requirement, not scope creep. No follow-up
planning cycle is warranted.
