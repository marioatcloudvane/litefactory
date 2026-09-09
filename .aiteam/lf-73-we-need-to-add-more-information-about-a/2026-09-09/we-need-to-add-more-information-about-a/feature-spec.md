# Feature Spec: Token and Model Metering
# Status: APPROVED
# Date: 2026-09-09

## Goal
- Give operators durable, explainable token consumption and independently priced estimates for every managed run, including all Claude and Codex subagents in the first release.

## Evidence and Assumptions
- Source: current owner requirements mandate full subagent support in v1, editable prices without historical recalculation, and historical recovery outside repository code.
- Verified existing owners: `internal/agent` supplies the shared runner result; `internal/harness` and `internal/codex` own execution; `internal/store/runs.go` stores nullable reported cost; `internal/cli` owns commands.
- No research brief exists in this session; provider payload capabilities are implementation verification obligations, not assumed guarantees.
- Assume `meter` as the command name and USD per million tokens as the initial pricing unit.
- Assume scope includes all agent work causally belonging to the managed run and every descendant; separate daemon helper invocations are excluded from run estimates and explicitly labelled as excluded overhead.
- Assume local CLI access follows existing operator permissions; no new network service or credentials are introduced.
- Assume immutable price revision selected at attempt start; edits affect later attempts only, including when an earlier attempt finishes after an edit.

## User Stories
| ID | Story |
|---|---|
| US-1 | As an operator, I need a shared measurement contract and verified supported agent versions so that complete metering has a demonstrable meaning. |
| US-2 | As an operator, I need every Claude root and descendant measured by actual model so that delegated work is accounted for. |
| US-3 | As an operator, I need every Codex root and descendant measured by actual model so that delegated work is accounted for. |
| US-4 | As an operator, I need durable deduplicated measurements across attempts and interruptions so that totals reflect consumed work. |
| US-5 | As an operator, I need editable price revisions and frozen estimates so that my agreements apply without changing history. |
| US-6 | As an operator, I need filtered metering reports and run summaries so that I can understand consumption and coverage. |
| US-7 | As a maintainer, I need end-to-end evidence for both agents and full descendant coverage before v1 can ship. |

## Data Model
| Entity | Fields | Required | Constraints |
|---|---|---|---|
| Attempt usage | Run identity, attempt identity, coding agent, agent version, mode, collection status, diagnostics | Yes | Separate executed attempts, including fallback and resume, contribute incremental consumption exactly once. |
| Invocation | Attempt identity, invocation identity, parent identity, source session/thread identity, lifecycle, coverage | Yes except root parent | All descendants attach to their actual parent; distinguish root-only from inclusive totals. |
| Measurement | Source identity, invocation, actual model, model attribution provenance, uncached input, cache read, cache write, output, optional provider-specific dimensions | Identity and provenance | Nonnegative integer counters; unknown dimensions remain nullable; cached subsets never inflate totals. |
| Price revision | Stable revision identity, creation/effective time, model selector, currency, dimension rates | Yes | Exact nonnegative decimal rates; immutable revisions; explicit unknown pricing. |
| Frozen valuation | Attempt, selected price revision, measurement quantities, applied rates, estimated amount, pricing completeness | Yes except unknown amount/rates | Pricing evidence and outcome survive later edits; never silently reprice old runs. |

### Relationships
- Repository → task → run → attempt → root invocation → descendant invocations → model measurements.
- Attempt → immutable price revision; measurement → frozen valuation.
- Provider-inclusive totals → reconciliation evidence; never added again to descendant totals.

## API / Interface Changes
| Surface | Action | Auth | Notes |
|---|---|---|---|
| CLI | `litefactory meter [repository [task-id]]` | Existing local operator | Summary of tokens, estimates and coverage across all attempts in scope. |
| CLI | `--mode`, `--ca`, `--model`, `--json` | Existing local operator | Composable filters; stable machine output distinguishes null from zero. |
| CLI | `--details` | Existing local operator | Show run, attempt, invocation ancestry and per-model dimensions. |
| CLI | `litefactory meter prices list` and `litefactory meter prices set <model>` with dimension-rate flags | Existing local operator | Supported revision-creating edit path; do not require manual daemon-state edits. |
| Existing report | Managed run completion | Existing factory delivery | Token summary and independently calculated estimate with explicit completeness. |
| Runner contract | Shared usage payload and collection evidence | Internal | Extend existing abstraction with one adapter per coding agent. |

