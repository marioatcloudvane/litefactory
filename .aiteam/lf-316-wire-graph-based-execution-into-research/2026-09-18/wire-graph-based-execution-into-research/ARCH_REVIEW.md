# Architect: Go Architect
# Created: 2026-09-18

Verdict: Ready for typed Plan authoring. Foundation integration and authentic delivered-host evidence are explicit implementation prerequisites, not completed facts. No major owner decision or standards deviation is needed.

## System Overview

### How This Feature Fits

Research becomes a production consumer of the #315 graph engine. Keep graph parsing and decisions pure in `internal/graphexec`, catalog capture in `internal/teamsource`, catalog mutation in `internal/team`, native file rendering in `internal/render`, shared runner data in `internal/agent`, process specifics in `internal/harness` and `internal/codex`, orchestration and effects in `internal/turn`, polling/time/admission in `internal/daemon`, and SQL exclusively in `internal/store`. `internal/report` renders; it must not acquire orchestration authority.

Important source qualification: this checkout contains no `internal/graphexec`, graph persistence accessors, or foundation graph migration. The retained commit `1fbd2c9716b6fea8d4c321a1ac713a6dbdbe4d5b` is locally readable. Its `internal/turn/graph_node.go` implements one activity reservation followed by atomic output/transition completion and explicitly has no production caller. Its `internal/graphexec/invoke_agent.go` decodes references without resolving them. Its migration `0048_graph_execution_foundations.sql` contains definitions, instances, attempts, outputs and transitions, but no owner-wait record or recovery protocol. Plan reuse against this retained revision, and require integration/revalidation of the existing increment before graph-dependent implementation. Do not rebuild it or treat historical branch evidence as merged-current behavior. Migration numbering must be chosen against the actual integrated tree.

Current reusable source inspected: `teamsource/snapshot.go` captures installed content under a catalog lock with 1 MiB per-resource and 16 MiB aggregate bounds; `team/catalog_write.go` writes data then index then provenance; `render/native.go` renders native role files; `turn/delivery.go` hashes delivery identity, intersects tools with the parent ceiling and manages session segments. Current catalog ownership accepts only `index.json`, `agents/` and `skills/`; the proposed team/mode DOT layout therefore needs coordinated publish/install/capture/status changes, not just a lookup helper. Existing bootstrap describes an orchestrator roster; graph main-role delivery must introduce a distinct typed selection, not quietly reuse that text.

### Components and Boundaries

No new service, executable or scheduling framework is required. Extend the existing graph package with strict Research action config/result decoding and loop/if decisions. Extend `teamsource` with a graph content capture that owns its bytes, resolved references, effective defaults and content identities. Keep parsing independent of filesystem access: graph syntax validation checks reference grammar; capture resolves and validates the complete closure before admission. Keep `team` independent of `render` and `teamsource` (SEC-18).

`turn` adapts that capture to a main-role `agent.Request` and a selected native skill closure. Native host launch settings belong exclusively to the respective runner, using supported file-backed configuration and stdin for authored input (SEC-2). The graph receives a validated node result, never provider event-stream fragments. The final application result stays under `agentresult`/submission validation and existing factory authority. Internal node ingestion grants no publication permission.

### Data Flow

1. Existing owner/mode admission selects a durable execution path. Eligible new Research selects graph; declared legacy continuations retain legacy.
2. Capture latest installed graph and role/skill closure, validate it, record actual attempt provenance, and compare semantic topology and effective instruction identities with the retained execution.
3. The current worker advances deterministic activities using durable transitions. Before an agent call, allocate/link an actual run, graph attempt and provider invocation. No SQL transaction or clone gate spans the model call.
4. Validate/save internal node JSON. `ask` routes to explicit question comment handling and a durable owner wait; `done` routes to explicit report comment handling and graph completion. There is no automatic second agent call after `ask`.
5. Persist effect intent before external delivery, reconcile exact identities after uncertainty, and release the worker. A correlated authenticated owner reply claims continuation once and makes the next agent pass eligible.
6. On instruction change, rotate provider session with saved findings. On topology change, create a successor instance at the latest start with retained brief/reply linkage. Previously authored effects remain associated with the old instance and their original identities.

### Concurrency and Context

