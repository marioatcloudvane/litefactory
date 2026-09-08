# US-2 — Install and maintain teams from trusted web sources

> As an operator, I want to fetch and explicitly update centrally installed teams so that prompt improvements reach projects without an application upgrade.

Extend central management with verified network acquisition and truthful version, drift and freshness reporting.

## Why This Matters

Installing from the web introduces outages, altered instructions and conflicting local changes. Operators need updates they can inspect and control while existing work continues offline. Corporate sources must receive the same integrity protections as public sources.

## Architecture Notes

- Preserves issue #128. T-2.1–T-2.5 supplied central management; T-2.6 onward extend that foundation. No compatibility/migration work returns.
- Acquisition/cache belongs in `teamremote`, wired from `cli` with source-specific credential resolution; `teamsource` retains Install/Update, composition, overrides, drift and publication ownership.
- Fetch/validate outside the publication lock, then recheck recorded local baselines under lock. Extend guarded publication with crash recovery or generation publication; per-file rename alone is insufficient for whole-release atomicity.
- Platform: caller context, bounded sequential fetching, sanitized typed errors, no background fetch/retry goroutine. Inject transport/authentication at the consumer boundary.
- Risk: verified cache history must not become permission to silently install an older candidate after failed acquisition. Keep permanent observed version/digest history separate from evictable blobs.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|------------|-----|
| T-2.6 | Implement bounded HTTPS index/manifest/file acquisition, origin-safe authentication and verified cache under the shared protocol; reject untrusted bytes before publication. Ref: Trust and Download Rules, Offline, Cache and Publication Rules. Agent: go-implementation-engineer. | logic | T-1.6, T-1.7 | L |
| T-2.7 | Extend central install/update with verified releases, baseline recheck, shared/override/deletion preflight and interruption-safe whole-team publication; preserve unrelated installations on errors. Ref: Version and Drift Rules, Offline, Cache and Publication Rules. Agent: go-implementation-engineer. | logic | T-2.6 | L |
| T-2.8 | Add cached status/explicit refresh, installed/latest versions, pin replacement/clear and explicit source switching; preserve existing central commands and repository selection. Ref: Interface Changes, Version and Drift Rules. Agent: go-implementation-engineer. | api | T-2.7 | L |
| T-2.9 | Test HTTPS trust, redirects/auth, corruption, unsafe/oversized responses, cancellation and no-network inspection using TLS fixtures and temporary roots. Ref: Trust and Download Rules, Offline, Cache and Publication Rules. Agent: go-unit-test-engineer. | test | T-2.6 | L |
| T-2.10 | Test five-state drift crossed with freshness/policies, immutable pin history, removal/shared/override conflicts, concurrent writes and crash recovery; assert failed updates leave installed state unchanged. Ref: Version and Drift Rules, Acceptance Criteria. Agent: go-unit-test-engineer. | test | T-2.7, T-2.8 | L |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-2.8 | Empty-home add fetches stable or exact release from a configured default, named corporate/public source or explicit HTTPS URL; explicit local package import uses the release contract without becoming a default. Unsupported schemes fail. |
| AC-2.9 | Normal certificate/hostname verification is mandatory; reject HTTP, userinfo, fragments, credential-bearing queries, cross-origin/downgrade redirects and distribution-root escapes. Accept at most three exact-origin redirects. Direct URLs receive no ambient credential; configured credentials never appear in logs, errors, cache, provenance or child environment. |
| AC-2.10 | Reject duplicate/absolute/traversal/encoded-separator paths, symlinks, nonregular local files and undeclared closure. Enforce 1 MiB discovery, 1 MiB manifest, 1 MiB per file, 16 MiB aggregate, 1,024 files and 120-second operation deadline or earlier cancellation, counting decoded bytes. |
| AC-2.11 | Verify exact manifest bytes against discovery and optional operator digest, then every file size/hash and complete closure. Hash/schema/version/closure failure leaves installed sources and trusted baselines unchanged; invalid bytes never enter verified cache. Changed digest for a previously verified source/team/version fails even with force and after blob eviction. |
| AC-2.12 | Stable update records concrete version/digest and refuses rollback. Exact version stays pinned; explicit exact selection may downgrade. A digest pin cannot silently disappear: replacing or clearing it requires explicit policy action naming the selected version. Force overwrites local drift only and cannot bypass integrity or pins. |
| AC-2.13 | Status displays installed version, latest stable, selected candidate, policy, last attempt/success/error and separate freshness. Identical bytes at a newer version report version availability without fabricated byte drift. Discovery-only observation does not claim candidate-byte comparison. |
| AC-2.14 | With verified candidate bytes, state ordering remains missing local → missing; local equals candidate → in-sync; local equals baseline → upstream-updated; candidate equals baseline → locally-modified; otherwise conflict. Coincident edits remain in-sync. Without candidate bytes, expose baseline-local drift and unknown upstream. |
| AC-2.15 | Default inspection makes zero requests. A successful observation is fresh for 24 hours using supplied time; expiry only marks stale. Failed refresh returns nonzero, preserves last verified data and marks unavailable/stale. Missing files stay missing during outage; removed releases never uninstall content. Drift yields nonzero status; stale cached in-sync inspection may succeed while showing staleness. |
| AC-2.16 | Add/update/refresh are the only fetch actions; failed add/update never substitutes an older cached candidate. Cached immutable bytes are reverified on use. Installed catalogs remain usable independent of cache/source availability. |
| AC-2.17 | Channel-coordinated update/read/edit and injected interruption tests expose only an old or new complete team after recovery, detect changed baselines and preserve shared dependencies, explicit override provenance and unrelated teams. Obsolete managed files are removed only after ownership/local-drift preflight. |
