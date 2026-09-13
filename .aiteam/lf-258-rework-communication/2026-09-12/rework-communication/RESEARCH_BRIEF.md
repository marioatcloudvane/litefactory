# Agent–litefactory communication inventory

Status: Complete through run 0005 — recommended three-issue delivery split assessed; Research only.

## Run 0002 clarification

The follow-up says only “Check this” and includes no attachment, link, target or specific concern. It could request verification of the previous communication inventory against the code, or refer to an omitted item. These interpretations change the investigation scope, so no new source investigation has begun. The prior findings below are preserved and have not been reverified in this run.

Historical run 0002 question (resolved by run 0003): Should I recheck the communication inventory above against the code, or did you mean to include a specific item to review? Run 0003 supplies concrete questions about legacy retirement, standardization and MCP; no further clarification is needed.

Issue: #258, `rework-communication`. Inspected revision: `3e55019712df03bcae8176a3c774855e66013861`, on 2026-09-12 in the supplied local worktree. All source references below belong to that revision.

## Request and scope

Inventory the mechanisms agents currently use to communicate with litefactory, then list the concrete paths by mode and explain how data returns. This is a source-based inventory, not a redesign or implementation. “Path” means a communication contract, not each Go return statement, retry, error branch or individual payload field.

The original run 0001 request was sufficiently clear to proceed. Include both coding-agent adapters, persisted compatibility contracts, and factory advisory invocations. Distinguish agent–factory communication from internal specialist delegation and from the factory's subsequent GitHub reporting. No owner decision was needed to finish that inventory; the run 0002 follow-up needs the clarification above.

## Mechanisms overview

The main return mechanisms are **final messages over subprocess stdout, shared files, and a live CLI helper over a Unix-domain socket**. Plain text and JSON are payload formats; stdio and the socket are transports. A JSON final message therefore uses the same transport as a prose final message.

| Mechanism | Direction and purpose | Current implementation |
|---|---|---|
| Plain-text prompt on stdin | Factory → coding agent: request, selected mode, history, frozen scope, instructions and supplied paths | Both Claude and Codex receive the prompt through process stdin. Session/model/tool settings travel in argv; selected roles/settings can also come from files. `internal/harness/runner.go:699`, `internal/codex/runner.go:300` |
| JSON event stream on stdout | Agent runtime → factory: final message, completion/failure, session identity and usage events | Claude `--output-format stream-json`; Codex `exec --json`. The final message inside those events may be prose or strict application JSON. `internal/harness/runner.go:594`, `internal/harness/stream.go:200`, `internal/codex/runner.go:399`, `internal/codex/runner.go:528` |
| CLI call with JSON stdin/stdout, backed by a local socket | Agent → factory → agent during a run: validate input, submit a contribution, retrieve its receipt | `litefactory submission preflight`, `submit`, `receipt` with a factory-supplied absolute descriptor. The helper uses authenticated, length-prefixed JSON over a Unix-domain socket; replies return on helper stdout. This is a custom protocol, not MCP or JSON-RPC. `internal/cli/cmd_submission.go:13`, `internal/submission/client.go:19`, `internal/submission/client.go:67`, `internal/submission/protocol.go:16` |
| Shared filesystem | Agent → factory: documents, worktree changes and compatibility JSON handoffs; factory → agent: context/configuration files | Some files carry machine-consumed workflow data, others preserve work or are linked in reports. Typed Plan also creates internal transport files, but the factory writes those after the socket submission. `internal/turn/plan_handoff.go:129`, `internal/turn/typed_plan.go:165`, `internal/turn/story_progress.go:232`, `internal/turn/report.go:824` |
| Runtime transcript/rollout files | Agent runtime → factory: supplementary usage accounting, including child invocations | Claude project transcripts and Codex session rollouts supplement stdout accounting. These are telemetry sources, not agent-authored task completion contracts. `internal/harness/usage_transcripts.go:68`, `internal/codex/usage_rollout.go:22`, `internal/codex/usage_rollout.go:407` |
| stderr and process termination | Agent process → factory: diagnostics and run outcome; factory → process: cancellation | Runners capture stderr and combine exit status, terminal events and cancellation/timeout evidence. An exit status alone is not a successful structured workflow handoff. `internal/agent/result.go:10`, `internal/codex/runner.go:358`, `internal/harness/runner.go:717` |
| Issue comment round trip | Agent → factory → owner, then owner → factory → next run | Questions are ordinary final-message text. The factory posts the report; the owner's reply starts a subsequent run. This is asynchronous, not an interactive agent callback. `internal/harness/prompt.go:657`, `internal/turn/ask.go:109`, `internal/report/run.go:869` |
| Hooks / MCP | No factory-owned task-result protocol identified in the inspected wiring | Claude explicitly uses strict MCP configuration with no MCP server supplied. Repository hooks can still execute in the ordinary Claude profile; this is documented ambient behavior, not a wired litefactory result receiver. Delivered-team runs additionally disable other settings sources. Do not infer that every possible operator-configured hook or Codex MCP server is absent. `internal/harness/runner.go:572`, `internal/harness/runner.go:623`, `internal/harness/doc.go:49`, `internal/codex/runner.go:403` |

## Concrete paths by mode

“All modes” means Research, Plan, Implement, Validate and Spike. The last three advisory operations are outside that mode vocabulary. Content/submission version and workflow version are separate: new managed features select content/submission v2, while retained generations may still use v1. Workflow v2 separates Implement engineering from Validate acceptance; workflow v1 combines them. Sources: `internal/turn/plan_handoff.go:22`, `internal/store/submission_contracts.go:42`, `internal/turn/submissions.go:98`.

