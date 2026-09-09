# Feature Spec: Token and Model Metering
# Status: APPROVED
# Date: 2026-09-08

## Goal
- Let the local owner inspect token consumption and independently priced estimates for every tracked main run and its complete descendant agent tree, across Claude and Codex.

## Decisions and Assumptions
| Source | Decision |
|---|---|
| Owner, run 0008 | Full subagent support for both coding agents is mandatory in the first release; it cannot be deferred or replaced by aggregate-only accounting. |
| Owner, run 0008 | Price changes never recalculate historical valuations. |
| Owner, run 0008 | Historical log rollup belongs in a separately commissioned script outside this repository; no script, backfill command or importer is included here. |
| Research brief | Extend the existing vendor-neutral agent contract; preserve vendor-native counters, scope, attribution and collection completeness. |
| Assumption: existing request naming | Use `meter` as the CLI entry point, including token reporting when no prices exist. |
| Assumption: research scope boundary | Meter main mode runs, every subprocess attempt and all their descendants; exclude classification/proposal helper invocations, clearly labelled in output. |
| Assumption: deterministic historical policy | Pin the applicable price revision when each attempt begins; freeze each recorded valuation, including unpriced status, without automatic or manual historical repricing. |
| Assumption: compatibility | Certify explicit deployed agent versions and collection capabilities before release; unsupported versions cannot claim complete metering. |
| Assumption: deployment | Local metadata-only collection or a runner transport change is acceptable where necessary to satisfy full descendant accounting; no external telemetry service is required. |

## User Stories
| ID | Story |
|---|---|
| US-1 | As an owner, I want reliable measured token records with explicit scope and completeness so retries, missing data and cumulative events do not distort consumption. |
| US-2 | As an owner, I want every Claude descendant attributed to its parent and observed model so I can inspect the complete agent tree. |
| US-3 | As an owner, I want every Codex descendant attributed to its parent and observed model so its accounting is as complete as Claude's. |
| US-4 | As an owner, I want measurements and frozen valuations retained durably so completed, failed and retried runs remain inspectable. |
| US-5 | As an owner, I want editable prices independent of vendor dollar figures so estimates reflect my rates without rewriting history. |
| US-6 | As an owner, I want CLI summaries and run reports by repository, task, mode, agent, model and child identity so I can understand where consumption occurred. |

## Data Model
| Entity | Fields | Required | Constraints |
|---|---|---|---|
| Attempt | Run identity, attempt identity, start/end time, agent kind/version, collection version, pinned price revision | Identity and provenance | Separate failed resume and cold fallback; never overwrite earlier attempt consumption. |
| Agent execution | Attempt identity, session/thread identity, agent identity, parent identity, observed/configured models, attribution quality | Identity and explicit attribution status | Root has no parent; descendants retain parent links; configured model is not observed attribution. |
| Measurement | Stable source identity, agent execution, timestamp, native numeric counters, normalized counters, category availability, measurement scope, descendant-inclusion status, coverage and reason | Identity, scope and availability | Nonnegative integers; absence differs from zero; original totals retained for reconciliation only where overlapping. |
| Coverage | Attempt/agent identity, complete/partial/unknown/unsupported, reason | Status | Completion requires all observed descendants accounted for and a validated collection boundary. |
| Price revision | Revision identity, provider/model/category, decimal rate, currency, token unit, effective interval | All pricing dimensions | Append revisions; finite nonnegative decimal rates; no overlapping conflicting intervals for identical dimensions. |
| Frozen valuation | Measurement/attempt identity, rate revision, frozen amount by currency, priced/unpriced/partial state and reason | State and revision selection | Unknown models or missing categories/rates are unpriced, never implicitly free; preserve history after edits. |

### Relationships
- Existing repository/task session → logical run → one or more attempts → rooted agent execution tree → measurements.
- Immutable measurement → frozen valuation referencing its pinned price revision; overlapping reconciliation totals do not create a second charge.
- Existing run metadata supplies mode and coding-agent dimensions; model and agent-tree dimensions come from measured attribution.

## API / Interface Changes
| Method | Action | Auth | Notes |
|---|---|---|---|
| CLI read | `litefactory meter [repository] [task-id]` | Existing local CLI access | Default scope is all tracked main runs and their descendants; use existing repository/task resolution conventions. |
| CLI read | Meter filters `--mode`, `--ca`, `--model`, `--from`, `--to`, `--json` | Existing local CLI access | Validate enumerations/date bounds; use UTC inclusive start and exclusive end; model filter matches measured model, with unknown attribution separately available. |
| CLI read | Meter detail `--run <number>` and `--agents` with a task selector | Existing local CLI access | Show all attempts and nested parent/child executions, counters, observed model, attribution and coverage. |
| CLI read/write | `litefactory meter prices list` / `set` | Existing local CLI access | Explicit provider, model, category, decimal rate, currency, token unit and effective-from inputs; create new revisions through supported application writes. |
| Report | Existing final run report | Existing report delivery | Add measured token total, coverage and locally priced estimate; visibly distinguish legacy/vendor figures from the primary estimate. |
| Internal contract | Existing `agent.Runner` result/collection boundary | Internal | Carry attempt-aware descendant measurements and capabilities without a competing runner abstraction. |

