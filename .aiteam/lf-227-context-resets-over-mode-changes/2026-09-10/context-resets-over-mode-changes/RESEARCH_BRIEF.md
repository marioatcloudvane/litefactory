# Research Brief — context-resets-over-mode-changes

## Request

[O] The owner asks whether changing modes, especially Research → Plan and Plan → Implement, can reset the coding agent's conversation so the next phase starts fresh. They ask whether each phase leaves enough durable context, whether anything would be lost, and specifically whether a completed research brief is sufficient to begin planning in a new conversation.

This run researches existing behavior and handoff feasibility only. It does not implement a reset policy, execute tests, or start Plan. The headless instruction rules out clarification or approval waiting.

## Codebase Context

### Scope and evidence

[V] The codebase-tour specialist inspected the repository's mode admission, conversation selection, prompt construction, artifact discovery, managed workflow inputs, both native adapters and relevant test source. This is a single Go 1.26 module with implementation and colocated tests under `internal/` (`go.mod:1`). The supplied optional `.aiteam/rulebook.md` does not exist.

Provenance: `[V]` identifies repository behavior or test source inspected during this run; `[O]` identifies the owner's request. Conclusions inferred from those observations are explicitly called inferences. Test source inspection does not mean those tests were executed. No prior session artifacts or external product documentation were used.

### Three kinds of context must be distinguished

[V] Native conversation identity is separate from both the task/worktree and the artifact session. The shared agent session reference contains a factory identity and an optional vendor resume handle (`internal/agent/session.go:3`). Turn preparation selects a conversation segment and previous terminal run, loads recorded comments and durable managed inputs, and builds the shared prompt (`internal/turn/prepare.go:202`, `internal/turn/prepare.go:247`, `internal/turn/prepare.go:328`).

The feasible reset is therefore a new **native conversation**, while retaining the task's files, artifact session and authoritative workflow state. Deleting those durable inputs would undermine the intended handoff. Starting a new subprocess by itself does not establish a new conversation when that process resumes an existing native session.

### Current mode-change behavior is configuration-dependent

| Path | Observed behavior | Evidence |
|---|---|---|
| External team instruction delivery | Mode participates in delivery identity. A different selected mode changes that identity and allocates a new conversation segment, producing a cold start. | [V] `internal/turn/delivery.go:40`, `internal/turn/delivery.go:104`, `internal/turn/delivery.go:138`, `internal/turn/delivery.go:346` |
| No external delivery or prior delivery history | Selection requests the first segment; resume selection does not compare the old and new modes. A prior terminal run can therefore be resumed across modes. | [V] `internal/turn/delivery.go:315`, `internal/turn/prepare.go:1003` |
| Return to an earlier mode with external delivery | Selection compares against the latest segment and increments its ordinal for a changed identity. It does not restore an old conversation associated with that mode. | [V] `internal/turn/delivery.go:328` |
| Same mode with other delivery changes | Delivery identity also covers files, snapshot, tools and sandbox, so mode is not the only reason a segment can rotate. | [V] `internal/turn/delivery.go:138` |

[V] Admitted owner mode changes reach downstream prompt construction, permission profiles and stored runs (`internal/turn/mode_transition_test.go:14`). Segment selection precedes resume selection: a previous terminal run in the selected segment permits Claude resume, while Codex additionally requires its retained resume token (`internal/turn/prepare.go:963`, `internal/turn/prepare.go:1003`).

**Finding:** an explicit universal “reset on mode change” policy is not currently present. The desired effect already exists on the external-delivery path. This run did not inspect live daemon configuration, so it does not claim which path issue #227 currently uses.

### Both adapters have cold and resume paths

[V] Claude cold execution uses `--session-id`; continuation uses `--resume`. The adapter documents that starting cold with an already-used session ID can be refused as a duplicate (`internal/harness/runner.go:548`, `internal/harness/runner.go:589`). A reset therefore needs a genuinely fresh conversation identity, not merely a changed resume boolean.

[V] Codex cold execution ends with stdin `-`; continuation uses `resume <token> -` (`internal/codex/runner.go:397`, `internal/codex/runner.go:444`). The repository already separates its vendor token from the factory identity. These findings establish repository adapter support, not independent validation of current external CLI behavior.

### Recovery has different semantics from a phase transition

[V] A refused resume receives one cold retry with the same run identity, a fresh Plan attempt transport and a rebuilt full prompt (`internal/turn/harvest.go:173`). That is a recovery path, not a universal mode-reset policy.

[V] Automatic recovery of an interrupted run explicitly requires the original segment and a usable resume identity. It refuses unusable conversations instead of falling back cold (`internal/turn/prepare.go:261`, `internal/turn/harvest.go:184`). A phase-reset change must distinguish a newly admitted mode from continuation of already admitted interrupted work.

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

[V] Managed Plan first locates identity-bound `PLAN_CONTEXT.json`. If absent, it looks for `plan_brief.md`, then `RESEARCH_BRIEF.md`, and puts its next context file in that artifact directory (`internal/turn/plan_attempt.go:40`, `internal/turn/plan_attempt.go:47`). The research brief is explicitly intended as an input to Plan (`.codex/skills/research-orchestrator/SKILL.md:46`).

