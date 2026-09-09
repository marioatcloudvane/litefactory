# Research Brief — move-validation-to-the-mode-validate

## Request

[O] Issue #195 asks to separate engineering completion from acceptance sign-off. Implement should finish when all user stories are implemented and their tests are written and run, leaving the sub-issues open. Validate should assess the acceptance criteria, tick those satisfied, and close each fully accepted sub-issue. Once all sub-issues are closed, the parent should be checked and its closure and integration into main proposed. Unmet criteria must be explained in the issue comment; the owner must be able to accept exceptions without indefinite blocking, with the exception documented. Creating a linked follow-up issue should also be possible. Research should identify other activities that can leave Implement, particularly code review.

[O] This is research run 0001, explicitly headless. No clarification or approval was requested. This run produces research artifacts only; it does not implement the requested lifecycle, edit forge state, commit, push, or open Plan mode.

Provenance: `[V]` means verified in this run by the codebase-tour specialist; `[O]` means supplied by the owner. Research interpretations and recommendations are explicitly labelled and are not claims of existing behavior. No earlier-session findings or external claims are carried forward.

## Codebase Context

### System boundaries and current execution

[V] This is a single Go module with executable wiring under `cmd/litefactory` and implementation under `internal/`. The relevant boundaries are mode contracts and evidence (`agent`), polling/scheduling (`daemon`), one-run orchestration (`turn`), durable lifecycle state (`store`), forge operations (`github`), repository operations (`git`), report rendering (`report`), permission profiles (`config`), acceptance parsing (`planbrief`) and distributed instructions (`teamsource`). The daemon owns scheduling; a turn prepares immutable scope, runs the selected agent, harvests output and delivers journaled progress effects. References: `go.mod:1`, `internal/turn/harvest.go:32`, `internal/turn/story_progress.go:39`.

[V] Validate already exists as a bounded, read-only verification mode. `/validate instruction` is actionable, while bare `/validate` is refused as an empty command. However, managed-scope admission rejects every non-Implement mode once approved scope has passed planning, unless the explicit replan route changes its state. Therefore the current command surface does not mean Validate can actually validate an approved managed feature. References: `internal/agent/mode.go:18`, `internal/turn/command.go:211`, `internal/turn/managed_scope.go:36`, `internal/turn/managed_scope.go:64`.

[V] Implement freezes approved GitHub story revisions, receives their complete bodies, ingests per-story evidence during execution, posts evidence comments and closes verified children, then saves/pushes the parent run. Frozen story context is injected only into Implement. Already-completed stories become read-only dependencies; selecting no unfinished stories enters parent aggregate recovery. References: `internal/turn/managed_scope.go:56`, `internal/turn/approved_context.go:20`, `internal/turn/approved_context.go:43`, `internal/turn/approved_context.go:51`, `internal/turn/story_progress.go:428`, `internal/turn/harvest.go:113`.

### Durable lifecycle and evidence

[V] Parent states are `planning`, `freezing`, `implementing`, and `completed`; child states are `planning`, `frozen`, `running`, `blocked`, `verified`, and `completed`. Database CHECK constraints encode these states. There is no explicit implemented-awaiting-validation state and no owner-waived outcome. Lifecycle schema changes belong in embedded SQL migrations. Reference: `internal/store/migrations/0011_managed_stories.sql:5`, `internal/store/migrations/0011_managed_stories.sql:16`.

[V] Progress events support `verified`, `blocked`, and `invalidated`. Verification requires nonempty acceptance, review and test groups, with every check passing and all evidence bound to identical retained implementation bytes. The per-run transport is a bounded `.litefactory-story-events` directory, retired after final ingestion. References: `internal/agent/story_progress.go:15`, `internal/agent/story_progress.go:115`, `internal/agent/story_progress.go:131`, `internal/agent/story_progress.go:149`, `internal/turn/story_progress.go:496`.

[V] The store authorizes each event against selected story scope, approved revision and active implementing generation, and journals accepted bytes plus desired remote effects transactionally before network delivery. Ingestion requires named evidence for every parsed acceptance criterion. Delivery checks numeric parent/child identities, recovers an existing bot-authored evidence comment by marker, persists its identity, then closes verified stories or reopens invalidated ones. References: `internal/store/story_progress.go:98`, `internal/store/story_progress.go:115`, `internal/store/story_progress.go:150`, `internal/turn/story_progress.go:434`, `internal/turn/story_progress.go:446`, `internal/turn/story_progress.go:472`, `internal/turn/story_progress.go:507`.

