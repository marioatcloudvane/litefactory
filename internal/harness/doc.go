// Package harness runs one headless Claude Code process and captures what it
// did.
//
// It is the Claude implementation of [agent.Runner] — internal/agent holds
// the vendor-neutral request, result and permission types this package's
// [Runner] is built from and returns; the aliases in result.go exist so the
// many call sites written before that split existed keep compiling. It is
// also the only place in litefactory that executes the claude binary, and it
// is the real security boundary of the product. Everything below is either an
// empirically verified finding from the T-LF3.0 de-risking spike or a
// consequence of one; none of it is stylistic.
//
// # The invocation
//
//	<absolute claude> -p --output-format stream-json --verbose \
//	  --session-id <uuid> --tools <surface> --settings <file> \
//	  --allowedTools <rules...> --strict-mcp-config
//
// The first run of a conversation is the one shown above. Every run after it
// replaces --session-id <uuid> with --resume <uuid> and changes nothing else:
// the two flags are mutually exclusive, and passing --session-id a second time
// does not continue the conversation, it exits non-zero with "Session ID <uuid>
// is already in use". The tool boundary is identical on both — a resumed
// session resolves --tools from this argv and does not inherit a wider surface
// from the transcript it is continuing.
//
// --verbose is mandatory. Without it claude hard-errors and writes nothing at
// all to stdout, which looks exactly like a hung run. The binary is an absolute
// path resolved from configuration and never by searching the ambient PATH:
// launchd hands a LaunchAgent PATH=/usr/bin:/bin:/usr/sbin:/sbin, which does
// not contain a Homebrew install (SD-7).
//
// The prompt is delivered on stdin, never in argv (AC-3.12). That removes the
// ~1 MB macOS ARG_MAX ceiling for a large issue body, and it keeps the prompt —
// which contains third-party text — out of every ps listing on the machine.
//
// # The tool boundary is --tools, not --allowedTools
//
// --allowedTools is *additive* to the operator's own ~/.claude/settings.json.
// Measured (SD-2): 250 ambient allow-rules leaked through, and Bash executed
// while absent from --allowedTools. Treating it as the sandbox is a hole.
//
// The boundary is --tools, which restricts the built-in tool surface claude may
// use at all; the init event's .tools array reflects it. --settings points at a
// litefactory-owned file ([WriteSettings]) so the run does not depend on the
// operator's personal permissions, hooks or project MCP servers, and
// --allowedTools is kept as a second, narrower layer.
//
// Two residual risks are accepted rather than solved, and they are real:
//
//   - claude runs *inside the worktree*, so it reads that branch's CLAUDE.md,
//     .claude/settings.json and hooks — and hooks execute. A branch is
//     third-party content. --strict-mcp-config removes the .mcp.json half of
//     this. An optional --mcp-config registers only the factory's attempt-local
//     server with exact installed tool permissions; the hook half remains.
//   - --settings supplies *additional* settings; it does not stop personal
//     agents and skills under ~/.claude from being discovered. Verified: a run
//     with --settings still reported the operator's agents in its init event.
//     Only --tools reliably narrows what can actually be called.
//
// # The secret boundary
//
// The child's environment is built from an explicit allowlist (see [buildEnv]
// and [AllowedEnvNames]) and never from os.Environ. Under github.token_env the
// bot token is a variable in the daemon's own environment, and an agent holding
// Bash can simply read it.
//
// Redaction is hygiene, not a boundary. An adversarial agent can base64 a
// secret past any pattern matcher, so nothing here relies on recognising a
// secret on the way out. The boundary is not giving the subprocess the secret
// in the first place.
//
// # Three outcomes, not two
//
// Never branch on the result event's .subtype: "success" appears even on
// is_error: true runs (SD-3). Branch on .is_error.
//
// Beyond success and a non-zero exit there is a third outcome: no terminal
// result event at all. A stale credential makes claude retry internally about
// ten times and then hang indefinitely, emitting nothing. Only the hard
// wall-clock timeout ends that, which is why the timeout is independent of any
// event-driven completion detection. Timeout, abandonment (the daemon's context
// was cancelled) and a plain non-zero exit are recorded as distinct reasons
// (S-9, AC-3.20).
//
// # Killing the process tree
//
// SysProcAttr{Setpgid: true} plus kill(-pgid) does not reap claude's
// grandchildren: measured 2 of 2 survivors reparented to PID 1, because the
// Bash tool setsids its shell into a new session and process group (SD-1). The
// mechanism is [killSubtree] — enumerate every pgid in the process subtree,
// signal each, then re-scan once because a tool may spawn between the
// enumeration and the kill. Setpgid, cmd.Cancel and WaitDelay remain as the
// outer safety net.
//
// # Goroutines
//
// Exactly two, both started by [Runner.Run]: one reading stdout, one reading
// stderr. Neither can outlive the call. Both drain concurrently with cmd.Wait —
// which is what stops a full pipe buffer from blocking the child forever — and
// both are joined before anything they wrote is read. If a descendant that
// escaped the kill sweep is still holding a pipe open once the process has been
// reaped, the read ends are closed from this side after a grace period and both
// goroutines return at once.
//
// stdout and stderr are this package's own os.Pipe rather than cmd.StdoutPipe,
// for that last reason: os/exec closes the pipes it creates only from inside
// Wait, and a reader stuck on one would mean Wait is never reached.
//
// The stdout reader is bufio.Reader.ReadBytes, never bufio.Scanner. Scanner
// does not merely truncate an over-long line: it returns ErrTooLong and aborts
// the loop, silently losing the terminal result event when Err is unchecked.
// Real lines of 72,411 bytes were measured from an ordinary file read (SD-6).
//
// # Resuming a failed run does not resume its claims
//
// [PromptInput.PreviousFailed] and [PromptInput.PreviousReason] carry the
// previous run's own success/failure verdict and stored error text into the
// prompt of the run that resumes it — internal/turn derives both from
// [store.Run.Succeeded] and hands them in already settled, the same way it
// already does for PreviousMayAsk and PreviousMode, because the failed run's
// own transcript cannot be trusted to say so: the last thing claude wrote
// before a failed run ended often reads like a finished, successful report.
//
// [writeResumeHeader] turns PreviousFailed into a short corrective note,
// written in litefactory's own voice, telling the resumed run that its
// predecessor did not complete and that none of its claims are this run's
// accomplishments. PreviousReason is agent-authored text — on the timeout and
// abandonment paths it is litefactory's own words, but on the plain-exit path
// it is [excerpt] of claude's own final message, bounded to 200 bytes and
// newline-stripped but not sanitised for meaning. It is therefore never
// interpolated into the header directly: it is capped again independently
// ([MaxPreviousReasonBytes]), passed through [neutraliseNoteTags], and framed
// inside a <note> element exactly like every other piece of third-party text
// in the prompt, so a reason containing "</note>" or a forged "- Mode:" or
// "# litefactory run" line stays confined to its own element instead of
// reaching the header as structure.
//
// A cold run and a resume whose predecessor succeeded write nothing here at
// all — the branch is silent, not merely short — which is what keeps both
// kinds of prompt byte-for-byte identical to a build of this package from
// before the feature existed.
//
// # Artefacts
//
// A run owns two files, both under $LF_HOME and both unredacted (S-1):
// NNNN.jsonl, written line by line as events arrive, and NNNN.meta.json, the
// [Meta] document. [Runner.Run] writes the first; internal/turn writes the
// second with [WriteMeta] once it knows the shas and the files touched, on the
// normal path and best effort from a defer on the abnormal ones (S-3).
package harness
