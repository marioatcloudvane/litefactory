# Plan Brief — move-validation-to-the-mode-validate
# Branch: lf/195-move-validation-to-the-mode-validate
# Date: 2026-09-09
# Parent issue: https://github.com/marioatcloudvane/litefactory/issues/195
# Citation base: 405d7df93e362f0c82b17596e5caef796865244b
# Research brief: [RESEARCH_BRIEF.md](RESEARCH_BRIEF.md)
# Status: complete proposal; pending factory publication and plan approval

## Shared Decisions

- The approved [feature-spec.md](feature-spec.md) incorporates the owner's run 0003 answers; [ARCH_REVIEW.md](ARCH_REVIEW.md) approves architecture. Story authority becomes factory-published GitHub issues and supplied frozen revisions. No local story Markdown is authoritative.
- Workflow version is pinned at feature approval. Existing active features and their replans retain the original version; pending legacy effects and already-closed stories retain their semantics. Newly approved features use separated engineering and acceptance.
- Implement retains code, environment setup, tests written/executed, fixes and engineering/save gates. Validate owns acceptance, code, architecture and approved-plan review, consumes retained checks and does not edit code or run/commission tests. Only explicit owner comments start a mode; suggestions require concrete completion evidence or a specific finding.
- Keep engineering completion, acceptance/waiver, remote closure, aggregate/save and merged state distinct. Dependent engineering can progress with child issues open. New engineering evidence is never acceptance authority.
- Historical acceptance records retain original snapshot/revision and do not expire or reopen after later changes; later work belongs in new issues. Initial unavailable/mismatched proof remains unverified. Delivery approval applies to a concrete PR/head, so changed delivery content needs fresh approval without reopening stories.
- An explicit numeric-owner control with reason can waive any substantive criterion, story, review/test or remaining readiness obligation. Expand exact scope durably and retain original facts. Manual ticks/closures, labels and free prose are not authority. Waived is distinguishable from proven; no waiver grants credentials, fabricates check/merge success or implicitly approves a merge.
- Normal readiness requires every attached child closed, including unmanaged children and attached follow-ups, plus managed acceptance rather than inferring it from remote state. Follow-ups default to separately linked issues; concrete contents require owner approval before creation and attachment is explicit. Follow-up creation alone never waives a source obligation.
- Final delivery retains task-branch persistence, proposes a reviewable PR into main and requires explicit approval bound to PR/head before merge. Confirm successful integration into the intended main target before parent closure. Failed effects stay recoverable and visible.
- All agent results are untrusted evidence proposals. The factory owns scope freezing, identities, persisted decisions, body projection, issue creation/closure and PR effects. Record intent before I/O and reconcile uncertain responses; preserve unrelated body edits and report ambiguous requirement conflicts.

## Workflow

- Backend dependency graph: US-001 → US-002 → US-003 → US-004 → US-005 → US-006; US-006 also consumes US-004 directly. This advances from independent engineering completion through review/acceptance and explicit controls to final delivery.
- Instruction dependency graph: US-007 → US-008 and US-007 → US-009. This branch is independent of backend implementation. Shared source is the common predecessor; neither protected-tree sibling depends on the other and there are no reverse edges.
- US-008 runs with Codex and edits `.claude/**` only. US-009 runs with Claude and edits `.codex/**` only. US-007 edits neither protected tree. The owner may switch harnesses between runs; report the required harness for unavailable work while continuing independently ready stories. No sibling requires mixed-tree generation or parity before it finishes.
- Each story gets scoped checks and retained criterion-named evidence at implementation completion. Temporary installed-tree drift is acceptable between independent instruction stories. After all nine stories, Implement runs the final cross-tree/shared-contract consistency gate and `make check`; no premature per-story parity gate may recreate the deadlock.
- This feature's own existing generation follows its currently frozen execution policy; the future new workflow changes behavior for newly approved features. Planning completion does not start Implement or authorize any forge mutation/merge.

## Cross-Story Constraints

