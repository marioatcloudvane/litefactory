# Architect Review — headless-mode-should-ask-via-a-comment
# 2026-08-27. Review of `IMPLEMENTATION_PLAN.md` (run 0006) by `go-architect`.

> Returned after the plan was written and after run 0006 reported. Kept as a separate document
> so the plan's own history stays readable. **Findings 4, 9, 19 and 21 were independently
> verified against source by the dispatcher before this file was written** — see the
> verification notes inline. The rest are the architect's, unverified.

## The three findings that change the plan most

### F-9 — T-A1.3 as written makes tagged runs commit and push *(VERIFIED)*

The gag at `prompt.go:396-401` is **one paragraph carrying two unrelated instructions**:

> …do not stop for approval. Where something is ambiguous, take the most reasonable reading,
> act on it, and say what you assumed in your final message. **Do the work in this worktree; do
> not commit or push — litefactory does that once you are finished.**

Branching the whole paragraph on `MayAsk` drops the commit/push prohibition on every tagged run,
colliding with litefactory's own commit step. **Add a task before T-A1.3: split the paragraph so
the worktree/commit sentence is unconditional.** Sharpest concrete bug in the story.

### F-21 — GitHub "Quote reply" silently kills the answer *(VERIFIED)*

`HasMarker` is `strings.Contains(body, "<!-- lf:")` — **anywhere** in the body
(`turn/classify.go:38`) — and `Classify` tests `selfReason` **before** the owner check
(`classify.go:207-214`), deliberately (`classify.go:193-196`).

GitHub's quote-reply copies the raw markdown of the quoted comment, HTML comments included. So:
owner reads litefactory's question comment, clicks quote-reply, types the answer, posts →
`HasMarker` fires → `ClassificationSelf` → dropped. **No run, no notification, and the parked run
never resumes.** This is the single most likely real-world path through A1's flow and it fails
closed and silently. B2 makes it worse, since the quotable artifact becomes a whole brief.

Needs a task and an AC in A1: either strip/neutralise `<!-- lf:` from *inbound* bodies before
classification, or (cheaper) have the question comment tell the owner to reply plainly rather than
quote — and test the drop path so it is at least logged with `SelfReasonMarker`.

### F-19 — the same matcher is a label-upgrade hole *(VERIFIED)*

