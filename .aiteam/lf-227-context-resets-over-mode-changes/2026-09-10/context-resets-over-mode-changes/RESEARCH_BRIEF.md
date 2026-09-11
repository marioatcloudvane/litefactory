# Research Brief — context-resets-over-mode-changes

## Request

[O] Run 0002, 2026-09-11: “Give me all open questions that need my decision. Give me a recommendation per open question. Also show me things you could answer on your own.” This iteration separates owner policy decisions from independently answerable facts and engineering defaults. Recommendations below remain proposals, not approved decisions.

[O] The owner asks whether changing modes, especially Research → Plan and Plan → Implement, can reset the coding agent's conversation so the next phase starts fresh. They ask whether each phase leaves enough durable context, whether anything would be lost, and specifically whether a completed research brief is sufficient to begin planning in a new conversation.

This run researches existing behavior and handoff feasibility only. It does not implement a reset policy, execute tests, or start Plan. The headless instruction rules out clarification or approval waiting.

## Codebase Context

### Scope and evidence

[R0] The codebase-tour specialist inspected the repository's mode admission, conversation selection, prompt construction, artifact discovery, managed workflow inputs, both native adapters and relevant test source. This is a single Go 1.26 module with implementation and colocated tests under `internal/` (`go.mod:1`). The supplied optional `.aiteam/rulebook.md` does not exist.

Provenance: `[R0]` identifies findings retained from run 0001; `[V]` identifies source findings verified by the codebase-tour specialist in run 0002; `[O]` identifies the owner's request. The historical sections below retain their original scope and evidence statements as run-0001 history; the current verification and decision inventory follow them. Conclusions inferred from those observations are explicitly called inferences. Test source inspection does not mean those tests were executed. No prior session artifacts or external product documentation were used.

### Three kinds of context must be distinguished

[R0] Native conversation identity is separate from both the task/worktree and the artifact session. The shared agent session reference contains a factory identity and an optional vendor resume handle (`internal/agent/session.go:3`). Turn preparation selects a conversation segment and previous terminal run, loads recorded comments and durable managed inputs, and builds the shared prompt (`internal/turn/prepare.go:202`, `internal/turn/prepare.go:247`, `internal/turn/prepare.go:328`).

The feasible reset is therefore a new **native conversation**, while retaining the task's files, artifact session and authoritative workflow state. Deleting those durable inputs would undermine the intended handoff. Starting a new subprocess by itself does not establish a new conversation when that process resumes an existing native session.

### Current mode-change behavior is configuration-dependent

| Path | Observed behavior | Evidence |
|---|---|---|
| External team instruction delivery | Mode participates in delivery identity. A different selected mode changes that identity and allocates a new conversation segment, producing a cold start. | [R0] `internal/turn/delivery.go:40`, `internal/turn/delivery.go:104`, `internal/turn/delivery.go:138`, `internal/turn/delivery.go:346` |
| No external delivery or prior delivery history | Selection requests the first segment; resume selection does not compare the old and new modes. A prior terminal run can therefore be resumed across modes. | [R0] `internal/turn/delivery.go:315`, `internal/turn/prepare.go:1003` |
| Return to an earlier mode with external delivery | Selection compares against the latest segment and increments its ordinal for a changed identity. It does not restore an old conversation associated with that mode. | [R0] `internal/turn/delivery.go:328` |
| Same mode with other delivery changes | Delivery identity also covers files, snapshot, tools and sandbox, so mode is not the only reason a segment can rotate. | [R0] `internal/turn/delivery.go:138` |

[R0] Admitted owner mode changes reach downstream prompt construction, permission profiles and stored runs (`internal/turn/mode_transition_test.go:14`). Segment selection precedes resume selection: a previous terminal run in the selected segment permits Claude resume, while Codex additionally requires its retained resume token (`internal/turn/prepare.go:963`, `internal/turn/prepare.go:1003`).

