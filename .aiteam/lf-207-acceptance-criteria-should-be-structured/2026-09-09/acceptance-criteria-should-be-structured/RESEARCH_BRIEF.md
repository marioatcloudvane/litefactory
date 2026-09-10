# Research Brief — acceptance-criteria-should-be-structured

## Request

Research issue `github:marioatcloudvane/litefactory#207`: replace agent-authored Markdown that the factory subsequently interprets as acceptance obligations with explicit structured items and factory-owned rendering. Include the triggering comment's scope extension: the factory should construct story-progress identity envelopes from its existing frozen context while the agent supplies the outcome and evidence. The follow-up prioritizes factory construction of the identity envelope and independently useful live field/type feedback. Investigate serial error discovery across costly reruns and whether comprehensive preflight plus acknowledged correction can resolve it within one run.

This is Research run 0005 (2026-09-10), continuing runs 0001–0004 on branch `lf/207-acceptance-criteria-should-be-structured`. The owner approves the full structure, evidence Option B, criterion-ID rules, and feedback/compatibility direction. The amendment is to recommend commenting desired changes on each story so litefactory infers and applies the update, while preserving direct inline edits. The existing 2026-09-09 session is reused. This run records those approvals and a concrete story-footer proposal; it does not implement, repair runtime state, commit, push, change mode, or wait for confirmation.

### Provenance and boundaries

- `[V]` means instructions inspected in the named run or explicitly cited bounded run 0004 findings, not validation of the proposed design. Production-code findings outside the explicitly identified run 0004 reinspection are `[R0]`, carried forward from runs 0001 and 0002, including their historical citations; run 0002 subsections identify the findings then reverified by its delegated codebase tour. `[O]` means owner-supplied incident evidence, not independently reproduced; `[W]` denotes other unverified second-hand material. Recommendations and inferences are not verification claims.
- Recommendations and inferences below are explicitly identified and are not existing behavior. Owner approvals are recorded separately under run 0005; those approvals supersede earlier pending-review wording without making the proposed protocol current code. No fresh code inspection was performed in run 0005; named earlier-run findings and citations retain their original provenance.
- Run 0001 covered the Plan proposal/publication/freeze path, story-event contracts and ingestion, and their relevant tests and constraints. Run 0002 targeted per-event error reporting, live ingestion versus author feedback, runner channels, and same-run correction semantics; original Plan findings are retained below. It did not inspect runtime databases, daemon logs, live GitHub content, or other research sessions. There were no external factual dependencies, so no web research was needed.
- No production files were changed and no tests were executed. Source inspection verifies the failure mechanisms, not the cited historical runs or a proposed fix.

## Codebase Context

### Ownership and end-to-end paths

[R0] This is a single Go 1.26 module. Protocol types belong to `internal/agent`, proposal validation to `internal/planbrief`, publication rendering to `internal/report`, orchestration to `internal/turn`, daemon polling to `internal/daemon`, and database persistence to `internal/store` (`go.mod:1`; `internal/agent/story_progress.go:29`; `internal/turn/plan_publication.go:48`; `internal/store/story_progress.go:21`).

[R0] The relevant Plan path is: managed Plan prompt → transient JSON proposal → validation after successful agent return → sanitized story publication → link-only index → readiness checks → frozen issue title/body revision for Implement. Completion evidence is checked against the frozen body, not an agent's local story file (`internal/turn/plan_handoff.go:74`; `internal/turn/plan_publication.go:144`; `internal/turn/managed_scope.go:140`; `internal/turn/story_progress.go:293`).

[R0] Story events take a separate path: approved bundle and shared instructions → agent-authored immutable event files → polling ingestion while workers remain occupied → validation and persisted acceptance/rejection → story effects. Finish drains transport and ends unverified scope before retiring files (`internal/harness/stories.go:10`; `internal/daemon/tick.go:149`; `internal/turn/story_progress.go:162`).

### Plan proposal and acceptance obligations

[R0] Proposal version 1 already has structured story entries, but each entry holds a single free-form `body`. Dependencies also have a separate structured representation. Limits include 8 MiB per proposal, 100 stories, and 65,536 bytes per body (`internal/planbrief/proposal.go:15`; `internal/planbrief/proposal.go:29`; `internal/planbrief/proposal.go:80`). The change is therefore a refinement of an existing structured protocol, not the introduction of the first JSON proposal.

[R0] Body validation requires five nonempty sections and invokes the acceptance parser. It does not semantically parse task descriptions, architecture prose, or dependency relationships from their sections (`internal/planbrief/proposal.go:159`). The issue's broader statement that every required section is reparsed should be understood with this distinction: section presence is validated across the body, but acceptance text specifically becomes a machine-enforced obligation set.

[R0] The acceptance parser assigns each nonempty line either its leading `AC` identifier or its stripped text as the criterion name. Its identifier expression accepts `AC` followed by dot-, underscore-, or hyphen-delimited alphanumeric segments. It does not impose criterion-name length or criterion-count bounds (`internal/planbrief/acceptance.go:10`; `internal/planbrief/acceptance.go:70`).

[R0] Evidence independently has a maximum of 256 checks per group and 256 bytes per check name (`internal/agent/story_progress.go:143`). Completion reparses acceptance names from the frozen body and requires their coverage; that coverage function does not reject extra evidence names (`internal/turn/story_progress.go:507`).

**Inference:** a proposal can pass Plan validation while creating obligations that cannot be represented by valid completion evidence. Both long accidental prose names and excessive criterion counts cross this boundary. Restricting only Markdown syntax would leave the producer/consumer contract mismatch intact.

[R0] The managed prompt directs the agent to author the complete body and align evidence with parser-derived names (`internal/harness/prompt.go:410`). The factory currently sanitizes/redacts the body, inserts its marker, closes fences, and refuses truncation; it does not build the required sections from typed items (`internal/report/complete_story.go:11`). Plan validation occurs after successful runner return, so the present proposal path also lacks validation at the moment the agent authors the payload (`internal/turn/plan_handoff.go:75`).

### Publication, owner edits, and frozen authority

[R0] Publication verifies parent/generation, existing story identities, current base hashes, and completed-story immutability. It stores dependency metadata separately from the body hash (`internal/turn/plan_publication.go:58`; `internal/turn/plan_publication.go:108`; `internal/turn/plan_publication.go:136`; `internal/turn/plan_publication.go:151`). Rendering dependencies from structured input can reduce divergence, but Plan must account for both existing representations.

[R0] Owner edits are already guarded: promotion fails when the current live title/body hash differs from the published hash, with guidance to refresh and republish the parent Plan so body and dependencies are approved together (`internal/turn/managed_scope.go:140`). It is inaccurate to assume arbitrary owner edits are currently accepted transparently at promotion.

[R0] Approved story revisions retain the complete body and hash; live issue edits cannot replace the frozen revision for an active Implement scope (`internal/store/approved_stories.go:12`; `internal/store/approved_stories.go:69`). GitHub title/body is the sole editable story source, and factory-only forge access plus frozen generations are normative constraints (`.aiteam/APP_CONSTRAINTS.md:90`; `.aiteam/APP_CONSTRAINTS.md:198`).

