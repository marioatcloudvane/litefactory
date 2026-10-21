# Response schema, version 1

The host requests either selection or drafting. Return exactly one JSON object.
For selection:

{"schema_version":1,"category_ids":["offered category ID"],"reason":"why these categories are relevant"}

For drafting:

{"schema_version":1,"title":"title","body":"Markdown guidance","category_ids":["supplied category ID"],"questions":[{"text":"missing information","options":["plain option"],"follow_up_to":"optional supplied question ID"}],"supersede_question_ids":["optional pending question ID"],"notes":"brief explanation"}

The drafting fields schema_version, title, body, category_ids and questions are
required. An empty title/body/category list may represent an incomplete draft;
it cannot be completed as a valid proposal. Options and follow_up_to on each
question, supersede_question_ids and notes are optional. The host assigns stable
question IDs. IDs may reference only supplied data. Never repeat an answered
question; use a new linked follow-up for a material clarification. Never
supersede answered questions. Responses are limited to 32 KiB. No extra fields,
null objects, duplicate members, or trailing JSON are permitted.
