# Architecture Review — agents-outside-repo, run 0013

Architect: Go Architect. Date: 2026-09-07. Verdict: APPROVE revised feature spec for story decomposition; this is a plan, not delivery verification.

Source baseline: `832c730882a13176f63e1e8eb1bd985150199fed`. The updated `feature-spec.md` supersedes embedded distribution and the old hard capability prerequisite. The known first-position orchestrator correction is `6eb29bf2a66ea9ee80a99130845cf2fcfb7bb10a` on local main; incorporate the correction without repeating its investigation.

## System overview

The existing central installation is the foundation. `team` already owns schema-2 ordered rosters, validation and guarded file writes; `teamsource` captures complete agent/skill resources and manages central provenance, overrides and five drift states. `config.Paths` owns external roots and explicit repository association already exists. US-1 and US-2 extend these implemented pieces rather than repeat centralization.

Add bounded remote acquisition, preferably `internal/teamremote`, containing distribution protocol, version comparison, HTTP acquisition and verified cache. It may import `team` for payload validation; neither `team` nor `render` imports it. `teamsource` consumes verified releases and remains owner of installed composition, snapshots and publication. Any acquisition interface is declared at its consumer (TEST-5). `cli` wires operator config, credential references and acquisition; daemon startup never fetches. No network belongs in config, render, store or runners.

The consumer protocol is also the publisher contract. Canonical sources move out of `internal/team/embedded` to a distribution input tree with an independently triggered content release workflow. A separate content repository is an optional hosting arrangement; independently versioned/released content in this repository already removes binary coupling. A concrete publisher deliverable merits US-6, rather than a documentation-only note in US-5.

```text
operator-approved HTTPS source
  -> discovery -> immutable manifest -> verified file closure
  -> teamsource publication -> $LF_HOME/teams + provenance
  -> selected team/mode snapshot -> pure renderer -> per-run bundle
  -> agent.Request -> existing Claude/Codex adapter in task worktree
```

Only explicit add/update/status refresh use acquisition. Runtime consumes installed content, independent of source/cache availability. Source/version/manifest provenance travels with snapshots and durable run metadata. Effective bundle identity additionally covers overrides, mode, dependencies, render/bootstrap revision and model mapping; content version alone is insufficient for conversation identity. `store` remains the only SQL owner; reuse repository association and existing conversation segments. `turn` retains CONC-4/7 reporting and state ordering.

### Concurrency and publication

Download and validate in owned bounded staging outside the central lock. Record the intended installed baseline; once locked, compare it again and refuse/replan after concurrent edits. Preserve shared-source conflicts and explicit overrides. Publication exposes an old or new complete selected-team installation to cooperative snapshot readers, metadata last. Per-file temp/rename alone does not prove crash atomicity for a multi-file release: add transaction staging/recovery or generation publication at the existing write owner and test interruption. Do not silently weaken this acceptance claim.

Release locks before rendering, subprocesses and network. No background fetch goroutine, retry loop or clock service (CONC-2/6). Context bounds acquisition/staging, and mutable data is copied across boundaries (CONC-1). Distinct runs have distinct immutable paths. Retain active/resumable bundles until safe lifecycle cleanup is demonstrated; credentials never enter retained bundle/cache content.

## Binding contracts

### Format and source selection

Use an HTTPS discovery index plus immutable manifests/files, not archives. Distribution schema `1` wraps existing catalog schema `2`. Discovery maps team/version to manifest path and exact-byte SHA-256; stable explicitly names one listed non-prerelease. The manifest repeats team/version/schema and lists all payload relative paths, sizes and SHA-256 hashes. Exactly one payload `index.json` describes the team and required shared definitions. Manifest references resolve relative to the discovery index directory; manifest file references resolve relative to the manifest directory. Payload references resolve inside the release root. All URLs stay beneath the configured index directory and exact HTTPS origin; consumer/publisher fixtures must agree on these bases.

