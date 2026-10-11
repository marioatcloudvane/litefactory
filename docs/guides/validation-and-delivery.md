# Validation and delivery

Use this journey for a separated workflow with frozen managed stories. Engineering, acceptance, publication and readiness are independent results.

```text
/validate Assess the completed stories against their retained evidence.
```

Expect criterion findings and code/architecture/plan review findings based on retained evidence. Validate is read-only: missing execution or an authentic observation stays unverified. It does not run replacement tests. Implement owns fixes and fresh checks; a successful engineering checkpoint alone neither accepts nor closes the child.

The factory records acceptance, reports it, projects criteria and confirms child closure. Pending projection or remote effects stay pending. Previously accepted snapshots remain historical facts; new requirements do not automatically reopen accepted work.

The numeric owner can make a reasoned exception with `/waive <generation> <selectors> -- <reason>`. Review the [exact selector reference](issue-command-reference.md) first. Waivers retain failed/unverified findings and their scope; they do not fabricate test success or authorize a merge.

A proposed follow-up needs separate exact-content approval with `/followup <generation> <proposal-sha256> linked` or `attach`. Linked work remains a separate issue; attaching makes it a child and can block readiness until closed. Neither action waives the source requirement. Unknown creation results require receipt/reconciliation, not blind recreation.

After required acceptance and engineering/save obligations are satisfied and attached children are closed or explicitly excepted, request `/deliver <generation>`. The factory evaluates readiness and prepares a concrete PR/head proposal. Review it before using the reported command:

```text
/merge <generation> <proposal-digest> <PR-number> <head-SHA> main
```

Use the exact returned values. Approval is bound to that PR, current head and `main`; a changed head needs a fresh delivery proposal and approval. Parent closure follows confirmed successful integration, never a receipt or an uncertain merge response. A readiness exception does not fake a save or integration.

Legacy scopes retain their older combined completion route; these separated acceptance controls do not silently upgrade them. If blocked, inspect whether the missing item is engineering evidence, assessment, child state, save, PR state or remote confirmation before choosing the next action.

See [workflow](workflow.md), [command reference](issue-command-reference.md), [owner-exception details](../workflow/owner-exceptions.md) and [recovery](../operations/recovery.md).

[Using the factory](README.md)