| Path | Mode / condition | How data is returned | Factory consumption / timing |
|---|---|---|---|
| Normal final report, Claude | All modes | Plain text in the terminal stdout JSON event's `result` field | Adapter captures the last result event as `RunResult.ResultText`; report renderer includes sanitized text after the run. Structured consumers below may parse or replace it. `internal/harness/stream.go:200`, `internal/agent/result.go:119`, `internal/report/run.go:869` |
| Normal final report, Codex | All modes | Text from the last completed `agent_message` item on stdout; completion separately comes from `turn.completed` | Adapter normalizes both into `RunResult`. Intermediate agent messages are not separate authoritative task results. `internal/codex/runner.go:337`, `internal/codex/runner.go:528` |
| Research findings and saved reference | Research | Final prose report plus `$session/RESEARCH_BRIEF.md` and `MANIFEST.md` | The skill requires saved evidence; factory reporting discovers the brief for a link. The inspected report boundary does not parse its prose into a typed research decision. `teams/source/agents/shared/research-orchestrator.md:14`, `teams/source/skills/research-evidence/SKILL.md:17`, `internal/turn/report.go:824` |
| Clarification questions | Research / Plan when questions are permitted; legacy tagged infer also supported | Questions in the final prose report; unresolved findings/context saved before exit | Factory posts the report; owner reply continues through a new run. There is no wait-for-answer callback in this contract. `internal/harness/prompt.go:657`, `internal/turn/ask.go:109` |
| Typed Plan discussion or completion | Plan, content/submission v2; default for new managed features | JSON contribution through `submission preflight/submit`; JSON diagnostics/receipt returned synchronously; `receipt` recovers correlation | Factory binds identities and writes outcome plus proposal or `PLAN_CONTEXT.json` internally. A receipt records input; publication/readiness still depend on terminal success and save. `internal/turn/typed_plan.go:133`, `internal/turn/typed_plan.go:196`, `internal/turn/plan_handoff.go:37` |
| Compatibility Plan discussion | Plan, content/submission v1 | Agent writes root `.litefactory-plan-outcome.json` with `discussion` and `$session/PLAN_CONTEXT.json`; final prose carries questions | Factory reads and validates files after successful process completion; no proposal may coexist. `internal/harness/prompt.go:401`, `internal/turn/plan_handoff.go:129` |
| Compatibility Plan completion | Plan, content/submission v1 | Agent writes root `.litefactory-plan-outcome.json` with `completed`, `.litefactory-plan-proposal.json`, and session `plan_brief.md` | Factory parses the complete proposal after terminal success, publishes stories and updates the index. Reserved JSON files are transient. `internal/harness/prompt.go:405`, `internal/turn/plan_handoff.go:168` |
| Plan shared document and Story Index | Plan, both contracts | Agent writes `plan_brief.md` containing shared context and `## Story Index`; factory later replaces the index | The Markdown file is still read and checked even with live typed submissions; story publication uses the explicit proposal, not per-story Markdown discovery. `internal/turn/typed_plan.go:196`, `internal/turn/plan_index.go:45` |
| Live engineering / legacy verification contribution | Implement, submission v2, selected unfinished stories | JSON through the same helper/socket. Engineering sends snapshot/check evidence or blocked/invalidated outcomes; workflow v1 uses the legacy combined verification stage | Preflight returns diagnostics; submit returns durable input/engineering/acceptance/remote-effect states during the run. Pending remote effects are not completion. `internal/turn/submission_runner.go:73`, `internal/turn/submissions.go:98`, `internal/turn/submissions.go:200` |
| File-based incremental story checkpoints | Implement; advertised when submission version is not 2 | Atomic consecutive `.litefactory-story-events/<run_id>/<sequence>.json` files; evidence includes snapshots and executed checks | Factory polls while runs are active and drains on finish. Workflow v2 uses `engineering_complete`; workflow v1 uses `verified`; blocked/invalidation events also exist. The reader remains in current code and is not guarded by submission version at its entry; v2 suppresses the prompt instructions, so do not call the filesystem reader physically removed. `internal/harness/stories.go:41`, `internal/agent/story_progress.go:243`, `internal/turn/story_progress.go:173`, `internal/turn/story_progress.go:232` |
| Retained typed assessment | Validate, submission v2 | **Final-channel JSON** `{"contributions":[...]}` over the normal runner stdout stream; each entry has validation findings | Read-only reviewer does not invoke the helper. After exit, factory validates the collection and submits it internally, retaining receipts. This is explicitly not live helper acknowledgment. `internal/turn/submission_runner.go:78`, `internal/turn/submission_runner.go:95` |
| Compatibility retained assessment | Managed Validate without the typed terminal path | **Final-channel JSON** version-1 assessment containing parent, generation and stories with criterion/review findings | Factory parses `ResultText` and retains acceptance results after successful process exit. No result file or fresh checks. Plain prose is rejected here. `internal/harness/validation.go:26`, `internal/turn/validation.go:89` |
| Work products and aggregate report | Implement / Spike; research and planning documents use the same filesystem mechanism | Uncommitted worktree changes plus final prose describing executed checks/results; Spike additionally writes `$session/SPIKE.md` | Factory owns saving and reporting. Aggregate report is separate from per-story evidence; it does not itself replace typed/checkpoint acceptance requirements. `internal/harness/stories.go:36`, `internal/turn/harvest.go:433`, `docs/workflow/spike.md:14` |
| Usage and diagnostic observations | All ordinary runs, when accounting enabled | stdout events, runtime transcript/rollout records, stderr and process status | Normalized by adapters for logs, usage accounting and run status; no task-payload schema or owner acknowledgment. `internal/harness/runner.go:626`, `internal/harness/usage_transcripts.go:68`, `internal/codex/usage_rollout.go:22`, `internal/agent/result.go:149` |
| Mode classification | Before work-mode dispatch; not itself Research/Plan/etc. | One exact mode word inside normal stdout result events | Stateless, tool-free adapter invocation; parser accepts only the closed mode vocabulary. `internal/harness/runner.go:27`, `internal/codex/runner.go:107` |
| Schedule advice | Scheduler advisory invocation; no ordinary work mode | Structured JSON proposal in final message inside stdout events | Stateless/tool-free call parsed into `ScheduleProposal`; no ordinary run/worktree handoff. `internal/harness/runner.go:74`, `internal/codex/runner.go:151`, `internal/daemon/proposal_adapter.go:31` |
| Conflict-resolution advice | Conflict advisory invocation, associated with synchronization; no separate work mode | Structured JSON region-resolution proposal in final message inside stdout events | Stateless/tool-free bounded call; factory parses against the supplied conflict request. `internal/harness/runner.go:124`, `internal/codex/runner.go:201`, `internal/turn/conflict.go:247` |

