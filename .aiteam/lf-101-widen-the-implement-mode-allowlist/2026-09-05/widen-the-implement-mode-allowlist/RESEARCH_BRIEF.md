# Research Brief — widen-the-implement-mode-allowlist

## Request

Issue #101, "Widen the implement-mode allowlist: headless runs make every gap a hard
denial (386 across 119 runs)". Asks to:

1. Widen `tool_profiles.permissions.implement.claude.allowed_tools` in the canonical
   default config.yaml.
2. Widen the matching `defaultAllowedTools()` in `internal/config/policy.go` so that
   `litefactory init` writes the same widened list into a freshly generated
   config.yaml (keep the two in sync).
3. Keep unchanged: the git-write ban, the secret-path deny rules, and
   `--strict-mcp-config`.
4. Correct a comment claiming `allowed_tools` is "defence in depth, NOT the
   boundary" — the issue argues that in headless/asyncAgent runs it *is* the hard
   boundary, because there is no human present to fall back on for an interactive
   permission prompt.
5. Provide a writable scratch directory for implement runs.

This run followed a Plan-mode attempt that was correctly gate-blocked because
`RESEARCH_BRIEF.md` did not yet exist (see `MANIFEST.md` history, 2026-09-05).

No web research was performed for the *first* pass of this brief: every question
raised by the issue at that point resolved inside this repository. This
follow-up pass **did** attempt one `web-researcher` call (see New Evidence below)
and it returned "no web access on this run" — the issue does not currently carry
the `lf:tools=web` label. That question is now recorded as an explicit Open
Question rather than answered from memory.

## New Evidence (added this run — run 6 of #89, reported by the issue owner)

