# Feature Spec: native-host-integration-and-verification
# Status: APPROVED
# Date: 2026-09-14

## Goal
- Make the existing mode-scoped litefactory MCP operations usable alongside permitted native tools in ordinary Claude Code and Codex attempts, with authentic evidence of both hosts' behavior and Validate enforcement.
- Planning status denotes ready requirements; implementation, authentic evidence, factory publication, and readiness are not established by this document.

## User Stories
| ID | Story |
|---|---|
| US-1 | As a factory operator, I want each eligible actual agent launch connected to the existing authorized MCP services so agents can use them without adopting #264's broader workflow changes. |
| US-2 | As a Claude Code agent, I want explicit attempt-local MCP registration alongside my permitted native tools so supported factory operations are available without weakening isolation. |
| US-3 | As a Codex agent, I want explicit attempt-local MCP registration alongside my permitted native tools so supported factory operations respect the existing sandbox and mode restrictions. |
| US-4 | As an agent, I want short, operation-specific guidance and valid payload context so I can preflight, repair, submit, and inspect receipts without confusing application results with checkpoints or duplicating effects. |
| US-5 | As a maintainer, I want automated coverage of launch lifecycle and compatibility so failures and concurrent attempts cannot leak authority or break existing completion paths. |
| US-6 | As an operator and reviewer, I want authentic current-implementation Claude and Codex evidence, including Validate, so configuration alone cannot be mistaken for delivered host integration. |

## Settled Decisions
| Decision | Basis |
|---|---|
| MCP supplements each mode's existing permitted tools; ordinary runs need not invoke it. | Owner direction retained in research run 0002. |
| Include the minimum runtime bridge needed for usable registration; defer result-driven scheduling, readiness, mode transitions, and replacement of the completion pipeline to #264. | Owner permitted necessary overlap while asking to keep it simple. |
| Render shared guidance plus applicable concrete cards and one relevant mode sentence. | Owner token-efficiency refinement; research run 0003. |
| Preserve all authentic dual-host and Validate obligations. | Issue criteria; subsequent scope refinements did not waive them. |
| Acquire live proof only through separately authorized operator-controlled ordinary top-level runs; engineering and CI use offline tests and supplied captures. | `docs/decisions/nested-agent-evidence.md`; `docs/agent-evidence-capture.md`. |

## Existing Features to Reuse
| Existing feature | Owner / source |
|---|---|
| Descriptor-authenticated stdio MCP adapter and fixed serve arguments | `internal/mcpadapter`, `internal/cli/cmd_mcp.go`, `internal/agent/mcp.go` |
| Runtime authority, attempt leases, permission digest, revocation | `internal/turn/mcp_attempt.go`, `internal/store/mcp_authority.go` |
| Application validation, submission, correlation, durable decisions | `internal/turn/agent_results.go`, `internal/turn/mcp_tools.go`, `internal/submission` |
| Installed mode-scoped family dispatch and current grants | `internal/turn/mcp_families.go`, `internal/submission/family_tools.go` |
| Actual-launch binding and terminal Validate compatibility seam | `internal/turn/submission_runner.go`, `internal/turn/submissions.go` |
| Native runner configuration, tool ceilings, sandbox and isolation controls | `internal/harness/runner.go`, `internal/codex/runner.go` |
| Existing contribution shapes, application create/update formats, snapshot/check evidence and receipt dimensions | `internal/agent`, `internal/agentresult`, `internal/submission` |
| External evidence provenance, sanitization, offline replay | `docs/agent-evidence-capture.md` |

## Data Model
| Entity | Field | Type | Required | Constraints |
|---|---|---|---|---|
| Runtime owner | Runtime identity | Existing trusted identity | When MCP enabled | Initialized once per factory runtime, not replaced for each attempt. |
| Actual launch | Attempt binding / permission digest | Existing immutable binding | When registered | Current run, mode and trusted scope; refresh on actual resumed/fallback launch. |
| MCP launch handoff | Executable / descriptor / attempt | Existing validated immutable value | When registered | Clean absolute paths; matching live broker and authorized attempt. |
| MCP launch handoff | Permitted tools | Copied bounded string collection | When registered | Derived from the same installed server profile; exact names, no independently invented provider catalog. |
| Host registration | Command / args / env / allowed tools | Structured host configuration | When registered | Fixed adapter command, minimal environment, no forge secrets or capability content in prompts/logs. |
| Prompt context | Installed operation cards and payload contract | Conditional text and supplied JSON shape | For advertised operations | One relevant payload contract; trusted baseline/revision for updates; no invented factory identities. |
| Completion observation | Request ID and receipt dimensions | Existing durable receipt | After submission | Preserve input, engineering, acceptance and remote effects independently. |
| Verification record | Source, host/version/context, events, integrity, provenance | Sanitized capture plus metadata | For authentic acceptance | Preserve identities and event ordering; distinguish authentic sources from synthetic tests and generated claims. |

