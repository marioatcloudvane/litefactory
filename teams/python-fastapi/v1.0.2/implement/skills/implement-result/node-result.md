Deliver your result only through the shared result route: communication
preflight, submit and receipt. Do not print a JSON result; terminal text is never
read as a result. Each worked result below is exactly what you pass to preflight
and submit. Replace the illustrative content, keep the shape.

# completed

The story is built and its unit tests pass. No questions and no reason.

```json
{
  "result": {
    "outcome": "completed",
    "comment": "Summarise what was built and which tests you ran, in a sentence or two; do not submit this example unchanged.",
    "payload": {
      "implement": {
        "stories": []
      }
    }
  }
}
```

# incomplete

Part of the story is not done. Say what is left in plain language. The story is
parked and the owner is asked.

```json
{
  "result": {
    "outcome": "incomplete",
    "comment": "Say what is built and what is not.",
    "reason": "Say plainly what is left to do and why you stopped.",
    "payload": {
      "implement": {
        "stories": []
      }
    }
  }
}
```

# blocked

Something outside your story stops you, and you need a decision. Say what and what
you need. The story is parked and the owner is asked.

```json
{
  "result": {
    "outcome": "blocked",
    "comment": "Say what is built before the blocker.",
    "reason": "Say plainly what blocks you and what decision you need.",
    "payload": {
      "implement": {
        "stories": []
      }
    }
  }
}
```