**Finding:** an explicit universal “reset on mode change” policy is not currently present. The desired effect already exists on the external-delivery path. This run did not inspect live daemon configuration, so it does not claim which path issue #227 currently uses.

### Both adapters have cold and resume paths

[R0] Claude cold execution uses `--session-id`; continuation uses `--resume`. The adapter documents that starting cold with an already-used session ID can be refused as a duplicate (`internal/harness/runner.go:548`, `internal/harness/runner.go:589`). A reset therefore needs a genuinely fresh conversation identity, not merely a changed resume boolean.

[R0] Codex cold execution ends with stdin `-`; continuation uses `resume <token> -` (`internal/codex/runner.go:397`, `internal/codex/runner.go:444`). The repository already separates its vendor token from the factory identity. These findings establish repository adapter support, not independent validation of current external CLI behavior.

### Recovery has different semantics from a phase transition

[R0] A refused resume receives one cold retry with the same run identity, a fresh Plan attempt transport and a rebuilt full prompt (`internal/turn/harvest.go:173`). That is a recovery path, not a universal mode-reset policy.

[R0] Automatic recovery of an interrupted run explicitly requires the original segment and a usable resume identity. It refuses unusable conversations instead of falling back cold (`internal/turn/prepare.go:261`, `internal/turn/harvest.go:184`). A phase-reset change must distinguish a newly admitted mode from continuation of already admitted interrupted work.

## Domain / Problem Space

### Feasibility conclusion

**Inference from verified handoff and adapter behavior:** yes, a fresh conversation at an authorized mode boundary is feasible, and Research → Plan is a particularly natural boundary. The research brief can be the durable starting point. However, “fresh” must mean a fresh conversation rebuilt from retained inputs; it cannot mean eliminating all prior task information. The repository already relies on this reconstruction for cold runs and some resume failures.

It is not possible to guarantee zero information loss from file existence alone. Only information captured in artifacts, approved story revisions, retained evidence or included comments will survive. A transcript may contain uncaptured decisions or tool observations that no cold prompt can recreate.

### Handoff inventory

| Transition | Durable inputs available | Completeness and discovery limitations |
|---|---|---|
| Research → Plan | Research brief; issue/current instruction; bounded recorded comments; existing identity-bound Plan context if present. | Brief content is not automatically inlined. The agent must read it. There is no mechanical proof that the brief captures every relevant conversation decision. |
| Plan → Implement | Frozen approved story bodies and revisions; identities, dependencies, workflow version and completion state; shared Plan brief in the worktree. | Approval/freeze is required. Shared decisions in the Plan brief are not automatically inlined by approved-context preparation. |
| Implement → Validate | Frozen criteria plus retained engineering snapshots and executed check evidence. | Missing, malformed, stale or misattributed evidence is unverified; failed checks remain failed. A transcript cannot substitute for this evidence. |
| Plan discussion → another Plan iteration | Identity-bound saved scope, decisions, assumptions and unresolved questions. | No completed proposal is necessary, but the saved context must actually capture material discussion. Same-mode continuity is a separate policy question from mode changes. |
| Validate → Implement or another reverse transition | Retained task/workflow state and whatever durable findings/artifacts are supplied. | Admission gates still apply; completeness of actionable feedback to a new implementation conversation needs an explicit handoff decision. |
| Spike → another mode | Spike report is required by the mode contract. | Managed Plan directory selection does not use a Spike report as a discovery marker. |

#### Research → Plan

[R0] Managed Plan first locates identity-bound `PLAN_CONTEXT.json`. If absent, it looks for `plan_brief.md`, then `RESEARCH_BRIEF.md`, and puts its next context file in that artifact directory (`internal/turn/plan_attempt.go:40`, `internal/turn/plan_attempt.go:47`). The research brief is explicitly intended as an input to Plan (`.codex/skills/research-orchestrator/SKILL.md:46`).

