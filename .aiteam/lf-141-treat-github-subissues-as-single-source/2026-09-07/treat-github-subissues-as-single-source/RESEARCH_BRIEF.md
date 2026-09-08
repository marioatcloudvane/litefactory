# Research Brief — treat-github-subissues-as-single-source

## Request

[O] Issue #141 asks to make GitHub sub-issues the sole authority for user stories: supply implementation agents with their context from those issues, use story comments to refine the plan only during Plan, announce that stories are locked after promotion to Implement, close completed stories, and stop keeping duplicate user-story Markdown files in GitHub. The triggering instruction was “Eval this.”

This is a Research run, not authorization to implement or mutate GitHub. The evaluation assumes the parent feature issue remains the coordination point, while child issue bodies hold the complete approved story contract. Temporary execution snapshots are provisionally acceptable if they are derived from GitHub, do not become independently editable authority, and are not committed as story files. These assumptions require a scope decision in Plan; no user response was requested in this headless run.

Provenance: `[V]` verified this run through repository inspection or official documentation; `[O]` owner-reported intent; `[W]` unverified or unresolved external fact; `[R0]` carried from prior research (none used). Recommendations and interpretations below are explicitly identified and are not claims about implemented behavior. Repository citations refer to this worktree; no other session artifacts were read. Specialists inspected code and documentation; no tests or live GitHub mutations were performed.

## Codebase Context

### Existing authority and publication

[V] The implementation workflow expressly consumes local `$session/US-<n>.md` files. Story Index GitHub links are provenance rather than fetch instructions, and specialists receive the file content from their orchestrator. Later test-selection and drift-reporting steps also assume these files exist (`.claude/agents/implement-orchestrator.md:14`, `.claude/agents/implement-orchestrator.md:281`, `.claude/agents/implement-orchestrator.md:393`). Plan requires writing the files before litefactory publishes them and replaces Story Index references with GitHub links (`.claude/agents/plan-orchestrator.md:255`). Workflow instructions have parallel Claude and Codex copies.

[V] After saving a run, publication parses the plan index and reads a Markdown file for each story (`internal/turn/subissues.go:146`, `internal/turn/subissues.go:363`). Matching an existing child by parent/story marker avoids duplicate creation but does not update that child's body (`internal/github/subissues.go:300`). This substantiates the reported synchronization problem: revising the source file does not revise an already-created story issue.

[V] Published bodies contain a business summary, technical excerpt, committed-file permalink and reply guidance. Size handling first drops the technical excerpt and then truncates the summary (`internal/report/subissue.go:215`, `internal/report/subissue.go:235`). The parser requires a title but permits missing architecture content (`internal/planbrief/storyfile.go:14`, `internal/planbrief/storyfile.go:38`). **Current child bodies cannot safely be assumed to contain a complete implementation contract.**

[V] Publication is artifact-driven, not explicitly restricted to Plan. Missing files skip stories; publication failures are logged without changing the run outcome (`internal/turn/subissues.go:114`, `internal/turn/subissues.go:172`, `internal/turn/subissues.go:350`, `internal/turn/subissues_test.go:556`). Best-effort mirroring is therefore part of current behavior, rather than a prerequisite for readiness to implement.

### Task identity, comments and execution context

[V] The daemon discovers issues/comments, dispatches work and advances a persistent polling cursor (`internal/daemon/tick.go:127`). Sessions are unique by repository and issue number; runs attach to sessions and segments rather than durable parent/story execution records (`internal/store/migrations/0001_init.sql:49`, `internal/store/migrations/0001_init.sql:82`).

[V] Open children are discovered as ordinary issues. Owner comments can start independent tasks because comment handling has no story-parent routing or Plan-only guard (`internal/daemon/tick.go:424`, `internal/daemon/tick.go:1304`). Generated story markers prevent the published body from becoming an automatic instruction, but do not suppress subsequent owner comments (`internal/report/marker.go:89`). Existing comment authorization precedes instruction dispatch and consumes self/third-party comments without executing them (`internal/daemon/tick.go:1313`). Any new child routing must preserve that admission boundary.