Any third party can promote their comment from `old_third_party` ("someone who cannot command
litefactory") to `litefactory` ("your own earlier replies") by writing the eight characters
`<!-- lf:` anywhere in it. Pre-existing, but B2 makes it more attractive by training the thread to
contain long marker-bearing litefactory documents worth impersonating.

Fix is a design decision, not a one-liner: anchor the marker check (prefix-of-body, or a marker on
its own line), **or** classify by author first and use the marker only as a tiebreaker. Note that
`classify.go:193-196` defends the current ordering for a real reason — the bot account being the
owner's account by misconfiguration.

## Correction to the plan's own reasoning

### F-4 — B2's central argument for route (ii) is wrong *(VERIFIED)*

The plan says feeding a brief through `RenderRun` "would elide it as though it were a log tail".
That inverts the order. `report/run.go:165-175` states the section order **is** the survival order,
and `runBody` cuts: **log tail (step 1) → result (step 2) → files (step 3)**. `secResult` is the
*second-most-protected* section, behind only the never-cut `secFixed`. Delete the bullet — it is
the main cost argument for route (ii) and it does not hold.

### F-3 — route (i) truncates the brief *twice*, not once

`run.go:528` cuts at `maxResultBytes` (32 KiB); then `runBody` step 2 cuts again to `roomFor(...)`
if the assembled body is still over budget, and below `len(resultHeading)+minTailBytes` it is
replaced wholesale by `droppedResultNotice`. So route (i)'s brief competes with the files list and
log tail. **Q4's 8 KiB target makes this unreachable — which is a stronger argument for coupling
B1 to B2 than the one in the plan.** Route (i) also silently answers OD-10 (brief replaces vs.
accompanies the status comment); say so or the owner will notice the missing question.

### F-3b — two comments per research run costs more than thread noise

Under route (ii) both comments carry `<!-- lf:`, so both classify `self` and both re-enter
`History` as `RoleLitefactory`. At `MaxCommentBytes` 8 KiB against `MaxHistoryBytes` 32 KiB,
**two litefactory comments per research run consume half the history block**, evicting the owner's
own comments oldest-first on the next cold rerun. Route (i) consumes one slot.

## Permission across resume — the plan's `PreviousAsked` is unimplementable

**Wrong bit.** `PreviousAsked` answers "did the last run ask?", not "may this run ask?". Only the
second governs permission. Because `writeResumeHeader` emits no permission statement, silence on
the resume path means *inherit from transcript* — so the dangerous cell is *previous run tagged →
this resume untagged*, where the transcript still says "you may ask" and nothing contradicts it.
`PreviousAsked` never touches that cell.

**Not derivable in A1.** Under A1's "no new state", Go's only signals are `store.Run.Status` (a run
that asked and one that didn't are byte-identical) and the previous run's final-message text —
and text-matching is what the codebase refuses to do elsewhere. `PreviousAsked` is an **A2 field
wearing an A1 label**.

Minimal correct field set on `PromptInput`:

| Field | Source | Derivable in A1? |
|---|---|---|
| `MayAsk bool` | tag on **this** run's trigger comment | yes |
| `PreviousMayAsk bool` | re-parse the tag from the **previous** run's trigger comment | **yes, zero schema cost** |
| ~~`PreviousAsked`~~ | requires `needs_input` | no — defer to A2 |

`PreviousMayAsk` is free: `prepare.go` already loads the full comment list for `History` on both
paths, and `store.Run.TriggerCommentID` identifies the previous run's trigger inside it. Same shape
as `PreviousMode`.

Rule for `writeResumeHeader`: emit a permission paragraph **when `MayAsk != PreviousMayAsk`**, in
both polarities. Structurally identical to the `writeInferGuidance` precedent (`mode != PreviousMode`),
but note *why* it differs: for infer-guidance, non-emission is merely redundant; here non-emission
**fails open**. Say so in the Architecture Notes or someone will "simplify" it back to
one-directional.

## Mode-name leakage — the purity argument is already dead

`harness/prompt.go` defines `const InferMode = "infer"` and `writeHeader` branches prompt text on
it. Go already holds a mode name and already emits mode-conditional prompt text, so matching one
more opaque literal concedes nothing new. OD-3's framing overstates the cost.

The real argument for mode-neutral spelling is **Q5**: if Q5 answers "all modes", a mode-named tag
forces Go to hold a table (`research-mode:`, `plan-mode:`, …) kept in sync with a vocabulary that
lives in the target repo's `CLAUDE.md`, not in litefactory. `lf: ask` is one literal forever.

**Third option worth putting to the owner:** gate on the *presence of any* `lf:` directive line and
let the agent read the rest. Go emits permission on "a directive was present"; the word after the
colon is never parsed by Go. Strictly one bit, survives any future vocabulary.

**The headline process defect:** the plan opens with *"nothing genuinely his to decide has been
decided here"* and then silently decides OD-3 (Go enforces) and OD-4 (permission carries via
`PreviousAsked`). Both land on Story A1 — the story meant to ship first.

## Story A1 task-table defects

- **F-10.** AC-A1.2 as written is satisfied by deleting the `:388-390` paragraph outright — but its
  substance (stating the feature slug) is the entire D-3 mechanism; without it "a run stops on a
  question that will never be read." Only the justification clause may change. **Add AC-A1.2b: on
  the tagged path the prompt still states the feature slug.**
- **F-6.** "Two golden tests" is imprecise: there is no `testdata/` or `.golden` file in
  `internal/harness`. There is **one** full-head literal comparison, `TestSpikePromptIsUnchanged`
  (`prompt_test.go:359`), and one substring test,
  `TestBuildPromptStatesTheFeatureSlugAndForbidsQuestions` (`:76`). Name them.
- **F-11.** That substring test asserts both facts in one test and A1 splits them. Add a task to
  split it into a slug test (both paths) and a gag test (untagged only). T-A1.7's "assert the two
  existing goldens are untouched" is impossible for this one.
- **F-7.** `internal/harness` has **zero** tests setting `Resume: true` or `PreviousMode`.
  `writeResumeHeader` is entirely uncovered at unit level, and AC-A1.5 ("today's resume header,
  unchanged") has nothing protecting it. **Add a characterization test before T-A1.6.**
- **F-12.** T-A1.4 → T-A1.3 dependency is inverted: you cannot branch to a paragraph before it is
  written. Swap.
- **F-13.** T-A1.5's dependency on T-A1.2 is spurious — different field, different source. Rewritten
  as `PreviousMayAsk` it depends on **T-A1.1**.
- **F-14.** Field names disagree: plan says `AskingAllowed`/`PreviousAsked`, `feature-spec.md` says
  `MayAsk`/`AnswersQuestion`. Pick one (recommend `MayAsk`/`PreviousMayAsk`) and state it supersedes
  the spec.
- **F-15.** T-A1.1 only considers the free-text path. `ParseCommand` (`command.go:152`) also yields
  `KindSpike` with `Text = TrimSpace(rest)`, so a tag in `/spike research-mode: ask do X` lands
  mid-`Text` and must be stripped there too.
- **F-16.** Missing behaviour: tag-only comment. `firstToken` → `KindFreeText` → strip tag →
  `Instruction` empty → `BuildPrompt` returns `ErrInvalidPrompt` and the run dies with a validation
  error the owner will not understand. Recommend treating it as `KindNone` (no run), and give it
  an AC.
- **F-17.** AC-A1.6 is a claim, not a criterion. Replace with an executable `turn`-level test: a
  comment arriving after a `RunSucceeded` run in the same segment produces `Resume: true` /
  `RunPath = resume`.
- **F-22.** Tag text is owner-only (it is read only from a comment that already classified
  `instruction`), so injection risk is low. Two guardrails anyway: match the tag **line-anchored**,
  not `Contains`, or an issue body quoting this plan turns permission on; and the tag must never
  reach `Instruction` un-stripped, since `Instruction` is documented as trusted and is the one
  field with no size cap.

## Platform lens

Most of `platform-constraints.md` genuinely does not apply — no new package, no HTTP layer, no DB
access, no migration, no new dependency, no concurrency (A1/B1/B2 are synchronous string assembly;
A2's sidecar read happens after subprocess exit). State that in one line.

What does apply:

- **Error handling, T-A1.1.** `ParseCommand`'s contract is *"never fails. Every body classifies as
  something."* A tag parser returning an `error` breaks that invariant. Say "never returns an
  error; a malformed tag is simply absent."
- **A2's platform bullet is wrong.** A2 adds no new query — it adds values to existing typed string
  constants. Replace with the real trap: `terminalRunStatus` is an explicit switch with
  `default: return false`, so a status added to the store but not to that switch **silently becomes
  non-terminal** — no compile error, no test failure, and the symptom is a cold start with the
  question's context gone.
- **Testing.** AC-A1.7 has `-race` but not `-cover`; the skill names `go test -race -cover ./...`.
- **B1's bullet is over-confident.** AC-B1.2 is not checkable by any test; say "no automated
  verification; verified by observation of the next research run."

## Sequencing

Recommended: **Story 0 (self-filter / quote-reply, F-19+F-21) → B1 + B2(route i) as one unit →
A1 → A2.**

- **Story 0 first**, because both A1 and B2 depend on the owner being able to reply to a
  marker-bearing comment. Ship either before it and the first real use fails silently.
- **B1 and B2 route (i) are one story.** B2 route (i) is a single instruction in
  `research-orchestrator.md`, and its only correctness requirement is B1's byte budget.
- **A1 after**, because it carries two unanswered owner decisions plus F-9/F-10/F-16, and it is the
  only story that can change untagged runs. Its "small, self-contained" sizing is real for the
  *diff* and optimistic for the *risk*.
- **A2 last**, unchanged.

**Hidden coupling:** the tag parser (T-A1.1) is the same code that derives `PreviousMayAsk` and,
if Q5 answers "all modes", the same code that gates Plan and Implement. It is the one genuinely
load-bearing new function in the plan, currently sized **M** alongside a doc-comment task at **S**.
Give it its own AC.

**Unverifiable ACs that cannot gate Implement mode** — mark explicitly as manual/observational so
`implement-orchestrator` does not stall: AC-B1.2 (future brief within target), AC-A1.6 (a claim),
AC-B2.1 ("readable without opening the repo" — no failing condition).

**T-B1.4 has no AC** and is arguably a different story (`codebase-tour.md`'s output format, not the
brief template). Add an AC or move it out.

**T-B2.5 / AC-B2.2 may already be satisfied under route (i)** — `runBody` trims to budget,
`run.go:528` truncates with an explicit marker, and `redact.Redactor.Apply`'s hard-truncate is the
backstop. The spec's silent-failure path may be unreachable for `RenderRun`. Verify before
building; if so T-B2.5 collapses to S and AC-B2.2 becomes a regression test.

## Sidecar hygiene (A2 / T-B2.4)

`redact.Body` having no exported constructor does make "post an unredacted body" inexpressible —
keep that. But redaction covers credentials only. A sidecar body is agent-authored text going
straight into a comment, so it can contain `<!-- lf:` (self-filtered — harmless, litefactory posted
it) or markdown that breaks the report's `<details>` fence or callout structure. `report/run.go`
already has `sanitize` and `fenceFor`; require the brief/question renderer to route through the
same. Keep T-B2.4's "fixed path, not an env var" — widening `env.go`'s eight-name allowlist would be
a security change for zero benefit.

## Not covered

Nothing here needs guidance outside Go backend architecture. The two items normally flagged as
out-of-domain — the agent-definition markdown in B1, and the owner-facing question wording in
T-A1.4 — have no rostered specialist. Engineer should use judgment and note assumptions in task
notes.
