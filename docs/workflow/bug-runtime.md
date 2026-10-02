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

Either agent can take either role. For Claude, list the tools explicitly, within
its configured tool ceiling (`agents.claude.tools`):

```yaml
bug-analyst:
  claude: {tools: [Read, Glob, Grep], allowed_tools: [Read, Glob, Grep]}
bug-fixer:
  claude:
    tools: [Read, Glob, Grep, Edit, Write, Bash, Task]
    allowed_tools: [Read, Glob, Grep, Edit, Write, "Bash(go build:*)", "Bash(go test:*)", "Bash(go vet:*)", "Bash(gofmt:*)"]
```

The analyst only reads. Codex reads through its shell, inside a read-only
sandbox; Claude reads with its file tools and gets no shell, because Claude has
no operating-system sandbox to keep a shell read-only.

The fixer works like an Implement engineer. Besides editing it may grant
`MultiEdit`, `Bash`, `Task`, `SendMessage`, `WebFetch` and `WebSearch`, with
`Bash` narrowed by `allowed_tools` rules such as `Bash(go test:*)`. Codex runs
the fixer in its `workspace-write` sandbox with the shell on. The fixer builds and
tests its own change before handing it back; the factory's check afterwards
stays the final gate. Missing role profiles block the corresponding agent;
existing mode profiles never supply substitute permissions.

Check execution has a separate optional operator ceiling at the configuration
root. Checks run under the Codex sandbox on macOS or Linux:

```yaml
bug_checks:
  binary: /absolute/path/to/codex
  path: /Users/me/.nvm/versions/node/v24/bin:/usr/local/bin:/usr/bin:/bin
  writable_roots: [/Users/me/Library/Caches/go-build, /Users/me/go/pkg/mod]
  deny_read: [/Users/me/.ssh, /Users/me/.config/gh]
  environment: []
  network: true
```

`binary` is the Codex CLI; when it is the npm launcher, `path` must also reach
`node`. `path` lists the directories recipe commands resolve bare names from.
`writable_roots` opens extra directories for writing, typically the toolchain
caches that `agents.codex.writable_roots` opens for the agents. Everything is
readable except `$LF_HOME`, which is always hidden, and `deny_read`.
`network` is off by default, and then checks are fully offline, loopback
included. The sandbox has no loopback-only mode, so projects whose tests start
local servers (for example Go `httptest`) need `network: true`, which opens the
network as for Codex agents with `network_access: true`. `environment` contains variable names whose values are resolved
from the operator process environment at startup. Values are never written
inline in configuration. Recipes may request only those names; protected
provider, forge and result-service credentials and environment overrides remain
forbidden even when named here. The retained configuration revision hashes the
effective bindings without exposing their values.

A missing configuration or an unavailable sandbox launcher blocks checks before
an unisolated command can run. It does not prevent analysis or change the
requirements for recorded passing evidence. Project command authority remains
the exact target commit's `.litefactory/bug-checks.yaml`; see
[check recipes](../bug-checks.md).

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
