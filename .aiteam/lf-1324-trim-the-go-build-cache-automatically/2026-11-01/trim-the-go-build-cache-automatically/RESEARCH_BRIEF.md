# Research brief

## Request

Automatically trim the factory's Go build cache below a configurable margin above the configured run-stop threshold, only when cache users are idle; log reclaimed space and expose a continuing disk block to the owner.

## Decisions

- **S-01** — mode=feature, size=medium — adds pressure-triggered cache maintenance across daemon admission, configuration and owner visibility. (agent's call)
- **O-01** — Trim the Go build cache automatically near the configured stop threshold, only when no run using it is active. (owner said: issue #1324 Scope)
- **O-02** — Log the trimmed cache and recovered space; make a remaining below-threshold condition visible to the owner. (owner said: issue #1324 Scope)
- **O-03** — Configure the margin and cache path with sensible defaults; allow future cache types while shipping Go first. (owner said: issue #1324 Scope)
- **N-01** — Exclude DB backups, logs, archives and anything outside factory-owned caches. (owner said: issue #1324 Out of scope)
- **K-01** — Preserve active cache users and the existing configurable disk stop guard. (owner said: issue #1324 Goal and Scope)
- **O-04** — Trigger pressure cleanup strictly below the configured stop threshold plus a configurable nonnegative margin, defaulting to 8 GiB, with a separate cleanup disable switch and no pressure cleanup when the disk guard is disabled. (agent's call)
- **O-05** — Clear disposable Go build-cache contents completely for eligible targets rather than introducing an age or size eviction policy. (agent's call)
- **O-06** — Only clear targets on the pressured factory-home filesystem, deduplicate identical targets, and report targets on other volumes as unable to relieve this guard. (agent's call)
- **O-07** — Hold exclusion from the idle check through cleanup completion, including reviewers, checks and surviving child processes, and skip cleanup whenever ownership or quiescence cannot be established. (agent's call)
- **O-08** — Remeasure free bytes after every cleanup attempt and apply the existing stop threshold to admission, even if the higher cleanup trigger remains unmet. (agent's call)
- **O-09** — Log each target, attempt result, elapsed time and before/after free bytes as observed filesystem change rather than claiming every changed byte was reclaimed by cleanup. (agent's call)
- **O-10** — Expose a remaining disk hold together with cleanup success, failure or skip reason and an operator recovery action through existing status and health surfaces. (agent's call)
- **O-11** — Bound a cleanup pass to five minutes, honor shutdown cancellation, and impose a thirty-minute retry cooldown after any attempted pass so empty caches and persistent pressure cannot create a cleanup loop. (agent's call)
- **O-12** — Treat missing or empty caches as successful no-ops, retain admission protection until cleanup actually stops, and report partial deletion or measurement errors without claiming successful recovery. (agent's call)
- **O-13** — Validate cleanup targets as dedicated cache directories, reject roots, home directories, worktrees and symlink escapes, and never expand deletion beyond the validated target. (agent's call)
- **O-14** — Support omitted settings on existing installations, document GiB units and cold rebuild costs, and keep target configuration extensible to future cache types while accepting only Go in this version. (agent's call)
- **O-15** — Demonstrate threshold boundaries, idle exclusion and competing launches, retry behavior, bad targets, filesystem mismatch, failures, cancellation and the remaining-block status without deleting the operator's real cache. (agent's call)
- **N-02** — Exclude Go module downloads, reviewer temporary directories, unrelated language caches, and automatic retirement of the existing host cache if dedicated caching is selected. (agent's call)
- **N-03** — Exclude a generic cleanup plugin system, a new manual cleanup command, and a guarantee against disk growth caused by unrelated applications. (agent's call)
- **K-02** — Preserve existing run-stop settings, daemon and check disk-probe error behavior, queued work, active work, and the existing -trimpath prevention. (agent's call)
- **K-03** — Preserve sandbox restrictions except for the specific factory Go cache grants explicitly included in the owner's selected ownership option. (agent's call)
- **K-04** — Preserve backup, log and archive maintenance behavior and existing reviewer isolation. (agent's call)


## Assumptions

- **AS-01** — Use the actual configured stop threshold, not a hardcoded 16 GB; propose an 8 GiB margin, so the current 2 GiB default triggers below 10 GiB, and disable pressure cleanup when the stop guard is disabled. Live operator configuration and binary version remain uninspected. (agent's call)
- **AS-02** — Only configured, validated factory-owned Go build caches may be cleared; Q-01 determines whether ownership comes from dedicated paths or an operator exclusivity declaration, and neither choice authorizes module caches or arbitrary home-directory cleanup. (agent's call)
- **AS-03** — Require exclusion of active users and new launches throughout cleanup; Q-01 settles cache ownership and Q-02 settles admission draining, while uncertainty about surviving children or ownership must skip cleanup rather than presume idle. (agent's call)
- **AS-04** — The two manual clears and 8.2 GB growth remain owner evidence rather than measured host observations; scope includes deletion failures, empty/missing targets, ineffective cleanup, volume mismatch, cancellation and repeated pressure without requiring reproduction of that day's growth. (agent's call)
- **AS-05** — Q-03 settles whether current status visibility suffices or #1308 becomes a delivery dependency; no proactive incident service is assumed to exist. (agent's call)
- **AS-06** — Cooldown is runtime state and may reset on daemon restart; existing configuration and health storage are sufficient for the proposed status-only scope, with no separate cleanup history database or public API required. (agent's call)
- **AS-07** — When a cleanup pass is in cooldown, do not keep an above-stop admission hold solely to wait for another pass; a below-stop hold continues under the existing disk guard. (agent's call)


## Open questions

### Q-02 — May cleanup pause new runs while active runs finish?

_Asked of: Product Owner Questions_

- **drain** — Yes (recommended): below the cleanup trigger, hold new top-level runs, let active runs and their required child checks finish, then clear the idle caches and remeasure; do not cancel work. This ensures a busy factory eventually gets a cleanup window, at the cost of queue delay.  *(recommended)*
- **opportunistic** — Only clean during naturally idle periods: continue admitting work until the existing stop threshold is reached. This avoids earlier queue delays but cannot promise cleanup before the stop line under continuous load.

### Q-03 — Must this delivery proactively notify the owner when disk space still blocks runs?

_Asked of: Product Owner Questions_

- **status** — Status and logs now (recommended): expose the remaining disk block, cleanup result and required operator action through existing status/health surfaces; leave proactive Discord incidents and all-clear messages to #1308. This ships cleanup without depending on a separate alert service, but the owner must inspect status.  *(recommended)*
- **alerts** — Proactive notification is required before this feature is complete: coordinate delivery with #1308's owner alert capability and include one unresolved-disk incident plus recovery notification. This adds an integration dependency and may delay delivery; alert routing and readiness must then be established.

### Q-01 — Which cache ownership boundary should automatic cleanup use?

_Asked of: Technical Questions_

- **dedicated** — Dedicated factory cache (recommended): direct factory Go builds and checks to a configurable cache unique to each factory home, while retaining the separate reviewer cache as another managed target; include narrowly scoped sandbox grants for those caches, reject shared ownership, and leave the existing host cache untouched for manual retirement. This gives a clear safety boundary but requires cold rebuilds and does not immediately recover the old 8 GB.  *(recommended)*
- **existing** — Existing cache: clean the configured host Go build cache plus the reviewer cache without moving builds; the operator must declare those paths exclusive to this factory and keep unrelated Go commands and other factory homes off them. This recovers today's accumulated cache but depends on an operational exclusivity guarantee; sharing remains unsupported.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
