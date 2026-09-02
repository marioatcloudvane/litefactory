# Research brief — litefactory-owned mode routing

2026-09-02. Issue #32 asks litefactory to select a run's mode before launching
the coding agent. A GitHub label should select it when present; otherwise a
configured default agent should classify the request and litefactory should set
the label. The coding agent doing the work must no longer infer or change its
own mode.

## Finding

This is a workflow and protocol change, not a prompt-only edit. Today free-text
comments become `turn.KindFreeText`; `turn.Request.mode()` converts that to
`harness.InferMode`; and `internal/harness/prompt.go` tells the launched agent to
read the target repository's `CLAUDE.md` and route itself. The selected mode is
then recorded consistently in the run row, metadata, prompt, commit trailer,
and ask-permission logic, but litefactory only knows the placeholder `infer`.

The daemon already contains a useful precedent for label routing. `Coding
Agent: <Name>` and `lf:tools=<profile>` are resolved from the fresh issue
snapshot before a worktree or run row is created. Both use a strict ladder for
missing, ambiguous, malformed, unknown, and valid labels. Mode selection should
follow that architecture, with one important difference: an absent mode label
is not a default-value branch; it starts a classifier run and a GitHub label
write.

The GitHub package currently reads issue labels but has no mutation method, and
the daemon's `Forge` interface consequently has no label operation. The normal
agent `Runner` contract is also the wrong direct abstraction for classification:
it requires a task ref, persisted conversation identity, run ordinal, worktree,
permissions, and full run logging. Classification happens before those things
should exist and must not dirty a task worktree.

## Recommended behavior

Use exactly four canonical labels:

| Label | Internal mode |
|---|---|
| `Mode: Research` | `research` |
| `Mode: Plan` | `plan` |
| `Mode: Implement` | `implement` |
| `Mode: Spike` | `spike` |

Treat the label as the selector for the **next run**, rather than permanent
session state. The processing sequence should be:

1. Read the fresh issue snapshot and resolve a mode label with the same
   detect-loosely/accept-strictly ladder used by agent labels.
2. If one valid label exists, use it. Multiple, mis-cased, malformed, or unknown
   `Mode:` labels refuse before a worktree or run row exists.
3. If none exists, invoke a dedicated classifier through the configured default
   coding-agent implementation. Give it the current owner instruction plus the
   issue and condensed thread context, and require one value from the closed
   four-mode vocabulary. Do not expose tools or a writable task worktree.
4. Add the canonical label through GitHub, then dispatch the ordinary turn with
   that concrete mode. If adding the label fails, do not run: otherwise the
   forge and the stored run would disagree about who selected the workflow.
5. After the run report is posted and its trigger watermark is committed,
   remove the mode label. If removal fails, retain/retry cleanup without rerunning
   the consumed instruction.

One-shot consumption solves the issue's “doomed to stay in one mode” edge case:
the next owner comment is classified afresh unless the owner deliberately sets
a label first. It also preserves “one run, one mode”; no agent can transition
the run it is currently executing.

Explicit slash commands should remain accepted for compatibility, but become
an input to the same mode decision rather than a separate source of truth. A
command and a label that disagree should be refused visibly; silently choosing
either would make the run header misleading. A matching pair is harmless.

## Classification contract

Add a small classifier contract at its point of consumption rather than reuse
`agent.Runner`. Its request contains only redacted/framed repository context and
its response is a mode enum. Each coding-agent integration may implement the
contract using its own non-interactive invocation, but litefactory owns:

- the closed mode vocabulary and exact output parsing;
- prompt framing and the rule that third-party notes are evidence, not commands;
- timeout, environment allowlist, logging, and redaction;
- choosing the configured default agent;
- label mutation and all retry/failure semantics.

Reject prose, empty output, multiple values, and unknown modes. Do not attempt
to extract a plausible word from an otherwise invalid response. A classifier
failure is a reportable outcome and must leave the triggering comment
unconsumed so a later tick can retry.

The classifier prompt should include the target repository's routing text. A
classifier running outside the worktree cannot merely be told to read
`CLAUDE.md`; litefactory should read a bounded, explicitly named routing file
from the checked-out base revision through `internal/git`, or define a
repository-neutral fallback rubric. The existing four-mode rubric in this
repository is a good default, but hard-coding it as the only policy would make
litefactory specific to its own repository.

## Configuration

Add a top-level setting such as:

```yaml
default_agent: "Claude"
```

