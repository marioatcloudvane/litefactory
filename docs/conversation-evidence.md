# Persistent Discord conversation evidence

Evidence was collected on 2026-10-13 without contacting Discord or invoking a model. The factory database, registered repositories, operator configuration and provider credentials were not inspected or changed.

## Installed capabilities

Read-only `--version` and `--help` checks found Claude Code **2.1.296** and Codex CLI **0.160.0**.

Claude help advertises exact `--resume`, persistent sessions by default, explicit empty `--tools`, `--strict-mcp-config`, `--setting-sources`, disabled skills, restricted mode and safe mode. Codex help advertises explicit `exec resume`, persisted sessions unless `--ephemeral` is selected, private configuration, ignored user configuration and rules, strict configuration and profiles.

Help output establishes advertised syntax. It does **not** verify enforcement, native context retention, sandbox filesystem denial, tool denial or authentication isolation. Neither executable was launched for a model turn. These versions alone do not establish the launch contract. The Claude adapter requires advertised controls, including safe mode to disable ambient customizations, then refuses an initialization event unless it explicitly reports a version and empty native-tool and MCP catalogs. The probe and init versions are recorded without requiring an exact patch version (see the #848 regression below). The Codex adapter runs a harmless filesystem probe under its named deny-root profile before a model turn, requiring a forbidden private-home read and write to fail and a private-scratch write to succeed. Unsupported probes or missing controls refuse launch. The production adapter gates were not executed against the installed binaries.
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

The main-path test uses the production Discord intake, conversation service, provider adapters, durable store and outbox. Only the provider executable and Discord sender are fixtures. Each fixture process exits after one turn and retains a harmless remembered fact only in its private session files. After the first delivered answer, the recorded provider emits a native compaction event and replaces its earlier fact file with a summary under the same handle. The test then closes and reopens the database and recreates both the service and provider adapter. A follow-up without the fact must resume the exact saved provider handle and return the remembered fact from that summary. This is exercised for both providers.

The executable fixture verifies the resume argument and private state survive process recreation. It does not demonstrate a real model remembering a fact or establish installed-provider isolation.

## Native demonstrations still required

These demonstrations are owner checks in Validate on the running daemon, with provider authentication outside model-accessible paths. Use disposable conversation state and harmless sentinels while preserving existing conversations. Workers do not perform these checks. No native demonstration is claimed by this record.

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

Native demonstrations belong to owner checks in Validate on the running daemon. Workers do not launch real provider sessions from their sandbox. Tests of fixtures cannot replace those live observations in acceptance review.

## Repair attempt 0008

The prepared worktree is at `f1ec3cd8bf71c767e0f04f9a57d11a3e8fc1cc83`, which contains the reviewed implementation commit `f6c495db360fc94a5371229d8aada250493d0d98`. The conversation adapters, service, restart fixture and turn persistence are unchanged between those commits. Three delegated investigations examined scratch-path handling, installed-provider capabilities and evidence retention; none changed production code.

A fresh credential-free Codex probe used the adapter's deny-root profile, minimal runtime reads, private scratch writes and disabled command networking. Its sentinel command would require a forbidden private-home read and write to fail and a scratch write to succeed. With fresh temporary `HOME` and `CODEX_HOME`, it exited **71** with `sandbox-exec: sandbox_apply: Operation not permitted` before executing the command. No sentinel outcome was verified. Operator authentication and configuration were not read, no model turn ran, and the temporary probe files were removed. Claude still advertises the pinned controls; native enforcement and continuity remain unverified for both providers.

The returned suite failure concerns scratch worktree canonicalization in existing validation infrastructure. `filepath.EvalSymlinks` refuses a permission-denied ancestor before base synchronization reaches conflict resolution. Source inspection found that validation scratch lives below the factory home, while the suite's filesystem profile denies that home and separately permits scratch descendants. This is a possible explanation, not an established diagnosis: the retained full suite log and generated sandbox rules were not supplied to this attempt. Ignoring permission errors or granting broader factory-home reads would weaken isolation and was not attempted. No validation infrastructure was changed. The Implement role permits affected-package checks, not the full repository suite; those checks cannot certify recovery of the original sandboxed suite execution.

Fresh complete tests passed for all **38 affected packages**, including reverse production and test importers. Commands used `go test -count=1 -race -timeout=0` for 34 packages and `go test -count=1 -timeout=0` for `internal/cli`, `internal/daemon`, `internal/store` and `internal/turn`, with `GOFLAGS=-trimpath` and canonical `TMPDIR=/private/tmp/lf776-run0008/scratch`. The complete messaging run includes the two-provider restart fixture. The complete turn run passed in 938.814 seconds and includes the reported concrete-conflict and budget scenarios. This local execution does not reproduce Validate's separate full-suite sandbox. Pinned golangci-lint 2.13.1 reported zero issues for those packages; initial optional linter cache writes emitted sandbox-denial warnings, while the additional importer check used a writable temporary cache. Story Go files are gofmt-clean and `git diff --check` passed.

The shared aggregate-capture preflight rejected these new facts with `store: aggregate requires exact Implement binding and bounded evidence`. No capture was submitted or retained, and no passing retained check ID is claimed. The attempted manifest contained 34 complete story files and two fresh checks (successful affected-package verification and failed installed sandbox feasibility); it did not reuse an earlier check identity. Engineering verification is therefore reported here and in the result, separately from retained aggregate evidence and acceptance.

## Worker verification boundary (attempt 0009)

The supplied owner guidance assigns live Claude and Codex demonstrations to owner checks in Validate on the running daemon. Workers must not launch real provider sessions or nested native sandboxes. This attempt uses fake or recorded provider runners exclusively; the inability to start a nested macOS Codex sandbox is no longer treated as a worker engineering blocker. The approved prompt-only access boundary and production capability gates remain unchanged.

Recorded-provider tests establish the factory's behavior against explicit provider protocols: exact saved identity, retained private state after compaction and restart, unfinished-turn recovery without replay, missing-session refusal without a replacement, and isolated launch controls with visible refusal for unsupported capabilities. They do not establish real-model recall or certify an installed provider's filesystem, tool or network enforcement. Those live observations remain unverified until the owner performs them in Validate.

Attempt 0009 complete verification passed for **29 affected packages**, covering the changed packages and their production/test importers: 25 packages with `go test -count=1 -race -timeout=0`, and `internal/cli`, `internal/daemon`, `internal/store` and `internal/turn` with `go test -count=1 -timeout=0`. The complete messaging run passed in 845.313 seconds; the complete turn run passed in 1991.186 seconds. Commands used `GOFLAGS=-trimpath` and canonical temporary fixture storage. `go build ./...` passed; pinned golangci-lint 2.13.1 reported zero issues for the three changed packages; the five changed Go files are gofmt-clean. Raw scoped command logs and their source hashes are retained in the supplied session artifact directory under `run0009-checks`. These are worker execution records, not retained suite-command IDs or live-provider acceptance. No full suite, real provider session or nested native sandbox ran in this attempt.

## Patch-version admission regression (#848)

Recorded Claude fixtures now exercise 2.1.299 for capability probing and stream init.
Admission depends on the required flags and runtime empty tools/MCP catalogs, not
an exact patch version. Probe and init numeric versions are logged. These fixtures
establish adapter behavior, not native CLI certification. Historical observations
above remain observations of their recorded versions.
