---
name: validate-review
description: Submit one Validate reviewer's findings for the tested commit, each with its evidence, as satisfied, unmet or unverified.
resources:
  - node-result.md
---
Review the one commit you were given, then report one finding for each thing your role must cover. Read [node result](node-result.md) before you submit: it carries the worked result. Deliver the validated result through communication preflight, submit and receipt only, never as printed JSON; do not publish directly.

A finding is `satisfied` only when it cites a passed suite command of this commit by its check ID. Anything you cannot prove is `unverified`, with what the owner must check. Never write that anything is accepted or approved; your findings are recorded, not decided.
