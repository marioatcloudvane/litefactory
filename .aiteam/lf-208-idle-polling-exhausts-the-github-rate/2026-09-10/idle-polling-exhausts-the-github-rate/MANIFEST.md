# Manifest
# Branch: lf-208-idle-polling-exhausts-the-github-rate
# Session: 2026-09-10

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [ ] **Plan** — draft reused and typed contribution durably acknowledged as valid; publication, Story Index replacement and save pending *(run 0006)*
- [ ] **Implement** — code + tests *(not started)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

**Validate** returns read-only structured findings through runner output.

## History

- 2026-09-10 — session created for research run 0001, issue #208.
- 2026-09-10 — Research completed: repository tour and official GitHub/SDK/HTTP documentation synthesized in RESEARCH_BRIEF.md; live request volume and endpoint-specific validator behavior remain explicit open questions. No implementation or runtime changes; no tests executed.
- 2026-09-10 — Research run 0003 completed: answered all eight owner responses; measured a retained-log lower bound of 40 repository-list requests in 20 minutes, distinguished it from unmeasured total traffic, rechecked conditional/timestamp/HTTP documentation, and proposed cache, identity, fairness and quota defaults with explicit tradeoffs. Poll interval removed from unresolved feature decisions. Prior technical evidence retained as R0. No production/runtime changes or tests; commits and pushes remain factory-owned.
- 2026-09-10 — Plan run 0005 prepared the specification, architecture review, shared brief and two-story typed contribution. Preflight passed; submission failed with `submission: cannot persist correlation` and an empty request ID. No durable acknowledgment was established in that run.
- 2026-09-10 — Plan run 0006 retried the unchanged contribution using the newly supplied helper binding. Preflight passed (`valid=true`, `complete=true`, empty diagnostics and unchecked paths). Submission and receipt lookup confirmed durable request `3367ff6c1d6422ecc335f3c1bb413255`: `InputState=valid`, `EngineeringState=not_applicable`, `AcceptanceState=not_applicable`, `EffectState=not_applicable`, `PlanState=pending`. This establishes valid retained input only; publication, Story Index replacement, readiness and save remain factory-owned and unconfirmed until terminal success. The prior correlation-write blocker did not recur.

## Notes

Run 0005 Plan produced two stories: bounded memory-only conditional caching; fair background discovery admission and request summaries. No owner questions. Timestamp narrowing, persistent cache, new settings and polling-interval changes are excluded. Proposed defaults reserve 80% primary quota and allow a separate 120 requests/minute token bucket with capacity 120. Architecture source revision: `c16fb64a76646f5744b659835238824fe9cbc51d`.

The unchanged transient contribution `/private/tmp/lf-208-plan-run5.json` was submitted in run 0006; it is not a story source or committed artifact. Its durable request correlation is recorded above. No implementation or tests were performed; live endpoint support and production savings remain unverified. Base synchronization was deferred by the factory (one commit behind and four ahead); no merge was attempted and existing draft provenance is unchanged. Leave publication, Story Index link replacement, readiness, commits and pushes to litefactory.
