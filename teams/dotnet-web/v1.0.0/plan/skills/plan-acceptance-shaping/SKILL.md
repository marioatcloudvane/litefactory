---
name: plan-acceptance-shaping
description: Define observable completion and assemble the third-stage review.
requires: [plan-evidence]
resources:
  - implementation-engineer.md
  - integration-test-engineer.md
  - unit-test-engineer.md
---
# Acceptance shaping
Use stable explicit criterion IDs. Write each criterion in plain language:
- `text`: one sentence from the user's point of view ("You can …", "The owner
  sees …"), ending with its marker `(Auto)` or `(Manual)`. No internal jargon
  without a short explanation.
- `proof_method`: how it will be proven, briefly. Automatic: which test, in a few
  words. By the owner: start with `Owner check:` and say what they do. Then
  `Covers: O-…`.
Most criteria are (Manual): the Validate reviewer judges them by reading, or the
owner checks them. At most one per story is (Auto), the end-to-end test of its
main path. About five to seven criteria per story at most, usually fewer; finer
detail belongs in the tasks. A markdown-only story gets no language-test proof:
its proofs are a text review or a structural check (plan-evidence Work kinds).
Example: `You can export a project as one file. (Auto)` — `End-to-end test
exports a fixture project and compares the file. Covers: O-01`.

What (Manual) means: the criterion is checked in Validate, by the reviewer
reading the delivered work or by the owner, never by the story worker. An owner
check names only the owner's action and the visible result ("Connect Claude, ask
what needs attention, see the waiting items with links"), never test
infrastructure, fixtures or code inspection. Anything a machine can prove goes
into the (Auto) criterion or a task's tests. A story never needs live external
access (a real hosted client, a test endpoint, owner credentials) to be built;
such a check is an owner (Manual) criterion.

A criterion promises only what the recorded external-system limits allow; an impossible
guarantee becomes an ask with options, never a criterion. Build complete task-to-criterion links using only
authorized owner roles; each task states its kind and follows the `S-01` mode
(plan-evidence Work modes, Work kinds); validate tasks and dependencies. Keep proposed completion
obligations distinct from actual executed evidence: no predicted check is a pass.
Preserve the approved intent, applicable general/data/service/solution tables,
selected guidance, frontend references, decisions and assumptions unchanged unless
an explicit scoped revision reopens the earliest affected stage.

Assemble the exact current active story set, dependency edges and shared-decision
revision for third-stage owner review together with acceptance criteria. There is
no fourth stage. Exclude confirmed retired scope explicitly and preserve its
history/replacements. Hold empty scope, cycles, dangling/retired references,
unresolved material questions, pending edits/effects and incomplete typed stories.
The factory reuses existing draft issue identities, publishes the link-only parent
index and saves before readiness; this role cannot approve or freeze scope.
