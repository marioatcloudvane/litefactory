Retained review content is separate from an accepted application result. A bounded,
authorized rejected submission may be saved and delivered with its diagnostics and
unaccepted status; it does not satisfy this result contract, advance a workflow or
change any owner gate. Correct the submission and use a new request identity. Exact
replay must keep the original request identity and bytes, then recover its receipt.
Keep the complete authored report or plan within the input ceiling; never shorten a
plan merely to pass validation. Full retained owner output uses ordered numbered
parts linked from the short report. Delivery remains pending until every part has a
confirmed receipt. Shared contributions remain bounded at 4 MiB; Plan node results
remain bounded at 1 MiB and the legacy Plan proposal at 8 MiB. Retention and remote
delivery can fail separately; do not claim missing content was retained or delivered.

# Repair result

`result.payload.bug` carries `response` and `repair`; it never carries analysis or approval.

- `succeeded`: `result.outcome` is `completed`, with no questions. The approved bug is fixed and the repository's own checks pass.
- `reshape`: `result.outcome` is `incomplete`, with a `reason` explaining why the diagnosis or remedy must change.
- `blocked`: `result.outcome` is `blocked`, with a `reason` naming the obstacle.

`repair` holds a plain-language `summary` of what changed (or what partial work exists) and, optionally, `checks`: the commands you ran with their outcome, for example `{"command": "xcodebuild -scheme App -sdk iphonesimulator CODE_SIGNING_ALLOWED=NO build", "outcome": "passed"}`. Use the repository's own checks: its README, Makefile, AGENTS or CLAUDE files, and CI configuration. Never claim a check passed that you did not run or that failed.

The change itself lives in git: the factory commits your worktree, pushes, merges and closes. You echo no digests, revisions or factory identifiers.

## Shared submission

Use communication preflight, correct every diagnostic, submit, then read the matching receipt. Recover the original request after a lost reply. The final chat message is a note and never an application result.

```json
{"result":{"outcome":"completed","comment":"Fixed: empty queries now return no matches.","payload":{"bug":{"response":"succeeded","repair":{"summary":"Guard empty input in Search.swift; added a regression test.","checks":[{"command":"xcodebuild -scheme App -sdk iphonesimulator CODE_SIGNING_ALLOWED=NO build","outcome":"passed"}]}}}}}
```
