// Package webhook receives GitHub webhook deliveries and turns them into
// wake-up hints. It is a leaf: stdlib only, no store, no forge client and no
// keychain.
//
// A delivery is never state. Only a repository id and an issue number leave
// the handler, through [Sink.Wake]; the daemon re-reads the issue from the
// forge before it acts, so a forged or replayed payload can at worst cost one
// read.
//
// # Check order
//
// The order of checks in [Handler.ServeHTTP] is the security design and is
// fixed:
//
//  1. path and method: 404 for another path, 405 for another method;
//  2. the body is read, capped at [MaxBodyBytes]: 413 above it;
//  3. the event and delivery headers: 400 when absent;
//  4. repository.full_name is decoded and nothing else: 400 for bad JSON;
//  5. the repository is looked up case-insensitively;
//  6. its cached secret is fetched;
//  7. the HMAC-SHA256 of the raw body is compared in constant time.
//
// Every failure from step 4's lookup onward, and a missing or malformed
// signature, answers the same 401 with the same body, so the response does not
// say whether a repository is registered or has a secret. Nothing is logged at
// info or above, no hint is queued and no delivery id is recorded before the
// signature verifies.
//
// Only after a valid signature: ping answers 200; events other than issues,
// issue_comment and sub_issues answer 202; a repeated delivery id answers 200
// without a second hint; the issue number(s) are extracted and queued, and a
// signed event without a number answers 202.
package webhook
