# Implementation Plan — comment-intent

# Branch: main

# Date: 2026-08-25

Replaces `IMPLEMENTATION_PLAN.superseded.md` (9 stories, 46 tasks). Same goal, a
fraction of the machinery.

## The idea

An owner comment with no slash command currently ends at `KindNone`: recorded,
ignored, consumed. Make it start a run instead, and let the agent decide what
kind of work it is.

The decision does not need to happen in Go. The worktree the run executes in
contains the target repo's own `CLAUDE.md`, which already has a *Classifying the
Mode* section. The child reads it and routes itself. litefactory's job is only
to stop demanding the prefix, and to say in the prompt that picking the mode is
part of the task.

Everything the header needs is already there: `FeatureSlug` is frozen per task
and stated in the prompt, and the header already tells the child it is headless
and must not ask questions.

## Changes

Three files, three edits.

### T-1 — free text spawns a run (`internal/turn/command.go`)

Add one kind:

```go
// KindFreeText is an owner comment with no leading slash command. It starts a
// run whose mode the agent decides for itself.
KindFreeText
```

- `ParseCommand`: a body whose first token is not a command token returns
  `Command{Kind: KindFreeText, Text: strings.TrimSpace(body)}`, or
  `Command{Kind: KindNone}` when the trimmed body is empty.
- `Spawns()` → true for `KindFreeText`.
- `NeedsReply()` → false for `KindFreeText` (unchanged for the rest).
- `String()` → `"free_text"`.

`KindNone` survives, now meaning only "empty/whitespace body": recorded, ignored,
consumed, exactly as today.

**Untouched:** `/spike`, `/plan` and friends, and every unknown slash command.
Their parse results, replies and reactions are byte-identical.

### T-2 — mode is `infer` (`internal/turn/execute.go`)

`mode()` reads `Command.Name[1:]`, and free text has no name. Switch on the kind:

```go
func (r Request) mode() string {
	if r.Command.Kind == KindFreeText {
		return "infer"
	}
	return r.Command.Name[len("/"):]
}
```

`validate()` gains `KindFreeText` alongside `KindSpike` in its "must have
instruction text" case. `prepare.go` needs no change at all — `Instruction` is
already `req.Command.Text`, which now holds the whole body.

### T-3 — the prompt says to choose (`internal/harness/prompt.go`)

In `writeHeader`, when `Mode == "infer"`, one extra paragraph after the
feature-slug sentence:

> Part of this task is deciding what kind of work it is. If this repository's
> `CLAUDE.md` defines modes or a routing scheme, follow it and pick the one this
> request calls for. Otherwise just do the work as asked. Write any documents
> you produce under the feature slug named above.

No other mode changes its prompt.

### T-4 — tests

- `command_test.go`: free text spawns; empty and whitespace-only bodies do not;
  a body opening with `/usr/bin/…` is free text, not a command.
- **Golden test**: `/spike do a thing` produces the same `Command` and the same
  `BuildPrompt` output as before this change. This is the regression bound — M0
  ran end to end against real GitHub on 2026-08-25 and must not move.
- `prompt_test.go`: `Mode: "infer"` adds the paragraph; `Mode: "spike"` does not.

`make check` green.

## Deliberately not built

| Dropped | Why |
| --- | --- |
| Second `claude -p` classifier subprocess | The run itself classifies. A separate call costs a second model invocation to produce a label the first one derives anyway. |
| `mode` column + migration `0002` | Nothing queries it. The mode is in the prompt, the metadata document and the report. |
| Typed `Mode` enum, `Source`, `turn/infer.go`, `harness/classify.go` | A string in a header does not need a type system. |
| Per-mode tool sets | If Research needs `WebSearch`, that is one line in `~/.litefactory/config.yaml`. |
| Cost ceilings + budget plumbing | You already chose no ceilings (OD-3). |
| Frozen UTC session date | See below. |

## Two things to know before this ships

**Every owner comment now costs a run.** That is the point, but it is a real
change: a one-word "thanks" on an issue starts `claude -p`. If that bites, the
cheapest brake is a config'd minimum body length, not a budget system.

**Comments on different days land in different folders.** Your `CLAUDE.md:55-60`
has the child run `date=$(date +%Y-%m-%d)`, so day two writes a new session
directory beside day one's. The superseded plan solved this with a stored column
and a migration. The one-line version is to change that line in your own
`CLAUDE.md` to take the date from the issue instead. Not litefactory's problem
and not in this plan.

## Order

T-1 → T-2 → T-3 → T-4. Serial, one engineer, one sitting.
