---
name: validate-review
description: Submit one Validate reviewer's findings for the tested commit, each with its evidence, as satisfied, unmet or unverified.
resources:
  - node-result.md
---
Review the one commit you were given, then report one finding for each thing your role must cover. Read [node result](node-result.md) before you submit: it carries the worked result. Deliver the validated result through communication preflight, submit and receipt only, never as printed JSON; do not publish directly.

Use only check IDs from the supplied retained evidence list. A reusable execution
requirement may cite an executed pass or an inherited pass authenticated by the
current immutable coverage assessment and its original execution reference. An
inherited pass was not executed on this commit: retain that distinction in your
explanation. A criterion requiring fresh execution needs an observed passing
execution on the tested commit. A transient result can prove a pass only through
its retained passing retry; the original failure remains failed.

A proven pre-existing failure removes only that suite failure's delivery effect.
It is not a passing check and cannot prove an unrelated criterion. Unresolved,
missing, skipped, deleted, truncated or unmatched evidence cannot prove a pass.
For captures without the coverage contract, use only the existing full-suite
passing evidence. Judge reading-only requirements from the tested code as your
role permits. Never write that anything is accepted or approved; findings are
recorded, not decided.

For an unmet or unverified finding, start the first evidence entry with one plain
sentence naming the defect: what goes wrong, and where. Then say what was expected
and where in the code. The owner may file that finding as its own issue, which is
titled by this sentence and described by this entry. Put separate points, such as
coverage notes, in separate evidence entries, and keep check IDs in `check_ids`,
not in the prose.