The issue owner added a concrete example from a real `implement`-mode run (run 6
of #89, timed out at the 90-minute wall) as supporting evidence for AC-3
(workspace mutation) and AC-5 (scratch directory). **[O]** (owner-reported,
not independently re-derived from raw logs by this session — the underlying run
artifacts live outside this repository):

- A `go-implementation-engineer` created a scratch verification file
  (`internal/cli/zzverify_slog_test.go`) to compare `renderSlogLine`'s output
  against a real `slog.TextHandler`. Once done with it, it tried to delete the
  file and was refused **8 times across 5 escalating strategies**: plain `rm`
  (x5), `mv`, `rm` inside a compound command, `python3 -c "os.remove(...)"`, and
  finally a subshell/brace-expansion attempt — all blocked, the last two
  specifically flagged as "requires approval" and "expansion obfuscation"
  respectively.
- The engineer gave up and **emptied the file instead of deleting it**. That
  emptied file was committed to `lf/89-logging-is-only-on-issues-but-not-on-the`
  in commit `a963729`, with a code comment explaining the sandbox restriction —
  i.e., **the denial gap did not just cost turns, it produced a concrete,
  committed artifact** (a dead test file with an apology comment) that a human
  reviewer now has to notice and clean up. This is a new class of consequence
  beyond wasted time: a workaround chosen under repeated denial pressure left a
  permanent trace in the repository.
- The same run separately hit 5 `Read` denials (`decision_reason_type: rule`)
  on paths under the agent's own scratch directory,
  `/private/tmp/claude-501/.../tasks/...` — including reading a file the agent
  had itself just written — plus `tail -100` / `tail -c 3000` denials on the
  same path via `Bash`. This reinforces AC-5 (scratch directory) with a second,
  independent failure mode: even where a writable temp area exists (Claude
  Code's own per-task scratch path), the agent cannot reliably *read back* its
  own writes there.
- Run 6 totals: 36 denials in 90 minutes — Bash 30, Read 5, 1 unattributed; by
  reason: `other` 13, `subcommandResults` 12, `rule` 5.
- The issue owner's suggested shape for AC-3 (deliberately narrow): allow `rm`
  and `mv` scoped to the run's worktree path (e.g.
  `~/.lf-worktrees/litefactory/task-<n>`), not unqualified; keep the existing
  secret-path deny rules and the git-write ban exactly as they are.

This session ran a targeted `codebase-tour` follow-up specifically to check
whether that suggested shape ("scope rm/mv to the worktree path") is a narrow
value change or new design work. It is new design work, on two independent
fronts — see the new Codebase Context findings and Open Questions #6 below.

## Codebase Context

**[V]** Package shape (from `AGENTS.md:117-135`, confirmed by `codebase-tour`):
`internal/config` is a stdlib-only leaf package (enforced by
`internal/config/purity_test.go`); `internal/harness` executes Claude Code and is a
downstream consumer of `internal/config`'s output. The two packages this issue
touches — `internal/config` (the policy/defaults) and `internal/harness` (the
runtime settings and CLI invocation) — sit in that dependency order, so widening
`internal/config` cannot introduce a cycle.

**[V]** Default-construction chain, config side:
- `internal/config/config.go:409-437` — `DefaultToolProfiles()` builds the
  four-mode permission matrix (`research`, `plan`, `implement`, `validate`/spike).
  Line 411: `implementAllowed := defaultAllowedTools()` — **implement's
  `allowed_tools` and the function the issue names are the same call site**, so
  there is exactly one place to widen for the Go-side default.
- `internal/config/policy.go:63-65` — `defaultTools()`, the `--tools` *ceiling*
  (ships as `{Read, Glob, Grep, Edit, Write, Bash, Task}`). This is the true hard
  surface restriction and is **not** what the issue asks to widen — care must be
  taken in Plan/Implement not to touch this function by mistake.
- `internal/config/policy.go:67-136` — `defaultAllowedTools()`, the function the
  issue names directly, quoted in full:
  ```go
  func defaultAllowedTools() []string {
      return []string{
          "Read", "Glob", "Grep", "Edit", "Write",
          "Bash(git status:*)", "Bash(git diff:*)", "Bash(git log:*)", "Bash(git show:*)",
          "Bash(git rev-parse:*)", "Bash(git ls-tree:*)", "Bash(git merge-base:*)", "Bash(git rev-list:*)",
          "Bash(mkdir:*)",
          "Bash(go build:*)", "Bash(go test:*)", "Bash(go vet:*)", "Bash(gofmt:*)",
          "Bash(grep:*)", "Bash(sed -n:*)", "Bash(head:*)", "Bash(tail:*)", "Bash(wc:*)",
      }
  }
  ```
  No `defaultDeniedTools()` (or similarly named) sibling exists anywhere in the
  package — confirmed by search. There is no `Bash(git commit...)`, `push`, `add`,
  or `stash` entry in this or any profile: **the git-write ban is enforced purely
  by omission**, not by an explicit deny rule.
- Comparison profiles (for scoping — see Open Questions #7):
  - `research`/`plan` (`artifactTools`/`artifactAllowed`): tools =
    `{Read,Glob,Grep,Write,Bash,Task}`; allowed = `Read,Glob,Grep,Write` + the same
    4 read-only git verbs + `mkdir` — **no** `go build/test/vet/gofmt` and **no**
    `grep/sed/head/tail/wc`. Narrower than implement's list today.
  - `validate` (spike reuses `implement`'s profile at runtime — see request
    context) (`readTools`/`readAllowed`): `{Read,Glob,Grep,Task}` /
    `{Read,Glob,Grep}` — strictly read-only.
  - Sandbox mode (Codex side): research/plan/implement = `"workspace-write"`,
    validate = `"read-only"`.
  - Attachments default: research/plan = `true`, implement/validate = `false`
    (`config.go:432-435`).

**[V]** `internal/config/template.go:13-106` — `DefaultFileContents()` renders the
same in-memory defaults to the YAML text that `litefactory init` writes to disk.
An anti-drift test (`TestDefaultFileMatchesDefaults`) decodes that rendered file
and diffs it against the Go constructors, so as long as both the config.yaml
template comment/content and `defaultAllowedTools()` are edited together, they
cannot silently diverge — this materially de-risks ask #2 (init parity).

**[V]** Runtime enforcement, harness side:
- `internal/harness/runner.go:408-443` — `(r *Runner) args()` builds the actual
  Claude Code CLI invocation: `--tools <profile.Claude.Tools>`, then (if
  non-empty) `--allowedTools <profile.Claude.AllowedTools>`, then `--settings
  <litefactory-owned file>`, then **unconditionally** `--strict-mcp-config`
  (line 442). This flag is not parameterized by profile at all today, so ask #3's
  requirement to leave it untouched is automatically satisfied by the current
  structure — there is nothing to accidentally regress here as long as
  `runner.go:442` is not edited.
- `internal/harness/settings.go:69-114` — `DefaultSettings(home)` builds the
  separate Claude-CLI-native settings JSON, independent of `tool_profiles`.
  `DefaultMode: "default"` (line 80) is the literal mechanism that turns an
  unlisted tool call into a hard denial rather than an interactive prompt when no
  human is present — this is the mechanism the issue's title refers to.
- `internal/harness/settings.go:83-96` — the secret-path deny list, quoted in
  full:
  ```go
  Deny: []string{
      "Read(//" + filepath.ToSlash(filepath.Clean(home)) + "/**)",
      "Read(~/.ssh/**)", "Read(~/.aws/**)", "Read(~/.gnupg/**)",
      "Read(~/.config/gh/**)", "Read(~/.claude/**)", "Read(~/.netrc)",
      "Read(./.env)", "Read(./.env.*)",
  },
  ```
  This is a **separate mechanism** from `allowed_tools` entirely — it is a runtime
  path-deny list independent of the tool-name allowlist, so widening
  `defaultAllowedTools()` cannot touch it. (Distinct again from
  `internal/config/secretscan.go`, which scans config.yaml's own decoded values
  for credential-shaped literals at load time — a third, unrelated mechanism.)
- `internal/harness/settings.go:97` — `AdditionalDirectories: []string{}` is
  always empty. This is the **only** existing structural hook toward a writable
  scratch directory (ask #5), and nothing currently populates it. No other
  scratch/temp-dir concept for implement runs exists beyond the git worktree
  itself — see Open Questions #5.

**[V] New this run — Bash rule syntax has no path-scoping precedent anywhere in
this codebase** (targeted `codebase-tour` follow-up, run specifically to check
feasibility of the issue owner's "scope rm/mv to the worktree path" suggestion):
- `internal/config/policy.go:42-44` — `toolRuleRE`, the regex that parses a rule
  string, is `^([A-Za-z][A-Za-z0-9_-]*)(?:\((.*)\))?$`: a tool name plus an
  opaque free-text qualifier. The qualifier's meaning is entirely delegated to
  Claude Code's own permission engine — nothing in this repo interprets or
  validates a path component inside it.
- Every existing `Bash(...)` entry in `defaultAllowedTools()`
  (`policy.go:71-136`) — all 17 of them — is a shell-command-prefix pattern
  (`Bash(git status:*)`, `Bash(mkdir:*)`, `Bash(tail:*)`, etc.). None contains a
  filesystem path. `validateAllowedTools`/`isAllowAllQualifier`
  (`policy.go:180-220`) validate only command-prefix shape (`*`, empty, `*:*`);
  there is no code path anywhere that recognizes a path component in a `Bash`
  qualifier.
- By contrast, `Read(...)` rules in the Deny list (`settings.go:83-96`) genuinely
  are path-scoped (`Read(~/.ssh/**)`, `Read(./.env)`). **That idiom is applied
  only to `Read` in this codebase, never to `Bash`**, confirmed by a full-repo
  grep across production and test code
  (`internal/turn/tools_test.go:144`, `internal/harness/runner_test.go:1287,1305,1433,1436`,
  `internal/config/toolprofiles_test.go:46`, none path-scoped).
- **Whether Claude Code's own settings schema even supports a path-scoped `Bash`
  qualifier at all is unestablished** — nothing in this repo's parsing/validation
  code either permits or forbids it (the qualifier is opaque), and this is a
  question about Claude Code's product surface, not this codebase. A
  `web-researcher` call was attempted this run specifically to settle it and
  returned "no web access on this run — the issue needs the `lf:tools=web`
  label" (see Open Questions #6).

**[V] New this run — the worktree path is not currently plumbed into settings
construction**, which is the second independent obstacle to "scope rm/mv to the
worktree path":
- `internal/harness/settings.go:69,122,140` — `DefaultSettings(home)`,
  `WriteSettings(path, home)`, and `EnsureSettings(path, home)` all take only
  `home` ($LF_HOME) as a parameter; none takes a worktree path.
- The worktree path *is* a known, validated value in scope at the same point in
  the call chain: `internal/harness/runner.go:274-282` validates
  `req.WorktreePath` is absolute and exists, `runner.go:330` calls
  `EnsureSettings(r.settingsPath, r.home)` in the same `Run()` function body
  (without threading `req.WorktreePath` through), and `runner.go:509` later sets
  `cmd.Dir = req.WorktreePath`. So plumbing the value through is a signature
  change, not a from-scratch discovery of where it lives.
- `internal/git/worktree.go:33-38,78-80` — `Worktree.Path()` confirms the
  worktree path is already a first-class, computed value elsewhere in the
  system by the time a run reaches the harness.
- **Constraint that makes this non-trivial**: `EnsureSettings`'s own doc comment
  (`settings.go:133-139`) states it is write-once-if-missing, shared across all
  runs against one `$LF_HOME`. Different runs/tasks have different worktree
  paths (this very session runs from
  `/Users/mariomh/.lf-worktrees/litefactory/task-101`, a different path than any
  other run's worktree). A single cached settings.json cannot statically embed
  *the* worktree path for every subsequent run — baking a worktree path into
  settings would mean either regenerating settings every run (a change to
  `EnsureSettings`'s idempotency contract, moving it closer to `WriteSettings`'s
  always-overwrite semantics) or a different mechanism entirely (e.g.
  `AdditionalDirectories`, still hard-coded empty at `settings.go:60,97`, with no
  documented intended use anywhere in the repo outside this brief).
- No reference anywhere in `.go` sources, `AGENTS.md`, or `doc.go` files
  describes how/where a worktree path string is computed at harness-settings
  time; the closest hit, `AGENTS.md:154`, only states that
  slug/branch/worktree-path concerns belong outside `config.Paths`, without
  describing the mechanism.

**[V] New this run — Claude Code's own scratch/tmp directory (the second half of
the new evidence, the `/private/tmp/claude-<uid>/.../tasks/...` Read/tail
denials) has no representation anywhere in this codebase at all.** It is not
computed, named, or referenced by litefactory. The only adjacent fact: `TMPDIR`
is one of the fixed environment variables litefactory forwards unmodified to the
child process (`internal/harness/env.go:29,48`, `internal/identity/env.go:30,48`)
— Claude Code presumably derives its own scratch path from that forwarded
`TMPDIR` (or its own internal logic) entirely on its own side, at a point
invisible to and uncontrolled by litefactory's settings construction. **This
means the scratch-directory Read/tail denials cannot be fixed by the same
mechanism as the worktree-path-scoping idea above** — it is a materially
different problem needing its own mechanism (e.g. not denying that
`TMPDIR`-rooted subtree at all, if such a rule can even be expressed), not a
sub-case of AC-3. See Open Questions #5.

**[V]** The "defence in depth, NOT the boundary" comment (ask #4) appears in more
places than the issue's literal wording suggests:
- `internal/config/template.go:61-64` (rendered into every generated config.yaml
  — the literal target the issue names):
  > `# tools is the hard --tools boundary. allowed_tools becomes`
  > `# --allowedTools and is defence in depth, NOT the boundary. --settings`
  > `# prevents ambient permissions from the operator's personal settings.`
  > `# An omitted tool_ceiling defaults to exactly tools.`
- `internal/config/config.go:222-227` — the Go-side struct doc this comment is
  restating, with its empirical justification (SD-2): *"Measured (SD-2):
  --allowedTools is additive to the operator's own settings — 250 ambient
  allow-rules leaked through and Bash executed while absent from --allowedTools.
  It is therefore defence in depth, not the sandbox. It is still policy-checked…"*
- `internal/config/config.go:250-252` — a shorter field-doc echo of the same
  claim.
- `internal/harness/doc.go:37-47` — the fullest prose version of the SD-2 spike
  rationale (250 ambient allow-rules leaked, Bash executed while absent from
  `--allowedTools`).
- `internal/harness/settings.go:28-30` — a fourth, textually similar "defence in
  depth" comment, but about the **Deny list**, not `allowed_tools` — this one
  should **not** be touched by a fix scoped to the `allowed_tools` framing; it is
  making a different (and still accurate) claim about the deny list's own
  brittleness.
- `internal/turn/execute.go:445,464` also contain the string "defence in depth"
  but describe an unrelated store-lock/concurrency mechanism — confirmed
  false-positive, out of scope.

**[V]** A literal-string test pins the current wording:
`internal/config/template_test.go:157-164`,
`TestDefaultFileDocumentsTheTwoToolKeys`, asserts the rendered config file
contains `"--tools"`, `"--allowedTools"`, `"--settings"`, **and `"NOT the
boundary"`**. This test will fail — correctly, by design — the moment the wording
is corrected, and must be updated in the same change, not treated as a
regression.

**[V]** No test currently pins the exact contents of `implement`'s (or
`research`'s/`plan`'s) `allowed_tools` list.
`internal/config/toolprofiles_test.go`'s `TestValidateProfileIsExactlyReadOnly`
(lines 21-35) pins only `validate`'s exact lists.
`TestCanonicalPermissionMatrix` (68-88) asserts only structural properties (all 4
profiles present, `validate` excludes Bash/Write/Edit). Practically: widening
`defaultAllowedTools()` risks no exact-match test breakage today, but the
anti-drift tests (`TestDefaultFileMatchesDefaults`, `TestDefaultFileValidates`)
must keep passing — they should, automatically, since template and Go-side
defaults derive from the same function.

**[V]** `forbiddenFlag()` (`internal/config/policy.go:138+`) assembles
`--dangerously-skip-permissions` from string fragments purely so it stays
grep-able as a review invariant, and `AGENTS.md:95-99` independently asserts this
flag must never be constructible. Unrelated to this issue but adjacent —
worth preserving as-is; nothing in this change should touch it.

**[V]** No `docs/**/*.md` and no `.aiteam/rulebook.md` exist in this repo. README.md
has zero mentions of `allowed_tools`/`tool_profiles`/permission design. The
rationale for the current design lives entirely in code comments (`doc.go` files,
named findings like `SD-2`), not in prose documentation — Plan mode should expect
to work from the same code-comment sources cited above, not a separate design
doc.

## Domain / Problem Space

The core mechanism: litefactory drives Claude Code headlessly (no human present
to answer an interactive permission prompt). Claude Code's settings JSON is
configured with `DefaultMode: "default"` (`internal/harness/settings.go:80`),
which means any tool invocation not covered by an allow rule is refused outright
in that mode — there is no fallback prompt, so a gap in the allowlist becomes a
hard failure of that specific tool call, not a pause for approval. Issue #101's
title cites 386 such denials across 119 runs as the empirical cost of the current
implement-mode list being too narrow relative to what real implement-mode
turns actually need (e.g., presumably things like additional `go` subcommands, or
other common read/build utilities beyond the 20 entries currently allowed).

**[O]** The new run-6-of-#89 evidence (see New Evidence above) sharpens this from
an abstract denial count into a concrete failure story: a denial gap in one
category (workspace mutation — deleting a file the agent itself created) escalated
through 5 strategies and 8 refusals before the agent settled on a workaround
(emptying rather than deleting the file) that left a permanent, reviewable trace
in the repository. This is evidence that the cost of these gaps is not uniformly
"wasted turns" — some gaps, when unresolved, produce artifacts that outlive the
run and require human cleanup. Plan mode should treat AC-3 (workspace mutation)
with that severity in mind, not just as one more entry in a tool list.

**[R0]** This repo's own design comment history (SD-2, cited above) already
establishes empirically that `--allowedTools` is *additive* to the operator's own
ambient `~/.claude/settings.json` — a prior spike measured 250 ambient allow-rules
leaking through, with Bash executing even when absent from `--allowedTools`, on
whatever machine that spike ran on. That finding is the origin of the current
"defence in depth, not the boundary" language. The issue's ask #4 does not dispute
the underlying measurement — it disputes the *general* framing, on the grounds
that in a headless/asyncAgent run there is no ambient personal `~/.claude/`
settings file layered on top the way there was on the interactive machine the
SD-2 spike measured. **This is a meaningful nuance for Plan mode**: the corrected
comment likely needs to distinguish "defence in depth on an interactive
operator's machine" from "the hard boundary in a headless/asyncAgent run,"
rather than simply inverting the old claim — see Open Questions #2.

## Constraints & Risks

- **Git-write ban must be preserved by omission.** There is no deny-list
  mechanism for `Bash(...)` patterns today (`policy.go`) — the ban exists purely
  because no git-write verb (`commit`, `push`, `add`, `stash`) appears in
  `defaultAllowedTools()`. Any widening must not introduce one, and there is no
  test that would catch it structurally if it did (see Open Questions #4).
- **Secret-path deny rules are a separate mechanism** (`settings.go:83-96`) and
  are unaffected by this change by construction — no risk here, but worth stating
  explicitly in the plan brief so a reviewer does not need to re-derive it.
- **`--strict-mcp-config` is already unconditional** (`runner.go:442`), not
  parameterized by profile — ask #3's "leave unchanged" requirement is satisfied
  as long as this line is not touched.
- **The "defence in depth" comment exists in (at least) three near-duplicate
  locations** plus one textually similar but semantically distinct comment about
  the Deny list. A wording fix scoped only to the literal config.yaml template
  target (`template.go:61-64`) would leave the Go-side struct docs
  (`config.go:222-227`, `config.go:250-252`) internally inconsistent with the
  corrected framing. Plan mode should decide explicitly whether to fix all three
  `allowed_tools`-related occurrences together (recommended) or scope narrowly to
  the literal file the issue names.
- **A pinned test will break by design.**
  `TestDefaultFileDocumentsTheTwoToolKeys` (`template_test.go:157-164`) asserts
  the literal substring `"NOT the boundary"` — updating the comment without
  updating this assertion in the same change will fail CI, correctly.
- **No exact-match test protects the widened list from drift.** Unlike
  `validate`'s profile, `implement`'s `allowed_tools` has no test pinning its
  exact contents, so nothing currently would catch an accidental over-widen
  (e.g., an inadvertent git-write verb) beyond manual review. Plan mode should
  decide whether to add one (see Open Questions #4).
- **The scratch-directory ask has no existing implementation to extend.**
  `AdditionalDirectories: []string{}` in `settings.go:97` is the only structural
  hook, and it is currently always empty — this is new design surface, not a
  narrow value change like the other four asks, and it will need its own design
  decisions (where the directory lives, how it's created/cleaned up, whether it's
  per-run or per-repo, how it interacts with the existing secret-path deny rules
  and the git worktree boundary). **New this run**: the run-6-of-#89 evidence
  shows the gap has (at least) two independent shapes — writability of a scratch
  area, and separately, the *readability* of an agent's own writes under Claude
  Code's own internal per-task tmp path, which litefactory does not compute or
  control at all (see Codebase Context above). A fix for one does not
  necessarily fix the other.
- **New this run — "scope rm/mv to the worktree path" (AC-3) is two independent
  pieces of new design work, not a value change**: (1) no `Bash` rule in this
  codebase has ever been path-scoped — only `Read` rules are, and whether Claude
  Code's own settings grammar even supports a path-scoped `Bash` qualifier is
  unverified (blocked on web access — see Open Questions #6); (2) the worktree
  path is not currently threaded into `DefaultSettings`/`WriteSettings`/
  `EnsureSettings`, and `EnsureSettings`'s write-once-per-`$LF_HOME` contract is
  in tension with a value (worktree path) that varies per run — see Codebase
  Context above for the exact call sites. Plan mode should not assume the
  "scoped" version of AC-3 is achievable without first resolving both points;
  the fallback (an unqualified `Bash(rm:*)`/`Bash(mv:*)` allow, accepting the
  wider blast radius within the existing `--tools` ceiling and worktree
  boundary) should be evaluated as an explicit alternative, not treated as a
  fallback of last resort only if the scoped version turns out to be infeasible.
- **New this run — the denial loop has a demonstrated consequence beyond wasted
  time.** In the run-6-of-#89 example, repeated denial of a deletion attempt led
  the agent to choose a workaround (emptying, not deleting, a scratch file) that
  reached a real commit on a real branch. This is a data-integrity/code-hygiene
  risk distinct from the efficiency argument the issue otherwise makes: even
  after AC-3 is fixed prospectively, Plan mode may want to consider whether
  anything should catch this *class* of workaround (a comment explaining a
  sandbox-restriction workaround, an emptied file that used to have content)
  before it reaches a commit — see Open Questions #7.

## Open Questions

1. **The concrete evidence (386 denials / 119 runs) and any per-tool breakdown
   are not present in this repository.** No telemetry or log format recording
   denial counts by tool name was found under `internal/daemon`, `internal/turn`,
   or `internal/harness`. This data — and any specific list of tools the issue
   author wants added — likely exists only in the GitHub issue body/comments or
   in daemon logs on a specific host, outside this repo's reach. **Plan mode (or
   the requirements-engineer conversation) should pull the issue's full body/
   comments for the actual target tool list**, since this brief only had the
   condensed summary passed in by the router, not the issue's full text (this
   run's new evidence — run 6 of #89 — is one such comment, now incorporated
   above, but the issue may carry others not yet seen by this session).

2. **Scope of the comment fix is ambiguous.** The "defence in depth, NOT the
   boundary" framing appears in `template.go:61-64`, `config.go:222-227`, and
   `config.go:250-252` (all about `allowed_tools`), plus a textually similar but
   semantically distinct comment in `settings.go:28-30` (about the Deny list —
   should likely be left alone). Should Plan mode scope the fix to all three
   `allowed_tools` occurrences for internal consistency, or only the literal
   config.yaml template comment the issue names? Recommend: fix all three
   `allowed_tools` occurrences together; leave the Deny-list comment untouched.

3. **The wording of the corrected comment needs a design decision, not just an
   inversion.** The old claim ("defence in depth, not the boundary") was
   empirically grounded in an interactive-operator spike (SD-2) where an ambient
   personal `~/.claude/settings.json` leaked 250 allow-rules through. In a
   headless/asyncAgent run there is no such ambient file layered on top, so
   `allowed_tools` genuinely is the hard boundary there. Should the corrected
   comment state this conditionally (interactive vs. headless) rather than
   asserting one framing unconditionally? This affects both `template.go` and
   `config.go`'s prose.

4. **Should a new test pin implement's exact `allowed_tools` contents** (mirroring
   `TestValidateProfileIsExactlyReadOnly`) to protect the widened list from silent
   future drift — in particular to catch an accidental git-write verb, given
   there is no deny-list mechanism to fall back on? Recommend yes, added in the
   same change that does the widening.

5. **The scratch-directory ask (#5 / AC-5) has no existing design to build on,
   and this run's new evidence shows it is actually two distinct problems.**
   `AdditionalDirectories` is the only hook and is currently unused — that
   covers *writability*. Separately, run 6 of #89 shows a *readability* gap:
   5 `Read` denials and `tail` denials on files the agent itself had just
   written, under Claude Code's own internal per-task scratch path
   (`/private/tmp/claude-<uid>/.../tasks/...`), which litefactory neither
   computes nor references anywhere in this codebase (confirmed this run — see
   Codebase Context). Plan mode needs to decide: (a) where a
   litefactory-managed scratch directory lives relative to the worktree,
   whether per-run or persistent, how it's created/cleaned, and whether it
   needs its own secret-path-deny-list entry (the original ask), **and
   separately** (b) whether/how to address denials on Claude Code's *own*
   scratch path, which may need a different mechanism entirely (e.g. an allow
   rule scoped to the `TMPDIR`-rooted subtree, if expressible — see Open
   Questions #6 for the same path-scoping-support question) since litefactory
   cannot relocate or rename that path. This is the largest single unknown in
   the issue and should likely be its own user story, now understood to need
   two sub-decisions rather than one.

6. **New this run — is a path-scoped `Bash` allow rule (for AC-3's "scope
   rm/mv to the worktree path", and potentially for the scratch-path readability
   problem in #5) even expressible in Claude Code's settings schema?** This
   repo has zero precedent either way — no `Bash` rule here has ever carried a
   path component, only `Read` rules do (`settings.go:83-96`), and the parsing
   code (`policy.go:42-44`) treats the qualifier as opaque, neither validating
   nor rejecting a path-shaped string. A `web-researcher` call was attempted
   this run to settle this against Claude Code's own documentation and returned
   **"no web access on this run — the issue needs the `lf:tools=web` label."**
   This question cannot be resolved without either that label being added and
   web research re-run, or an operator with direct access to Claude Code's
   settings documentation/changelog confirming it directly. Until it is
   resolved, Plan mode should treat "scope rm/mv to the worktree path" as
   **unconfirmed feasible**, and should have a fallback story ready (an
   unqualified `Bash(rm:*)`/`Bash(mv:*)` allow) in case the scoped form turns
   out not to be supported.

7. **New this run — should Plan mode add a safeguard against the workaround
   pattern itself (emptying rather than deleting a file, then committing it),
   independent of fixing the underlying allowlist gap?** The run-6-of-#89
   evidence shows this pattern reached a real commit on a real branch. Fixing
   AC-3 prospectively prevents new instances of this specific trigger, but does
   not address the general risk that an agent under denial pressure will pick
   *some* workaround and commit its result. Is this worth its own acceptance
   criterion (e.g., guidance to the implementation-engineer agent about
   workaround policy, or a review-time check for emptied/placeholder files), or
   is it out of scope for this issue and better tracked separately? Recommend
   surfacing this explicitly to the issue owner rather than deciding silently
   either way.

8. **Is the widening scoped strictly to `implement`, or should `research`/`plan`
   also be widened?** The issue title says "implement-mode" specifically, but
   `research`/`plan` share the exact same headless hard-denial mechanism
   (`DefaultMode: "default"`) and currently have an even narrower
   `allowed_tools` list than `implement` (no `go build/test/vet/gofmt`, no
   `grep/sed/head/tail/wc`). If research/plan runs are also generating denials in
   practice, scoping the fix to implement-only leaves the same class of problem
   unaddressed elsewhere. Recommend confirming scope with the issue owner before
   Plan mode finalizes story boundaries.

9. **`EnableAllProjectMCPServers`/`EnabledMCPJSONServers` are tied to
   `--strict-mcp-config` only by a comment** (`settings.go:99-103`), not a test —
   unrelated to this issue, but noted as a nearby pattern of comment-only
   invariants that Plan mode may want to flag separately (not in scope for this
   feature).

## Recommended scope for Plan mode

**In scope:**
- Widen `internal/config/policy.go`'s `defaultAllowedTools()` for the `implement`
  profile with the concrete tool list the issue owner specifies (Plan mode should
  obtain the exact list from the full issue text/comments — not fabricated here;
  see Open Questions #1).
- Verify (or add, per Open Questions #4) a test pinning the new exact
  `allowed_tools` contents for `implement`, following the existing
  `TestValidateProfileIsExactlyReadOnly` pattern.
- Correct the "defence in depth, NOT the boundary" comment consistently across
  `internal/config/template.go:61-64`, `internal/config/config.go:222-227`, and
  `internal/config/config.go:250-252` (leave `internal/harness/settings.go:28-30`'s
  Deny-list comment untouched — different claim, still accurate), and update
  `TestDefaultFileDocumentsTheTwoToolKeys` in the same change.
- Design and implement the workspace-mutation ask (AC-3: `rm`/`mv`, and per the
  issue's stated shape, likely `touch`/`cp`/`rmdir` too), informed by the run-6
  evidence above. Plan mode should decide explicitly between: (a) an unqualified
  `Bash(rm:*)`/`Bash(mv:*)`-style allow within the existing `--tools`
  ceiling/worktree boundary, ready to implement now; or (b) a worktree-path-scoped
  rule, which first needs the feasibility question in Open Questions #6 resolved
  (needs the `lf:tools=web` label) and the worktree-path plumbing described in
  Codebase Context implemented (a signature change through
  `DefaultSettings`/`WriteSettings`/`EnsureSettings`, plus a decision about
  `EnsureSettings`'s write-once contract).
- Design and implement a writable scratch directory for implement runs (AC-5),
  using `AdditionalDirectories` in `internal/harness/settings.go:97` as the
  likely integration point for the writability half — this needs its own design
  pass (location, lifecycle, interaction with the secret-path deny list) since
  no prior art exists. Separately assess whether the *readability* half (denials
  on Claude Code's own per-task tmp path) is addressable at all from
  litefactory's side, given that path is not computed or known to this codebase.
- Explicitly re-verify, as an acceptance criterion rather than an assumption,
  that no git-write `Bash(...)` verb is introduced and that
  `--strict-mcp-config` remains unconditional in `runner.go:442`.
- Raise Open Questions #6 and #7 to the issue owner explicitly (path-scoping
  feasibility needs the `lf:tools=web` label; the committed-dead-file consequence
  needs an explicit in/out-of-scope decision) rather than resolving either
  silently.

**Out of scope (unless the issue owner confirms otherwise per Open Questions #8):**
- Widening `research`'s or `plan`'s `allowed_tools` (`artifactAllowed`) — flagged
  as a real but separate risk, not silently folded into this change.
- Any change to `defaultTools()` (the `--tools` ceiling) — that is the actual hard
  surface boundary and is not named by the issue.
- Any change to the secret-path deny list (`settings.go:83-96`) or
  `internal/config/secretscan.go` — both are unrelated, unaffected mechanisms.
- The `forbiddenFlag()` / `--dangerously-skip-permissions` invariant
  (`policy.go:138+`) — adjacent, must not regress, not itself part of this
  feature.
- Cleaning up the specific emptied file committed in `lf/89-...` commit
  `a963729` — that belongs to issue #89's branch, not this one; noted here only
  as evidence, not as a task for this issue.
