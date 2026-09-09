# Research Brief — modes-should-stay

## Request

[O] Issue #170 asks for a selected mode to persist across calls instead of being removed after each call. The coding agent should be able to suggest changing modes, for example from Plan to Implement when planning finishes, “or update the label.” The owner emphasizes that several iterations can occur within one mode and that initial planning may involve back-and-forth with the user.

[O] This is factory run 0001, explicitly Research, on `lf/170-modes-should-stay`. The run is headless: no clarification questions, approval waits, commits, pushes, or implementation are requested. Ambiguities are recorded below rather than blocking research.

Provenance: **[V]** verified in this run through the delegated codebase tour; **[O]** stated by the owner; **[W]** unresolved interpretation or recommendation, not an established requirement. No earlier research was carried forward. Repository citations are relative to the worktree root. No external factual research was necessary.

## Codebase Context

### Repository map and existing ownership

[V] This is the Go 1.26 module `github.com/cloudvane/litefactory`, with substantive implementation under `internal/`. Relevant concerns already have owners: daemon polling and routing; turn execution and session lifecycle; shared agent contracts; shared prompts and Claude execution; Codex execution; GitHub transport; SQLite persistence; and managed Plan transport. Existing seams are `agent.Runner`, the separate `agent.Classifier`, daemon `labelForge`, and store mode accessors. Sources: `go.mod:1`, `internal/agent/kind.go:37`, `internal/agent/mode.go:70`, `internal/daemon/daemon.go:87`, `internal/store/modes.go:18`, `internal/github/issues.go:244`.

[V] The runtime recognizes **Research, Plan, Implement, Validate, and Spike**. Router documentation describes four modes and does not recognize Validate among its concrete trusted header modes. This pre-existing drift matters when describing a universal persistent-mode contract; it should not silently exclude a runtime mode. Sources: `internal/agent/mode.go:11`, `CLAUDE.md:17`.

### How the current one-shot mode works

[V] An owner comment passes admission and self-filtering, command parsing, mode resolution, mode-specific permission selection, execution, reporting, and mode-label deletion. The mode is consumed as part of finishing a reported turn rather than retained as a task preference. Sources: `internal/daemon/tick.go:1330`, `internal/daemon/tick.go:1521`, `internal/daemon/tick.go:1629`, `internal/daemon/tick.go:1703`.

[V] Ordinary mode selection prioritizes the durable decision for the same trigger on retry, then a current issue mode label, then an explicit slash command, then classification. An explicit command that disagrees with an existing mode label is rejected. Thus retaining `Mode: Plan` alone would make a subsequent `/implement …` conflict instead of transitioning. Sources: `internal/daemon/tick.go:1541`, `internal/daemon/tick.go:1570`.

[V] Canonical labels use exact values such as `Mode: Plan`. Detection folds prefix case to recognize and reject malformed variants; multiple competing mode labels refuse execution. A persistent mode must preserve deliberate handling of malformed or ambiguous selections. Sources: `internal/daemon/mode.go:20`, `internal/agent/mode.go:50`.

[V] The daemon applies modes chosen by commands or classification as labels before execution. Following a successfully delivered report, it schedules cleanup, removes the selected label, and deletes the durable decision. Cleanup also follows reported unsuccessful work; it is not conditional on a successful implementation or completed artifact. Sources: `internal/daemon/tick.go:1588`, `internal/daemon/tick.go:1707`.

[V] `mode_selections` is a retry journal keyed by `(session_id, trigger_comment_id)`, with label-applied and cleanup-pending fields. It is not a durable task-level “current mode” preference. Reusing it as one without defining trigger and lifecycle semantics would conflate different kinds of state. Sources: `internal/store/migrations/0004_mode_selections.sql:2`, `internal/store/modes.go:9`.

[V] Recovery removes pending mode labels before admitting the next comment. `PendingModeCleanup` can infer cleanup from the consumed-comment watermark even when `cleanup_pending` is unset. Merely removing normal-path deletion leaves a crash-recovery path capable of deleting a supposedly persistent label. Sources: `internal/daemon/mode.go:40`, `internal/store/modes.go:73`.

[V] Persisted comment watermarks govern consumption and stop processing after an unconsumed comment. Production admits one actionable instruction per tick specifically to avoid applying a stale one-shot label snapshot to another comment. Persistent labels change that rationale, but do not eliminate concurrent label edits or stale snapshots. Sources: `internal/daemon/tick.go:1210`, `internal/daemon/tick.go:1229`.