**Inference:** declaring structured criteria canonical only in a transient proposal would not, by itself, eliminate later reconstruction from Markdown. The approved/frozen representation and the editable GitHub source need an explicit relationship. Persisting structured criteria beside the issue without defined revision reconciliation risks two conflicting sources of truth. A versioned machine-readable representation in the issue or a validated derived projection are possible design directions for Plan to assess, not decisions made by this research.

### Story-progress envelope and evidence ingestion

[R0] The approved bundle already contains all five identities raised in the comment: parent task reference, run ID, generation, story issue ID, and story revision. It also contains numeric `parent_id`, so several parent-shaped values really are available to the agent (`internal/agent/stories.go:6`; `internal/agent/stories.go:21`).

[R0] The event contract nevertheless asks the agent to copy identities and author sequence, attempt, outcome, and evidence. Its example uses `"parent":"supplied parent"`; the actual parent field is a string (`internal/agent/story_progress.go:31`; `internal/agent/story_progress.go:172`). A numeric parent fails typed JSON decoding before scope validation (`internal/agent/story_progress.go:97`).

[R0] Ingestion then independently validates exact frozen identities and selected-story authorization, required acceptance coverage, snapshots, and store acceptance (`internal/turn/story_progress.go:277`; `internal/turn/story_progress.go:283`; `internal/turn/story_progress.go:315`). Factory-supplied envelopes would prevent copying mistakes but must preserve these checks; supplying a valid envelope is not evidence that work was verified.

[R0] Rejected events are not physically discarded: the store retains raw bytes and digest as a rejected event, consumes the sequence, and withholds claimed story identity (`internal/store/story_progress.go:273`). Rewriting the same rejected sequence file does not retry it; ingestion skips the previously processed sequence (`internal/turn/story_progress.go:264`). Corrections therefore require a later sequence under the current protocol.

[R0] Rejection errors reach daemon logs, but the inspected ingestion path writes no agent-visible receipt. Existing typed decode errors already identify the offending field and expected type. The missing part of the suggested fallback is timely delivery to the author and a correction protocol, not solely better decoder wording (`internal/agent/story_progress.go:97`; `internal/daemon/tick.go:152`; `internal/turn/story_progress.go:162`).

[R0] Event files are immutable and consecutively numbered across stories; attempt is per story. Any publisher or receipt mechanism must fit those ordering, replay, recovery, and invalidation semantics (`internal/agent/story_progress.go:173`). Prefilling five fields alone leaves sequence allocation, story selection, and attempt ownership unresolved.

### Run 0002: serial validation and the missing feedback loop

[R0] Both reported type mismatches match the current contract: `StoryEvent.Parent` is a string; the evidence text inside each `StoryCheck` is also a string, although acceptance/review/tests are arrays of checks (`internal/agent/story_progress.go:29`, `:45`, `:62`). The instructions provide examples, not an author-callable validator (`internal/agent/story_progress.go:172`). The approved JSON bundle contains all five requested factory-known envelope identities and also numeric parent identities that can be confused with the task reference (`internal/agent/stories.go:6`, `:21`; `internal/harness/stories.go:23`). A mutable prefilled template reduces copying but does not prevent accidental edits; factory construction is a stronger ownership boundary.

[R0] `ParseStoryEvent` checks size and duplicate keys, calls one typed JSON decode, and returns immediately on decode failure. Semantic validation follows only successful decoding (`internal/agent/story_progress.go:87`). The installed Go decoder stores the first error, not a collection of all incompatible fields (`/opt/homebrew/opt/go/libexec/src/encoding/json/decode.go:247`; local toolchain source, not a repository artifact). Semantic validation similarly returns early through manifest/file/group/check loops. Some failures collapse multiple causes into “check does not verify the scoped snapshot” without identifying which evidence group failed (`internal/agent/story_progress.go:132`). Ingestion does aggregate errors across separate files, using `errors.Join`, but that does not enumerate independent defects within one event (`internal/turn/story_progress.go:333`).

[R0] Field and expected type are available for typed decode failures, but the exact indexed path in the owner-provided error was not reproduced. The local decoder maintains a struct-field stack and its array loop does not add an index to that stack (`/opt/homebrew/opt/go/libexec/src/encoding/json/decode.go:255`, `:542`). **Inference:** a stable diagnostic contract with JSON paths such as `evidence.acceptance[0].evidence`, expected type and observed type should be explicit; simply forwarding native wording does not establish index precision or comprehensive diagnostics. Safely collecting independent errors also needs boundaries: malformed syntax or wrong container types can make dependent semantic checks meaningless.

[R0] Validation is already possible while a worker is running: daemon ticks reconcile progress with workers occupied, although a backoff return can precede polling (`internal/daemon/tick.go:142`, `:149`). Final harvest also drains progress after `runClaude` returns (`internal/turn/harvest.go:47`). **Correction to the incident's generalized timing claim:** repository validation is not exclusively post-run. The observed incidents describe when failures became visible to the owner; absent author feedback allows a run to finish without learning either a live or finish-time rejection.

[R0] The shared request offers a fixed prompt and outgoing usage capture, with no incoming validation-feedback channel (`internal/agent/request.go:51`). Both runner adapters use `strings.NewReader(req.Prompt)` as stdin; their stdout is consumed into results/logs rather than a duplex conversation (`internal/harness/runner.go:687`; `internal/codex/runner.go:297`). A new log message alone cannot reach the active author. A filesystem receipt requires instructed polling/wait behavior; an author-callable validator requires an executable or tool surface delivered into the run. Neither mechanism exists in the inspected event path.

[R0] Rejection persists raw bytes/digest and a rejected marker, but no diagnostic reason; its store API has no diagnostic parameter (`internal/store/story_progress.go:273`). Corrections must use a new global event sequence because previously processed files are skipped, including rewritten rejected files (`internal/turn/story_progress.go:264`). Rejection advances `last_sequence`, but it does not advance the claimed story attempt: rejected rows have blank authoritative story identity and attempt zero. Acceptance permits the previous story attempt or the next one, so a first rejected submission may be corrected at sequence 2 while retaining attempt 1 (`internal/store/story_progress.go:139`). A new run is not inherently required by the journal protocol.

[R0] The current suite already tests rejection, correction and acceptance within one run: restore rejected snapshot bytes, publish sequence 2, then close the story (`internal/turn/story_progress_test.go:253`). Held-parent tests exercise live acceptance for both configured agent selections (`internal/turn/story_progress_test.go:144`). These establish journal/lifecycle capabilities, not an author-visible feedback loop. The malformed contract table lacks both exact reported type mismatches and checks rejection rather than actionable diagnostic detail (`internal/agent/story_progress_contract_test.go:84`).

