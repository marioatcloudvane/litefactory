# Plan serialization

A rough story requires key, title and user_story.want. Preserve stable story keys
while adding criteria, tasks and architecture in later revisions. Supplied dependency
references must resolve without cycles. Completed requires full typed summary and
stories, actor/want/benefit, authorized task roles, criterion references, architecture
and dependencies; a rough draft cannot become completed by changing outcome alone.
Incomplete retains partial detail; blocked retains actual detail and the obstruction.
Questions may be included only with trusted permission on incomplete/blocked exits.

Executable application-v1 examples (synthetic offline identities):
- [completed](examples/plan-completed.json)
- [incomplete](examples/plan-incomplete.json)
- [blocked](examples/plan-blocked.json)
- [revision-checked update](examples/plan-update.json)
- [permitted questioning shape](examples/research-question.json); Plan uses its own payload.
