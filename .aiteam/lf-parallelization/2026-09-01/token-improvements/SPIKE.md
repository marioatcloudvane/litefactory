# Spike — token-improvements

2026-09-01. Mario is now running litefactory's own development through litefactory,
and asked why the runs are so expensive. This spike measures where the tokens
actually go, using the run logs litefactory already writes, and names the levers.

No code was changed.

## What was done

Read the 24 recorded runs under `~/.litefactory/logs/runs/github/marioatcloudvane/litefactory/`.
Each run has an `NNNN.jsonl` stream log and an `NNNN.meta.json`. Three things
were extracted:

- **Per-request usage.** Every `type: "assistant"` event carries
  `message.usage` and — this is the useful part — a `subagent_type` field, so
  input/output/cache tokens can be attributed to the specific agent that spent
  them rather than to the run as a whole.
- **Terminal totals.** The `type: "result"` event carries `modelUsage`, which is
  the authority on what the run cost and what context window it ran in.
- **Tool traffic.** Every `tool_use` block and its matching `tool_result`, to see
  which files were read, how often, and how many bytes each agent pulled into
  its context.

Rates were not looked up. They were backed out of litefactory's own recorded
`cost_usd` and reconcile to within ~1%: roughly $0.50/MTok cache read,
$6.25/MTok cache write, $25/MTok output for `claude-opus-5`. Good enough to
attribute cost by category, which is all this spike needed.

## What was found

### The shape of the spend

Across all 24 runs:

| | tokens |
|---|---|
| cache **read** (context resent on every request) | 73.8M |
| cache **write** (new context entering the window) | 3.1M |
| **output** (what the runs produced) | 0.87M |

