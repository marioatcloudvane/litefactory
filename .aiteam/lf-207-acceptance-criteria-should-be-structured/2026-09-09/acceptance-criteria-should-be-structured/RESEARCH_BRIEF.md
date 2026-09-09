# Research Brief — acceptance-criteria-should-be-structured

## Request

Research issue `github:marioatcloudvane/litefactory#207`: replace agent-authored Markdown that the factory subsequently interprets as acceptance obligations with explicit structured items and factory-owned rendering. Include the triggering comment's scope extension: the factory should construct story-progress identity envelopes from its existing frozen context while the agent supplies the outcome and evidence. The follow-up prioritizes factory construction of the identity envelope and independently useful live field/type feedback. Investigate serial error discovery across costly reruns and whether comprehensive preflight plus acknowledged correction can resolve it within one run.

This is Research run 0002, updating the completed run 0001 brief, on branch `lf/207-acceptance-criteria-should-be-structured`. No implementation, operational repair, commit, push, or forge mutation is included. The headless instruction takes precedence over the research skill's confirmation step: no questions were asked and unresolved product decisions are recorded for Plan.

### Provenance and boundaries

- `[V]` means repository behavior reverified in run 0002 by the delegated codebase tour. `[R0]` means findings carried forward from run 0001, including its citations, without fresh verification unless a run 0002 subsection explicitly says otherwise. `[O]` means owner-supplied incident evidence, not independently reproduced; `[W]` denotes other unverified second-hand material. Recommendations and inferences are not verification claims.
- Recommendations and inferences below are explicitly identified; they are not existing behavior or approved requirements.
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

[V] Both reported type mismatches match the current contract: `StoryEvent.Parent` is a string; the evidence text inside each `StoryCheck` is also a string, although acceptance/review/tests are arrays of checks (`internal/agent/story_progress.go:29`, `:45`, `:62`). The instructions provide examples, not an author-callable validator (`internal/agent/story_progress.go:172`). The approved JSON bundle contains all five requested factory-known envelope identities and also numeric parent identities that can be confused with the task reference (`internal/agent/stories.go:6`, `:21`; `internal/harness/stories.go:23`). A mutable prefilled template reduces copying but does not prevent accidental edits; factory construction is a stronger ownership boundary.

[V] `ParseStoryEvent` checks size and duplicate keys, calls one typed JSON decode, and returns immediately on decode failure. Semantic validation follows only successful decoding (`internal/agent/story_progress.go:87`). The installed Go decoder stores the first error, not a collection of all incompatible fields (`/opt/homebrew/opt/go/libexec/src/encoding/json/decode.go:247`; local toolchain source, not a repository artifact). Semantic validation similarly returns early through manifest/file/group/check loops. Some failures collapse multiple causes into “check does not verify the scoped snapshot” without identifying which evidence group failed (`internal/agent/story_progress.go:132`). Ingestion does aggregate errors across separate files, using `errors.Join`, but that does not enumerate independent defects within one event (`internal/turn/story_progress.go:333`).

[V] Field and expected type are available for typed decode failures, but the exact indexed path in the owner-provided error was not reproduced. The local decoder maintains a struct-field stack and its array loop does not add an index to that stack (`/opt/homebrew/opt/go/libexec/src/encoding/json/decode.go:255`, `:542`). **Inference:** a stable diagnostic contract with JSON paths such as `evidence.acceptance[0].evidence`, expected type and observed type should be explicit; simply forwarding native wording does not establish index precision or comprehensive diagnostics. Safely collecting independent errors also needs boundaries: malformed syntax or wrong container types can make dependent semantic checks meaningless.

[V] Validation is already possible while a worker is running: daemon ticks reconcile progress with workers occupied, although a backoff return can precede polling (`internal/daemon/tick.go:142`, `:149`). Final harvest also drains progress after `runClaude` returns (`internal/turn/harvest.go:47`). **Correction to the incident's generalized timing claim:** repository validation is not exclusively post-run. The observed incidents describe when failures became visible to the owner; absent author feedback allows a run to finish without learning either a live or finish-time rejection.

[V] The shared request offers a fixed prompt and outgoing usage capture, with no incoming validation-feedback channel (`internal/agent/request.go:51`). Both runner adapters use `strings.NewReader(req.Prompt)` as stdin; their stdout is consumed into results/logs rather than a duplex conversation (`internal/harness/runner.go:687`; `internal/codex/runner.go:297`). A new log message alone cannot reach the active author. A filesystem receipt requires instructed polling/wait behavior; an author-callable validator requires an executable or tool surface delivered into the run. Neither mechanism exists in the inspected event path.

