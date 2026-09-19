Your terminal output is one strict JSON object. The worked envelope for each
response follows; replace the illustrative content, keep the shape.

# response ask

An incomplete result, a non-blank reason and at least one question. Give every
question concrete options and name the one you recommend.

Every question names its audience: `business` for a decision about what we build
and why, `technical` for a decision about how. The factory groups the published
questions under that heading, so the right person finds their part and skips the
rest. Ask each question of whoever can actually settle it.

```json
{
  "version": 1,
  "response": "ask",
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
  "version": 1,
  "response": "done",
  "result": {
    "outcome": "completed",
    "comment": "Summarise the finished research; do not submit this example unchanged.",
    "artifacts": [
      {
        "id": "brief",
        "kind": "document",
        "path": "path/to/RESEARCH_BRIEF.md"
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

# Artifacts

The factory renders RESEARCH_BRIEF.md and RESEARCH_TECHNICAL.md from your result
and writes them into the supplied session directory. You do not write them. Name
them in artifacts if you want them referenced:

- `<session>/RESEARCH_BRIEF.md` with kind `brief`
- `<session>/RESEARCH_TECHNICAL.md` with kind `technical`

Any other artifact path is worktree-relative, not session-relative, sits strictly
under the supplied session directory, and must already exist as a regular file
when you return, with no symlink anywhere in its path, no leading slash, no dot
segment, no backslash and no colon.

# Corrections

Submit application-result corrections through the supplied helper when it is
available; the terminal result member must agree with the last accepted
candidate. A helper receipt only acknowledges input. It neither publishes your
report nor completes the graph. Missing authentic observations must remain
explicitly unverified.