**85 tokens of context re-read per 1 token produced.** ~$185 total, median ~$8
per run, worst $18.49 (issue #5).

For run #5 the cost splits $7.77 cache read / $4.49 cache write / $6.11 output —
so no single category is the villain. The volume is.

### The mechanism: cost is quadratic in tool calls per agent

Every tool call is one API request, and every request resends the whole
conversation so far. An agent that makes *n* tool calls while accumulating
context pays roughly n²/2 × growth-per-turn. Run #5, by agent:

```
agent                  requests   first ctx   median ctx   max ctx      Σ input
research-orchestrator     95       12,215     103,468     195,826    9,229,338
codebase-tour             88       10,391      95,753     158,146    7,839,152
general-purpose          135       11,698      44,078     113,123    6,533,691
Explore                   41        5,575      60,038      96,829    2,433,484
MAIN                      24       16,845      26,068      38,002      633,171
```

`codebase-tour` pulled ~362KB (~90k tokens) of tool results into its context,
then paid for them on each of its remaining ~80 turns: **7.8M tokens billed for
90k tokens of actual material.** The two orchestrating agents are ~70% of the run.

The important consequence: halving an agent's tool-call count *quarters* its
cost. Trimming what it reads is linear; trimming how many turns it takes is
quadratic.

### The five things feeding it

1. **`.aiteam/` archives live inside the worktree.** 1.6MB across 65 markdown
   files, and research agents grep and read them. `lf-7`'s
   `IMPLEMENTATION_PLAN.md` is 117KB (~29k tokens) — one `Read`.
   `AC_AMENDMENTS.md` is 70KB and was read 3× in run #9; `APP_CONTEXT.md` is
   51KB, read 2×. An 18k-token file read at turn 20 of a 95-turn agent is
   resent 75 more times: 1.35M cache-read tokens, ~$0.68, for a single `Read`.

2. **Fan-out multiplies the static prefix.** Run #5 launched 15 subagents. Each
   starts cold: ~17k baseline (system prompt, tool schemas, skills list) plus
   CLAUDE.md (10.9KB) plus its own agent definition (`go-architect.md` is 21KB).
   That is what the 3.1M cache-write column is — 15 separate cache primings,
   nothing shared between them.

3. **`contextWindow: 1000000`.** Confirmed in every `result` event: the runs use
   `claude-opus-5[1m]`. There is no compaction pressure, so contexts grow
   monotonically to 196k and every later turn pays full freight on everything
   earlier. The 1M window removes the one thing that would otherwise cap this.

4. **Whole-file reads, repeated.** `initialplan.md` (27.5KB) read 9× in run #5.
   `internal/daemon/tick.go` (1630 lines) 5×. `internal/harness/prompt.go` 5×.
   Each repeat is a fresh cache write on top of the resends it then causes.

5. **Roughly 10% is spent on runs that produce nothing — and that figure is an
   undercount.** $14.73 is recorded against non-succeeded runs, but a run killed
   before its `result` event has `cost_usd: null` and contributes $0.
   `4/0001` (abandoned) burned 11.6M tokens that way; `8/0001` (failed, status
   143) burned 42.1M, with `research-orchestrator` alone doing 196 requests at a
   183k average context. Two runs on issue #7 hit the 1800s timeout wall
   mid-flight. The true waste is materially above 10%.

### What is *not* the problem

- **Prompt assembly.** `internal/harness/prompt.go` is lean and the caps
  (8KB body, 8KB comment, 32KB history) hold. Run #9's prompt was ~1.5KB.
- **Resume.** It works — resumed runs send only the delta, as designed.
- **The tool surface.** 7 tools, tightly scoped. Not a factor.

The expense is entirely inside the run, in the `.aiteam` agent framework's own
fan-out and reading habits.

## Decisions worth remembering

- **Measure from the stream logs, not from `cost_usd`.** `cost_usd` is null for
  every run that died before its terminal event, which is exactly the set of
  runs you most want to account for. Summing `message.usage` across the jsonl
  is the honest number.
- **`subagent_type` on assistant events is the whole diagnostic.** Without it,
  a run is one undifferentiated $18. With it, two agents are visibly 70% of it.
  Worth preserving in any future log-format change.
- **Optimise turn count before context size.** The quadratic term is where the
  money is. "Read less" helps linearly; "take fewer turns" helps quadratically.
- **The archive is a compounding liability.** `.aiteam/` grows with every
  session and every future run pays to grep past it. This gets worse on its own.

## Not done

- No code, config or agent definition was changed.
- The fixes below are not estimated beyond the rough halving claimed for the
  first two — that estimate comes from the run-#5 attribution, not from a trial.
- Other repos driven by this daemon were not examined; only litefactory's own runs.
- No check on whether `contextWindow` is settable per-subagent in the current
  claude CLI. That needs verifying before fix 4 is planned.

## Recommendation

Ranked by leverage. The first two attack the quadratic term and together should
roughly halve the spend.

1. **Get `.aiteam/` out of the worktree, or gate it.** Ship only the current
   session directory; keep archives out via sparse-checkout, or move them to a
   separate branch or repo. Nothing in a run needs last week's 117KB plan.
2. **Cap `codebase-tour` and `research-orchestrator`.** Give both an explicit
   tool-call budget, and require targeted reads (`grep -n` to locate, then a
   line range) instead of whole-file `Read`. Note the two-pass tour currently
   reads broadly and *then* narrowly — the broad pass is the expensive one and
   it stays in context for the whole narrow pass.
3. **Split `APP_CONTEXT.md` (51KB) and `AC_AMENDMENTS.md` (70KB)** into indexes
   plus per-package sections, so an agent loads ~3k tokens instead of ~31k.
4. **Drop the 1M context variant for subagents.** A 200k window forces
   compaction and hard-caps the per-turn resend. Verify the CLI supports this
   per-agent first.
5. **Trim the agent definitions.** 164KB across 13 files, paid once per subagent
   launch. `go-architect.md` at 21KB is ~5k tokens on every invocation.

Items 1, 3 and 5 are self-contained edits. Item 2 changes how the research
agents are instructed to work and item 4 touches the runner's model surface —
**those two want a Plan run**, not a follow-up spike.
