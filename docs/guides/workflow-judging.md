# Optional workflow judging

Research and Plan profiles can add an `lf_judge` node to challenge a saved source. Shipped graphs remain unchanged. Judging retains a private report; a separate advisory comment activity controls publication. Existing owner reviews and approval gates still apply.

The judge requires an explicit `lf_source` naming an `lf_agent` node in the same captured graph instance, a positive `lf_limit`, and exactly the `success,failed` outcomes. It pins that source node's latest completed attempt. A failed, question-only, missing or otherwise ineligible latest result is explained without selecting older work or another node. Only saved substantive Research results and Plans are eligible; a saved draft need not have owner approval.

Optional overrides are `lf_agent=claude|codex|other`, `lf_model`, `lf_thinking`, `lf_focus` and `lf_budget` (1–60 minutes). These use the [shared judging settings](judge.md). `other` means the opposite of the recorded source provider. One availability fallback may use the source provider; invalid settings and isolation failures do not permit fallback. The shared total budget, any shorter node budget and cancellation cover both launches.

This fragment inserts judging before an existing owner review:

```dot
source [lf_action=lf_agent, lf_outcomes=success];
challenge [lf_action=lf_judge, lf_source=source, lf_limit=1,
           lf_agent=other, lf_focus="Check assumptions and missing evidence",
           lf_budget=10, lf_outcomes="success,failed"];
share_advice [lf_action=lf_comment, lf_kind=advisory,
              lf_source=challenge, lf_outcomes=published];
owner_review [lf_action=lf_ask, lf_kind=review,
              lf_source=source, lf_outcomes=published];

source -> challenge [on=success];
challenge -> share_advice [on=success];
challenge -> share_advice [on=failed];
share_advice -> owner_review [on=published];
```

Embed the fragment into the mode's complete profile and preserve its required question handling, source effects and approval paths. Without `share_advice`, the retained report stays unpublished. A valid report follows the same success edge whether its verdict is no material concerns, concerns found or unable to assess. Failed execution retains an explanation that an explicit advisory comment can publish. Judge results cannot drive conditions, loops, owner questions, Plan story effects, generation or approval.

The advisory comment selects the named judge node's completed attempt in the same instance. It pins the rendered report bytes and digest before delivery, opens no owner wait and invokes no source publication or Plan promotion hooks. Delivery recovery reuses the retained bytes and existing journal. Uncertain remote delivery remains pending; no exactly-once guarantee is made.

Recovery preserves the original pinned request and uses an already retained report without another model call. An abandoned reserved attempt with no retained request or safely recoverable report fails explicitly. A new graph entry creates a new attempt, subject to its positive limit. Judge authority is limited to reading captured evidence and submitting advisory reports; it grants no source editing, test execution, forge access or owner approval.

Workflow records referenced by a retained judge report stay in private local storage, including after the issue closes. Archiving keeps those records so the report's original source and attempt identities remain intact.
