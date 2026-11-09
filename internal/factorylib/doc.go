// Package factorylib is the factory library: the agents that belong to the
// factory itself rather than to a project team (the judge, the chat and
// conversation agents, task advisory, the advisors, the
// standards author, the log and drift analysts, the Doctor workflow and the
// chat connector guidance).
//
// The library is editable Markdown in teams/source/factory. Go holds only a
// closed registry: each Role constant maps to one fixed relative path, and
// Files lists every path the library must hold. There is no embedded copy and
// no fallback text. Install validates the source folder against the registry
// and copies it as one unit into <home>/factory/<digest>/ with a manifest of
// per-file sha256; <home>/factory/current names the digest the factory reads.
// The daemon verifies the current installation at start and refuses to run
// without one.
//
// New work captures the bytes it needs (Capture, ConversationCapture) and
// retains them, so a reinstall never changes work in flight. Restore also
// accepts captures retained by the retired embedded instruction package.
package factorylib
