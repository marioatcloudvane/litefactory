# Research Brief — we-need-to-add-more-information-about-a

## Request

Fresh research for github:marioatcloudvane/litefactory#73, run 0007, 2026-09-08: “do a research here again and give me a summary how we can track token consumption!”

[O] The supplied issue history asks for consumption across repositories, tasks, modes, coding agents and models, including subagents. It expresses a preference for measuring tokens/models and applying a user-editable price table rather than relying on Claude's reported dollar figure. This brief treats that as the intended direction; it does not initiate Plan or Implement.

Provenance: **[V]** verified this run (local code or current official documentation, as cited); **[O]** reported owner preference; **[R0]** earlier-run claim supplied in conversation; **[W]** unverified second-hand claim. Recommendations/inferences are identified separately. No earlier session artifact or runtime log was opened. Official documentation was checked on 2026-09-08; documentation capabilities are not proof of the installed binary's behavior.

## Codebase Context

### Existing collection and ownership

[V] This is a single Go 1.26 module (`go.mod:1`). The vendor-neutral runner abstraction already exists: `internal/agent/doc.go:1`, `internal/agent/result.go:77`. Its result carries cost/presence, one model, session and timing information, and stream diagnostics, but no token counters or per-model usage (`internal/agent/result.go:114`, `:164`). Extending this contract is the natural integration point; a new competing runner abstraction is unnecessary.

[V] Claude's result decoder omits `usage` and `modelUsage` entirely (`internal/harness/stream.go:65`). Its root model comes from the observed initialization event (`internal/harness/runner.go:783`). The main invocation already requests verbose streaming JSON (`internal/harness/runner.go:581`). Codex's decoder similarly omits usage and merely marks `turn.completed` (`internal/codex/runner.go:412`, `:433`). A test already feeds input/output counters which are not asserted or retained (`internal/codex/runner_test.go:18`).

[V] Contrary to the earlier blanket statement that Codex has no model attribution, litefactory resolves a configured root model, passes it explicitly, and records it (`internal/codex/runner.go:83`, `:380`, `:326`). This is evidence of intended root configuration, not proof that all root, rerouted or child requests used that model. Both agents can have specialist model configuration (`internal/render/native.go:18`, `:42`, `:57`; `internal/harness/runner.go:281`).

### Persistence and reporting

[V] The turn allocates a database run before execution (`internal/turn/prepare.go:246`). Harvest runs the agent, writes metadata and finalizes the run (`internal/turn/harvest.go:53`, `:75`, `:101`). The persisted outcome currently retains cost, not tokens (`internal/turn/harvest.go:245`). Runs store mode, agent, timing, cost and log location; sessions provide repository/task identity (`internal/store/runs.go:101`, `:132`; `internal/store/migrations/0001_init.sql:49`). Model is in sidecar metadata, not the run table (`internal/harness/meta.go:53`, `:72`, `:113`). These dimensions support future grouping, but metering queries do not exist.

[V] Reports currently display monetary cost and one model (`internal/report/run.go:646`); CLI command registration has no consumption command (`internal/cli/commands.go:8`). SQL and accessors must remain inside `internal/store`, with context first and the existing single-connection discipline (`internal/store/store.go:1`). New schema belongs in numbered embedded migrations (`internal/store/migrate.go:16`).

### Important scope gaps

[V] Both runners save main-process stdout before or alongside parsing (`internal/harness/stream.go:115`; `internal/codex/runner.go:403`). Omitted decoder fields can therefore survive in raw logs. Those logs are not guaranteed copies of child transcripts.

[V] A logical run can include multiple subprocess attempts: a failed resume can retry cold, replacing the returned result (`internal/turn/harvest.go:147`, `:182`). Both runners append to the log (`internal/harness/runner.go:491`; `internal/codex/runner.go:272`). Treating an entire file as one terminal snapshot would lose attempts; summing every apparent snapshot could double-count cumulative usage.

