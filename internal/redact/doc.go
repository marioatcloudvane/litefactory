// Package redact strips credential-shaped text out of every body litefactory
// sends to GitHub.
//
// # What this package is for
//
// litefactory posts comments on public issues. Those comments quote the tail of
// a run's output, which is whatever `claude` happened to print — a stack trace,
// a `curl -v` transcript, an error message from a tool that echoed its own
// configuration. Any of those can carry a credential. This package is the last
// transformation applied before the API call, and it is applied unconditionally
// (S-7).
//
// # This is hygiene, not a security boundary
//
// Be honest about what pattern matching buys. An agent that wants to exfiltrate
// a secret can base64 it, split it across lines, or spell it out in words, and
// no matcher in this package will notice. What this package catches is
// *accidents*: a tool that logged its own Authorization header, a stack trace
// carrying an environment dump, a git remote URL with a password in it.
//
// The real boundary is internal/harness, which does not hand the subprocess the
// credential in the first place. This package is the second layer, for the
// mistakes that happen anyway. Do not treat a passing redaction test as
// evidence that it is safe to give an agent a secret.
//
// # Outbound only, by design
//
// Redaction is applied to forge-bound bodies and never to $LF_HOME logs. The
// local copy is deliberately unredacted: at 23:00, debugging a failed run, the
// verbatim output is the thing you need, and it is on your own disk under
// umask 0o077. AC-5.7 asserts a token-shaped string is present verbatim in the
// local JSONL while AC-4.6 asserts it is absent from the outbound body — those
// two criteria are the same design decision seen from both ends.
//
// There is deliberately no log-redaction entry point here. Do not add one.
//
// # The compiler-enforced part
//
// [Body] is opaque: its fields are unexported and there is no exported
// constructor, so the only way to obtain a non-empty Body anywhere in the
// module is [Redactor.Apply]. github.PostComment accepts a Body and never a
// string, which makes "post an unredacted body" inexpressible without editing
// this package. A Body zero value is empty and reports Valid() == false, so it
// is not a silent bypass either.
//
// # The single recover in the tree
//
// A panic inside the redaction pass means the pass did not finish, which means
// the body may still contain a credential. [Redactor.Apply] recovers that panic
// and turns it into an error, so the caller aborts the post, the run stays
// un-`reported` and the watermark does not advance (AC-4.9). This is the only
// recover in litefactory and it is load-bearing — see the comment on
// Redactor.Apply before removing it.
//
// # Ordering
//
// Redaction changes length. Usually it shrinks a body, but a match shorter than
// the ten-byte "[redacted]" literal grows it, and a body full of eight-character
// environment values grows measurably. So the size limit cannot be applied
// before redaction alone, and re-truncating after redaction risks a
// truncate-redact loop. [Redactor.Apply] owns the whole sequence — truncate to
// [Budget], redact, assert, hard-truncate at a safe boundary — so that a caller
// cannot sequence it wrongly.
//
// # Dependencies
//
// This package imports the standard library and internal/secrets only. Zero
// third-party imports is a named global invariant, enforced by
// TestPackageHasNoThirdPartyImports.
package redact
