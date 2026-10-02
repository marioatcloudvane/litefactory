// Package webhookreg makes sure a registered repository has the GitHub webhook
// litefactory's listener expects: the right URL, content type and events, and a
// secret that sits in the keychain.
//
// [Registrar.Ensure] reports every problem in its [Result] and never as an
// error: a repository that cannot get a webhook keeps working on polling, so
// callers print a warning and carry on.
//
// The order is fixed and stops at the first failure:
//
//  1. the configured public URL is checked; empty skips registration;
//  2. the repository's secret is read from the keychain, or generated and
//     stored there before GitHub is contacted at all, so a hook can never exist
//     whose secret was lost;
//  3. the hook is found (recorded id, else a repository hook already pointing
//     at the public URL) and created or edited;
//  4. the hook id GitHub confirmed is recorded.
//
// Hooks that are not litefactory's are never changed or deleted.
package webhookreg