### Managed planning and implementation have separate state

[V] Managed feature states (`planning`, `freezing`, `implementing`) and completed-story locks are separate from issue mode labels, agent-selection labels, sessions, and runs. A label's presence or absence does not alone authorize reopening approved work. Source: `internal/turn/managed_scope.go:57`.

[V] Frozen managed scope forces Implement ahead of ordinary labels and classifier history. Explicit parent `replan` or `/plan <instruction>` selects Plan; child refinement separately forces Plan. Changing a label cannot reopen approved stories. Persistence work must specify how effective mode is presented when managed scope overrides a retained label, without weakening the approved-scope rules. Sources: `internal/daemon/tick.go:1537`, `internal/turn/managed_scope.go:18`.

[V] A successful managed Plan subprocess is currently expected to produce a complete `.litefactory-plan-proposal.json`. A missing proposal fails publication. No successful “discussion iteration” outcome bypasses that requirement. Preparation clears existing readiness; successful publication and saving restore it. Consequently a useful first Plan run that only asks questions can still be treated as a failed handoff. Sources: `internal/turn/plan_handoff.go:33`, `internal/turn/plan_handoff.go:74`, `internal/turn/plan_handoff.go:88`, `internal/turn/plan_handoff.go:141`.

### Agents already receive mode and conversation context

[V] Cold and resumed shared prompts already include the current mode, previous mode, asking-permission changes, and previous failure context. Resume fallback exists and is tested. The gap is therefore not simply a missing mode header: the lifecycle meaning of the header and expected outputs needs to match repeated conversations. Sources: `internal/harness/prompt.go:538`, `internal/harness/prompt.go:708`, `internal/turn/prepare.go:324`, `internal/turn/resume_test.go:40`, `internal/turn/resume_test.go:223`.

[V] Asking is currently per-trigger opt-in: the owner must include the exact standalone `lf: ask` marker, and the run must be Research or Plan. An ordinary reply does not retain the previous run's asking permission. The resumed prompt can describe the reply as an answer while requiring the agent to stop asking further questions. Sources: `internal/turn/ask.go:27`, `internal/turn/ask.go:65`, `internal/turn/ask.go:138`, `internal/turn/prepare.go:287`, `internal/harness/prompt.go:678`.

[V] Asking is asynchronous: the agent ends its process with a final report, the report becomes a GitHub comment, and a later owner comment resumes the conversation. There is no in-process waiting or tracked question-round limit. Multiple planning iterations need to preserve that structure. Sources: `internal/harness/prompt.go:642`, `internal/harness/prompt_ask_test.go:260`.

[V] Requirements-engineer instructions acknowledge asynchronous questions but also direct production of an assumption-based spec despite outstanding material questions. Plan orchestration requires the completed spec and assumes completed plan synthesis. Those conventions do not currently model an unfinished but successful discussion iteration as a first-class result. Sources: `.claude/agents/requirements-engineer.md:34`, `.claude/agents/requirements-engineer.md:119`, `.claude/agents/plan-orchestrator.md:107`, `.claude/agents/plan-orchestrator.md:144`.

[V] Existing “One Run, One Mode” instructions allow recommendations but require a human to open another mode; completing the mode's artifact ends the run. Repeated runs in one mode are conceptually compatible with that boundary. An agent automatically opening Implement is a separate policy change from keeping Plan selected. Source: `CLAUDE.md:115`.

[V] Session artifacts are scoped by branch/date/feature. Existing artifact discovery can locate a session from an earlier day when the requested artifact exists, with tests for date preference, newest session, and missing marker. Cross-day continuity is therefore partly supported already; research does not establish continuity for a discussion-only run that produced no expected artifact. Sources: `internal/turn/sessiondir.go:15`, `internal/turn/sessiondir_test.go:46`.

## Domain / Problem Space

### Distinct outcomes should not be collapsed

[O] The desired user experience is that choosing Plan means subsequent replies continue planning until the owner chooses something else, while the agent can indicate when another mode would be appropriate.

[W] The most reasonable working interpretation is that **a selected mode persists**, **a run still ends**, and **a completed artifact does not automatically select the next mode**. A planning conversation may need several completed runs before the plan itself is complete. This is an assumption for research, not a settled automatic-transition specification.

