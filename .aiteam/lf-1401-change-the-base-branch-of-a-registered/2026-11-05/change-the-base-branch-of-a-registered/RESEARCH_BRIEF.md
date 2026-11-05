# Research brief

## Request

Add a CLI command (e.g. `litefactory repo base <project-id|path> <branch>`) to change the base branch of a registered repository. New work then cuts from and lands on the new branch. Refuse if the branch is not on the remote, and refuse (or apply safely) while runs or graph attempts are open. `repo list` must show the new base. Existing repos with base main must behave as before. First use: project 3 (lisisite) to `stage`.

## Decisions

- **S-01** — mode=feature+change, size=small — one new CLI subcommand plus a store update and a remote check on an existing registration model; still small after shaping. (agent's call)
- **O-01** — `litefactory repo base <project-id|path> <branch>` changes the stored base branch of a registered repository, and `repo list` shows it. (owner said: "The owner can change the base branch of an already registered repository")
- **O-02** — The command refuses with a plain message if the branch does not exist on the remote. (owner said: "The base branch must exist on the remote. The command checks this and refuses with a plain message if it doesn't.")
- **O-03** — The change is refused while the repository has open work, as defined by q1; nothing is written when it refuses, and the message names what is open. (owner said: "The change is refused while the repository has open runs or open graph attempts, or it is applied safely between runs.")
- **O-04** — One successful command updates both the repository record and the current code connection (as a new revision), so `repo list`, worktree creation and delivery for new issues all use the new base. (agent's call, rests on f3 and f4)
- **O-05** — Issues started after the change cut branches from and deliver PRs to the new base; already-started issues follow q2. (owner said: "New work uses the new base; open PRs keep their target unless the owner retargets them.")
- **O-06** — Success prints the repository and the old and new base; asking for the base it already has changes nothing and says so. (agent's call)
- **O-07** — First use works: `litefactory repo base 3 stage` moves lisisite to `stage`. (owner said: "`litefactory repo base 3 stage` for lisisite.")
- **K-01** — Existing repositories with base `main` behave exactly as before; nothing changes unless the owner runs the command. First-time `repo add` is unchanged; its base-change path on an existing repo follows q3. (owner said: "no change to how existing repositories with base `main` behave")
- **K-02** — An issue's pinned settings are never rewritten silently; paused or finished issues keep the base they recorded unless q2 says otherwise. (agent's call, rests on f4 and f10)
- **K-03** — Branch prefix, worktree root, clone path and remote of the repository stay as they are; only the base changes. (agent's call)
- **N-01** — No unregister command and no retargeting of open PRs. (agent's call)
- **N-02** — No change to the global `default_base_branch` setting, no per-issue base override, no automatic creation of the branch on the remote. (agent's call)
- **N-03** — No edits to deployment or GitHub Actions in lisisite; the stage-only deploy rule is the owner's existing setup. (agent's call)


## Assumptions

- **a1** — The command refuses (rather than defers) while work is open. Refusing is simpler and safe, and the owner listed it first. Which work counts is q1. (agent's call)
- **a2** — The command accepts a numeric project id (as shown in `repo list`) or a registered clone path, like the first use `repo base 3 stage`; an unknown id or path is refused plainly. (agent's call)
- **a3** — The open-work check and both writes happen in one database transaction, so a run that starts at the same moment either sees the old base or is counted as open. Needs a test against the real daemon path. (agent's call)
- **a4** — The remote check asks the clone's origin whether `refs/heads/<branch>` exists. If the remote cannot be reached the command refuses with a clear message instead of guessing. (agent's call)
- **a5** — superseded by q3: whether `repo add --base` gets the same guards is now an owner question. (agent's call)
- **a6** — The daemon's regular fetch makes `origin/<new base>` available locally before the next worktree is cut; if the build finds it does not, the command also fetches that one branch. (agent's call)
- **a7** — The command needs no daemon restart; the daemon reads the new base on its next session. The CLI writes the same database the daemon reads, as `repo add` already does. (agent's call)
- **a8** — Docs: the CLI reference gets the new command and its refusals; tests cover missing branch, unreachable remote, open work, same-base no-op, and that a new session pins the new base. (agent's call)


## Open questions

### q1 — Which open work should block a base change?

_Asked of: Product Owner Questions_

- **a** — Recommended. Only work that is executing right now: an agent run in progress, or a workflow step queued or running. Issues paused waiting for your reply do not block; they keep the base they started with. Why: lisisite can have issues waiting on you for days, and blocking on them could stop the change indefinitely.  *(recommended)*
- **b** — Any unfinished workflow on the repository, including issues waiting for your reply. Safest, but you may have to finish or cancel every open lisisite issue first.

### q2 — Should lisisite issues that already started keep delivering to main?

_Asked of: Product Owner Questions_

- **a** — Recommended. Yes. Each issue pins its base when it starts, so it keeps `main` for its whole life; only issues started after the change go to `stage`. Small and predictable. Merging into `main` does not deploy, so nothing goes live by accident. You can retarget an old PR by hand.  *(recommended)*
- **b** — No. Move issues that have not delivered yet to the new base too. Bigger change: it rewrites each issue's pinned settings and its branch may need rebasing onto `stage`.

### q3 — What should `repo add --base` do on an already registered repo?

_Asked of: Product Owner Questions_

- **a** — Recommended. If it would change the base, refuse and say to use `repo base`. Today it rewrites the repository record but probably not the pinned copy runs read, which leaves the two disagreeing. First-time registration and re-adding with the same base stay exactly as now.  *(recommended)*
- **b** — Leave it untouched. Smallest change, but keeps the half-update trap open.
- **c** — Make it apply the same checks and full update as `repo base`. Two commands doing one job.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
