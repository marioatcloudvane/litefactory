# Research brief

## Request

Automatically trim the factory's Go build cache below a configurable margin above the configured run-stop threshold, only when cache users are idle; log reclaimed space and expose a continuing disk block to the owner.

## Decisions

- **S-01** — mode=feature, size=medium — covers dedicated Go cache routing and permissions, bounded pressure cleanup, configuration and existing status surfaces without an alert integration. (agent's call)
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
- **O-11** — Limit deletion to five minutes after the idle gate, honor shutdown cancellation, and wait thirty minutes after completion, failure, drain timeout or any safety skip before starting another cleanup cycle. (agent's call)
- **O-12** — Treat missing or empty caches as successful no-ops, retain admission protection until cleanup actually stops, and report partial deletion or measurement errors without claiming successful recovery. (agent's call)
- **O-13** — Validate cleanup targets as dedicated cache directories, reject roots, home directories, worktrees and symlink escapes, and never expand deletion beyond the validated target. (agent's call)
- **O-14** — Support omitted settings on existing installations, document GiB units and cold rebuild costs, and keep target configuration extensible to future cache types while accepting only Go in this version. (agent's call)
- **O-15** — Demonstrate threshold boundaries, idle exclusion and competing launches, retry behavior, bad targets, filesystem mismatch, failures, cancellation and the remaining-block status without deleting the operator's real cache. (agent's call)
- **N-02** — Exclude Go module downloads, reviewer temporary directories, unrelated language caches and automatic retirement of the existing host cache. (owner said: Q-01 dedicated cache)
- **N-03** — Exclude a generic cleanup plugin system, a new manual cleanup command, and a guarantee against disk growth caused by unrelated applications. (agent's call)
- **K-02** — Preserve existing run-stop settings, daemon and check disk-probe error behavior, queued work, active work, and the existing -trimpath prevention. (agent's call)
- **K-03** — Preserve sandbox restrictions except for narrowly scoped write grants to the dedicated factory Go build caches. (owner said: Q-01 dedicated cache)
- **K-04** — Preserve backup, log and archive maintenance behavior and existing reviewer isolation. (agent's call)
- **A-01** — Q-01 cache ownership, Q-02 draining, Q-03 visibility and builder-check correction — owner: "Following the judge: Q-01 dedicated cache, Q-02 drain, Q-03 status only (no alerts in this release). Add the judge's finding to the scope: the drain wait itself is bounded; on timeout, or when cleanup is skipped because quiescence cannot be proven, the extra admission hold is released and the next try uses a backoff. Keep the existing below-stop guard and add a test with a cache user that lingers." (owner said: comment 6564453344)
- **O-16** — Direct factory Go builds and isolated checks to a configurable dedicated cache unique to each factory home while retaining the separate reviewer Go cache as another managed target. (owner said: Q-01 dedicated cache)
- **O-17** — When pressure cleanup is due, hold new top-level admissions while letting active work and required child checks finish without cancellation, subject to their existing disk guards. (owner said: Q-02 drain)
- **O-18** — Bound the drain wait and release the extra admission hold on drain timeout or when safe idleness cannot be proven, retaining the existing below-stop guard and backing off before retry. (owner said: comment 6564453344)
- **O-19** — Set the drain deadline to five minutes from the first extra admission hold without extending it on later ticks or changing active-user counts. (agent's call)
- **O-20** — Add a lingering-cache-user test that proves no deletion occurs, above-stop admissions resume on timeout or safety skip, below-stop admissions remain held, and a new cycle waits for the retry backoff. (owner said: comment 6564453344)
- **O-21** — Show draining, cleanup, timeout or skip reason, last cleanup result and next retry time through existing status and health surfaces. (agent's call)
- **O-22** — Enable automatic cleanup by default for the managed factory caches, with a configurable main cache default in a factory-home-specific sibling directory outside worktrees and separate from the reviewer cache. (agent's call)
- **O-23** — Reject overlapping or shared cache ownership across factory homes and refuse cleanup if surviving cache users cannot be ruled out after restart. (agent's call)
- **O-24** — Abort a pending drain without deleting when fresh free-space measurement reaches the cleanup trigger, and release its extra hold subject to existing admission rules. (agent's call)
- **N-04** — Exclude proactive alerts and recovery notifications from this release, leaving them to #1308 without making it a delivery dependency. (owner said: Q-03 status only, no alerts in this release)
- **N-05** — Exclude supporting unrelated Go commands or other factory homes sharing the managed caches. (owner said: Q-01 dedicated cache)


## Assumptions

- **AS-01** — Use the actual configured stop threshold with an 8 GiB margin, yielding a trigger below 10 GiB at the current 2 GiB default, and disable pressure cleanup when the stop guard is disabled; live operator configuration and binary version remain uninspected. (agent's call)
- **AS-02** — Superseded by O-16, O-22, O-23 and N-05: Q-01 chose dedicated factory-owned caches, so an operator assertion of exclusive use of the shared host cache is not the selected approach. (agent's call)
- **AS-03** — Superseded by O-07 and O-17 through O-20: Q-02 chose bounded draining with release and backoff on timeout or unproven idleness, while cleanup still requires exclusion of active users and new launches. (agent's call)
- **AS-04** — The two manual clears and 8.2 GB growth remain owner evidence rather than measured host observations; scope includes deletion failures, empty/missing targets, ineffective cleanup, volume mismatch, cancellation and repeated pressure without requiring reproduction of that day's growth. (agent's call)
- **AS-05** — Superseded by N-04: Q-03 chose status and logs only, with no proactive alert integration in this release. (agent's call)
- **AS-06** — Cooldown may reset on daemon restart, but restart never proves the caches idle; configuration and existing health storage carry the required surfaces without a separate cleanup history database or public API. (agent's call)
- **AS-07** — Cooldown after cleanup, timeout or skip never holds above-stop admission; release of the extra cleanup hold never bypasses the existing below-stop guard or permits cache use while deletion is still running. (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
