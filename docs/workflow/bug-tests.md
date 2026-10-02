# Bug test child

`bug_tests.dot` is a separate persisted Bug graph, limited to regression,
focused and full phases in that order. Each phase has a single dispatch budget;
only a verified passing phase advances. The child has no agents, result-service
capabilities, nested calls or delivery activities.

`CreateBugTestCall` binds the parent attempt, completed repair, source/tree,
trusted recipe digest, main target and environment/permission revisions. Parent
and child definitions must belong to the same captured package. Child creation,
parent suspension and initial daemon dispatch are one transaction. A replay
returns the same call and refuses changed inputs.

The parent releases its worker and task lock while suspended. Durable child
ownership excludes fixer/save work and foreign check operations between phases.
The daemon routes a recorded child dispatch directly to `BugTestChild.Advance`,
which performs one graph node through the normal graph executor. Factory check
inputs come from the constructor's resolver, with exact equality to the retained
phase binding. Missing check execution configuration cannot launch a fallback.
The parent package composition supplies `daemon.Options.BugTests`; standalone
child support does not enable an incomplete parent workflow.

A child outcome is derived from retained phase and command evidence, never from
`GraphSucceeded`. Settlement and parent wakeup are atomic. The suspended parent
attempt is completed only when its call is consumed, atomically with its outgoing
transition. Before consumption, the parent activity reobserves the source, recipe,
target and execution context. Invalidated evidence retains its original binding.

On restart, reconcile calls before generic orphan handling. The check activity
owns process reconciliation: completed records are reused, unknown launches and
cleanup stay unresolved, and a stopped interrupted process is blocked. No PID is
signalled by the child. A durable cancellation prevents command launches even
when its phase record does not yet exist. Cancellation remains unresolved until
cleanup is confirmed; requesting it cannot manufacture a cancelled result.

A fresh generation requires a consumed quiescent blocked call (or retained
passing history invalidated by drift), a distinct factory operation naming the
resolved cause, and revalidated repair/source inputs. It reruns all three phases.
Neither timer dispatch nor ordinary assertion failure grants a generation, and
known flakiness on the same binding remains blocked. Analysis and repair counters
are never reset or incremented by testing.

These mechanisms do not establish host isolation by themselves. The configured
check executor enforces that boundary; unsupported hosts refuse execution.
