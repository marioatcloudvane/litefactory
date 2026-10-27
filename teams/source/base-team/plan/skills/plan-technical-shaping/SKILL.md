---
name: plan-technical-shaping
description: Review applicable architecture while retaining settled choices.
requires: [plan-evidence]
---
# Technical shaping
Start from exact approved intent and prior decisions. Check reuse, architecture
fit, interfaces, permissions, entities/relationships, invariants, dependencies and
failure behavior. When the story relies on an external system's guarantee (API
semantics, delivery, ordering, idempotency, deduplication, rate limits), check it
against the provider's documentation or the research evidence and record the
decision and its limits in general. If the platform cannot give what the intent
needs, ask the owner with options; never leave it for Implement. Record
decisions/rationales and assumptions. Populate typed
general for every architecture review. Data model is for actual entity/relationship
scope; service design for layering, service/repository/database access and
decomposition; solution design for collaboration, interfaces and abstractions,
with inheritance only where relevant. Neither Go nor object orientation is required.
Use Design references only for frontend scope. Preserve existing applicable tables
and frontend references across cycles; explicit scoped changes alone replace them.
Select applicable guidance only from the admitted team profile and actual artifacts.
Frontend references alone do not select backend guidance.
Propose revision or retirement if intent fails feasibility or relevance; the owner
must confirm retirement against the exact displayed active candidate.

## Detail from supplied story size
Use the matching set entry's supplied size and reason, following plan-evidence's
shared sizing rules. Do not assign or rescale a size here. Missing or invalid
size keeps the existing planning depth, with an honest unavailable-size note;
a valid size still guides depth when its reason is unavailable. Exempt old sets
receive no request to add sizing, including through replanning. Missing
applicability does not authorize backfill.

These examples change explanation depth only. At every size retain all agreed
outcomes, relevant `N-`/`K-` entries, `Covers:` and `Ruling:` notes and required
proof links. Keep stage ownership, owner gates and required evidence unchanged.
The examples are guidance, with no numeric task-count or criterion-length rules.

- **S — local wording change:** name the existing text owner, before/after
  behavior and compatibility boundary in concise general decisions. Omit
  irrelevant data, service and solution tables; retain any material risk.
- **M — a role and its supplied facts change together:** explain producer and
  consumer behavior, interface ownership, missing-data fallback and shared
  surfaces in normal detail. Include applicable compatibility and failure
  decisions; omit layers and tables the change does not need.
- **L — migration across several components:** explain dependency order,
  transition and retirement, compatibility, migration invariants, failure paths
  and recovery. Check relevant external guarantees and unknowns explicitly.
  Omit unrelated infrastructure and optional design sections without a real
  role in the migration; full detail is proportionate to the actual risks.
