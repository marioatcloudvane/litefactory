# Research brief

## Request

Evaluate where and how to add agent-reported per-story Implement progress through a new MCP tool, show the states, and edit an existing parent comment instead of posting every update.

## Decisions

- **d1** — The shaping deliverable defines the Implement progress scope for owner review without implementing or publishing it.
- **d2** — Reuse the parent Implement hand-off comment and retain its final summary sections alongside the live story table.
- **d3** — Display all managed stories in story-number order with linked story, status and optional detail.
- **d4** — Use the eight display states listed in the review and derive all non-running states from the factory ledger.
- **d5** — Allow active workers to report implementing, testing or repairing through story_progress_update with positive sequence and optional 512-byte plain-text detail.
- **d6** — Derive parent, story and attempt authority from invocation context and permit progress only for the currently active running worker.
- **d7** — Persist attempt-scoped phase, sequence, message and report time, plus durable parent publication identity and content fingerprint.
- **d8** — Treat identical retries as idempotent, reject conflicting or stale sequences, and allow increasing sequence gaps.
- **d9** — Reset advisory state on each attempt and let terminal ledger transitions override all worker reports.
- **d10** — Require reports at worker start and phase changes, including repair entry, without periodic heartbeat calls.
- **d11** — Target changed-table publication within 30 seconds under healthy operation and coalesce edits to at most one per 15 seconds.
- **d12** — Publish at Implement entry, after relevant ledger changes and at hand-off, skipping unchanged rendered content.
- **d13** — Serialize parent publication and converge on the latest snapshot across simultaneous worker reports and edits.
- **d14** — Recover publication after restart and reconcile uncertain creation before creating another comment.
- **d15** — Replace a confirmed deleted owned comment once while retaining one shared report identity.
- **d16** — Reject invalid input without mutation and keep durable progress acceptance separate from publication, tests and acceptance.
- **d17** — Render parked reasons and dependency blockers safely and retain reported phases during silence without inferring worker failure.
- **d18** — Exclude scheduling, merge, acceptance, Validate reporting, heartbeat, log streaming and per-update comment changes.


## Assumptions

- **a1** — The requested main story is the managed feature parent, and each row is an approved Implement story, based on the comparison with Plan's parent summary.
- **a2** — Agent activity is advisory, so completion labels derive from factory results rather than progress calls certifying tests, acceptance or delivery.
- **a3** — The tool schema, shared hand-off identity and cadence in this scope are shaping choices inferred from the edit-in-place request and existing factory reporting precedent, pending owner review.
- **a4** — The 30-second publication target applies during healthy daemon and GitHub operation; outages retain pending delivery and the last accepted progress.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
