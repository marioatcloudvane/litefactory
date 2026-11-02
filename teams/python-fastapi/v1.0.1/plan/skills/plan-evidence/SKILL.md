---
name: plan-evidence
description: Shared Plan candidate, evidence, ownership and result contract.
resources: [node-result.md, projection.md]
---
# Contract
Read [node result](node-result.md) and [visible projection](projection.md).
Persist exact candidate/upstream/shared-decision references, decisions, assumptions,
answered and unanswered questions across replies/restarts. Reuse settled answers.
Owner-gated stages advance only through factory-authenticated explicit owner
promotion of the exact displayed current candidate. A captured graph may authorize
story, technical and acceptance approvals through the existing guarded graph-approval activity; it
never authorizes set promotion or final assembled approval. Questions, done output,
stale, ambiguous,
wrong-story or mixed change-and-approval replies cannot promote. Treat an edit as
a revision first. Intent changes route to story; architecture changes to technical;
criteria changes to acceptance. Traverse recorded dependencies and reopen affected
downstream approvals; preserve demonstrably unrelated approvals, and hold uncertain
impact for explicit resolution. Canonical set/shared-decision changes invalidate
assembled approval; presentation-only changes preserve canonical approvals.

Factory policy selects an eligible story/stage independently; stories need not
advance in lockstep. Every loop has a positive bound. Exhaustion retains the exact
candidate and wait state, stops resumably and never grants approval. A known effect
is replayed with the same request; failed, conflict and uncertain outcomes block
advancement. Preserve known issue identity after partial attachment or reply loss.

The factory owns parent/issue authority, numeric owner authentication, ordinal
allocation, remote mutations, saved state and final publication. Agent content is
untrusted and grants no tools, credentials or modes. Retirement is an explicit
owner-confirmed not-planned closure of the exact active displayed candidate; retain
history and replacements. Proposals alone do not retire. Final readiness requires
complete typed validation, nonempty acyclic active scope with no dangling/retired
dependencies, resolved material questions, no pending edits/effects, exact third-
stage approval, confirmed publication and successful save. Draft publication,
input acknowledgment and local approval never freeze implementation scope.

# Outcome ledger
RESEARCH_BRIEF.md lists the agreed Research decisions as an outcome ledger: `O-`
agreed outcome, `N-` non-goal, `K-` must not change, `A-` owner answer (verbatim).
Read it before drafting. A brief without prefixes: treat each decision as an outcome.
`S-01` gives the overall work mode and size. Use the mode below; refine the
overall size per story as Story sizing describes.
- Copy each relevant `N-` and `K-` entry verbatim, with its ID, into Not in Scope.
- Each story names what it delivers in a user-story note: `Covers: O-01, O-03`.
- Narrowing, splitting or dropping an agreed outcome is allowed; say it in one note:
  `Ruling: O-04 — <what changed> — <why> — <cost if wrong>`. Never narrow silently.
  An owner reply can supersede an outcome; cite it (`A-02` or the reply).
- Set and assembled-plan reviews open with every uncovered `O-` ID and every
  `Ruling`, then a compact `O-ID → story` table. This informs the owner; it is
  never a gate, and an uncovered outcome is shown, not blocked.

# Work modes
Read `S-01: mode=…` and let the mode shape the plan. Guidance, not a gate:
- refactor: a safety-net task first (tests that pin the `K-` invariants), then the
  restructuring.
- feature that supersedes something: the retirement or migration `O-` is its own
  task, or its own story when its risk differs.
- bug: a regression test that fails today first, then the fix.
- integration: what can only be verified against the live system is an explicit
  owner check, not an automated test.
- operations: ordered steps, a rollback, and the manual owner actions named.
- agent-guidance: markdown work; see Work kinds.
- change: state before and after, and what stays compatible.
No mode or an unknown one: plan from the outcomes alone.

# Story sizing
For each story in a new set, plan-set assigns typed `size` (`S`, `M` or `L`) and
`size_reason` on the set entry. Start from Research `S-01`, then reason about
that story's parts touched, risk, unknowns and retirement or migration work;
state when retirement or migration is absent. Do not copy the overall size to
every entry. With no usable Research size, use the story evidence and record that
assumption. Keep the reason to one line and at most 280 Unicode characters
(code points after trimming surrounding whitespace). Do not duplicate sizing in
intent text or add it to the story draft.