[R0] In that runtime path, the research brief is a directory-discovery marker; its body is not automatically injected into the prompt. The next agent needs to read the brief in the supplied session (`internal/turn/plan_attempt.go:40`).

**Inference:** the owner's proposed handoff is sound when the brief records the problem, verified findings and sources, assumptions, constraints, risks, unanswered questions and useful boundaries for planning. A summary that omits a material owner decision or operational limitation will not be repaired by starting fresh. Presence of a brief should not be confused with completeness or with automatic consumption of its content.

#### Plan → Implement

[R0] Implementation receives complete immutable approved story bodies, their issue identities and revisions, dependencies, persisted workflow version and completion state from the store (`internal/turn/approved_context.go:18`). Managed admission freezes approved scope; having a local Plan brief alone does not authorize implementation (`internal/turn/managed_scope.go:55`).

[R0] The shared Plan brief carries context across stories and has a link-only Story Index. Its shared decisions are not automatically loaded inline by approved-context preparation; the agent relies on the session/artifact reading contract (`.claude/modes/session.md:181`, `internal/turn/approved_context.go:18`).

**Inference:** the authoritative scope is robust to losing the native transcript, but architecture rationale and shared constraints must remain discoverable in the shared brief. The reset must retain approved/frozen inputs rather than rebuilding scope from conversation history, local story copies or current live issue edits.

#### Implement → Validate

[R0] Managed separated Validate reconstructs frozen criteria and retained engineering snapshots/checks from store evidence. It checks attribution, revision and digest (`internal/turn/validation.go:17`, `internal/turn/validation.go:66`). Validation and approved-story sections render on every prompt path (`internal/harness/prompt.go:463`).

**Inference:** this is the strongest examined cold handoff because validation should assess durable evidence independently of the preceding agent's narrative. Missing or invalid evidence is still a blocker after a reset; starting fresh does not weaken acceptance requirements or authorize executing new checks in Validate.

#### Same-mode continuation and saved planning discussions

[R0] Saved Plan context is parsed and bound to parent task, branch and feature before prompt injection; multiple matching contexts are rejected rather than choosing the newest date (`internal/turn/plan_context.go:18`, `internal/turn/plan_context.go:58`). Scope, decisions, assumptions and questions are emitted before the prompt's resume/cold branch (`internal/harness/prompt.go:410`). This supports continuing a discussion without a completed proposal and without relying exclusively on native memory.

**Inference:** resetting at phase boundaries does not require resetting every reply within a phase. Those are separate product decisions. If a reset is allowed after an incomplete phase, working context must be saved first or the next run must explicitly report what is missing.

#### Reverse transitions, gates and Spike

[R0] Managed approval does not permit arbitrary subsequent mode switching: explicit separated Validate is admitted, reopening Plan requires `replan` or `/plan`, and other non-Implement modes are refused under the relevant managed scope gate (`internal/turn/managed_scope.go:18`, `internal/turn/managed_scope.go:55`). Reset behavior must follow admitted transitions, not bypass these gates.

[R0] Spike requires `SPIKE.md`, but Plan's marker lookup uses Plan context, Plan brief and Research brief, not Spike (`.claude/modes/spike.md:14`, `internal/turn/plan_attempt.go:47`). The examined prompt construction does not expose a general session-path field injected for every mode.

**Inference:** forward managed handoffs are better supported than a universal “any mode to any mode” guarantee. Spike and reverse transitions warrant deliberate session and feedback handoffs before promising equivalent completeness.

### Session continuity across dates

[R0] Session instructions require reuse of the supplied directory across dates and cold/fallback attempts (`.claude/modes/session.md:13`). Runtime cross-date discovery exists for recognized artifacts (`internal/turn/sessiondir.go:36`). Identity-bound Plan context is preferred and ambiguous matching contexts fail explicitly (`internal/turn/plan_context.go:18`).

