# Research brief

## Request

Shape weighted workflow-step admission into a buildable scope while retaining max_concurrent_runs and default cost 1.

## Decisions

- **d1** — Owner retains max_concurrent_runs, requires omitted cost 1, and requires arbitrary workflow-step costs in DOT rather than hardcoded Go policy.
- **d2** — This shaping pass proposes a complete scope for owner review and does not implement or claim approval.
- **d3** — Use lf_slot_budget with effective node attribute precedence over an optional graph default and fallback 1.
- **d4** — Charge executing steps through cleanup and require atomic full-cost admission at every entry and step transition.
- **d5** — Defer unavailable work without blocking ticks, consuming attempts or repeating completed activity effects.
- **d6** — Reject invalid positive-integer attributes and refuse oversized steps with actionable diagnostics while retaining pending work.
- **d7** — Initially set only the Go Implement story-worker step to cost 3 and preserve existing configured capacity and limits.
- **d8** — Track live reservation cost separately from worker identity and release exactly once on all exits.
- **d9** — Nested independently executing work shares capacity, defaults to 1 without its own step, and must yield unfinished work rather than wait while exhausting capacity.
- **d10** — Existing workflow authority and pinned snapshots govern cost changes and agents cannot alter admission cost via results.
- **d11** — Expose weighted admission diagnostics and document syntax and semantics without adding dashboards or changing scheduling priorities.
- **d12** — Acceptance covers the 8-slot example, step transitions, all dispatch routes, cleanup, cancellation, restart and nested work.
- **d13** — Automatic pressure response, cost measurement, hot reload, other shipped cost policies and changes inside admitted activities are excluded.


## Assumptions

- **a1** — Dynamic means declarative assignment via the existing release/capture process; hot reload and changing retained workflow snapshots are excluded.
- **a2** — The owner’s step requirement selects per-step charging; node lf_slot_budget overrides graph lf_slot_budget, then fallback 1.
- **a3** — Existing dispatch priorities remain; no starvation-freedom guarantee is added, so cheap work can postpone costly steps.
- **a4** — Cost 3 is the owner’s initial policy for Go Implement story workers, not a measured memory safety guarantee.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