[R0] Original Plan findings remain relevant on targeted reinspection: proposals carry free-form `Body` with separate dependency data; the acceptance parser turns nonempty lines into obligations without matching evidence-name/count bounds; other required sections are checked for nonempty presence (`internal/planbrief/proposal.go:37`, `:159`; `internal/planbrief/acceptance.go:70`). The prior publication, owner-edit and compatibility findings below remain carried forward unless explicitly reverified above.

### Run 0003: current instructions and interpretation limits

[V] The supplied replacement `AGENTS.md` and inspected `teams/source/skills/workflow/SKILL.md` distinguish persisted legacy workflow from separated Implement/Validate. Legacy retains immediate scoped acceptance and affected invalidation. Separated Implement retains engineering/test evidence and releases dependent implementation while acceptance remains open; Validate reviews retained evidence without fresh tests. The factory alone projects acceptance and closure. Historical acceptance in separated workflow does not expire because later code/main changes. Therefore the earlier descriptions of immediate closure and invalidation are historical/legacy context, not a proposal to impose those effects on every workflow version.

[V] The shared workflow also requires independently owned instruction changes: a Codex story may update `.claude/**`, and a Claude story `.codex/**`; shared/root sources are outside both trees. Cross-tree parity is an aggregate feature obligation. Plan must account for both harnesses without making independent story readiness depend on editing the other protected tree. These are constraints, not owner choices to reopen.

[V] `.claude/modes/session.md` directs reuse of an existing supplied session across dates and permits continuing Plan discussion; entering Plan does not require every decision in this inventory to have been answered. The current Research artifact is the only substantive document changed. No fresh production-code inspection or tests were needed for this decision consolidation; the prior code findings remain carried forward and should be revalidated during Plan, especially version-sensitive event consumers.

## Domain / Problem Space

### Reported incidents and what this run establishes

[O] The issue reports that #170 acquired an unintended obligation from a trailing “Evidence contract…” paragraph; its derived name exceeded evidence limits and blocked verification. No historical payload was inspected. [R0] The current parser and evidence limits establish that this class of failure is possible (`internal/planbrief/acceptance.go:70`; `internal/agent/story_progress.go:143`).

[O] The triggering comment reports that run 282 for US-009/#204 used numeric parent `5399889036`, while accepted run 268 for US-007 used parent task reference `github:marioatcloudvane/litefactory#195`. It reports clean agent completion, eight passing scoped checks, a rejected event, and blocked completion. These run-level facts and the cited log line remain unverified in this research. [R0] The current typed field and decode path explain the reported rejection mechanism (`internal/agent/story_progress.go:31`; `internal/agent/story_progress.go:97`).

**Inference:** the common failure is an authoring boundary that permits avoidable mistakes before downstream acceptance. The two parts need different concrete remedies. Criteria need explicit semantic IDs plus consistent producer/consumer limits; event identities are already factory-known and can be removed from the agent's authoring responsibility. Neither remedy establishes truth of evidence, which remains subject to existing verification.

### Run 0002: reported serial failures and limits of the evidence

[O] The new follow-up identifies two concrete failed publication attempts for the same completed story: run 12 supplied numeric `parent` (reported cost about $1.26), and run 13 supplied an array where an acceptance check's evidence text must be a string (about $0.43). Those two itemized costs total approximately $1.69, consistent with the owner's rounded “roughly $1.70.” Neither costs, scoped-check results nor historical payloads were independently inspected. Earlier run 0001 evidence identifies database run 282; this follow-up uses run labels 12–14. Their complete mapping was not supplied and is not inferred.

[O] Despite prose asserting three rejections, the supplied table explicitly leaves run 14 pending. Treat the record as two diagnosed rejections and a third publication attempt of unknown outcome. The reported offline check's remaining defect is unspecified; neither its field nor its eventual outcome can be inferred. No runtime state or forge was consulted to resolve that gap.

**Inference:** first-error reporting plus no corrective author channel makes repeated paid retries plausible even after implementation has passed its checks. Same-run feedback can remove the full-run retry boundary, but exposing only the first failure may still require several corrections inside that run. To address serial discovery itself, consider a preflight that returns every safely discoverable independent diagnostic together, in deterministic order. Factory ownership of the envelope removes identity copying failures, but it does not prevent malformed agent-authored evidence text or failed semantic checks.

### Meaning of success and feedback

[R0] An event's raw retention, successful parsing, successful verification acceptance, and resulting story effects occur at different points (`internal/store/story_progress.go:21`; `internal/turn/story_progress.go:277`; `internal/daemon/tick.go:149`).

**Recommendation for Plan:** distinguish “submitted,” “rejected,” “accepted verification,” and any outstanding effect delivery in the user/agent contract. A helper that says success after writing a file would reproduce the current gap between the agent's final report and the factory's decision. Live feedback needs an observable result and a defined correction action. It must also cover finish-time races, since the transport is drained and retired at run completion.

### Run 0002: independently useful feedback boundaries

**Research recommendation, not an implementation plan:** the smallest independently useful capability to assess is author-callable, side-effect-free preflight against the shared event contract before a file is published. Existing `agent.ParseStoryEvent` is a reusable validation boundary, but currently returns only the first error (`internal/agent/story_progress.go:87`). Delivery to both agents, actionable diagnostics, and optional aggregation are additional work; this research does not establish that implementation is small. Preflight needs no forge access and must not consume an event sequence or mutate authoritative progress.

[R0] Preflight success cannot establish authoritative acceptance. Ingestion separately checks frozen scope and identities, required acceptance coverage, actual worktree snapshot bytes twice, event-byte stability and reporting bounds; transactional persistence checks state and attempt ordering (`internal/turn/story_progress.go:277`; `internal/store/story_progress.go:118`). Files can change between a local check and ingestion. Accepted verification and delivered forge effects remain separate states.

**Research recommendation:** treat authoritative receipts as a complementary capability when the promised outcome is “the agent knows acceptance before ending.” They must identify the submitted event safely, report rejected/accepted/pending outcomes, support correction at a later sequence, and account for daemon polling/backoff and final transport retirement. Durable diagnostics cannot be reconstructed by replaying an existing stored reason because none is stored. A preflight-only change should explicitly report validated input, not accepted story completion; a file-write acknowledgment should report submission only.

## Constraints & Risks

### Authority and compatibility

- [R0] GitHub remains the sole editable story source; local story-file substitution is outside the managed workflow (`internal/turn/plan_handoff.go:100`; `.aiteam/APP_CONSTRAINTS.md:90`). **Risk:** structured persistence without a declared authority model creates divergent contracts.
- [R0] Existing published stories and frozen revisions contain bodies with legacy acceptance semantics (`internal/store/approved_stories.go:12`; `internal/turn/story_progress.go:507`). **Risk:** silently changing interpretation can add/remove obligations or invalidate already accepted work. Legacy publication, new publication, and active frozen runs need distinct compatibility decisions.
- [R0] Completed-story immutability and parent/generation checks already protect publication (`internal/turn/plan_publication.go:58`; `internal/turn/plan_publication.go:108`). **Risk:** blanket re-rendering or automatic repair could cross these boundaries.
- [R0] Sanitization/redaction changes publication input before emission (`internal/report/complete_story.go:19`). **Risk:** a new renderer must maintain agreement between declared IDs, published bytes, body hashes, and frozen acceptance semantics rather than assuming rendering alone is sufficient.