## Boundary distinctions and retained code

- **Factory → specialists:** selected team roles can be delivered through Claude plugin/bootstrap files or Codex role TOML plus prompt text. Native agent delegation and specialist messages are within the coding-agent runtime; they are not additional independently wired factory submission channels. Their artifacts can reach the factory through the paths above, and their usage can be observed. Sources: `internal/harness/runner.go:623`, `internal/codex/runner.go:412`, `internal/render/native.go:57`, `internal/turn/prepare.go:415`.
- **Old Markdown story discovery is not a live production path.** `fileStoryIssues` still contains a `plan_brief.md` → per-story `US-*.md` reader, but current production wiring supplies `PlanForge`, which exits before that reader. It remains reachable with test substitutions. Do not count this as another current production return mechanism. Source: `internal/turn/subissues.go:113`.
- **GitHub is downstream of the agent boundary.** The factory renders reports and projects accepted workflow decisions; agents do not need to post their results to GitHub themselves. The helper receipt distinguishes retained decisions from remote effects. Sources: `internal/report/run.go:869`, `internal/turn/submission_runner.go:73`, `internal/turn/validation.go:124`.
- **Resume is not another transport.** Claude changes session flags; Codex invokes `resume <id> -`. Input still uses stdin and output the same event parser. Sources: `internal/harness/runner.go:584`, `internal/codex/runner.go:444`.

## Evidence quality, risks and next step

These are observations from current code, focused callers and test definitions in the supplied worktree. No external service, installed agent binary or running daemon was probed, so this is not a claim about which compatibility contracts exist in the operator's current database, nor about every ambient operator configuration. No tests were executed and no source code was changed.

Focused test evidence inspected includes `internal/turn/submissions_test.go:259` (synthetic Claude/Codex paths receiving a live decision), `internal/turn/typed_plan_test.go:129` (typed Plan artifacts and pending readiness), and `internal/turn/validation_test.go:137` (process success with unstructured Validate output rejected). Synthetic runner coverage is not a live-binary interoperability measurement.

The key risk for a communication rework is conflating transport, payload shape and authority: a final prose message, a JSON validation assessment, an engineering receipt and successful remote publication are different contracts even when some share stdout. Persisted v1 generations and the Validate no-shell restriction materially affect migration choices.

Options for the next phase are to standardize only structured workflow contributions or to redesign the entire boundary, including ordinary reports, questions, advisory invocations and telemetry. Recommendation: use these tables as the starting inventory and decide that scope in Plan; preserve explicit version compatibility and Validate's read-only boundary. No redesign option is selected or implemented here.

Open decisions for the original run 0001 Research request: none. The future redesign scope is a subsequent planning decision, not a blocker to that inventory. Run 0002 was incomplete at that time; run 0003 resolves its scope below.

## History

- 2026-09-12, run 0001: Completed the two-level communication inventory with production/compatibility distinctions and source references; no implementation or mode transition.
- 2026-09-12, run 0002: Preserved the completed inventory and recorded the ambiguous “Check this” follow-up as incomplete pending clarification. No new source investigation, source changes or tests.


## Run 0003 — legacy retirement, standardization and MCP

Status: Complete. The owner's concrete questions resolve run 0002's ambiguous follow-up. This is an assessment and recommendation, not authorization to implement or change modes. Inspected revision: `64561b62ba20040c0b6ee823cc51e9f96c00c14c`, 2026-09-12. References in this section belong to this revision; the original inventory above retains its original revision.

### Findings: legacy is present and partly live

**Yes: legacy protocols remain. Some can be cleaned up independently; the active compatibility contracts require a deliberate retirement boundary.** New managed features already choose content/submission v2 and separated workflow v2 (`internal/turn/plan_handoff.go:22`). Existing persisted generations keep their contract. Migration 0028 defaults existing feature contracts to v1 (`internal/store/migrations/0028_submission_contracts.sql:2`); generation bindings are immutable (`internal/store/submission_contracts.go:38`). Therefore new-feature defaults do not prove that old support is unused.