[V] Rejection persists raw bytes/digest and a rejected marker, but no diagnostic reason; its store API has no diagnostic parameter (`internal/store/story_progress.go:273`). Corrections must use a new global event sequence because previously processed files are skipped, including rewritten rejected files (`internal/turn/story_progress.go:264`). Rejection advances `last_sequence`, but it does not advance the claimed story attempt: rejected rows have blank authoritative story identity and attempt zero. Acceptance permits the previous story attempt or the next one, so a first rejected submission may be corrected at sequence 2 while retaining attempt 1 (`internal/store/story_progress.go:139`). A new run is not inherently required by the journal protocol.

[V] The current suite already tests rejection, correction and acceptance within one run: restore rejected snapshot bytes, publish sequence 2, then close the story (`internal/turn/story_progress_test.go:253`). Held-parent tests exercise live acceptance for both configured agent selections (`internal/turn/story_progress_test.go:144`). These establish journal/lifecycle capabilities, not an author-visible feedback loop. The malformed contract table lacks both exact reported type mismatches and checks rejection rather than actionable diagnostic detail (`internal/agent/story_progress_contract_test.go:84`).

[V] Original Plan findings remain relevant on targeted reinspection: proposals carry free-form `Body` with separate dependency data; the acceptance parser turns nonempty lines into obligations without matching evidence-name/count bounds; other required sections are checked for nonempty presence (`internal/planbrief/proposal.go:37`, `:159`; `internal/planbrief/acceptance.go:70`). The prior publication, owner-edit and compatibility findings below remain carried forward unless explicitly reverified above.

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

[V] Preflight success cannot establish authoritative acceptance. Ingestion separately checks frozen scope and identities, required acceptance coverage, actual worktree snapshot bytes twice, event-byte stability and reporting bounds; transactional persistence checks state and attempt ordering (`internal/turn/story_progress.go:277`; `internal/store/story_progress.go:118`). Files can change between a local check and ingestion. Accepted verification and delivered forge effects remain separate states.

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
- [R0] Workflow instructions have copies under `.claude/`, `.codex/`, and `teams/`. Shared Go instructions feed the approved bundle (`internal/harness/stories.go:10`); source/release prompt copies are additional protocol consumers. Applicable `AGENTS.md` requires Claude/Codex parity when those instructions change.

### Run 0002: feedback-specific risks

- [V] SEC-20/21 and CONC-10 preserve factory-only forge authority, frozen selection, safe rooted transport, ordered ingestion and parent ownership (`.aiteam/APP_CONSTRAINTS.md:90`, `:198`; `internal/turn/story_progress.go:277`). Malformed identity claims must not become trusted receipt-routing or story-selection authority.
- [V] STYLE-9 requires aggregation of independent validation-item failures and TEST-7 deterministic ordering (`.aiteam/APP_CONSTRAINTS.md:235`, `:153`). Existing early-return behavior is relevant tension to resolve; error aggregation must not weaken fail-closed validation or invent downstream checks after an invalid container.
- **Risk:** a receipt lost at crash or finish recreates uncertainty unless persistence, recovery and agent waiting are defined. The current rejected-event journal has no reason field. A guarantee of same-run correction is stronger than adding readable daemon errors.
- **Recommended validation concerns:** exact number-for-string and array-for-string diagnostics; two independent invalid fields reported together if aggregation is chosen; stable indexed paths and deterministic order; no sequence mutation during preflight; correction after a rejected sequence; both agents actually observing rejection and acceptance before termination; pending effects distinguished from accepted verification; finish/backoff/crash handling; stale and unauthorized factory-envelope selection remains rejected.

### Existing verification and remaining evidence gaps

[R0] Acceptance parser tests cover prose, numbered/mixed lists, checklists, tables, and ambiguous constructs (`internal/planbrief/acceptance_test.go:8`). Event contract tests cover invalid identities, duplicate/unknown fields, digests and paths; turn tests exercise live close, invalidation, replay, and concurrency (`internal/agent/story_progress_contract_test.go:84`; `internal/turn/story_progress_test.go:144`). The inspected malformed-event table lacks the exact numeric-parent regression. Existing rejection tests do not establish live author-visible correction and republication.

**Recommended validation concerns for Plan:** establish that every accepted criterion set is expressible as valid evidence, IDs remain stable through rendering/publication/freeze, legacy revisions retain intended obligations, manually edited issues follow the chosen policy, factory envelopes reject stale or unauthorized selectors, and rejected events can be corrected without violating sequence/replay rules. Feedback should distinguish evidence acceptance from later effects, including run-finish behavior. These are cross-boundary behavior checks, not a proposed implementation task list.