## Business Rules
- Every supported agent collector must observe complete descendant trees, parent links and model attribution, including nested/concurrent descendants, resumed children and multiple models; aggregate root totals alone do not satisfy first-release support.
- Validate exact deployed binary outputs, schema and reset semantics before implementation claims support; if available events cannot attribute rerouted requests or descendants, add a collection path that can, or block release for that version rather than assigning parent-model guesses.
- Normalize to disjoint billable categories: ordinary input, cache read, cache write where exposed, and output; preserve cache TTL distinctions when available. Reasoning is an output subcount and is not charged twice.
- Preserve uncertainty when exposed fields cannot distinguish billable categories; tokens may be known while exact monetary valuation remains partial or unknown.
- Select one authoritative accounting view per scope. Inclusive root/model rollups reconcile against child/request records; never sum both. Deduplicate stable event/request identities and process cumulative increments only across demonstrated reset boundaries.
- Persist attempt measurements before a retry can replace a result. Failed/cancelled attempts count their observed consumption; resumed invocations must not charge prior-session consumption again.
- Price edits apply only to subsequent attempts eligible for the new revision. A running attempt keeps its initial revision; previously unpriced history remains unpriced. Do not implement a historical repricing option.
- Token collection works with an empty price table. No hardcoded vendor rate or Claude cost substitutes for a missing user price.
- Currency totals remain separate; no currency conversion. Numeric token collection must not copy prompts, responses, tool bodies or credentials into metering storage.
- Existing pre-feature rows remain visibly unmetered; ordinary reads and migrations do not inspect old logs or populate usage from them.

## Edge Cases & Error Handling
- Unsupported or changed agent telemetry schema → explicit compatibility/coverage failure; never claim a certified complete record from an unrecognized format.
- Truncated stream, collector outage, absent child completion, orphan child, process crash or missing terminal counters → retain observed measurements and mark affected scopes partial/unknown with reasons.
- Legitimate terminal zero counters → measured zero only with affirmative complete measurement evidence; zeroed crash output does not prove no consumption.
- Duplicate event, replay, repeated cumulative total or delivery retry → idempotent storage and unchanged aggregate consumption.
- Negative counters, impossible subset relation, overflow or cumulative reset without a validated boundary → reject/quarantine that measurement and expose reduced coverage.
- Unknown observed model → preserve tokens in an unknown-model bucket and leave the corresponding valuation unpriced; retain configured model separately.
- Invalid selector, incompatible `--run`/`--agents` scope, invalid enum/date/rate/currency/unit → actionable CLI error without data mutation.
- No matching runs → explicit empty result; mixed complete/partial/unmetered runs → coverage counts and partial estimate labels, never a false complete grand total.

## Out of Scope
- Any in-repository historical rollup/backfill/import script, automatic log scan, retrospective measurement recovery or historical repricing.
- Billing reconciliation, actual invoice attribution, subscription allocation, currency conversion or automatic web price ingestion.
- Daemon classification/proposal helper overhead; reports state this boundary.
- New coding agents beyond Claude and Codex; the contract permits future adapters.
- Production changes, binary invocation experiments and implementation during this Plan run.

## Acceptance Criteria
- [ ] AC-1: Supported Claude and Codex fixtures and live compatibility evidence demonstrate root plus concurrent/nested descendant collection, stable parent linkage and observed per-model token attribution; aggregate-only support fails release acceptance.
- [ ] AC-2: Each provider preserves native counters and produces validated, availability-aware disjoint categories without adding reasoning to output or cached-input subsets to total input twice.
- [ ] AC-3: Duplicate events, cumulative snapshots, inclusive parent totals, resumed sessions and failed-resume/cold-fallback attempts yield the expected single-counted totals under deterministic fixtures.
- [ ] AC-4: Missing or malformed measurement data and missing descendants survive as explicit partial/unknown/unsupported coverage; measured zero remains distinguishable from unmetered usage.
- [ ] AC-5: Durable attempt and descendant records retain observed usage through failure/cancellation and idempotent repeated persistence; existing runs remain unmetered without log inspection.
- [ ] AC-6: Price revision creation validates inputs and leaves every prior amount, pinned revision and prior unpriced state unchanged, including an attempt running during a price edit.
- [ ] AC-7: CLI aggregate and agent-tree views produce consistent totals and coverage for all/repository/task scopes, each documented filter, retries and mixed currencies; JSON preserves numeric values and explicit unknown states.
- [ ] AC-8: Final run reports show token consumption, completeness and the user-priced estimate; missing prices do not suppress tokens or fall back to vendor dollar figures.
- [ ] AC-9: Metering rows and public summaries contain numeric/provenance data only, and enabling metering preserves existing worktree, permission, report and save behavior for both runners.
- [ ] AC-10: Delivery contains no historical rollup/import/repricing command or script, and documents that helper overhead and pre-feature consumption are outside the reported coverage.
