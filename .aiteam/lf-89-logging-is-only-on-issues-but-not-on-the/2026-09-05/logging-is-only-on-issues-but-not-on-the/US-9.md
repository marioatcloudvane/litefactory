# US-9 — Tail by default

> As an operator, I want the default view to show the tail rather than the whole unrotated file so that a months-old `daemon.log` does not flood my terminal.

This story makes the app-log view print the last 200 records by default, with `-n` and `--all` to change that.

## Why This Matters

Nothing today prunes litefactory's own log, so a machine that has been running for months could have a file large enough that printing all of it — every time someone just wants to see what's happening right now — would flood the terminal and take a long time doing it. Showing the tail by default fixes the common case; letting someone ask for everything keeps the full history reachable when they actually want it.

## Architecture Notes

- Bind `-n` (default 200) and `--all` in `logsOptions.bind` alongside the app log's other flags. `-n` together with `--all`, or either flag on the issue path, is a usage error, exit 2, naming the offending flag — mirroring US-1's flag-applicability rule.
- `-n` must be `>= 1`; `-n 0` or negative is a usage error, exit 2.
- The tail computation reads backward from the end of the file to find the Nth-from-last newline, then streams forward — it must never load the whole file into memory to count lines. `daemon.log` is unrotated by design and must be assumed able to grow large.
- The tail window is over lines on disk, applied *before* filtering — `-n 200 --level error` can legitimately show fewer than 200 lines; this is correct and must not be "topped up" after filtering.
- A file shorter than N lines starts at offset 0 with no error. A missing file is not an error (see US-1); the reader must never create the file or its directory as a side effect of computing the tail.
- Hands its computed starting offset to US-2's follow loop rather than the follow loop re-seeking independently.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-9.1 | Bind `-n` (default 200) and `--all` flags; validate mutual exclusivity and `-n >= 1` | code | — | 1.5h |
| T-9.2 | Implement backward-scan tail computation (find the Nth-from-last newline without loading the whole file) | code | T-9.1, US-3's decoder | 3h |
| T-9.3 | Apply `--all` as "start from offset 0" | code | T-9.1 | 0.5h |
| T-9.4 | Hand the computed offset to the follow loop for `-f` (compute once, do not re-seek) | code | T-9.2, US-2 | 1h |
| T-9.5 | Tests: default 200; `-n 20` prints 20; `--all` prints everything; `-n`/`--all` mutual exclusivity exits 2; `-n 0` exits 2; a file shorter than N lines starts at offset 0 | test | T-9.1, T-9.2, T-9.3 | 2.5h |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-9.1 | `litefactory logs` prints the last 200 records by default; `-n 20` prints 20; `--all` prints every record |
| AC-9.2 | `-n` together with `--all` exits 2; `-n 0` exits 2 |
| AC-9.3 | Requested `-n 5000` on a file with 12 records prints all 12, exit 0 |
| AC-9.4 | The tail computation never loads the entire file into memory to count lines |
