---
name: owner-report
description: Write any owner-facing comment, question or report — opens with an AI;DR line, stays compact, uses a small Mermaid diagram when a picture is faster, and explains outcome, decisions, evidence limits and next action within the existing result contract.
resources:
  - mode-guidance.md
  - examples.md
---
# Owner reports

Read [mode guidance](mode-guidance.md) for your role and [representative examples](examples.md). Apply this guidance to your existing owner-facing comment, question and report-text fields. It changes prose, not the result schema or route. Your role and result skill still control responsibilities, evidence, typed findings, questions, approvals, publication, submission and workflow gates. Preserve required preflight, submit and receipt steps.

Open every owner-facing comment with `AI;DR:` — one or two lines: the result, and a suggestion of what the owner could add or decide next. No preamble before it.

Lead the rest of an initial report with what the outcome means for the owner's work. On a rerun, lead with what changed since the supplied prior review, then retain unresolved blockers. Do not repeat the whole earlier review. If that review is unavailable, say so; use only supplied history and permitted current evidence. Do not fetch prohibited archives or infer progress elsewhere.

Compact by default. Aim for about 120 words of prose, excluding structured evidence and questions; a routine update may need only the `AI;DR` line. This is a target, not a limit; keep safety-relevant detail, uncertainty and usable recovery steps even when they need more space. Use plain language and explain specialist terms only when necessary. There are no fixed sections: no headings, tables or lists by reflex — add structure only when it makes the comment easier to read.

Visualise when a picture makes the point faster than text; you decide. Prefer a small Mermaid diagram in a ` ```mermaid ` fenced block (`flowchart`, `sequenceDiagram`, `stateDiagram`) — a handful of nodes, short labels. GitHub renders Mermaid in issues and comments; Linear renders code blocks with language `mermaid` as diagrams. While shaping, a question or option comparison may carry one small diagram per option so the difference is visible.

Keep evidence clickable: link to a permalink at a fixed commit where you can. Long evidence belongs in the technical document, not the comment. Never put secrets, tokens or local machine paths in anything the owner sees.

Explain material decisions and why they were made, connecting relevant evidence to the conclusion. Separate observations, inferred causes and unknowns. State what the checks establish and their limits; never turn unavailable proof into a confirmed defect or a successful check into a delivery claim. Keep required structured evidence and findings alongside the prose.

Finish with the supported next action and who owns it. When no owner action is needed, say so and explain what happens next, but only as current supplied evidence supports. If the next step is unknown, say what is unknown rather than inventing a command, restart route, target or promise of continued work. For a stop or wait, name the affected work and any independently continuing work known from the evidence; disclose an unknown wider impact.

When your role authorizes a question and a choice is needed, explain what is held up, give supported options and their effects, and recommend one with a reason. Use existing question fields. Include prerequisites, the exact reply or command and destination only when supplied, the authorized actor and any evidence only the owner can provide. Keep independent choices distinct. This skill grants no permission to ask, wait, approve, publish, select a disposition or change modes.

For a review or wait, distinguish the ready candidate from its pending approval. Ordinary feedback, continuation and restart do not establish approval or advance a stage; explain only the current gate and supported action supplied to your role.

For a shortened report, keep the outcome, immediate action and consequence visible. Refer to a full report only at an existing supplied destination that retains the omitted evidence and recovery steps; a link alone is not an explanation. An input receipt acknowledges input: it proves neither acceptance, execution, approval nor publication. Report engineering state, acceptance and remote effects separately.
