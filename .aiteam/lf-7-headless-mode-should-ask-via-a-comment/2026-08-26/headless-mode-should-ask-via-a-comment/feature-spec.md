# Feature Spec: Opt-in asking via comment (A) + research brief as comment (B)
# Status: DRAFT — 11 open decisions for the owner (see last section). NOT approved.
# Date: 2026-08-27
# Issue: marioatcloudvane/litefactory#7
# Source: RESEARCH_BRIEF.md (rev 2) + owner comment 2026-08-27, which overrides it.

## Goal
Let a headless run stop and ask the owner on the issue **when, and only when, the owner has opted that run in**, and put the research brief in the issue thread as a short, readable comment — so the owner reviews work where he already reads.

## Owner decisions already settled (do not re-open)
| # | Decision | Consequence |
|---|---|---|
| D-1 | Ask (C) — plan as GitHub sub-issues — **out of scope**. Owner raises his own story. | Brief's OQ-8, OQ-17..OQ-24, R-14/15/20/21/23, F-8 are all parked. No sub-issue or issue-creation client methods. |
| D-2 | Ask (B) approved in principle: brief lands inline as a comment. | |
| D-3 | Briefs must be **much shorter**. First-class requirement, readability not size. | "It fits in 63,488 bytes" (brief F-6) is **not** an answer. |
| D-4 | Ask (A) default is **inverted**: never ask unless tagged `research-mode: ask`. | Asking is opt-in and rare. |
| D-5 | Code review untouched (`go-code-reviewer`, `CODE_REVIEW.md`, Implement review gates). | Propose nothing there. |

### What D-4 defuses (state explicitly; do not carry forward unchanged)
- **R-3 (removing the gag fires eleven latent approval gates)** — largely defused. The gag is *not* removed; it stays as the default and is replaced only on tagged runs. Untagged runs are byte-identical to today.
- **OQ-2 (the bar for asking)** — much less urgent. The tag *is* the bar. Prompt text on a tagged run can be permissive ("ask when the answer changes what gets built") without risk to untagged traffic.
- **OQ-1 (timeout) / OQ-3 (ask cap)** — less urgent. A parked run only exists because the owner asked for it. Still needs a decided answer (OD-4, OD-11) but is no longer a product risk.
- **Brief's F-2 seam still bites**: the gag lives in `writeHeader` only and is never re-emitted on resume. Any cap or permission rule must be enforced in **Go or the resume header**, never in cold-path prompt text.

## User Stories
| ID | Story |
|----|-------|
| US-A1 | As the owner, I tag a comment `research-mode: ask` so that run is permitted to ask me questions. |
| US-A2 | As a permitted run, when I hit an ambiguity whose answer changes what gets built, I post one question comment and end cleanly instead of assuming. |
| US-A3 | As the owner, I reply in the thread and the parked run resumes the same conversation in the same mode, treating my reply as an answer and not as a new instruction. |
| US-A4 | As the owner, an **untagged** run never asks — identical behaviour to today. |
| US-B1 | As the owner, when a research run finishes, the brief appears as a comment on the issue. |
| US-B2 | As the owner, that comment is short enough to read in the thread without scrolling past a wall of text. |
| US-B3 | As the owner, an oversize brief still produces a visible comment, never silence. |

## Data Model

