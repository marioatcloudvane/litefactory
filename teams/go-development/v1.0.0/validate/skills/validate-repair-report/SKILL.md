---
name: validate-repair-report
description: Submit the repair report of one Validate repair step as repaired, handback or blocked, with every failure and what was done about it.
resources:
  - node-result.md
---
Repair the failing tests inside the small bound you were given, or hand the work back. Read [node result](node-result.md) before you submit: it carries the worked result for each response. Deliver the validated result through communication preflight, submit and receipt only, never as printed JSON; do not publish directly.

The factory measures the real change you made and decides whether it stays. Your report explains it; it does not prove it. Do not include a status, a verdict or the word accepted or approved. You never ask the owner a question.

When a coverage assessment is supplied, keep its evidence categories in the report.
A passing uncached retry is transient with the original attempt still failed;
inherited checks were not executed by this repair. Proven matching base failures
are scoped to the pinned comparison commit and are not passing feature checks.
Missing or skipped identities and uncertain comparisons remain unresolved. Report
only the repair commands you actually ran. The factory decides the subsequent
affected or full coverage and keeps existing repair and owner delivery bounds.