**Inference:** a new native conversation should continue using the same artifact session, even on a different day. Creating a fresh dated artifact directory merely because the conversation is fresh could strand prior context. The verified discovery paths do not establish universal, ambiguity-free handoff for every mode and every possible artifact combination.

### What a cold run restores—and what it can lose

[R0] Cold prompts include the issue/current instruction and bounded recorded comment history. Issue and individual comment bodies are capped at 8 KiB; aggregate history is capped at 32 KiB and drops older entries (`internal/harness/prompt.go:71`, `internal/harness/prompt.go:981`). Recorded history may omit comments predating initial watch/bootstrap (`internal/turn/prepare.go:1034`).

Consequences inferred from those limits:

- Durable files, frozen scope and valid retained evidence can survive a native reset.
- Unwritten conclusions, decision rationale, transient debugging state and detailed tool output do not automatically survive.
- Even owner comments are not an unlimited substitute for a handoff: old or long material can be absent or truncated.
- “Fresh context” currently still includes selected historical task information. Requiring no historical comments would be a different behavior with additional loss risk.
- A successful process start or an existing artifact does not prove the receiving agent read and understood the necessary handoff.

## Constraints & Risks

1. **Configuration-dependent reset semantics.** [R0] External delivery rotates segments when mode changes; the no-delivery path lacks that comparison (`internal/turn/delivery.go:315`, `internal/turn/delivery.go:346`, `internal/turn/prepare.go:1003`). A product promise of universal reset would exceed current behavior.
2. **Identity correctness.** [R0] Claude cold starts cannot safely reuse an already-used native session ID; Codex continuation has a separate retained token (`internal/harness/runner.go:548`, `internal/codex/runner.go:444`). A reset must change the intended native conversation without losing task identity.
3. **Handoff completeness is partly procedural.** [R0] Research and shared Plan bodies are not inlined by the examined preparation paths (`internal/turn/plan_attempt.go:40`, `internal/turn/approved_context.go:18`). Inference: a fresh start is only as complete as the retained material and its consumption.
4. **Artifact path reliability differs by transition.** [R0] Cross-date recognized-artifact lookup exists, but Spike is not a Plan marker and a universal injected session path was not found in the examined construction (`internal/turn/sessiondir.go:36`, `internal/turn/plan_attempt.go:47`).
5. **Conversation history is bounded and incomplete.** [R0] Cold history truncation and pre-watch omissions are intentional (`internal/harness/prompt.go:981`, `internal/turn/prepare.go:1034`). Inference: it should be supporting context, not the sole record of scope or decisions.
6. **Recovery and authorization must remain intact.** [R0] Interrupted recovery requires its existing conversation, and managed transitions enforce scope gates (`internal/turn/prepare.go:261`, `internal/turn/managed_scope.go:18`). Resetting must not silently turn recovery into a new phase or reopen approved scope.
7. **Evidence limits.** [R0] Existing tests cover mechanisms, not semantic completeness of real handoffs. Neither actual Claude/Codex execution nor current suite success was established by this research.

### Relevant existing test coverage (inspected, not executed)

| Area | What test source establishes | Reference |
|---|---|---|
| Delivery identity and segments | Mode participates in identity; changed identities rotate segments and equal identities reuse them. | [R0] `internal/turn/delivery_test.go:54`, `internal/turn/delivery_test.go:162` |
| Admitted mode propagation | New mode reaches prompt, permission profile and stored run. | [R0] `internal/turn/mode_transition_test.go:14` |
| Plan continuation | Identity-bound context, cross-date discovery and continuation behavior. | [R0] `internal/turn/plan_continuation_test.go:51`, `internal/turn/plan_continuation_test.go:135` |
| Approved attempts | Frozen approval inputs survive resume/fallback paths. | [R0] `internal/turn/approved_attempts_test.go:31` |
| Validation handoff | Retained snapshots and check inputs are carried through adapter paths. | [R0] `internal/turn/validation_test.go:44` |

