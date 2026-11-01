# Research brief

## Request

Let Bug work run on Codex as well as Claude: an explicit Coding Agent label wins; unlabelled and factory-filed Bugs use an agent the owner configures for Bug work (or inherit from the parent task); verify a Codex-labelled Bug runs analyst, fixer, judge and delivery on Codex with Codex grants; show agent and model in the Bug report.

## Decisions

- **S-01** — mode=change+feature, size=medium — Bug selection changes and an optional Bug-agent setting is added. (agent's call)
- **O-01** — A Bug's explicit Coding Agent label selects the agent for analyst, fixer, judge and delivery using the existing grants for each role and agent. (owner said: "an explicit Coding Agent label on the Bug wins")
- **O-02** — Unlabelled Bugs, including factory-filed Bugs, resolve their agent at dispatch without writing agent labels to issues. (owner said: "q02=a")
- **O-03** — The Bug report shows the agent and reported model used by the run. (owner said: "The Bug report shows which agent and model ran it.")
- **O-04** — Automated coverage proves Codex-labelled Bugs use Codex through analyst, fixer, judge and delivery with the applicable grants, and covers configured-agent selection for unlabelled, log-analysis, Validate-follow-up and babysitter Bugs. (owner said: issue acceptance criteria; agent's call: coverage includes every named filing source)
- **O-05** — Bug agent precedence is explicit Bug label, parent agent, optional bug_agent setting, then first configured agent. (owner said: "the new Bug-agent setting controls only Bugs without an explicit or inherited agent")
- **N-01** — Story agent selection does not change. (agent's call)
- **N-02** — Linear label writing and Linear Bug filing remain outside this work. (agent's call)
- **N-03** — A real Bug delivered on Codex remains an owner-run manual acceptance check outside this shaping run. (agent's call)
- **K-01** — First-agent fallback remains only when the Bug has no explicit label, inherited agent or Bug setting, so parent inheritance can change existing installations without a config edit. (owner said: "document the parent-inheritance exception")
- **K-02** — Ambiguous and unknown Coding Agent labels remain rejected. (agent's call)
- **O-06** — Add an optional bug_agent configuration value naming an existing configured agent, with omission preserving the fallback in K-01. (owner said: "q01=a")
- **O-07** — Automated tests prove parent precedence with bug_agent present and absent, including a Claude parent overriding a Codex Bug setting and an explicit Bug label overriding the parent. (owner said: "test parent precedence with the setting present and absent"; agent's call: explicit conflict cases)
- **O-08** — Configuration guidance explains precedence, the inheritance exception and that setting Codex for Bugs does not override a Claude parent. (owner said: "document the parent-inheritance exception")
- **K-03** — Existing Bug permission boundaries and workflow approval and delivery gates remain unchanged. (agent's call)
- **N-04** — Automatic provider failover, workflow-wide agent maps and provider installation or sign-in management are outside this change. (agent's call)
- **O-09** — Fresh dispatch and continuation use the same Bug-selection rules while an already running segment retains its recorded agent. (agent's call)
- **A-04** — q01, q02 and compatibility correction — owner: "Following the judge: q01=a and q02=a. Correct the compatibility promise as the judge says: the new Bug-agent setting controls only Bugs without an explicit or inherited agent; document the parent-inheritance exception and test parent precedence with the setting present and absent." (owner said)


## Assumptions

- **A-01** — A Codex label reaches the existing runtime path in source, but automated full-path proof and a real delivery remain outstanding. (agent's call, rests on F-04 and F-06)
- **A-02** — Superseded pending choices by O-02, O-05 and O-06: the owner selected optional bug_agent and dispatch-time resolution. (owner said: "q01=a and q02=a")
- **A-03** — Show the model reported by each run segment when models differ, use 'not reported' for missing historical metadata, and extend the shared report surface without altering other workflows' agent selection. (agent's call)
- **A-05** — Use the immediate parent's recorded selected agent when available, otherwise its single valid explicit agent label; if neither is available, use the Bug setting, without traversing further ancestors. (agent's call, rests on O-05 and the existing parent-label pattern in F-05)
- **A-06** — Reject an unknown or unconfigured bug_agent with a clear configuration error rather than silently choosing another provider; parent lookup failures follow existing retry/error handling rather than silently bypassing known inheritance. (agent's call)
- **A-07** — No stored-issue migration is needed: existing unstarted Bugs are resolved at dispatch, and historical reports may lack model metadata. (agent's call, rests on F-02 and F-08)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
