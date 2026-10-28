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

Deliver your result only through the shared result route: communication
preflight, submit and receipt. Do not print a JSON result; terminal text is never
read as a result. The worked result below is exactly what you pass to preflight
and submit. Replace the illustrative content, keep the shape.

Report one entry under `stories` for every story you were given. Use the story
key and the snapshot ID exactly as given. The names you must cover depend on your
role:

 - acceptance: every criterion ID of the story under `criteria`, and `plan` under `reviews`.
 - code: `code` under `reviews`, and no criteria.
 - architecture: `architecture` under `reviews`, and no criteria.

Criteria are always `blocking`. A review is `blocking` or `advisory`. Each
finding has at least one entry of evidence. `check_ids` may hold only check IDs
from your evidence list. Executed passes and authenticated inherited passes can
prove reusable execution requirements; fresh-execution requirements need a pass
executed on this tested commit. Cite the supplied assessment check ID, never an
invented origin ID. Explain inherited coverage as inherited, with its retained
source. Proven base failures and unresolved evidence cannot prove a passing
criterion. Raw failed attempts remain failed.

# completed

```json
{
  "result": {
    "outcome": "completed",
    "comment": "One criterion is proven by the passing suite; one needs a person to try it. The plan was followed.",
    "payload": {
      "validate": {
        "stories": [
          {
            "story": "story-002",
            "snapshot_id": "commit-0123456789abcdef",
            "criteria": [
              {
                "name": "AC-001",
                "status": "satisfied",
                "classification": "blocking",
                "evidence": [{"summary": "The suite command that covers this behavior passed on this commit.", "check_ids": ["suite-12-0"]}]
              },
              {
                "name": "AC-002",
                "status": "unverified",
                "classification": "blocking",
                "evidence": [{"summary": "Needs a person to open the page and confirm the layout; no command proves it.", "check_ids": []}]
              }
            ],
            "reviews": [
              {
                "name": "plan",
                "status": "satisfied",
                "classification": "advisory",
                "evidence": [{"summary": "The change stays inside the planned files and tasks.", "check_ids": ["suite-12-0"]}]
              }
            ]
          }
        ]
      }
    }
  }
}
```

# blocked

You cannot review, for example the checkout is empty. Say why. Nothing is claimed.

```json
{
  "result": {
    "outcome": "blocked",
    "comment": "I could not review.",
    "reason": "The checkout holds no files, so there is nothing to read.",
    "payload": {
      "validate": {}
    }
  }
}
```
