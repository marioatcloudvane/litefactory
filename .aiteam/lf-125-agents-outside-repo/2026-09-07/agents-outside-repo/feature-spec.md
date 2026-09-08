# Feature Spec: agents-outside-repo
# Status: APPROVED
# Date: 2026-09-07

## Goal
- Operators install and explicitly update versioned teams from trusted web sources independently of litefactory releases; managed runs use installed, isolated mode bundles to reduce irrelevant instruction exposure.

## Evidence and Decisions
| Input | Consequence |
|---|---|
| Owner run 0013 | Remote distribution is primary; corporate URLs, versions, integrity, offline operation and independent publication are required. |
| Current team/types.go, teamsource/manage.go and snapshot.go | Preserve schema-2 mode rosters, central management, dependency capture, guarded publication, overrides, five-state drift and repository association; extend existing owners. |
| Current team/embedded.go | Remove embedded catalog and installation default; relocate content into independent distribution sources. |
| Main correction 6eb29bf; not yet ancestor of task HEAD | Integrate the known fix and preserve the three orchestrators as first entries of their mode; do not re-investigate it or remove orchestration. |
| Owner's Codex 0.153.4 reproduction | Nested EPERM came from the outer sandbox, not unsupported transport; remove the old hard capability blocker. |
| Previously approved scope | Retain CLI delivery, classifier isolation, conversation identity, focused Implement/read-only Validate and measured token evidence. |
| APP_CONTEXT lookup | Reuse designated packages; current code supersedes its stale repository-scoped and three-tier source descriptions. |
| Default-source assumption | No verified first-party public endpoint is available; use an operator-configured named HTTPS default. Missing default fails installation with setup guidance; installed runs continue offline. |
| Protocol assumption | HTTPS discovery index plus immutable per-release manifest and relative files; no archive extraction or registry service. Local packages remain explicit development/offline imports only. |

## User Outcomes
| Area | Story |
|---|---|
| Distribution | Install from public or corporate sources centrally without changing the binary or a repository. |
| Maintenance | Inspect versions and drift, then explicitly update with integrity checks and conflict protection. |
| Publisher | Publish immutable team releases and advance discovery metadata independently of application builds. |
| Runtime | Keep installed teams usable offline and deliver only the selected mode to each CLI. |
| Evidence | Trace each run to source/version/digest and measure instruction/token changes. |

## Data Model
| Entity | Fields | Constraints |
|---|---|---|
| Source configuration | alias, HTTPS index URL, optional credential reference; default alias | Operator-owned; existing secret resolution; repository text cannot introduce sources. |
| Discovery index | distribution schema version, teams, releases, stable version, manifest path/hash | Strict decoding; unique team/version; stable names a listed non-prerelease; no executable hooks. |
| Release manifest | distribution schema version, team ID, semantic content version, catalog schema version, files | Immutable; one team's complete closure; each file has relative path, byte length and SHA-256; digest covers exact manifest bytes. |
| Team payload | Existing schema-2 teams/shared/desc and five ordered rosters | Retain references, inline definitions, tiers, skill/resource closure and existing validation. Content version differs from schema version. |
| Installation provenance | source alias/URL, team, selection policy, installed version, manifest digest, baseline hashes | Extend existing metadata; changing default source never retargets installed teams. |
| Selection policy | stable or exact semantic version; optional expected manifest SHA-256 | Default stable installs a concrete release; exact version remains pinned until explicit change. |
| Upstream observation | checked-at, latest verified version/digest, freshness, last error | Freshness: never-checked/fresh/stale/unavailable; retain last verified observation on failure. |
| Drift | baseline, installed and verified candidate hashes | Retain in-sync/locally-modified/upstream-updated/conflict/missing; freshness is separate. |
| Run bundle | source/version/manifest provenance, override hashes, team/mode, selected IDs, digest, input bytes | Immutable per run; identity survives cleanup and controls conversation reuse. |

### Relationships and Storage
- Source alias → discovery index → immutable release manifest → verified file closure → central installation → selected-mode bundle.
- Keep managed sources under $LF_HOME/teams and provenance under its .litefactory directory; repositories store only selected team IDs.
- Place verified downloads/discovery observations in a dedicated config.Paths-owned cache beneath $LF_HOME; no project cache.
- Keep immutable bundles under $LF_HOME/bundles/<forge>/<owner>/<repo>/<issue>/<run-ordinal>/.
- Versions are scoped by source and team; replacing a team's source requires explicit operator action.
- Unequal shared identifiers across installed teams remain actionable conflicts; never overwrite another team's source silently.

