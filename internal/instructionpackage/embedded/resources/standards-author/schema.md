# Response schema, version 1

The host requests either selection or drafting. Return exactly one JSON object.
For selection:

{"schema_version":1,"category_ids":["offered category ID"],"reason":"why these categories are relevant"}

For drafting:

{"schema_version":1,"category_id":"supplied category ID","rule":"MUST ...","description":"the rule, its purpose and the expected implementation, with short compliant and non-compliant examples where they help","evidence":"the artifact, record, configuration or report that shows compliance","scope":"systems, repositories, stacks or lifecycle stages","check":"automated","applies_to":["agent role name"],"questions":[{"text":"missing information","options":["plain option"],"follow_up_to":"optional supplied question ID"}],"supersede_question_ids":["optional pending question ID"],"notes":"brief explanation"}

The drafting fields schema_version, category_id, rule, description, evidence,
scope, check, applies_to and questions are required. The rule starts with
"MUST ". check is automated, semi_automated or manual. applies_to names the
agent roles that apply the standard, such as story-worker. An empty field may
represent an incomplete draft; it cannot be completed as a valid proposal.
Options and follow_up_to on each question, supersede_question_ids and notes are
optional. The host assigns stable question IDs. IDs may reference only supplied
data. Never repeat an answered question; use a new linked follow-up for a
material clarification. Never supersede answered questions. Responses are
limited to 32 KiB. No extra fields, null objects, duplicate members, or trailing
JSON are permitted.
