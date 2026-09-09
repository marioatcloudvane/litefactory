# Run 0014: recover the provider capability check

## Fresh observations

This retry checked installed CLI help/version and performed exactly one ordinary, 45-second-bounded Codex execution in a fresh temporary directory. It used only present PATH, HOME, USER, SHELL, LANG, LC_ALL, TERM and TMPDIR environment values, the existing saved authentication and read-only model-tool permissions. No production code, credentials, daemon settings or runtime state were edited.

Recorded on 2026-09-09, before the report timestamp 11:15:06 UTC. The probe finished naturally before its 45-second deadline; exact start time and elapsed duration were not instrumented, so this report does not invent them.

- Codex: `codex-cli 0.153.4`; executable `/Users/mariomh/.nvm/versions/node/v24.16.0/bin/codex`.
- `codex login status`: exit 0, `Logged in using ChatGPT`.
- Claude: `2.1.266 (Claude Code)`; version check only in this retry.
- Codex execution: exit 1, zero stdout bytes, before any usage or model event:

```text
WARNING: proceeding, even though we could not create PATH aliases: Operation not permitted (os error 1)
Error: failed to initialize in-process app-server client: Operation not permitted (os error 1)
```

Command:

```text
codex exec --json --ignore-user-config --ignore-rules --sandbox read-only --skip-git-repo-check --ephemeral -
```

Stdin was `Reply exactly ready. Do not use tools or read any files.`

The CLI initializes unsuccessfully. The error does **not** identify the denied resource, prove an authentication problem, prove that the PATH warning causes the fatal error, or establish that Codex cannot expose descendant usage. Host filesystem/IPC restrictions are hypotheses until a comparison identifies the denied operation. The previous CAPABILITY_REPORT remains historical evidence; its Claude execution was not repeated here.

## Concrete recovery path

An operator should run the same minimal probe from their normal authorized host terminal, under the same account and executable, without changing the CLI sandbox. This distinguishes a managed-run environment restriction from an installation/host problem. The following creates a private scratch directory and captures output there; stop with Ctrl-C if it has not completed after one minute.

```sh
lf73_codex=/Users/mariomh/.nvm/versions/node/v24.16.0/bin/codex
"$lf73_codex" --version
"$lf73_codex" login status
lf73_probe_dir=$(mktemp -d /private/tmp/lf73-host.XXXXXX)
chmod 700 "$lf73_probe_dir"
cd "$lf73_probe_dir"
"$lf73_codex" exec --json --ignore-user-config --ignore-rules \
  --sandbox read-only --skip-git-repo-check --ephemeral - \
  > codex.jsonl 2> codex.stderr <<'PROMPT'
Reply exactly ready. Do not use tools or read any files.
PROMPT
lf73_probe_status=$?
printf 'exit=%s\n' "$lf73_probe_status"
wc -c codex.jsonl codex.stderr
```

1. **Host succeeds, managed run fails:** give the environment administrator the two results, exact executable/version and timestamps. Have them identify the denied initialization resource from host diagnostics and permit only the required supported resource in the managed execution policy, or provide a separately authorized validation environment. Re-run the same probe there before restarting this feature. This agent cannot edit its enclosing policy or approve additional access.
2. **Host has the same EPERM:** investigate the local CLI installation and host denial diagnostics first. Preserve the small stderr reproducer and OS/CLI version for support. Do not presume that changing authentication or installing another version fixes it. Any supported replacement version must repeat the capability checks and have its own fixtures.
3. **Host instead reports an authentication failure:** resolve that distinct failure through the normal `codex login` flow. Current managed evidence does not justify resetting the existing login.

These steps do not require weakening model-tool permissions, moving authentication files, changing the live daemon or editing its runtime state. Raw probe output remains private scratch material; share only reviewed, sanitized diagnostics and evidence.

## What successful initialization unlocks

A successful `ready` response proves startup only. For US-1 AC-1.4, capture real root, parallel and nested executions for **both** providers, then cover the failed-child and model-change scenarios required by downstream stories. Each sanitized fixture needs exact CLI version/source format, invocation and parent identities, source-proven actual models, token dimensions, terminal/coverage evidence and the capture command/topology. Include replay/resume baseline evidence; reconcile exclusive child measurements with available inclusive summaries without adding both. Preserve stable fixture digests and a provenance document; generated narrative is not model/ancestry proof.

Claude's earlier task notifications alone do not prove every child's exclusive cache/input/output breakdown. Select associated-session or event sources only after their real captures establish those dimensions. The same applies to Codex once initialization works.

The approved implementation can then finish US-1's shared contract and normalization, its tests and scoped review, before marking that dependency satisfied. Full subagent support remains a v1 release requirement. None of these recovery observations verifies a story or implements metering.

## Documentation checked

Local `codex exec --help` confirms the flags above and states that ignoring user configuration still uses the existing authentication home. Repository `internal/codex/runner.go` uses the same JSON/config/rules/read-only switches for helper requests. Official OpenAI documentation describes noninteractive execution, saved authentication, JSONL output and ephemeral sessions: [Non-interactive mode](https://learn.chatgpt.com/docs/non-interactive-mode). That documentation does not diagnose this particular EPERM; the proposed host comparison is an investigation step, not a confirmed fix.