`teams.sources` maps operator aliases to HTTPS URL and optional credential reference; `teams.default_source` selects an alias. No verified first-party endpoint is known, so ship an unset default: add without source fails with configuration guidance, while installed runs and daemon startup work. `--source` distinguishes alias, HTTPS URL and explicit local development package path; reject unsupported schemes. Repositories select installed team IDs only. Direct URLs inherit no ambient credential; authenticated sources use configured aliases. Installed provenance stores resolved URL, so editing alias/default never silently retargets teams. Source switching requires an explicit operator action naming the new source/team.

### Versions and pins

Use strict SemVer 2 syntax and numeric core/prerelease ordering, with optional build metadata. Reject leading `v`, ranges/wildcards and implicit prefix matching. Build metadata does not affect precedence; equal-precedence versions must not be selected through arbitrary ordering. Stable explicitly selects its target. Default installation tracks stable while recording exact installed version/digest; `--version` selects exact policy and may name a prerelease. A content version differs from distribution/payload schema versions.

An optional independently supplied digest is a concrete release pin: moving to another release requires explicit replacement or clearing of that digest pin. The CLI must expose that policy change unambiguously, preferably requiring an exact version when replacing/clearing a pin. A previously verified source/team/version whose digest changes is always an integrity failure, including under `--force`. Force authorizes local-edit overwrite only. Ordinary stable update refuses rollback; explicit exact selection may downgrade. Exact-pin status reports latest stable separately from its selected candidate. Newer version with identical bytes is a version difference, not byte drift. Removed release means unavailable, never uninstall. Unsupported schemas fail directly without compatibility conversion.

### Drift and freshness

Preserve `teamsource.driftState` order: missing local -> missing; local equals candidate -> in-sync; local equals baseline -> upstream-updated; candidate equals baseline -> locally-modified; otherwise conflict. Coincidentally matching local/upstream edits remain in-sync. Compare definitions/effective override resources as well as agent bodies. A removed file supplies candidate absence: deletion requires ownership/local-drift preflight and must preserve still-shared/override dependencies.

Freshness is separate: never-checked/fresh/stale/unavailable. Fresh means successfully checked within 24 hours using caller-supplied time; expiry only changes display, never triggers network. Record last attempt, last successful observation and last error. Cached byte comparison must identify its verified candidate and stale/unavailable status. Without verified candidate bytes, report local baseline drift and unknown upstream; never substitute baseline and claim verified synchronization. Missing local stays missing during outages. Status fails for drift or failed explicit refresh (STYLE-6); normal offline inspection may succeed when cached bytes are in sync while truthfully displaying stale freshness. Discovery-only refresh may report version availability, not fabricated file drift; claiming candidate byte drift requires verified candidate bytes.

### Trust, network and cache

- Normal TLS certificate/hostname verification is mandatory. Reject HTTP, URL userinfo/fragments and credential-bearing queries. Corporate DNS/trust roots use operator-managed platform trust, never a verification bypass.
- At most three redirects, exact scheme/host/effective port and distribution-root containment. Credentials never cross origins. Sanitize remote/redirect URL errors before logging.
- Reject absolute/traversal refs, empty/dot segments, backslashes, encoded separators/traversal, schemes/query/fragment refs, duplicate normalized paths, nonregular/symlink local files and undeclared dependency downloads. Reuse rooted closure validation.
- Limits: 1 MiB discovery; 1 MiB manifest; 1 MiB each payload file (`MaxSourceBytes`); 16 MiB total payload (`MaxSnapshotBytes`); 1,024 files; 120-second operation deadline, with earlier caller cancellation. Count decoded body bytes despite compression/Content-Length. Bounded sequential fetching is sufficient initially.
- Verify manifest exact-byte hash against discovery and optional operator digest, then all sizes/hashes and complete closure before publication. Invalid bytes never become verified cache or replace trusted history.
- A `config.Paths`-owned root such as `$LF_HOME/cache/teams/` stores immutable verified blobs by digest and observations by resolved source/team/policy. Reverify on consumption. Keep historical version/digest pins independently of evictable cache blobs. No automatic fetch/expiry deletion. Failed explicit fetch cannot silently install an older cached release; explicit local import supports offline installation.
- Existing secret facilities resolve source-specific credentials only at the HTTP boundary: no bot-token fallback, literal config secrets, child environment forwarding or secrets in logs/cache/provenance. Review tests pinning Token.Reveal call sites deliberately when adding this authorized boundary (TEST-10), not by weakening protections generally.
- Source choice plus HTTPS establishes initial publisher trust. Same-source hashes cannot authenticate a compromised publisher independently; out-of-band digest pins strengthen first-install trust. Signatures/key rotation are explicitly deferred. Downloaded files cannot alter local permission ceilings, execute hooks, install plugins or apply CLI settings during installation.

