# Research brief

## Request

Establish how Research, Plan, Implement and Validate work today; assess a simple graph-based Validate flow with factory-run language-neutral full-suite tests, failure repair, acceptance and drift review, scheduling/capacity options, possible test servers, and existing merge/closure authority. Inspect available past Validate runs.

## Decisions

- **d1** — Owner requests Research and advisory ideas, with simplicity and language neutrality. No stories, implementation, remote mutations or mode switch are authorized by this pass.
- **d2** — The supplied issue preserves read-only reviewer authority, owner-selected modes, engineering/acceptance separation, and distinct adoption/retirement. The suggested repair and merge steps are proposals to shape, not authority already granted.


## Assumptions

- **a1** — Treat 'drift check' as code/architecture/approved-plan compliance, because those are the stated issue review obligations. Broader team-file or deployment drift is not established.
- **a2** — Treat scheduling, all-slot exclusivity and dedicated servers as candidate choices. No owner decision is needed to establish present behavior; shaping owns these choices.
- **a3** — Advisory starting point: one local factory full-suite activity, one read-only reviewer and explicit repair handback. A one-at-a-time expensive check gate may preserve ordinary work better than consuming all slots; resource usage and fairness were not measured.
- **a4** — Bounded evidence limitations: source inspected at 680c4f2ba575a18b2f704a90f8b777079470724f on the supplied macOS worktree; no tests executed, daemon state changed or database queried. Historical logs are older snapshots, not proof of current admission behavior. Remote runner support beyond the inspected local executor, exact causes of all prior failures, and live adoption remain open.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