- Preserve existing package ownership: store owns SQL/migrations, agent owns neutral contracts, adapters own runner boundaries, turn owns one action, daemon owns time/admission, planbrief owns pure criteria/projection, report owns sanitized presentation, github owns forge API calls and git owns git CLI access. No new service/module or generic workflow engine.
- One SQLite connection, additive embedded migrations, context-first access, no transaction over remote I/O, bounded context propagation, existing daemon serialization/retries and consumer-owned interfaces. Registered Clone remains read-only; use task Worktree for existing saves.
- Preserve immutable numeric identity/revision/snapshot binding and deep-copy runner inputs. Do not grant forge credentials, expand sandbox permissions, write runtime state or `dist/`, or introduce skip-permission flags. Protected-tree edits stay isolated by permitted harness.
- Use meaningful stdlib tests, migrated real SQLite for transaction claims, handwritten fakes, offline `internal/gittest` and HTTP fixtures. No invented integration tags or live forge/coding-agent CI prerequisite. Update intentional API surface tests with new gateway methods.
- Every story carries a criterion-complete evidence inventory. Synthetic fixtures prove parsing/authorization/state transitions, not authentic present-day agent behavior or an actual hosted merge. Missing proof stays unmet/unverified; explicit exceptions accept risk without altering facts. No unsupported nested live Codex observation is required.
- Final aggregate evidence must be saved for subsequent Validate. Actual production follow-up creation/merge success remains a future factory-observed operation after its required owner approval; offline verification does not claim those effects happened.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| US-001 | Finish implementation without accepting or closing stories | [#196](https://github.com/marioatcloudvane/litefactory/issues/196) |
| US-002 | Provide frozen managed scope and check evidence to read-only Validate | [#197](https://github.com/marioatcloudvane/litefactory/issues/197) |
| US-003 | Record validation sign-off and close each accepted story | [#198](https://github.com/marioatcloudvane/litefactory/issues/198) |
| US-004 | Allow documented owner exceptions without fabricating passes | [#199](https://github.com/marioatcloudvane/litefactory/issues/199) |
| US-005 | Create linked follow-up issues only after approval | [#200](https://github.com/marioatcloudvane/litefactory/issues/200) |
| US-006 | Propose an approved PR merge and close the parent after integration | [#201](https://github.com/marioatcloudvane/litefactory/issues/201) |
| US-007 | Publish the shared lifecycle contract and safe instruction rendering | [#202](https://github.com/marioatcloudvane/litefactory/issues/202) |
| US-008 | Update Claude instructions using Codex in an isolated story | [#203](https://github.com/marioatcloudvane/litefactory/issues/203) |
| US-009 | Update Codex instructions using Claude in an isolated story | [#204](https://github.com/marioatcloudvane/litefactory/issues/204) |

## Out of Scope

- Automatic mode cycling, tests or code changes inside Validate, later automatic acceptance invalidation, retroactive conversion of active legacy features, direct pushes to main or automatic merge approval.
- Automatic follow-up creation, treating manual issue edits as acceptance authority, broadening agent permissions, live nested agent capture as an implementation requirement or a new mutating maintenance stage in Validate.
- Committing/pushing this planning run or publishing stories from an agent; `.litefactory-plan-proposal.json` is transient transport consumed solely by the factory.

## Provenance

- Requirements: supplied issue #195 and owner answers in run 0003, same-session research, approved feature spec and architecture review. No current GitHub issue body/identity/base revision was fetched or invented.
- Research repository references were carried from runs 0001–0002; architecture reviewed current owners for planning. Planning citation base is the root-verified HEAD `405d7df93e362f0c82b17596e5caef796865244b`; implementation must check its supplied frozen revision/context and re-pin references before edits if HEAD changes.
- Interpretations: answer 6 rejects manual edits as approval; answer 7 includes every attached child; answer 5 allows explicit readiness exceptions without implicit merge authority. Existing-feature replans retain legacy workflow version. These assumptions resolve scope without another headless question.
- Prerequisites remain implementation and retained scoped/aggregate verification, opposite-harness availability for each protected tree, factory publication/approval, and future explicit delivery approval plus confirmed merge for actual delivery. No unavailable authentic nested-agent evidence is promised or relabeled as verified.