[V] Acceptance parsing strips both checked and unchecked checkbox prefixes. A checked box is not an independent source of completion authority. Story closure changes state/reason only; it does not tick acceptance boxes, and manual closure does not by itself supply verification evidence. References: `internal/planbrief/acceptance.go:13`, `internal/planbrief/acceptance.go:62`, `internal/github/story_state.go:9`, `internal/github/story_state.go:36`.

[V] Parent database completion requires every managed story completed, successful aggregate save/push, no active progress run and no pending remote effect. This is not GitHub parent closure. Successful harvest saves/pushes worktree output without a Validate-specific exclusion. Existing git synchronization merges the base into the task worktree; it does not integrate a finished task into main. References: `internal/store/story_completion.go:12`, `internal/turn/harvest.go:290`, `internal/turn/harvest.go:340`, `internal/git/merge.go:332`.

### Existing forge capabilities and authority

[V] The dedicated GitHub surface supports issue creation, title/body updates and identity-checked direct-child state changes. It does not expose PR integration or a general parent-close operation. Only the factory owns forge authority and commits/pushes agent output. Existing conflict confirmation is for base synchronization, not approval to integrate a finished feature. References: `internal/github/surface_test.go:67`, `internal/turn/harvest.go:290`, `internal/daemon/confirm.go:14`, `.aiteam/APP_CONSTRAINTS.md:90`.

[V] Validate denies Claude Bash/Write/Edit and gives Codex a read-only sandbox. Its current reviewer cannot publish the existing evidence files or write an artifact unchanged. References: `internal/config/toolprofiles_test.go:43`, `teams/source/agents/go-team/go-code-reviewer.md:11`.

### Instruction sources already disagree

[V] Editable team source already excludes dedicated review, context updates, release and administrative work from Implement. It retains implementation, test creation/execution, bounded fixes, `TEST_REPORT.md`, notes and drift reporting. Team Validate selects the Go reviewer, whose source returns inline severity/location/impact/fix findings and forbids writes, shell/tests, context edits and implementation delegation. References: `teams/source/agents/shared/implement-orchestrator.md:17`, `teams/source/agents/shared/implement-orchestrator.md:29`, `teams/source/index.json:128`, `teams/source/agents/go-team/go-code-reviewer.md:11`.

[V] Legacy checked-in Codex Implement still prescribes a blocking review/re-review loop, environment setup, tests, bug loop, drift report, context update and manifest completion. Team source still asks for local story files, while the factory supplies frozen inline revisions and requires immediate scoped acceptance/review/test evidence. Thus moving review is partly reflected in team source but conflicts with other instruction layers and runtime enforcement. References: `.codex/skills/implement-orchestrator/SKILL.md:268`, `.codex/skills/implement-orchestrator/SKILL.md:400`, `teams/source/agents/shared/implement-orchestrator.md:12`, `internal/harness/stories.go:30`, `internal/agent/story_progress.go:172`.

## Domain / Problem Space

### Distinct meanings of done

Research interpretation: the request introduces three decisions that current completion combines: engineering work is finished; the approved story is accepted or explicitly accepted with exceptions; the parent is ready for integration and closure. A story can be implemented yet still await sign-off. Dependency satisfaction during Implement therefore cannot depend solely on the later GitHub closure. Likewise, Implement finishing must not be confused with the parent being finished in the store or closed on GitHub. This follows from the current context selection and aggregate completion rules cited above.

Research recommendation: keep implementation evidence and acceptance decisions distinct. Preserve the approved story revision and implementation snapshot binding for both, including recovery and invalidation. Validation should account for each criterion as satisfied, unmet, not verified, or explicitly waived by the owner; these are proposed domain distinctions, not existing event values. A failing or unverified criterion must remain distinguishable from a passed check even when the owner chooses to proceed.

### Validation, checkbox projection and owner exceptions

Research interpretation: “tick everything off” expresses owner authority to accept documented exceptions, not permission for an agent to fabricate passing evidence. The research assumes an exception records who authorized it, its reason, the affected criteria/story and relevant revision. Whether that decision appears as a checked box with a waiver annotation, a separate exception block, or both remains a product decision.

