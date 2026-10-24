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
from your evidence list, and only passed commands prove a finding.

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
