# Research Brief — agents-are-headless-but-must-act-non

## Request

- [O] Determine why headless implementation can edit ordinary worktree files and `.claude/modes/spike.md`, but cannot edit `.claude/agents/**` or `.codex/skills/**`.
- [O] Identify practical ways to complete issue #34 and durable ways to prevent the same class of failure.

## Codebase Context

### Execution and permission flow

- [V] The runner-neutral request carries an opaque vendor permission payload: Claude receives a tool surface and Codex receives a sandbox configuration (`internal/agent/permissions.go:3-24`).
- [V] The daemon constructs and registers Claude and Codex runners independently (`internal/daemon/runners.go:15-52`).
- [V] Tool-profile labels resolve separate Claude and Codex settings; permissions are not translated or unioned between vendors (`internal/daemon/tools.go:78-93`, `internal/daemon/tools.go:197-211`).
- [V] Turn preparation supplies an absolute worktree plus either Claude tools or a Codex sandbox to the runner (`internal/turn/prepare.go:251-312`).
- [V] Claude is launched with a controlled tool/settings surface; Codex is launched with `read-only` or `workspace-write`, the worktree as cwd, and optional extra writable roots (`internal/harness/runner.go:382-459`, `internal/codex/runner.go:148-186`, `internal/codex/runner.go:242-263`).
- [V] After the child exits, litefactory itself stages, commits, and pushes all worktree changes (`internal/git/commit.go:119-168`, `internal/turn/harvest.go:15-27`).

### What does not cause the failure

- [V] The files and directories have ordinary owner-write filesystem bits; writable and rejected files under `.claude` share those permissions.
- [V] Litefactory contains no worktree-subpath denylist for either runner.
- [V] Claude's generated settings deny sensitive reads but do not deny writes to repository agent definitions (`internal/harness/settings.go:63-103`).
- [V] Claude's default policy includes Edit and Write without path qualifiers (`internal/config/policy.go:69-94`).
- [V] The exact rejection text is absent from this repository; it is produced by the external CLI/hosting layer.

### Actual boundary

- [V] The active Codex environment exposes `.codex/skills` as a special read-only filesystem entry even though the worktree as a whole is writable.
- [V] Runs have also demonstrated the same protected-root behavior for `.claude/agents`.
- [V] `.claude/modes/spike.md` lies outside those protected discovery roots and was successfully edited.
- [V] Codex configuration supports only general sandbox mode and extra writable roots. The latter is intended for paths outside the worktree and cannot override a higher-priority protected subpath (`internal/config/config.go:134-207`, `internal/config/validate.go:244-280`).
- [V] Loading user configuration affects discovery/isolation, not path authorization (`internal/config/config.go:195-207`, `internal/codex/runner.go:242-250`).
- [V] The repository forbids permission-bypass flags, so bypassing the host policy is not an acceptable option (`AGENTS.md:87-99`).

### Definition parity today

- [V] Claude definitions live under `.claude/agents`; Codex copies live under `.codex/skills`; shared mode prose remains under `.claude/modes` (`AGENTS.md:25-44`).
- [V] `AGENTS.md` calls the Codex copies generated and requires both sides to change together, but no generator, synchronization command, or parity test exists (`AGENTS.md:61-63`).
- [V] Current synchronization is therefore manual prose policy across two roots that coding-agent hosts may intentionally protect.

## Domain / Problem Space

### Immediate completion options

1. **Trusted human or host-process edit.** [V] Apply the nine planned definition-file changes outside the coding-agent sandbox, then use normal git workflow. This is the only known option requiring no litefactory code change. It is operationally simple but manual and does not prevent recurrence.
2. **Supported host-policy exception.** [V] Configure the external Claude/Codex host to make the two repository-local definition roots writable, if that host exposes a supported per-project control. Litefactory has no such setting today. This retains the existing plan and automation, but depends on an external capability not yet identified.
3. **Cross-agent editing.** [W] Run Claude against Codex definitions and Codex against Claude definitions if each host protects only its own discovery root. This needs an explicit experiment; current evidence does not establish that either cross-edit is allowed, and it risks a fragile tool-specific workaround.

### Durable repository options