### Runtime and content

Owner-reported outside-sandbox success on Codex 0.153.4 supersedes the EPERM capability conclusion. Adapter coding has no dependency on reproducing a nested probe. Suitable-environment exact-version verification remains release acceptance; denied environments mean missing evidence, not unsupported transport. No probes/permission bypass are part of this Plan run. Preserve SD-4 factory-owned Claude authentication isolation.

Research/Plan/Implement retain their orchestrator first followed by exact spec order. Validate remains review-only, Spike empty. The small workflow skill cannot replace orchestration. Remove redundant root routing/full-body duplication while coordinating trusted mode headers and session artifacts (STYLE-8). Do not restore updater/admin/remediation phases.

## Task decomposition

Implementation tasks are assigned to go-implementation-engineer by the decomposer convention; test tasks remain separate for test-roster scheduling. Existing completed tasks remain historical foundation; these are remaining deltas. US-6 is recommended to the product owner as the publisher outcome explicitly required by the approved spec. All estimates are at most one day.

| ID | Title | Type | Depends On | Story | Est |
|---|---|---|---|---|---|
| T-1.6 | Distribution/version/provenance contracts | data | — | US-1 | M |
| T-1.7 | Operator source config and cache paths | infra | T-1.6 | US-1 | M |
| T-1.8 | Strict protocol/version/closure tests | test | T-1.6, T-1.7 | US-1 | M |
| T-2.6 | Bounded HTTPS acquisition and verified cache | logic | T-1.6, T-1.7 | US-2 | L |
| T-2.7 | Remote install/update and atomic publication | logic | T-2.6 | US-2 | L |
| T-2.8 | Cached status, refresh and pin/source controls | api | T-2.7 | US-2 | L |
| T-2.9 | Transport/security/offline tests | test | T-2.6 | US-2 | L |
| T-2.10 | Drift/pin/concurrency/command tests | test | T-2.7, T-2.8 | US-2 | L |
| T-3.1 | Correct capability record and target-environment procedure | infra | — | US-3 | M |
| T-3.2 | Native rendering and immutable bundle publication | logic | T-1.6 | US-3 | L |
| T-3.3 | Neutral request and CLI adapters | logic | T-3.2 | US-3 | L |
| T-3.4 | Post-classification and conversation identity | logic | T-3.3 | US-3 | L |
| T-3.5 | Classifier/resume/concurrency tests | test | T-3.4 | US-3 | L |
| T-4.1 | Move distribution sources and preserve corrected rosters | logic | T-1.6 | US-4 | M |
| T-4.2 | Focused skills and Implement orchestration | logic | T-4.1 | US-4 | L |
| T-4.3 | Read-only Validate/bootstrap/vendor parity | logic | T-4.2 | US-4 | M |
| T-4.4 | Remove embedded acquisition/production embed | logic | T-2.8, T-4.3, T-6.1 | US-4 | M |
| T-4.5 | Roster/closure/permissions/independence tests | test | T-4.4, T-3.5 | US-4 | M |
| T-5.1 | Durable version/source/bundle provenance | logic | T-3.4, T-2.7 | US-5 | M |
| T-5.2 | Reproducible benchmark and runner evidence | infra | T-5.1, T-4.5 | US-5 | L |
| T-5.3 | Operator documentation and final checks | test | T-5.2, T-6.3 | US-5 | M |
| T-6.1 | Deterministic release builder/artifacts | logic | T-1.6, T-4.1 | US-6 | L |
| T-6.2 | Independent publication workflow/hosting contract | infra | T-6.1 | US-6 | M |
| T-6.3 | Publisher reproducibility/consumer roundtrip tests | test | T-6.2, T-2.8 | US-6 | L |