[V] In that runtime path, the research brief is a directory-discovery marker; its body is not automatically injected into the prompt. The next agent needs to read the brief in the supplied session (`internal/turn/plan_attempt.go:40`).

**Inference:** the owner's proposed handoff is sound when the brief records the problem, verified findings and sources, assumptions, constraints, risks, unanswered questions and useful boundaries for planning. A summary that omits a material owner decision or operational limitation will not be repaired by starting fresh. Presence of a brief should not be confused with completeness or with automatic consumption of its content.

#### Plan → Implement

[V] Implementation receives complete immutable approved story bodies, their issue identities and revisions, dependencies, persisted workflow version and completion state from the store (`internal/turn/approved_context.go:18`). Managed admission freezes approved scope; having a local Plan brief alone does not authorize implementation (`internal/turn/managed_scope.go:55`).

[V] The shared Plan brief carries context across stories and has a link-only Story Index. Its shared decisions are not automatically loaded inline by approved-context preparation; the agent relies on the session/artifact reading contract (`.claude/modes/session.md:181`, `internal/turn/approved_context.go:18`).

**Inference:** the authoritative scope is robust to losing the native transcript, but architecture rationale and shared constraints must remain discoverable in the shared brief. The reset must retain approved/frozen inputs rather than rebuilding scope from conversation history, local story copies or current live issue edits.

#### Implement → Validate

[V] Managed separated Validate reconstructs frozen criteria and retained engineering snapshots/checks from store evidence. It checks attribution, revision and digest (`internal/turn/validation.go:17`, `internal/turn/validation.go:66`). Validation and approved-story sections render on every prompt path (`internal/harness/prompt.go:463`).

**Inference:** this is the strongest examined cold handoff because validation should assess durable evidence independently of the preceding agent's narrative. Missing or invalid evidence is still a blocker after a reset; starting fresh does not weaken acceptance requirements or authorize executing new checks in Validate.

#### Same-mode continuation and saved planning discussions

[V] Saved Plan context is parsed and bound to parent task, branch and feature before prompt injection; multiple matching contexts are rejected rather than choosing the newest date (`internal/turn/plan_context.go:18`, `internal/turn/plan_context.go:58`). Scope, decisions, assumptions and questions are emitted before the prompt's resume/cold branch (`internal/harness/prompt.go:410`). This supports continuing a discussion without a completed proposal and without relying exclusively on native memory.

**Inference:** resetting at phase boundaries does not require resetting every reply within a phase. Those are separate product decisions. If a reset is allowed after an incomplete phase, working context must be saved first or the next run must explicitly report what is missing.

#### Reverse transitions, gates and Spike

[V] Managed approval does not permit arbitrary subsequent mode switching: explicit separated Validate is admitted, reopening Plan requires `replan` or `/plan`, and other non-Implement modes are refused under the relevant managed scope gate (`internal/turn/managed_scope.go:18`, `internal/turn/managed_scope.go:55`). Reset behavior must follow admitted transitions, not bypass these gates.

[V] Spike requires `SPIKE.md`, but Plan's marker lookup uses Plan context, Plan brief and Research brief, not Spike (`.claude/modes/spike.md:14`, `internal/turn/plan_attempt.go:47`). The examined prompt construction does not expose a general session-path field injected for every mode.

**Inference:** forward managed handoffs are better supported than a universal “any mode to any mode” guarantee. Spike and reverse transitions warrant deliberate session and feedback handoffs before promising equivalent completeness.

### Session continuity across dates

[V] Session instructions require reuse of the supplied directory across dates and cold/fallback attempts (`.claude/modes/session.md:13`). Runtime cross-date discovery exists for recognized artifacts (`internal/turn/sessiondir.go:36`). Identity-bound Plan context is preferred and ambiguous matching contexts fail explicitly (`internal/turn/plan_context.go:18`).

**Inference:** a new native conversation should continue using the same artifact session, even on a different day. Creating a fresh dated artifact directory merely because the conversation is fresh could strand prior context. The verified discovery paths do not establish universal, ambiguity-free handoff for every mode and every possible artifact combination.

### What a cold run restores—and what it can lose

[V] Cold prompts include the issue/current instruction and bounded recorded comment history. Issue and individual comment bodies are capped at 8 KiB; aggregate history is capped at 32 KiB and drops older entries (`internal/harness/prompt.go:71`, `internal/harness/prompt.go:981`). Recorded history may omit comments predating initial watch/bootstrap (`internal/turn/prepare.go:1034`).

Consequences inferred from those limits:

- Durable files, frozen scope and valid retained evidence can survive a native reset.
- Unwritten conclusions, decision rationale, transient debugging state and detailed tool output do not automatically survive.
- Even owner comments are not an unlimited substitute for a handoff: old or long material can be absent or truncated.
- “Fresh context” currently still includes selected historical task information. Requiring no historical comments would be a different behavior with additional loss risk.
- A successful process start or an existing artifact does not prove the receiving agent read and understood the necessary handoff.

