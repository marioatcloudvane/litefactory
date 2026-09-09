# Feature Spec: modes-should-stay
# Status: APPROVED
# Date: 2026-09-09

## Goal
- Keep an issue's selected mode across owner replies, failures and restarts, while allowing explicit owner transitions and successful multi-run planning conversations.

## User Stories
| ID | Story |
|----|-------|
| US-1 | As an owner, I want a selected mode to remain on the issue after each run so that ordinary follow-up comments continue the same work stage. |
| US-2 | As an owner, I want to switch the selected mode with an existing mode command or label edit so that persistence does not trap the issue in a stage. |
| US-3 | As an owner, I want Plan to ask material questions across separate issue replies and distinguish discussion from a completed proposal so that early planning iterations succeed without prematurely publishing stories. |
| US-4 | As an owner, I want both coding agents to understand continuing mode and planning context on every attempt so that repeated, resumed and fallback runs preserve the same workflow. |

## Data Model
| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| Issue | Selected mode label | Existing canonical mode label | No | At most one valid label; Research, Plan, Implement, Validate or Spike; absence permits normal classification on a new trigger. |
| Trigger mode decision | Session and trigger identity, selected mode, label application and retirement progress | Existing durable retry journal, adapted as needed | Yes after selection | Retry identity remains per trigger; finishing a trigger retires its journal without deleting the selected issue label. |
| Managed planning iteration | Outcome | Discussion or completed proposal | Yes for successful managed Plan | Explicit machine-readable distinction; absence of a proposal alone is not evidence of discussion. |
| Planning session | Working context | Persisted repository artifact and existing issue history | Yes for continuing discussion | Carries scope, decisions, assumptions and unresolved material questions even when no completed plan exists. |
| Managed feature | Generation, readiness and frozen revisions | Existing managed scope state | Yes where already managed | A discussion cannot publish or approve a generation; existing frozen revisions and completed-story locks remain authoritative. |

### Relationships
- Issue selected mode governs new ordinary triggers; a retry retains its already admitted trigger decision.
- A planning iteration belongs to the existing feature session and managed generation.
- Issue mode selection does not confer permission to reopen an approved generation or completed story.

## API / Interface Changes
| Method | Endpoint / Action | Auth | Notes |
|--------|------------------|------|-------|
| Owner comment | Existing mode slash command with instruction | Existing owner admission | Replaces one valid selected mode for the newly admitted trigger; supported mode vocabulary stays unchanged. |
| Issue label edit | Replace or remove canonical mode label | Existing authorized forge user | Replacement selects a mode for the next trigger; removing all mode labels returns the next new trigger to normal command/classifier routing. |
| Agent final report | Recommend a next mode | Agent output only | Recommendation names an executable owner action, such as `/implement <instruction>`; does not itself switch mode or grant tools. |
| Managed Plan result | Explicit discussion or completed-proposal handoff | Factory validates agent transport | Discussion succeeds without a complete proposal; completed proposal continues using strict existing version-1 proposal validation and publication. |
| Owner reply | Answer planning questions in another comment | Existing owner admission | Normal Plan replies permit further material questions without repeating `lf: ask`; the subprocess ends with each report. |

## Business Rules
- Selected canonical labels survive successful, failed and interrupted runs and report delivery; selection by command or classification also persists.
- Recovery retires old pending or inferred mode-cleanup work without deleting the mode label; it does not restore labels that the owner removed.
- Existing managed-scope overrides retain precedence: frozen implementation stays Implement unless an eligible explicit parent replan opens unfinished scope; child refinement remains Plan; changing labels alone cannot unlock scope.
- The factory synchronizes a managed override's effective mode to the canonical issue mode label when admitting that run; the report and prompt show the same effective mode.
- Explicit owner commands replace a single valid retained label; malformed or multiple mode labels remain actionable validation failures requiring correction before execution.
- Once admitted, a trigger keeps its durable mode and corresponding tool permissions on retry; label edits apply to subsequent triggers and do not alter a running subprocess.
- Mode-label transition failure prevents execution with an unconfirmed label state and is retryable; recovery completes only the recorded transition, without adding unrelated labels or consuming a new trigger prematurely.
- A Plan run may complete with a discussion outcome and material questions, retaining session context and Plan mode; discussion neither publishes story changes nor marks the generation ready.
- Starting another authorized Plan iteration invalidates prior planning readiness until a fresh completed proposal is validated, published and saved; a failed or discussion iteration cannot accidentally re-enable stale approval.
- A completed Plan proposal leaves Plan selected and recommends an explicit owner transition; a normal reply continues Plan and does not execute implementation automatically.
- Selecting Plan permits asynchronous material questions by default, subject to explicit per-run no-question instructions; Research retains its existing `lf: ask` policy and other modes do not gain question permission.
- Every run stays in one effective mode and returns one final report; asking a question never means waiting inside the process.
- Discussion success requires a valid explicit outcome; missing, malformed, contradictory or stale completion evidence fails clearly rather than silently becoming discussion success.
- Factory-owned planning transport remains transient; coding agents cannot access forge credentials or publish story issues directly.