### Protocol and security

- [R0] Rooted filesystem access, symlink refusal, selected-scope validation, and repeated snapshot checks protect event ingestion (`internal/turn/story_progress.go:201`; `internal/turn/story_progress.go:283`; `internal/turn/story_progress.go:315`). A factory publisher/receipt channel must preserve these properties and must not introduce forge credentials into agent transport.
- [R0] Story content is treated as untrusted and delimited in prompts; it grants no forge authority (`internal/harness/stories.go:30`). An agent-provided story selector or evidence path remains untrusted even when identities are factory-filled.
- [R0] Events reject unknown and duplicate keys, trailing JSON, and excessive nesting/content. Proposal decoding rejects unknown/trailing fields but lacks the same recursive duplicate-key validation (`internal/agent/story_progress.go:87`; `internal/agent/story_progress.go:181`; `internal/planbrief/proposal.go:58`). **Risk:** a richer proposal schema should define ambiguity handling explicitly rather than inheriting accidental decoder behavior.
- [R0] Workflow instructions have copies under `.claude/`, `.codex/`, and `teams/`. Shared Go instructions feed the approved bundle (`internal/harness/stories.go:10`); source/release prompt copies are additional protocol consumers. The earlier instruction set required Claude/Codex parity when those instructions changed. Run 0003 superseding instruction ownership is recorded below; aggregate parity remains required, but an individual story must not edit both protected trees.

### Run 0002: feedback-specific risks

- [R0] SEC-20/21 and CONC-10 preserve factory-only forge authority, frozen selection, safe rooted transport, ordered ingestion and parent ownership (`.aiteam/APP_CONSTRAINTS.md:90`, `:198`; `internal/turn/story_progress.go:277`). Malformed identity claims must not become trusted receipt-routing or story-selection authority.
- [R0] STYLE-9 requires aggregation of independent validation-item failures and TEST-7 deterministic ordering (`.aiteam/APP_CONSTRAINTS.md:235`, `:153`). Existing early-return behavior is relevant tension to resolve; error aggregation must not weaken fail-closed validation or invent downstream checks after an invalid container.
- **Risk:** a receipt lost at crash or finish recreates uncertainty unless persistence, recovery and agent waiting are defined. The current rejected-event journal has no reason field. A guarantee of same-run correction is stronger than adding readable daemon errors.
- **Recommended validation concerns:** exact number-for-string and array-for-string diagnostics; two independent invalid fields reported together if aggregation is chosen; stable indexed paths and deterministic order; no sequence mutation during preflight; correction after a rejected sequence; both agents actually observing rejection and acceptance before termination; pending effects distinguished from accepted verification; finish/backoff/crash handling; stale and unauthorized factory-envelope selection remains rejected.

### Existing verification and remaining evidence gaps

[R0] Acceptance parser tests cover prose, numbered/mixed lists, checklists, tables, and ambiguous constructs (`internal/planbrief/acceptance_test.go:8`). Event contract tests cover invalid identities, duplicate/unknown fields, digests and paths; turn tests exercise live close, invalidation, replay, and concurrency (`internal/agent/story_progress_contract_test.go:84`; `internal/turn/story_progress_test.go:144`). The inspected malformed-event table lacks the exact numeric-parent regression. Existing rejection tests do not establish live author-visible correction and republication.

**Recommended validation concerns for Plan:** establish that every accepted criterion set is expressible as valid evidence, IDs remain stable through rendering/publication/freeze, legacy revisions retain intended obligations, manually edited issues follow the chosen policy, factory envelopes reject stale or unauthorized selectors, and rejected events can be corrected without violating sequence/replay rules. Feedback should distinguish evidence acceptance from later effects, including run-finish behavior. These are cross-boundary behavior checks, not a proposed implementation task list.

[R0] The repository's aggregate gate is `make check` (format, vet, pinned lint, race tests; `Makefile:25`). No tests were run because this run only produced research artifacts. Runtime remediation and reproducing the historical incidents remain outside this investigation.

## Run 0005 — approved direction and comment-preferred editing

[O] The owner explicitly approves the run 0004 structure (all five story sections), evidence **Option B** (typed lists of result explanations), the suggested criterion-ID rules, and both feedback stages with the stated compatibility policy. All four owner-review items previously listed are now settled. The content shapes below are approved research direction for Plan, not an implemented schema or a completed implementation specification.

[O] The owner amends the editing experience: **recommend comments as the preferred way to request changes, while continuing to support inline issue editing**. Each story should end with a short factory-rendered note encouraging the owner to describe the desired change and let litefactory infer and apply the corresponding structured update. This reduces opportunities for conflicting revisions; it cannot guarantee conflict-free updates. GitHub issues remain the authoritative story source.

### Concrete footer proposal

> Want to change this story? Comment with the requested change and litefactory will update it. Direct edits are supported, but comments help avoid conflicting revisions.

This wording is a proposed rendering detail implementing the approved requirement; no further owner confirmation is needed before Plan. Put it at the bottom of each newly rendered story in a dedicated **Editing this story** section, outside Acceptance Criteria, Tasks, Architecture, Dependencies and User Story. The renderer and readers must explicitly classify it as guidance, never an obligation, dependency or evidence requirement. Appending it to the final parsed section without a boundary would repeat the original accidental-prose defect. Do not bulk-rewrite legacy stories or implicitly upgrade their contract to insert the footer. Plan should specify safe footer handling for legacy publication without changing legacy interpretation or migration policy.

### Comment-driven reconciliation requirements

- [O] Commenting a requested change is the preferred interaction; direct edits remain supported without JSON or local-artifact requirements. Litefactory proposes/derives typed changes from ordinary language and performs the authorized issue update. This is an approved new requirement, not a claim that existing comment processing already implements this feature.
- The factory authenticates the owner and checks authorization before treating a comment as a requested mutation. Quoted content and arbitrary third-party comments remain untrusted evidence. Neither story prose nor a comment author impersonation may grant tools, permissions, scope approval or mode changes.
- Bind each inferred update to the current authoritative issue revision and recheck the expected revision before applying it. If another comment or inline edit changes the issue in between, preserve the current issue and reconcile against it; never silently overwrite it with an older inferred update. Keep the originating comment and before/after revision association available for explaining the change and deduplicating retries. A projection or local research/Plan artifact is not an independent source of authority.
- A comment saying that a string must now be `y` instead of `x` is a requested semantic change, even when it names the same criterion label. Apply the approved identity rule: materially replacing an obligation receives a new criterion ID; a clarification may retain its ID. The factory must not interpret label reuse as proof that the approved obligation is unchanged. Ambiguous intent or conflicting changes remain saved and receive a plain-language explanation.
- Reflect clear changes in the GitHub issue with an understandable change summary, preserving unrelated owner edits. The issue as updated is the editable story source; the comment is the authenticated request and audit context, not a competing obligation set. Approval and frozen snapshots remain explicit, revision-bound workflow state.
- Requested or inline edits must not silently alter an active run’s frozen scope, automatically reopen accepted historical work, select another workflow/mode, or bypass managed replan/approval gates. Accepted historical scope remains immutable; follow-up work follows the shared lifecycle rules. A preference for comments is an editing aid, not an approval shortcut.