## Per-task architectural hints

### T-1.6
Reuse team schema-2 decoder/capture; outer wire contracts belong in teamremote, installed provenance in teamsource. No render/network import in team. Strict duplicate/unknown/future schema faults aggregate deterministically (SEC-12, CFG-5); no migration or second registry.

### T-1.7
Follow config Defaults/template/Paths (CFG-1–8). Missing default is an add precondition, not startup failure. Source credentials are references, resolved outside config. Existing repository association remains a team ID only.

### T-1.8
Table-test semver core/prerelease/build metadata, equal precedence, unsafe paths, relative URL bases and unsupported schemas. Ordinary deterministic Go tests, no live Internet (TEST-1/2/7).

### T-2.6
Place acquisition/cache in teamremote with injected transport/auth at consumer wiring. Context/byte/redirect limits apply before trust. No automatic retry or fetch (CONC-6). Sanitize remote error/URL data and keep credential values out of cache.

### T-2.7
Extend teamsource Install/Update, capturePackage/validatePublication and existing write owner. Fetch outside lock, baseline-check inside. Protect obsolete/shared/override files. Prove multi-file crash recovery; aggregate independent-team errors without half-installing a team (STYLE-9). Force never bypasses integrity or pin policy.

### T-2.8
CLI owns flags/error mapping (STYLE-6); teamsource owns policy, drift and observations. Reuse list/show/validate/manage/repo association. Explicit source switch and digest-pin replace/clear are distinct from changing configured defaults. No network for ordinary inspection.

### T-2.9
Use httptest TLS and temporary roots for corruption, redirects, certificates, auth, cancellation and limit edges. Prove zero requests on daemon/classifier/work/resume/cached inspection. No integration tags or public/paid subprocess tests.

### T-2.10
Cross five states with freshness/policies, removed files, overrides and shared conflicts. Channel-coordinate writers/readers and inject interruption; avoid sleeps (TEST-8). Pin history survives blob eviction. Verify unchanged installations after any failed update.

### T-3.1
Correct RUNNER_CAPABILITIES provenance and suitable-environment procedure (EXT-2). Deliberately not a predecessor of adapter coding. Complete permissions/auth/delegation/isolation evidence before accepted delivery; denied environment does not imply unsupported transport.

### T-3.2
render owns pure native output; teamsource supplies selected immutable bytes/provenance. Reuse guarded publication with run paths, never remote origin reads. Keep full bodies out of bootstrap.

### T-3.3
agent.Request stays neutral; harness/codex own vendor role/config syntax. Preserve tiers and permission ceilings. Instructions travel through files/stdin, not raw argv (SEC-2). Reuse Claude auth isolation; no shared mutable catalogs.

### T-3.4
Resolve after mode selection in turn; preserve CONC-4/7 and existing vendor-switch guard. Extend current store segment ownership rather than another session subsystem. Identity changes rotate segments; durable provenance survives cleanup. Replace assigned-team refusal with complete delivery rather than fallback.

### T-3.5
Fake runners/real store fixtures prove concurrent modes, classifier empty exposure, offline launch, stable resume and rotation. Retained/restorable resources matter as well as hashes. Real observed isolation remains suitable-environment evidence.

### T-4.1
Relocate complete canonical trees to publisher input and retain known 6eb29bf exact first-position orchestrators. Preserve relative resources and vendor parity. Do not replace orchestrators with workflow skill.

### T-4.2
Keep implement-orchestrator sequencing production/test authoring and verification. Select language bodies through declared skills without forced delegation. Remove dedicated review/updater/admin phases and stale Codex updater references.

### T-4.3
Validate returns inline acceptance/code/evidence/drift verdict under unchanged read-only profiles. No Bash/test execution expansion. Update root router/header trust and paired outputs together (STYLE-8), retaining session artifacts.

