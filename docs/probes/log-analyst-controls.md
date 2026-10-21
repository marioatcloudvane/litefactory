# Log analyst provider control probe

On 2026-10-21, read-only `claude --version` and `claude --help` probes against
an installed binary reported **2.1.306 (Claude Code)**. Both commands ran with a
fresh temporary HOME and TMPDIR, an allowlisted PATH, and no model invocation.

The help output advertised every control required by the log analyst adapter:
`--tools`, `--safe-mode`, `--restricted`, `--disable-slash-commands`,
`--strict-mcp-config`, `--setting-sources`, `--system-prompt`, `--settings`,
`--agents`, `--no-chrome` and `--no-session-persistence`.

To repeat, resolve the configured Claude binary, create a private temporary
directory, and invoke `--version` and `--help` with only PATH, HOME and TMPDIR in
the environment, pointing HOME and TMPDIR at that directory. Remove the directory
afterward. Compare the complete flag tokens, rather than similar flag prefixes.

This verifies the installed version and advertised switches only. It does not
establish authentic model execution, cancellation, or tool isolation. The launch
adapter separately refuses an unexpected initialization catalog; automated
provider scripts exercise that refusal and terminal/cancellation behavior as
fixtures. No authentic analyst model invocation was performed for this record.
