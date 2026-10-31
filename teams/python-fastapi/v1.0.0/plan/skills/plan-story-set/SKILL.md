---
name: plan-story-set
description: Split agreed parent scope into independent, ordered stories without inventing or dropping scope.
requires: [plan-evidence]
---
# Story set
Start from the parent thread and the saved research scope. List every deliverable,
decision and requirement they settled, then group what belongs together into one
story; its pieces become that story's tasks later. Tasks are the key work items:
fewer stories, more tasks. A separate story needs a real reason: independently
deliverable and reviewable, a clearly different area or risk (for example a
retirement or migration), or a forced ordering dependency. Default to one story for
small work, rarely more than two for medium. Each extra story justifies itself in
one line in the comment. When in doubt, fewer stories. Never cut one outcome into
layers, chores or "part 1/part 2".

Account for all agreed scope. Each item lands in exactly one entry; say in the
comment what you left out and why. Do not add scope nobody agreed. Map every `O-`
outcome of the Research ledger to its entry as plan-evidence's Outcome ledger says.

Boundaries: give every entry a one-line intent of at most 280 characters that says
what the story delivers and where it stops, in plain language, with no file paths
or implementation identifiers. Names are short, single-line and unique, at most 80 characters.

Order and dependencies: give every entry a temporary id (`s1`, `s2`, ...) that is
unique inside the set and means nothing outside it. List entries so prerequisites
come first. Use `depends_on` only for a real prerequisite: an entry that cannot
start, or cannot be accepted, before another. It names ids from this set, never the
entry itself, and never forms a cycle. Independent entries stay independent. With
more than one entry, a small Mermaid flowchart of order and dependencies may say it
faster than the list (owner-report).

Shared places: entries without a dependency are built at the same time, so two of
them must not extend the same shared place (a command list, a route or menu table,
a schema or migration sequence, a config file, one documentation page). If they
would, the entry that creates the shared place comes first and the others depend
on it, or the entries become one.

Set length: a set has 1 to 12 entries. More than 12 means the parent is too large for
one plan; ask the owner to split the parent instead of squeezing the list.

Per-story sizing: follow plan-evidence's Story sizing rules. Refine Research
`S-01` separately for every new-set entry using its touched parts, risk, unknowns
and retirement or migration work, explicitly including when none is needed.
Supply `size` as `S`, `M` or `L` and a one-line `size_reason` of at most 280
Unicode characters, separate from `intent`. With no usable Research size, use
story evidence and state that assumption in the comment. For example:

- S: a local wording correction with a known text owner, low risk, no unknowns
  and no retirement or migration.
- M: a role and its supplied facts change together, with compatibility risk and
  a bounded fallback question, but no retirement or migration.
- L: a migration across components with dependencies, transition unknowns and
  old code to retire.

These are qualitative examples, not scores or task-count thresholds. Fill
reported new-set sizing gaps on the next supported proposal invocation as the
shared rules say; never request sizing for exempt old sets or replace a promoted
set to repair a gap.

Deliver the set as a storyset in your shared result; the factory shows it to the
owner and creates the sub-issues only after the owner promotes it. Offer revisions
for owner review, not owner promotion. Reuse settled answers when the owner asks to
merge, split, rename or reorder, and keep the parts they did not change. Leave story
content absent: no drafts, architecture, tasks, acceptance criteria or placeholders.

Ordinary owner comments can arrive at any Plan state. Answer questions about the retained set and stories in result.comment with a completed result, an empty plan payload, no questions and neither storyrequest nor storyset. Only return a revised set when the owner asks for a supported change to an unpromoted set. Replacing a promoted set is unsupported: explain this in an answer rather than proposing a replacement. Answering never grants approval.
