// Package github is litefactory's client for the GitHub REST API.
//
// # Concrete, not abstract
//
// This package exports a concrete [Client] and concrete result structs. There
// is no Forge interface and no capability descriptor: the second forge is
// LF-18's problem, and its protocol will be *extracted* from this working
// implementation rather than guessed at in advance. A file named forge.go in
// M0 is a review blocker.
//
// # No SDK
//
// Six endpoints do not justify a dependency. Everything here is net/http plus
// the Link-header helper in pagination.go. The rule for revisiting that: if a
// third pagination special case appears, reach for a library. Two do not.
//
// # Identity is numeric
//
// Every actor this package returns carries both a numeric id and a login, and
// callers must compare the id (D-I). Logins are renameable, and a fine-grained
// PAT on a machine *user* account reports type "User" rather than "Bot", so
// neither the handle string nor the forge's bot flag is a sound identity check.
// The logins are for display.
//
// # The credential
//
// The [secrets.Token] handed to [New] is revealed at exactly one place in this
// package — where the Authorization header is set, in [Client.do]. It is never
// logged, never placed in an error, and never put in a URL. The typed errors
// here carry a method, a URL, a status and GitHub's own message, all of which
// are safe to print.
//
// # Failure is typed
//
// Non-2xx responses become typed errors — [RateLimitError], [UnauthorizedError],
// [ForbiddenError], [NotFoundError], [ValidationError] — each of which also
// unwraps to a sentinel for errors.Is and to [APIError] for the shared response
// detail. The daemon reads [RateLimitError.RetryAfter] to drive its backoff, so
// that value is a field on a type rather than prose inside a message string.
//
// This package retries nothing and sleeps nowhere. Backoff is the daemon's job,
// because only the daemon knows what else is queued behind the failing call.
package github
