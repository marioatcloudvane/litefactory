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
read as a result. The accepted submission is this step's result, and the factory
derives the step's response from it. The worked result for each response follows:
each is exactly what you pass to preflight and submit. Replace the illustrative
content, keep the shape.

# response ask

An incomplete result, a non-blank reason and at least one question. Give every
question concrete options and name the one you recommend.

Every question names its audience: `business` for a decision about what we build
and why, `technical` for a decision about how. The factory groups the published
questions under that heading, so the right person finds their part and skips the
rest. Ask each question of whoever can actually settle it.

```json
{
  "result": {
    "outcome": "incomplete",
    "comment": "Summarise what is established so far; do not submit this example unchanged.",
    "reason": "State why research cannot complete without the owner's answer.",
    "questions": [
      {
        "id": "q1",
        "audience": "business",
        "text": "Ask the material question that blocks completion.",
        "options": [
          {
            "id": "a",
            "text": "Describe one permitted choice."
          }
        ],
        "recommended_option": "a"
      }
    ],
    "payload": {
      "research": {
        "request": "Restate the owner's request.",
        "findings": [
          {
            "id": "f1",
            "text": "Record one observed claim.",
            "evidence": [
              {
                "path": "path/to/file",
                "line": 1
              }
            ]
          }
        ],
        "assumptions": [
          {
            "id": "a1",
            "text": "Record one reasonable assumption."
          }
        ]
      }
    }
  }
}
```

# response done

A completed result and no questions.

```json
{
  "result": {
    "outcome": "completed",
    "comment": "Summarise the finished research; do not submit this example unchanged.",
    "artifacts": [
      {
        "id": "brief",
        "kind": "brief",
        "path": "path/to/session/RESEARCH_BRIEF.md"
      },
      {
        "id": "technical",
        "kind": "technical",
        "path": "path/to/session/RESEARCH_TECHNICAL.md"
      }
    ],
    "payload": {
      "research": {
        "request": "Restate the owner's request.",
        "findings": [
          {
            "id": "f1",
            "text": "Record one observed claim.",
            "evidence": [
              {
                "path": "path/to/file",
                "line": 1
              }
            ]
          }
        ],
        "decisions": [
          {
            "id": "O-01",
            "text": "Record one agreed outcome, ending with its source: owner said or agent's call."
          }
        ]
      }
    }
  }
}
```

Any other shape, such as a completed result that still carries questions, fails
the step.

# Owner approval

Approval is the authenticated owner's verdict about the published current review.
The factory handles an explicit `done` reply using the retained displayed result
and follows the graph's final publication branch without another agent invocation.
Do not submit an `approved` response or reproduce a candidate to claim approval.
Ordinary feedback resumes the originating stage and may produce another review.

# response continue

An incomplete result with a reason, cumulative partial findings and no questions requests another bounded pass of the same stage. The factory publishes no progress comment.

```json
{"result":{"outcome":"incomplete","comment":"Retained boundary findings.","reason":"Trace the remaining callers.","payload":{"research":{"request":"Inspect boundaries","continue":true,"findings":[{"id":"f1","text":"The store owns SQL."}]}}}}
```

Keep all still-applicable findings and evidence in each snapshot, including previously consumed owner guidance as background context. A failed process never requests another automatic pass.