**Residual risk:** comments reduce direct structural edits but still require semantic interpretation and concurrency control. Two comments can conflict, as can a comment and an inline edit. Expected-revision detection, preserved owner content, explicit unresolved status and actionable feedback remain necessary. Plan owns the mechanics, including comment ordering/deduplication, authorization, revision checks, approval integration and footer exclusion tests.

## Run 0004 — retained structure sketch, approved in run 0005

### Status and accepted direction

[O, run 0004] The owner requested a structure sketch to inspect **before moving on**. Run 0005 approves its direction as recorded above. The examples below remain illustrative, not valid current proposals/events, a completed implementation specification, an implementation plan, or an invocation of a mode. No machine-consumed proposal or outcome file is written. “All” is interpreted as all five story sections, with explicit structure at every agent-to-factory machine-consumed boundary; free text still belongs inside typed narrative fields. It does not mean forcing ordinary owner comments into JSON.

[O, run 0004] The owner superseded the run 0003 defaults as follows. This historical decision table is retained for provenance; run 0005 above settles its pending structure, evidence and ID reviews and amends editing guidance:

| Prior question | Recorded decision |
|---|---|
| 1 | Structure **all** story sections now: User Story, Acceptance Criteria, Tasks, Architecture, Dependencies. Present the sketch for owner validation first. |
| 2 | Owners freely edit normal GitHub issues; prioritize natural, easy interaction. Do not require JSON edits or routine Plan refresh merely to edit a story. |
| 3 | Existing stories retain the old contract. Upgrade only through explicit owner CLI action; do not silently migrate on republication. CLI availability is not assumed. |
| 4 | Explain single evidence text versus a structured list using samples; the owner has not selected a representation. |
| 5 | Include both preflight and authoritative receipts. |
| 6 | Include author-visible Plan preflight. |
| 7 | Accepted ordering: envelope plus usable feedback first, structured publication next; independently useful feedback can ship sooner if materially quicker. This is a priority, not an effort estimate. |
| 8 | GitHub issues are authoritative; a local text artifact or independently editable structured copy cannot override them. |
| 9 | Propose concrete ID rules for review. |
| 10–14 | Accept the recommended ownership, diagnostics, stage distinctions, reliable feedback/recovery, explicit compatibility and cross-boundary verification direction. Plan resolves concrete mechanisms within these decisions; do not ask the owner to approve those directions again. |

[V] The shared workflow inspected this run preserves the distinction between immutable historical acceptance and live editing, between engineering evidence and acceptance, and between explicit mode selection and a report suggesting another mode (`teams/source/skills/workflow/SKILL.md`). The new **content/event contract version** must not be conflated with the separately persisted **workflow version**.

### A. Proposed full story content returned by an agent

**Illustrative draft only.** This deliberately removes an agent-authored whole Markdown `body`. A factory-bound interface knows the protocol version and current parent/run/generation. It accepts content and a story selector; factory rendering owns the five headings, lists/tables, identity metadata, and status markers. This example defines the content shape, not an executable CLI, current v1 schema, or final transport. The bound interface must still authorize every selector.

```json
{
  "summary": "Make validation feedback visible during the run.",
  "stories": [
    {
      "key": "US-001",
      "intent": "create",
      "title": "Receive actionable submission feedback",
      "user_story": {
        "actor": "coding agent",
        "want": "to receive all discoverable submission errors before finishing",
        "benefit": "I can correct them within the same run",
        "notes": []
      },
      "acceptance_criteria": [
        {
          "id": "AC-001",
          "text": "A rejected submission reports each safely discoverable defect with its field path and expected type."
        },
        {
          "id": "AC-002",
          "text": "A corrected submission receives a factory receipt without starting another run."
        }
      ],
      "tasks": [
        {
          "id": "T-001",
          "description": "Return correlated validation diagnostics to the submitting agent.",
          "owner_role": "go-implementation-engineer",
          "criterion_ids": ["AC-001", "AC-002"],
          "depends_on": []
        }
      ],
      "architecture": {
        "summary": "Use a factory-bound submission interface shared by both runners.",
        "components": [
          {
            "name": "submission boundary",
            "responsibility": "Validate content and correlate receipts with the authorized run."
          }
        ],
        "interfaces": [
          {
            "name": "submit story outcome",
            "input": "Authorized story selector and typed outcome/evidence.",
            "output": "Receipt or a deterministic list of actionable diagnostics."
          }
        ],
        "constraints": ["Agents receive no forge credentials."],
        "decisions": [
          {
            "decision": "The factory constructs identity fields and sequence counters.",
            "reason": "These are known before publication and should not be copied by the agent."
          }
        ]
      },
      "dependencies": {
        "story_keys": [],
        "issue_refs": []
      }
    }
  ]
}
```

All leaf narrative values shown are strings; `notes`, `criterion_ids`, `depends_on`, `constraints`, `story_keys`, and `issue_refs` are arrays of strings. `stories`, `acceptance_criteria`, `tasks`, `components`, `interfaces`, and `decisions` are arrays of the exact object shapes shown. Arrays exist even when empty; no string-or-list unions, inferred null defaults, or Markdown tables in place of objects. Required narrative strings must be nonblank. Each story has at least one criterion and task; an architecture summary is required, while its other arrays may be empty when inapplicable. Narrative fields may contain normal prose and line breaks, but are never scanned for undeclared criteria. Unknown or duplicate keys fail validation with precise diagnostics.

`intent` is exactly `create`, `update`, or `unchanged`; `key` selects an existing story in an update or proposes a new story key on creation. The factory checks intent against its bound story set and preserves existing required no-op/update semantics. The factory retains any existing issue identity and current base revision in the bound Plan context; an agent must not invent numeric forge IDs, hashes, parent refs or generations. For legitimate external dependencies, `issue_refs` uses an explicit task-reference string; the factory resolves and authorizes it. `tasks.depends_on` names same-story task IDs; cross-story ordering belongs to `dependencies`. `owner_role` is validated against the supplied roster. Unknown references, dependency cycles and duplicate IDs are semantic errors. Changing dependency prose cannot quietly leave obsolete dependency metadata active.

The shared `plan_brief.md` remains required Plan context under the existing workflow; its Story Index remains link-only. It is not the editable story source. In a future protocol, machine-consumed Plan completion/discussion and other stage outcomes must likewise be typed; this Research sketch does not replace today's strict managed Plan v1 outcome/proposal contract.

