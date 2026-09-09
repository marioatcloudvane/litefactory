# Decision: evidence for nested coding agents

Status: adopt externally captured evidence under the existing run sandbox.

## Boundary and attribution

Ordinary top-level litefactory execution launches the configured agent to edit
files and run offline checks. This decision does not prohibit that workflow.
A managed run cannot promise live nested Codex observation under the current
boundary: the supplied [issue #183](https://github.com/marioatcloudvane/litefactory/issues/183)
reports nested Codex 0.153.4 failing before a model call while initializing its
in-process app-server, although the same invocation succeeded in an ordinary
shell. The PATH-alias warning was an earlier symptom, not a sufficient diagnosis.
The report also says a Claude probe succeeded inside a run; that difference is
attributed to the supplied issue, not established here or generalized to all
coding agents, versions, hosts, or sandbox configurations.

Use operator-controlled capture outside managed runs, sanitize the stream, and
review its provenance before committing it as an offline fixture. Follow the
[external capture procedure](../agent-evidence-capture.md). Captures support only
represented historical observations in their documented context. A synthetic
fixture tests parsing or classification, not authentic runtime capability.
Neither kind alone proves present-day delivery, conversation lifecycle, sandbox
compatibility, or end-to-end runtime behavior. Missing authentic evidence remains
an unsatisfied obligation. Do not silently remove promised scope or relabel it as
verified because live observation is unavailable.

Do not expand sandbox permissions, add a fallback agent, or make a live coding
agent capture an implementation/CI requirement to satisfy an evidence criterion.
A proposal to change this security boundary requires separate scope and threat
review; this decision supplies no such authorization. Direct stderr diagnostics
and manual recognition of nested command output are described in
[initialization troubleshooting](../codex-initialization-troubleshooting.md).

## Inventory before acceptance criteria freeze

Requirements engineering reads this decision before defining affected criteria.
Story authoring carries the inventory in each affected story's full body; the
planning orchestrator checks the specialist handoff for inventory completeness
and unresolved prerequisites before freeze. Shared decisions remain in
`plan_brief.md`; its Story Index stays link-only. Use a row per affected criterion:

| Criterion | Proof method | Source availability | Version/context bounds | Missing prerequisite / disposition |
|---|---|---|---|---|
| AC identifier matching the criterion | Offline assertion, scoped documentation review, or separately authorized external observation | Exact supplied/repository source, authentic or synthetic, or unavailable | Known version, environment, relationships and claim limits; explicit unknowns | Evidence or review still needed; retain an unsatisfied obligation if absent |

Reject a method that requires unsupported live nested Codex observation inside
a managed run. Do not equate an available parser test with an unavailable live
capability check. Record unknown facts and assumptions explicitly. If the promised
behavior cannot be established by the proposed method, retain the criterion and
its prerequisite rather than weakening the promise. Any change to a frozen
contract must use factory-mediated replanning; local notes and live issue edits
do not replace supplied approved revisions.

A representative review of an obligation to prove distinct child-model
attribution would record: proof method = offline assertions over an authentic
external capture; source availability = unavailable in the supplied narrative;
version/context = capture version and parent/child identities unknown; missing
prerequisite = sanitized stream with provenance and preserved identity/model
relationships. The obligation remains unsatisfied. A synthetic stream can test
identity handling but cannot settle that attribution obligation, and an authentic
historical stream would still not prove present-day behavior.

## Historical recovery is a separate review

The supplied #183 narrative references
[#73](https://github.com/marioatcloudvane/litefactory/issues/73) for child-model
attribution and [#125](https://github.com/marioatcloudvane/litefactory/issues/125)
for delivery and lifecycle acceptance. These are supplied historical reports;
they were not revalidated. Their current status and full approved criteria were
not supplied. This decision neither changes those issues nor claims recovery.

Before claiming recovery, obtain factory-supplied approved criteria and examine
each criterion against an inventory of actual available evidence, including
additional live or lifecycle obligations a fixture cannot prove. Use the
[capture procedure's bounded mappings](../agent-evidence-capture.md) as planning
aids only. Changes to unfinished frozen scope require factory-mediated replanning
where applicable, followed by criterion-named verification. Unavailable evidence
continues to block the affected obligation; historical references are attribution,
not proof that deferred scope has been completed.