## Interface Changes
| Action | Behaviour |
|---|---|
| teams.sources / teams.default_source configuration | Named HTTPS endpoints, same protocol for public/corporate sources; optional existing-secret reference for an authorization header. |
| team add <team> [--source <alias-or-https-url>] [--version <semver>] [--sha256 <manifest-digest>] | Fetch stable or exact release, verify closure, atomically install with provenance. |
| team add <team> --source <local-package-root> | Explicit development/offline import using the same release format; never an automatic default. |
| team status [<team>] [--refresh] | Cached/local only by default; explicit refresh checks source without installing; reports versions, policy, freshness, errors and byte drift. |
| team update [<team>] [--version <semver>] [--sha256 <digest>] [--force] | Explicit refresh and verified publication; preserve pin unless version explicitly changes; force addresses local drift only. |
| team list/show/validate/manage and repository --team | Preserve central semantics; add provenance/version reporting where relevant; no implicit network. |
| Distribution build/publish workflow | Validate content, emit deterministic index/manifests/files, publish immutable releases and advance stable independently of application builds. |

## Version and Drift Rules
- Order semantic versions numerically; default stable excludes prereleases, exact selection may name a prerelease.
- Persist exact installed version/digest even for stable policy; no change occurs without explicit update.
- Changed digest for a previously observed source/team/version is an integrity failure; publishers must issue a new version.
- Ordinary update refuses a stable rollback; an explicit exact-version request may deliberately downgrade after verification.
- Report newer version even when bytes match; upstream-updated remains a byte comparison, not a version synonym.
- Exact-pin status shows latest stable separately; update cannot silently unpin.
- An expected manifest-digest pin persists separately from version policy. Moving releases requires explicit replacement or clearing of that digest with an exact version; force never clears it. Retain observed source/team/version digest history independently of evictable cache blobs.
- Five-state comparison uses baseline/local/verified-candidate bytes. Without current upstream evidence show local baseline drift plus unknown/stale upstream; never claim verified upstream synchronization.
- Missing local files remain missing during outage; unreachable/removed releases never imply deletion of installed files.
- Preflight obsolete managed files, shared dependencies and overrides before publication; preserve unrelated teams and explicit override provenance.

## Trust and Download Rules
- Source selection belongs to the operator; repository text, agent output and remote instructions cannot alter trust, credentials or permission ceilings.
- Require HTTPS with normal host/certificate validation; reject plain HTTP, userinfo and fragments.
- Follow at most three redirects within the exact HTTPS origin; reject downgrade/cross-origin redirects.
- Manifest/file references are safe relative paths contained under the distribution root, never arbitrary URLs.
- Resolve credential references through existing secret facilities; send only to configured origin; never expose values in URLs, provenance, output, logs or cache.
- Verify discovery-advertised manifest SHA-256 and all file sizes/hashes before publication; optional operator-supplied digest must also match.
- Same-source hashes provide integrity against recorded pins, not independent publisher authentication. Initial trust is explicit source choice plus HTTPS; an independently obtained digest strengthens first-install pinning.
- Reject duplicate/absolute/traversal paths, encoded separators, symlinks, nonregular entries, undeclared dependencies and containment escapes using existing closure validation.
- Bound index/manifest size, each file, aggregate release size, file count and operation duration; architecture selects concrete limits consistent with existing source/snapshot bounds.
- Treat downloaded content as data; no installer scripts, downloaded CLI settings, permission overrides or automatic plugin execution.
- Any hash/version/schema/dependency failure leaves installed sources and trusted baselines unchanged; invalid downloads never enter verified cache.
- Release signatures and key rotation are explicitly deferred; initial distribution must not be described as signed.