[V] Mode labels select a run and are removed afterward. Their persistence records trigger-specific routing and cleanup, not an enduring “promoted to Implement” state (`internal/daemon/tick.go:1490`, `internal/daemon/tick.go:1650`, `internal/store/migrations/0004_mode_selections.sql:1`). A durable story freeze cannot be inferred from the presence or absence of a current label.

[V] Turn preparation supplies the current task issue, triggering comment, history and attachments; it does not fetch related story bodies (`internal/turn/prepare.go:235`). Resume prompts intentionally omit issue context supplied previously (`internal/harness/prompt.go:208`). A future fetch path must explicitly deliver the relevant approved story revision even when resuming an agent session; merely changing initial prompt assembly would leave stale-context cases.

### Completion and visibility

[V] Agent results describe subprocess outcome and final text, without structured completed-story identities (`internal/agent/result.go:67`). Reports target the current task issue; no inspected path fans parent implementation results out to stories (`internal/turn/report.go:566`). Closing issues currently means stop processing them (`internal/daemon/tick.go:425`). Consequently, process success alone cannot establish which individual story met its acceptance conditions, and a requirement to explain late comments on closed stories conflicts with current discovery behavior.

### Existing foundations and test coverage

[V] The module uses Go 1.26 and `go-github/v90` (`go.mod:1`). GitHub integration already supports reading issues, listing children, creating issues and attaching children. Its enumerated surface lacks body-update, close/reopen, lock/unlock and parent-read methods (`internal/github/surface_test.go:67`). Consumer-side interfaces separate turn publication from the SDK (`internal/turn/subissues.go:36`). Stable parent/story markers support retry deduplication (`internal/report/marker.go:73`).

[V] Existing tests cover duplicate creation, partial failures, attachment recovery, pagination and rate limiting (`internal/github/subissues_test.go:412`, `internal/github/subissues_test.go:480`, `internal/github/subissues_test.go:534`, `internal/github/subissues_test.go:563`, `internal/github/subissues_test.go:664`, `internal/github/subissues_test.go:801`); missing files, permalinks and nonfatal publication errors (`internal/turn/subissues_test.go:431`, `internal/turn/subissues_test.go:473`, `internal/turn/subissues_test.go:556`); and reply text, truncation and redaction (`internal/report/subissue_test.go:85`, `internal/report/subissue_test.go:314`). These provide seams for later work but currently protect several behaviors this feature would intentionally change. Tests were inspected, not run.

## Domain / Problem Space

### Feasibility and GitHub capabilities