| Legacy path | Current reachability / gating | Retirement assessment |
|---|---|---|
| Agent-written Plan outcome/proposal JSON and discussion context files | Existing content/submission v1 generations; no typed broker when generation contract is absent or submission version is not 2. `internal/turn/submissions.go:76`, `internal/turn/plan_handoff.go:129`, `internal/harness/prompt.go:401` | Remove v1 agent instructions/parsers after old active scopes are completed or explicitly upgraded. Do not delete all Plan handoff files: v2 still writes internal handoffs and shares publication/document processing. |
| `.litefactory-story-events/<run>/<sequence>.json` incremental input | Instructions suppressed for submission v2 (`internal/harness/stories.go:41`), but registration and ingestion are not gated there by submission version (`internal/turn/harvest.go:48`, `internal/turn/story_progress.go:173`, `internal/turn/story_progress.go:232`). | Gate filesystem ingestion to the intended old contract first, then retire the reader after its old streams and pending effects drain. This is live compatibility code, not a dead reader. |
| Version-1 Validate final assessment JSON | Fallback harvest runs for managed validation unless typed terminal validation already succeeded. `internal/turn/validation.go:89`, `internal/turn/submission_runner.go:83` | Retire the old output schema/parser with old submission contracts. Keep final-output transport available: current v2 Validate also uses it because shell helpers are forbidden. |
| Legacy combined verification/acceptance workflow | `WorkflowLegacy = 1` is separate from content/submission version. A v2 broker uses stage `legacy` when workflow is 1. `internal/store/managed_stories.go:11`, `internal/turn/submissions.go:98` | Cannot be removed merely by upgrading transport/content. Retiring combined acceptance is a separate workflow migration or finish-old-work decision. |
| Markdown `plan_brief.md` / `US-*.md` discovery and implicit subissue creation | Explicitly production-unreachable: production `SubIssueCreator` satisfies `PlanForge`, and the phase returns before discovery. Test doubles can reach it. `internal/turn/subissues.go:113` | Best immediate cleanup candidate. Remove dead phase and tests/interfaces exclusively supporting it, after checking callers. Preserve shared helpers: `locateSessionDir` is used by live `internal/turn/plan_handoff.go:181`. |

Do not confuse deleting an input adapter with deleting the underlying workflow machinery. V2 `recordEngineering` creates the existing story event/effect records (`internal/turn/submission_records.go:14`, `internal/turn/submission_records.go:82`; `internal/store/submission_progress.go:12`). `StoryProgress` also owns recovery and remote delivery (`internal/turn/submission_recovery.go:12`, `internal/turn/story_progress.go:211`). Deleting the whole service would break current v2 operation.

### Existing upgrade route and practical retirement order

Fact: the owner command `litefactory story-contract upgrade <parent-ref> --preview` exists; preview is the default, and explicit `--apply` publishes conversion (`internal/cli/cmd_story_contract.go:25`; `docs/story-contract.md:123`). It upgrades the entire unfinished cohort using exact issue revisions. Safety checks require planning state, content/submission v1, no active parent/story attempt, and no pending legacy effects (`internal/store/story_contract_upgrade.go:74`). Conservative conversion rejects ambiguous legacy source instead of guessing; completed children are excluded from conversion (`internal/turn/story_contract_upgrade.go:119`). Partial publication has retained resumable intent (`internal/turn/story_contract_upgrade_test.go:195`).

Fact: completion selects a new v2 generation, preserves historical generations and the workflow version, and leaves readiness false (`internal/store/story_contract_upgrade.go:195`). The next Plan must be published and approved. It is not an in-place version flip, nor a route around frozen-scope restrictions. Features with no unfinished cohort cannot use this conversion (`internal/turn/story_contract_upgrade.go:137`).

Recommendation:

1. Remove the production-unreachable Markdown publication phase as an independent cleanup, retaining its shared live helpers.
2. Establish an explicit supported-upgrade boundary: inventory unfinished v1 scopes, active/recoverable attempts and undelivered effects through supported operator/store tooling. Complete frozen old work, or move it through authorized existing planning admission and preview/apply conversion; resolve conversion conflicts before retrying. Do not rewrite persisted versions by hand.
3. Once no supported execution/recovery can require v1 input, remove v1 prompt contracts, filesystem ingress, output parsers and their exclusive wiring. Update canonical team instructions, rendered bundles, docs and tests together. Preserve common v2 handoff/publication/effect code and historical reads/migrations needed to open and explain existing databases.
4. Treat combined workflow v1 retirement separately. The existing content upgrade intentionally leaves it unchanged; either finish those workflows or design an explicit workflow migration.

Inference: if the operator accepts a release that refuses unfinished old scopes until migration, retirement can be more direct, but that is an intentional compatibility policy requiring a clear upgrade error/path. This investigation does not establish that the deployed database is already free of legacy work. No daemon runtime state was inspected or changed. The recommendation is feasible in code; immediate production-safe deletion of every legacy branch is not established by repository evidence alone.

### What standardization buys

**The useful standard is a shared application contract with explicit meaning, not forcing every byte through one transport.** A substantial foundation already exists: the custom transport has method/request ID/payload and correlated replies (`internal/submission/protocol.go:16`); contributions have stage/outcome and stage-specific content (`internal/agent/contribution.go:23`); the factory supplies trusted run/generation/revision/attempt identities, and receipts separate validation, engineering, acceptance and remote effects (`internal/turn/submission_runner.go:73`, `internal/turn/submissions.go:225`). Standardization should extend this existing boundary, not discard its guarantees.

