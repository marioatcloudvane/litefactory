# Spike — logging improvements

2026-08-25. Issue #3: `litefactory logs <task>` handed back a path to a JSONL
file instead of the logs.

## What changed

`logs` now renders the run's event stream. Every output line starts with its
kind, which is what makes the whole thing greppable:

```
system  init  model=claude-opus-5  session=ef123456  cwd=/work/api  tools=3
text    I'll start by reading the router.
tool    Read  /work/api/router.go
output  Read  3 lines, 29 B  package api
tool    Bash  go test ./...
output  Bash  error  2 lines, 28 B  FAIL api 0.2s
result  ok  4m31s  $0.8312  22 turns
```

- default view: newest run's summary, then its rendered events. The run listing
  moved to `--runs`.
- `--only kinds` — `system,text,think,tool,output,result,user,other`, plus the
  aliases `agent` (text+think+tool) and `all`. Thinking is off unless asked for.
- `--grep regexp`, `-i`, `--invert` — line filters over the rendered output, or
  over the JSONL under `--raw`.
- `--full` — lifts the caps on tool arguments and tool output.
- `-f` follows in rendered form; `-f --raw` is the old byte-for-byte follow.
- the positional now also takes a task ref (`github:acme/api#123`).
- the forge comment's **Log:** line leads with `litefactory logs N --run M`
  rather than with the file path.

## Decisions worth remembering

- Rendering happens per output line, not per event, so `--grep` behaves like
  grep: a matched line still carries its kind label and can be placed.
- Sub-agent events are tagged `[agent <id>]` from `parent_tool_use_id`. It is
  the only thing in the stream that distinguishes a Task sub-agent's activity
  from the main agent's.
- `--raw` with no filter is still `io.Copy` — byte for byte, AC-5.5 intact. The
  follow tests that assert byte-for-byte output were re-pointed at `--raw`;
  their rendered equivalents are new tests beside them.
- Redaction is still outbound-only (AC-5.7). The structural guard now covers
  `cmd_logs_render.go` too, so splitting the renderer out is not a way around
  it.

## Not done

- No colour and no TTY detection: the output is meant to survive a pipe.
- `--grep` does not exit non-zero when nothing matched, unlike grep(1).
- The forge comment still quotes the raw output tail; only its **Log:** line
  changed.