### New / changed state
| Entity | Field | Type | Required | Constraints |
|---|---|---|---|---|
| `runs` | `status` | TEXT | yes | add `needs_input`. **MUST be terminal** (`terminalRunStatus`, `prepare.go:335-344`) or `previousRun()` will not resume the asking conversation. No migration — `0001_init.sql:10-14` deliberately has no CHECK. |
| `sessions` | `state` | TEXT | yes | add `needs_input` (LF-11's long-anticipated `blocked`, under `initialplan.md`'s name). No migration. Update `SessionState.valid()`. |
| Signal doc (sidecar) | `status` | enum | yes | `needs_input \| working \| ready_for_review \| blocked` per `initialplan.md:245-255`. |
| Signal doc | `summary` | string | no | free text. |
| Signal doc | `questions` | []string | no | non-empty exactly when `status = needs_input`. |
| Signal doc | `brief` | string | no | (B) only, and only under OD-9 option (a). |
| `PromptInput` | `MayAsk` | bool | yes | true when the triggering comment carried the tag (or the tag's carrier per OD-2/OD-3). Selects gag vs ask-permission paragraph in `writeHeader`. |
| `PromptInput` | `AnswersQuestion` | bool | yes | true when this resume answers a parked question. Drives the resume-header paragraph. |
| Comment marker | `lf:question` | string | yes | `<!-- lf:question task=<ref> run=<n> -->`, same shape as `StatusMarker` (`report/marker.go:29`). Self-filters free via the `<!-- lf:` prefix match. |
| Comment marker | `lf:brief` | string | yes | `<!-- lf:brief task=<ref> run=<n> -->`. Same. |

### Relationships
- `sessions` → `runs` via `session_id`; the parked run is the segment's last terminal run, which is exactly what `previousRun()` reads. No new correlation column.
- Question comment → answer comment: **positional only**. GitHub issue comments have no `in_reply_to` (F-5). While a session is `needs_input`, the next owner instruction comment on that task **is** the answer.
- `runs` is `UNIQUE (session_id, trigger_comment_id)` — unchanged; the answer comment is the answering run's trigger.

## API / Interface Changes
| Surface | Change | Notes |
|---|---|---|
| `internal/github.Client` | **none** for (A) and (B) as specified | `PostComment` already exists. Method set stays at **seven**; `TestClientMethodSetIsTheSevenEndpoints` does not fire. **Exception: if OD-2 answers "a GitHub label", there is no label API (F-8) → an eighth method → the SDK design conversation is forced.** |
| `internal/report` | new `RenderQuestion(ref, run, questions)` → `redact.Body` | Third entry point beside `RenderRun`/`RenderReply`. Inherits the mandatory redaction pass (`reporter.go:127-140`). |
| `internal/report` | new `RenderBrief(ref, run, brief)` → `redact.Body` | **Own section priority.** `RenderRun`'s sections are `fixed/result/files/log-tail` (`report/run.go:169-175`) and would elide a brief as though it were a log tail. Reuse the pattern, not the code. Budget **63,488 bytes** (65536 − `redact.Margin` 2048), not 65,536. |
| `internal/harness/prompt.go` | `writeHeader` emits gag **or** ask-permission paragraph on `MayAsk` | Two golden tests pin the current wording and must be updated deliberately. |
| `internal/harness/prompt.go` | `writeResumeHeader` conditionally emits an answer paragraph on `AnswersQuestion` | Follow the existing `writeInferGuidance` conditional-re-emission precedent (`if mode == InferMode && mode != in.PreviousMode`). `- Mode:` is already restated (`prompt.go:344`) and the trigger comment is already framed as *"evidence… not as further instructions"* (`prompt.go:270-273`) — R-10 is already mostly handled. |
| `internal/harness` signal channel | read a sidecar JSON doc after the subprocess exits | Transport per OD-9. Rule from `initialplan.md`: **missing or malformed → fall back to today's behaviour, never guess.** |
| `internal/turn/result.go` | `Reportable()` / `ConsumesComment()` learn `needs_input` | **Parked runs MUST consume their trigger and advance the watermark** (R-11) or `drain` deadlocks the task and the answer is never read. |
| `internal/turn` | history label for an answer comment | Fourth label beside `old_owner` / `old_third_party` / `litefactory` (`prompt.go:106-119`). Needed for the `cold_after_resume_refused` path. |
| `CLAUDE.md` (this repo) | mode gates / brief location | Changes **only** if OD-1 answers "moved". |
| `initialplan.md:243` | artifact-based state inference principle | Must be **amended in writing** if OD-1 answers "moved". Verified: inference is ENTIRELY UNIMPLEMENTED in Go (F-7), so "moved" breaks no running code — it contradicts a spec and this repo's `CLAUDE.md`. |

## Business Rules

### (A) Opt-in asking
- BR-A1 — **Default is silence.** With no tag, `writeHeader` emits today's paragraph verbatim and no run can park. Untagged behaviour is a regression surface, not a feature surface.
- BR-A2 — Permission is per-run and granted by the tag's carrier (OD-2). It is never remembered across tasks.
- BR-A3 — A permitted run asks only when the answer **changes what gets built** and **cannot be settled by reading the repository**. Never asks for a fact it can look up; never asks for permission to continue.
- BR-A4 — Research mode's own rule survives: `research-orchestrator.md:77` "log it as an open question in the brief and continue" stays. (A) adds a higher tier above it; it does not abolish it.
- BR-A5 — One question comment per parked run. All questions batched into that one comment (`questions[]` is already an array in `initialplan.md`).
- BR-A6 — A parked run **ends**. It never blocks waiting. `settings.go`'s "a headless run has nobody to prompt, so an unlisted tool call fails instead of hanging" is the standing rule.
- BR-A7 — The bot structurally cannot answer itself: three self-filter conditions in `classify.go`, any sufficient, including the `<!-- lf:` prefix. No ask-loop can close without a human.
- BR-A8 — Posting the question and writing the parked state obey the existing `RunReported` discipline: comment on the forge **and** watermark moved, one transaction (R-9).
- BR-A9 — A resumed answering run finishes **the same mode it started**. One Run, One Mode is not weakened by a pause.

### (B) Brief as comment
- BR-B1 — The brief comment is posted by the same single path as every other comment: `turn.Reporter.post` → `github.Client.PostComment`, carrying a `redact.Body` that has no exported constructor. Redaction is therefore not optional.
- BR-B2 — **Never multi-comment splitting.** Nothing in the tree splits one document across comments (`turn/report.go:418-420`, single non-looping `PostComment`). One document, one comment.
- BR-B3 — **Oversize must not fail closed.** Today `redact.go:245-247` returns `LimitError` and `reporter.go:85-86` leaves the run unreported and the watermark unmoved → total silence (R-22). Wrong shape for a visibility feature. See OD-8.
- BR-B4 — No blob/permalink links. LF-7 is unstarted and `report/report_test.go:240-243` actively fails a body containing `/blob/` (R-24). Do not absorb LF-7.
- BR-B5 — Briefs get a hard length budget (OD-7). "Shorter" without a number is not testable and Implement mode needs a checkable criterion.
- BR-B6 — Known, accepted degradation: a posted brief re-enters comment history and is **replayed truncated to its first 8 KiB** (`MaxCommentBytes = 8<<10`), against a 32 KiB total block (`MaxHistoryBytes`). A comment is therefore not a usable channel for handing a full brief to a later cold run. Shorter briefs (D-3) reduce this to near-zero — the two requirements agree.

## Edge Cases & Error Handling
| Case | Expected behaviour |
|---|---|
| Untagged run hits an ambiguity | Assume, disclose in the final message. Exactly today. |
| Tagged run finds nothing worth asking | Normal completion, normal status comment. No question, no park. |
| Sidecar missing or malformed | Fall back to today's disposition. Never infer a park. |
| Sidecar says `needs_input` with empty `questions[]` | Treat as malformed → fall back. A park with no question is a deadlock. |
| Third party replies while parked | Recorded, triggers nothing (unchanged classification). Session stays parked. |
| Owner reply is a fresh instruction, not an answer | See OD-5 — undecided. |
| Transcript gone → `cold_after_resume_refused` | Rerun is cold, gets the gag plus replayed history in which the answer is just another `old_owner` entry. Needs the fourth history label so the rerun can recognise an answer. Refusal detection stays purely structural (`runner.go:599-604`) — do not match on stderr text. |
| Question posted, DB write fails | A later tick posts a second question comment (R-13, by design). A duplicated *question* is more confusing than a duplicated report; marker-based recovery via `lf:question` is available if wanted. |
| Nobody ever answers | See OD-4. There is no reaper: `ReconcileOrphans` runs once at daemon start, there is no periodic sweep, and `claude.timeout_seconds` does not apply to a run that ended cleanly. |
| Brief exceeds 63,488 bytes | See OD-8. Must not be silence. |
| `litefactory status` meets a parked session | No vocabulary for it today; a parked session is indistinguishable from `idle`. Minimum: report it as `needs_input`. |

## Out of Scope
- Ask (C): plan as GitHub sub-issues. Owner will raise his own story (D-1). No client methods, no SDK conversation, no `IMPLEMENTATION_PLAN.md` parsing, no `sub_issue_id` schema work.
- Code review of any kind (D-5).
- Labels (LF-6), draft PRs (LF-13), PR review comments as input, `/include`, prompt-injection hardening.
- Unblocking `/research`, `/plan`, `/implement` as commands.
- Rewriting the eleven interactive approval gates across the agent definitions (F-12) — the tag governs them instead.
- Multi-comment splitting of any document.
- Blob-SHA permalinks (LF-7).
- Any change that teaches Go what a *mode* is beyond matching a literal tag — `mode-closure/SPIKE.md:70-73` forbids it (see OD-3).

## Acceptance Criteria

### (A)
- [ ] AC-A1 — With no tag, the cold prompt contains today's "do not ask clarifying questions" paragraph byte-for-byte; golden test unchanged.
- [ ] AC-A2 — With the tag present, the cold prompt contains the ask-permission paragraph and **not** the gag; new golden test.
- [ ] AC-A3 — A run whose sidecar reports `needs_input` finishes with run status `needs_input` and session state `needs_input`.
- [ ] AC-A4 — `needs_input` is in `terminalRunStatus`, proven by a test that `previousRun()` returns the parked run as resumable.
- [ ] AC-A5 — A parked run consumes its trigger comment and advances the watermark (regression test against R-11 deadlock).
- [ ] AC-A6 — Exactly one comment is posted for a parked run, carrying `<!-- lf:question task=… run=… -->`.
- [ ] AC-A7 — That comment is filtered out by `classify.go`'s self-filter (no self-triggered run).
- [ ] AC-A8 — The next owner comment on a `needs_input` session produces a **resume** run (`--resume`, `RunPath = resume`), not a cold one.
- [ ] AC-A9 — The resume prompt restates `- Mode: <original>` and contains the answer paragraph; it does not present the answer as a new instruction.
- [ ] AC-A10 — Malformed / missing sidecar → run completes with today's disposition; no park.
- [ ] AC-A11 — The question comment body passes the redaction residual assertion (inherited via `redact.Body`).

### (B)
- [ ] AC-B1 — A research run posts a comment carrying `<!-- lf:brief task=… run=… -->` containing the brief.
- [ ] AC-B2 — The brief comment renderer has its own section priority; a test proves the brief body is not elided as a log tail.
- [ ] AC-B3 — The brief comment body is within the length budget set by OD-7, asserted in a test.
- [ ] AC-B4 — An over-budget brief still produces a posted comment (per OD-8), and the run is reported; never silence, never an unmoved watermark.
- [ ] AC-B5 — The brief comment body contains no `/blob/` (existing test stays green).
- [ ] AC-B6 — `internal/github.Client` still has exactly seven network methods; `TestClientMethodSetIsTheSevenEndpoints` unchanged and green.

---

## Open decisions for the owner
*Headless run. Nobody was at a terminal — which is the bug this feature exists to fix. Each item below is a genuine gap, not a manufactured one. Recommendations are **recommendations, not decisions**; nothing here has been assumed into the spec above.*

**OD-1 — Is the brief MIRRORED (file stays + comment added) or MOVED (comment only, no file)?**
*Why it matters:* mirrored keeps this repo's `CLAUDE.md` Plan gate ("Plan requires `$session/RESEARCH_BRIEF.md`") working untouched, and keeps a full-length authoritative copy behind a short comment. Moved means that gate can **never pass** and `CLAUDE.md` plus `initialplan.md:243` must both change in this feature.
*Options:* (a) mirrored; (b) moved; (c) moved, with a short "brief lives in the thread" pointer file kept purely to satisfy the gate.
*Facts:* artifact-based state inference is **entirely unimplemented in Go** (F-7) — "moved" breaks no running code, only written specs. This is also the last moment the unbuilt inference layer can be redirected without a rewrite.
*Recommendation (not a decision):* (a) mirrored, and let D-3's shortening do the real work — the comment becomes the readable artifact, the file the complete one. But (b) is defensible and cheaper long-term, and it is your call, not mine.

**OD-2 — Where does the `research-mode: ask` tag live?**
*Why it matters:* different implementation, different failure mode, and one option costs an eighth GitHub client method.
*Options:* (a) in the triggering comment body; (b) a GitHub label on the issue; (c) the issue body; (d) a setting in `CLAUDE.md`.
*Facts:* `ParseCommand` (`internal/turn/command.go:152`) inspects only the **first token**; a `KindFreeText` comment carries the **whole body** as `Command.Text`, so a tag anywhere in the comment is already available to Go and to the agent at zero cost. There is **no label API in `internal/github`** (F-8) — (b) is new client surface and trips `TestClientMethodSetIsTheSevenEndpoints`. (c) means permission is sticky for the whole issue, never per-run. (d) makes it a repo-wide default, not an opt-in.
*Recommendation (not a decision):* (a). Per-run, zero new API, matches "unless there is a tag saying research-mode: ask" most literally.

**OD-3 — Who enforces the tag: Go, or the agent?**
*Why it matters:* the gag paragraph is emitted by Go. If Go does not read the tag, a tagged run gets a prompt that says "never ask" and a comment that says "you may ask" — a direct contradiction with no precedence rule. If Go **does** read it, Go learns the literal string `research-mode: ask`, which contains a mode name — and `mode-closure/SPIKE.md:70-73` says litefactory must not learn what a mode is.
*Options:* (a) Go matches the literal tag and flips the prompt paragraph — Go learns one opaque string, not a mode system; (b) agent-only, gag softened to "ask only if this comment tells you to"; (c) mode-neutral tag (e.g. `lf: ask`) so Go never holds a mode name.
*Recommendation (not a decision):* (a) with (c)'s spelling if you accept renaming the tag — Go matching one opaque literal is a much smaller concession than making the gag conditional on the model's own reading of the body. Flag: (c) contradicts your exact wording, so it needs your say-so.

**OD-4 — Does asking permission survive the resume?**
*Why it matters:* the tag is on the *original* comment. The *answer* comment almost certainly will not carry it. So can the resumed run ask a follow-up? Nothing decides this today, and `writeResumeHeader` never re-emits the gag (F-2) — so **by default the resumed run is un-gagged and can ask again forever.**
*Options:* (a) permission does not carry over — the resume header explicitly re-gags, one question per tag; (b) permission carries over for the whole parked/resumed chain, with a hard cap in Go (2 rounds, precedent: `daemon/backoff.go`, `implement-orchestrator.md:99`); (c) permission carries over only if the answer comment re-tags.
*Recommendation (not a decision):* (b) with a cap of 2. (a) is safest but makes a genuine clarifying exchange impossible, which is most of the value. Either way this **must be enforced in Go or the resume header**, never in cold-path prompt text.

**OD-5 — Does the tag apply to Research only, or to all modes?**
*Why it matters:* you wrote "**research** never asks unless…". That may be scoped, or shorthand for everything. It decides whether the tag is a Research-mode concept or global, and whether pausing mid-Implement with a half-written file is permitted at all.
*Options:* (a) Research only — other modes keep today's unconditional gag; (b) all modes, one tag; (c) all modes, but the tag names the mode (`plan-mode: ask`, etc.).
*Recommendation (not a decision):* (b), spelled mode-neutrally per OD-3. Under (a) the four Plan-mode approval gates in `plan-orchestrator.md` and `requirements-engineer.md` stay permanently fictional — including the one that produced *this* document.

**OD-6 — "-> separate task for that!" — separate story in this plan, or a separate issue entirely?**
*Why it matters:* it decides whether ask (A) is planned here at all. Read one way you asked for (A) to be split into its own story inside this plan; read the other way, this plan is (B) only and (A) leaves the room.
*Options:* (a) a separate user story inside this plan, shipped independently (what this spec assumes structurally: (A) and (B) are kept fully separate and can ship in either order); (b) a separate issue/run outside this plan, and this spec should be cut down to (B) alone.
*Recommendation (not a decision):* (a). The sentence sits directly after "research never asks unless there is a tag", which reads as "the tag mechanism is its own piece of work", not "delete (A)". If you meant (b), say so and everything above marked (A) is deleted, not deferred.

**OD-7 — How brief is "brief"? Concrete proposal to react to.**
*Why it matters:* untestable otherwise. This brief is 1,034 lines / ~44 KB and you called it too chatty. AC-B3 needs a number.
*Proposal — the comment brief:* **hard cap 8,192 bytes (8 KiB), target ~150 lines.** 8 KiB is not arbitrary: it is exactly `MaxCommentBytes`, so a brief at that size is replayed into a later cold run **whole** rather than truncated (BR-B6). Sections that survive: Request; Findings (max 8, one paragraph each, no sub-bullets); Risks (max 6, one line each); Assumptions; Open Questions; Recommended scope. Sections cut: provenance/tag tables, revision notices, verbatim code quotes, per-finding citations reduced to `path:line` only, "Domain / Problem Space" prose, and anything restating a previous run.
*Alternatives:* (a) 4 KiB / ~80 lines — a genuine executive summary, likely loses detail an architect needs; (b) 16 KiB / ~300 lines — comfortable but still a scroll; (c) no cap on the file, cap only the comment (only coherent under OD-1 = mirrored).
*Recommendation (not a decision):* 8 KiB cap + (c) if OD-1 is mirrored. Note these two decisions are coupled: under "moved" there is only one artifact and the cap is absolute.

**OD-8 — What happens when a brief will not fit?**
*Why it matters:* today's analogue produces **no comment at all** and an unreported run with an unmoved watermark (R-22) — silence, from a feature whose entire purpose is visibility.
*Options:* (a) elide the tail, post, and append "brief truncated — full brief at `<path>`" (needs OD-1 = mirrored); (b) post `summary` + open questions only, drop findings; (c) hard-fail the run with an explicit "brief too long, shorten it" reply comment — turns D-3 into an enforced rule rather than a request.
*Recommendation (not a decision):* (c) as the primary with (a) as the safety net: a brief over budget is a **defect in the brief**, and failing loudly is what makes D-3 stick. Silence is not an option under any reading.

**OD-9 — How does the payload get from the agent to Go, and what carries it?**
*Why it matters:* two coupled sub-questions, one channel.
*(i) Transport:* `$LF_TURN_OUT` as an env var **widens the closed eight-name env allowlist** (`internal/harness/env.go`, SD-4) — a documented security boundary — versus a **fixed path named in the prompt**, which needs no env change at all. This is a security decision and it is yours.
*(ii) What (B) sends:* (a) the agent writes the brief text into the sidecar and Go posts it — Go never reads an `.aiteam` artifact, consistent with F-7; or (b) Go reads `$session/RESEARCH_BRIEF.md` off disk — which means **Go learns what a research brief is and where sessions live**, the same R-17 concession as OD-3, and requires Go to derive the session path (branch/date/slug).
*Recommendation (not a decision):* fixed path for (i); (a) for (ii). (b) puts aiteam's directory convention into Go, which is the thing `mode-closure/SPIKE.md` most wants to avoid.

**OD-10 — Does the brief comment replace or accompany the run's status comment?**
*Why it matters:* every run already posts an `lf:status` report. A research run would post two comments, and you already object to comment noise.
*Options:* (a) two comments (brief, then status) — simplest, no change to the reporter's single-post invariant, but doubles thread volume on research runs; (b) one comment: brief rendered as a section inside the status report — keeps one-run-one-comment, but the brief then competes with the status sections for the 63,488-byte budget and inherits `RenderRun`'s elision order; (c) brief replaces the status comment on research runs only.
*Recommendation (not a decision):* (a). `turn/report.go:418-420` records "post is the single call to the forge in this package" — that is a structural invariant worth two comments rather than one merged renderer.

**OD-11 — What happens if nobody ever answers a parked run?**
*Why it matters:* there is no reaper. `ReconcileOrphans` runs once at daemon start and only repairs crash wreckage; there is no periodic sweep; `claude.timeout_seconds` does not apply to a run that ended cleanly. A parked session sits for ever and `litefactory status` has no word for it.
*Options:* (a) park indefinitely — defensible now that asking is opt-in, since you chose to be asked; (b) time out after N hours and fall back to assume-and-disclose (today's behaviour plus a delay — strictly the status quo as a failure mode) — needs a periodic sweep that does not exist; (c) park indefinitely, but make it visible in `litefactory status`.
*Recommendation (not a decision):* (c). D-4 makes (a)'s risk small, and building a reaper for a rare opt-in path is disproportionate — but a parked session that looks like `idle` is a trap regardless of how rare it is.