| Proposed improvement | What it would buy | What it would not establish |
|---|---|---|
| One validated, versioned application event family with typed payloads | Common parsing, diagnostics and adapter tests across modes/runners; fewer incompatible handoffs and prompts | One universal payload for reports, progress, evidence and acceptance |
| Factory-bound identity plus correlation and explicit retry rules | Reliable attribution, duplicate handling, traceability and recovery across helper/MCP/final-output adapters | The agent supplying authoritative run IDs or revision claims |
| Explicit distinction between progress, questions, artifacts, results and workflow contributions | UI/reporting can display partial activity and requested input without inferring it from prose | A progress claim proving tests, readiness or acceptance |
| Separate retained input, decided outcome and delivery status | Accurate operator visibility when recording succeeds but GitHub publication remains pending | Exactly-once remote effects merely because JSON was accepted |
| Shared machine-readable question/result fields alongside narrative | More reliable continuation/status rendering; final prose remains readable | A new interactive wait-for-answer contract without product/workflow changes |

Recommended initial scope: keep the existing v2 contribution/receipt semantics, add an explicit advisory progress payload and normalized final outcome/question metadata where needed, and share validation across transport adapters. Factory envelope metadata belongs to the receiving boundary; agents author the payload and approved selectors. Files remain suitable for code and larger artifacts, final stdout for a read-only terminal handoff, and runtime stdout/transcripts for vendor telemetry. Avoid turning every artifact or token-usage event into a workflow submission.

A possible *conceptual* progress payload is `kind: progress`, a short message, optional authorized story selector and phase. The factory adds run/attempt identity, receipt time and ordering/deduplication metadata. This is a recommendation, not a finished schema. Keep progress advisory: no issue closure, acceptance, fabricated percentage, or run-success override. Rate-limit/coalesce display updates; define retention and stale-after-exit behavior. Silence alone does not prove a hung process. Owner replies should retain today's next-run semantics unless explicitly redesigned.

### During-run updates and MCP

Fact: current helper methods are only `preflight`, `submit` and `receipt`; all non-receipt input goes through contribution validation (`internal/turn/submissions.go:187`). `Contribution` has no general run-status message (`internal/agent/contribution.go:23`). Existing story progress means durable engineering/verification/invalidation and its delivery, not arbitrary “investigating X” telemetry. Also, broker startup currently requires a managed Plan/story/validation scope (`internal/turn/submissions.go:65`); supporting Research and Spike status requires extending run-scoped capability lifecycle, not only adding a new method name.

| Option | Benefit | Cost / fit |
|---|---|---|
| Add a progress operation to the current helper/service | Smallest extension for run status; existing local authentication, attempt lifetime and runner integration can be reused | Still requires shell helper access and run-scoped support outside managed submissions; no automatic tool discovery |
| Add a thin MCP adapter over the same application service | Discoverable tools and input/output schemas; structured correction feedback; could expose narrowly permitted status tools without general shell access | Requires explicit per-run MCP configuration, capability binding, host permissions, adapter/lifecycle tests and actual Claude/Codex compatibility validation |
| Replace the broker and all output paths with MCP | One advertised tool interface | Largest change; still needs application durability, receipts, authority and artifact handling; cannot safely erase read-only final-output support by assumption |

Recommendation: **yes to standardized application messages and explicit during-run progress; MCP is useful as an optional thin adapter if native tool discovery or restricted-shell access is a product goal. Do not make MCP a prerequisite for status updates alone.** Keep one underlying validation/receipt service, avoid maintaining independent MCP and CLI business rules, and initially keep existing final-output handling for Validate. An MCP tool can be designed to preserve read-only review while permitting telemetry, but that is a new explicitly scoped profile, not something currently enabled.

Repository integration facts: Claude uses strict MCP configuration and supplies no server (`internal/harness/runner.go:572`, `internal/harness/runner.go:629`). Codex Validate explicitly disables shell and multi-agent features (`internal/codex/runner.go:408`); typed Validate is currently instructed to return final JSON rather than invoke shell helpers (`internal/turn/submission_runner.go:78`). Neither fact proves a new MCP adapter is impossible; both require deliberate integration rather than ambient configuration assumptions.

External evidence was checked against the pinned **2025-11-25 MCP specification**, not asserted to be the latest revision or the version supported by installed hosts:

- MCP tools provide discovery, input schemas, optional output schemas and structured results. These could make factory calls easier for an agent to discover and correct. They do not define litefactory's durable acceptance/remote-effect semantics. [MCP tools](https://modelcontextprotocol.io/specification/2025-11-25/server/tools)
- MCP progress notifications belong to an active request with a progress token; sending them is optional and they end with the request (with task-related lifetime provisions). **They are not a generic unsolicited agent-run status feed.** With the runtime as client and litefactory as server, reporting overall agent work is naturally an agent call such as `status_update` (design inference), rather than assuming server progress notifications run in that direction. [MCP progress](https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/progress)
- Standard MCP transports are JSON-RPC over stdio or Streamable HTTP. A stdio MCP server is a child of the MCP client; that is a separate stream from the existing coding-runner event stdout. A thin local stdio server could bridge to the existing service; “both use stdio” does not make existing runtime events MCP. [MCP transports](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports)

### Evidence limits and next step

Focused test definitions read: `internal/turn/aggregate_lifecycle_test.go:79` (both workflow versions), `internal/turn/aggregate_lifecycle_test.go:517` (v1 skips typed broker), `internal/turn/submissions_test.go:201` (legacy decision recovery), `internal/store/story_contract_upgrade_test.go:50` and `:138` (history/admission/safety), `internal/turn/story_contract_upgrade_test.go:112`, `:195`, `:253`, `:299` (preview/apply, restart, completed legacy dependencies and frozen generation dispatch). These corroborate intended behavior; no tests or installed-host probes were run.

