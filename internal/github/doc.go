// Package github is litefactory's client for the GitHub REST API.
//
// # Concrete, not abstract
//
// This package exports a concrete [Client] and concrete result structs. There
// is no Forge interface and no capability descriptor: the second forge is
// LF-18's problem, and its protocol will be *extracted* from this working
// implementation rather than guessed at in advance. A file named forge.go in
// M0 is a review blocker. Adopting an SDK did not change this — a dependency
// is not an abstraction, and [Client] wraps go-github as a struct rather than
// hiding it behind an interface nobody has a second implementation for.
//
// # The SDK
//
// The transport is **github.com/google/go-github/v90 v90.0.0**, pinned exactly.
// v90.0.0 was published on 2026-08-04 and was checked against proxy.golang.org
// on 2026-08-31: v91 and above do not exist, and v90 has no patch releases, so
// this is both the newest and the only version of the newest major. go-github
// ships one major per release. Its own go.mod asks for go 1.25.0 against
// litefactory's 1.26; it adds go-querystring v1.2.0 to the graph and pulls
// go-cmp at v0.7.0, the version already required here. The licence is
// BSD-3-Clause. "Latest" is not a pin and is not what this says.
//
// This section used to read *"# No SDK — six endpoints do not justify a
// dependency … the rule for revisiting that: if a third pagination special case
// appears, reach for a library."* That policy was reversed by the owner in
// August 2026, and it matters *how*:
//
//   - **The documented trigger did not fire.** There was never a third
//     pagination special case. The Link-header reader handled every endpoint
//     this package called, right up to the day it was deleted.
//   - **The actual trigger was sub-issues.** litefactory needs to file a plan
//     as one GitHub sub-issue per user story. That is a capability this package
//     could not express, and the request shapes it would need — endpoint paths,
//     payload field names, and the sub_issue_id-versus-issue-number trap, where
//     the parent is addressed by its *number* and the child by its database
//     *id* and both are int64 — are exactly the facts nobody here could verify
//     first-hand. An SDK converts them into a dependency's tested surface. That
//     v90.0.0 supports sub-issues was confirmed on 2026-08-31 by reading its
//     own source, not its documentation.
//   - **It was an owner decision, taken on the record.** The tripwire that
//     raised it was TestClientMethodSetIsTheSevenEndpoints, whose failure
//     message said an eighth method is "a reason to re-open the SDK question,
//     not to add a method quietly." The question was re-opened and answered.
//     That test survives, reworked, as TestClientMethodSetIsPinned.
//
// The migration is total: all seven endpoints go through the SDK, and
// pagination.go's Link-header reader is gone. The deciding argument was not
// tidiness. A hybrid would have left two HTTP stacks, two error taxonomies and
// two credential paths, and the third of those is the one that matters — one
// credential path is one thing to guard.
//
// (The prose here said "six endpoints" while the surface test said seven. Seven
// was correct: ListCommentsAfter, ListCommentedIssues, PostComment,
// AddCommentReaction, ListIssues, GetIssue and AuthenticatedUser. BaseURL is
// the eighth method and reads a field.)
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
// package — where it is passed to go-github's WithAuthToken, in [New]. The SDK
// installs a RoundTripper that sets the Authorization header on a clone of each
// request, so the value reaches a header and never a URL, a log attribute or an
// error. It is never logged, never placed in an error, and never put in a URL.
// The typed errors here carry a method, a URL, a status and GitHub's own
// message, all of which are safe to print.
//
// TestRevealIsCalledExactlyOnce fails the build if Reveal is called anywhere
// else, more than once, or on a line that is not that call.
//
// # Failure is typed
//
// Non-2xx responses become typed errors — [RateLimitError], [UnauthorizedError],
// [ForbiddenError], [NotFoundError], [ValidationError] — each of which also
// unwraps to a sentinel for errors.Is and to [APIError] for the shared response
// detail. The daemon reads [RateLimitError.RetryAfter] to drive its backoff, so
// that value is a field on a type rather than prose inside a message string.
//
// These are this package's types, not the SDK's. go-github's own error types
// are translated at the package boundary, in errors.go, so no caller outside
// this package had to change when the transport did. Classification is derived
// here from the status code and the response headers rather than delegated,
// because GitHub answers 403 for the hourly quota and 429 for a secondary limit
// and the SDK only recognises the subset of those carrying the headers it looks
// for — reading them here keeps "come back later" from being reported as "the
// token cannot do this".
//
// This package retries nothing and sleeps nowhere. Backoff is the daemon's job,
// because only the daemon knows what else is queued behind the failing call.
// The SDK's client-side rate-limit bookkeeping is switched off in [New] for
// that reason: left on, it remembers a refusal and fails the *next* call
// without asking GitHub, which is backoff by another name and in the wrong
// place.
package github