## Constraints & Risks

1. **Configuration-dependent reset semantics.** [V] External delivery rotates segments when mode changes; the no-delivery path lacks that comparison (`internal/turn/delivery.go:315`, `internal/turn/delivery.go:346`, `internal/turn/prepare.go:1003`). A product promise of universal reset would exceed current behavior.
2. **Identity correctness.** [V] Claude cold starts cannot safely reuse an already-used native session ID; Codex continuation has a separate retained token (`internal/harness/runner.go:548`, `internal/codex/runner.go:444`). A reset must change the intended native conversation without losing task identity.
3. **Handoff completeness is partly procedural.** [V] Research and shared Plan bodies are not inlined by the examined preparation paths (`internal/turn/plan_attempt.go:40`, `internal/turn/approved_context.go:18`). Inference: a fresh start is only as complete as the retained material and its consumption.
4. **Artifact path reliability differs by transition.** [V] Cross-date recognized-artifact lookup exists, but Spike is not a Plan marker and a universal injected session path was not found in the examined construction (`internal/turn/sessiondir.go:36`, `internal/turn/plan_attempt.go:47`).
5. **Conversation history is bounded and incomplete.** [V] Cold history truncation and pre-watch omissions are intentional (`internal/harness/prompt.go:981`, `internal/turn/prepare.go:1034`). Inference: it should be supporting context, not the sole record of scope or decisions.
6. **Recovery and authorization must remain intact.** [V] Interrupted recovery requires its existing conversation, and managed transitions enforce scope gates (`internal/turn/prepare.go:261`, `internal/turn/managed_scope.go:18`). Resetting must not silently turn recovery into a new phase or reopen approved scope.
7. **Evidence limits.** [V] Existing tests cover mechanisms, not semantic completeness of real handoffs. Neither actual Claude/Codex execution nor current suite success was established by this research.

### Relevant existing test coverage (inspected, not executed)

| Area | What test source establishes | Reference |
|---|---|---|
| Delivery identity and segments | Mode participates in identity; changed identities rotate segments and equal identities reuse them. | [V] `internal/turn/delivery_test.go:54`, `internal/turn/delivery_test.go:162` |
| Admitted mode propagation | New mode reaches prompt, permission profile and stored run. | [V] `internal/turn/mode_transition_test.go:14` |
| Plan continuation | Identity-bound context, cross-date discovery and continuation behavior. | [V] `internal/turn/plan_continuation_test.go:51`, `internal/turn/plan_continuation_test.go:135` |
| Approved attempts | Frozen approval inputs survive resume/fallback paths. | [V] `internal/turn/approved_attempts_test.go:31` |
| Validation handoff | Retained snapshots and check inputs are carried through adapter paths. | [V] `internal/turn/validation_test.go:44` |

The future validation question is not only whether the native ID changed. It is whether an independently started receiver gets the correct session, authoritative scope, material decisions and relevant evidence for each supported boundary. This is a research finding about evidence needs, not an implementation plan.

## Open Questions

These are decisions for the owner and Plan mode; no answer was requested during this headless run.

1. Should every admitted mode change explicitly start a fresh conversation, including deployments without external team delivery? The reasonable assumed intent is yes.
2. Does “fresh context” permit the bounded historical comments currently included in cold prompts? This research assumes yes: discard native conversational state while preserving useful task facts.
3. Is the boundary any admitted mode change, or only a completed phase followed by another phase? What durable working context is required when leaving an incomplete phase?
4. Should returning to a prior mode always create another new conversation? External delivery already behaves that way; this research assumes that matches the request better than resurrecting old per-mode conversations.
5. What minimum handoff content is required, and how should missing/incomplete/ambiguous artifacts be reported? Artifact presence cannot certify semantic completeness.
6. Should every mode receive an explicitly bound artifact-session path, including Spike and reverse transitions, to avoid reliance on differing discovery rules?
7. How should shared Plan decisions and actionable Validate findings be delivered to a new implementation conversation? The precise feedback completeness contract remains unresolved.
8. Which live delivery configuration must be supported? This run establishes repository branches, not the daemon configuration for this issue.

## Recommended scope for Plan mode

Define a consistent policy for starting a new native conversation on **authorized mode changes**, preserving task/worktree identity, the artifact session, approval/frozen revisions and retained evidence. Clarify the handoff requirements for each supported transition, especially Research → Plan and Plan → Implement, and the handling of incomplete or ambiguous artifacts. Keep same-mode continuation, interrupted-run recovery and resume-refusal fallback distinct from phase changes.

Do not expand this into deletion of task history or files, replacement of approved scope with transcript summaries, reopening accepted work, or a guarantee of zero loss from arbitrary unsaved reasoning. The evidence supports a fresh-context handoff using durable inputs; stronger guarantees require a defined completeness contract.

Assumptions for the headless request: “context reset” means native conversation reset, not a new worktree or erased artifacts; retained comments and authoritative workflow inputs remain allowed; the desired behavior should apply to both adapters. Research is complete. No implementation or mode transition was performed.