No source code changed, no upgrade applied, no daemon state inspected and no automatic mode transition. Research answers are complete. Future Plan decisions are the supported legacy upgrade boundary and whether native MCP tool access is needed beyond status visibility; neither blocks this assessment.

- 2026-09-12, run 0003: Resolved run 0002 clarification through concrete follow-up; assessed live/dead legacy paths, supported migration prerequisites, shared-code removal boundaries, standardization benefits and MCP fit using repository evidence and pinned primary specification sources.


## Run 0004 — compact result contracts, mutable items and deferred serialization

Status: Complete. Date: 2026-09-13. Inspected revision: `fd09d62440bd730fabc2a35f7fb85f94636d0f69`. This section explores the owner's proposed direction without implementing a schema, creating a skill or advancing to Plan. Prior sections retain their historical revisions.

### Recommendation and concrete shape

The proposed `meta` / `result` split is a useful direction. Recommend a factory-owned envelope, mode-specific result payloads, and sparse updates to an authoritative retained result. The largest likely token saving is avoiding repeated full story output and unnecessary schema injection; JSON by itself is not a measured token improvement.

Illustrative **factory-stored result** (not a finalized executable schema):

```json
{
  "meta": {
    "schema_version": 1,
    "run_id": 25804,
    "mode": "plan",
    "revision": "r17"
  },
  "result": {
    "outcome": "completed",
    "comment": "Updated the communication plan.",
    "plan": {
      "summary": "Standardize factory communication.",
      "stories": {
        "US-001": {
          "title": "Return structured results"
        }
      }
    }
  }
}
```

The story is abbreviated solely to show nesting. Existing required story fields—user story, criteria, tasks, architecture and dependencies—still belong to the retained full snapshot. Prefer the existing term `stories`; GitHub subissues are the factory's projection of those stories. `result.plan` is present only when appropriate to Plan; other modes have their own schema branch rather than a collection of unrelated optional fields. A completed Plan requires complete valid Plan content; a discussion requires context/questions instead. `outcome` is the agent's reported disposition, not an authoritative claim of publication, readiness or acceptance. Receipt dimensions remain separate.

`meta` is supplied and checked by litefactory. Agents should not regenerate run ID, mode, issue identity, generation or authority. At most they echo an opaque expected revision for concurrency. `schema_version`, result `revision`, persisted content/submission version and workflow version are different concepts; do not reuse one integer for all four. In production, task/result scope is also bound by the authenticated attempt and durable storage identity; it need not be repeated in each agent-authored payload.

A follow-up agent could send this **update**, using a named merge-patch operation:

```json
{
  "base_revision": "r17",
  "patch": {
    "comment": "Clarified the first story title.",
    "plan": {
      "stories": {
        "US-001": {
          "title": "Return a structured result after every run"
        }
      }
    }
  }
}
```

Here `patch` applies only to the editable retained result, not to `meta`. For a new result with no baseline, the agent submits full `result` instead. Exactly one of snapshot or patch should be accepted. The current run's outcome/comment should be explicitly supplied or reset by the receiver, so a prior run's completed outcome cannot silently become the next run's claim. The returned receipt supplies the new revision; the next owner request references that retained revision rather than relying on an old conversation copy.

Distinguish the mutable Plan aggregate from immutable run history: updating a story creates a new Plan/result revision attributed to the new run. Do not overwrite the old run's report, receipt, frozen scope or acceptance evidence. A prior envelope's `run_id` is historical provenance, never authority for the new attempt.

### Update semantics