[V] GitHub REST supports listing a parent's sub-issues and reading a child's parent. Listing is paginated (30 by default, up to 100 per page); relationship writes take a child issue ID, not its display number. Private-resource reads require fine-grained Issues read permission, and relationship writes require Issues write. [Sub-issues REST reference](https://docs.github.com/en/rest/issues/sub-issues)

[V] A child remains a regular issue: read its current body through the issue endpoint, update it with PATCH, and close it with state `closed` and state reason `completed`. Issue updates require the relevant write access. [Issues REST reference](https://docs.github.com/en/rest/issues/issues)

[V] GitHub supports 100 direct children per parent and eight nesting levels. GraphQL also exposes parent/child relationships and issue update/closure operations. Whether litefactory supports nested or cross-repository stories should be an explicit scope boundary rather than inherited accidentally from platform capability. [Adding sub-issues](https://docs.github.com/en/issues/tracking-your-work-with-issues/using-issues/adding-sub-issues), [GraphQL Issues reference](https://docs.github.com/en/graphql/reference/issues)

[V] Documentation checked on 2026-09-07 renders REST examples using version 2026-03-10, including when requested with an older version parameter. Compatibility of every needed endpoint with this repository's configured request version was not tested. The existing list/attach implementation establishes some repository support, not proof that every proposed operation is already supported.

### “Locked” requires two different decisions

[V] GitHub conversation locking **does not prevent the owner, collaborators or people with write access from commenting**. It generates a visible timeline event and restricts ordinary participants. Thus it does not enforce the owner's requested silence during Implement. [Locking conversations](https://docs.github.com/en/communities/moderating-comments-and-conversations/locking-conversations)

[V] Lock/unlock REST endpoints exist and require the documented access. Native lock reasons are limited to `off-topic`, `too heated`, `resolved` and `spam`; none represents “implementation in progress.” A separate explanatory notice would be needed. [Issues REST reference](https://docs.github.com/en/rest/issues/issues)

[W] No documented mechanism was found to freeze an issue body against its owner/admin. GitHub documents conversation locks separately from issue editing; body immutability must not be promised from conversation locking. [Locking conversations](https://docs.github.com/en/communities/moderating-comments-and-conversations/locking-conversations), [Issues REST reference](https://docs.github.com/en/rest/issues/issues)

Recommendation: distinguish **processing policy** (later comments cannot alter the active implementation) from **GitHub conversation moderation** (optional restriction on who can comment). Treat the first as necessary. A recorded approved revision is compatible with GitHub being the authoring authority if it is an execution record rather than a competing editable specification. Announce that revision and the policy visibly; decide separately whether to invoke native locks. Never silently claim the UI prevents owner edits or comments.

### Detecting changes and avoiding lost updates

[V] Comments belong to an individual issue. Per-issue and repository-wide comment endpoints support pagination and `since` based on last update; repository-wide responses identify their issue through `issue_url` and can sort by update time. [Issue comments REST reference](https://docs.github.com/en/rest/issues/comments)

Inference: child activity must be explicitly observed through child polling, repository-wide comment filtering or corresponding events. Polling the parent's comments alone does not provide a documented child-discussion feed. [Issue comments REST reference](https://docs.github.com/en/rest/issues/comments)

[W] No authoritative guarantee was found that child activity changes the parent's `updated_at`, or that issue `updated_at` is a body-only revision identifier. GraphQL exposes edit timestamps, but these do not establish an atomic update precondition. [GraphQL Issues reference](https://docs.github.com/en/graphql/reference/issues)

[V] GitHub does not support conditional requests for unsafe methods unless the endpoint explicitly documents support. Issue PATCH documents no revision precondition. A read/check/write sequence can detect earlier drift but cannot guarantee against a concurrent edit between checking and updating. [REST best practices](https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api), [Issues REST reference](https://docs.github.com/en/rest/issues/issues)

Recommendation: retain the fetched body/revision identity used for execution, define how drift is reported and reapproved, and avoid presenting an application comparison as an atomic GitHub lock. Decide whether agent-proposed edits replace the whole body or update a defined section, and how manual edits are preserved.

[V] Standard authenticated personal REST access normally permits 5,000 requests/hour; installation-token and enterprise rules vary. Conditional authenticated GET responses of 304 avoid primary quota consumption, while secondary limits still apply. Story fan-out should account for pagination, rate-limit headers, and serialized mutation traffic. [REST rate limits](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api), [REST best practices](https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api)

## Constraints & Risks

1. **Incomplete existing authority.** The current issue representation can omit required technical content. Removing files before reconciling legacy bodies risks irreversible loss of implementation context. Promotion should not proceed against a missing, inaccessible, truncated or structurally incomplete story.
2. **Lifecycle is absent, not merely hidden.** Run-scoped mode routing cannot express durable draft/approved/in-progress/completed story states. The parent-child association, selected revision, run identity and completion evidence need durable semantics, with writes owned by the store package.
3. **Concurrent planning and implementation.** A child Plan comment can race parent promotion, and a human can edit the issue during a run. Scope must specify which revision wins and what happens to queued comments; GitHub locking does not solve this.
4. **Authority cannot rely on editable markers alone.** Preserve authorized-comment admission and verify actual parent/child relationships plus repository identity. Body markers and Story Index URLs are useful locators but can be edited, duplicated or removed.
5. **Best-effort publication is insufficient.** A successful parent run can currently leave absent or stale children. The source-of-truth workflow needs a visible readiness/failure policy, including safe recovery after creation succeeds but attachment fails.
6. **Completion requires evidence.** Closing every child after a parent subprocess exits successfully could misreport partial work or failed review/testing. Define per-story success and require the chosen gates before closure. A failed close request must be retryable without reimplementing the story.
7. **Closed-story visibility changes discovery.** Current closed-issue skipping prevents processing late comments. Decide whether an initial lock notice is sufficient or whether completed stories must remain observable for subsequent explanations and reopening.
8. **Workflow parity and context refresh.** Plan, implementation, specialist inputs, tests, report links and both agent instruction copies rely on files. Removing only publication would leave implementation broken; refreshing only first-run context would miss resumed agents.
9. **Access and redaction.** Existing prompt code treats issue/comment content as untrusted (`internal/harness/prompt.go:38`); issue creation uses redacted bodies (`internal/github/issues.go:367`, `internal/report/subissue.go:175`). New fetched context and outbound updates must preserve those boundaries. Ordinary tasks should not gain story routing merely because their body resembles a marker.

### Applicable normative rules

[V] The tour read the relevant sections of `.aiteam/APP_CONSTRAINTS.md`; no `.aiteam/rulebook.md` exists. These constraints apply to later implementation:

| Rules | Constraint and implication | Source |
| --- | --- | --- |
| SEC-2, SEC-4, SEC-9 | Deliver fetched story text through stdin, retain the environment allowlist, and use the GitHub integration rather than adding `gh` calls outside identity discovery. | `.aiteam/APP_CONSTRAINTS.md:56`, `.aiteam/APP_CONSTRAINTS.md:68`, `.aiteam/APP_CONSTRAINTS.md:99` |
| SEC-5, SEC-6, SEC-8 | Validate/redact outbound story bodies, completion reports and lock explanations; retain numeric-identity authorization. | `.aiteam/APP_CONSTRAINTS.md:75`, `.aiteam/APP_CONSTRAINTS.md:84`, `.aiteam/APP_CONSTRAINTS.md:96` |
| SEC-7, CFG-8 | Temporary story material belongs outside the registered clone; new runtime paths are owned by `config.Paths`. | `.aiteam/APP_CONSTRAINTS.md:91`, `.aiteam/APP_CONSTRAINTS.md:143` |
| TEST-2, TEST-3, TEST-5 | Extend handwritten fixtures and consumer-owned interfaces; do not introduce a mocking framework or module. | `.aiteam/APP_CONSTRAINTS.md:155`, `.aiteam/APP_CONSTRAINTS.md:160`, `.aiteam/APP_CONSTRAINTS.md:171` |
| TEST-6, TEST-8, TEST-9 | Exercise durable transitions with real SQLite; coordinate race tests with channels and `Daemon.Quiesce`, without sleeps or invented lifecycle observables. | `.aiteam/APP_CONSTRAINTS.md:176`, `.aiteam/APP_CONSTRAINTS.md:185`, `.aiteam/APP_CONSTRAINTS.md:189` |
| TEST-10 | Changes to the pinned GitHub method surface are architectural changes requiring deliberate test updates. | `.aiteam/APP_CONSTRAINTS.md:195` |
| CONC-1, CONC-2 | Copy collection boundaries for concurrent handoff and keep any additional workers bounded with a documented lifecycle. | `.aiteam/APP_CONSTRAINTS.md:206`, `.aiteam/APP_CONSTRAINTS.md:218` |
| CONC-3, CONC-4 | Preserve reportable failures as results. A required lock-refusal report must post successfully before its reporting transaction advances the watermark. This is database bookkeeping, not a git commit. | `.aiteam/APP_CONSTRAINTS.md:228`, `.aiteam/APP_CONSTRAINTS.md:233` |
| CONC-5, CONC-6, CONC-7 | Store accessors take context first; retry/backoff belongs to the daemon; failed preparation must leave session state untouched. | `.aiteam/APP_CONSTRAINTS.md:236`, `.aiteam/APP_CONSTRAINTS.md:237`, `.aiteam/APP_CONSTRAINTS.md:240` |
| STYLE-7, STYLE-8 | Read affected package contracts before coding; keep any literal prompt-mode-header changes aligned with dispatcher instructions. | `.aiteam/APP_CONSTRAINTS.md:286`, `.aiteam/APP_CONSTRAINTS.md:289` |

[V] The rulebook contains stale API inventory: SEC-6 describes `PostComment` as the sole redacted-body consumer with no exported extraction method, while current code uses `Body.String()` and accepts redacted bodies in issue creation (`internal/github/issues.go:373`, `internal/github/issues.go:392`). Its enforcement table still names exactly seven endpoints (`.aiteam/APP_CONSTRAINTS.md:323`), while the pinned surface is larger (`internal/github/surface_test.go:67`). Preserve the security requirement and resolve the stale wording during planning; do not mistake the old inventory for actual coverage. Intentional normative deviations require documentation under the amendments rule (`.aiteam/APP_CONSTRAINTS.md:20`). Research made no deviation or rulebook change.

## Open Questions

Plan mode should resolve these without treating the research assumptions as accepted requirements:

- Does parent promotion freeze all stories, or does each story freeze when dispatched? What durable event records promotion?
- Should child Plan comments run in the parent session/worktree or in a child session supplied with shared parent context? Who may approve story-body changes?
- Is the required lock an application refusal with a visible notice, optional native conversation locking, or both? GitHub cannot guarantee that owners stop commenting or editing.
- What counts as finished: story acceptance plus review/tests, saved and pushed work, or merged work? Which machine-readable outcome proves it?
- Are derived, uncommitted snapshots acceptable? What approved revision is shown on a retry or resume after manual issue edits?
- How are legacy truncated stories reconciled before deleting their tracked files? What happens to old file permalinks and parent indexes?
- Are nested, cross-repository, manually attached or reordered children included? Are closed, detached or deleted stories ignored, errored or reconciled?
- How should conflicting Plan edits, promotion races, failed attachments, failed notices and failed close requests recover without duplicate work or overwriting human changes?
- Must closed stories receive fresh lock explanations for later comments, and how can a completed story be reopened for a new planning cycle?
- What run/progress information belongs on each child and at what cadence? The problem statement asks for visibility but does not define milestones.
- What minimum GitHub API version and token permissions will be supported? GraphQL-specific authorization and live endpoint behavior were not verified.

## Recommended scope for Plan mode

The feature is feasible and addresses a verified duplication problem, but it is a workflow and persistence change rather than a switch from one input path to another.

Plan should define an authoritative complete story-body contract, stable parent/story identity, Plan-only revision workflow, durable promotion and execution revision semantics, explicit child-comment routing, refreshed implementation inputs, per-story progress/completion reporting, retryable GitHub updates, and migration away from tracked story files. Preserve a parent planning index if useful, but make it a reference/index rather than a second story specification. Include the corresponding Claude/Codex instruction changes and the tests affected by replacement of best-effort mirrors.

Keep the initial scope focused on this repository's existing parent-feature workflow. Treat generalized project management, automatic approval/merge policy and support for every GitHub nesting pattern as separate decisions. Do not promise immutable GitHub issue bodies or absolute prevention of owner comments. This run ends with research artifacts; no implementation plan, code change, GitHub update, commit or push was performed.
