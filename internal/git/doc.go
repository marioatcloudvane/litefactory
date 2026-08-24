// Package git is litefactory's only gateway to the git command line.
//
// # The boundary
//
// The product's most important guarantee is that litefactory never writes
// inside the repository you registered. That guarantee is not a rule written
// down somewhere for a reviewer to remember — it is the shape of the types in
// this package.
//
//   - [Clone] is the registered clone. Its method set is the S-6 allowlist and
//     nothing else: [Clone.Fetch], [Clone.WorktreeAdd], [Clone.WorktreeList],
//     [Clone.WorktreeRemove], [Clone.RevParse], [Clone.RemoteURL],
//     [Clone.ConfigGet] and [Clone.ShowRef]. There is no Commit, Checkout,
//     Switch, Stash, Reset, Merge or Pull method, so writing to the registered
//     clone is a compile error rather than a code-review comment.
//   - [Worktree] holds the mutating half of git. It has no exported
//     constructor and no exported field, so the only ways to obtain a usable
//     one are [Clone.WorktreeAdd] and [EnsureWorktree] — both of which build it
//     through the single unexported constructor, which refuses any path that
//     overlaps the registered clone.
//   - The process runner is unexported and confined to exec.go, which is the
//     only file in the module that imports os/exec for git. No other package
//     may exec git; test fixtures are the single documented exception and live
//     in internal/gittest.
//
// # Subprocess discipline
//
// Every method takes a context first and carries a per-command timeout. This is
// not decoration: a passphrase-protected key or an unreachable host turns an
// untimed git call into a hung tick, and a hung daemon looks alive. The child
// environment is an explicit allowlist (see childEnv), never a pass-through of
// os.Environ.
//
// Failures are reported as [*CommandError], which carries the exact command
// that ran and its stderr as fields, so a caller can print them verbatim rather
// than re-deriving them from a formatted string.
package git
