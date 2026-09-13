# Feature Spec: materially closer meter estimates
# Status: APPROVED
# Date: 2026-09-12

## Goal
- Recover evidenced missing consumption and make remaining uncertainty explicit, so the operator gets a materially closer local cost estimate without invoice reconciliation or guessed multipliers.

## Decisions and assumptions
- Owner run 0007 requests the combined bounded scope established in research, kept simple; no unresolved product question blocks planning.
- Historical #195 run 5 is the quantitative acceptance target: $5.148750 root plus $6.754276 across three children = $11.903026 at the retained rates; this is an observed subtotal, not certified complete spend.
- The original run-8/72% diagnosis is rejected; runs 6–8 must keep their existing attribution and values.
- Assume recovery is an explicit operator action, preview by default and opt-in application; implementation/CI verifies against isolated fixtures and stores, without mutating the live daemon database.
- Assume one scoped recovery entrypoint using existing task/run selectors is sufficient; no scheduler, new service or generic repair framework.
- No protected instruction-tree edits are required.

## User Stories
| ID | Story |
|---|---|
| US-1 | As an operator, I can preview and apply supported historical descendant recovery once without charging resumed attempts twice. |
| US-2 | As an operator, I get correctly attributed descendant consumption within bounded supported collection, with explicit uncertainty when collection cannot establish coverage. |
| US-3 | As an operator, I can distinguish an evidenced zero-cost resume refusal from an unmeasured failure. |
| US-4 | As an operator, I can see a useful partial cost number without mistaking it for a complete total. |

## Existing features to reuse
| Concern | Existing owner |
|---|---|
| Provider collection and read-only bounded source discovery | `internal/codex`, `internal/harness` |
| Usage identity, nullable counters and invocation ancestry | `internal/agent` |
| Reconciliation, replay protection, subsets and exact valuation | `internal/meter` |
| Durable attempts, observations, checkpoints and pinned prices | `internal/store` |
| Meter selection, CLI totals/details and structured export | `internal/cli`, `internal/report` |
| Structural resume refusal and fresh fallback | Existing runner outcome and turn orchestration |

## Data model and relationships
| Entity | Required information | Constraints |
|---|---|---|
| Historical recovery candidate | Selected task/run/attempt, invocation/parent identity, observed model, exclusive counters, source identity and attempt-time evidence | Parentage alone never establishes attempt ownership; no guessed model; missing/ambiguous evidence stays unresolved |
| Applied recovery | Existing durable meter records plus provenance and deduplication identity | Preserve original attempt identity and retained price provenance; repeated apply is a no-op for already measured consumption |
| Refused attempt | Structural refusal, affirmative terminal zero turns and zero provider cost, existing attempt/fallback link | Preserve unknown model and separate provider zero-cost evidence from token-derived pricing |
| Report | Existing subtotal, complete estimate and status fields; explicit refusal explanation | Unknown remains distinct from zero; incomplete complete-estimate remains null |
- Invocation → original attempt; descendant → parent invocation; repeated thread use → separate attempt deltas.

## Interface and validation
| Action | Access | Required behavior |
|---|---|---|
| Scoped historical recovery preview | Existing local operator CLI access | Select a task/run using existing selector conventions; report before/after totals, included records and unresolved/skipped reasons; no mutation |
| Explicit recovery apply | Same operator access, explicit apply opt-in | Revalidate selected evidence/state, safely reconcile against current measurements and return applied/no-op/unresolved results; reject invalid selectors and unsafe source changes |
| Existing meter summary/details | Existing CLI access | Label incomplete known subtotal as “observed lower bound at recorded rates; total unknown”; explain confirmed refusal separately |
| Existing JSON/CSV | Existing interfaces | Preserve nullable totals/status semantics and stable existing fields; expose refusal facts additively where required |

## Business rules and edge cases
- Count each response's exclusive usage once; exclude foreign/inherited records and historical consumption outside the selected attempt; reasoning/cache subsets are not billed twice.
- Missing evidence, ambiguous ownership, unsupported shapes, counter conflicts, source replacement or exhausted discovery limits produce explicit uncertainty; never silently become complete or zero.
- Recovery is bounded to the selected scope; interruption/retry cannot duplicate charges or leave a partially applied candidate represented as successful.
- Discovery tests cover parallel/nested children, resumed deltas, unchanged intermediate ancestors, delayed children and bounded scans; fix reproduced failures using the shared collector rather than a mode-specific path.
- Late activity outside a bounded observation window does not justify an unbounded wait or completeness claim; preserve appropriate collection diagnostics.
- A refusal classification requires all affirmative evidence; missing terminal cost, nonzero cost, interruption or actual work cannot take the zero-cost path.
- Refusal classification does not erase independent compatibility or descendant-coverage diagnostics on the successful fallback.

