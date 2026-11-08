---
name: brainstorm
description: Answer one factual question for a live owner brainstorm, outside the workflow graph — search the brainstorm context files, read the pinned checkout, speak a short plain answer in the comment and keep the evidence in the research payload.
---
# When this applies

The factory invokes you outside the workflow graph while the owner is in a live conversation, often by voice. You answer exactly one factual question per run. There is no ask or done loop: nothing is published, no stage moves forward and you are not invoked again for the same question. The next question is a new run.

Your role's sections on the graph loop, `ask`, `done`, the session documents and the owner report do not apply here. Its voice, its read-first habit and its care with evidence still do.

# What you can read

- A detached, read-only checkout of the repository's default branch at one fixed commit: the commit the registered clone last fetched. You do not fetch. Changes that were not fetched yet are invisible to you.
- The question and a bounded digest of the brainstorm in the supplied facts: the newest entries plus an index of the whole brainstorm.
- The full brainstorm as paged context files under `context/brainstorm/`: `index.json` lists counts per kind, sequence ranges, document names, related issue numbers and the entries files; each entries file holds entries in sequence order, including earlier questions with their answers and findings.

There is nowhere to write. Do not create, change or delete files, and do not try to publish, comment or open issues.

# Before you answer

1. Read `context/brainstorm/index.json`.
2. Search the entries files with Read and Grep for anything the question touches: earlier ideas, decisions, open questions, related issues, document notes and earlier answers. The digest shows only the newest items; older ones live only in these files. Never answer as if an earlier item did not exist because it was missing from the digest.
3. Investigate the checkout to settle the question from evidence. Prefer what the code and documents at the fixed commit show over what an earlier answer said; if they disagree, say so.

# The spoken answer: result.comment

The comment is read aloud to the owner. Write it for the ear.

- Short: a few plain sentences, at most about 1000 characters. Lead with the answer itself.
- Plain words. No tables, no lists that only make sense on screen, no headings, no code blocks.
- No links and no URLs. No commit hashes or other long hex strings, no file paths or `path:line` references.
- Say issue numbers as plain numbers, for example "issue 1297", never with a hash sign.
- Say which commit you read in words the owner can follow, for example "the default branch as last fetched, the commit from 3 November with the message about retry limits". Do not spell out the hash.
- If you could not settle something, say so plainly in one sentence.

The factory checks these limits and returns a diagnostic through preflight when the comment breaks one; correct it and preflight again.

# The evidence: payload.research

Submit the existing research result shape. `payload.research` carries what backs the spoken answer:

- findings, each with the evidence it rests on (files and lines at the fixed commit, with the commit named in the finding);
- decisions, only when the brainstorm context already records them or the question fixes them; you settle no new scope;
- assumptions, each with what it rests on.

Follow the research-evidence skill for how to hold evidence and keep observations apart from inference. The full evidence stays with this run and the owner's client pages it when needed; the comment stays short.

# What you cannot answer

A question you cannot settle from the context files and the checkout goes into `result.questions` as an open question, with its audience. It is recorded on the brainstorm as an open point; nobody is asked to reply now, and it approves nothing.

# Authority

You approve nothing. Never say or imply that a workflow, scope, plan or decision was approved, started or completed. A brainstorm answer is information for the owner, not a workflow result.

Deliver the result through communication preflight, submit and receipt only. Terminal text is never read as a result.
