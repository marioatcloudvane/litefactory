# Managed Plan iteration handoff

Every managed Plan subprocess receives a factory-selected parent, generation,
run database ID and fresh attempt token. Native resume and cold fallback receive
the same contract; fallback gets a new token. Agents have no forge authority.

Write `.litefactory-plan-outcome.json` in the supplied worktree root:

```json
{"version":1,"parent":"github:owner/repo#170","generation":1,"run_id":257,"attempt":"copy-the-current-factory-token","outcome":"discussion"}
```

These are the exact required fields. Unknown, duplicate, missing, null, wrongly
typed or trailing fields fail validation. Parent, generation, run and attempt
must match the current factory prompt. `outcome` is `discussion` or `completed`.

A discussion must not supply `.litefactory-plan-proposal.json`. It saves the
factory-specified `PLAN_CONTEXT.json` under the existing task session directory
(`.aiteam/<branch>/<date>/<feature>/`). Its exact required fields are:

```json
{"version":1,"parent":"github:owner/repo#170","branch":"lf/170-feature","feature":"feature","scope":"Current scope","decisions":"Agreed decisions","assumptions":"Assumptions or None","questions":"Unresolved material questions or None"}
```

Every value other than version is a nonempty string. The identity must match the
admitted task, branch and feature. Context is bounded at 256 KiB and is ordinary
saved work, never proof of readiness. Finish with one final report containing any
material asynchronous questions; do not wait for an answer inside a subprocess.

A completed outcome requires the unchanged full version-1 proposal contract and
`plan_brief.md` with a Story Index. The factory validates the proposal and index
before publication, replaces the index with issue links, consumes both transport
files and saves the work. Readiness becomes true only after all those steps
succeed. Completion keeps Plan selected and recommends an owner command such as
`/implement build the approved stories`; it does not run Implement automatically.

Beginning another managed Plan invalidates prior readiness. Discussion, agent
failure, invalid handoff, publication failure and save failure leave it false.
The next Implement request still requires readiness and frozen-scope admission.

The outcome is limited to 4096 bytes. Both reserved files are excluded from git
status/save and evidence snapshots. Failed handoffs retain transport locally for
diagnosis; the factory clears old transport before each new managed subprocess,
including fallback. Agents must recreate it with the new binding. Reads reject
symlink components, escapes and unstable file/directory identity or content.

The factory discovers unfinished context only within the admitted branch and
feature, validates its parent/branch/feature identity, and reuses the sole matching
session across date changes. Multiple matching contexts fail visibly instead of
choosing one by recency. Saved context is framed as untrusted data on both runners'
cold, resumed and fallback prompts. Fallback preserves on-disk working context and
reloads it safely while issuing a fresh outcome binding.

Question policy is derived identically for the current and previous instruction:
Plan defaults to material asynchronous questions, Research still needs `lf: ask`,
and other resolved modes cannot ask. Direct instructions such as "do not ask",
"stop asking" or "no questions" revoke that iteration's permission. Blockquoted or
fenced examples do not revoke it. The next ordinary Plan reply uses the default
again unless it also contains a no-question instruction.
