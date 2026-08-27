# Research Brief — tool-permissions

**Session:** `.aiteam/main/2026-08-26/tool-permissions/` · branch `main` · 2026-08-26
**Mode:** Research. No code written, nothing committed.

Evidence: a whole-repo tour of the working tree (uncommitted changes included), a
documentation pass on the `claude` CLI permission surface, and direct reads of the
load-bearing files. Where a claim is inferred rather than read, it says so.

---

## Request

> "we need to allow tools for claude sessions in litefactory. right now, all tools seem
> to be blocked by default; i would rather say that we should enable them, but carefully -
> e.g. via tags or so? how could we let a session know what tools are usable? it should
> come from the github somehow!"

---

## Codebase Context

### The premise, checked: refuted as stated, but the frustration is real

Tool permissions are not absent from the tree — they are among the most heavily specified
things in it. Four mechanisms stack, and **all four are static per-daemon**:

| # | Mechanism | Decided at | Default |
|---|---|---|---|
| 1 | `--tools <bare names>` — **the actual boundary** | `internal/harness/runner.go:402`, value from `config.Claude.Tools` | `Read, Glob, Grep, Edit, Write, Bash, Task, TodoWrite` (`internal/config/policy.go:26-28`) |
| 2 | `--settings <file>` | `runner.go:403`; content at `internal/harness/settings.go:69-114` | `defaultMode: "default"`, `allow: []`, `ask: []`, 9 `Read(...)` deny rules |
| 3 | `--allowedTools <rules>` — additive, defence in depth | `runner.go:405-410` | 13 rules (`policy.go:34-50`) |
| 4 | `--strict-mcp-config`, no `--mcp-config` | `runner.go:411` | no MCP server reachable |

The argv, verbatim (`internal/harness/runner.go:388-412`):

```go
func (r *Runner) args(sessionID string, resume bool) []string {
	sessionFlag := "--session-id"
	if resume {
		sessionFlag = "--resume"
	}
	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--verbose",
		sessionFlag, sessionID,
		"--tools", strings.Join(r.tools, ","),
		"--settings", r.settingsPath,
	}
	if len(r.allowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(r.allowedTools, ","))
	}
	return append(args, "--strict-mcp-config")
}
```

There is **no** `--permission-mode`, **no** `--disallowedTools`, and **no** sandbox flag
anywhere in the tree. The permission-skipping flag is deliberately never spelled as a
literal so a repo-wide grep stays a review invariant (`policy.go:52-58`).

**So why does it feel like everything is blocked?** Four concrete gaps in the defaults:

1. **`Task` is in `--tools` but has no allow rule** — compare `policy.go:27` with
   `policy.go:35-49`. `policy.go:25` says Task is there *precisely* because "aiteam's
   agents are subagent invocations". For a repo whose `CLAUDE.md` routes every request
   through an orchestrator sub-agent, a non-functioning Task tool reads as total paralysis.
   **See the top open question — `Task` may not even be the tool's current name.**
2. **`Bash` is qualified to 8 read-only/build commands** (git status/diff/log/show, go
   build/test/vet, gofmt). Everything else — `mkdir`, `rm`, `git add`, `make`, `rg`, `ls`,
   `cat` — has no rule. Intentional: litefactory does the commit and push itself
   (`policy.go:31-33`, `internal/turn/harvest.go:222-231`). Still a large fraction of what
   an agent reaches for.
3. **`WebFetch` and `WebSearch` are not in `--tools` at all** — hard-unavailable, not
   merely unpermitted. Any research-shaped run is crippled by construction.
4. **An unlisted call fails rather than prompts.** `settings.go:77-80` states the design
   intent outright: "A headless run has nobody to prompt, so an unlisted tool call fails
   instead of hanging." Correct, but it makes every gap above silent and terminal.

### The single most important fact for this design

`--allowedTools` **is not a boundary.** Measured during the T-LF3.0 spike
(`.aiteam/main/2026-08-23/lf-setup/SPIKE_LF3.0_DECISIONS.md:28-42`):

> `--allowedTools` is **additive to the operator's own `~/.claude/settings.json`.** In
> testing, 250 ambient allow-rules leaked through and **Bash executed even when it was not
> in `--allowedTools`.** Treating it as the sandbox is a security hole.

