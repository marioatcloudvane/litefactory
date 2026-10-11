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
