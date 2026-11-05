# Research brief

## Request

Add a CLI command (e.g. `litefactory repo base <project-id|path> <branch>`) to change the base branch of a registered repository. New work then cuts from and lands on the new branch. Refuse if the branch is not on the remote, and refuse (or apply safely) while runs or graph attempts are open. `repo list` must show the new base. Existing repos with base main must behave as before. First use: project 3 (lisisite) to `stage`.

## Decisions

- **S-01** — mode=feature+change, size=small — one new CLI subcommand plus a store update and a remote check on an existing registration model, and one guard in `repo add`; still small after shaping. (agent's call)
- **O-01** — `litefactory repo base <project-id|path> <branch>` changes the stored base branch of a registered repository, and `repo list` shows it. (owner said: "The owner can change the base branch of an already registered repository")
- **O-02** — The command refuses with a plain message if the branch does not exist on the remote. (owner said: "The base branch must exist on the remote. The command checks this and refuses with a plain message if it doesn't.")
- **O-03** — The change is refused only while work is executing on the repository: an agent run in progress or a workflow step queued or running. Issues paused waiting for an owner reply do not block. Nothing is written when it refuses, and the message names what is open. (owner said: "q1: a", see A-01)
- **O-04** — One successful command updates both the repository record and the current code connection (as a new revision), so `repo list`, worktree creation and delivery for new issues all use the new base. (agent's call, rests on f3 and f4)
- **O-05** — Issues started after the change cut branches from and deliver PRs to the new base; issues already started keep their pinned base for their whole life, and their PRs keep their target. (owner said: "q2: a", see A-02)
- **O-06** — Success prints the repository and the old and new base; asking for the base it already has changes nothing and says so. (agent's call)
- **O-07** — First use works: `litefactory repo base 3 stage` moves lisisite to `stage`. (owner said: "`litefactory repo base 3 stage` for lisisite.")
- **O-08** — `repo add` on an already registered repository refuses when its `--base` would change the stored base, and tells the user to run `repo base`; nothing is written. (owner said: "q3: a", see A-03)
- **K-01** — Existing repositories with base `main` behave exactly as before; nothing changes unless the owner runs the command. First-time `repo add` and re-adding with the same or no `--base` are unchanged. (owner said: "no change to how existing repositories with base `main` behave"; refined by A-03)
- **K-02** — An issue's pinned settings are never rewritten; paused or finished issues keep the base they recorded. (owner said: "q2: a", see A-02)
- **K-03** — Branch prefix, worktree root, clone path and remote of the repository stay as they are; only the base changes. (agent's call)
- **N-01** — No unregister command and no retargeting of open PRs. (agent's call)
- **N-02** — No change to the global `default_base_branch` setting, no per-issue base override, no automatic creation of the branch on the remote. (agent's call)
- **N-03** — No edits to deployment or GitHub Actions in lisisite; the stage-only deploy rule is the owner's existing setup. (agent's call)
- **N-04** — No moving of already-started issues to the new base and no rebasing of their branches. (owner said: "q2: a", see A-02)
- **A-01** — q1 "Which open work should block a base change?" — owner: "q1: a" (only work executing right now; issues waiting for a reply do not block). (owner said)
- **A-02** — q2 "Should lisisite issues that already started keep delivering to main?" — owner: "q2: a" (yes; each issue keeps the base it started with). (owner said)
- **A-03** — q3 "What should `repo add --base` do on an already registered repo?" — owner: "q3: a" (refuse a base change and point to `repo base`). (owner said)


## Assumptions

- **a1** — The command refuses (rather than defers) while work is executing; it never queues the change for later. (agent's call)
- **a2** — The command accepts a numeric project id (as shown in `repo list`) or a registered clone path, like the first use `repo base 3 stage`; an unknown id or path is refused plainly. (agent's call)
- **a3** — The open-work check and both writes happen in one database transaction, so a run that starts at the same moment either sees the old base or is counted as open. Needs a test against the real daemon path. (agent's call)
- **a4** — The remote check asks the clone's origin whether `refs/heads/<branch>` exists. If the remote cannot be reached the command refuses with a clear message instead of guessing. (agent's call)
- **a5** — superseded by O-08 (owner answered q3). (agent's call)
- **a6** — The daemon's regular fetch makes `origin/<new base>` available locally before the next worktree is cut; if the build finds it does not, the command also fetches that one branch. (agent's call)
- **a7** — The command needs no daemon restart; the daemon reads the new base on its next session. The CLI writes the same database the daemon reads, as `repo add` already does. (agent's call)
- **a8** — Docs: the CLI reference gets the new command, its refusals and the new `repo add` refusal; tests cover missing branch, unreachable remote, executing work, a paused issue not blocking and keeping its base, same-base no-op, `repo add` base-change refusal, and that a new session pins the new base. (agent's call)
- **a9** — Paused issues stay safe under O-03 because their delivery and worktree paths read the pinned snapshot (f10); any reader found using the live repository row for a session that has a snapshot must be switched to the snapshot or covered by a test. (agent's call, rests on f5 and f10)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