This is reachable because `HOME` is forwarded to the child (`internal/harness/env.go:42`).
`internal/harness/settings.go:22-30` restates it: `--settings` loads *additional* settings,
does not replace user/project settings, and does not stop personal agents and skills under
`~/.claude` being discovered. **Only `--tools` reliably narrows what can be called.**

Any design that grants capability by adding `--allowedTools` rules is decorating a boundary
that has already been measured not to hold. Capability must be granted and revoked on
`--tools`.

### Where per-run configuration would have to go

Exactly two things vary per run today, both on `harness.Request`: `ClaudeSessionID`
(`runner.go:200-211`) and `Path RunPath` (`runner.go:216-219`, cold/resume/cold-after-refusal).
Everything else — tools, allowed rules, settings path, timeout, env, binary — lives on the
`Runner` struct (`runner.go:79-89`), built once at daemon boot (`internal/daemon/setup.go:134-142`).

**There is no per-issue configuration channel into the argv at all.** `turn.Runner` is a
one-method interface `Run(ctx, harness.Request)` (`internal/turn/execute.go:24-26`), so
widening `Request` is the natural move and costs no interface change.

### What GitHub already gives us — and what it does not

`issuePayload` (`internal/github/issues.go:144-156`) declares only `Number, Title, Body,
State, User, HTMLURL, CreatedAt, UpdatedAt, PullRequest`. The comment at `issues.go:142-143`
says the rest of a very large object is discarded by the decoder.

- **Labels are already on the wire and silently dropped.** Adding them costs a struct field,
  not an API call. `ListIssues` (`tick.go:190`) and `GetIssue` (`tick.go:268`) both go
  through `issuePayload`, so one change covers both paths.
- **`author_association` is not fetched** (`comments.go:315-323`). Also free to add.
- **Who applied a label is not available from the issue payload at all.** That needs the
  issues timeline/events API — a genuinely new request. *(Inferred from the absence of any
  such call in `internal/github/`; the implemented surface is `ListIssues`, `GetIssue`,
  `ListCommentsAfter`, `ListCommentedIssues`, `PostComment`, `AddCommentReaction`,
  `AuthenticatedUser`.)*

Trigger model: **polling only**, no webhooks (`internal/daemon/tick.go:64`). Only *comments*
trigger runs — an issue body never does (`internal/turn/classify.go:204-206`). And comments
are already **gated to the owner's numeric id** (`classify.go:150`, `classify.go:211`).
**Labels are gated by nothing.**

### The worktree is third-party content

`internal/harness/doc.go:45-55` records this as an accepted residual risk: claude runs inside
the task worktree, so it reads that branch's `CLAUDE.md`, `.claude/settings.json` **and
hooks — and hooks execute.** A branch is third-party content. `--strict-mcp-config` exists
specifically to neutralise the `.mcp.json` half of the same problem.

Consequence for option 3 below: a policy file read from the worktree is a file **anyone who
can open a PR can write**, and it would be authorising its own permissions.

---

## Domain / Problem Space — what the `claude` CLI actually permits

Verified against the docs for **claude 2.1.246** (the version `CLAUDE_CLI_CONTRACT.md:7`
pins). Load-bearing points only.

**Headless default is not "all blocked".** With `--permission-mode default` (what litefactory
uses), read-only tools (Read, Glob, Grep, LSP) auto-allow inside the working directory;
mutating tools (Write, Edit, Bash, NotebookEdit) and network tools (WebFetch, WebSearch) are
denied. A denied call **returns an error to the model and the turn continues** — it does not
hang and does not abort the run. There is also a built-in allowlist of read-only Bash
commands (`ls`, `cat`, `git log`, …) that run without a rule in any mode.

**Precedence.** `deny` always beats `allow`, at every scope. CLI flags override settings files
but **merge with** rather than replace them — `--allowedTools` *adds to* `permissions.allow`.
That is exactly the additivity the T-LF3.0 spike measured. `ask` rules behave as **deny** in
headless, since there is no prompt to answer.

