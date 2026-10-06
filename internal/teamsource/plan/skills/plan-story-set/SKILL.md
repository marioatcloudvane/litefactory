---
name: plan-story-set
description: Split agreed parent scope into independent, ordered stories without inventing or dropping scope.
requires: [plan-evidence]
---
# Story set
Start from the parent thread and the saved research scope. List every deliverable,
decision and requirement they settled, then group them by independent outcome: an
outcome that can be built, checked and accepted on its own is one story. Several
independent outcomes are several stories; one deliverable is one story. Never cut
one outcome into layers, chores or "part 1/part 2", and never hide several outcomes
in an umbrella story.

Account for all agreed scope. Each item lands in exactly one entry; say in the
comment what you left out and why. Do not add scope nobody agreed.

Boundaries: give every entry a one-line intent of at most 280 characters that says
what the story delivers and where it stops, in plain language, with no file paths
or Go identifiers. Names are short, single-line and unique, at most 80 characters.

Order and dependencies: give every entry a temporary id (`s1`, `s2`, ...) that is
unique inside the set and means nothing outside it. List entries so prerequisites
come first. Use `depends_on` only for a real prerequisite: an entry that cannot
start, or cannot be accepted, before another. It names ids from this set, never the
entry itself, and never forms a cycle. Independent entries stay independent.

Size: a set has 1 to 12 entries. More than 12 means the parent is too large for
one plan; ask the owner to split the parent instead of squeezing the list.

Deliver the set as a storyset in your shared result; the factory shows it to the
owner and creates the sub-issues only after the owner promotes it. Offer revisions
for owner review, not owner promotion. Reuse settled answers when the owner asks to
merge, split, rename or reorder, and keep the parts they did not change. Leave story
content absent: no drafts, architecture, tasks, acceptance criteria or placeholders.

Ordinary owner comments can arrive at any Plan state. Answer questions about the retained set and stories in result.comment with a completed result, an empty plan payload, no questions and neither storyrequest nor storyset. Only return a revised set when the owner asks for a supported change to an unpromoted set. Replacing a promoted set is unsupported: explain this in an answer rather than proposing a replacement. Answering never grants approval.