## Offline, Cache and Publication Rules
- Work launch, classification, resume, daemon startup and normal inspection never contact sources; installed valid catalogs remain usable indefinitely during outage.
- Only add/update/status refresh perform remote fetch; no background refresh, TTL-triggered network or unattended prompt updates.
- Verified immutable blobs remain reusable by digest with verification on consumption; discovery becomes stale after 24 hours for display only.
- Failed explicit refresh reports nonzero error and preserves last verified observation with unavailable/stale labeling; running installations remain usable.
- Failed add/update never silently substitutes an older cached candidate; explicit local import supplies offline installation.
- Download/validate in bounded staging before publication lock; under lock recheck local baselines to avoid concurrent-edit loss.
- Interrupted publication leaves an old or new complete installation; cleanup touches only owned staging, never active/sibling bundles or central sources.
- Move canonical content out of internal/team/embedded; remove production go:embed and embedded installation/runtime fallback. Sample test fixtures cannot become defaults.
- Independent publisher emits complete closure/hashes, rejects overwriting versions, and advances discovery only after immutable files exist.
- Publisher deliverables include hosting layout, corporate self-host instructions, independent release workflow, reproducibility tests and an HTTP-served end-to-end example.
- Actual production hosting requires owner-controlled credentials; distinguish locally validated publisher artifacts from a verified public release URL.
- App upgrades do not reinstall/refresh teams; content publication does not build litefactory. Unsupported future schemas fail clearly without conversion.

## Runtime and Workflow Requirements Retained
- Classifier sees zero team agents/skills; after mode selection both CLIs receive only the immutable selected-mode snapshot in the supplied worktree.
- Preserve project instructions/session conventions; remove duplicated full specialist bodies from root bootstrap instructions.
- Research: research-orchestrator, codebase-tour, web-researcher.
- Plan: plan-orchestrator, requirements-engineer, go-architect, proxy-product-owner.
- Implement: implement-orchestrator, go-implementation-engineer, go-unit-test-engineer, go-integration-test-engineer.
- Validate: go-code-reviewer; Spike: empty.
- Orchestrators sequence specialists; daemon mode selection replaces only redundant top-level routing, not orchestration.
- Implement authors production code/tests and executes verification; Validate reviews acceptance/code/evidence/context drift read-only with inline findings.
- Retain focused language skills, existing tiers/permission ceilings/runner/session ownership; no updater restoration or automatic remediation loop.
- Changed team/mode/effective digest requires fresh conversation segment; cleanup must preserve resume safety and concurrent run isolation.
- Owner's successful unsandboxed Codex 0.153.4 probe corrects prior diagnosis. Complete delegation/tier/isolation/authentication/lifecycle evidence in a suitable environment; outer-sandbox EPERM is missing environment evidence, not a blocker to independent adapter implementation.

## Out of Scope
- Legacy schema conversion/compatibility, embedded bootstrap/fallback, automatic upstream updates and repository-owned registries.
- New language stacks, registry hosting service, archive installers, arbitrary plugins and second model/tier configuration.
- Signature/key-rotation infrastructure, general story synchronization, live host edits or production publishing during this Plan run.

## Acceptance Criteria
- [ ] Empty isolated installation obtains complete Go team from configured HTTPS default or corporate source; missing default gives setup guidance.
- [ ] Content-only release/update works with same binary; application upgrade leaves installed team version/content untouched.
- [ ] Release integrity covers all agents, orchestrators, skills and resources; run provenance identifies source/version/effective digest.
- [ ] Exact pins, stable updates, changed same-version digest, rollback, release removal and semantic ordering have deterministic tests.
- [ ] All five drift states work with fresh/stale/unavailable upstream; unknown upstream never appears verified in-sync.
- [ ] Tampering, TLS/redirect violations, credential leakage, unsafe/oversized downloads and invalid closure refuse publication without installed changes.
- [ ] Offline work/resume/cached status make zero remote requests; failed refresh preserves installation and truthfully reports failure.
- [ ] Concurrent updates/interruption/shared conflicts preserve atomic publication, existing overrides and unrelated teams.
- [ ] Publisher workflow and corporate hosting example exercise same consumer protocol; public endpoint availability is reported honestly.
- [ ] Binary contains no embedded team prompts/default tree; Go content preserves all three first-position orchestrators.
- [ ] Claude/Codex delivery and focused workflows complete deferred scope; runner evidence distinguishes environment limitations from unsupported transport.
- [ ] Benchmark records vendor/version, task outcome, bytes, available input/cached/total tokens and latency for full versus selected catalogs without unmeasured claims.