Research recommendation: treat checkboxes as a visible projection of a durable acceptance decision. Preserve unrelated issue-body content and distinguish factory status updates from owner edits to frozen scope. The present parser ignores checkbox state, so editing boxes alone cannot implement the requested workflow. Parent readiness should explain outstanding criteria, exceptions and follow-up links, rather than silently treating all closed stories as equivalent successes.

Research interpretation: a follow-up issue records deferred work but does not automatically satisfy the original acceptance criterion. Creating and linking one should be possible through factory-owned operations, with clear owner intent and durable deduplication. Existing issue creation is a building block; an exception/follow-up workflow is not established by that capability alone.

### Activities that can leave Implement

| Activity | Research assessment | Boundary to retain |
|---|---|---|
| Acceptance sign-off and story closure | Owner explicitly requests moving this to Validate. | Factory applies evidence-backed decisions to GitHub; agent assesses. |
| Code review and architectural consistency review | Strong candidate; current team source already assigns read-only review to Validate. | Findings return to implementation for changes; review does not imply code changes. |
| Comparison of implementation against plan and drift assessment | Candidate for Validate when it is inspection/reporting. | Correcting code or modifying approved scope belongs elsewhere. |
| Final readiness report and proposal to integrate/close parent | Natural validation output after accepted stories. | The later integration/closure operation needs its own explicit authorization and success handling. |
| Test writing, execution needed to finish engineering work, and fixes | Retain in Implement under the owner's stated stopping point. | Fresh validation checks are an open choice and must fit permissions. |
| Environment setup needed to execute tests | Retain alongside implementation/testing. | It changes the environment and is not inherently read-only review. |
| Context-document updates | Team source already excludes them; separate maintenance activity is plausible. | Writing documentation is a mutation, unlike assessing documentation drift. |
| Committing/pushing run output | Already factory-owned. | Do not transfer git execution to the agent or confuse task-branch persistence with main integration. |

This table is a research boundary assessment, not a story breakdown or implementation sequence. Verified foundations are the instruction-source and execution references above.

## Constraints & Risks

1. **Admission and scope.** [V] Managed Validate is refused and does not receive the approved story bundle today. Research implication: merely changing prompts would leave the proposed workflow unusable or ungrounded. References: `internal/turn/managed_scope.go:64`, `internal/turn/approved_context.go:20`.
2. **Completion semantics.** [V] State, dependency selection, event acceptance and aggregate completion assume the current implementing generation and completed children. Research implication: deferring only the close API call can stall siblings or parent recovery, or falsely declare validation complete. References: `internal/store/story_progress.go:98`, `internal/turn/approved_context.go:43`, `internal/store/story_completion.go:12`.
3. **Read-only evidence transport.** [V] Current Validate permissions and reviewer forbid the writes used by progress events. Research implication: choose an explicit result transport or tightly bounded factory-owned publication mechanism; do not silently grant broad write/shell permissions. References: `internal/config/toolprofiles_test.go:43`, `internal/agent/story_progress.go:15`.
4. **Authenticity and exceptions.** [V] SEC-21 requires passing scoped evidence; EXT-4 treats missing authentic evidence as unsatisfied and changes to frozen scope as replanning. Research implication: owner waiver authority needs an explicit rule and representation, not a false pass or an untracked relaxation. References: `.aiteam/APP_CONSTRAINTS.md:91`, `.aiteam/APP_CONSTRAINTS.md:255`.
5. **Staleness and recovery.** [V] Snapshot digests, strict bounded JSON, duplicate-key rejection, rooted reads, identity checks and immutable replay handling protect ingestion. Research implication: preserve these across separated runs and define what invalidates acceptance after code changes or base synchronization. References: `internal/agent/story_progress.go:86`, `internal/turn/story_progress.go:260`.
6. **Migration of in-flight work.** Research risk: already-completed stories and previously queued verified-close effects need defined treatment. Replaying an old event under new semantics could unexpectedly close or reopen stories. Existing early-close and recovery tests encode the behavior being changed: `internal/turn/story_progress_test.go:144`, `internal/turn/story_progress_test.go:203`, `internal/turn/story_progress_test.go:253` [V].
7. **Checkbox/body concurrency.** Research risk: projecting acceptance into a live issue body must preserve owner edits and avoid treating factory checkbox updates as unauthorized scope changes. Checked state currently has no completion authority: `internal/planbrief/acceptance.go:62` [V].
8. **Parent delivery is larger than validation.** [V] Existing forge surface lacks general parent-close/PR integration and base synchronization runs in the opposite direction from main integration. Research implication: a validation proposal is a separate scope from safely executing delivery. References: `internal/github/surface_test.go:67`, `internal/git/merge.go:332`.
9. **Instruction drift.** [V] Source team instructions, legacy skills and factory-injected story requirements disagree. Research implication: selecting only one file family risks agent-dependent behavior. References: `teams/source/agents/shared/implement-orchestrator.md:12`, `.codex/skills/implement-orchestrator/SKILL.md:268`, `internal/harness/stories.go:30`.