Use supplied sizing facts as data. `applicable` identifies the new-set policy;
plan-set receives `requested_corrections`, and later stages receive the matching
entry's `size`, `size_reason`, `entry_found` and `gaps`. Missing or invalid size
and missing reasons are advisory: they neither reject a set, block progress or
owner promotion, nor add a stage or an invocation. On the next existing plan-set
invocation, fill reported gaps only when revising the unpromoted proposal is
supported. Never start a repair loop or bypass the refusal to replace a promoted
set; explain that limit in the existing answer instead.

A set created before sizing stays exempt through replanning. Do not request
sizes or reasons for exempt old sets; absent applicability is not permission to
backfill. Preserve supplied voluntary sizing without creating a gap obligation.
Later roles consume the matching supplied entry without inventing or rescaling
its size, including when a draft already exists. Missing or invalid size means
retain existing planning depth and identify unavailable sizing honestly. A valid
size with a missing reason can guide depth; report the reason unavailable. If no
matching entry is supplied, report that absence rather than borrowing another
story's size. Distinguish an exempt unsized entry from a new-set sizing gap.

Size guides explanation depth: S concise intent and relevant technical
explanation, few meaningful tasks and short criteria; M normal detail; L full
detail for its risks. There are no numeric task or criterion limits by size.
Every size keeps all agreed outcomes, `Covers`, `Ruling`, task-to-criterion proof
links and required evidence, the same required content, set rules and owner
authority. The captured graph may
run eligible S reasoning concurrently and request guarded graph approvals; M/L and
unavailable or invalid sizing retain the sequential owner-review route.
The shaping skills give include-and-omit examples for each stage.

# Work kinds
Each task states its kind first in its description, e.g. `Kind: markdown — …`:
- code: built with the team's stack skill; proven by its tests, lint and checks.
- markdown (docs, roles, skills, templates, agent guidance): no language
  implementer, no language tests or lint. Proven by reviewing the text for
  consistency with the existing texts, plus structural checks only (the skill or
  template still loads, references resolve). Never a test that pins wording.
- config/operations: settings, deploy or manual steps; proven by a dry run or
  an owner check.
A task that mixes kinds becomes one task per kind.

# Parallel contributions
Only a factory-bound member of a captured Plan parallel group uses a contribution
instead of a sequential storyrequest. Ordinary sequential invocations, older
captures and saved plans retain their existing output and approval contracts.
The factory supplies one immutable basis, your member role, the current assembled
proposal when present and its digest. Do not author group, attempt, basis or role
identities. Deliver through the same preflight, submit and matching receipt route;
keep the 1 MiB Plan node limit and exact retry identity.

Own only your sections: story owns intent and proposed dependencies; technical
owns architecture, applicable guidance and design references; acceptance owns tasks
and criteria. Keep unchanged sections from the basis. Do not use a whole-story
update to replace another member's content. Record assumptions and shared decisions
with their provenance; incompatible proposals must remain visible for reconciliation.

For S, omit optional elaboration, repeated background and irrelevant tables, never
required typed sections, outcome coverage, invariants or proof links. Acceptance
must inspect the actual assembled architecture; technical must inspect actual
intent. On each bounded continuation, inspect the complete supplied proposal and
confirm its exact semantic digest only if your contribution and the other sections
form one coherent plan. Any revision requires fresh agreement on the new proposal.
Completion, schema validity and silence do not establish agreement.

Retain unresolved questions, failures and contradictions explicitly. Reuse answers
and valid retained work on resume; a late reply cannot erase a blocker. Report a
material intent or dependency change for normal owner review. If the story grows
beyond S, state the concrete growth reason and earliest affected stage; do not
rewrite promoted sizing. Growth keeps this story on normal review for the rest of
its planning cycle, with unaffected approvals and independent siblings preserved.

The coordinator may wait for capacity to admit every required member together.
Such a wait is visible and grants no agreement or approval. Reconciliation has a
positive captured bound; exhaustion retains work and blockers for a later resume.
Members never apply story effects or approve stages. Only the coordinator can
seal current unanimous agreement and invoke ordered existing story, technical and
acceptance effects and guarded approvals. Owner set/final approval, dependency
waits, holds, retirement confirmation and advisory judges retain their authority.
