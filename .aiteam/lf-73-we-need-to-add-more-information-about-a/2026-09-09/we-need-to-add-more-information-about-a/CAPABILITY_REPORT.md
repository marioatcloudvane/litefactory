# Metering capability gate — run 0012

Date: 2026-09-09. Scope: frozen US-1, issue #172, revision `3a37dbaea3f505cf001954c1c861bc0294da62c3d24ddeb7d084cbb12d05b3b2`.

**Result: blocked at AC-1.4.** No provider collection surface has been approved as supporting the complete two-provider contract. No production files were changed by the US-1 engineer.

## Probe boundary

Manual commands ran in fresh temporary directories outside the repository. Prompts were delivered on stdin. The child environment contained only present values of PATH, HOME, USER, SHELL, LANG, LC_ALL, TERM and TMPDIR, matching the existing runner allowlist. No forge credentials, permission bypass, alternate authentication, runtime-state edits or daemon changes were used. Python subprocess deadlines bounded executions and owned process groups were killed on timeout; neither real execution timed out. Raw stdout/stderr remain scratch-only and are not proposed as committed fixtures.

## Installed versions and authentication

- `claude --version`: `2.1.266 (Claude Code)`, exit 0.
- `codex --version`: `codex-cli 0.153.4`, exit 0; PATH alias creation warning: `Operation not permitted (os error 1)`.
- Allowlisted `claude auth status --json`: exit 0; loggedIn true, authMethod claude.ai, apiProvider firstParty. Identity fields were not retained.
- Allowlisted `codex login status`: exit 0; `Logged in using ChatGPT`. No credential bytes were read or printed.

## Claude: partial positive evidence

```text
claude --print --verbose --output-format stream-json --no-session-persistence --tools Agent,Read --permission-mode default
```

Stdin requested two parallel subagents, one delegating to a nested subagent, with no file reads/writes, shell or network tools. Exit 0; stdout 23,055 decoded characters; stderr empty; terminal result success, is_error false, num_turns 3, permission_denials empty.

Machine events contain three distinct task_started identities and completed task_notification records. One assistant event has a nonnull parent_tool_use_id and model `claude-opus-5`; root assistant events also report that model. Task notifications expose total_tokens but do not by themselves establish complete exclusive input/output/cache accounting or actual model for every child. Repeated root assistant event fragments share usage snapshots, demonstrating that naïvely summing message usages would double count. Terminal modelUsage contains `claude-opus-5[1m]` and `claude-haiku-4-5-20251001` entries; these remain reconciliation evidence, not child attribution.

This is evidence that real delegated work and richer metadata exist, **not** proof of AC-1.4 or the failed-child/model-switch requirements in downstream stories. This probe did not inspect associated persistent transcripts, enable extra forwarding or assert a supported collection format. Generated final prose was not used as capability proof.

## Codex: environment blocker confirmed twice

Initial command:

```text
codex exec --json --ignore-user-config --ignore-rules --sandbox read-only --skip-git-repo-check --ephemeral -
```

Stdin requested the same bounded delegation topology. Exit 1; stdout zero bytes. Diagnostic:

```text
WARNING: proceeding, even though we could not create PATH aliases: Operation not permitted (os error 1)
Error: failed to initialize in-process app-server client: Operation not permitted (os error 1)
```

One minimally different confirmation removed `--ephemeral` and reduced stdin to a request to reply `ready` without tools:

```text
codex exec --json --ignore-user-config --ignore-rules --sandbox read-only --skip-git-repo-check -
```

It produced the identical exit 1, empty stdout and initialization error. The JSON/user-config/rules/sandbox options match the repository runner; `--skip-git-repo-check` is required only because the authorized probe directory is outside a repository. Failure therefore precedes any model or descendant accounting evidence and persists without ephemeral mode. This establishes an environment execution blocker, **not** a claim that Codex lacks the required capabilities. No broader permissions or alternate credentials were attempted.

## Consequence and resumption

AC-1.4 is unmet. US-1 cannot verify; US-2/3/4 require US-1 and downstream stories cannot be started as satisfied dependencies. Do not fabricate supported versions, child models or sanitized real-execution fixtures from these incomplete results. Root-only implementation cannot substitute for the approved full-descendant release requirement.

Resume when the managed execution environment permits the standard Codex CLI to initialize its in-process app-server without changing the repository security boundary. Repeat version and allowlisted auth checks, then the minimal read-only scratch command above. Once it succeeds, perform source-pinned Claude and Codex root/parallel/nested/failed-child and model-change probes, sanitize only the relevant associated evidence, and validate complete ancestry, exclusive usage and replay/resume baselines before selecting the adapters' collection surfaces. No user login change is currently indicated by these results.