This replaces the current hard-coded `store.AgentClaude` fallback for both
ordinary unlabelled agent selection and mode classification. Validate it
against the runners actually configured at daemon startup, using the same
canonical display-name mapping as `Coding Agent:` labels. The shipped default
should remain Claude for compatibility. Add it to `Defaults()` and the generated
template together, preserve strict YAML decoding, aggregate validation errors,
and keep `internal/config` independent of runtime runner types.

The setting should name an agent, not a “harness”. `harness` is currently the
Claude implementation detail; `agent.Runner` is already the vendor-neutral
boundary and Codex is a second implementation.

## Persistence and failure semantics

The selected concrete mode should replace `infer` everywhere downstream. Store
it on the run as today; no migration is required merely to admit the four string
values. Add a durable cleanup marker only if label removal cannot be made part
of an existing post-then-commit transaction boundary. A process crash after the
watermark commit but before label removal must not cause the next unrelated
comment to inherit the stale label.

GitHub label mutation has race concerns. Avoid a read/replace-all-labels API,
which can overwrite a human's concurrent edits. Use targeted add/remove
operations. Before removing, verify the canonical label still represents the
completed run; a human may have replaced it to queue a different mode while the
run was active. The safest design records the selected label and the issue
snapshot/version associated with the run, then removes only that exact label.

Classification introduces an extra external process and API mutation on the
unlabelled path. It must occur after admission/authorization but before taking a
turn slot if classifier capacity is separate, or count against the same bounded
worker pool if it is not. It must never happen on every discovery pass for the
same pending comment. A small persisted “classification pending/selected” state
may therefore be preferable to relying solely on an eventually consistent
label read.

## Transition alternatives considered

### Persistent current-mode label plus human edits

Simple and visible, but it creates exactly the reported trap: ordinary replies
inherit the old mode forever. Natural-language “move to the next phase” cannot
reach the classifier because the label prevents classification.

### Agent-emitted `mode change requested`

Useful as advice, but unsafe as an automatic control channel. Model output is
not authorization, and automatically accepting it violates the repository's
rule that only a human opens the next mode. It also needs a robust structured
output protocol and creates ambiguity when the report succeeds but the control
message cannot be parsed. A report may recommend the next mode in prose; the
owner then sets a label or comments, and the following run decides.

### Artifact-driven automatic transitions

Deterministic for repositories with the same Research/Plan gates, but not
portable and still violates one-run/one-mode if it opens the next phase without
a human instruction. Artifacts are good validation inputs: selecting Plan
without a research brief, or Implement without a plan brief, should be caught by
the target repository's agent workflow. They should not be the transition
trigger in litefactory.

### One-shot next-run labels (recommended)

Keeps manual control, makes unlabelled natural-language replies useful, and
prevents stale modes. It costs label cleanup and durable crash handling, but
those are explicit engineering problems rather than hidden workflow behavior.

## Suggested delivery slices

1. Introduce a typed mode vocabulary and pure mode-label resolver with refusal
   rendering; pass a resolved mode explicitly on `turn.Request`; delete
   `Request.mode()` inference and all infer guidance from agent prompts.
2. Add `default_agent`, replace hard-coded Claude fallbacks, and validate the
   selected implementation at startup.
3. Add the dedicated classifier implementations and strict result parser,
   initially retaining `/spike` and explicit labels so this can ship safely.
4. Add targeted GitHub label mutation plus durable selection/cleanup state and
   wire the unlabelled classification path.
5. Add transition and crash tests: ambiguity, label/classifier disagreement,
   classifier failure, add failure, report failure, stale cleanup after restart,
   concurrent human relabeling, and next-comment reclassification.

## Acceptance boundaries for a later plan

- No ordinary work-agent prompt contains `Mode: infer` or tells the agent to
  choose/change its mode.
- Every started run has one concrete mode known before worktree preparation.
- A valid `Mode: <Type>` label avoids the classifier call.
- An absent mode label invokes only the configured default agent's classifier
  and sets the canonical label before work starts.
- Invalid or ambiguous labels, invalid classifier output, and GitHub mutation
  failures produce no worktree and no run row.
- Completing one run cannot silently pin subsequent comments to that mode.
- A human relabel during an active run is not overwritten by cleanup.
- Existing post-before-watermark guarantees and daemon concurrency bounds remain
  intact.

## Scope deliberately left for Plan

The exact durable state shape and transaction split need a story-level design
against the current store/report path. The classifier CLI argv/event formats
also need implementation-specific measurement, especially for Codex. Those are
planning and implementation tasks; this brief establishes the workflow and the
invariants they must preserve.
