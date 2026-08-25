# T-LF3.0 — De-risking Spike Decisions
# Binding design amendments to T-LF3.1 and T-LF3.2
# Date: 2026-08-23

These are **empirically verified findings**, not suggestions. Where they contradict
`IMPLEMENTATION_PLAN.md`, **these win** — the plan's text was written before the spike ran.
Every engineer touching `internal/harness`, `internal/git`, `internal/launchd` or
`internal/config` must build these in, not merely note them.

---

## SD-1 — Process-group kill is insufficient (amends AC-3.19)

`SysProcAttr{Setpgid: true}` + `kill(-pgid)` **does not reap claude's grandchildren.**
Measured: 2 of 2 survivors reparented to PID 1. Claude's Bash tool `setsid`s its shell
into a **new session and pgid**, so it escapes the parent's process group entirely.

**Required implementation:** enumerate all descendant pgids in the process subtree
(recursive `pgrep -P`), then `kill(-g)` **each** pgid. **Re-scan once** after the first
sweep, because a tool may spawn between enumeration and kill. Keep `Setpgid` and
`WaitDelay` as the outer safety net, but they are not the mechanism.

AC-3.19 as originally written ("killed along with its grandchildren" via Setpgid) **does
not hold**. The AC's *intent* stands and must still be asserted by test — spawn a child
that spawns a sleeper, confirm the sleeper is gone — but the mechanism is the subtree
pgid sweep.

## SD-2 — `--allowedTools` is not a tool boundary (amends AC-3.14, AC-3.15, T-LF3.4)

`--allowedTools` is **additive to the operator's own `~/.claude/settings.json`.** In
testing, 250 ambient allow-rules leaked through and **Bash executed even when it was not
in `--allowedTools`.** Treating it as the sandbox is a security hole.

**Required implementation:**
- Use **`--tools`** to restrict the actual built-in tool surface.
- Pass **`--settings <file>`** pointing at a litefactory-controlled settings file, to stop
  inheriting the operator's personal config.
- The `init` event's `.tools` array reflects **`--tools`**, not `--allowedTools` — so
  AC-3.17's "tool surface from the init event" reads the `--tools` result.

The existing `allowed_tools` config validation (reject bare `Bash`, reject `Bash(*)`)
still applies, but it is now defence in depth rather than the boundary itself.

## SD-3 — Never branch on `.subtype`; handle the no-result stall (amends AC-3.20, S-9)

`.subtype: "success"` appears **even on `is_error: true` runs.** Branch on **`.is_error`**.

There is a **third outcome** beyond success and non-zero exit: **no `result` event at
all.** A bad or stale `ANTHROPIC_API_KEY` causes ~10x internal retries and an indefinite
hang emitting zero result events. This requires a **hard wall-clock timeout** independent
of any event-driven completion detection. S-9 already says "terminates without emitting a
terminal `result` event" is a failure — this is that case, and it is reachable in
practice, not theoretical.

## SD-4 — Env allowlist for the `claude` child (amends the T-LF3.2 security note)

Final allowlist: **`PATH`, `HOME`, `USER`, `SHELL`, `LANG`, `LC_ALL`, `TERM`, `TMPDIR`.**

- **`USER` is REQUIRED.** This machine authenticates claude via the **macOS Keychain**
  (`apiKeySource: "none"`), and without `USER` the Keychain auth silently fails with
  "Not logged in" and exit 1.
- **`SSH_AUTH_SOCK` is deliberately NOT forwarded** to the claude child — claude does not
  need it. (It *is* forwarded to the git child; see SD-5.) This is a tightening of the
  plan, which listed it in the claude allowlist.
- **Do NOT forward any `ANTHROPIC_*` var by default.** Make it opt-in config. Forwarding a
  stale or wrong key **overrides Keychain auth** and produces the SD-3 hang.
- If `ANTHROPIC_*` forwarding is ever enabled it needs an **explicit carve-out** from the
  `TOKEN|SECRET|PASSWORD|PASSWD|KEY|CREDENTIAL|API_KEY` name-pattern strip — currently
  self-cancelling, since `ANTHROPIC_API_KEY` matches the `KEY` pattern. The plan's
  allowlist as written was internally contradictory.
- **Do NOT set `CLAUDE_CONFIG_DIR`.** It breaks Keychain auth even when seeded.

## SD-5 — Env for the `git` child (separate from the claude child)

`PATH`, `HOME`, `SSH_AUTH_SOCK`, `GIT_TERMINAL_PROMPT=0`,
`GIT_SSH_COMMAND="ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new"`.

Verified end-to-end over real SSH from a launchd-started process against a local sshd.

Caveat: setting `HOME` does **not** redirect ssh's `known_hosts`/`config` lookup — OpenSSH
uses `getpwuid`, not `$HOME`. If isolation is ever needed, pass `-o UserKnownHostsFile=`
explicitly.

## SD-6 — `bufio.Scanner` must not be used for the JSONL reader (amends AC-3.16)

The plan said Scanner "silently truncates". It is **worse than that**: Scanner returns
`ErrTooLong` and **aborts the read loop**, silently losing the terminal `result` event if
`s.Err()` is not checked. Measured real lines up to **72,411 bytes** from an ordinary
100 KB file read — routinely over the 64 KB limit.

**Required implementation:** `bufio.Reader.ReadBytes('\n')`. Not a bigger Scanner buffer.

## SD-7 — Exact working `claude` invocation

```
<absolute-path-to-claude> -p --output-format stream-json --verbose \
  --session-id <uuid> --tools <surface> --settings <file>
```

- **`--verbose` is MANDATORY** — hard error without it, zero stdout.
- The binary must be an **absolute path from config**, never `exec.LookPath`. launchd's
  `PATH` is `/usr/bin:/bin:/usr/sbin:/sbin` and will not find Homebrew's claude.

## SD-8 — LaunchAgent in `gui/$UID`, not a LaunchDaemon (amends T-LF2.7)

Keychain access — and therefore claude auth — depends on the user's **GUI security
session**. The plist must be installed as a LaunchAgent bootstrapped into `gui/$UID`.

## SD-9 — SSH to GitHub is deferred (amends T-DOD.1 / AC-D.2) — **user decision**

This machine currently has **no SSH credential for GitHub**: empty ssh-agent, no keys in
`~/.ssh`, `gh` not authenticated, and `origin` is HTTPS rather than SSH.

**User's decision:** build the git-push-over-SSH mechanism per the spike's proven design
(verified against a local sshd), but **defer the real GitHub push test**. Implementation
must not block on provisioning an SSH key.

Consequence for the Definition of Done: the real-GitHub push in `T-DOD.1` / `AC-D.2` is
reclassified from a blocking automated acceptance criterion to a **documented manual
verification step with an environment prerequisite** (an SSH key registered with GitHub).
Automated DoD covers the mechanism against a local sshd fixture.
