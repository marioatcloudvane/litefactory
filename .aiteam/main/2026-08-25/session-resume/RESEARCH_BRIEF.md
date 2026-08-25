# Research brief — session-resume

> Provenance: this is a **spike-promoted brief**, not a fresh Research pass. The
> owner chose Plan mode directly after the spike below, with the instruction
> "don't over-complicate this". Every `file:line` here was read during that
> spike on 2026-08-25 against `94d7f75`.

## The problem

Every litefactory run starts cold. A task that has had four comments and three
runs gives run four a model that knows nothing about runs one to three — not
its own reasoning, not what it already tried, not what it already said. The
owner's words: "we would loose too much context."

## What a run is given today

`harness.BuildPrompt` (`internal/harness/prompt.go:118`) takes six fields and
nothing else: the instruction, the issue title/body/author, and the **triggering**
comment's body/author. The prompt is a header, an `## Instruction` section, and
an `## Context` section holding two `<note>` blocks (`prompt.go:144-156`).

Prior comments are absent. So is anything the previous run did or said.

## There is no resume

- `--resume` does not appear anywhere in the tree.
- `runner.go:307` passes `--session-id` only.
- `prepare.go:108` derives that id from `store.FirstSegmentOrdinal`, hardcoded
  to `1` (`store/segments.go:14`). **Every run of a task therefore reuses the
  same claude session id** — with no resume flag to make that mean anything.
- `runner.go:455` already warns that a mismatched session id would mean "a
  future segment would resume the wrong one", so the mechanism was designed for
  and then not wired up.

## The history is already on hand — twice

1. **SQLite.** The `comments` table (`migrations/0001_init.sql:113`) stores
   `body` verbatim, author, `created_at`, and `classification`
   (`instruction | third_party | self`) per session. `Admit`
   (`turn/classify.go:268`) records every comment, including litefactory's own
   replies. Gap: `bootstrap` (`daemon/tick.go:225`) fetches pre-existing
   comments, takes the max id, and **discards the payloads** — comments
   predating first sight are not in the table.
2. **GitHub, at zero marginal cost.** `ListCommentsAfter`
   (`github/comments.go:56`) sends no `since` and no cursor: it fetches the
   entire comment list via `fetchPages`, then filters `id <= afterID`
   **client-side** (`comments.go:76-81`). The full thread is already on the wire
   on every drain. Asking for it costs no extra request.

## The owner's four directives

1. **Resume the conversation** between runs rather than restarting.
2. **Relabel prior owner comments** — render the author as `old_owner` or
   similar so a historic `/spike …` does not read as a live command.
3. **Ignore prompt injection for now.** Explicitly deferred by the owner. The
   existing `<note>` framing and `</note>` neutralisation (`prompt.go:28-36`)
   stay as they are; no new hardening is in scope.
4. **Compact the history** — summarise prior comments, carry the latest one
   verbatim.

## The shape this implies

Resume and thread-replay substitute for each other. A resumed session already
holds the prior reasoning and every comment previously shown, so the only thing
to append is the **delta** since the last run, and compaction is claude's own
context management rather than litefactory's job.

Compaction earns its place on the **cold-start path** — session file gone,
worktree recreated, machine changed — where context must be rebuilt from the
`comments` table alone. That is where "summary of the thread + verbatim latest
comment" is the right answer.

Ordering matters: **resume primary, compaction fallback.** Built the other way
round, a summarizer is paid for on every run.

## Two unknowns, both untested

- **Does `claude -p` resume when handed an already-used `--session-id`,** or
  does it error? Nothing in the tree exercises this. Whether the feature needs
  `--resume` instead — and how that composes with `-p` — is unsettled.
- **Does the session file survive between runs?** Claude sessions are stored per
  working directory. The worktree path is stable per issue
  (`naming.WorktreePath`, keyed on issue number), which suggests yes, but it has
  not been verified, and a recreated worktree is a real case.

Both are cheap to answer empirically and both change the design. Plan mode
should treat them as the first task, not as assumptions.

## Out of scope

Prompt-injection hardening (deferred, above). The `/include cN` mechanism that
`turn/classify.go:260` reserves for M1 — related, not this.