[V] Classification/proposal helper processes are separate from the persisted main-run accounting; Codex uses discarded output for those paths (`internal/codex/runner.go:141`, `:183`, `:233`). “All repository consumption” therefore needs a declared boundary: tracked main runs initially, or additional instrumentation for daemon helper overhead. Historical helper consumption cannot be recovered from current run rows.

## Domain / Problem Space

### What Claude and Codex expose

| Source | Available information | Scope and limitation |
|---|---|---|
| Claude final `modelUsage` | Model-keyed input, output, cache-read, cache-creation counters | Includes subagents; appropriate starting point for per-model run accounting |
| Claude final `usage` | Aggregate token information | Excludes subagents; overlaps other views and must not be added to `modelUsage` |
| Codex `exec --json` terminal usage | Input, cached input, output; current documented example also includes reasoning output | No model or dollar fields in that usage object; descendant inclusion/resume semantics not established by the example |
| Codex configured root model | Already retained by litefactory | Intended model; weaker than observed request attribution |
| Claude request telemetry | Model/token buckets plus agent and parent identity | Richer optional collection, with deployment/version prerequisites |
| Codex app-server | Thread/turn model selection, reroute and usage notifications, collaboration thread identities | Potential richer collection; not yet a verified per-model/child ledger |

[V] Claude documentation specifies `modelUsage`'s subagent coverage and its distinction from `usage`. Single-shot results cover their invocation; streaming-input mode accumulates across turns, reset by clear/reset/new. Take the final cumulative snapshot per interval. Assistant IDs can repeat and assistant output counters are placeholders; authoritative output comes from results or partial-stream deltas. A crash can return zeroed usage, which is not evidence of no consumption. `costBasis` requires Claude Code 2.1.246+. These are documented SDK semantics requiring CLI fixture validation for the deployed version. [Claude cost tracking](https://code.claude.com/docs/en/agent-sdk/cost-tracking)

[V] Codex's current noninteractive example includes `input_tokens`, `cached_input_tokens`, `output_tokens`, and `reasoning_output_tokens` on `turn.completed`. It does not establish the child-thread or resumed-session scope. Do not silently turn that absence of documentation into either an inclusion or exclusion guarantee. [Codex noninteractive mode](https://learn.chatgpt.com/docs/non-interactive-mode)

### Per-subagent tracking is possible, but requires more than current run logs

