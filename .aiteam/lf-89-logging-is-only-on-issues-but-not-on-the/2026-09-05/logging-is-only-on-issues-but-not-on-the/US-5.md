# US-5 — `--level` as a display-time filter on `logs`

> As an operator, I want `litefactory logs --level warn` to show only records at or above a level keyword so that I can cut the firehose down.

This story adds a `--level` flag to `logs` that hides app-log records below a chosen threshold.

## Why This Matters

A daemon that has been running for even a short while writes far more than an operator needs to look at in any one moment. This gives a way to say "just show me warnings and worse" without already knowing what's in the file. It's worth being upfront about its one real limitation: it can only hide detail the daemon already wrote — it can't reveal detail that was never captured in the first place, which is exactly the confusing-empty-result trap this same feature closes elsewhere (see US-8).

## Architecture Notes

- Pattern to follow: bind `--level` in `logsOptions.bind`, the one place `logs` flags are declared, alongside the app log's other flags.
- Parse with `logging.ParseLevel` — the only level parser used anywhere in this feature. An unparseable value is a usage error, exit 2, naming the vocabulary (`debug, info, warn, error`).
- A record whose level is missing or unrecognized is always shown, never hidden by `--level`.
- This flag is display-time only: it must not touch `config.yaml` and must not write anything. Combined with `--raw`, the filter still applies to selection even though `--raw` changes only the rendering, not the selection.
- Misusing `--level` on the issue path (an issue positional given alongside `--level`) is a usage error, exit 2, naming `--level` — the symmetric counterpart of US-1's flag-applicability rule.
- This story's filter owns the "levels actually seen" observation that US-8's stderr advisory reads.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-5.1 | Bind `--level` flag in `logsOptions.bind` | code | — | 1h |
| T-5.2 | Parse the flag value with `logging.ParseLevel`; unparseable value is a usage error naming the vocabulary | code | T-5.1 | 1h |
| T-5.3 | Apply the filter to decoded records (`>=` comparison); records with a missing/unrecognized level always pass | code | T-5.2, US-3's decoder | 2h |
| T-5.4 | Track which levels were actually observed, as a small piece of state owned by the app-log view, for US-8 to consume | code | T-5.3 | 1h |
| T-5.5 | Tests: `--level warn` shows WARN/ERROR only; `--level DEBUG` case-insensitive; `--level loud` exits 2 naming the vocabulary; `--level` with an issue positional exits 2 naming `--level` | test | T-5.1, T-5.2, T-5.3 | 2h |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-5.1 | `litefactory logs --level warn` prints WARN and ERROR records and no INFO or DEBUG records |
| AC-5.2 | `litefactory logs --level DEBUG` is accepted (case-insensitive); `litefactory logs --level loud` exits 2 and names `debug, info, warn, error` |
| AC-5.3 | A record whose level is absent or unrecognized is always shown regardless of `--level` |
| AC-5.4 | `--level` supplied together with an issue positional exits 2 and names `--level` |