### B. Agent contribution versus factory-owned envelope

The same principle applies to story outcomes: agents choose one supplied story selector and provide stage-appropriate outcome/evidence. The following sketch uses a separate `submission` object so content cannot smuggle envelope fields. Snapshot contents and evidence are illustrative placeholders, not valid digests or proof of execution.

```json
{
  "story": "US-001",
  "submission": {
    "outcome": "engineering_complete",
    "evidence": {
      "snapshot": {
        "implementation_digest": "<digest of the scoped implementation>",
        "files": [
          {
            "path": "internal/agent/story_progress.go",
            "digest": "<digest of the captured file>"
          }
        ]
      },
      "checks": [
        {
          "id": "check-01",
          "kind": "test",
          "name": "rejects numeric parent",
          "result": "passed",
          "summary": "A numeric parent produces an expected-string diagnostic."
        },
        {
          "id": "check-02",
          "kind": "test",
          "name": "rejects evidence array under text contract",
          "result": "passed",
          "summary": "An evidence array produces an expected-string diagnostic."
        }
      ]
    }
  }
}
```

This is an **engineering shape example**, not a new substitute for existing snapshot/check truth requirements and not acceptance proof. It intentionally has no acceptance/review findings: separated Implement submits engineering checks; Validate alone reviews retained evidence. The `checks` array here contains test-kind records; the final schema must preserve the exact stage-specific requirements. Full engineering/acceptance submission must enumerate whatever the persisted workflow and frozen scope require, retain actual captured file state and executable-check results with snapshot attribution, and explain failures/missing evidence. The final schema must retain existing detailed snapshot semantics, including deletion and scope, and reference retained check records with provenance; a short summary or a claimed `passed` field alone cannot establish a passed check. Factory helpers may capture actual bytes/digests, but they may not invent executed results.

| Factory-owned property | Source and responsibility |
|---|---|
| Protocol/schema version | Factory-selected supported contract, fixed for the run |
| Parent task reference | Bound parent context; always a string, never agent-entered |
| Run ID, generation | Bound current run and approved generation |
| Story issue ID, revision | Authorized selector resolved within the supplied frozen story bundle |
| Sequence, attempt, submission/receipt IDs | Factory allocator with durable correlation, concurrency and retry semantics |
| Persisted workflow version, permitted outcomes | Stored feature workflow and selected mode; never inferred from a model's text |

The selector is agent-chosen, not authoritative. Stale, unknown or unauthorized selection still fails. Mutable prefilled JSON would not establish ownership; a bounded publisher must build the envelope without accepting agent overrides. Mode-specific outcomes remain explicit: separated Implement reports engineering state; Validate reports retained-evidence findings; legacy scoped verification retains legacy effects. A generic agent `verified` claim must not imply acceptance or closure in separated workflow.

A separate Validate contribution can follow this same ownership rule: select the authorized story; return every frozen criterion finding and the required code/architecture/plan reviews. The factory supplies issue/revision/snapshot attribution from its retained review bundle. Historical illustrative fragment using Option A (retained to explain the contrast; run 0005 selects Option B for the new contract):

```json
{
  "story": "US-001",
  "submission": {
    "outcome": "validation_complete",
    "criteria": [
      {
        "id": "AC-001",
        "status": "satisfied",
        "check_ids": ["check-01", "check-02"],
        "evidence": "Both retained type-validation results support this criterion."
      },
      {
        "id": "AC-002",
        "status": "unverified",
        "check_ids": [],
        "evidence": "No retained same-run correction and receipt result was supplied."
      }
    ],
    "reviews": [
      {
        "name": "code",
        "status": "unverified",
        "classification": "blocking",
        "evidence": "Review has not been performed in this illustrative example."
      },
      {
        "name": "architecture",
        "status": "unverified",
        "classification": "blocking",
        "evidence": "Review has not been performed in this illustrative example."
      },
      {
        "name": "plan",
        "status": "unverified",
        "classification": "blocking",
        "evidence": "Review has not been performed in this illustrative example."
      }
    ]
  }
}
```

The example deliberately shows unverified findings instead of fabricating completed reviews or checks. Criterion findings are blocking by contract; review classification permits blocking/advisory findings under the existing workflow rules. Option B changes evidence values into the same typed list consistently for criterion/review evidence, with empty `check_ids` allowed for a narrative explanation of missing evidence or a source-based review. No fresh checks are run during Validate. Factory scope/coverage admission is still required, and a validation-complete response can contain unmet or unverified findings; it does not promise acceptance.

### C. Evidence question 4 — equivalent samples and tradeoff

Both examples below express the **same two validation results**. Neither changes the fact that the frozen criterion ID is a string or that actual executed-check evidence must be retained.

**Option A: one string, with references to both checks.**

```json
{
  "id": "AC-001",
  "check_ids": ["check-01", "check-02"],
  "evidence": "The numeric-parent test passed and returned expected string. The evidence-array test passed and returned expected string."
}
```

This keeps the evidence text close to the current string contract. Multiple results are combined into prose, optionally separated with newlines. A consumer treats it as one explanation; it cannot address or display each explanation independently without interpreting prose. Supplying an array here remains a type error, caught before submission.

**Option B: an explicitly typed list of result explanations.**

```json
{
  "id": "AC-001",
  "evidence": [
    {
      "check_ids": ["check-01"],
      "summary": "The numeric-parent test passed and returned expected string."
    },
    {
      "check_ids": ["check-02"],
      "summary": "The evidence-array test passed and returned expected string."
    }
  ]
}
```

This lets the factory display, validate and attribute each supporting result separately. It adds object fields and bounds to the contract. An array of arbitrary sentences would still be invalid because entries must be objects with `check_ids` and `summary`. Lists prevent the exact array-versus-string confusion only when the chosen schema is actually enforced; they do not eliminate author errors on their own.

**Approved by the owner in run 0005:** choose Option B for the new contract. Multiple supporting results are naturally plural, and the full structured boundary is already in scope. Keep the existing string type in legacy events. There must be exactly one accepted representation per contract version, never a permissive string-or-list union. Option A is retained here only for comparison and the unchanged legacy contract; it is not an unresolved alternative for the new contract. The evidence alternatives describe criterion findings for Validate (or legacy verified evidence), not an acceptance group added to separated engineering events.

### D. Live preflight and authoritative response

Preflight should return all safely discoverable independent errors from the same validator used at ingestion. Illustrative response to Option A being supplied a list instead of text:

```json
{
  "receipt_id": "receipt-17",
  "stage": "preflight",
  "status": "rejected",
  "errors": [
    {
      "path": "submission.criteria[0].evidence",
      "code": "type_mismatch",
      "expected": "string",
      "actual": "array",
      "message": "This contract requires one text value; combine the two result explanations into that string."
    }
  ],
  "validation_complete": true,
  "unchecked_paths": []
}
```