[V] Today the code separates turn execution from managed plan publication, but publication assumes a successful Plan run means a complete proposal. Asking permissions and orchestration instructions add further completion pressure. Sources: `internal/turn/plan_handoff.go:74`, `internal/turn/ask.go:65`, `.claude/agents/plan-orchestrator.md:107`.

[W] Plan mode must distinguish at least a continuing discussion, a completed plan available for approval/promotion, and a failed run. These are product outcomes to define, not a prescription for a particular result schema. A human-readable recommendation can be delivered in the existing final report; automatically applying an agent recommendation would require a separately defined factory-authorized protocol.

### Persistence needs a source of truth and transition rules

[V] Current labels already participate in selecting the next run, while persisted mode rows protect a specific trigger's retries. Managed scope can override either ordinary labels or classification. Sources: `internal/daemon/tick.go:1537`, `internal/daemon/tick.go:1541`, `internal/store/modes.go:9`.

[W] Leaving the issue mode label in place may provide ordinary persistence without a new task-level database field, but that is a candidate architectural direction, not a proven complete solution. Cleanup recovery, command conflicts, manual label removal, retries, and managed overrides all require explicit semantics before choosing storage changes.

[W] The phrase “or update the label” is ambiguous: it can mean the owner changes the label, or the agent asks the factory to change it. This brief assumes an owner-controlled transition with agent suggestions as the baseline. It does not infer permission to grant the agent forge credentials or make free-form final prose an executable control channel.

[W] Natural-language “move to the next phase” has no dedicated transition parser identified by the tour. A persistent current label would win ordinary selection unless an explicit transition mechanism changes that behavior. The invitation used in agent instructions therefore needs to match an actually supported owner action.

## Constraints & Risks

### Behavioral and compatibility risks

- [V] **Incomplete persistence:** normal-path retention alone can be undone by recovery cleanup, including watermark-inferred cleanup. Sources: `internal/daemon/mode.go:40`, `internal/store/modes.go:73`.
- [V] **Blocked transitions:** retained labels presently conflict with explicit commands selecting another mode. Source: `internal/daemon/tick.go:1570`.
- [V] **False planning failures:** discussion-only managed Plan success currently fails when the proposal file is absent. Source: `internal/turn/plan_handoff.go:88`.
- [V] **Readiness ambiguity:** Plan preparation clears readiness, so continuing a discussion has consequences for a previously ready proposal. Source: `internal/turn/plan_handoff.go:33`.
- [V] **Conversation truncation:** plain owner answers revoke asking permission; agent instructions can then force assumptions instead of further discussion. Sources: `internal/turn/ask.go:65`, `.claude/agents/requirements-engineer.md:119`.
- [V] **Stale labels and retries:** the daemon uses issue snapshots, one actionable comment per tick, and per-trigger durable decisions. Their interaction with label edits must remain deterministic. Sources: `internal/daemon/tick.go:1229`, `internal/daemon/tick.go:1541`.
- [V] **Frozen-scope safety:** modes and labels cannot be used to bypass story locks or substitute live story edits for approved revisions. Sources: `internal/turn/managed_scope.go:18`, `.aiteam/APP_CONSTRAINTS.md:206`.
- [V] **Prompt/instruction drift:** mode headers and router trust must agree, and Claude/Codex copies must remain aligned. The existing Validate vocabulary mismatch demonstrates an already-present inconsistency. Sources: `.aiteam/APP_CONSTRAINTS.md:230`, `internal/agent/mode.go:11`, `CLAUDE.md:17`.

### Architecture and authorization boundaries

[V] Mode resolution precedes tool-permission selection. Therefore an automatically executed agent-requested transition affects capabilities granted to a subsequent run, not merely a display label. Owner/self filtering and canonical mode validation are already present. Sources: `internal/daemon/tick.go:1629`, `internal/turn/classify.go:309`, `internal/agent/mode.go:50`.

[V] Existing package seams are suitable for the affected concerns: store owns persistence and SQL, daemon owns routing and label orchestration, turn owns execution and publication, and shared prompt construction supports both runners. Repository research found no need for an external service or new dependency. Sources: `internal/store/modes.go:18`, `internal/daemon/daemon.go:87`, `internal/harness/prompt.go:538`, `internal/turn/plan_handoff.go:74`.

