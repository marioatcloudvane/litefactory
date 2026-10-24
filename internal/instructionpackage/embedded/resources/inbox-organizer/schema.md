# Inbox organizer result schema, version 1

Return exactly these fields:

```json
{"version":1,"layout":"action_groups","tickets":[{"key":"exact input ticket key","decision_keys":["every exact decision key for this ticket"],"summary":"concise descriptive wording","promotion_reason":"","evidence_ids":[]}]}
```

`version` must be 1 and `layout` must be `action_groups`. `tickets` contains every input ticket exactly once in proposed order, with every input decision key exactly once on its own ticket. No added, omitted, repeated or moved decision identifiers. Each ticket has only `key`, `decision_keys`, `summary`, `promotion_reason`, and `evidence_ids`. For baseline entries, use empty promotion reason and evidence identifiers. A forward promotion requires a nonempty explanation and applicable factory-issued evidence identifiers from that ticket. No unknown fields, duplicate object keys, nulls or trailing data. Complete output must fit 256 KiB. Summaries must be short and contain descriptive text only; the factory owns factual actions and navigation.