### Relationships
- One runtime owner → multiple concurrent authorized actual-launch bindings.
- One actual launch → one live tool-enabled registration and its cleanup/revocation lifetime.
- One installed server profile → neutral permitted-tool projection → provider filtering and guidance.
- One submitted payload → existing durable correlation → receipt; terminal fallback reconciles recorded checkpoints before new application of effects.
- One acceptance assertion → identified offline check or operator-supplied authentic observation with version/context bounds.
- No new business entity, schema migration, tool-family semantic, or publication grant is required by this scope; reuse existing records.

## API / Interface Changes
| Method | Endpoint / Action | Auth | Notes |
|---|---|---|---|
| Factory preparation | Optional actual-attempt MCP bridge | Existing trusted runtime/attempt scope | Works for eligible unmanaged Research/Spike as well as managed modes; does not require converting them to managed stories. |
| Claude launch | Generated per-attempt `--mcp-config` plus existing `--strict-mcp-config` | Exact installed MCP tool authorization | Preserve native `Tools` ceiling; separate validated MCP registration surface. |
| Codex launch | Per-attempt `mcp_servers` command/args/env/`enabled_tools` | Exact installed MCP names | Structured encoding; retain existing configuration isolation and Validate shell/multi-agent disabling. |
| Agent prompt | Shared usage block, applicable cards, payload context | Render from live trusted registration | No registration means no MCP-specific instructions. |
| Existing MCP calls | Communication, feedback/evidence/receipts, issue/activity and optional creation families | Unchanged mode, target and grant checks | Existing schemas and semantics; no publishable Plan submission via communication tools. |
| Completion compatibility | Reconcile live checkpoint receipts with terminal fallback | Existing bound contribution scope | Preserve required final findings/artifacts; prevent duplicate effects without trusting prose alone. |

## Business Rules
- Honor `mcp_auto_start`; disabled/absent registration preserves current behavior and does not allocate usable MCP authority.
- Resolve identity once per runtime; bind/revoke each actual launch, including cold, resumed and fallback launches; transport and private files share the attempt lifetime.
- Failed registration cannot yield a partially authorized launch or advertise dead operations; preserve clear diagnostics and existing safe completion behavior.
- Claude strict MCP isolation remains effective; Codex injection does not admit ambient/colliding servers or weaken user-config, delivery or sandbox controls.
- Validate retains its permitted native read surface: no shell, edits, fresh tests, engineering capture, creation or unrestricted delegation; permitted checkpoint/activity/scoped-comment effects retain their existing authorization model.
- Guidance teaches `communication_preflight(P)` → `communication_submit(P)` → `communication_receipt({"request_id":ID})` for application results, and `feedback_preflight({"kind":"checkpoint","input":C})` → `evidence_submit(C)` → `receipts_get({"request_id":ID})` for checkpoints only where installed.
- Include conditional issue lookup, comment, activity, creation and mode notes from research run 0003; creation requires its separate live grant, not mere tool visibility.
- Supply valid mode-relevant application/checkpoint payload context once; updates include the trusted expected revision; omit factory identities and unsupported fields.
- Preflight is validation, not recorded proof/approval; correct diagnostics and inspect the matching receipt before exit after submission; pending/uncertain remains pending/uncertain.
- Lost replies trigger known-request receipt lookup; never mutate payload solely to force a retry or switch to a path that can duplicate an uncertain effect.
- Normal runs retain existing completion paths; designated external verification scenarios deliberately exercise MCP. MCP receipts do not become new scheduling/readiness authority.
- Editorial targets: shared guidance roughly 150–200 words; ordinary combined guidance roughly 300–450 words excluding the one supplied payload contract. Record actual available provider-rendered token measurements, including schemas/examples, or explicitly report measurement unavailable; word counts are not billed-token proof and target overruns need a concrete justification.

