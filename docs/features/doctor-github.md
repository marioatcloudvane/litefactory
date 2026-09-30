# GitHub doctor conversations

`/doctor <instructions>` diagnoses the issue carrying the comment. Every turn
requires its own prefix. `/doctor stop` ends the active doctor generation while
preserving history. `/doctor apply` currently explains that repair is unavailable.
Commands from anyone except the configured numeric owner are ignored.

GitHub requires the daemon. The independent CLI conversation remains the fallback
when it is down. Native provider adapters remain disabled until their isolation
has authentic observations; input and the explicit failure are still retained.

The daemon reserves one diagnosis worker outside the normal workflow pool. This
adds one bounded worker to the documented CONC-2 budget (maximum twenty-one),
joined by Quiesce and shutdown. Discovery and stop use the existing tick goroutine.
Diagnosis never cancels normal workflow work or advances normal watermarks.

A tick reads one twelve-comment repository page, then verifies at most twelve
owner targets. The fixed scan window and page persist separately from normal
polling. Initial activation skips older comments; source IDs deduplicate overlap
and preserve admitted text even if the remote comment is edited. Request accounting
uses the existing discovery budget and real quota observations, with operational
capacity reserved ahead of background discovery. Local admitted work runs during
remote backoff.

Only one diagnosis per issue writes its shared CLI/GitHub conversation. Busy CLI
ownership requeues the GitHub input. Stops fence queued and claimed older input,
including delayed discovery. Restart recovery adopts a retained accepted result
without another invocation. Missing results permit one recovery attempt; another
interruption requires a new command.

Replies are redacted and retained before posting in the existing report ledger,
with independent doctor ownership. Numbered parts preserve full proposal details;
presentation completes only after every part is confirmed. Uncertain posts are
observed through bounded paginated reads and exact body, marker, destination and
numeric bot provenance. Absence never authorizes a blind repost. Publication debt
rotates independently of later diagnosis and stop.

Controlled SQLite, daemon and HTTP tests cover these boundaries. They do not
establish authentic native provider isolation or live GitHub delivery.
