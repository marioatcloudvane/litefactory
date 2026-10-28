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

## Detail from supplied story size
Use the matching set entry's supplied size and reason, following plan-evidence's
shared sizing rules. Do not assign or rescale a size here. Missing or invalid
size keeps the existing planning depth, with an honest unavailable-size note;
a valid size still guides depth when its reason is unavailable. Exempt old sets
receive no request to add sizing, including through replanning. Missing
applicability does not authorize backfill.

These examples change explanation depth only. At every size retain all agreed
outcomes, relevant `N-`/`K-` entries, `Covers:` and `Ruling:` notes and required
proof links. Keep stage ownership, owner gates and required evidence unchanged.
The examples are guidance, with no numeric task-count or criterion-length rules.

- **S — local wording change:** use a small set of meaningful work items and
  short observable criteria with concise proof methods. Include the affected
  outcome and compatibility evidence; omit redundant tasks and separate
  criteria for each sentence or file. Markdown proof remains review and
  structural checks, with no language tests.
- **M — a role and its supplied facts change together:** separate meaningful
  delivery work from compatibility evidence, link each task to criteria and
  describe the main path and missing-data proof in normal detail. Omit
  repetitive criteria for individual edge cases that belong in task detail.
- **L — migration across several components:** show dependency order and
  observable transition, compatibility, retirement and recovery proof, with
  tasks that preserve those obligations. Omit unrelated checks and duplicate
  criteria; size does not increase the permitted number of Auto criteria or
  justify dropping outcome coverage.

## Size beside each story in the final review
In the existing assembled final-review comment, put each story's retained `size`
and `size_reason` beside that story, taken from its matching active set entry.
Do not duplicate these facts into story intent or invent a story-draft sizing
field. Keep the outcome table, `Covers:`, `Ruling:` and task-to-criterion proof
links even when the review is concise.

Examples of honest presentation (use the actual supplied facts):
- Sized entry: `S — One role text changes; low risk, no unknowns or migration.`
- Valid size with missing reason: `M — reason unavailable`; use M depth.
- New-set missing size: `Size unavailable; reason unavailable — new-set sizing
  gap`; retain a supplied reason if one exists, and keep existing depth.
- Invalid size: `Size unavailable (invalid supplied value); reason: <supplied
  reason or unavailable> — new-set sizing gap`; keep existing depth.
- Exempt old set without sizing: `Size and reason unavailable — exempt old set`;
  do not request sizing, including through replanning.

If applicability is unavailable, say that it is unavailable instead of labelling
an entry exempt or requiring backfill. Report supplied valid reasons faithfully.
Sizing gaps remain advisory, with repair only on the next supported plan-set
invocation; they add no review gate, stage or repair loop and grant no authority
to replace a promoted set.
