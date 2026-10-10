# Research brief

## Request

Rework research for the factory-wide, mandate-based babysitter (issue #530 direction change of 2026-10-10): what exists to build on and what is missing.

## Decisions

- **D1** — Factory-wide babysitter as a separate process, started only from the CLI or a remote channel, never from tickets. Settled by the owner on 2026-10-10. Replaces the Bug-only scope.
- **D2** — Work under a mandate with an expiry, not an on/off switch. Settled by the owner.
- **D3** — v1 is observe and steer only. Deploys, doctor apply, team release or link and config edits are not in v1. Manual merges, database writes, design decisions and closing tickets are never allowed. Settled by the owner.
- **D4** — Steering uses a delegated right through the daemon, not owner comments. Ticket notes are traceability only. Settled by the owner.
- **D5** — Approve when sound; ask when wrong, a redesign, or a design decision. Settled by the owner.
- **D6** — Banned actions are not available to the babysitter at all, and the daemon enforces mandate scope and expiry. My decision.
- **D7** — One babysitter session per factory; a new mandate replaces the old after confirmation. My decision.


## Assumptions

- **A1** — The delegated right is a new daemon concept naming the session and mandate, tied to each approval's analysis revision. Rests on F1, F2, F3.
- **A2** — The babysitter prefers an agent other than the one that wrote the analysis when two are configured, always at flagship. Rests on the earlier owner request and the doctor's provider selection (F4).
- **A3** — A full wake-up list is replies, failed runs, deliveries and idle phases. Rests on F9 and the owner's direction.
- **A4** — Load-flake handling is model judgement under the mandate, not a hard list. Rests on the owner's learnings that routine answers were judgement calls.


## Open questions

### Q1 — How do we handle the missing Discord and Linear dependencies?

_Asked of: Product Owner Questions_

- **a** — Recommended: build the CLI window first and define narrow chat and ticket interfaces. Discord and Linear plug in when #562 and #556 land. GitHub is the first ticket implementation.  *(recommended)*
- **b** — Wait for #562 and #556 before starting. Cleaner, but blocks on two unfinished items.
- **c** — Build Discord inside this work. Large, and duplicates #562.

### Q2 — Should a hard floor sit under 'approve when sound'?

_Asked of: Product Owner Questions_

- **a** — Recommended: yes. The daemon refuses a babysitter approval for any analysis over 10 files or 3 packages, or with a migration or new stored state. The owner decides those.  *(recommended)*
- **b** — No floor. Pure model judgement. Fastest, but one bad call starts a large fix.
- **c** — Stricter floor of 5 files and 2 packages.

### Q3 — How is a mandate written and confirmed?

_Asked of: Product Owner Questions_

- **a** — Recommended: owner writes it in plain words. The babysitter restates it as targets, actions and expiry. Nothing starts until the owner confirms in the session.  *(recommended)*
- **b** — Fixed command syntax only. Precise, but stiff.
- **c** — Start at once without confirming. Quick, but a misread mandate runs.

### Q4 — What limits apply to filing Bugs and spending?

_Asked of: Product Owner Questions_

- **a** — Recommended: at most 5 new Bugs per mandate, no duplicates of open ones, and a token budget per mandate. At the cap it stops and asks.  *(recommended)*
- **b** — No caps. Risk of runaway cost and ticket noise.
- **c** — No Bug filing in v1. Safest, loses the most valuable part.

### Q5 — How does the babysitter observe the factory?

_Asked of: Technical Questions_

- **a** — Recommended: reuse the doctor's isolated read tools and provider selection, read-only, with no write tool in the session. Steering is a separate set of typed daemon commands.  *(recommended)*
- **b** — A new read layer built for the babysitter. More work, same result.
- **c** — Give it a shell and let it read freely. Flexible, but weakens the safety guard.

### Q6 — How is the learnings memory stored and trusted?

_Asked of: Technical Questions_

- **a** — Recommended: a per-factory store table the babysitter appends to. The owner can read, edit and delete entries. Memory is advice only and never overrides a daemon guard.  *(recommended)*
- **b** — A plain file in the repository. Easy to read, but bypasses the store's history and redaction.
- **c** — No memory in v1. Simplest, repeats old mistakes.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
