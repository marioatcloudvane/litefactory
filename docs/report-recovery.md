# Report delivery recovery

An unready `/implement` receives one explanation that no agent launched. After
readiness changes, submit a fresh command. Retrying delivery of the explanation
never turns the refused command into an invocation.

Reports and replies retain their exact final redacted content, destination,
source identity and delivery marker in local history. This content is not
pruned automatically. Delivery states distinguish:

- **Pending:** intent exists but dispatch has not begun; safe dispatch can retry.
- **Uncertain:** dispatch may have reached GitHub; only a trustworthy matching
  comment or confirmed receipt can resolve it. An empty or failed search is not
  proof that nothing was posted.
- **Confirmed:** the comment identity is known; retries perform local completion
  only. Deleting that comment remotely does not cause its recreation.
- **Completed:** local delivery accounting and the source acknowledgement were
  committed together.

The first recovery is due after the configured poll interval. Failed eligible
recoveries double the delay up to one hour. A later GitHub rate-limit hold takes
precedence. Due times and recovery counts survive restart; a temporary local
storage outage retains scheduling debt in the running daemon until storage can
save it again. A startup failure without a tick timestamp is conservatively
scheduled one interval after the first tick.

Structured `report recovery pending` diagnostics appear on the first failure,
once at the third failed eligible recovery, and on a changed delivery state.
They identify the task, delivery/source, optional run, redacted reason and next
eligible attempt. `report recovery resolved` records successful repair. The latest redacted
diagnostic and escalation flag remain stored with the resolved state, which
does not schedule further attempts. Repeated
unchanged failures do not generate another alert each tick. Unresolved requests
preserve source ordering, while their waiting time releases worker capacity for
other tasks.

Recovery continues safely at the one-hour cap; reaching a count never authorizes
another possibly duplicate post or discards a request. Equivalent existing
comments are preserved and the lowest trustworthy comment ID is adopted.
Conflicting content, legacy task-only markers and unverifiable historical
content remain unresolved. If trustworthy evidence never becomes available,
investigate the recorded task/delivery identity, remote comments and storage
failure. There is no force-resend or skip control; do not repair this by editing
the runtime database or by deleting evidence.

An observed stopped process whose outcome cannot be saved retains task ownership
without retaining a worker slot. It follows the same bounded retry cadence and
cannot launch again or report until its outcome is durable. A process restart
uses exclusive orphan reconciliation; never-launched preparation remains
retryable, while interrupted launched runs become terminal outcomes.