Recommendation: use standard JSON Merge Patch when schema fields do not require meaningful stored null values. State the operation explicitly; a bare sparse JSON object is ambiguous. [RFC 7396](https://www.rfc-editor.org/rfc/rfc7396) defines recursive object updates, omitted members unchanged, null members removed and arrays replaced in full.

| Concern | Proposed semantics / reason |
|---|---|
| Prior state | Factory provides an authoritative revision-bound snapshot or scoped projection, with a compact index and retrieval reference when large. Read the changed item's actual baseline before editing. |
| Item identity | Stable story keys; criteria retain AC identities. Objects keyed by identity make sparse updates smaller and less sensitive to list order. Ordered collections need a separately defined order if order is meaningful. |
| Omitted field or item | Unchanged in a patch; never an implied deletion. Initial full snapshots must include required fields. |
| Null / deletion | Under Merge Patch, null requests removal; it is not a literal null value. Required-field removal rejects. Story deletion must also pass domain rules and is not automatically permission to delete/close a GitHub issue. Initially reject story removal unless separately designed; current code preserves existing stories. |
| Arrays | Merge Patch replaces the whole array. Either accept that cost for short lists or use stable-ID maps for independently editable collections. Do not pretend array indexes are stable item IDs. |
| Nullable domain values | If literal null is needed, select explicit set/remove operations or JSON Patch instead; do not label custom null semantics RFC 7396. [RFC 6902](https://www.rfc-editor.org/rfc/rfc6902) is the explicit-operation alternative. |
| Concurrency | Compare `base_revision` to the authoritative current revision; reject a stale patch with actionable diagnostics. For simplicity start with one aggregate revision. Per-item revision checks can come later if measured contention warrants them. |
| Merge and validation | Apply to a copy, reconstruct a complete candidate and validate the full schema, dependencies, roles, criteria history, frozen/completed item restrictions, size and redaction before accepting anything. A locally valid changed field can invalidate another story. |
| Storage / retries | Retain the accepted full snapshot plus patch/provenance as useful, with current receipt/correlation guarantees. Readers and recovery should not require replaying an unbounded chain of patches. |
| Authority | Factory identities, revisions, approvals, acceptance and remote-effect state are read-only. Sparse updates must not bypass existing Plan admission or issue revision checks. |

A stable ID identifies an entity, not permanent approval for any new meaning. Changed acceptance obligations must still follow current criterion retirement/clarification rules. Small transport patches do not make semantic requirement changes presentation-only. Similarly, changing result data cannot select a different work mode.

### Existing support and gaps

Facts from the inspected revision:

- V2 already separates agent contribution values from the factory identity envelope (`internal/agent/contribution.go:23`; `internal/turn/typed_plan.go:196`). Existing issue IDs and observed title/body hashes are captured server-side (`internal/turn/typed_plan.go:73`). Reuse this authority split.
- Typed Plan currently expects complete typed content, not sparse field updates. Parsing checks object shapes and complete story graph (`internal/planbrief/typed_content.go:115`); proposal building rejects missing existing stories and changes to completed dependencies (`internal/turn/typed_plan.go:271`). A patch adapter must merge before these validators; passing the example patch directly to today's endpoint would reject it.
- Current typed projections retain exact issue revisions and typed content (`internal/store/submission_contracts.go:73`), while publication checks observed base hashes (`internal/turn/plan_publication.go:257`). These are useful foundations; an aggregate result revision and partial-update API are additional work, not existing features.
- Plan setup constructs existing story context from current GitHub titles/bodies and expected hashes (`internal/turn/plan_handoff.go:85`), and saved working context is read separately (`internal/turn/plan_attempt.go:61`). This is not currently a canonical prior-result JSON patch baseline.
- Narrow prompt-path finding: `PlanWorkingContext` and `PlanStoryContext` are rendered inside `SubmissionVersion != 2` in `internal/harness/prompt.go:425–451`; v2 callback instructions require full content but do not inject those fields (`internal/turn/typed_plan.go:196`). Therefore the new design should explicitly supply/retrieve its prior baseline rather than assuming these fields already guarantee v2 agents receive it. Prior conversation or other artifacts may still contain relevant context; this is a code-path observation, not a reproduced claim that all v2 continuation lacks context.

The exact canonical prior result should live with factory-owned state. Giving an agent a read-only snapshot or scoped fetch is preferable to having it reconstruct state from final prose or independently reread GitHub. A scoped fetch may return only touched items and required context; the server must still validate the entire merged aggregate. If baseline data changes after the agent reads it, reject/rebase rather than silently use newer unseen bytes.

### Token efficiency and the finish-time skill

The owner’s idea works well for **serialization instructions**: keep a short always-visible requirement to finalize through the contract, then load the detailed schema/examples immediately before final submission. This also applies to questions/blocked/discussion exits, not only successful completion. Loading after the process actually finishes would be too late to correct malformed output.

Official Codex skill documentation describes progressive disclosure: skill name/description/path are initially available; the full file is read when selected explicitly or implicitly. It does not promise deterministic finish-only activation or removal from context after reading. [Build skills](https://learn.chatgpt.com/docs/build-skills) (checked 2026-09-13). A skill alone is guidance, not an enforceable terminal protocol. A lifecycle validation/finalization gate is the stronger mechanism if every run must emit the contract; transport failure, cancellation and crashes still require factory-generated failure records.

Keep these available during work, before finalization:

- Actual scope, constraints, selected mode and the relevant existing items being changed.
- Stable identity/base-revision rules and required artifact/evidence capture. A finishing skill cannot reconstruct missing executed checks or recover discarded evidence truthfully.
- A small pointer to the finalization contract and the run's supported submission mechanism.

Defer exhaustive field descriptions and serialization examples. Fetch only required baseline data when needed, avoid sending both rendered Markdown and equivalent typed JSON unless each is necessary, and send patches for small changes. Do not shorten useful field names or assume minified JSON universally costs fewer tokens without measurement. JSON escaping and patch bookkeeping have overhead, and full snapshots may be cheaper for small or mostly rewritten results. A separate finalizer invocation could isolate formatting context, but adds latency, tokens and a handoff; it is not necessary for the first version and must not invent evidence.

No token counts or live model reliability measurements were performed. Future validation should compare representative current Plan full-output runs with baseline-plus-patch runs, including input, output, retries and schema reads; measure total cost and correction rate, not only final payload byte count.

### Conclusion for this research iteration

Recommend the envelope shape, factory-owned metadata, typed mode payloads, full stored snapshots and revision-bound sparse updates. Defer serialization schema to just before submission, while supplying domain constraints and edited item context when the work needs them. Start with a small shared contract and the existing validation/receipt service; neither MCP nor a separate finalizer is required to realize this design. Full schema, deletion policy and lifecycle enforcement remain subsequent design decisions, not facts implemented here.

Research complete; no source edits, tests, skill creation, daemon-state access or mode transition. Run 0004 scope was sufficiently clear; no blocking owner questions remain.


## Run 0005 — recommended three-issue delivery split

Status: Complete. Date: 2026-09-13. This recommendation uses the completed run 0003/0004 findings, including source inspected at `fd09d62440bd730fabc2a35f7fb85f94636d0f69`. No source reinvestigation or new verification was performed. The owner proposes groundwork, integration/retirement, and MCP as three issues. This endorses a direction for discussion; it does not freeze the illustrative schemas, authorize issue creation, or change Research mode.

**Recommend this split.** Keep #258 as the umbrella/research record and make the three deliverables independently reviewable. It separates shared communication meaning, production adoption, and native tool access. Strengthen the first issue beyond a Go JSON reader, and make legacy retirement conditional on the migration evidence rather than requiring the second issue to delete every old branch immediately.

| Suggested issue title | Scope | Exclusions | Dependencies / completion conditions |
|---|---|---|---|
| **Define and implement the shared agent communication contract** | Agree the envelope and mode-specific payloads, factory-owned identity, schema versioning, sparse-update/base-revision rules, progress semantics and outcome/receipt distinctions. Implement a Go decoder plus semantic validation and patch application against an authoritative baseline, reconstructing a complete validated snapshot. Reuse existing contribution, identity, evidence and receipt machinery where possible. Provide representative valid/invalid fixtures and a reusable finalization skill with concise early discovery instructions and deferred serialization details. | No live runner routing changes, production cutover, old-path deletion, or MCP server. No generic event framework beyond the actual inventory. | Can be reviewed independently through contract examples and executable service/fixture checks. Demonstrate creation, a small update, stale-revision rejection, unauthorized-field rejection, mode-specific invalid content, and progress that cannot grant completion/acceptance. Decide omitted/null/deletion and retry/version behavior explicitly. The skill instructs finalization for successful, blocked and questioning exits; it does not promise deterministic activation by itself. |
| **Adopt the shared contract across agent communication paths and retire obsolete handoffs** | Wire production runner/helper/final-output adapters to the shared service. Explicitly cover Research, Plan, Implement, Validate and Spike: final outcomes/comments, questions, artifacts and applicable workflow contributions. Supply prior canonical state when updates need it; introduce the pre-final submission/validation path and actionable rejection handling. Map advisory callers to shared version/error conventions where useful while preserving their typed payloads. Carry run-scoped progress through the existing interface if during-run status is included, using issue 1 semantics. Migrate or retire superseded input paths and update canonical instructions/docs together. | No MCP-specific business rules. Do not force binary artifacts, vendor stdout parsing, usage transcripts or diagnostics through the business-result JSON schema. Do not remove shared StoryProgress recovery/delivery, current v2 internal Plan handoffs, immutable history or live shared helpers. | Depends on issue 1. Completion requires cold/resumed/retry/failure behavior, authoritative prior-state delivery, preserved read-only Validate behavior, and equivalent workflow authority/receipt semantics through supported adapters. Each old path has an explicit disposition: removed, safely migrated, or deliberately retained with a bounded retirement condition. Use separate rollout and removal milestones. If old scopes cannot drain during this issue, explicitly revise its scope and split remaining compatibility removal into a fourth issue; do not mark promised retirement complete while it remains deferred. |
| **Expose the shared communication service through a scoped MCP interface** | Add a thin MCP adapter using the same validation, patch, status and receipt semantics. Bind permissions and lifetime to the current run/attempt, make in-flight status usable for unmanaged Research/Spike as well as managed modes, and return discoverable schemas/actionable feedback. Define a narrow permitted tool set for each profile, including Validate, plus restart/cancellation/closed-attempt behavior. | No second contract, duplicate business logic, new acceptance authority, or MCP-only progress semantics. Do not equate MCP request-progress notifications with agent-run updates or broaden Validate to general shell access. | Implementation depends on issue 1's service; rollout depends on the run lifecycle and adapters established in issue 2. Completion requires Claude/Codex host integration evidence, correct attempt isolation and expiry, allowed/denied tool behavior, and parity with existing submission semantics. An agent-facing status tool reports overall run activity; normal MCP progress notifications describe the progress of a particular active MCP request. |

### Sequencing and boundaries

Recommended delivery sequence: **1 → 2 → 3**. MCP adapter development may start once the first service is stable, but the straightforward release order gives it proven production lifecycle behavior to reuse. The first issue is useful before cutover because it produces a reviewable contract, reusable code and fixtures; the second delivers behavior without needing MCP; the third adds native tool access without reinventing semantics.

Legacy removal is the main scheduling risk. Prior evidence distinguishes production-dead Markdown discovery (an independent cleanup candidate) from live persisted content/submission v1 and separate workflow v1. Existing contract upgrade leaves the workflow version intact and requires safe planning admission, no active attempts and settled legacy effects. Therefore “retire old paths” means a verified migration/removal matrix, not deletion of every symbol named legacy. If a supported release must continue running old scopes, retain those adapters explicitly and open a focused retirement follow-up once its prerequisite is achievable. Research alone cannot establish that deployed state is ready.

Progress should be defined in issue 1 even if its first user-facing integration lands later. The payload's advisory meaning, identity, ordering/deduplication, retention and rate/coalescing policy belong to the common service. Issue 2 can supply the run-scoped lifecycle; issue 3 exposes it as tools. This avoids making MCP the only place that knows what status means.

The finalization skill and factory gate have distinct responsibilities. The skill teaches agents when/how to serialize; the factory decides whether a terminal result is valid and handles absent output after failure or interruption. Evidence capture and constraints needed during work remain available before finalization. Existing Validate's no-shell contract remains until an explicitly narrow tool profile is designed and verified.

Open contract choices belong in issue 1: final exact field names and mode variants, merge-patch versus explicit operations, optional field/null/deletion policy, result revision scope, retention, and how incomplete/questioning outcomes are represented. The owner’s proposed issue boundaries do not settle these details. These are work items for the first issue, not blockers to recommending the split.

No GitHub issues created, no formal stories or Plan proposal produced, no implementation or skill edits, no tests, and no mode transition. Run 0005 Research is complete.