The future validation question is not only whether the native ID changed. It is whether an independently started receiver gets the correct session, authoritative scope, material decisions and relevant evidence for each supported boundary. This is a research finding about evidence needs, not an implementation plan.

## Run 0002 — Current verification and independently answerable questions

[V] A fresh codebase tour rechecked current source without executing tests, modifying production files, reading live daemon configuration or invoking either external agent CLI. The optional rulebook remains absent. The following are current findings; earlier sections tagged `[R0]` preserve the first run's technical detail rather than silently treating all old citations as freshly checked.

### Answers established independently

| Question | Answer and evidence |
|---|---|
| Is a fresh conversation technically feasible for both agents? | Yes at the repository adapter level. Claude distinguishes `--session-id` from `--resume`, and Codex adds `resume <handle>` only on resume. A genuinely new native identity is needed; disabling resume on an occupied Claude identity is insufficient. [V] `internal/harness/runner.go:545`, `internal/harness/runner.go:589`, `internal/codex/runner.go:444`. Actual external CLI execution was not tested. |
| Does every mode change already reset? | No. External delivery includes mode in identity and creates a new segment when it changes. Without external delivery or a prior delivered segment, the first segment is reused; resume selection checks predecessor/agent/capability, not mode or phase completion. There is even test source for resumed mode changes. [V] `internal/turn/prepare.go:129`, `internal/turn/delivery.go:143`, `internal/turn/delivery.go:318`, `internal/turn/delivery.go:332`, `internal/turn/prepare.go:986`, `internal/harness/prompt_test.go:500`. |
| Can Research → Plan start from the brief? | Yes, with the receiving agent reading the brief. Plan uses a prior Plan context first and otherwise uses the Plan brief or Research brief to select its artifact directory. This lookup neither injects the Research brief body nor certifies completeness. [V] `internal/turn/plan_attempt.go:39`, `internal/turn/plan_attempt.go:47`. |
| What must survive Plan → Implement? | Frozen complete approved story bodies, issue identities/revisions, dependencies and workflow version already render before the cold/resume split. Live story drift does not replace them. Shared decisions still live separately in the Plan brief and are not injected by the approved-context constructor. [V] `internal/turn/approved_context.go:18`, `internal/turn/approved_context.go:35`, `internal/turn/approved_context.go:75`, `internal/harness/prompt.go:463`; `.claude/modes/session.md`, “Plan mode’s output”. |
| Does Validate require the implementation transcript? | Its authoritative assessment inputs do not: frozen criteria, snapshots, engineering evidence and retained checks are reconstructed. Missing evidence stays unverified and failed checks stay failed. [V] `internal/turn/validation.go:17`, `internal/turn/validation.go:45`, `internal/turn/validation.go:74`, `internal/turn/validation.go:90`. |
| Does corrective Implement reliably receive Validate feedback? | Findings are retained and rendered in reports; blocking findings can suggest Implement. However, the approved implementation bundle does not attach the prior structured assessment, so reports/history are not an equivalent guaranteed reverse handoff. [V] `internal/turn/validation.go:119`, `internal/turn/validation.go:148`, `internal/agent/validation.go:206`, `internal/turn/approved_context.go:18`. |
| Can unfinished planning survive a fresh start? | There is already identity-bound working context containing scope, decisions, assumptions and questions. Multiple matching contexts fail explicitly. This supports reconstruction only to the extent material information was saved. [V] `internal/turn/plan_context.go:16`, `internal/turn/plan_context.go:48`, `internal/turn/plan_context.go:61`, `internal/turn/plan_attempt.go:62`, `internal/harness/prompt.go:410`. |
| Is a reset a new worktree or a new dated artifact session? | No: conversation identity and artifact location are separate existing concerns. Retaining task/worktree/session identity is a sound engineering default consistent with the session contract; resetting native memory does not require erasing files or changing scope. [V] `internal/agent/session.go:8`, `internal/turn/sessiondir.go:15`; `.claude/modes/session.md`, “Deriving the path”. |
| Is artifact discovery equally reliable everywhere? | No. Identity-bound Plan contexts reject ambiguity, while generic brief discovery prefers today then the newest matching date. Spike is not in Plan's fallback marker list. This warrants explicit session binding as an engineering requirement, rather than asking the owner to select a path mechanism. [V] `internal/turn/plan_context.go:48`, `internal/turn/sessiondir.go:38`, `internal/turn/sessiondir.go:75`, `internal/turn/sessiondir.go:124`, `internal/turn/plan_attempt.go:47`. |
| Can history guarantee no loss? | No. Cold prompts retain at most 32 KiB of comment history, cap individual comments at 8 KiB and drop oldest entries; pre-observation comments may be missing. Unsaved reasoning and tool output are not recovered merely because files exist. [V] `internal/harness/prompt.go:68`, `internal/harness/prompt.go:981`, `internal/turn/prepare.go:1032`. |