For Option B the expected type at that location is `array`, with nested object/string checks at each indexed element. Diagnostics follow the bound contract, not the example chosen by the agent. `validation_complete` means all applicable input checks were performed; it does not mean evidence accepted. Invalid syntax or containers yield `validation_complete: false` and bounded, explicit unchecked paths where safe enumeration is possible. Never fabricate exhaustive results when dependent checks cannot run. Preflight does not allocate an event sequence or mutate accepted story state.

A separate durable response must establish what the factory actually recorded:

```json
{
  "receipt_id": "receipt-18",
  "submission_id": "submission-9",
  "stage": "engineering",
  "status": "recorded",
  "acceptance_status": "pending",
  "issue_status": "open",
  "errors": []
}
```

Success at this stage cannot imply Validate acceptance, successful remote effects, issue closure or parent delivery. Responses must also express rejected and pending states; remote effect failure is distinct from valid recorded evidence. The agent receives errors while its submission call/receipt observation is still active, can correct and resubmit within the same run, and reports pending if a bounded wait ends without a decision. Durable correlation, idempotent retries, crashes, lost responses, daemon backoff and final-drain races remain engineering design obligations already accepted by the owner. Native first-error decoder wording alone does not meet this contract. Plan preflight must provide the equivalent feedback before publication; completed/discussion semantics and factory-only publication remain intact.

### E. Freely editable GitHub issues, authoritative meaning, deterministic agent input

The proposed separation is between **agent authoring** and **owner editing**, not between two authoritative story sources:

1. The agent fills typed fields; the factory renders an ordinary readable GitHub issue. No JSON editing, secondary Markdown file or hidden structured blob becomes an owner obligation.
2. Prefer an authenticated owner comment describing the desired change, with litefactory deriving and applying a revision-checked structured update as recorded in run 0005. The owner can still edit the title, paragraphs, checklists, task rows, architecture and dependencies directly in GitHub. Preserve those bytes and unrelated edits. Checkbox ticks remain presentation and do not assert accepted evidence.
3. Derive a validated structure from the current issue revision. This is a cache/projection bound to the exact issue body/title revision, not a competing editable source. Recheck the issue revision before publication, reconciliation or freeze; concurrent changes trigger reconciliation instead of overwrite.
4. Recognize explicit criterion items as items, never every nonempty line. Existing short `AC-001` labels preserve identity; an ordinary newly added checklist item can receive the next unused label from the factory. Multiline continuation belongs to the item. Section prose stays narrative. A note such as “Evidence contract: …” never silently becomes a new criterion.
5. Reflect clear changes in the derived structure and display a human-readable change summary. Owner wording remains intact; factory-added identifiers or status projection must be narrow, revision-checked edits. Reordering does not renumber; dependency edits update the validated dependency projection rather than retaining stale hidden edges.
6. If IDs are removed/duplicated, criterion boundaries conflict, a dependency is unclear, or prose could be a requirement addition/removal that cannot be safely classified, preserve the edit and report the exact ambiguity in normal language. Do not silently discard it, rewrite it to fit the schema, approve guessed obligations, or require the user to hand-author JSON. Resolution can be an ordinary edit or owner instruction identifying the intended items.
7. Requirement changes are available to the next explicitly approved scope; active runs continue using their frozen revision. Accepted historical snapshots remain accepted under separated workflow. Permission to edit does not grant automatic scope approval or replacement of frozen work. Harmless narrative edits should not force requirement reapproval if the validated obligations and other approved semantics are unchanged; uncertainty remains pending rather than guessed.

**Fundamental limitation:** arbitrary free prose cannot be mapped perfectly to deterministic semantics. The initial agent path stops requiring prose reconstruction. The owner-edit path still needs a reconciliation policy and observable ambiguity handling. Normal checklists and short labels make common edits automatic; unusual free-form edits remain saved and visible but can block approval until their intended meaning is resolved. The owner has approved the editing direction with the run 0005 preference for comments; it is not a promise of flawless semantic extraction. Supporting all five sections means reconciling all their structured content too; “no changed AC” does not prove a task, architecture or dependency edit is harmless.

### F. Suggested criterion identity and size rules

**Approved research direction in run 0005, not currently implemented; final shared transport budgets remain Plan-owned:**

- Canonical ASCII format `AC-` followed by **3–6 decimal digits**, e.g. `AC-001`, `AC-027`, `AC-1000`. Prefix uppercase, no spaces or alternative separators; suffix must represent a positive number and use `max(3, decimal digit count)` digits. Thus `AC-000`, `ac-001`, `AC-01`, `AC-0001`, and semantic suffixes are invalid. Length is 6–9 ASCII bytes, comfortably below the carried-forward 256-byte evidence name ceiling.
- Uniqueness is **within one story**; evidence resolves the criterion within its authorized story/revision. Different stories may both contain `AC-001`. IDs are never re-used for another obligation during that story's history.
- **1–256 active criteria per story**, preserving the carried-forward maximum of 256 checks per evidence group as a compatibility ceiling. IDs need not be consecutive and ordering is independent of identity. Factory assignment uses the next unused number, not list position; explicit IDs proposed by the planner are validated against existing issue history.
- Reordering and wording clarification retain identity. A materially different obligation gets a new ID; uncertain meaning changes are reported for resolution, not guessed. Removing a criterion from a new revision does not remove it from older frozen evidence or erase historical acceptance.
- Unknown criterion IDs in criterion coverage are rejected. Supplemental engineering checks use check IDs in the check collection, not fabricated acceptance criteria. Required criterion coverage is checked for the applicable workflow/stage; merely listing a criterion or passing a formatting check does not prove it.
- Bound criterion text at **4,096 UTF-8 bytes**, one explanation at **8,192 UTF-8 bytes**, and **1–16 explanation items per criterion** for Option B. Independently enforce the total serialized and rendered document budget before accepting a proposal, and the total evidence payload budget before accepting a submission. Per-item maxima do not promise their Cartesian product fits a document.
- The new evidence contract must support all **256 criterion records** with separate bounded supporting-check records; sharing checks through references avoids duplicating full snapshots/results per explanation. Validate total bytes jointly across proposal, rendered issue and evidence so a valid obligation set cannot be impossible to evidence purely because of inconsistent producer/consumer limits. Large real evidence can still exceed a payload budget and needs an explicit retained-record reference strategy; ID rules alone do not solve that.

[R0] Historical limits cited above are the prior research's `internal/agent/story_progress.go:143` (256 checks/name bytes) and `internal/planbrief/proposal.go:15` (8 MiB proposal; 100 stories; 65,536-byte story body). Run 0004 reinspection below confirms the current limits, including additional snapshot and event budgets. New field/count/text limits require shared validation and precise error messages at authoring time; final numeric byte budgets and retained-evidence reference capacity are architectural work within the approved consistency requirement.

### G. Legacy and upgrades