Use current task ownership, worker capacity and clone gates. One worker can advance ordinary activities without a queue job per graph node. An owner wait is data, never a goroutine or blocked subprocess. `daemon` supplies timestamps and recovery scheduling; `turn` does not read the clock or sleep (CONC-2, CONC-6). Store operations use short transactions on the existing single SQLite connection (CONC-5, CONC-18). Clone slices/maps at request/result boundaries (CONC-1). Explicit owner continuation may use supported refused-resume cold fallback; automatic recovery must not launch a Research graph agent, irrespective of the legacy quota policy.

### Shared State and Side Effects

Keep instance lifecycle distinct from subprocess outcome, provider session availability, final application outcome and comment delivery. Successful question handling is an incomplete Research application result, not a failed process: it may pass existing success-only save gates. A failed/timed-out agent stays failed with uncommitted work preserved; do not weaken CONC-8 to save it. Internal graph output remains durable in store independently of git save. Only confirmed comment delivery advances delivery acknowledgments (CONC-4, CONC-10).

Add a store-owned explicit question-wait record linked to instance, originating node/attempt, question output and effect identity, with optional claimed owner comment and continuation state. This avoids overloading `graph_instances.status` or interpreting arbitrary pause-node names as authoritative state. Existing `graph_outputs.owner_comment_id` can retain reply attribution; uniqueness must be scoped to the task/source identity, not guessed globally. Pending waits exclude due-agent dispatch, have no expiry, and retain a durable claim until the continuation attempt is allocated; a crash before allocation cannot lose the reply. Topology restart transfers authorized input linkage, not effect identity.

## Cross-story Contracts

These names are architecture vocabulary, not instructions to publish an unimplemented wire schema.

- Actions: register `lf_research_agent`, `lf_research_loop`, `lf_research_if`, `lf_research_comment` with closed outcome sets and graph validation. Start/end stay markers. Wait is persisted control state entered after the question comment, not another agent. Validation checks the question route cannot return to agent without owner authorization.
- Conditions: use explicit `lf_field`, `lf_condition`, `lf_default` attributes; initially the supported typed field is string `response` and equality is the only operation. The default is permitted for the loop's first evaluation before any output exists; after an attempt, missing output/field is an error. `ask` and `done` are the initial response vocabulary; no implicit `continue` response or expression evaluator. Successful if/loop outcomes map to declared true/false edges. Finite `lf_limit` applies across owner continuations of that instance, with exhaustion handled as an explicit incomplete diagnostic/comment path.
- Node result: strict versioned object containing `response` and an authored Research application-result candidate (`result`), with bounded saved-output references supplied separately by the factory. Validate `ask` against incomplete result plus nonempty permitted questions; `done` against completed Research result and no questions. Reuse existing Research content, question and artifact structures instead of maintaining a competing prose schema. Reject duplicate/unknown keys, null required members, extra JSON, wrong response types, inconsistent outcomes and unknown versions. Initial total node JSON ceiling: 1 MiB, further constrained by existing application-result limits. Artifacts are worktree-relative regular files beneath the supplied session; reject absolute/dot traversal, symlink ancestry and foreign session references. Input/output byte limits apply before allocating/unmarshalling.
- Submission bridge: a factory-owned invocation kind selects internal node validation/retention versus ordinary whole-turn publication. The node's retained application candidate still satisfies the adopted final-result obligation at subprocess exit. Explicit comment activity applies the already validated candidate to publication through existing journals. Factory metadata binds graph/node/attempt/run identity; agent JSON cannot choose it. Preserve existing receipt semantics and correction/retry identity. Internal return must not trigger the old unconditional final comment path.
- Content identity: retain separate canonical semantic-topology identity and effective-instruction identity. Formatting-only DOT changes need not restart topology, but effective action/task/model/condition/edge changes do. Role/skills/defaults changes rotate the provider session. Actual DOT/captured bytes remain provenance; the foundation's pinned definition check must not be bypassed or repointed in place. A successor instance records the new definition and links prior findings.
- Limits/defaults: node override > role default > team default. Missing effective task/model or invalid limits fail preflight. Keep configurable finite node limits and existing subprocess timeout; no new global budget. Initial shipped node limits should be documented in the workflow and exercised by bounded tests; a high explicit finite question allowance avoids implying that a pause expires by age.

## Per-outcome Structural Hints

The feature spec contains no task IDs yet; the following blocks bind candidate outcomes and their implementation/test task groups. The Plan author maps these to typed task IDs without inventing extra product scope.

### Catalog and complete package delivery
**Agent:** go-implementation-engineer; tests: go-unit-test-engineer and go-integration-test-engineer.