[R0] The repository's aggregate gate is `make check` (format, vet, pinned lint, race tests; `Makefile:25`). No tests were run because this run only produced research artifacts. Runtime remediation and reproducing the historical incidents remain outside this investigation.

## Open Questions

These are decisions for the owner and Plan/architecture stage, not requests for this headless run to wait.

1. **Structured scope:** Should the first contract cover acceptance IDs/text only, or typed user-story/task/architecture/dependency data as well? The latter is broader; the demonstrated semantic parsing failure is specifically acceptance coverage.
2. **Canonical source:** How are structured criteria represented or derived while GitHub remains the sole editable source? What exact data is frozen, and how is its version bound to the body/revision hash?
3. **Owner edits:** Retain the existing edit → refresh/republication requirement, or introduce another explicit reconciliation flow? How should edited generated sections be interpreted and validated?
4. **Legacy behavior:** Which old proposals/published stories continue using the old parser? Are existing unfinished stories explicitly republished, and how are already frozen runs and completed stories protected? Does any repair of historical blocked stories belong to a separate operational task?
5. **Identifier contract:** What charset, length in bytes, uniqueness scope, maximum count, and ordering apply? Are IDs agent-selected and validated or factory-assigned from items? How do IDs survive edits and reordering? Limits must match evidence capacity.
6. **Rendering contract:** Which sections become deterministic, how are prose and dependencies represented, and what renderer/schema version enters compatibility decisions? How are sanitization and redaction incorporated into semantic/hash consistency?
7. **Publisher ownership:** What bounded selector does the agent provide for a selected frozen story? Who allocates global event sequence and per-story attempt when the factory owns the five identity fields? Does factory ownership include the complete publication operation or only a generated template?
8. **Live feedback:** What channel is available to both supported agents, and what receipt establishes accepted verification? How does the author obtain field/type diagnostics, correct using a new sequence, and know whether story effects are still pending?
9. **Lifecycle compatibility:** How does a new envelope/receipt version interact with resumed/fallback attempts, invalidation, replay, concurrent story publication, and finish-time transport retirement?

10. **Diagnostic completeness:** Must one response list every independently discoverable error, including type errors and semantic item failures? What limits apply after malformed syntax or containers, and what stable indexed-path/expected-type/observed-type representation is promised?
11. **Independent feedback deliverable:** Does the first improvement provide callable preflight, authoritative receipts, or both? Preflight alone addresses schema guessing before publication but cannot prove accepted completion. Assess distribution/tool availability for both agents before estimating effort.
12. **Durability and waiting:** Where are diagnostic reasons retained, how are event submissions correlated without trusting malformed identities, and how does an agent wait through polling/backoff, crash recovery and finish-time retirement? What result authorizes a final success report?
13. **Incident provenance:** Run 14 is still pending in the supplied evidence; the third alleged rejected field and the remaining offline defect are unknown. These gaps do not block planning the demonstrated class and should not be silently filled from assumptions.

## Recommended scope for Plan mode

Treat structured acceptance contracts and factory-owned story-progress identities as two connected parts of this issue. Prefer removing factory-known fields from agent-authored payloads over merely substituting clearer example placeholders. Retain independent authorization, revision, evidence, and snapshot verification. Include timely field/type feedback and same-run correction as explicit scope alongside envelope ownership; prefilling identities alone does not handle other malformed evidence. Evaluate all-independent-errors preflight to avoid serial discovery, while distinguishing preflight validation from authoritative acceptance receipts. The owner ranks factory ownership first and feedback as independently valuable; this priority does not establish relative implementation effort.

Define criterion IDs and shared limits at proposal acceptance, deterministic story rendering, and propagation of canonical criteria through the approved frozen representation. Make the GitHub editing and legacy compatibility policies explicit before choosing the storage/serialization design. Consider the other required sections as a deliberate scope choice, with special attention to the already separate dependency array; avoid assuming they all have the acceptance parser's behavior.

Do not expand this work into changing evidence truth requirements, replacing GitHub as the editable source, giving agents forge authority, or silently repairing historical runtime state. Run 0002 assumes the owner wants the original structured Plan contract plus both event authoring and feedback improvements addressed in the next Plan; the third attempt is treated as pending, while preserving the existing frozen-run and owner-republication guarantees unless Plan explicitly revises them. This brief completes Research only; it does not approve an implementation plan.
