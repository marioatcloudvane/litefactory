AI;DR: The complete proposal preserves current Discord behavior and saved recovery while separating component ownership. Review the criteria and task order at this third-stage gate; implementation remains future work.

Uncovered outcomes: none. Ruling: none; no agreed outcome is narrowed.

| Outcomes | Active story |
|---|---|
| O-01, O-02, O-03, O-04, O-05, O-06, O-07, O-08, O-09, O-10, O-11, O-12, O-13, O-14 | story-001 |

Parent link-only index: [story-001 — Separate Discord from factory internals](https://github.com/marioatcloudvane/litefactory/issues/1370). Story-specific intent, architecture, criteria and tasks remain on that sub-issue.

Exact active set: s1 → story-001 only; no retired stories, sibling edges or external issue dependencies. Set revision: 0f1eb008e1bf954656308d15ea960fee819a33d4ed9e75493cc88a89d49fb2c4. Shared decisions: initial, unchanged. The core prerequisite is already merged; remote extraction is not a prerequisite. Overlapping future approval/composition edits require merge ordering outside this set.

story-001: L — Spans Discord lifecycle, conversations, core actions, shared approvals and persisted recovery across packages; retires direct paths. No schema migration; upgrade/rollback and trusted-host boundary evidence remain to establish.

Shared decisions retain one repository/module/process/release and unchanged SQLite state. Discord owns transport and conversations; core owns policy, publication and the single shared approval coordinator. Consumers receive bounded capabilities. K-01–K-08 and N-01–N-03 remain verbatim in the story; AS-01–AS-04 remain assumptions, including unverified historical compatibility and live drain evidence. No material question remains.

The assembled plan retains the settled actor/want/benefit, problem, scope and all general/data/service/solution architecture tables. Its seven criteria cover bounded ownership (AC-001), the main interaction path (AC-002, the sole Auto criterion), policy and shared approvals (AC-003), availability (AC-004), compatibility/recovery (AC-005), retirement (AC-006) and operator guidance (AC-007). Validate or the owner judges every Manual criterion; worker tests only supply evidence.

Task order: T-01 safety net → T-02 core operations → T-03 shared approvals → T-04 lifecycle cutover → T-05 main-path/behavior evidence and T-06 boundary evidence → T-07 compatibility/full checks → T-08 process rehearsal → T-09 documentation. Every task names an authorized owner and its criterion links in the story. Replacement and retirement happen together; each cut must remain usable.

Proof is planned, not executed. Baseline/current/baseline fixtures and process rehearsal do not certify all historical installations or live providers. Delivery retains saved bytes, ordering and bounded nonce handling; indefinite duplicate suppression is not promised. This proposal changes no code and establishes neither acceptance nor publication. The owner reviews this exact assembled proposal through the factory gate; after final Plan approval, use `/implement <instruction>`.


Story criteria (future proof):


AC-001 — You can change Discord interaction handling within the existing deployment while factory decisions and persistence authority remain behind bounded operations. (Manual)

Validate reviewer: review the classified Research F-02–F-08 inventory against the final component, all consumer adapters, trusted hosts and compound storage operations; confirm lifecycle ownership, core policy ownership, permitted value aliases and unchanged transaction fences. Covers: O-01, O-03, O-04, O-05, O-08.


AC-002 — You can continue an authorized conversation, confirm a permitted action and receive its saved reply through the extracted Discord component. (Auto)

Story-worker end-to-end test with controlled Discord/model/provider adapters drives signed intake through the real extracted composition, conversation persistence, core confirmation and saved delivery; asserts current authority, protected presentation, recorded outcome and ordered saved-byte replay after restart without another model call or repeating a saved completed effect. Covers: O-01, O-06, O-09, O-10.


AC-003 — You retain the same permissions, research answers, feature confirmations, retained ticket routing and shared approvals with the remote consumer enabled or disabled. (Manual)

Validate reviewer: compare the classified behavior inventory and before/after characterization evidence for research claim/reconciliation, feature confirmation and uncertain creation recovery, routing revocation around reads and before effects, redaction, commands and one shared approval coordinator/runner; confirm transport receipt grants no authority and saved outcomes are reused within recorded provider limits. Covers: O-04, O-06, O-09, O-10.


AC-004 — You can keep core ticket work running while Discord is unavailable and recover Discord through the existing credential and connection behavior. (Manual)

Owner check: start with Discord disabled or its optional token unavailable, continue ticket work and see required Discord approvals remain pending or unavailable; restore credentials and observe recovery, reconnect/catch-up and status, and confirm malformed enabled settings still refuse startup. Validate reviewer also reviews controlled-provider refusal, throttling, polling fallback and prescribed affected-package/full-check race evidence. Covers: O-03, O-09, O-13.


AC-005 — You retain saved conversations, captures, unfinished turns, approvals and partially delivered replies across upgrade, stop/drain/restart and rollback to compatible previous code. (Manual)

Validate reviewer: review extraction-specific baseline/current/baseline reopen evidence and controlled process stop/drain/restart records for all named pending state, unchanged identities/encodings, joined workers, reconciliation and reuse of saved outcomes without fresh execution; record historical-state and provider limits. Owner check: follow the documented stop/drain/restart and compatible rollback procedure and see pending work resume without deleting state. Covers: O-11, O-12.


AC-006 — You run one active Discord path after each usable cutover, with replaced daemon loops and direct chat policy paths retired. (Manual)

Validate reviewer: inspect the final ownership and retirement map plus cutover evidence for a single transport lifecycle, policy path and approval runner; confirm remaining daemon code only composes dependencies or performs primitive effects, and any compatibility bridge has an owner and removal condition. Covers: O-02, O-07, O-08.


AC-007 — You can operate and recover the component using documented ownership, unchanged settings and the stated compatibility and delivery limits. (Manual)

Validate reviewer: read operator documentation for consistency with delivered composition, status, missing-token recovery versus invalid-configuration refusal, recovery, rollback and unchanged commands/configuration; check references resolve and the tested upgrade scope and bounded provider deduplication are explicit. Covers: O-02, O-14.


Story tasks:


T-01 — Kind: code — Establish the refactor safety net before replacing behavior. Map Research F-02–F-08 responsibilities and K-01–K-08 to existing tests; add missing characterization for research wait/claim/publication recovery, separate-message feature confirmation and uncertain marker recovery, retained routing revocation around provider I/O, and shared approvals with remote on/off. Preserve caller messages, redaction, instruction capture, model selection and atomic fences; retain a before/after evidence map. These tests support later Validate review; they do not discharge Manual criteria.

Owner: unit-test-engineer. Criteria: AC-001, AC-003, AC-006. Depends on: none.


T-02 — Kind: code — Move research-answer, feature-creation/recovery and retained-inbox policy to focused factory operations and neutral contracts; adapt Discord callers, retain bounded results and primitive effect hosts, remove replaced direct policy bodies in each usable cut. Keep authority and connection checks, protected bytes, exact-body/marker reconciliation, session locks and compound transactions unchanged. Review standards/analysis/diagnosis/publication ownership against the classified inventory.

Owner: implementation-engineer. Criteria: AC-001, AC-003, AC-006. Depends on: T-01.


T-03 — Kind: code — Compose the one core owner-action coordinator and recovery runner independently of Discord readiness and remote enablement. Reuse bounded consumer actions, install Discord delivery/reaction capabilities when available, and preserve current principal/channel/prompt/generation validation. Remove replaced composition and policy fallbacks in the same usable cut; late credentials must attach to the existing runtime.

Owner: implementation-engineer. Criteria: AC-001, AC-003, AC-004, AC-006. Depends on: T-02.


T-04 — Kind: code — Introduce internal/discordcomponent using existing messaging, Discord adapter and narrow storage port. Move credential retry/redaction, validation, registration, signed handler, gateway/resume/cursor/poll intake and independent conversation/delivery workers into its joined lifecycle. Mount the existing endpoint through daemon composition; preserve unavailable responses, status, wake and config defaults. Stop admission, cancel/join workers before resource release; remove replaced daemon loops in the same usable cut.

Owner: implementation-engineer. Criteria: AC-001, AC-002, AC-004, AC-005, AC-006. Depends on: T-03.


T-05 — Kind: code — Write the story main-path end-to-end test through the extracted composition for AC-002. Adapt the existing gateway/conversation/presentation/authorization/owner-action/delivery suites to that boundary, covering reconnect and polling, deferred credentials, permanent refusal, Retry-After, uncertainty, saved-byte order, nonces, authority changes and remote on/off without stronger provider guarantees.

Owner: implementation-engineer. Criteria: AC-002, AC-003, AC-004. Depends on: T-04.


T-06 — Kind: code — Extend dependency guards across the component and all consumer adapters; inspect trusted hosts, ports, shared approval wiring and compound methods against the inventory. Prohibit general DB/SQL/daemon/workflow/transaction/coordinator capabilities, factory imports of messaging/component and duplicated policy/loops. Record permitted value aliases and compatibility bridges with owner/removal condition; verify each cut stayed usable.

Owner: unit-test-engineer. Criteria: AC-001, AC-003, AC-006. Depends on: T-04.


T-07 — Kind: code — Add extraction-specific baseline-written fixtures and baseline/current/baseline reopen/recovery checks covering conversations, immutable captures, native sessions, unfinished turns, pending approvals and partial acknowledgments. Check unchanged schema/bytes/identities, transactional fences and reconciliation of saved outcomes; retain baseline provenance and explicit coverage limits. Run affected-package verification and make check including its prescribed race coverage after the final code changes, with core ticket workflows exercised while Discord is absent.

Owner: integration-test-engineer. Criteria: AC-003, AC-004, AC-005. Depends on: T-05, T-06.


T-08 — Kind: config/operations — Prepare and run a controlled process rehearsal using isolated state and controlled providers: start pending work, stop new admission, drain/cancel and join workers, restart reconciliation, then stop/drain and reopen with compatible previous code. Record pending-state comparisons and effect observations, without live credentials or claiming all historical/provider cases. Supply an owner walkthrough for the separate Manual check; owner acceptance remains with Validate/owner.

Owner: integration-test-engineer. Criteria: AC-004, AC-005. Depends on: T-07.


T-09 — Kind: markdown — Update Discord, factory-core, conversation-evidence and relevant configuration documentation with final ownership and retirement map, unchanged operator commands/settings, unavailable-token versus malformed-config behavior, status, recovery and compatible rollback. State the fixture/process coverage limits and bounded nonce/history/uncertain-effect guarantees. Review text consistency and resolving references; no language tests or wording-pinning checks. Supply review material for AC-007 rather than declaring it satisfied.

Owner: implementation-engineer. Criteria: AC-005, AC-006, AC-007. Depends on: T-08.
