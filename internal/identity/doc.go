// Package identity derives the owner identity — the human whose comments
// litefactory treats as instructions — from the `gh` CLI that is already
// logged in on this machine.
//
// # This is the only package that runs gh
//
// `gh` is identity discovery and nothing else (S-5). It is never an API
// transport: it is authenticated as Mario, so asking it anything about the bot
// would answer about the wrong account. The bot handle and id come from the
// GitHub API through internal/github, under the bot's own token.
//
//	owner, err := identity.ResolveOwner(ctx, identity.Options{})
//
// Only `litefactory init` calls this, and a test in this package fails the
// build if any other package execs gh or imports this one.
//
// # Why the token variables are stripped
//
// The gh child environment is built from an explicit allowlist ([childEnv]),
// which means GITHUB_TOKEN, GH_TOKEN and GH_ENTERPRISE_TOKEN never reach it.
// That is not tidiness, it is the reason this package exists.
//
// `gh api user` honours those variables. If the *bot* token happens to be
// exported in the shell that runs `litefactory init` — which is exactly what a
// developer using github.token_env has done — gh would answer as the bot, init
// would store the bot as the owner, and the bot != owner check would then pass
// on two rows describing the same account. The daemon would read its own
// comments as instructions and loop forever on the first run. The check that
// exists to prevent that failure would have been the thing that certified it.
//
// GH_HOST is left out of the allowlist for the same reason: the host is pinned
// to github.com on the command line ([Host], AC-0.8) rather than inherited
// from an ambient variable.
//
// # Keychain prompts, and why every rebuild re-prompts
//
// `gh auth status` reads the token gh stored, which on macOS is usually a
// keychain item, and litefactory's own bot-token lookup shells out to
// /usr/bin/security for the same reason (see internal/secrets).
//
// macOS gates keychain access on the identity of the *calling binary*. A
// locally built litefactory is unsigned and therefore has no stable identity,
// so the OS raises its allow-access dialog again after every rebuild, even if
// you clicked "Always Allow" last time. Click **Always Allow** anyway: it
// suppresses the prompt until the next build. Under launchd the same dialog
// can be raised where nobody can see it, which is why every gh invocation here
// carries its own timeout and fails loudly instead of hanging.
package identity
