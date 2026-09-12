# Session setup

Reuse the supplied session, including unfinished context across replies, dates
and retries. Otherwise derive `.aiteam/<branch>/<date>/<feature>/`: branch is the
current branch lowercased with slashes replaced by hyphens; date is YYYY-MM-DD;
feature is the supplied slug, lowercased, specials replaced by hyphens, at most
40 characters. Never scan archived sessions to choose a path.

Outside Validate, scaffold MANIFEST.md before delegation with branch, session
date, unchecked Research/Plan/Implement/Validate/Spike entries, History and Notes.
Reuse an existing manifest. Each mode updates only its own entry and history;
incomplete work remains unchecked. Validate uses supplied context read-only,
creates no session or artifacts, and returns structured findings.

Under `.aiteam/`, read/write only the supplied session; project APP_CONTEXT.md,
relevant APP_CONSTRAINTS.md and optional rulebook.md are the only read exceptions.

Research writes RESEARCH_BRIEF.md. Plan follows its supplied publication and
working-context contract; unfinished context suffices to continue. Implement uses
the supplied frozen story bundle, never local/live story substitutes. Spike writes
SPIKE.md. A human selects each mode; completion never starts the next mode.
Research and Plan can ask material questions unless explicitly prohibited: save
incomplete findings, return one final report and never wait for an answer.