### T-4.4
Remove production embed/default only after acquisition/publisher inputs exist. Test fixtures are explicitly test-owned. App build/update never refreshes teams; no bootstrap seed or compatibility fallback.

### T-4.5
Assert exact arrays, first orchestrators, complete closure and no unrelated eager bodies. Production build input excludes prompt trees; remote fixture remains installable. Preserve policy tests (TEST-10).

### T-5.1
Extend harvest/inspection: source URL/alias, version/manifest digest, selected IDs/mode, overrides/effective digest and bytes. No credentials in metadata; retain outbound redaction (SEC-6).

### T-5.2
Preserve old full-catalog benchmark fixtures before conversion. Compare equivalent task outcomes and all delegated usage, recording input/cached/total tokens when available, versions/models/cache/latency and missing telemetry. No bytes-as-token claims or nested-EPERM conclusion.

### T-5.3
Document configured HTTPS empty-home install, corporate auth, explicit local import, pin/source changes, offline status/update conflicts and retention. Complete make check/safe build; no dist/live runtime writes. Distinguish validated publishing artifacts from an unprovisioned public endpoint.

### T-6.1
Builder uses the consumer validation contract and exact deterministic manifest bytes/hashes. Output immutable team/version paths and discovery, complete resources and no hooks. Content release requires no litefactory rebuild or runtime registry service.

### T-6.2
Publish immutable files/manifest first, reject version replacement, advance stable last. CI/hosting topology is outside Go backend architecture and no infrastructure architect is rostered; engineer follows APP_CONSTRAINTS and records hosting assumptions. Supply independently triggered workflow/corporate instructions; owner credentials/public hosting remain deployment prerequisites, not invented URLs.

### T-6.3
Two content versions install/update with one unchanged consumer binary; rebuilding the app leaves installed content untouched. TLS-served fixtures prove path/hash agreement and reproducibility. An explanatory HTTP layout must not disable production HTTPS rules. These are local acceptance tests, not live deployment claims.

## Security findings

Scope: proposed remote distribution and inspected ownership boundaries. Rulebook: APP_CONSTRAINTS §1; architectural review, not a whole-code vulnerability scan.

| Area | Status | Finding |
|---|---|---|
| Input validation | Concern addressed by plan | New network boundary requires strict limits, rooted closure and digest validation before publication. |
| Auth/authorization | Concern retained explicitly | TLS/source choice is first-use trust; same-origin hashes cannot independently authenticate compromised publisher. Permissions remain local. |
| Secrets/configuration | Compliant design | Source-specific references only, approved origin only, no secret persistence with instructions. |
| Data handling | Compliant design | No SQL outside store; provenance excludes credentials; remote errors sanitized. |
| Supply chain | Concern addressed by plan | Independent content versions and retained digest history; signatures deferred, not implied; no new modules. |
| Error handling | Compliant design | Typed aggregate diagnostics; failed fetch/integrity preserves installed state with truthful freshness. |

No standards deviation requested. Recorded SD-4 factory-set Claude configuration amendment still applies. Structural tests must change deliberately with reviewed boundaries (TEST-10), not be disabled.

## Platform findings

- Platform: project layout — internal transport, pure config, reuse existing owners.
- Platform: errors — typed/wrapped aggregate errors, no new panic/recover.
- Platform: concurrency — caller context, bounded sequential network and existing locks, no background refresh.
- Platform: interfaces — small consumer-defined seams only.
- Platform: dependencies — stdlib HTTP/crypto/JSON, no new module required.
- Platform: HTTP server/shutdown — not applicable (client); servers are fixtures only.
- Platform: database — reuse association; any conversation migration remains store-owned and one-connection-safe.
- Platform: tests — co-located deterministic hand fakes/TLS fixtures, race checks, no paid CLI tests/invented integration tags.
- Platform: logging — existing structured logger/redaction, no added telemetry framework.
- Platform: format/lint — make check and safe build; record actual unrelated baseline failures rather than assuming old counts persist.