[O] Existing stories remain on the old **content/event contract** until the owner explicitly upgrades through the CLI. Republishing alone is not permission to upgrade. New contracts must dispatch by persisted version across cold/resumed/fallback runs and event replay; content/event contract changes do not silently select a different workflow version. An upgrade must be an explicit factory operation with previewable affected scope, validated current issue content, and preservation of immutable past acceptance and active frozen revisions. Research does not execute an upgrade or infer authorization to repair #170/#204. [V] The current CLI registration has no managed-contract upgrade command (`internal/cli/commands.go:8–31`; task exposes only clean at `internal/cli/cmd_task.go:28–38`). Delivering explicit CLI upgrade is therefore additional capability needed to fulfill this policy; no executable syntax is proposed or invoked here.

### H. Run 0004 bounded codebase reinspection

[V] A delegated incremental `codebase-tour` inspected only the current proposal/event/Validate contracts and CLI surface; it wrote no files and ran no tests. Findings:

- Current Plan v1 includes `version`, `parent`, `generation`, and stories with `key`, `intent`, `issue_number`, `issue_id`, `expected_base`, `title`, `body`, `dependencies` (`internal/planbrief/proposal.go:29–48`). All five headings remain body text (`:184`). Existing story updates have base-hash/identity checks; dependencies are known story keys and acyclic (`:100–154`). The draft external `issue_refs` support is a proposed expansion and must respect approved attached-child policy, not be mistaken for an existing capability.
- Proposal transport is 8 MiB; body 65,536 bytes; title 256 bytes; 1–100 stories (`internal/planbrief/proposal.go:15–25`, `:80`, `:97`). Current key format is `^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$` (`:48`). The draft retains current permitted story keys; criterion IDs are a separate proposed stricter rule.
- The criterion parser still has neither evidence-name length nor criterion-count bounds (`internal/planbrief/acceptance.go:71–86`). Check groups have 1–256 checks, unique names of at most 256 bytes, evidence **strings** up to 16,384 bytes, and per-check implementation-digest attribution (`internal/agent/story_progress.go:61`, `:161–170`). New proposed 8,192-byte explanations are stricter, not current behavior.
- Snapshots have 1–256 sorted safe file paths with JSON-base64 content, SHA-256 and deletion state (`internal/agent/story_progress.go:53–59`, `:150–159`). Event cap is 4 MiB, 4,096 events/run and 64 MiB aggregate run transport (`:18–25`). These make joint payload validation and retained-reference design necessary; schematic file digests alone must not replace actual snapshots.
- Approved bundles supply parent/run/generation/workflow/story issue ID/revision (`internal/agent/stories.go:6–21`). Event v1 still requires agents to repeat identities and counters (`internal/agent/story_progress.go:29`). `engineering_complete` requires tests and forbids nonempty acceptance/review groups (`:115–125`, `:141–147`); legacy `verified` requires all three groups. The examples above preserve the separation while proposing a new content boundary.
- Validate currently repeats parent/generation plus story issue ID/revision/snapshot in output (`internal/agent/validation.go:65–83`). Findings have string evidence, status `satisfied`/`unmet`/`unverified`, blocking criteria and blocking/advisory reviews, including code/architecture/plan (`:109–169`). The owner’s “always” direction is interpreted to cover this output as well as Plan and progress; the explicit sketch above makes that scope visible.
- No managed-story/contract upgrade CLI exists in the complete registration surface (`internal/cli/commands.go:8–31`; `internal/cli/cmd_task.go:28–38`). The owner's manual-upgrade policy requires new capability, not documentation of an available command.

These are current-code findings only, not validation of the proposed schemas or historical incidents. New proposal/outcome contracts need explicit version dispatch; research examples cannot be sent to the existing strict v1 decoder.

## Open Questions

All owner-review decisions from run 0004 are settled by run 0005. Do not ask again whether to structure all sections, choose evidence A/B, adopt the suggested criterion IDs, support inline edits, retain GitHub authority, provide both feedback stages/Plan preflight, or preserve explicit CLI-only legacy upgrades. The new comment preference and bottom-of-story guidance are approved requirements.

Plan must resolve these engineering details within that direction:

1. Exact versioned contracts, author-callable transport and durable receipt/correction mechanics across both harnesses and workflow versions.
2. Comment ingestion authorization, order and retry deduplication; revision-bound inference/publication and safe handling of conflicting comments or inline edits; understandable change summaries and ambiguity feedback.
3. Reconciliation and identity-history implementation across all five sections, explicit handling of semantic replacement versus clarification, and integration with frozen-scope/approval and historical-acceptance rules.
4. Renderer boundaries for the guidance footer, ensuring no consumer interprets it as an obligation or dependency, including safe handling of legacy rendering without implicit upgrades.
5. Compatible total proposal/rendered-issue/evidence budgets, retained-check/snapshot reference capacity, and exact bounds/diagnostics so valid requirements remain expressible as valid evidence.
6. Explicit CLI contract upgrade capability and version dispatch for cold, resumed, fallback and replay paths, preserving active frozen revisions and historical acceptance.
7. Cross-boundary verification of multiple-error reporting, authoritative acknowledgments, correction in one run, pending states, crash/finish races, unauthorized or stale submissions, and editing/reconciliation conflicts.

These are Plan/architecture obligations, not fresh product approval questions. Run 14's unknown historical outcome remains an incident-evidence gap and not a prerequisite. This Research run saves the decisions and ends without starting Plan or waiting for another confirmation.

## Recommended scope for Plan mode

Use the owner decisions in runs 0004 and 0005, superseding earlier criteria-first, structured-owner-edit and evidence-A defaults. All five story sections use the approved typed agent authoring shape and deterministic factory rendering. GitHub issues stay the authoritative editable source. Prefer authenticated owner comments requesting changes, let litefactory infer and apply revision-checked updates, keep direct editing supported, and render the short editing guidance at each story's bottom in its own non-obligation section. No text artifact or hidden structured copy overrides live issue edits. Preserve exact-revision conflict detection, unrelated owner changes, plain-language ambiguity handling and the existing approval/frozen-scope boundaries.

Choose typed evidence lists (Option B) for the new contract, with short stable criterion IDs and the approved per-story limits. Existing stories retain their legacy contract until explicit CLI upgrade; republication or comment editing alone must not upgrade them. Include the new CLI capability with historical acceptance and active frozen-scope preservation.

Include factory-owned envelope/counters, stage-specific outcome content, both comprehensive preflight and authoritative durable receipts, Plan preflight and observable same-run correction for both harnesses. Preserve authorization, actual check and snapshot provenance, frozen scope, workflow-version behavior and historical acceptance. The owner has accepted the ordering and consistency/recovery/testing direction; Plan should settle implementations rather than reopen that direction.

The reviewed structure is sufficient research evidence for an explicit owner handoff to Plan. Recommend `/plan` with an instruction to build the plan from these approved decisions; do not infer a mode change from these approvals or start Plan automatically. No production protocol changes, plan proposal, migrations, runtime repair or mode transition are performed by this Research artifact.
