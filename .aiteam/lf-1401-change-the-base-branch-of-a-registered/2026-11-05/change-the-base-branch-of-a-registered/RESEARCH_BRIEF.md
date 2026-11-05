# Research brief

## Request

Add a CLI command (e.g. `litefactory repo base <project-id|path> <branch>`) to change the base branch of a registered repository. New work then cuts from and lands on the new branch. Refuse if the branch is not on the remote, and refuse (or apply safely) while runs or graph attempts are open. `repo list` must show the new base. Existing repos with base main must behave as before. First use: project 3 (lisisite) to `stage`.

## Decisions

- **S-01** — mode=feature+change, size=small — one new CLI subcommand plus a store update and a remote check on an existing registration model. (agent's call)
- **O-01** — `litefactory repo base <project-id|path> <branch>` changes the stored base branch of a registered repository, and `repo list` shows it. (owner said: "The owner can change the base branch of an already registered repository")
- **O-02** — The command refuses with a plain message if the branch does not exist on the remote. (owner said: "The base branch must exist on the remote. The command checks this and refuses with a plain message if it doesn't.")
- **O-03** — The change is refused while the repository has open runs or open graph attempts, or is applied safely between runs. New work uses the new base; open PRs keep their target. (owner said: "The change is refused while the repository has open runs or open graph attempts, or it is applied safely between runs.")
- **K-01** — Existing repositories with base `main` behave exactly as before; `repo add` behaviour is unchanged. (owner said: "no change to how existing repositories with base `main` behave")
- **N-01** — No unregister command and no retargeting of open PRs. (agent's call)


## Assumptions

- **a1** — The command refuses (rather than defers) while work is open. Refusing is simpler and safe, and the owner already listed it first. (agent's call)
- **a2** — The command accepts a numeric project id or a path, like the first use `repo base 3 stage`. (agent's call)
- **a3** — The command updates both the repos row and the code connection in one step, so new sessions pin the new base. Needs a test against the real daemon path. (agent's call)
- **a4** — The remote check uses `git ls-remote --refs origin refs/heads/<branch>`. If the network is unreachable the command refuses with a clear message instead of guessing. (agent's call)
- **a5** — `repo add --base` is left as is in this change. Whether it should also get the same guards is a shaping call. (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
