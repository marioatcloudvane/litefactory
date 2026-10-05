# Research brief

## Request

Make Plan faster and simpler: create the initial story drafts in parallel instead of one agent after another; run independent story stages in parallel the simple way; keep it very simple and break nothing.

## Decisions

- **d1** — Do the whole issue, in small steps, kept simple (owner answer to Q1).
- **d2** — A failed draft fails only its story; the others are kept and the owner retries by reply (owner answer to Q3).
- **d3** — Each story review appears as soon as its draft is merged (owner answer to Q4).
- **d4** — Clean-up is in scope but removes only what is provably safe; goal is simplicity and fewer errors, not fewer lines (owner answer to Q5).


## Assumptions

- **a1** — First-draft parallelism is the smallest safe first step. It rests on the owner's comment and on the sequential story_pick loop in the Plan graph.
- **a2** — Parallel agents do not write shared state; the factory merges each finished draft into freshly read state one at a time, in displayed order. This rests on the shared revision-checked state found in research.
- **a3** — Only an owner reply approves a stage; fan-out changes none of the gates. This rests on the existing Plan graph, where review-ready is never approval.
- **a4** — A story drafts once the stories it depends on have a draft; independent stories start together. The owner asked for an explanation of Q2 and gave no other direction; this matches today's ordering rule.
- **a5** — Steps are built in order: parallel first drafts, per-story tasks, then safe clean-up. This rests on the owner's wish for simple and nothing breaking.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
