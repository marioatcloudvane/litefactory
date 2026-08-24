// Package launchd installs, removes and inspects litefactory's macOS
// LaunchAgent.
//
// # A LaunchAgent, never a LaunchDaemon
//
// The agent is bootstrapped into gui/$UID — the user's GUI security session —
// and its plist lives in ~/Library/LaunchAgents. This is not a stylistic
// preference (SD-8): keychain access, and therefore claude's own
// authentication, is scoped to that GUI session. A LaunchDaemon has no session,
// so claude silently fails to authenticate and the daemon is wedged with no
// diagnosable error. Nothing in this package may be changed to write into
// /Library/LaunchDaemons or to bootstrap into system/.
//
// # PATH is materialised, not inherited
//
// launchd hands an agent PATH=/usr/bin:/bin:/usr/sbin:/sbin and nothing else.
// Homebrew's claude lives in /opt/homebrew/bin, so a daemon that relied on that
// PATH would never find it. [EffectivePATH] takes the installing shell's PATH
// and puts it in the plist's EnvironmentVariables, which is one half of the fix
// (AC-2.2); the other half is the daemon resolving its binaries at startup and
// failing loudly.
//
// # The plist is XML, and paths are hostile
//
// A home directory, a PATH entry or an environment value may contain &, < or a
// quote. Every dynamic value written into the plist goes through
// encoding/xml's escaper, and [Agent.Validate] refuses control characters
// outright rather than emitting a replacement rune. There is no string
// concatenation of unescaped values anywhere in this package.
//
// # The plist may hold a secret
//
// The supported credential source for a launchd-started daemon is the keychain
// (D-D). If github.token_env is configured anyway, the caller materialises that
// variable into [Agent.Environment], and the token is then sitting in plain
// text on disk. That is why the plist is always written 0600 ([PlistMode]) and
// why the caller is expected to say so loudly (AC-2.3).
//
// # launchctl is behind an interface
//
// Every launchctl invocation goes through [Runner], declared here at the point
// of consumption. [Exec] is the only production implementation; tests supply
// their own, because bootstrapping a real agent from a test would leave a
// running daemon behind on the machine.
//
// # No PID file
//
// The daemon is a foreground process supervised by launchd. It never forks and
// writes no PID file (D-8, AC-2.5), so [Manager.Status] asks launchctl for the
// pid rather than reading one out of $LF_HOME.
package launchd
