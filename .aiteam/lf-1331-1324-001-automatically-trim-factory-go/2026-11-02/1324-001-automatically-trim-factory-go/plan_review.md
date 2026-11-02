AI;DR: The complete one-story Plan now has five observable criteria and seven linked tasks. Review this acceptance proposal through the existing third-stage gate; implementation has not started.

Uncovered original outcomes: O-01, O-04, O-05, O-07, O-08, O-11, O-12, O-17, O-18, O-19, O-20, O-24 are explicitly removed by the owner. Retained outcomes below cover only the portions allowed by the Rulings.

Ruling: O-01, O-04, O-05, O-07, O-08, O-11, O-12, O-17, O-18, O-19, O-20, O-24 — removed from this delivery and recorded in N-06 — owner comment 6571785897 accepted scope reduction after the one-round containment gate failed — cost: no automatic disk-space recovery; operator intervention remains necessary.

Ruling: O-02, O-03, O-06, O-09, O-10, O-13, O-14, O-15, O-21, O-22, O-23 — retain cache routing/configuration, path and ownership validation, filesystem diagnostics, existing disk-hold visibility and related verification/documentation only; removed portions are explicit in N-07 — owner comment 6571785897 — cost: diagnostics explain pressure but cannot claim cleanup or safe idleness.

Ruling: the ownership portions of the preceding Ruling and N-07, including cross-home exclusion in O-23, and the operator recovery-action portion of O-10 are superseded by A-03 and N-09. Retain path validation, home-specific defaults and local main/reviewer separation; do not assert exclusive ownership. Cost: intentionally conflicting custom configurations are not detected across factory homes. Cache-size reporting is an observation, not reclaimed-space accounting.

| O-ID | Story |
| --- | --- |
| O-16 | story-001 |
| O-02, O-03, O-06, O-09, O-10, O-13, O-14, O-15, O-21, O-22, O-23 | story-001, narrowed by the Rulings above |
| O-01, O-04, O-05, O-07, O-08, O-11, O-12, O-17, O-18, O-19, O-20, O-24 | None; owner removed automatic deletion and its lifecycle |

Parent link-only story index: [story-001 — Isolate factory Go caches and explain disk-pressure holds](https://github.com/marioatcloudvane/litefactory/issues/1331).

Active scope: exactly story-001; no story dependency edges or external delivery dependencies; no retired stories or replacements are supplied. #1308 stays independent. Shared decisions remain unchanged: dedicated main/reviewer routing, existing admission rules, truthful nonblocking observations, no factory deletion, ownership registry or recovery workflow. Settled intent, architecture, guidance and assumptions are retained.

story-001 — M. Supplied reason: “Cache routing, permissions, admission, cleanup and status change together; user exclusion and restart safety need care. Existing surfaces bound the work; routing moves to cold dedicated caches, with no data migration or host-cache retirement.” This is historical sizing; owner scope cuts remove cleanup and ownership work without replacing the promoted set.

The story sub-issue holds the complete typed intent, scope, design, criteria and task descriptions. Its delivery order is T-01 → T-02/T-03 → T-04 → T-05 → T-06; T-07 follows T-04. Configuration/routing and the sampler converge in existing persisted health/status views. AC-001 uses the worker-written end-to-end main-path test; AC-002 through AC-005 are judged in Validate, supported by task evidence. Every criterion has a linked delivery task. The required large-cache fixture checks progress beyond 100,000 entries, completion and responsiveness; it cannot prove an atomic live size or interrupt a blocked filesystem call.

No material owner question remains. All N-01 through N-09 and K-01 through K-04 remain on the story, with the recorded superseding Rulings. Disk pressure still needs operator intervention; custom caches have no cross-home exclusivity guarantee. No implementation, tests, acceptance judgment or remote publication is claimed. Exact acceptance/assembled-plan approval remains with the owner in this third stage. After the factory completes that gate and saves the Plan, the recommended next action is `/implement <instruction>`.
