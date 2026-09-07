# US-3 — Render re-parsed JSON as stock `slog.TextHandler` text

> As an operator, I want that output to look exactly like what `litefactory daemon run` prints on my terminal so that I do not have to learn a second format or read JSON.

This story turns one `daemon.log` JSON line back into the exact text that Go's standard `slog.TextHandler` would have printed for that same record.

## Why This Matters

The whole point of this feature is that reading litefactory's own log doesn't require learning a new format — it has to look exactly like what already scrolls past when the daemon runs in the foreground. That is a much stricter promise than "looks similar": it means reproducing quoting rules, attribute ordering, and how grouped fields get flattened, byte for byte, and getting any one of those wrong is invisible until someone puts the two outputs side by side. This is also the piece with no dependency on anything else in the feature, and the one most worth testing against the real formatter rather than hand-written expectations.

## Architecture Notes

- Pattern to follow: `logSink` in `cmd_logs_render.go` is the extension point — a third implementation beside `rawSink` and `renderSink`, in a new file (`cmd_logs_slog.go`, or appended to `cmd_logs_render.go`). Either way, it joins `logsSourceFiles`.
- Key interface: `write(line []byte) (done bool, err error)`, with `done` always `false` — commented to explain why, since the app log has no concept of a run ending.
- The byte-identity promise requires: an order-preserving decode (`json.Decoder`/`Token()`-based — never `map[string]any`, which destroys attribute order); flattening a `WithGroup`-produced nested JSON object to dotted keys; and reformatting the JSON line's RFC3339-nanosecond timestamp to `TextHandler`'s RFC3339-millisecond form. This promise only holds because `logging.New` sets no `AddSource`/`ReplaceAttr` on its handlers — note that in a comment, since adding either later would silently break this renderer.
- A line that is not valid JSON, or is JSON but not an object, is emitted verbatim — never dropped, never an error. This applies identically to the tail pass and the follow pass.
- `--raw` bypasses this renderer entirely and emits the selected lines byte for byte.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-3.1 | Order-preserving JSON line decode (`json.Decoder`/`Token()`-based) | code | — | 3h |
| T-3.2 | Line renderer: built-in key order (`time`, `level`, `msg`, then attrs), value quoting matching `TextHandler` | code | T-3.1 | 4h |
| T-3.3 | `WithGroup` nested-object to dotted-key flattening | code | T-3.1, T-3.2 | 2h |
| T-3.4 | Timestamp reformat: RFC3339-nanoseconds to RFC3339-milliseconds | code | T-3.1, T-3.2 | 1h |
| T-3.5 | Pass non-JSON / non-object lines through verbatim | code | T-3.1 | 1h |
| T-3.6 | `--raw` pass-through: selected lines emitted byte for byte, skipping the renderer | code | T-3.1 | 1h |
| T-3.7 | New sink implements `logSink` with `done` always `false`, commented | code | T-3.2 | 1h |
| T-3.8 | Add the new renderer file(s) to `logsSourceFiles` | test | T-3.7 | 0.5h |
| T-3.9 | Golden test: log identical records through a real `slog.JSONHandler` and a real `slog.TextHandler`, diff the re-rendered output against the `TextHandler` output | test | T-3.1, T-3.2, T-3.3, T-3.4 | 3h |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-3.1 | Given a `daemon.log` line produced by `slog.NewJSONHandler` from a record, the rendered app-log line is byte-identical to what `slog.NewTextHandler` produces for that same record |
| AC-3.2 | A record written through `WithGroup("git")` with attr `branch=main` renders as `git.branch=main`, not as a JSON object |
| AC-3.3 | Attribute order in the rendered line matches the order in the `daemon.log` line |
| AC-3.4 | Timestamps render in RFC3339-milliseconds form, matching `TextHandler` |
| AC-3.5 | A non-JSON line in `daemon.log` is emitted verbatim and does not abort the view |
| AC-3.6 | `litefactory logs --raw` with no issue writes the selected `daemon.log` lines byte for byte |