[O] Only the factory may mutate forge state; coding agents receive no forge credentials. Any future agent-requested label transition must respect that division. This run performs no forge mutation.

### Verification evidence and gaps

[V] Existing tests cover mode parsing, cleanup and retry storage, mode-specific tools, frozen-scope precedence, resume and fallback behavior, question prompting, approved attempts, and managed lifecycle. Sources: `internal/daemon/mode_internal_test.go:9`, `internal/store/modes_test.go:5`, `internal/daemon/tools_test.go:18`, `internal/daemon/managed_scope_test.go:11`, `internal/turn/resume_test.go:40`, `internal/harness/prompt_ask_test.go:110`, `internal/turn/approved_attempts_test.go:31`, `internal/turn/managed_lifecycle_test.go:47`.

[V] The tour did not identify tests asserting a retained mode across consecutive owner replies, upgraded pending-cleanup behavior, an explicit transition against a retained label, or a successful discussion-only managed Plan run. These are verification gaps exposed by the requested behavior, not claims that existing behavior is untested.

[W] Plan should include acceptance examples for consecutive comments, failed and interrupted runs, restart with old cleanup rows, conflicting/malformed/manual label edits, owner mode switches, permission changes, and Plan discussion followed by actual publication. Cold, resumed, and fallback attempts must communicate the same effective mode and preserve frozen bundles. Cross-day discussion continuity also needs an explicit check where no completed artifact exists.

[V] The project uses colocated tests with migrated temporary SQLite, temporary git repositories, `httptest`, and deliberate fake execution boundaries; `make check` runs formatting, vet, pinned lint, and race tests. Sources: `.aiteam/APP_CONSTRAINTS.md:145`, `Makefile:22`.

No tests were run: this run changed research artifacts only and makes no claim that production behavior has been changed or validated.

## Open Questions

These decisions belong to the owner and Plan mode. They are recorded for a future planning run; they are not questions awaiting a response in this headless run.

1. [W] **Who changes mode?** Does “or update the label” permit a validated agent request that the factory executes, or only an owner label edit? Working assumption: the agent recommends; the owner controls transitions.
2. [W] **How does an explicit transition work?** Should an owner slash command replace an existing mode label? How should ordinary “move to the next phase” replies behave? Working assumption: an explicit owner transition needs to be supported; retaining today's conflict rule unchanged does not meet the intended experience.
3. [W] **What clears persistence?** Does removing all mode labels re-enable classification, or does stored task state restore the last mode? How are multiple or malformed mode labels corrected? No additional persistent preference store is assumed yet.
4. [W] **When may planning ask questions?** Does selecting Plan authorize iterative questions for the conversation, or is `lf: ask` still required on each owner reply? Research assumes the current run's no-question instruction remains binding; future product behavior needs a deliberate policy.
5. [W] **What ends planning?** How are discussion-only success, readiness for review, approval/promotion, and execution failure distinguished? What happens to a previously ready proposal when another planning discussion begins?
6. [W] **How does upgrade recovery behave?** Existing mode-selection rows can request or imply deletion. Should old pending operations be retired, honored, or converted? Recovery semantics must be explicit before rollout.
7. [W] **What does a retained label show under managed overrides?** Frozen Implement and explicit replan rules remain authoritative. Decide whether labels are synchronized to that effective mode or can display a different selected preference.
8. [W] **How is unfinished discussion resumed across dates or fallback?** Existing artifact lookup supports completed markers, but the required continuity for question-only iterations has not been established. Define the authoritative discussion/progress context rather than presuming an artifact exists.

## Recommended scope for Plan mode

[W] Plan a coherent mode lifecycle spanning retained selection, explicit owner transitions, retry/recovery behavior, and repeated conversation runs. Include an explicit successful-but-unfinished planning outcome and its effect on managed publication/readiness. Align shared cold/resumed/fallback prompts and both agent instruction copies so a run ending does not imply its mode or planning conversation is finished.

[W] Preserve one mode per run, mode-derived permissions, asynchronous final-report conversations, canonical label validation, and frozen managed-story rules. Resolve agent-applied transitions as a separate explicit policy decision; do not silently introduce automatic phase progression or direct agent forge access.

[W] Keep implementation choices, schema changes, story decomposition, and acceptance criteria for Plan mode. Do not broaden this work into unrelated daemon scheduling, dependency changes, generic classifier redesign, or a new question-round counter. No production code was changed in Research.