4. **Explicit maintenance command.** [V] Add a litefactory command, executed by a trusted operator outside the child-agent sandbox, that synchronizes validated definition bodies into both roots. This preserves a clear human-controlled trust boundary and is narrower than automatic mutation.
5. **Canonical neutral source plus trusted generation.** [V] Store shared behavioral bodies in an ordinary source directory, retain tool-specific wrappers/templates, and generate both discovery trees from the canonical source using a trusted host-side command or release step. This directly fixes manual drift and makes “generated copy” truthful.
6. **Constrained post-run patch application.** [V] Let the child write a patch/manifest into an ordinary worktree path; after exit, litefactory validates and applies only allowlisted definition changes before staging. This preserves agent-driven maintenance but creates a new security boundary requiring path allowlists, traversal and symlink defenses, content/size validation, atomic failure behavior, audit reporting, and comprehensive tests.
7. **Thin protected wrappers referencing neutral bodies.** [W] Keep minimal discovery files in protected roots and move editable instructions elsewhere. This is attractive only if both CLIs reliably follow referenced local bodies; that behavior has not been verified and wrapper edits would still occasionally require a trusted path.

### Comparative assessment

| Option | Completes issue now | Litefactory change | Recurrence protection | Security/operational cost |
|---|---:|---:|---:|---|
| Trusted manual edit | Yes | No | None | Low technical cost; recurring human work |
| Host-policy exception | Yes, if supported | No | Medium | Depends on external configuration; must remain narrowly scoped |
| Cross-agent editing | Unknown | No | Low | Fragile and unverified |
| Maintenance command | After building it | Yes | High | Moderate; explicit trusted operation |
| Canonical source + generation | After building it | Yes/tooling | High | Moderate; migration and generated-output discipline |
| Post-run patch application | After building it | Yes | High | Highest security complexity |
| Thin wrappers | Unknown | Tooling/docs | Medium | Depends on CLI inclusion semantics |

## Constraints & Risks

- [V] Global sandbox weakening or forbidden bypass flags are excluded by repository security policy.
- [V] Re-adding the already-writable worktree through Codex's extra-root option cannot override a protected descendant.
- [V] Switching tool profiles only changes the supported general sandbox/tool surface; it does not add a path exception.
- [V] Any paired-definition mechanism must preserve Claude frontmatter and Codex-specific enforcement, delegation, and reporting preambles; raw whole-file copying is invalid.
- [V] Automatic patch application consumes untrusted child output at a trusted boundary. A permissive implementation could become a sandbox escape.
- [V] A generator must define canonical ownership and detect stale generated output; otherwise it merely relocates the drift problem.
- [V] Selecting another agent is not a demonstrated fix: both protected roots may be enforced by the shared host rather than vendor ownership.
- [V] Existing runner tests pin sandbox modes, extra roots, network flags, and user-config behavior (`internal/codex/runner_test.go:17-139`); new synchronization behavior would need separate parity and path-security tests.

## Open Questions

- [V] Which external host component installs each protected-root policy, and does it support a narrowly scoped project exception?
- [V] Can Claude edit `.codex/skills` and can Codex edit `.claude/agents`, or are both roots protected for every agent?
- [V] Is the statement that Codex copies are generated an intended future architecture or documentation that got ahead of the implementation?
- [V] Should synchronization be an explicit operator action or automatic after an agent run?
- [V] If generation is chosen, which content is canonical: neutral behavioral bodies, Claude definitions, or another structured format?
- [V] Can both CLIs reliably include neutral local instruction bodies from thin discovery wrappers?
- [V] If patch application is chosen, which exact paths and transformations are allowed, and how is the triggering owner's authority represented?

## Recommended scope for Plan mode

- [V] **Fastest path for issue #34:** use a trusted manual/host edit, or enable a documented narrow host-policy exception if one exists. Do not redesign litefactory merely to finish nine prose files.
- [V] **Recommended durable follow-up:** plan a canonical neutral source plus an explicit trusted synchronization command. It resolves manual Claude/Codex drift without turning arbitrary agent output into trusted writes.
- [V] Treat automatic post-run patch application as a separate security-sensitive feature, not an incidental workaround for this issue.
- [V] Before Plan mode, run two minimal external-policy experiments: Claude editing a disposable file under `.codex/skills`, and Codex editing one under `.claude/agents`. The results decide whether cross-agent editing is viable or should be discarded.
- [V] Keep issue #34's behavioral scope unchanged; permission/synchronization architecture should be a separate issue unless the owner explicitly combines them.
