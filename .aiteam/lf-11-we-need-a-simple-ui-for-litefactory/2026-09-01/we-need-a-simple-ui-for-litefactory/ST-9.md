# ST-9 — `litefactory ui` command

> As the operator, I want a `litefactory ui` command that prints the URL
> including the token, so I can open the UI without hunting for the secret.

Adds the `litefactory ui` CLI command: it prints exactly one line, the UI's
URL with the bootstrap token, and nothing else.

## Why This Matters

The token that gates the whole feature lives in a file on disk, and asking
an operator to go find it, base64-decode it, and build the URL by hand is
exactly the kind of friction that gets a security feature quietly bypassed
or ignored. This tiny command is the whole difference between "the UI is
gated by a token" being a minor inconvenience and being something nobody
actually uses correctly.

## Architecture Notes

- Output discipline: exactly one line, `http://<ui.addr>/?token=<token>`, and
  nothing else — no headers, no extra prompts, so the command is safe to
  pipe or open directly.
- Since the operator must stay on whichever host is printed (per the plan
  brief's Host/Origin allowlist decision — cookies are host-scoped even
  though both `127.0.0.1` and `localhost` are accepted), the command's help
  text should say so in one sentence.
- Precondition errors use the existing `*PreconditionError` / exit-3
  convention: `ui.enabled: false`, `$LF_HOME` not initialised, and
  `ui.token` absent (message: start the daemon once) are three distinct,
  named failures, not one generic error.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-9.1 | `newUICommand()` in a new `cmd_ui.go` + one line in `Commands()`; prints exactly one line to `env.Stdout` | api | ST-1's T-1.5 | S |
| T-9.2 | Precondition errors (exit 3 via `*PreconditionError`): `ui.enabled: false`; `$LF_HOME` not initialised; token file absent → "start the daemon once" | api | T-9.1 | S |
| T-9.3 | Tests: exact single-line output matching the token file; each precondition's message and exit kind | test | T-9.2 | S |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-9.1 | `litefactory ui` prints exactly one line, `http://127.0.0.1:8787/?token=<token>`, matching the contents of the token file. |
| AC-9.2 | `litefactory ui` with `ui.enabled: false`, with `$LF_HOME` uninitialised, and with no token file yet, each exit non-zero with a distinct, named precondition message. |
