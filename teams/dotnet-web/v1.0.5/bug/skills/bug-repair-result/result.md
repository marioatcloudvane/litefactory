# Repair result

`result.payload.bug` carries `response` and `repair`; it never carries analysis or approval.

- `succeeded`: `result.outcome` is `completed`, with no questions. The approved bug is fixed and the repository's own checks pass.
- `reshape`: `result.outcome` is `incomplete`, with a `reason` explaining why the diagnosis or remedy must change.
- `blocked`: `result.outcome` is `blocked`, with a `reason` naming the obstacle.

`repair` holds a plain-language `summary` of what changed (or what partial work exists) and, optionally, `checks`: the commands you ran with their outcome, for example `{"command": "dotnet test <explicit-test-project>", "outcome": "passed"}`. Use the repository's own checks: its README, Makefile, AGENTS or CLAUDE files, and CI configuration. Never claim a check passed that you did not run or that failed.

The change itself lives in git: the factory commits your worktree, pushes, merges and closes. You echo no digests, revisions or factory identifiers.

## Shared submission

Use communication preflight, correct every diagnostic, submit, then read the matching receipt. Recover the original request after a lost reply. The final chat message is a note and never an application result.

```json
{"result":{"outcome":"completed","comment":"Fixed: empty queries now return no matches.","payload":{"bug":{"response":"succeeded","repair":{"summary":"Guard empty input in Search.cs; added a regression test.","checks":[{"command":"dotnet test <explicit-test-project>","outcome":"passed"}]}}}}}
```
