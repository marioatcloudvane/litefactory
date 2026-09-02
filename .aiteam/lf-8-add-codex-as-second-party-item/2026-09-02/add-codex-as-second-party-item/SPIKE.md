# Spike — add-codex-as-second-party-item

2026-09-02. Before opening Implement mode, the requester asked two direct
questions: (a) can Codex genuinely be used as a second harness right now, and
(b) how do they install the Codex CLI on their machine so it's available.

## What was done

- Grepped the current worktree (not the archived `.aiteam/` session — that
  directory is out of bounds per `.claude/modes/session.md`'s boundary rule)
  for any Codex-specific code that might already exist.
- Traced the harness's binary-selection path from config through to process
  launch to confirm how tightly it's bound to Claude specifically.
- Attempted to check for a `codex` binary on this machine's `PATH` directly
  via Bash.
- Delegated the installation/authentication question to `web-researcher`,
  since it's outside the repo (per Spike mode's "delegate breadth" rule).

## What was found

**(a) Not yet — no Codex code exists in this codebase today.**

- `grep -ril "codex" --include="*.go" .` (excluding `.aiteam/`) returns **zero
  matches**. Every `.go` file in the repo only ever talks about Claude.
- The harness's config type is Claude-shaped, not vendor-neutral:
  `internal/config/config.go:140` — `type Claude struct { Binary string;
  Model string; ... }`. There is no `Agent`/`Runner` interface or factory in
  the codebase yet.
- The runner constructor takes that concrete type directly —
  `internal/harness/runner.go:141` — `binary, err :=
  opts.Claude.ResolveBinary(pathEnv)`. Nothing abstracts over "which coding
  agent" at this layer.
- `internal/harness/` alone has 16 files that reference `claude` by name
  (config, runner, stream parsing, prompt handling, settings, env, etc.).

This matches what earlier Research and Plan runs on this same branch already
concluded (see this branch's 2026-08-31 session — referenced here only by
date, not read, per the session boundary rule): a `Runner` interface and
factory were *planned* (story US-5 in that session's `plan_brief.md`), but
Plan producing a design is not the same as it being built. **Implement mode
has not run for this feature.** So today, in this repository, there is no
way to select Codex as a harness — not because of a missing binary, but
because the plumbing to run anything other than Claude doesn't exist in code
yet.

Separately: attempting to check for a `codex` executable on this machine's
`PATH` from inside this session (`command -v codex` / `which codex`) required
approval that nothing here can grant, so I could not verify one way or the
other whether the binary is even installed on this host. That's an
orthogonal fact from (a) above — even if it were installed, this repo has no
code path that would invoke it yet.

**(b) Installing the Codex CLI, headless-friendly** (via `web-researcher`,
sourced from OpenAI's current docs and the `openai/codex` GitHub repo —
medium-high confidence; see caveat below):

Install (pick one):
```
npm install -g @openai/codex
# or
brew install --cask codex
# or
curl -fsSL https://chatgpt.com/codex/install.sh | sh
# or: download a platform binary from
# https://github.com/openai/codex/releases/latest
```

Headless auth (no browser flow):
```
printenv OPENAI_API_KEY | codex login --with-api-key
```
or, skip login entirely and pass the key per invocation:
```
CODEX_API_KEY=<key> codex exec "<task>"
```
A device-code flow (`codex login --device-auth`) also exists for SSH/headless
boxes where you complete sign-in from a second device — an alternative to the
API-key path, not required alongside it. Either the API key or a ChatGPT
sign-in works standalone; you don't need both. The API-key path is the one
that avoids any browser dependency, which is what a headless/automation box
needs.

For unattended runs specifically: use `codex exec "<task>"` (or `codex exec
-` for stdin) rather than the interactive `codex` TUI, with
`--sandbox workspace-write` for CI-like file-edit access. `--skip-git-repo-check`
is likely relevant if this ever runs inside a git worktree the way litefactory
does (`.git` living outside the writable root was flagged as a friction point
in the earlier Research brief on this branch).

**Caveat on (b):** the web-researcher could reach OpenAI's rendered docs
pages but not the raw `codex exec --help` output or GitHub README markdown
directly, so exact current flag names (particularly around sandbox/approval
flags) carry medium rather than high confidence. Before wiring any of this
into automation, run `codex exec --help` once by hand and diff it against
the flags above.

## Decisions worth remembering

- The gap for (a) is not "is Codex installed" — it's "does this repo have
  any code path that could run it." It doesn't, yet. Story US-5 (Runner
  interface + factory) from this branch's Plan output is the gating piece;
  nothing downstream of it can land until it exists.
- The `config.Claude` struct name itself (not just its contents) is part of
  the coupling — a factory can't return a `config.Claude` for a Codex run
  without either renaming/generalizing that type or wrapping it, which is a
  design decision Implement will need to make, not just a mechanical typing
  exercise.

## Not done

- Did not install Codex or verify the binary actually works on this machine
  — that's a hands-on step for the requester, not something this spike can
  do on their behalf.
- Did not re-verify the earlier Research/Plan brief's claims — they're in a
  different session directory and out of bounds here.
- Did not cross-check the web-researcher's flag names against
  `codex exec --help` directly (see caveat above).

## Recommendation

Answering these two questions didn't require new Research or Plan work — the
existing Plan artifacts on this branch already cover the design gap, and this
spike only needed to confirm code state and install steps. Implement mode is
what actually closes the gap in (a): specifically, US-5 (`Runner`
interface + factory) needs to land before Codex can be selected at all, and
until then the answer to "can I use Codex as a second harness" stays no
regardless of whether it's installed locally. Whether to open Implement mode
is still the requester's call, not this spike's.