[V] Existing tests cover early closure, restart/response-loss recovery, mutable-evidence refusal, invalidation, independent-service serialization, complete acceptance coverage, aggregate completion, the managed lifecycle and remote identity checks. References: `internal/turn/story_progress_test.go:144`, `internal/turn/story_progress_test.go:203`, `internal/turn/story_progress_test.go:253`, `internal/turn/story_progress_test.go:348`, `internal/turn/story_progress_test.go:387`, `internal/turn/story_progress_test.go:401`, `internal/turn/managed_lifecycle_test.go:47`, `internal/github/story_state_contract_test.go:57`. These identify regression surfaces; they are not verification of a proposed implementation. No tests were run during this descriptive research.

Research method: the codebase-tour specialist performed its two-pass map and architecture/security/testing scans, read relevant current persistent context/constraint sections, and did not inspect previous session archives. `.aiteam/rulebook.md` was absent. No external factual question was needed, so no web research was performed.

## Open Questions

Plan mode and the owner must resolve these choices; none blocked this headless research run:

1. Does Validate inspect retained implementation test evidence, obtain fresh checks through the factory, or both? Which freshness guarantee is required?
2. What revision/snapshot is signed off, and which later changes, including base synchronization, invalidate which criteria or stories?
3. Are owner exceptions authorized per criterion, per story, or both? Which are genuine waivers and which alter the contract enough to require replanning?
4. Do manual GitHub ticks/closures count as authorized overrides? How are actor, reason and revision recorded without allowing an unrelated edit or third-party closure to confer authority?
5. Does “all sub-issues” mean the frozen managed generation or every attached child, including newly created follow-ups?
6. Does “checked in” mean task-branch commit/push, a final parent readiness check, or main integration? Is final delivery a pull request merge or direct branch operation, and when does parent closure occur relative to confirmed integration success?
7. Is Validate owner-triggered only, proposed after Implement, or automatically dispatched? The research assumes an explicit mode handoff and does not start another mode.
8. What authorizes follow-up creation, and how are retries deduplicated and the original criterion, exception and follow-up linked?
9. How should existing completed stories, frozen generations and queued old close effects behave after migration?
10. Which instruction sources and released team versions must change together to make both supported agents observe the same workflow and artifact contract?

Assumptions used: preserve factory-only forge/git authority; retain coding, tests and necessary fixes in Implement; make validation findings read-only at the agent boundary; interpret owner overrides as documented exceptions; interpret final delivery as a proposal pending an explicit decision, not immediate permission to push main. These are working interpretations of the request, not final requirements.

## Recommended scope for Plan mode

Research recommendation: define the end-to-end distinction between implemented, validated/accepted-with-exceptions, and delivered; identify the authoritative approved scope and implementation evidence for each phase; settle validation admission and read-only result publication; specify criterion projection, owner exceptions, follow-up linking, parent readiness/proposal, and restart/migration behavior. Include consistency across factory prompts, current team sources, legacy Claude/Codex instructions, and applicable constraints. Include the existing review extraction and inspection-only drift assessment so Implement has one clear engineering stopping point.

Keep fresh test execution policy and final integration execution explicit decisions. Do not assume that automatic main integration, a broad permission expansion, new integration-test infrastructure, or a rewrite of unrelated scheduling is necessary. The next mode should produce requirements and an implementation plan from these findings; this research supplies neither code changes nor implementation stories.