[V] Claude SDK messages carry `parent_tool_use_id`, which can be joined to the spawning tool call. Both `Agent` and legacy `Task` names matter, with the change documented at 2.1.63. This supplies attribution, not a completeness guarantee. [Claude subagents](https://code.claude.com/docs/en/agent-sdk/subagents)

[V] Claude's `SubagentStop` hook supplies child identity and `agent_transcript_path`, distinct from the parent transcript. Thus the earlier claim that children cannot be distinguished is too broad. Collecting these files is additional work; current litefactory logs do not automatically include them. [Claude hooks](https://code.claude.com/docs/en/hooks)

[V] Current Claude monitoring documents request spans with model, token categories, request IDs, `agent_id` and `parent_agent_id`. Enhanced tracing is beta and disabled by default; it requires telemetry, enhanced telemetry and a trace exporter. Raw prompt/tool-body capture is not necessary for numeric token collection. Metrics can use delta or cumulative temporality; transcript joins depend on internal formats. A progressive-stream metric counting issue was fixed in 2.1.214. Recommendation: evaluate request telemetry before choosing long-term transcript scraping, and retain explicit loss/completeness status. [Claude monitoring](https://code.claude.com/docs/en/monitoring-usage)

[V] Codex app-server exposes `thread/tokenUsage/updated`, selected models, `model/rerouted` with thread/turn/from/to identities, and collaboration items linking threads. The surface includes stable APIs and separately gated experimental features. Generate JSON schemas from the actual installed binary. Inference: this may support a richer collector, but exact payload, cumulative boundaries, descendant delivery and usage-to-model pairing must be demonstrated before promising complete accounting. Changing from the current exec runner is a separate architectural decision. [Codex app-server](https://learn.chatgpt.com/docs/app-server)

### Normalize categories before summing or pricing

[V] Anthropic ordinary input, cache-read and cache-creation are separate input categories. Total input is their sum. Cache-creation TTL can affect the price, so preserving only one combined write counter may limit accuracy. [Anthropic prompt caching](https://platform.claude.com/docs/en/build-with-claude/prompt-caching)

[V] OpenAI cached input is a subset of input. Current GPT-5.6+ API documentation also describes a cache-write category and separate pricing, while the cited Codex CLI example does not expose it. Where those API categories are present, ordinary input is total input minus cache-read minus cache-write. Do not infer an absent CLI field is zero; the CLI may not expose enough detail for exact independent pricing. [OpenAI prompt caching](https://developers.openai.com/api/docs/guides/prompt-caching)

[V] OpenAI reasoning is included in output. Preserve it as a diagnostic subcount, never add it again to total output or its charge. [OpenAI reasoning tokens](https://developers.openai.com/api/docs/guides/reasoning)

Recommendation: retain original provider fields plus normalized disjoint billable categories. Attach availability to each category; unknown, unsupported and measured zero are different states. Validate nonnegative counters and subset relationships. Preserve a reported total for reconciliation without also counting it as an independent usage record.

### A practical shared abstraction

Recommendation, grounded in the existing shared runner contract:

- **Identity:** logical run, subprocess attempt, agent/provider, session/thread, optional child/parent identity and request or event identifier.
- **Attribution:** observed model when available, configured model separately, and explicit attribution quality. Preserve unknown model buckets rather than assigning them silently to the parent.
- **Measurement:** provider-native fields, normalized input/output/cache categories, optional reasoning subcount, source schema/collector version, and timestamps.
- **Scope:** request delta, turn total, session cumulative snapshot, model rollup or run rollup; whether descendants are included, excluded or unknown.
- **Coverage:** complete, partial, unknown or unsupported, with reasons such as truncated stream, absent terminal event or unavailable child telemetry.

Adapters belong with their vendor runners; shared normalized data crosses the existing agent result boundary. Persistence and aggregation remain store concerns. A future agent supplies the same measured data and declares capabilities rather than inventing a new reporting path.

Recommendation: separate immutable measurements from derived summaries and price valuations. Store one authoritative accounting view per scope, while keeping overlapping provider summaries as reconciliation evidence. Never sum a root inclusive rollup with its child records. For request-based collection, deduplicate by stable source identity. For cumulative counters, calculate increments only within an established reset boundary; unknown boundaries yield partial coverage, not speculative subtraction. Capture an attempt separately before a retry can overwrite its result.

### Editable prices and CLI behavior

[O] The owner prefers independent prices. Recommendation: token collection must work without a price table. Show consumption immediately; show monetary value only for categories with a known model and matching rate. A table ready at launch is useful, but is not a prerequisite for shipping truthful token metering. This corrects the earlier claim that replacing Claude pricing necessarily requires launch-ready rates.

Recommendation: use versioned rate records by provider/model/category, currency, unit and effective interval; retain the rate revision used for a valuation. Preserve decimal precision. Versioning is a proposed reproducibility policy, not a vendor requirement or a settled owner decision. Provide explicit historical repricing if desired, rather than silently changing old valuations. A subscription or special agreement may make token-derived amounts a comparison estimate rather than an invoice. Retain Claude's original cost only as optional provenance, not the primary valuation.

Recommendation: `litefactory meter` is a reasonable umbrella name because this feature covers tokens as well as money. Suggested exploration in Plan: all tracked runs, repository and task views, with mode, coding-agent, model and date filters and machine-readable output. Columns should include input/cache/output consumption, estimated value, and coverage/unpriced counts. Display partial sums as partial, never as a complete grand total. Use the existing task identity conventions rather than creating ambiguous bare issue-number handling.

### Historical recovery

Recommendation: an optional backfill can parse retained stdout and sidecars, recover emitted usage/model evidence, and write idempotently identified measurements. It must distinguish subprocess attempts, terminal snapshots and cumulative resets before aggregation. Retain extraction provenance and parser version. Preview coverage before promising recovered totals.

[V] Current code preserves richer stdout but lacks token persistence, child transcript collection and helper-call records. Therefore backfill is possible in principle for some main runs, not guaranteed for every historical run or subagent (`internal/harness/stream.go:115`; `internal/codex/runner.go:403`; `internal/codex/runner.go:233`). No production logs were inspected in this research.

## Constraints & Risks

1. **Version mismatch:** current documentation is not installed-binary evidence. No Codex release/commit was pinned. Representative deployed outputs and generated schemas remain required.
2. **Double-counting:** inclusive Claude model totals versus root totals, parent versus child records, reasoning versus output, cache versus input, and repeated cumulative snapshots all overlap in different ways.
3. **Incomplete accounting:** crashes, stream failures, unavailable child events and helper calls can leave gaps even when a run succeeds (`internal/agent/result.go:164`). Missing data must not become zero. Claude's existing cost-presence behavior already turns a missing numeric cost into zero when a terminal event exists (`internal/harness/runner.go:803`); do not copy that pattern for usage.
4. **Model ambiguity:** configured Codex root model does not settle child or rerouted consumption. Keep known tokens even when exact monetary valuation is impossible.
5. **Pricing ambiguity:** missing cache-write/TTL detail, unknown models and custom agreements limit valuation. An editable table cannot repair absent measurement.
6. **Historical scope:** appended retry logs need attempt-aware extraction, and discarded helper output is unrecoverable from current storage.
7. **Data handling:** raw logs and metadata may contain unredacted prompts/tool content (`internal/harness/meta.go:34`, `:93`). Numeric metering does not need to copy those contents into database rows or public reports. Existing runtime state must not be edited manually.
8. **Repository constraints:** preserve store ownership/migration rules and runner boundaries. Research changed documentation only; no production code or tests were changed or executed.

## Open Questions

Plan must resolve these with representative version-pinned fixtures or explicitly constrained scope, without treating them as reasons to delay this research artifact:

- Which deployed Claude/Codex versions are supported, and what exact result payloads and reset semantics do they emit?
- Does the deployed Codex exec total include children, and what happens across resumed sessions? Can app-server events be paired reliably with models and all descendant usage?
- Does deployed Claude CLI output match documented per-model inclusion semantics, and which cache-write details survive? Is enhanced telemetry operationally acceptable?
- Should first delivery account for main mode runs only, or also classification/proposal overhead? Recommendation: label main-run scope explicitly first.
- Are full child attribution and historical backfill required initially? Recommendation: separate these from reliable available run/model totals.
- Should default valuations preserve historical rate revisions, and should explicit repricing be offered? Recommendation: preserve revisions; retain provider cost only as optional evidence.
- Which historical logs actually remain available? This requires an authorized local inventory during subsequent work; no assumption of complete retention was made here.

## Recommended scope for Plan mode

Build on the existing agent contract and runner adapters to persist available token measurements, attribution and coverage per subprocess attempt, aggregate by repository/task/mode/agent/model, and apply optional user-editable versioned prices. Claude per-model totals are the strongest documented immediate source. Codex's terminal counters and configured root model are useful immediately, with honest limits on model and descendant attribution. Validate supported versions, retries, resumed sessions, overlap, incomplete streams, missing fields and pricing against representative fixtures; use real SQLite for persistence guarantees under existing project testing rules (`.aiteam/APP_CONSTRAINTS.md:137`).

Treat complete per-child collection, an app-server transport change, helper-process coverage and historical reconstruction as separately bounded additions. They are feasible research directions with specific prerequisites, not features already supplied by the current log parser. This run concludes Research only.