## Edge Cases & Error Handling
- Report delivery fails or process crashes after label selection → preserve selection and retry existing report/trigger recovery without deleting the label.
- Upgrade encounters legacy cleanup-pending rows or watermark-inferred cleanup → retire cleanup bookkeeping without removing issue labels or resurrecting owner-cleared labels.
- Owner changes the label during an admitted run → current attempt stays pinned; later new triggers re-read current issue labels under existing admission serialization.
- Transition partly updates labels before a forge failure or restart → durable recovery produces the intended canonical selection before running; no ambiguous multi-label selection is executed.
- A Plan iteration emits neither valid discussion evidence nor a completed proposal → report a handoff error.
- Discussion evidence appears with completed-proposal transport, mismatched generation or previous-run evidence → reject the contradictory/stale handoff and keep readiness false.
- Planning resumes on another day, through a cold process, or after native-resume fallback → recover the same feature's unfinished planning context without requiring a completed artifact marker.
- An owner attempts Implement while discussion is incomplete → existing managed readiness/freeze requirements prevent approval and execution of unready scope.
- Several ordinary replies arrive → each new trigger uses retained/current selection with existing ordering and owner/self-comment filtering.

## Out of Scope
- Autonomous agent-applied mode changes, automatic phase progression or agent forge credentials.
- Natural-language mode-transition inference while a valid mode label is present; owner uses a supported explicit command or label edit.
- New mode names, scheduling behavior, live chat, question-round counters or generic classifier redesign.
- Reopening completed managed stories, substituting live story bodies for frozen revisions or changing acceptance-evidence authorization.
- Changes to Research question opt-in beyond wording needed to distinguish it from Plan.

## Assumptions and Provenance
| Source | Decision |
|--------|----------|
| Issue #170 and research brief | Modes persist across multiple iterations; early Plan may involve back-and-forth; agents can recommend a mode change. |
| Research brief open questions 1–3 | Owner controls transitions; `/mode` commands replace a single valid retained label; manually removing all mode labels returns routing to classification. |
| Research brief open question 4 | Plan selection is sufficient permission for material asynchronous questions on subsequent replies; explicit no-question instructions still win. |
| Research brief open question 5 | Discussion is an explicit successful but unready outcome; a new planning iteration clears prior readiness until fresh validated publication. |
| Research brief open questions 6–7 | Legacy deletion work is retired; managed forced modes synchronize visible labels to the effective mode. |
| Research brief open question 8 | An unfinished planning context artifact is sufficient for cross-day continuity; completed artifact discovery alone is insufficient. |
| Managed handoff and headless run instructions | This run produces a complete proposal without questions; status APPROVED records the authorized planning output, not owner approval to implement. |
| Factory context supplied for run 0002 | No existing child story identities or base revisions were supplied; proposal stories are new creates, without invented existing identities. |

## Acceptance Criteria
- [ ] AC-1: Consecutive owner comments retain each supported selected mode and corresponding label after success, failure and report delivery, without reclassification while a valid label remains.
- [ ] AC-2: Restart recovery for normal, legacy pending and watermark-inferred cleanup retires trigger bookkeeping without deleting or restoring selected mode labels.
- [ ] AC-3: An explicit owner mode command replaces a single valid retained label and changes the next admitted run's mode and tools; manual replacement/removal affects subsequent triggers, while retries keep their original decision.
- [ ] AC-4: Malformed/multiple mode labels and failed/partial label transitions never execute an ambiguous mode; partial transitions recover deterministically after restart.
- [ ] AC-5: Frozen managed scope, explicit eligible parent replan, child refinement and completed-story protections keep their existing precedence, with labels, reports and prompts reflecting the admitted effective mode.
- [ ] AC-6: A valid explicit discussion-only Plan outcome succeeds, preserves working context and Plan selection, publishes no stories and leaves managed readiness false; a later complete proposal validates, publishes and becomes ready through existing gates.
- [ ] AC-7: Missing, malformed, stale or contradictory Plan outcome/proposal evidence fails clearly; starting another Plan iteration clears old readiness and neither failure nor discussion restores it.
- [ ] AC-8: Plan can ask another material question after a plain owner answer without `lf: ask`, while explicit no-question instructions and the existing Research policy remain enforced.
- [ ] AC-9: Both agents receive equivalent continuation, question-policy and owner-transition instructions on cold, resumed and fallback attempts; unfinished Plan context survives a date change without a completed artifact.
- [ ] AC-10: A completed proposal recommends an explicit owner transition, retains Plan, and cannot automatically start Implement or bypass unready/frozen-scope protections.
