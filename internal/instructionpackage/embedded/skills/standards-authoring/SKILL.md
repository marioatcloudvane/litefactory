---
name: standards-authoring
description: How the conversation agent takes one standard from the owner's idea to the published book.
---
# Authoring a standard with the owner

## The flow

1. **Start.** The owner has an idea for a standard. Propose `standards-author` with action `start`, the owner's idea spanned as `intent` and, when the owner names one, the `project`. Starting costs money, so the factory asks the owner to confirm it. After the start the author drafts in the background and its draft is posted here.
2. **Draft and answers.** The posted draft shows the rule, its open questions and what still blocks it. The owner answers, asks for changes or sets a field; each of these runs without a confirmation and the author drafts again by itself.
3. **Publish.** When `author_session.ready_to_publish` is true (no open questions, nothing in `blocking`, the author not running) and the owner says it is good, propose `standards-author` with action `publish`. That one step completes the draft into a proposal, approves it and publishes it to the book. There is no separate complete, approve or publish turn for the owner, and no code, version or digest to give.

## Where ids come from

- `author_session.reference` carries the session's id and version. Cite it as `reference` for `id` (and `version` when you give one), or leave both out: the factory then uses this conversation's session at its newest version.
- Never type ids, versions or digests yourself, never take them from your own earlier replies, and never ask the owner for them.
- `category_id`, `check`, `applies_to`, `version` and `mode` may be plain `value`s; the factory checks them against the book. Prose (`rule`, `description`, `evidence`, `scope`, `text`, `intent`) is always an exact span of the owner's words.

## What the owner says, and what to propose

- "1a 2b", "the first one, and CI" or any reply to the open questions: action `answer`, the owner's whole reply spanned as `text`.
- "make it shorter", "put it in category X", "suggest something", "it should also apply to plan-technical": action `answer` with the owner's whole message as `text`; the author works it in. With no open question the reply still reaches the author.
- The owner gives a literal value for one field ("set the check to manual", or pastes the rule): action `edit` with that field. Span pasted prose exactly; a check, category or role list can be a plain value.
- "yes", "good", "publish it", "ship it", "passt" after a draft that is ready: action `publish`, with the owner's words from the current message that say so spanned as `text`.
- "again" after a failed or interrupted draft: action `continue`.
- `stage` "proposal" means the draft was already turned into a proposal that is not in the book yet: "publish it" is action `publish` and picks up where it stopped; a change request is action `revise` with the owner's words as `text`.
- For the full draft, look up `author.inspect` with the session reference.

## When the draft is not ready

- If the owner says to publish while questions are open or `blocking` lists something, say plainly what is still open and do not propose `publish`. The factory refuses it anyway, with the reason.
- A `blocking` item about the category on an empty book means the draft needs a new category: the author proposes `new_category` and sets the draft's `category_id` to that category's id. Tell the owner the author adds the category with the standard; pass any wish about it on as an `answer`.
- An empty book is normal before the first standard: `book.categories` then lists none.

## Recovering from errors

- After an error, read the session again (`author_session`, or `author.inspect`) before you say anything about the draft.
- Never claim the draft changed, moved on or was published unless the version changed or the factory said so.
- "The author has a newer draft than the one you saw" means show that draft to the owner; nothing was changed.
- If publishing stopped part way ("proposal created, not yet published"), say exactly that; the owner's next "publish it" finishes it, once.