## Business Rules
- Full root and descendant usage with actual model attribution for both agents is a v1 release gate; partial display is failure handling, never an acceptable substitute for full support.
- Establish supported executable versions and collection capabilities using captured real executions and version-pinned fixtures before declaring the gate passed.
- Capture all descendant usage, including nested, parallel, resumed and failed work; allow bounded settling for asynchronously finishing children.
- Distinguish cumulative snapshots from increments; deterministic source identities prevent replay duplicates without deleting genuine repeated work.
- Normalize mutually exclusive billable token dimensions; preserve provenance and optional details such as cache-write duration or reasoning subsets without counting them twice.
- Aggregate token/model measurements first; calculate estimates exclusively from local price revisions, never from Claude's reported cost field.
- Store both known subtotal and coverage when records are missing; never label a partial total as complete.
- Unmatched model prices yield unpriced tokens and unknown estimate, not free usage; legitimate configured zero rates produce known zero estimates.
- Resolve and freeze rates for each attempt before execution; price edits never change completed or in-flight attempt valuations.
- Preserve existing historical reported costs as explicitly labelled legacy data; do not manufacture historical token records.

## Edge Cases & Error Handling
- Process failure, cancellation, timeout or missing terminal event → retain observed usage and mark incomplete evidence.
- Unsupported provider version or unavailable child collection capability → actionable compatibility failure; v1 full-support gate remains unsatisfied.
- Unknown actual model, model switch or missing descendant finalization → expose unresolved coverage; configured model alone is not proof of actual attribution.
- Counter regression, malformed payload or overflow → reject invalid measurement with diagnostics; do not corrupt prior valid rows.
- Duplicate events, callback replay or restart → persist usage idempotently; distinct retries remain counted.
- Parent totals disagree with child sum → expose reconciliation mismatch and prevent complete status.
- Mixed known and unknown prices → display priced subtotal plus unpriced coverage; no misleading all-in estimate.
- Invalid repository/task selector, unknown filter value or invalid decimal rate → actionable validation error with no partial mutation.
- Existing installations without price revisions → token reports work with unpriced estimates until operator configures rates.

## Out of Scope
- Historical-log import/backfill/rollup commands, scripts or code in this repository; any recovery script is external work and is not produced by this Plan.
- Invoice reconciliation, subscriptions, credits, taxes, or claims that estimates equal the provider bill.
- Recalculating old estimates after price edits, automatic remote pricing fetches, and additional coding-agent implementations.
- Separate daemon helper overhead in managed-run consumption totals; all descendants of the managed run remain mandatory.

## Acceptance Criteria
- [ ] AC-1: Version-pinned real execution evidence proves root, parallel and nested descendant collection and actual-model attribution for Claude and Codex; absence blocks v1 release.
- [ ] AC-2: Both adapters emit the shared contract with explicit known/unknown token dimensions and provenance, without provider-specific exported contract naming.
- [ ] AC-3: Retry, resume, fallback, restart and replay scenarios preserve every distinct attempt's consumption exactly once and avoid adding inclusive totals to child measurements.
- [ ] AC-4: Failure and cancellation preserve observed usage; missing evidence produces partial/unknown coverage rather than zero or complete status.
- [ ] AC-5: Price edits create immutable revisions; a completed and an in-flight attempt retain original rates and estimates after an edit, while later attempts use the new revision.
- [ ] AC-6: Global, repository and task CLI scopes and composable mode/agent/model filters return reconciled totals, per-model dimensions, invocation details and coverage in text and JSON.
- [ ] AC-7: Completion reports use local frozen estimates and distinguish unpriced or incomplete usage; legacy reported costs are labelled separately.
- [ ] AC-8: No historical recovery code or script enters the repository; all SQL and durable usage ownership remain within existing store boundaries.
