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
            "id": "d1",
            "text": "Record one settled decision."
          }
        ]
      }
    }
  }
}
```

Any other shape, such as a completed result that still carries questions, fails
the step.

# response approved

There is no separate result for approval, and you never claim it. It reports
the owner's verdict, never your own. The factory records `approved` for a
completed result only when it holds the owner's reply accepting the review of
that same candidate. When a reply you were given accepted the work, submit the
reviewed `done` result unchanged; if you change it, the factory shows the owner a
new review instead. Only a role whose instructions define an approved response
can reach it, and recording the approval in your comment does not report it.

# Artifacts

After `lf_agent` retains your result, the explicit `lf_research_brief` activity
renders RESEARCH_BRIEF.md and RESEARCH_TECHNICAL.md from that exact candidate
and writes them into the supplied session directory. Successful `lf_comment`,
`lf_ask` and stage handoff require this generation to succeed; old files do not
satisfy the gate. Only these two generated documents defer existence checks. You do not write them. Name
them in artifacts if you want them referenced:

- `<session>/RESEARCH_BRIEF.md` with kind `brief`
- `<session>/RESEARCH_TECHNICAL.md` with kind `technical`

Kind `data` is for structured reference material a later stage consumes verbatim:
a data model, a schema, an interface table. `technical` is evidence for a claim;
`data` is material to build from. Those three kinds are the whole set.

Any other artifact path is worktree-relative, not session-relative, sits strictly
under the supplied session directory, and must already exist as a regular file
when you submit, with no symlink anywhere in its path, no leading slash, no dot
segment, no backslash and no colon.

# Delivering the result

Deliver through communication preflight, submit and receipt, or the equivalent
methods of the factory-supplied result helper: preflight your result, correct
every diagnostic it reports, submit, then read the matching receipt. Do this
before you finish, and do it whether or not you have anything to correct — it is
how a result is delivered, not only how one is amended.

Do not print a JSON result afterwards. The accepted submission is the step's
result; an attempt without one fails whatever you print. End with a short
plain-language note.

A receipt only acknowledges input. It neither publishes your report nor
completes the graph, and acknowledgment is not acceptance. Missing authentic
observations must remain explicitly unverified.
