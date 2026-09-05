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

No web research was performed for this brief: every question raised by the issue
resolves inside this repository — the CLI-flag semantics that motivate the change
are already documented from litefactory's own prior spike measurements (see SD-2
below), not from upstream Claude Code documentation, so `web-researcher` was not
invoked.

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
- Comparison profiles (for scoping — see Open Questions #6):
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
  and the git worktree boundary).

## Open Questions

1. **The concrete evidence (386 denials / 119 runs) and any per-tool breakdown
   are not present in this repository.** No telemetry or log format recording
   denial counts by tool name was found under `internal/daemon`, `internal/turn`,
   or `internal/harness`. This data — and any specific list of tools the issue
   author wants added — likely exists only in the GitHub issue body/comments or
   in daemon logs on a specific host, outside this repo's reach. **Plan mode (or
   the requirements-engineer conversation) should pull the issue's full body/
   comments for the actual target tool list**, since this brief only had the
   condensed 5-point summary passed in by the router, not the issue's full text.

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

5. **The scratch-directory ask (#5) has no existing design to build on.**
   `AdditionalDirectories` is the only hook and is currently unused. Plan mode
   needs to make several decisions from scratch: where the directory lives
   relative to the git worktree, whether it is per-run or persistent, how/when it
   is created and cleaned up, and whether it needs its own entry in the secret-
   path deny list or config schema. This is the largest single unknown in the
   issue and should likely be its own user story.

6. **Is the widening scoped strictly to `implement`, or should `research`/`plan`
   also be widened?** The issue title says "implement-mode" specifically, but
   `research`/`plan` share the exact same headless hard-denial mechanism
   (`DefaultMode: "default"`) and currently have an even narrower
   `allowed_tools` list than `implement` (no `go build/test/vet/gofmt`, no
   `grep/sed/head/tail/wc`). If research/plan runs are also generating denials in
   practice, scoping the fix to implement-only leaves the same class of problem
   unaddressed elsewhere. Recommend confirming scope with the issue owner before
   Plan mode finalizes story boundaries.

7. **`EnableAllProjectMCPServers`/`EnabledMCPJSONServers` are tied to
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
- Design and implement a writable scratch directory for implement runs, using
  `AdditionalDirectories` in `internal/harness/settings.go:97` as the likely
  integration point — this needs its own design pass (location, lifecycle,
  interaction with the secret-path deny list) since no prior art exists.
- Explicitly re-verify, as an acceptance criterion rather than an assumption,
  that no git-write `Bash(...)` verb is introduced and that
  `--strict-mcp-config` remains unconditional in `runner.go:442`.

**Out of scope (unless the issue owner confirms otherwise per Open Questions #6):**
- Widening `research`'s or `plan`'s `allowed_tools` (`artifactAllowed`) — flagged
  as a real but separate risk, not silently folded into this change.
- Any change to `defaultTools()` (the `--tools` ceiling) — that is the actual hard
  surface boundary and is not named by the issue.
- Any change to the secret-path deny list (`settings.go:83-96`) or
  `internal/config/secretscan.go` — both are unrelated, unaffected mechanisms.
- The `forbiddenFlag()` / `--dangerously-skip-permissions` invariant
  (`policy.go:138+`) — adjacent, must not regress, not itself part of this
  feature.