Extend `team`, `teamsource`, publishing manifests and management CLI together for `<team>/<mode>.dot`, mode agents, team skills and team-local shared folders. Preserve legacy package reading and provenance. Resolution accepts kebab-case bare names or exactly `shared/name`; reject duplicate selected native names and dependency cycles. Capture closure under one catalog lock, validate all independent faults before any write, retain resource-relative links, and use owned-path rederivation/symlink rejection (SEC-12–18). Add pure validation tests and real temporary catalog install/update/drift tests; no installed sibling-tree parity requirement.

### Main role in Claude
**Agent:** go-implementation-engineer; tests: go-unit-test-engineer.

Extend `agent` delivery selection, `render.Native`, `turn/delivery.go` and `harness` launch composition. Select the rendered role as the main session role, not merely an available specialist. Keep ordinary project guidance in its proper channel; the existing bootstrap must not promote conflicting legacy routing into the selected role. Preserve mode tool ceiling, allowlisted environment and cold/resume/fallback behavior (SEC-2–4, CONC-23). Synthetic launch tests prove composed flags/files only. Authentic externally captured host evidence must establish main-role, skill and project-context behavior under exact delivered permissions.

### Main role in Codex
**Agent:** go-implementation-engineer; tests: go-unit-test-engineer.

Extend the same neutral delivery selection in `codex` with file-backed main developer instructions and selected native skill resources. Make instruction identity stable across compatible same-node continuation; ordinary resume supplies incremental task/reply data without appending the complete factory bootstrap again. Refused resume cold fallback receives the complete role/result/skill context and a separate metered invocation. Preserve native repository instruction discovery and filesystem restrictions. Authentic delivered-configuration and compaction evidence remains separate from synthetic argv/prompt tests (EXT-4–5).

### Agent execution and internal result retention
**Agent:** go-implementation-engineer; tests: go-unit-test-engineer and go-integration-test-engineer.

Extend foundation `ActivityRequest`/`ExecuteNodeAction`, the shared request/result contract and submission result adapter. Require a real run link for production agent attempts; reuse meter attempt lifecycle and provider segments. Keep graph result retention atomic with completion/transition, and stale-attempt checks effective. Separate agent process success, valid node input, graph continuation and public finalization. Test invalid/oversized/misattributed results and zero-comment successful internal returns against real migrated SQLite, fake runners and existing turn fixtures (TEST-3, TEST-6).

### Loop and if activities
**Agent:** go-implementation-engineer; tests: go-unit-test-engineer.

Implement typed config checks/decisions in `graphexec` and thin `turn` handlers; do not introduce executable conditions. Validate field/value/default and every branch before launch. Test first-pass initialization, strict post-result handling, non-spinning ask behavior, declared edges and persistent dispatch exhaustion. Pure decision tests suffice except persisted counter behavior, which uses the real store fixture.

### Explicit comment activity
**Agent:** go-implementation-engineer; tests: go-integration-test-engineer.

`turn` owns intent and delivery; `report` owns bounded rendering; `github` owns redacted mutation signatures. Reuse existing result/effect journals and receipt correlation. A replay checks matching bot provenance/identity and exact authored content; uncertainty never becomes permission to generate another comment. Exercise HTTP send-before-acknowledgment, reconciliation, render failure and terminal finalization suppression. Keep action errors reportable outcomes (CONC-3) and report debt independent of agent retry.

### Owner pause and continuation
**Agent:** go-implementation-engineer; tests: go-integration-test-engineer.

Store explicit waits and reply claims transactionally; route owner comments through existing numeric identity/mode/question permission admission in `daemon`. The corresponding `turn` continuation resumes the originating node only when compatible and carries saved output as untrusted data. Test reply duplication, restart before/after claim allocation, stale/non-owner input, no-question steering, question delivery pending and actual subsequent worker admission. Use rendezvous and subsequent ticks, not sleeps or invented slot-release events (SEC-8, TEST-8–9).

### Interruption, cancellation and recovery
**Agent:** go-implementation-engineer; tests: go-integration-test-engineer.

Extend startup run reconciliation and existing cancellation hooks with graph-attempt settlement and report-only recovery. Distinguish an allocated interrupted invocation from an unallocated continuation reservation and an already authored effect. Guard existing automatic quota/recovery admission against graph-agent retries. Issue closure, retirement and confirmed explicit mode switch cancel graph execution while preserving owed effects/findings. Retain original failure causes and never reinterpret a receipt as success (CONC-13–15, CONC-21).

