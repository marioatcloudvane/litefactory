# Define and implement the shared agent communication contract

Follow-up 1 of 3 from #258. Establish the shared contract before production adoption and MCP integration.

## Problem and outcome

Agents currently return data through several mode- and version-specific contracts. Define one versioned application contract, with reusable Go validation and agent instructions, so later adapters can share semantics without duplicating workflow logic.

## Scope

- Agree a JSON envelope with factory-owned `meta` and agent-authored `result`. Keep run/mode/identity and authoritative workflow state bound by litefactory.
- Define common outcome/comment/question/artifact fields and typed mode-specific payloads, including Plan stories and Validate findings. Required fields depend on mode and outcome.
- Specify snapshot creation and revision-checked sparse updates using stable item IDs. Decide merge-patch versus explicit operations, omitted/null/deletion behavior, ordered collections, allowed edits and stale-revision diagnostics.
- Preserve immutable run reports/receipts and frozen scope while retaining new full snapshots of mutable results. Validate the complete merged result, including dependencies and domain restrictions.
- Define correlation, retry/version behavior and separate received, accepted and published states, reusing existing v2 contribution/receipt machinery.
- Define advisory in-flight progress semantics now: run attribution, ordering/deduplication, retention and coalescing. Progress cannot grant completion, readiness or acceptance.
- Implement reusable Go decoding, semantic validation and patch application, with representative valid/invalid fixtures.
- Provide a reusable finalization skill. Keep a small discovery/finalization instruction available early and load detailed mode-specific serialization guidance just before submission, including blocked/questioning exits. Work constraints, relevant baseline data and evidence-capture requirements must remain available when needed.
- Favor scoped baseline reads and small updates over repeated full results/schema text. Evaluate representative total input/output and retry costs before claiming token savings.

## Completion criteria

- [ ] Contract details and examples cover all five modes and successful, incomplete/questioning and blocked outcomes.
- [ ] Tests demonstrate full creation, a small update preserving untouched fields, stale-revision rejection, invalid/unauthorized fields and full merged-result validation.
- [ ] Snapshot history, immutable metadata and receipt/acceptance boundaries are explicit.
- [ ] Progress is defined independently of MCP and cannot change authoritative workflow decisions.
- [ ] The skill and fixtures match the Go implementation; lifecycle enforcement remains a factory responsibility.
- [ ] Schema version, result revision, submission contract version and workflow version are distinguished.

## Boundaries

No live routing cutover, legacy deletion or MCP server in this issue. The illustrative JSON in #258 is design input, not an already frozen schema. Avoid a generic event framework beyond the actual communication inventory.

## Evidence

Research and agreed delivery boundaries: #258. Detailed repository notes: `.aiteam/lf-258-rework-communication/2026-09-12/rework-communication/RESEARCH_BRIEF.md` (runs 0003–0005). Current typed Plan expects complete content; sparse update support is new work.

