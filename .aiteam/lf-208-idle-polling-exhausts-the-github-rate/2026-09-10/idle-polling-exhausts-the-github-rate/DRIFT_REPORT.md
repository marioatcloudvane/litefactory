# Engineering scope notes — run 0008

Only US-002 resumed: concurrent paginated background reservations are serialized so unused capacity is returned before the next reservation. Added deterministic cancellation/refund and active-bypass regressions and bounded the formerly unbounded backoff fixture rendezvous.

US-001 was not edited. Background discovery retains the approved 80% floor, 20% spending cap, 120-capacity traffic bucket and 100-page reservations. No new settings, persistence, workers, dependencies, polling-interval changes or timestamp filtering.

No added or dropped feature scope reported by engineers. This records engineering scope only; acceptance, code/architecture and approved-plan review belong to explicit Validate.

Aggregate gate also required restoring the standard structured-protocol handoff in the owned `.claude/agents/research-orchestrator.md`. The existing parity test detected that omission; its focused race rerun passed. This is an incidental instruction repair, not polling feature scope. No `.codex` change was needed.