## Edge Cases & Error Handling
- Malformed launch value, stale guard, mismatched descriptor/attempt or inconsistent installed catalog → reject before host invocation.
- Configuration values with whitespace, quotes, non-ASCII or host syntax characters → preserve intended structured values without shell interpolation/config injection.
- One concurrent attempt completes/cancels → revoke and clean only its resources; other live attempts remain authorized.
- Resume/fallback replaces an attempt → old lease revoked and files/transport closed, replacement receives a fresh descriptor and guidance.
- MCP unavailable or reply lost → report known state, retain correlation, use permitted fallback only after preventing uncertain duplicate effects.
- Validate repeats live findings in required terminal output → reconcile durable records, apply each contribution/effect at most once; mismatching/conflicting findings must not silently overwrite a recorded result.
- Live Validate provides no complete terminal coverage → retain existing full-story findings requirements; a live receipt alone does not excuse missing final findings.
- Unavailable authentic capture, incomplete source ordering, unknown host version or missing enforcement observation → affected obligation remains unverified; no synthetic substitution.

## Out of Scope
- #264 scheduling/readiness adoption, automatic mode transitions, broad result orchestration or replacement of contribution/completion pipelines.
- Mandatory MCP use in every normal run; removal of permitted native tools; new tool families, business semantics, creation workflows or publication permissions.
- Persistent user-host configuration edits, permission widening, daemon restarts/runtime-state edits, nested paid host capture, live capture as a managed engineering/CI step.
- Broad installed-role/skill rewrites or protected instruction-tree changes; dynamic attempt guidance is the scoped delivery surface. If implementation establishes protected-tree edits are necessary, split them into independently owned provider stories through Plan before including them.
- Claims of compatibility for unobserved versions, broad historical-issue recovery, or claiming this issue is delivered before authentic evidence is reviewed.

## Assumptions and Risks
| Item | Disposition / basis |
|---|---|
| No unresolved owner scope question | Latest instruction requests a plan after explicit run 0002/0003 refinements. |
| External capture owner | Assume repository owner arranges a separately authorized operator after wiring is available; current planning authorization does not itself authorize nested capture. No capture currently supplied. |
| Minimum authentic matrix | One working-mode and one Validate scenario per host, four total; same implementation revision and recorded contexts. Both hosts must demonstrate a same-attempt correction/decision chain; Validate scenarios also exercise permitted live checkpoint/receipt calls and effective no-shell restrictions. |
| Version support | Versions from run 0001 (Claude 2.1.270, Codex 0.154.0) are historical probes only; implementation and capture record actual versions, with no assumed present compatibility. |
| Lifecycle complexity | Existing unmanaged-mode preparation and singleton authority require real bridge work; if delivery requires deferred #264 migration, report the concrete dependency and replan instead of expanding scope. |
| Token telemetry | Actual provider token measurements may be unavailable; record honest availability and scoped render sizes, never fabricate savings. |

## Acceptance Criteria
- [ ] AC-001: Eligible enabled actual launches bind existing trusted MCP services and a matching tool-enabled broker; disabled/absent registration preserves prior behavior, and Research/Spike availability does not require managed-story conversion.
- [ ] AC-002: Runtime authority is startup-owned; each actual cold/resumed/fallback launch receives isolated scope and resources, with cancellation/exit/replacement cleanup that does not revoke concurrent attempts.
- [ ] AC-003: Neutral registration carries validated immutable paths/attempt and a copied exact installed permitted-tool catalog; invalid/stale/mismatched registration is refused before host launch.
- [ ] AC-004: Claude receives a private per-attempt generated MCP config for the adapter plus retained strict MCP isolation and exact MCP tool authorization, preserving existing native tool ceilings, including Validate.
- [ ] AC-005: Codex receives correctly encoded command/args/minimal-env/exact-enabled-tools MCP configuration, preserving existing sandbox/config isolation and Validate shell/multi-agent restrictions.
- [ ] AC-006: Guidance is conditional on live installed operations, uses the agreed compact cards and valid supplied payload contracts, separates application results from checkpoints, teaches diagnostics/receipt/retry handling, and preserves final responses/artifacts and existing Plan/publication permissions; measured guidance sizes and available token measurements or explicit unavailability are recorded.
- [ ] AC-007: Live Validate checkpoint receipts are reconciled with required terminal findings so repeat output/recovery cannot apply duplicate contributions/effects; missing/conflicting findings remain explicit failures or unverified states under the existing completion contract.
- [ ] AC-008: Automated checks cover enabled/disabled/absent setup, malformed/quoted values, catalog parity, native-tool preservation, server collision/isolation, concurrent attempts, cancellation, cleanup, resume/fallback and completion reconciliation; implementation passes `make check` with actual retained output and no live agent acquisition in checks.
- [ ] AC-009: Authentic ordinary top-level Claude and Codex working-mode runs on the implemented revision each show permitted MCP discovery, a correctable preflight diagnostic followed by repaired input in that attempt, submission and matching durable decision receipt observed before exit, plus use of a permitted native tool alongside MCP.
- [ ] AC-010: Authentic Validate runs on both hosts show effective narrow native/MCP inventories, allowed live checkpoint/receipt use before exit, and observable refusal/unavailability of shell and delegation escape paths; merely refraining from shell or asserting configuration is insufficient, and retained evidence is used without edits/fresh execution.
- [ ] AC-011: Reviewed sanitized evidence records host/version, implementation revision, environment/configuration, protocol/transport, exact observed calls, source locators, stable attempt/request correlation, ordered decision/exit events and separate receipt dimensions, with provenance, SHA256 and record/byte counts; offline replay and documentation state only supported observations and preserve missing obligations.