## Acceptance criteria
- [ ] AC-001: Preview over the retained run-5 evidence and isolated historical store shows one root and three correctly parented/model-attributed children, with $6.754276 additional observed usage and $11.903026 resulting subtotal at recorded rates; preview changes no records.
- [ ] AC-002: Applying that recovery yields the previewed attribution and subtotal; repeated application and interrupted/retried application do not duplicate consumption; runs 6, 7 and 8 remain $1.746696, $0.326984 and $0.558352 respectively.
- [ ] AC-003: Ambiguous, missing, replaced, malformed or unsupported recovery evidence is reported without guessed usage/model/ownership or false completeness; invalid selectors fail before mutation.
- [ ] AC-004: Executed collector regression cases cover parallel and nested descendants, resumed deltas, inherited/foreign records, unchanged ancestors with active grandchildren, delayed discovery and scan limits; each observable own-attempt response is charged once, unrelated/historical responses are excluded and unsupported/incomplete cases retain diagnostics. Include an Implement-labelled case on the shared collection path and fix failures these cases demonstrate.
- [ ] AC-005: The retained Claude refusal is identified as provider-reported zero-cost refusal with the attempt and fallback relationship preserved and model unknown; missing/nonzero cost, interrupted and real-work negative cases are not classified as proven zero-cost refusals.
- [ ] AC-006: Complete, partial, wholly unknown and confirmed-refusal CLI cases clearly distinguish complete estimate, observed lower bound and provider-reported zero; incomplete complete-estimate remains null and unrelated diagnostics survive; removed run-comment meter metadata stays absent.
- [ ] AC-007: Operator documentation gives scoped preview/apply instructions, provenance/uncertainty limits and the measured historical improvement; fixtures and reports distinguish authentic historical evidence from synthetic test envelopes and make no fleet-wide, current-runtime or invoice-accuracy certification.

## Candidate evidence inventory
| Criterion | Proof method | Source availability | Version/context bounds | Missing prerequisite / disposition |
|---|---|---|---|---|
| AC-001 | Offline recovery test and exact valuation assertion | Session `RUNTIME_EVIDENCE.json` and `RESEARCH_BRIEF.md`: authentic extracted metadata/counters with source hashes; create minimal sanitized test envelopes | Codex 0.153.4, 2026-09-09 run 5; historical $10/$1/$50 per-million rates; reconstructed envelopes are synthetic | Executed test and provenance manifest needed; original full transcripts must not be committed; no current live capability claim |
| AC-002 | Isolated-store apply/replay/interruption and later-attempt assertions | Same retained historical extraction, run windows and recorded root values; synthetic retry scheduling | Historical attempt attribution only; current code behavior established by offline execution | Tests needed; actual operator backfill is a later explicit action, not fabricated as completed deployment |
| AC-003 | Negative fixture tests and read-only source handling review | Synthetic ambiguity, malformed input and changed-source cases | Offline behavior only | Tests needed; absent source remains unresolved rather than recovered |
| AC-004 | Actual collector exercised using provenance-preserving minimal envelopes plus synthetic scheduling/topologies | Authentic three parallel child extracts available; nested, delayed, unchanged-ancestor and limit scenarios synthetic | Known envelopes grounded in Codex 0.153.4; no authentic current full-hierarchy capture available | Offline regressions required; authentic present-day hierarchy certification remains unsatisfied/outside promised certification; do not launch nested paid agents or relax sandbox |
| AC-005 | Offline positive assertion over retained terminal refusal facts plus synthetic negatives | `RUNTIME_EVIDENCE.json` retained refusal result; successful fallback reports Claude 2.1.267 | Refusal process version itself not independently established; no invented model | Executed classification/report tests needed; historical result proves its own refusal only |
| AC-006 | Executed output and structured-value assertions | Existing report surfaces; synthetic complete/partial/unknown cases and retained refusal facts | Current CLI behavior under test; not original deleted comment output | Tests needed; no historical whole-group output reconstruction prerequisite |
| AC-007 | Documentation/fixture provenance review against retained evidence and executed checks | Session artifacts and resulting implementation evidence | Explicit historical versions and unknowns above | Final review needed; synthetic fixtures never discharge authentic runtime certification |

## Out of scope
- Provider invoices, fleet-wide accuracy percentage, guessed uplift multipliers, pricing redesign or blanket completeness claims.
- Charging run-5 children to run 8; rewriting existing root/resume pricing; dropping unknown evidence.
- Restoring meter data to run comments; redesigning reporting or adding services/background repair jobs.
- Running a live backfill during Plan/Implement, daemon changes, paid nested provider captures, sandbox changes, or certifying present-day full-hierarchy capability.
- Closing/recovering acceptance on historical issues #73/#125/#178 without their factory-supplied criteria and required evidence.

## Risks
- Retained extraction establishes the historical arithmetic; executable source envelopes still need careful provenance review and cannot substitute for authentic live end-to-end observations.
- Recovery may improve additional eligible historical attempts, but backlog size and fleet-wide uplift are unmeasured.
- Bounded discovery cannot promise collection of arbitrarily late or unavailable files; diagnostics remain part of successful behavior.