### Engineering defaults that do not need an owner decision

These are recommendations inferred from the source and existing contracts, not claims that each mechanism is already implemented:

- **Retain authoritative task state.** Preserve the worktree, identity-bound artifact session, frozen scope, existing approval/readiness rules and retained evidence. Never use a transcript or summary to substitute for approved revisions.
- **Use an explicit artifact-session binding across supported modes.** This avoids divergent date/marker discovery, particularly for Spike and reverse transitions. The precise field/schema is a Plan/engineering choice.
- **Define each handoff from the existing artifact's purpose.** Research carries the problem, verified findings/sources, constraints, decisions versus assumptions, risks and unanswered questions. Plan carries frozen stories plus shared decisions, rationale, dependencies and non-goals. Implement → Validate carries retained snapshot/check evidence. Validate → Implement carries exact blocking findings, affected scope and evidence references. Spike carries findings, decisions and unfinished work. Extend working context for incomplete modes where the current artifact is insufficient; do not invent facts from missing transcripts.
- **Make the receiving phase consume the handoff explicitly.** Passing a known path or injecting bounded material is an engineering choice, but merely locating a directory is insufficient. No mechanical field checklist can prove every relevant thought was captured.
- **Keep recovery semantics separate.** A newly admitted mode and an interrupted attempt are different events. Preserve the current recovery/readiness contracts while defining the reset boundary; a failed or absent required input must not become a claimed success.
- **Target all supported delivery configurations and both adapters.** The live deployment setting is not needed to define a consistent product policy. Live runtime confirmation can be an operator verification during later delivery, without making the user answer a configuration question now.

## Open Questions

Only three independent owner policy decisions remain after grouping the original eight questions. Each can change observable behavior or information retention. No interactive answer is required to complete this Research run, and none of the recommendations is recorded as owner approval.

| ID | Decision for the owner | Recommendation | Practical tradeoff |
|---|---|---|---|
| D1 | **Should every admitted change of mode start a new conversation, across both agents and all delivery configurations, including a return to an earlier mode?** | **Yes.** Reset when the next admitted run changes mode; returning to a prior mode gets another fresh conversation rather than resurrecting its old transcript. Keep ordinary same-mode replies resumable, subject to existing independent reset/recovery reasons. Apply to all five modes where their transition is allowed; preserve managed admission gates. | Predictable behavior and less carryover from the previous role, at the cost of rereading durable context and potentially repeating exploration. This expands consistent reset behavior to configurations that currently resume across modes. |
| D2 | **Should a fresh conversation still receive recorded issue comments?** | **Yes, retain the current bounded comment history initially**, alongside the current instruction and authoritative artifacts. “Fresh” means no native transcript carryover, not a task stripped of useful background. Avoid making history the only record of a material decision. | Keeps useful owner rationale and compatibility with existing prompts, but older statements can remain distracting and consume context. Removing history would produce a cleaner prompt while making omissions in handoff documents more consequential. |
| D3 | **When a mode is changed before its work is complete, or its handoff is missing, should the next phase be allowed to proceed?** | **Do not require every outgoing phase to be complete merely to change modes.** After normal admission, start fresh from saved work, reconstruct from authoritative retained sources where possible, and explicitly report omissions. Stop only work that requires missing or ambiguous authoritative inputs; leave that work incomplete with a concrete missing-input report. For example, Plan can continue from an incomplete research brief, but Implement still needs approved scope and Validate still needs valid evidence to establish acceptance. | Supports useful early moves and detours without inventing missing decisions. It may require rereading or later clarification, and some work must stop when safe reconstruction is impossible. Requiring a fully complete handoff for every transition would be stricter but would impede normal incomplete/research-first work; continuing regardless would risk incorrect work. |