## Candidate Evidence Obligations
| Criterion | Proof method | Source availability | Version/context bounds | Missing prerequisite / disposition |
|---|---|---|---|---|
| AC-001 | Source review and offline production-preparation tests using real local store/broker seams and fake host runner. | Existing services available; new production bridge coverage not written. | Current implemented source snapshot; does not establish actual host use. | Implement bridge and retain executed evidence, including unmanaged modes and disabled policy. |
| AC-002 | Offline concurrency, cancellation and cold/resume/fallback tests across the production preparation seam. | Existing authority tests available; integrated lifecycle coverage not written. | Current implemented source snapshot; does not establish actual host lifecycle compatibility. | Implement startup owner and per-launch cleanup; retain executed evidence. |
| AC-003 | Value validation, defensive-copy, catalog-equality and rejected-launch tests. | Existing launch-value validation available; permitted catalog handoff absent. | Current implemented source snapshot; provider acceptance additionally requires authentic captures. | Extend immutable handoff and retain executed rejection/copy assertions. |
| AC-004 | Structured config/argv/file-permission/cleanup tests and source review; actual interpretation cross-referenced to AC-009/AC-010. | Runner source exists; generated registration and authentic observations unavailable. | Tested source plus actual Claude capture version, not historical version command alone. | Implement runner path; external capture remains required for runtime claims. |
| AC-005 | Structured config/encoding/isolation tests and source review; actual interpretation cross-referenced to AC-009/AC-010. | Runner source exists; registration and authentic observations unavailable. | Tested source plus actual Codex capture version; unsupported nested invocation excluded. | Implement runner path; external capture remains required. |
| AC-006 | Conditional prompt render/schema-shape tests, documented word/byte sizes and available actual provider token measurements. | Existing payload contracts and proposed wording available; new renders/telemetry not supplied. | Per-mode/provider installed catalogs on implementation revision; unavailable billing/token detail explicitly bounded. | Implement guidance, retain size checks and genuine measurement availability. |
| AC-007 | Offline durable-receipt/live-plus-terminal reconciliation tests including duplicates, lost replies and conflicts. | Existing terminal handler and receipt services available; new reconciliation coverage absent. | Recorded test snapshot; real before-exit Validate proof remains AC-010. | Implement bounded reconciliation without #264 workflow migration. |
| AC-008 | Retained executed repository checks and targeted lifecycle cases. | Existing synthetic MCP tests available; new tests and final `make check` unexecuted. | Implementation snapshot only; no live-host claim. | Implementation engineering supplies passing checks; Validate reads them. |
| AC-009 | Separately authorized current-implementation external observation, reviewed sanitized provider traces and bounded offline replay. | Unavailable: no authentic MCP host capture supplied. | Actual Claude/Codex versions, implementation revision, working modes, host/sandbox and same-attempt event ordering must be recorded. | Operator acquisition after wiring; remains unsatisfied until both captures establish all relationships. |
| AC-010 | Separately authorized external Validate observations of permitted calls plus effective denial/unavailability, then read-only review of retained captures. | Unavailable: configuration and synthetic inventories are insufficient. | Both actual host versions and Validate profiles on the implemented revision; no fresh checks inside Validate. | Operator acquisition with supplied retained evidence; missing negative/enforcement observations remain unsatisfied. |
| AC-011 | Provenance/sanitization review, checksum/record checks, offline replay against specific observed assertions, scoped documentation review. | Capture policy/template exist; #274 authentic capture files do not. | Historical assertions limited to recorded source/version/context; no global present/future compatibility claim. | Supply reviewed captures and retain actual replay results; revise unavailable-evidence statements only for proven scope. |

## Questions for the Owner
- None required to complete Plan; external capture remains an explicit later operator prerequisite, not waived scope.
