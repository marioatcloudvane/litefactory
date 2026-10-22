# Research brief

## Request

Shape issue #1049 into a buildable scope covering B1–B4, preserving established research and asking the owner material decisions.

## Decisions

- **d1** — This pass shapes the B1–B4 scope for owner review without code changes, story creation, commits, pushes or an approval claim.
- **d2** — Preserve captured evidence before excerpting and retain the existing inline display limits.
- **d3** — Separate capture completeness, evidence availability and excerpt truncation in all records and consumer displays.
- **d4** — Use durable immutable references with source identity, capture time, byte count and digest across restart, resume and suite reuse.
- **d5** — Keep conversation answers tool-free and supply a directly fetched target body separately from bounded surrounding facts.
- **d6** — Preserve suite redaction and execution overflow behavior and never describe uncaptured output as complete.
- **d7** — Treat unavailable or corrupt evidence as an explicit handoff error without changing the recorded command outcome.
- **d8** — Exclude evidence from source-change calculations and enforce existing repository permissions with rooted regular-file access.
- **d9** — Require end-to-end readability checks across each cap, cleanup, restart, reuse, cancellation and artifact failures.


## Assumptions

- **a1** — The intended direction is to retain full safely captured evidence while keeping inline excerpts bounded, based on the supplied issue; it does not authorize removing execution, disk or model isolation safeguards.
- **a2** — Current source is the behavioral baseline; the issue's 032c55a6 line references are historical. No claim is made about production incidence or the assertion that failures are usually in the middle.
- **a3** — Thread coverage, untracked file bodies, retention and target/patch overflow policy remain owner decisions q1–q4; all recommendations remain proposals until settled.


## Open questions

### q1 — Must workflow snapshots include the full remote comment thread?

_Asked of: Product Owner Questions_

- **observed** — Recommended: retain the full locally observed history and label its coverage; this fixes truncation without adding remote-thread synchronization.  *(recommended)*
- **remote** — Fetch and paginate the full remote thread before each new capture; report a blocked context handoff if fetching fails, and accept extra latency and forge traffic.

### q2 — Should repair evidence preserve untracked text file bodies?

_Asked of: Product Owner Questions_

- **include** — Recommended: include nonignored regular untracked text files within repair scope before reset, using the same safety and size rules; list binary files and symlinks as metadata with an explicit coverage limitation.  *(recommended)*
- **tracked** — Retain tracked patches only and list untracked paths with a clear statement that their contents are absent; reviewers may lose newly created code after reset.

### q3 — How long must evidence remain readable?

_Asked of: Product Owner Questions_

- **records** — Recommended: retain evidence while any retained run or session record references it; scratch and worktree deletion must not remove it, and final record deletion permits reclamation.  *(recommended)*
- **expiry** — Expire evidence 30 days after its originating run finishes even if records remain, keep expiry metadata, and show explicit unavailability on later review or reuse.

### q4 — Which resource limits should govern target bodies and repair patches?

_Asked of: Technical Questions_

- **bounded** — Recommended: support target bodies up to 256 KiB of UTF-8 source text and tracked plus untracked repair text up to 16 MiB per capture; never silently truncate, report over-limit evidence explicitly, block a complete repair-evidence handoff, and answer oversized target requests only with an explicit limitation and independently supported facts.  *(recommended)*
- **stream** — Stream repair evidence without a new fixed byte ceiling under existing cancellation and disk-error handling, and admit target bodies up to the configured model context capacity; this supports more input but makes resource use less predictable.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
