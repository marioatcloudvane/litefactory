# Bug runtime configuration

Bug remains a standalone workflow selected by the `bug` label when no explicit
mode takes precedence. Its analyst and fixer use independent operator grants.
Add these entries to `tool_profiles.permissions` in operator configuration:

```yaml
bug-analyst:
  codex: {sandbox: read-only}
bug-fixer:
  codex: {sandbox: workspace-write}
```

Codex must also be configured as an agent. Claude can investigate with an explicit
`bug-analyst.claude` profile containing only `Read`, `Glob` and `Grep`, constrained
by its configured tool ceiling. Claude fixer execution is currently unsupported
and refused. Missing role profiles block the corresponding agent; existing mode
profiles never supply substitute permissions. Fixer shell and delegation remain
disabled by the adapter, independently of these grants.

Check execution has a separate optional operator ceiling at the configuration
root. The current backend requires Linux with bubblewrap and usable namespaces:

```yaml
bug_checks:
  binary: /usr/bin/bwrap
  read_only_roots: [/usr, /bin, /lib, /lib64]
  path: /usr/bin:/bin
  environment: []
```

Select only installed runtime system trees and the required executable paths.
`environment` contains variable names whose values are resolved from the operator
process environment at startup. Values are never written inline in configuration.
Recipes may request only those names; protected provider, forge and result-service
credentials and environment overrides remain forbidden even when named here.
The retained configuration revision hashes the effective bindings without exposing
their values. No network or shared-cache grant is added by this profile.

Missing configuration, unsupported hosts (including macOS for this backend), and
denied namespaces block checks before an unisolated command can run. They do not
prevent analysis or change the requirements for recorded passing evidence.
Project command authority remains the exact target commit's
`.litefactory/bug-checks.yaml`; see [check recipes](../bug-checks.md).

The parent runtime owns Bug reports, review waits and reply consumption. The daemon
dispatches its persisted parent and child identities without a mode surrogate.
Observed issue closure still permits owed parent confirmation and reporting;
it does not authorize new repair or replace either owner approval.

After a blocked infrastructure check has been resolved, the owner can reply
`/retry-bug-tests <callid> <resolution>` with the recorded child call identity and
a description of the resolution. The factory verifies the recorded blocked call
and completed cleanup before admitting a fresh test generation. Known flaky
results cannot use this path. It spends no repair reservation and supplies no
delivery approval; all required checks must still pass.
