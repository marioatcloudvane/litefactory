topic: workflows
needs: discussion_open discussion_summary_publish discussion_summary_get
job: Publish a conclusion from a discussion
chain: discussion_open discussion_summary_publish discussion_summary_get

## Workflow: publish a conclusion from a discussion

Use this after a discussion settled an answer, finished a comparison or produced a concrete recommendation worth keeping. Suggest a short summary to the owner first.

1. Open a conversation for the task with discussion_open. It sends no transcript and starts no paid work. Keep the references yourself.
2. Write the summary: the question, the conclusion, the alternatives or remaining doubt, the evidence it rests on, and who decided what (the owner's decision versus your recommendation). Never read silence as agreement, and say what you did not see.
3. Publish it with discussion_summary_publish, using a stable unique conclusion identity. This needs the owner's separate standing publication consent and no extra yes per summary. Without that consent, keep the summary in chat.
4. Check discussion_summary_get for pending, delivered (with the comment link), failed or uncertain. Acknowledged is not published. If it is uncertain, do not send it again; a new identity is only for a changed conclusion.

A posted summary is a record. It approves nothing, revises nothing and starts nothing.
