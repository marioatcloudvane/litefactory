# Durable whole-set reviews

The ordered stages are stories, engineering, acceptance. Every approval belongs
to one displayed stage and current set revision. Silence, worker completion,
input receipts, continuation and stale, duplicate or wrong-owner replies never
advance a gate. Retain stable draft/story IDs, revisions, decisions, assumptions,
questions, owner replies, review evidence and supersession links.

Meaningful membership or intent changes reopen stories; architecture/dependency
changes reopen engineering; criteria-only changes reopen acceptance unless they
change earlier intent. Clear approval at that earliest stage and every later
stage, retaining content and history. Wording-only edits may retain approvals
only with recorded comparison establishing unchanged meaning; uncertainty
reopens review. Renames preserve identity; splits and merges create linked IDs.

Owner waits end the invocation. Exhaustion retains drafts and explains the pause;
explicit continuation starts a fresh bounded attempt and does not approve work.
