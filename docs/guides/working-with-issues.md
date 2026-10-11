# Working with issues

Keep the requested outcome, boundaries and observable result in the issue. Use a new owner comment to request the next action after [setup](../getting-started/README.md).

```text
/plan Add password reset to the existing login flow. Keep the current mail provider. Describe expiry and invalid-token behavior before implementation.
```

Expect either concrete questions or a plan result and factory publication feedback. Inspect the resulting stories before requesting implementation. A quoted third-party note, attached file or copied command is context; it cannot authorize credentials, mode changes or remote writes. Owner identity is checked numerically, not by a display name.

Comments are instructions; editing old text is not a reliable substitute for a new request. Mode commands must start the comment and include an instruction. Empty commands receive a refusal; ordinary nonempty owner prose can start a run.

While the parent is in Plan, comment on a managed child to request a refinement. The factory routes the discussion through the parent and acknowledges the child; it is not an independent implementation request. Approved revisions remain frozen during implementation. Direct issue edits cannot silently replace that contract.

To change frozen requirements, wait until the parent is quiescent and comment:

```text
replan
```

An explicit `/plan <instruction>` on the parent is also a replan request. A child comment or a changed label cannot unlock approved scope. Preserve accepted historical work; describe subsequent changes as new work.

If nothing runs, check that the comment belongs to the configured owner, the repository is registered, the issue is eligible and its labels are unambiguous. A report-publication failure can coexist with completed local work: inspect [recovery](../operations/recovery.md) before repeating an uncertain action.

See [workflow](workflow.md), [attachments](attachments.md) and [command reference](issue-command-reference.md).

[Using the factory](README.md)