**Allow rules cannot be wildcarded.** Unanchored tool-name globs in an *allow* list (`"*"`,
`"B*"`) are skipped with a warning and grant nothing. Only *deny*/*ask* accept tool-name
globs. So "allow everything" is not expressible via rules — another reason the boundary has
to be `--tools`.

**Bash rule matching is prefix-based and bypassable.** `Bash(npm test)` permits
`npm test && rm -rf /` — matching stops at the first subcommand. Documented caveat. This is
why litefactory's `Bash(go test:*)`-style rules are hygiene, not containment.

**Three levers litefactory does not currently use, and which may fix SD-2:**

- **`--setting-sources user`** (or **`--bare`**) — stops discovery of project/ambient settings,
  hooks, plugins and `CLAUDE.md`. This is the direct answer to the 250 leaked ambient
  allow-rules, and to the "hooks execute" residual risk in `doc.go:45-55`.
- **`--permission-mode dontAsk`** — deny anything not pre-approved, explicitly intended for
  automation, instead of relying on `default` mode's incidental deny-on-no-prompt.
- **`sandbox` settings** (`{"sandbox":{"enabled":true, …}}`) — OS-level filesystem/network
  confinement for Bash, with `denyWrite` paths and `allowedDomains`. This is the only lever
  that makes an allow-listed `Bash` meaningfully less than arbitrary code execution.

These postdate or were unknown at the T-LF3.0 spike. **They are the highest-value finding in
this brief that is not about labels at all**, and they should be weighed independently of the
GitHub-signal question.

**Also available:** `--permission-prompt-tool` (an MCP tool adjudicates each call at runtime —
flexible and auditable, but adds a round-trip per call and denies everything if the server
does not connect within `MCP_TIMEOUT`), and `PreToolUse` hooks (run in headless; exit code 2
hard-blocks). Both are heavier than this problem needs, but they are the escape hatch if
per-call adjudication is ever required.

---

## Constraints & Risks

**C1 — `--tools` is the only real boundary.** Grant on `--tools`; treat `--allowedTools` as
defence in depth. (`SPIKE_LF3.0_DECISIONS.md:28-42`, `settings.go:14-30`)

**C2 — Nothing from GitHub may reach argv as a value.** `runner.go:436-441` carries a
`#nosec G204` justification reading "Nothing else on this argv is caller-supplied". Putting a
label's text on the command line invalidates it. **Design invariant: a GitHub signal is a
lookup key, never a value.** A label selects an operator-authored profile by name; only
config-authored strings are ever joined into argv. This preserves the justification as
written and reuses the existing anchored regexes (`policy.go:17`, `policy.go:21`).

**C3 — Labels are not authenticated.** Comment authorship is gated to the owner's numeric id;
label-setting is gated by nothing, and the issue payload does not even say who applied a
label. On a public repo, anyone with triage permission can set one. **A label must therefore
never be able to widen the tool set beyond a ceiling defined outside GitHub.**

**C4 — A repo-committed policy file is written by the same people it authorises.** Read from
the task worktree it is straightforwardly attacker-controlled (`doc.go:45-55`). Read from the
base branch it is better, but still PR-reviewable-then-merged rather than operator-controlled,
and it needs a git plumbing read that may require an entry in the `internal/git` subcommand
allowlist (`internal/git/exec.go`, cited as S-6 at `prepare.go:270`).

**C5 — Prompt injection is currently an accepted risk, and enabling tools changes its
severity.** The session-resume brief records the owner explicitly deferring injection
hardening. Untrusted issue/comment text is framed in un-forgeable `<note>` blocks
(`prompt.go:328-363`), which is good, but the deferral was priced when the agent could barely
act. Granting `Bash` or `WebFetch` re-prices it: `WebFetch` in particular turns an injected
instruction into an exfiltration channel for anything the agent has read.

**C6 — `~/.litefactory/claude-settings.json` can never be refreshed.** `EnsureSettings`
(`settings.go:140-154`) only creates when missing, and `WriteSettings` has **no non-test
caller** — despite its own doc comment at `settings.go:117-118` claiming `init` and
`daemon run` call it. *I read the deployed file: it currently matches `DefaultSettings`
exactly, so nothing is broken today.* But any future change to `DefaultSettings` will not
reach existing installs. That is a latent defect this feature will trip over.

**C7 — `runner.go` is dirty.** The in-flight session-resume work modifies exactly the edit
sites this feature needs — `Runner.args` and `harness.Request` (plus `internal/turn/prepare.go`
and `harvest.go`). It does not touch the tool flags, and `internal/turn/harness_test.go:275-279`
guards against them being dropped on the resume path. **Sequence this after resume lands, or
expect conflicts.**

**C8 — Resume interacts with a varying tool surface.** `CLAUDE_CLI_CONTRACT.md:58-71` verified
that a resumed session takes its boundary from the *current* argv and does not inherit a wider
one from the transcript — asserted as a security property. So narrowing mid-conversation works.
But the earlier turns of that conversation ran under a wider surface, and their output is still
in context. Whether re-labelling an in-flight issue downward is meaningful is a design call.

**C9 — Failure mode must be fail-closed.** An unknown label, an unparseable profile, or two
mutually contradictory labels must not silently fall back to something permissive. The tree's
precedent is strong here: `--continue` was rejected outright because it fails *open*
(`CLAUDE_CLI_CONTRACT.md:94-99`).

---

## Options

All four assume C2 (label as lookup key, never value) and grant on `--tools` (C1).

### Option A — Tiered labels selecting operator-authored profiles ★ recommended

Named profiles live in `$LF_HOME/config.yaml` under `claude:` (operator-controlled, **not** in
the repo, not writable by anyone on GitHub). A label like `lf:tools=research` names one.
No label → the configured default profile.

- **Blast radius:** `issuePayload` + `github.Issue` + `turn.Issue` + `tick.go:528-538` (labels
  through); `policy.go` (profile type, reusing `validateTools`/`validateAllowedTools`
  verbatim); `config.go:114-146` + `template.go:77-115`; `harness.Request` + `Runner.args` +
  `prepare.go` + `harvest.go:115-129` (the cold-rerun path rebuilds the request and must carry
  the profile). No new API call.
- **For:** ceiling is structurally outside GitHub, so C3 is *designed away* rather than
  mitigated — an untrusted label can only pick from a menu the operator wrote. Small enumerable
  set, validated at daemon startup, easy to render in a report. Profiles are reviewable in one
  place.
- **Against:** operator must edit `config.yaml` to create a new capability tier; a label alone
  can never introduce one. That inverts the naive mental model ("label grants Bash") — but that
  inversion *is* the carefulness the request asks for.

### Option B — Per-tool labels (`lf:tool:bash`, `lf:tool:web`)

Each label adds one tool, union-ed and then intersected with a ceiling.

- **Blast radius:** same as A, minus the profile map, plus union/intersect logic.
- **For:** maximally granular; reads most naturally as "tags".
- **Against:** label sprawl and combinatorial states that are hard to test or reason about;
  a label literally named after a capability invites treating it as a grant; still needs the
  ceiling from A to be safe, so it is A plus complexity. Renders badly in a report.

### Option C — Repo-committed policy file (`.aiteam/tools.yml`)

- **Blast radius:** new read in `prepare.go` (after `ensureWorktree`, ~line 95) or from the
  registered clone (`store.Repo.Path`, `internal/store/repos.go:19`); parse/validate in
  `policy.go`; plus the `harness` changes.
- **For:** versioned, PR-reviewed, travels with the repo, expresses per-repo differences well.
- **Against:** C4. Read from the worktree it is self-authorising and unsafe. Read from the base
  branch it needs git plumbing and possibly an `internal/git` allowlist entry, and it is still
  weaker than operator-local config. It is also the *least* "from GitHub" in the sense the
  request means — it is a file, not a signal on the issue.

### Option D — Directive in the triggering comment (`/spike --tools=research`)

- **Blast radius:** smallest. `internal/turn/command.go:152-176` already parses commands; no
  new fetching at all.
- **For:** **best trust story of the four** — the comment author is *already* gated to the
  owner's numeric id (`classify.go:150`, `classify.go:211`), so authorisation comes free and
  correct. Explicit and per-run.
- **Against:** ephemeral — must be repeated on every comment; not visible at a glance on the
  issue; does not express "this issue is a research issue" durably.

### Recommendation

**Option A, with Option D as a per-run override.**

A gives a durable, glanceable, GitHub-native signal while placing the ceiling somewhere no
GitHub user can reach — which is the only honest answer to C3, given that we cannot even tell
who applied a label without a new API call. D costs almost nothing on top, and inherits the
owner-identity gate that already exists, so it covers the "just for this run, let it search the
web" case without weakening anything.

Two things I would fold in regardless of which option wins, because they improve the boundary
itself rather than who selects it:

1. **Add `--setting-sources user` (or `--bare`) and `--permission-mode dontAsk`.** This is the
   direct fix for the 250 ambient allow-rules measured in SD-2 and for the executing-hooks
   residual risk in `doc.go:45-55`. It would make `--allowedTools` mean what the code already
   pretends it means.
2. **Consider `sandbox` settings before making `Bash` broadly grantable.** Prefix rules do not
   contain `&&` (documented); the sandbox does.

Both need empirical verification against 2.1.246 in the style of `CLAUDE_CLI_CONTRACT.md` —
that doc's five-probe method is the right template and its re-verification checklist
(`:137-148`) should gain a case for whichever flags are adopted.

---

## Observability

Most of the plumbing already exists.

- **`harness.Meta` already carries `Tools []string`** (`internal/harness/meta.go:62-64`),
  populated from the init event (`meta.go:131-133`) — i.e. the **resolved** surface, written
  atomically at 0600 from a `defer` so even an abnormal path leaves one
  (`internal/turn/harvest.go:36-41`). A policy feature wants a **second** field for what
  litefactory *requested*, plus **provenance** (which label, which profile) — because
  requested-vs-resolved divergence is exactly the bug class here (see the `Task` question).
- **`RunResult.Tools`** (`internal/harness/result.go:98-102`) already flows to
  `report.Run.Result` (`internal/turn/report.go:494`) with no new plumbing.
- **The run report has no tools line.** Add a `**Tools:**` bullet to `metadataList`
  (`internal/report/run.go:440-506`), after `Model` (`run.go:485-487`). It lands in `secFixed`,
  which is never truncated (`run.go:169-175`).
- **`litefactory logs <issue>` does not print `meta.Tools`** even though the field is right
  there — one entry at `internal/cli/cmd_logs.go:456`.
- **The `"claude started"` log line logs `tools` but not `allowedTools`**
  (`internal/harness/runner.go:485-492`).
- **The init event is already logged** with `model, session_id, tools, permission_mode,
  api_key_source` (`internal/harness/stream.go:170-175`), and the full event stream is
  persisted verbatim at `$LF_HOME/logs/runs/<forge>/<owner>/<repo>/<issue>/NNNN.jsonl`.
- **Redaction is a non-issue.** `internal/redact` matches credential shapes only
  (`patterns.go:34-93`, `patterns.go:100`); a tool list contains nothing secret-shaped and
  passes through untouched.

**Fail-closed surfacing (C9):** a rejected or unparseable tool request should be reported to
the GitHub thread. The path is `Reporter.ReportReply` (`internal/turn/report.go:337`) — the
same mechanism already used to answer a bare `/spike` or an unknown slash command
(`command.go:152-176`) — so a "label `lf:tools=nope` is not a known profile; run refused"
reply needs no new machinery.

---

## Open Questions

Ordered by how much they change the design.

1. **Is `Task` even a valid tool name in claude 2.1.246 — or is it now `Agent`?** The
   canonical tool list for this version names the subagent tool **`Agent`** (with specifiers
   like `Agent(Explore)`); litefactory configures **`Task`** (`policy.go:27`). If `Task` is not
   recognised, `--tools Task` grants nothing and **sub-agent spawning is silently unavailable**
   — which would, on its own, explain the entire "all tools seem blocked" experience for a repo
   that routes everything through orchestrator sub-agents. *This is a hypothesis, not a
   finding: I did not execute `claude`.* It is also the cheapest thing on this list to settle —
   `grep '"subtype":"init"' $LF_HOME/logs/runs/.../NNNN.jsonl` and read the `tools` array, or
   `litefactory logs <issue>`. **Settle this before any design work; it may be that the real
   bug is a stale tool name and the label feature is a separate, smaller want.**
2. **Should `WebFetch`/`WebSearch` be grantable at all?** They are absent from `--tools`
   entirely today. They are also the sharpest edge of C5 — an injected instruction plus
   `WebFetch` is an exfiltration channel. If research-shaped runs need them, they probably need
   `WebFetch(domain:…)` rules and a hard look at injection.
3. **May a label widen, or only narrow?** The recommendation says a label selects from an
   operator-authored menu (so it can raise capability relative to the *default*, but never
   beyond the *ceiling*). Confirm that is the intended semantics — it is the crux of "carefully".
4. **Who may set a label — accept the gap, or pay for attribution?** Knowing who applied a label
   requires a new timeline/events API call. Under the recommendation this only affects
   downgrade/DoS, not escalation, so accepting the gap is defensible. Confirm.
5. **What happens with no label?** Today's defaults, a named `default` profile, or refuse to
   run? Refusing would change behaviour for every existing issue.
6. **Which Bash command families become grantable, and with or without a sandbox?** Prefix rules
   do not survive `&&`.
7. **Adopt `--setting-sources user` / `--bare` / `dontAsk`?** Independent of labels; fixes a
   measured hole. Needs its own probe.
8. **Resume + changed surface (C8).** Is re-labelling an in-flight issue downward meaningful,
   given earlier turns ran wider and remain in context?
9. **Fix C6 (`WriteSettings` has no caller)?** Reconcile on `daemon run` startup, warn on drift,
   or leave it? Not urgent — the deployed file is currently correct — but it will bite.
10. **Unverified:** the "repository-wide test" enforcing the forbidden-flag grep invariant
    (`runner.go:385-387`) was not located. Worth confirming it exists before relying on it.

---

## Recommended scope for Plan mode

**In scope:**

- Settle open question 1 empirically **first** — it may reframe everything else.
- A per-run tool-policy channel: `harness.Request` gains the fields, `Runner.args` consumes
  them, `prepare.go` populates them, `harvest.go:115-129` carries them through the cold-rerun
  path. Validation at the harness boundary as well as in config, per the tree's convention
  (`runner.go:231-275`).
- Named profiles in `config.yaml` + label→profile resolution, reusing `validateTools` and
  `validateAllowedTools` verbatim. Labels plumbed through `issuePayload` → `github.Issue` →
  `turn.Issue` → `tick.go:528-538`.
- Fail-closed behaviour on unknown/ambiguous/unparseable, surfaced via `ReportReply`.
- Observability: requested set + provenance in `Meta`; `**Tools:**` in the report's `secFixed`;
  `meta.Tools` in `litefactory logs`; `allowed_tools` on the `"claude started"` line.
- Re-verification probes for any newly adopted flag, in the style of `CLAUDE_CLI_CONTRACT.md`,
  with its checklist (`:137-148`) extended.

**Explicitly out of scope:**

- Prompt-injection hardening. Still deferred by the owner — but note C5: this feature *changes
  the price* of that deferral, and Plan should say so rather than inherit it silently.
- `--permission-prompt-tool` and `PreToolUse` hooks. Heavier than this problem needs; record as
  the escape hatch if per-call adjudication is ever required.
- Webhooks, or any change to the polling trigger model.
- Per-tool labels (Option B) and the repo-committed policy file (Option C), unless the
  recommendation is overturned.

**Sequencing:** land or stabilise the in-flight session-resume work first (C7). `Runner.args`
and `harness.Request` are the exact edit sites and are currently uncommitted.

---

*Research is closed. Next mode: **Plan** — but only on a human's say-so, and ideally after
open question 1 has been answered, since it could change what is worth planning.*

---

## Addendum — owner decision, 2026-08-27

Recorded after a review conversation on the brief. This section is a decision
record, not new research; the body above is unchanged.

### Decision: Option A only

**Option A is adopted. Options B, C and D are rejected outright — not deferred.**

This overrides the brief's own recommendation, which paired A with D as a per-run
override. D is out. There is to be no comment-directive channel, no `/tools`
command, and no second resolution path. The label is the only signal.

Why D was dropped, after being explored in detail:

- **GitHub sends no structure.** `commentPayload` (`internal/github/comments.go:315-323`)
  carries `Body string` and nothing more — the raw markdown the author typed. Every
  bit of structure in a comment directive is structure litefactory would have to
  invent by parsing that string. Webhooks would not change this; same body, pushed
  instead of polled.
- **Any syntax is strict, and its failure is silent.** `ParseCommand` inspects only
  the first non-whitespace token (AC-2.18, `command.go:132-134`) — deliberately, so
  that a comment *discussing* a command does not become one. A directive written
  anywhere else is prose: the run proceeds with default tools and the directive
  rides along in the prompt as text. A run that looks like it was asked for
  something and quietly was not is the worst failure mode available here.
- **It buys nothing A does not already have.** Labels are re-read on every tick that
  has work (see below), so relabel-then-comment already covers the "just for this
  one, let it search the web" case that D existed to serve.

B and C are rejected as the brief argues them: B is A plus combinatorial state, and
C is self-authorising when read from the worktree (C4).

### Finding: labels are fresh on every tick, at no API cost

Confirmed by reading the poll path. An issue with new comments reaches the daemon as
a freshly fetched `github.Issue` by one of two routes — `ListIssues` (`tick.go:190`)
for issues whose `updated_at` moved, or `GetIssue` (`tick.go:268`) for issues
reachable only through the comment listing. Both decode through `issuePayload`, so
one struct field covers both. Nothing is cached between ticks: `task.issue` is built
at `tick.go:303` and lives for that tick only.

Consequences, all of which Plan should state as behaviour:

1. **Relabel, then comment, works.** The run that comment triggers reads the labels
   as they stand at that moment. No restart, no re-registration, no extra API call.
2. **A label change alone starts nothing.** Labelling bumps `updated_at`, so the
   issue returns from `ListIssues` on the next tick, but `drain` finds no comment
   above the watermark and does nothing. Only comments trigger runs
   (`classify.go:204-206`). This is correct and should stay.
3. **Granularity is per-tick, not per-comment.** `drain` (`tick.go:373-401`) loops
   over every queued comment against a single `t.issue`. Two comments drained in one
   tick share one label snapshot, even if the label changed between them. **Decision:
   document this rather than fix it** — fixing it costs an issue read per comment.
   The `**Tools:**` line in the run report is what makes it non-mysterious after the
   fact, which raises that line from nice-to-have to required.
4. **A relabel during a run applies to the next run.** Labels are read before the run
   starts; the argv is fixed at spawn.

### Finding: C8 resolves in the widening direction

`CLAUDE_CLI_CONTRACT.md:58-71` already verified that a resumed session takes its
boundary from the current argv and does not inherit from the transcript. So adding a
label five comments into a conversation genuinely widens the next run, with the
conversation intact. Narrowing binds new calls the same way, but earlier turns ran
wider and their output remains in context. That asymmetry is worth a sentence in the
plan, not a mechanism.

### Correction: one fewer edit site than the brief assumed

The brief lists `harvest.go:115-129` as needing to carry the profile through the
cold-rerun path. It does not. `harvest.go:126-129` **mutates `prep.request` in place**
rather than rebuilding it, so new fields on `harness.Request` ride along without a
change. The edit sites are `issuePayload` + `github.Issue` + `turn.Issue` +
`tick.go:528-538`, `policy.go`, `config.go` + `template.go`, `harness.Request` +
`Runner.args`, and `prepare.go:203-212`.

### Open questions after this decision

Moot, because D is gone: **OQ 4** (who may set a label — the ceiling makes it
downgrade/DoS only, and the brief already argues accepting the gap is defensible;
confirmed accepted).

Still open and still blocking Plan:

- **OQ 1** — is `Task` a live tool name in claude 2.1.246, or is it `Agent` now?
  Unanswered. Cheapest item on the list, and it may be the whole of the original
  "everything is blocked" complaint. Settle first.
- **OQ 2** — are `WebFetch`/`WebSearch` grantable at all, and under what rules? This
  is where C5 bites hardest.
- **OQ 3** — may a label widen relative to the default, or only narrow? The crux of
  "carefully".
- **OQ 5** — what happens with no label?
- **OQ 6** — which Bash families become grantable, with or without a sandbox.
- **OQ 7** — adopt `--setting-sources user` / `--bare` / `dontAsk`? Independent of
  labels; fixes a measured hole. Still the highest-value finding in this brief that
  is not about labels.
- **OQ 9** — `WriteSettings` has no non-test caller (C6).
- **OQ 10** — confirm the forbidden-flag repository-wide test exists.

*Research remains closed. Next mode: **Plan**, on a human's say-so.*
