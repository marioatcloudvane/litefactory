package harness

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// The agent child's own CLAUDE_CONFIG_DIR (SD-4, amended — see
// .aiteam/AC_AMENDMENTS.md).
//
// Without it, claude reads the operator's ~/.claude.json. That file carries a
// `projects` map with one entry per directory claude has ever been started in,
// and litefactory's child touches every one of them at startup. When any of
// those directories sits under a TCC-protected folder — ~/Documents, ~/Desktop,
// ~/Downloads — the open is gated by macOS privacy consent, attributed not to
// claude but to the *responsible process*: dist/litefactory.
//
// A LaunchAgent has no window for that consent prompt to appear in, so tccd
// logs AUTHREQ_PROMPTING and the openat blocks in the kernel until the request
// expires — measured at 9 to 31 minutes, on every run. The same binary with a
// byte-identical argv starts in under a second from a terminal, because a
// terminal's responsible process already holds the grant.
//
// Pointing claude at a directory litefactory owns removes the dependency
// entirely: the child never sees the operator's project registry, so it never
// reaches for a protected path and TCC is never consulted. It is the same
// argument --settings already makes, applied to the half of the configuration
// --settings cannot reach (see [WriteSettings]: personal agents and skills
// under ~/.claude are discovered regardless of --settings).
//
// The one thing that must survive the move is authentication. claude reads its
// OAuth credentials from <config dir>/.credentials.json, so the directory is
// seeded with a symlink rather than a copy: a copy goes stale the moment the
// token refreshes, and refreshing writes through the link to the real file.

// claudeHomeDirMode is 0700, matching $LF_HOME (S-1).
const claudeHomeDirMode fs.FileMode = 0o700

// credentialsFileName is the OAuth credential file claude reads from its config
// directory, and the only thing seeded into it.
const credentialsFileName = ".credentials.json"

// userClaudeDirName is the operator's own claude configuration directory,
// relative to their OS home. It is read for exactly one file — the credentials
// symlink target — and never written.
const userClaudeDirName = ".claude"

// ClaudeConfigDirEnv is the variable that relocates claude's configuration
// directory. It is set by litefactory rather than inherited: see the package
// comment above and [envAllowlist].
const ClaudeConfigDirEnv = "CLAUDE_CONFIG_DIR"

// EnsureClaudeConfigDir creates the agent child's configuration directory and
// links the operator's credentials into it.
//
// It reports whether it created the directory, so that startup can log the
// first run after an upgrade distinctly from every run after it.
//
// osHome is the operator's OS home directory, not $LF_HOME: the credential file
// being linked is theirs, and litefactory has no way to mint its own. An empty
// osHome creates the directory and links nothing, for the same reason a missing
// credential file does — see below.
//
// A missing credential file is not an error here. litefactory cannot log in on
// the operator's behalf, and failing the run at this point would replace
// claude's own "Not logged in" — which says what to do about it — with one
// raised several steps earlier, from a function whose job is to make a
// directory.
func EnsureClaudeConfigDir(dir, osHome string) (bool, error) {
	if dir == "" || !filepath.IsAbs(dir) {
		return false, invalidOptions("claude config dir %q must be an absolute path", dir)
	}
	if osHome != "" && !filepath.IsAbs(osHome) {
		return false, invalidOptions("$HOME %q must be an absolute path", osHome)
	}

	created := false
	switch _, err := os.Stat(dir); {
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(dir, claudeHomeDirMode); err != nil {
			return false, fmt.Errorf("create %s: %w", dir, err)
		}
		created = true
	case err != nil:
		return false, fmt.Errorf("stat %s: %w", dir, err)
	}

	if osHome == "" {
		// No HOME to link from. claude needs one for its own reasons and says
		// so plainly; failing here would pre-empt that with a worse message
		// from a function whose job is to make a directory.
		return created, nil
	}
	if err := linkCredentials(dir, osHome); err != nil {
		return created, err
	}
	return created, nil
}

// linkCredentials points <dir>/.credentials.json at the operator's own file.
//
// An existing link is left alone only when it already points at the right
// place. Anything else there — a stale link from a moved home directory, or a
// copy someone made by hand — is replaced, because a wrong credential file
// fails as an authentication error, which is a long way from its cause.
func linkCredentials(dir, osHome string) error {
	target := filepath.Join(osHome, userClaudeDirName, credentialsFileName)
	link := filepath.Join(dir, credentialsFileName)

	switch _, err := os.Lstat(target); {
	case errors.Is(err, fs.ErrNotExist):
		// Nothing to link to. See the doc comment: claude reports this better
		// than litefactory can.
		return nil
	case err != nil:
		return fmt.Errorf("stat %s: %w", target, err)
	}

	switch existing, err := os.Readlink(link); {
	case err == nil && existing == target:
		return nil
	case err == nil:
		// A link, but to the wrong file.
	case !errors.Is(err, fs.ErrNotExist):
		// Not a link at all, or unreadable. Either way it is replaced below;
		// os.Remove reports anything that makes that impossible.
	}

	if err := os.Remove(link); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", link, err)
	}
	if err := os.Symlink(target, link); err != nil {
		return fmt.Errorf("link %s to %s: %w", link, target, err)
	}
	return nil
}
