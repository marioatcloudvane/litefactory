# Research brief

## Request

Explain the root causes behind #371 and #370 and shape a proposed scope for recovery eligibility, durable diagnostics, failure isolation and operator resolution.

## Decisions

- **d1** — Owner-specified behavior: an old-pinned terminal instance with no unsettled work must not block any execution mode; a later authorized Research launch must create a successor using the current definition.
- **d2** — Owner-specified boundary: running old-pinned instances and instances with unsettled dispatches, owner waits or publications remain refused, with one confirmed diagnostic reply and unchanged running-dispatch and capture guards.
- **d3** — This run provides Research findings and a proposed scope only, with no implementation, production edits, direct publication or mode change.
- **d4** — Proposed primary user story: an owner receives one actionable refusal for invalid input, later eligible comments proceed, and an operator can inspect and resolve retained failures without editing the database.
- **d5** — Proposed recovery eligibility covers the latest Research instance and all predecessors reachable by publication recovery, treating open dispatches, waiting or claimed owner waits, active node attempts and unconfirmed publications as unsettled.
- **d6** — Proposed safety rule: missing or unreadable recovery records leave eligibility unknown and pending, rather than proving that no work remains.
- **d7** — Proposed refusal behavior consumes missing-feature and changed-exception comments only after confirmed durable reporting while preserving the original exception content and authority.
- **d8** — Proposed #370 behavior excludes recognized submission-v2 marker rows from legacy follow-up extraction for both existing and new records, preserving receipts and all independently pending proposals.
- **d9** — Proposed format rule: malformed or unsupported assessments are individually held with a diagnostic so other independent assessments and proposals for the same parent can proceed without treating the held assessment as accepted or materialized.
- **d10** — Proposed diagnostic delivery reuses stable retained delivery identities and bot-authored marker recovery, with post-success receipt loss, consumption faults and restart unable to create a second reply for the same event.
- **d11** — Proposed retained failure data contains task, work kind and immutable work identity, input revision, failure category and fingerprint, attempt count, first and last failure times, disposition, diagnostic delivery identity and an audited resolution.
- **d12** — Proposed failure dispositions distinguish retryable, diagnosed refusal, held unknown, resolved and explicitly skipped work, without conflating queue consumption with acceptance or remote completion.
- **d13** — Proposed interfaces provide an owner-facing diagnostic and local task failure inspection, retry and owner-comment skip controls addressed by exact failure identity and revision.
- **d14** — Proposed permissions restrict mutation to the existing authorized local operator boundary and preserve current owner identity checks for issue commands.
- **d15** — Proposed operator mutation requires a reason and current failure revision, rejects active or uncertain-effect work, records an audit entry, and makes repeated requests idempotent.
- **d16** — Proposed skip behavior consumes only the selected owner comment after its diagnostic is confirmed and never deletes graph debt, rewrites a capture pin, grants approval or marks background work successful.
- **d17** — Proposed retry behavior reopens only the selected held item with a recorded resolution attempt and retains its existing effect identities and failure history.
- **d18** — Proposed acceptance covers failed and cancelled old instances across modes, unsettled predecessors and missing records, refusal delivery fault recovery, old and new v2 markers, malformed assessment isolation, restart-persistent retry limits and stale or repeated operator requests.
- **d19** — Proposed out-of-scope work includes automatic pin migration, replay of old effects under current policy, new receipt-based follow-up generation, a replacement delivery journal, production repairs and unrelated legacy-reader retirement.
- **d20** — Proposed executor capability checks surface missing owner-control wiring once at startup and in failure inspection while affected work remains pending.


## Assumptions

- **a1** — The current request broadens the investigation to the shared causes across #371 and #370; the proposed scope covers their concrete defects and common recovery behavior, subject to q1.
- **a2** — The #320 instance IDs, affected-session list, deployment dates and manual repair, and #370's production rows and log counts, are owner reports rather than independently verified production observations.
- **a3** — Unsettled work is determined from retained obligations, not terminal status alone; cancelled dispatches are terminal, whereas pending/reserved/running dispatches, waiting/claimed owner waits and pending/running node attempts remain open.
- **a4** — Recommended policy pending q2: recognized permanent safe refusals report immediately; unknown work is held after five consecutive matching failures spanning at least fifteen minutes; known temporary store, forge or lock failures remain retryable.
- **a5** — Failure matching uses work identity, input revision and a stable cause category rather than timestamps in error text; a changed input or cause starts a new sequence, restart preserves it, and confirmed success resolves it.
- **a6** — Existing submission receipts remain authoritative for v2 findings, and skipping a compatibility marker neither invents follow-up proposals nor discards already retained proposals.
- **a7** — Independent background work may continue around a held assessment, but operations requiring that assessment or unresolved external effects remain blocked.
- **a8** — Previously saved test results apply to adfe773b890319948432268f9246cf089029d3bf; this shaping pass inspected d4baf006eaa386fd4c817a6b9cfb80208417e58c and ran no new tests or production probes.


## Open questions

### q1 — Should the proposed fix cover both owner-command handling and story-progress recovery?

_Asked of: Product Owner Questions_

- **a** — Recommended: cover the concrete defects in #371 and #370 plus shared diagnostics, failure isolation and operator controls; this addresses the larger failure pattern.  *(recommended)*
- **b** — Limit this scope to the known exception, stale-pin and v2-marker defects with durable diagnostics; defer the general retry bound and operator controls as explicit remaining work.

### q2 — What should happen when an unknown failure reaches the retry bound?

_Asked of: Product Owner Questions_

- **a** — Recommended: after five consecutive matching failures spanning at least fifteen minutes, send one diagnostic and hold that item for an operator; independent background work continues, but a held owner comment keeps its ordered queue pending until safe retry or explicit skip.  *(recommended)*
- **b** — Automatically consume the owner comment after the same bound and confirmed diagnostic only if no active or uncertain effect exists; otherwise hold it, and always hold failed background work rather than mark it successful.