D3 defines the product policy for incomplete handoffs; engineering determines the minimum per-transition fields and how to communicate a missing-input result under the existing workflow. Nothing here weakens acceptance gates, introduces a new approval flow, or permits starting Implement from an unapproved partial Plan.

### Disposition of all eight earlier open questions

| Run-0001 question | Current disposition |
|---|---|
| 1. Universal reset, including no external delivery? | Owner decision D1; recommend yes. |
| 2. Keep bounded historical comments? | Owner decision D2; recommend yes initially. |
| 3. Any mode change or only completed phases; incomplete context? | D1 defines the reset event; owner decision D3 defines continuation with incomplete inputs. No completion predicate exists in current conversation selection. |
| 4. Returning to a previous mode: another new conversation? | Included in D1; recommend another fresh conversation, consistent with external-delivery behavior. |
| 5. Minimum handoff and missing/incomplete/ambiguous artifact handling? | Existing contracts and the engineering defaults above establish most content requirements; D3 covers the owner-visible stop/reconstruct/proceed policy. Semantic completeness cannot be guaranteed by existence checks. |
| 6. Explicit artifact-session path for every mode? | Engineering default: yes, bind the existing session explicitly; no storage-layout preference needed from the owner. |
| 7. Shared Plan decisions and Validate feedback into Implement? | Answered as concrete gaps in guaranteed input delivery; consume the shared brief and include retained actionable findings explicitly. Representation is for engineering, not an additional owner choice. |
| 8. Which live delivery configuration must be supported? | Recommend all supported configurations under D1. The actual live setting remains uninspected; it is an operational verification fact, not a product decision required from the owner. |

### Evidence limits and remaining engineering investigation

[V] This iteration establishes source behavior only. No test suite, cold CLI launch, production configuration check or end-to-end semantic handoff was performed. Later engineering must verify both adapters, all supported configuration branches, forward and reverse mode boundaries, incomplete/missing/ambiguous inputs, session reuse across dates, and separation from interrupted-run recovery. These are verification tasks, not unanswered questions for the owner. Repository inspection cannot guarantee that an arbitrary earlier conversation recorded every important fact.

## Recommended scope for Plan mode

Use D1–D3, once decided or explicitly adopted as assumptions, to define consistent fresh native conversations on authorized mode changes. Strengthen durable handoffs and explicit session continuity, including shared Plan context and actionable validation feedback. Define missing-input behavior per phase using existing authority and evidence requirements. Keep same-mode continuation and interrupted-run recovery distinct from mode changes.

Do not expand scope into deleting artifacts/history, changing worktree identity, replacing frozen scope with summaries, weakening acceptance, bypassing managed gates or promising zero loss of unsaved reasoning. This is a research decision inventory, not an implementation plan or a claim of approval.

Assumptions retained: reset means native conversation reset while retaining durable state; both adapters are in scope. Research run 0002 is complete. No implementation, test execution, commit, push or mode transition was performed.
