# Persistent Discord conversation evidence

Evidence was collected on 2026-10-13 without contacting Discord or invoking a model. The factory database, registered repositories, operator configuration and provider credentials were not inspected or changed.

## Installed capabilities

Read-only `--version` and `--help` checks found Claude Code **2.1.296** and Codex CLI **0.160.0**.

Claude help advertises exact `--resume`, persistent sessions by default, explicit empty `--tools`, `--strict-mcp-config`, `--setting-sources`, disabled skills, restricted mode and safe mode. Codex help advertises explicit `exec resume`, persisted sessions unless `--ephemeral` is selected, private configuration, ignored user configuration and rules, strict configuration and profiles.

Help output establishes advertised syntax. It does **not** verify enforcement, native context retention, sandbox filesystem denial, tool denial or authentication isolation. Neither executable was launched for a model turn. These versions alone do not establish the launch contract. The Claude adapter separately requires its pinned CLI version and advertised controls, including safe mode to disable ambient customizations, then refuses an initialization event unless it explicitly reports that same version and empty native-tool and MCP catalogs. The Codex adapter runs a harmless filesystem probe under its named deny-root profile before a model turn, requiring a forbidden private-home read and write to fail and a private-scratch write to succeed. Unsupported probes or missing controls refuse launch. The production adapter gates were not executed against the installed binaries.
Codex's `default_permissions` selects a matching named permissions table, as
specified by its [configuration reference](https://learn.chatgpt.com/docs/config-file/config-reference).
That documented contract does not certify this installation's enforcement.

An exploratory native Codex sandbox probe used:

```sh
codex sandbox -c 'permissions={conversation={filesystem={":root"="deny",":minimal"="read"},network={enabled=false}}}' -P conversation -- /bin/echo verified
```

The corrected inline profile reached OS sandbox setup but exited **71** with
`sandbox-exec: sandbox_apply: Operation not permitted`. The harmless command did
not run successfully. This establishes that this managed environment refused
that native sandbox launch; it establishes no successful filesystem or network
isolation. It was an exploratory CLI probe, not the adapter's sentinel gate.

## Automated main path

The main-path test uses the production Discord intake, conversation service, provider adapters, durable store and outbox. Only the provider executable and Discord sender are fixtures. Each fixture process exits after one turn and retains a harmless remembered fact only in its private session files. After the first delivered answer, the test closes and reopens the database and recreates both the service and provider adapter. A follow-up without the fact must resume the exact saved provider handle and return the remembered fact. This is exercised for both providers.

The executable fixture verifies the resume argument and private state survive process recreation. It does not demonstrate a real model remembering a fact or establish installed-provider isolation.

## Native demonstrations still required

These demonstrations need a dedicated test installation with verified isolation and provider authentication outside model-accessible paths. No production Discord channel or factory database should be used. No native demonstration is claimed by this record.

| Check | Claude | Codex | Evidence to retain |
|---|---|---|---|
| Restart and exact resume | Not demonstrated | Not demonstrated | Initial and resumed native handles; context-dependent reply after process and factory restart |
| Native compaction | Not demonstrated | Not demonstrated | Same handle before and after native compaction; useful retained fact on follow-up |
| Interrupted turn | Not demonstrated | Not demonstrated | Cancellation/process exit; saved binding; ordered unfinished-turn notice; next input resumes without replaying uncertain input |
| Missing session | Not demonstrated | Not demonstrated | Removed disposable native state; visible continuity error; no cold replacement |
| Isolation | Not verified natively | Exploratory sandbox launch refused (exit 71); not verified | Harmless repository, factory, sibling-session and credential sentinels; denied tools/files/network; empty external-tool catalog |
| Delivery recovery | Fixture coverage only | Fixture coverage only | Saved reply bytes and nonce; no additional provider turn; earlier reply dispatch before later turn |
| Routing compatibility | Code review/test coverage only | Code review/test coverage only | Separate channel handles, rename retention, drained provider switch, unauthorized rejection and unchanged slash/confirmation paths |

For delivery recovery, lose acknowledgment after a disposable reply, restart, and retry the same saved bytes and nonce before admitting the next turn. Discord may duplicate replies beyond its short nonce window; local dispatch order is the guarantee. Temporary throttling should leave other channels runnable. Permanent refusal and revoked authorization must preserve pending state and show a health reason.

Native demonstrations are unexecuted because installed-provider isolation has not been demonstrated in this run and no dedicated external test endpoint was supplied. Tests of fixtures cannot replace those demonstrations in acceptance review.