### Default admission and latest installed content
**Agent:** go-implementation-engineer; tests: go-unit-test-engineer and go-integration-test-engineer.

Use the existing repository configuration owner for the explicit legacy choice; document the actual key in defaults/operator docs and validate it strictly (CFG-1–6). Persist graph/legacy selection per admitted task continuation, separate from graph version selection. Capture latest installed content at each authorized launch boundary, rotate same-node sessions for instruction changes, and start successor instances for semantic topology changes. Reconcile prior effects before producing equivalent successor publications. New eligibility does not silently convert a legacy continuation. Keep default enablement gated until assembled recovery and authentic-host evidence is retained.

### Standard Research workflow and adoption
**Agent:** go-implementation-engineer; tests: go-unit-test-engineer and go-integration-test-engineer.

Ship the graph and simplified single role in shared `teams/source` and release packaging. The role performs clarity/investigation itself and returns the internal contract. Add offline assembled clear/ask/reply/failure/report-recovery scenarios; retain separately authorized versioned Claude/Codex captures before default rollout. Capture exact snapshot, host version, launch settings, assertions and unavailable prerequisites; no automated paid host tests, no claim that fixtures satisfy authentic #274/#296 helper/correction/receipt observations (EXT-1–5). Do not require the owner to produce a performance comparison for implementation completeness.

### Usage attribution
**Agent:** go-implementation-engineer; tests: go-unit-test-engineer and go-integration-test-engineer.

Extend existing `agent` usage capture, `turn/meter.go`, `store/meter*` and inspection/log projections with graph/path/node/attempt/run links. Do not add a second collector or pricing pipeline. Include failed and refused-resume fallback invocations and available timing, while retaining unknown counters as unknown (CONC-18–20). Join actual attempts rather than assigning a whole multi-node run's usage to every node. No benchmark framework or guaranteed savings.

## Ordering and Planning Notes

Integrate/revalidate existing #315 increment first. Establish catalog capture and neutral node/result contracts before host-specific adapters and control handlers. Comment delivery precedes owner wait/resume; node retention and wait semantics precede recovery. Usage attaches alongside node execution. Default admission/update behavior depends on recovery; final shipped-workflow rollout depends on the assembled path and required external evidence. The previous eleven outcomes remain a reasonable story organization; integrating the existing foundation is a prerequisite, not a new rewrite story.

Keep shared source/renderer changes outside installed protected trees. If concrete implementation later requires `.claude/**` or `.codex/**`, create separate independent ownership stories: Codex edits `.claude`, Claude edits `.codex`; shared-source work precedes both and depends on neither. No evidence requires these extra stories now. Do not run a generator that modifies both trees.

## Security and Platform Findings

| Area | Finding |
|---|---|
| Input validation | Concern to address: DOT attrs, native-name collision, node JSON and rooted artifacts must fail before action/effect boundaries. Existing bounded capture is reusable. |
| Authorization | Preserve factory-selected mode, tools and numeric owner identity; catalog role/skills cannot grant permissions or forge access. |
| Secrets | Preserve allowlisted subprocess environment and redacted outbound bodies; no credentials in graph files or submission payloads. |
| Data | SQL stays in store with ctx-first short transactions; no query or transaction escapes into action handlers. |
| Dependencies | Reuse foundation parser dependencies after integration; no new expression engine, queue or mocking framework. |
| Errors | Keep actual failed outcomes and safe bounded diagnostics separate from transport/recording errors and pending effects. |

Platform: internal package boundaries and consumer-owned interfaces apply (TEST-5); no HTTP service topology or new frontend is needed. Structured logging remains injected `slog`. New migrations are embedded SQL files, with the pool capped at one connection. Implementation runs scoped meaningful checks and the repository's required final `make check`; this architecture review ran no tests. Generic platform advice to add migration libraries, recover handlers or mocking tools does not override repository rules.

## Deviations and Evidence Limits

No standards deviation requested. Current tree absence of the foundation is an explicit integration gate. Historical selected-host versions are not verification of future delivery. The referenced nested-agent evidence and story-contract manifest documents are absent; their supplied evidence/contract obligations remain in force. Required authentic host observations and helper correction/receipt evidence remain unverified until a separately authorized operator supplies them; no paid nested agent launches are authorized here.
