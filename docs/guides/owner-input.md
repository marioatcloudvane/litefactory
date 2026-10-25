# Owner instructions and standing guidance

Ordinary authenticated owner comments are one-time instructions. Litefactory
retains their exact admitted text before acknowledging ingestion. If a workflow
is already handling another answer, the instruction stays **undelivered**. A
later eligible agent step receives it once, in comment order. Eligibility means
a substantive `lf_agent` invocation that accepts current owner direction;
read-only review fan-out receives standing guidance separately. A polling,
reporting, or transport activity cannot consume it. Retention does not start a
new run, reopen completed scope, or grant new authority. If there is no next
step, the instruction remains retained and visibly undelivered.

An optional explicit first line targets an existing receiver:

```text
Input-Target: instance=123 node=worker_agent
Use ISO dates in the existing shared formatter.
```

The instance and node identify the exact workflow step. An optional `launch=456`
adds the existing launch identity. Intervening steps cannot take that input, and
a replacement workflow instance does not inherit it. These are explicit numeric
identities from the retained workflow; mentioning a step in ordinary prose does
not select a target. The pending-input response lists the retained comment and
its target. Until that exact eligible step exists, the instruction stays
undelivered.

Assignment is local and durable: the saved source comment, receiver attempt,
and exact text survive a crash. If crash recovery allocates another attempt for that same logical node and
launch, it reuses the saved input without changing the original assignment. A
successful invocation ends that input window. Resuming that logical step uses
the same input;
later steps do not receive it again as current direction. This does not promise
exactly-once execution by an external provider.

`/guide <text>` sets optional standing guidance for this issue. Another `/guide
<text>` replaces it; `/guide` shows it; `/guide clear` withdraws it. Revisions and
their authenticated source comments remain retained. Guidance is separate from
ordinary instructions, approvals, shipping waiver reasons, and return
explanations. It never executes a decision or expands approved scope.

Cold and resumed worker prompts receive a retained guidance revision outside the
bounded historical notes. A run keeps the revision it received through
prelaunch retries. Later runs receive the current revision, including an
explicit withdrawal after clearing guidance. Relevant Validate reviewer prompts
also receive standing guidance within their existing read-only permissions.
