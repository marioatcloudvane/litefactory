---
name: brainstorm-drafts
description: Turn a live owner brainstorm into one to five task drafts, outside the workflow graph — read the brainstorm context files and the pinned checkout, write each draft with its own outcome ledger, speak a short summary in the comment and ask your open questions in the conversation.
---
# When this applies

The factory invokes you outside the workflow graph when the owner says the brainstorm is ready and asks for drafts. You run once and turn the whole brainstorm into task drafts. Nothing is published: you create no issue, you start no workflow and no stage moves forward. The owner reads your drafts, may drop some, and decides in the conversation whether the factory creates them. Asking for drafts again is a new run that replaces this set.

Follow the brainstorm skill for what you can read, the read-first habit and the rules of the spoken comment. Your role's sections on the graph loop, `ask`, `done`, the session documents and the owner report do not apply here. Its voice, its way of settling scope and its care with evidence still do.

# Before you draft

1. Read `context/brainstorm/index.json`.
2. Read every entries file, not only the newest. Ideas, decisions, open questions, related issues, document notes and earlier answers all count. An earlier decision still holds unless a later entry changes it. An owner reply to one of your earlier questions is a decision entry: use it, and never ask that question again.
3. Check the checkout where a draft depends on how things work today, so a draft does not ask for something that already exists.

# Cutting the work into drafts

- Make one to five drafts. One draft is one task someone can build and ship on its own. Prefer fewer, whole drafts over many thin ones.
- Split only where the parts can ship apart and the owner gains something from each part alone. Keep work together when one part is useless without the other.
- Leave out what the brainstorm did not ask for. Do not invent work to fill five drafts.

# Each draft

Each entry of `payload.brainstorm_drafts.drafts` has:

- `key`: a short stable name for the draft, unique in this set, for example `d1`. The owner drops drafts by key, so keep keys simple.
- `title`: one plain line, at most 256 bytes, that a reader understands without the brainstorm. It becomes the issue title.
- `size`: `S`, `M` or `L`. S is a small change in one place. M touches a few places or needs a migration or a new tool. L crosses several parts of the system or needs a design choice that is still open.
- `impact`: one or two plain sentences on who gains what, and what it touches.
- `related_issues`: issue references the brainstorm recorded as related to this draft. Use only references from the brainstorm entries; do not guess numbers.
- `duplicate_warning`: empty when you know of no overlap. Otherwise one plain sentence naming the related issue that may already cover this draft and why. It is a warning for the owner to check, never proof. The factory also runs its own issue search per title and shows those hits beside your warning.
- `ledger`: the existing research result shape for this one draft. Plan builds the task from it, so it must stand on its own.

# The ledger

The ledger has `request` (one plain line on what this draft asks for), `findings`, `decisions` and `assumptions`, as in the research-evidence skill.

- `decisions` hold the outcome ledger with prefixed ids, unique inside this draft:
  - at least one `O-nn`: the goal and what is in scope, as what must be true when the work is done;
  - `N-nn`: non-goals, what this draft explicitly does not do, including work that went to another draft;
  - `K-nn`: what must not change;
  - `A-nn`: owner answers from the brainstorm that shape this draft, quoted as the owner said them.
- End each decision with its source, `(owner said: "...")` or `(agent's call)`, as the research-evidence skill asks.
- An open question that matters for this draft goes into its `assumptions`: the question, the working answer you chose until the owner answers, and `(agent's call)`.
- `findings` carry the facts the draft rests on, with evidence at the fixed commit, and keep what the code shows apart from what you infer.
- Write each draft's ledger as if the reader never saw the brainstorm or the other drafts.

# The spoken summary: result.comment

The comment is read aloud. Follow the brainstorm skill's rules for the spoken answer: short, plain words, no lists made for the screen, no links, paths or hashes, issue numbers as plain numbers. Say how many drafts there are, name each one by its title in a few words with its size, and say if any may duplicate an existing issue. Leave the detail to the drafts.

# Your open questions: result.questions

Put the few questions that most change the drafts into `result.questions`, at most about five, weightiest first. Give each its audience, concrete options and the one you recommend. Keep the text to the question itself, in one line, so it is easy to read aloud.

The questions are kept on the brainstorm as open questions and the owner's client asks them in the conversation. Do not wait for answers: the drafts you submit now already use your recommended answers as their working assumptions. When the owner answers, the answer is kept on the brainstorm and the next drafts run uses it.

# Submitting

Submit under the payload key `brainstorm_drafts`, never `research`: `payload.brainstorm_drafts.drafts` holds the drafts in the order you want them read. The factory checks the shape and limits and returns a diagnostic through preflight when something breaks a rule, for example a missing `O-` decision, a repeated id, a title that is too long or an issue body that would be too large; correct it and preflight again.

Deliver the result through communication preflight, submit and receipt only. Terminal text is never read as a result.

# Authority

You approve nothing. Drafts are proposals for the owner, not tasks. Never say or imply that an issue was created, that Plan started or that a scope or stage was approved. Only the owner's own yes in the conversation creates issues and starts Plan, and that yes approves no Plan stage.
