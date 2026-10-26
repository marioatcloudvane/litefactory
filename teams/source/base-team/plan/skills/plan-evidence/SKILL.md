---
name: plan-evidence
description: Shared Plan candidate, evidence, ownership and result contract.
resources: [node-result.md, projection.md]
---
# Contract
Read [node result](node-result.md) and [visible projection](projection.md).
Persist exact candidate/upstream/shared-decision references, decisions, assumptions,
answered and unanswered questions across replies/restarts. Reuse settled answers.
Only factory-authenticated explicit owner promotion of the exact displayed current
candidate can advance a stage. Questions, done output, stale, ambiguous,
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
`S-01` gives the work mode and size; let it set the depth and shape of the plan
(Work modes below).
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
