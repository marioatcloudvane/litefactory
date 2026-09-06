package harness

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// The litefactory-controlled claude settings file (SD-2).
//
// --allowedTools is not a sandbox. Measured during the T-LF3.0 spike: it is
// *additive* to the operator's own ~/.claude/settings.json, 250 ambient
// allow-rules leaked into a run, and Bash executed while absent from
// --allowedTools. The boundary is --tools, which narrows the built-in tool
// surface itself; this file is the second half of the answer, so that a run's
// permissions come from litefactory rather than from whatever the operator
// happens to have accumulated in their personal configuration.
//
// Be precise about what this buys, because it is less than it looks:
//
//   - --settings loads *additional* settings. It does not replace the user and
//     project settings, and it does not stop personal agents and skills under
//     ~/.claude from being discovered. Only --tools reliably narrows what can
//     actually be called.
//   - The deny rules below are therefore defence in depth. If a pattern does
//     not match what a future claude expects, it silently fails open — which is
//     survivable precisely because nothing load-bearing rests on it.
//
// The file is litefactory's, not the operator's: [WriteSettings] overwrites it.
// [EnsureSettings] only creates it when missing, which is what a run does, so
// that pointing claude.settings_path at a hand-maintained file stays possible.

// settingsFileMode is 0600. The file names local paths and is written into
// $LF_HOME, which is itself 0700.
const settingsFileMode fs.FileMode = 0o600

// settingsDirMode is 0700, matching $LF_HOME (S-1).
const settingsDirMode fs.FileMode = 0o700

// claudeSettings is the document written to claude.settings_path.
//
// Only keys Claude Code documents are emitted. An invented key is at best
// ignored and at worst a hard startup error in the child, and a hard error in
// the child looks exactly like a hung run.
type claudeSettings struct {
	Permissions                settingsPermissions `json:"permissions"`
	EnableAllProjectMCPServers bool                `json:"enableAllProjectMcpServers"`
	EnabledMCPJSONServers      []string            `json:"enabledMcpjsonServers"`
	IncludeCoAuthoredBy        bool                `json:"includeCoAuthoredBy"`
}

type settingsPermissions struct {
	DefaultMode           string   `json:"defaultMode"`
	Allow                 []string `json:"allow"`
	Ask                   []string `json:"ask"`
	Deny                  []string `json:"deny"`
	AdditionalDirectories []string `json:"additionalDirectories"`
}

// DefaultSettings returns the settings document litefactory runs claude with.
//
// home is $LF_HOME. It is denied explicitly: daemon.log is written unredacted
// by design (S-7 applies to forge-bound bodies, never to the local copy), so
// the one directory on the machine that is guaranteed to contain the bot token
// in plain text is litefactory's own.
func DefaultSettings(home string) ([]byte, error) {
	if home == "" || !filepath.IsAbs(home) {
		return nil, invalidOptions("settings: $LF_HOME %q must be an absolute path", home)
	}

	settings := claudeSettings{
		Permissions: settingsPermissions{
			// The same value argv carries. The document and the flag must
			// always agree: a run whose settings say one thing and whose
			// command line says another is a run nobody can reason about
			// from either artefact alone.
			//
			// This was "default" — prompt on anything unlisted — until the
			// cost of it was measured. A headless run has nobody to prompt, so
			// "default" did not degrade to a prompt, it degraded to a denial,
			// and every tool the allowlist had not anticipated ended a run
			// rather than slowing it. See PermissionMode.
			DefaultMode: PermissionMode,
			Allow:       []string{},
			Ask:         []string{},
			Deny: []string{
				// litefactory owns the commit and the push (AC-3.21). No agent
				// makes either, and work left uncommitted at the end of a run is
				// the normal state.
				//
				// These rules are load-bearing under PermissionMode "auto" and
				// were not needed before it. While runs were in "default" the
				// ban was a side effect of the allowlist being the boundary: git
				// writes were absent from allowed_tools and every absence was a
				// refusal. Under "auto" an absence is a question put to a
				// classifier, and a classifier reading an ordinary coding task
				// has every reason to answer yes. A deny rule is the only form
				// of this ban that does not depend on the permission mode.
				"Bash(git add:*)",
				"Bash(git commit:*)",
				"Bash(git push:*)",
				"Bash(git stash:*)",
				// litefactory's own state: unredacted daemon.log, the database,
				// and every run's raw event stream.
				"Read(//" + filepath.ToSlash(filepath.Clean(home)) + "/**)",
				// The credential stores an agent would reach for first.
				"Read(~/.ssh/**)",
				"Read(~/.aws/**)",
				"Read(~/.gnupg/**)",
				"Read(~/.config/gh/**)",
				"Read(~/.claude/**)",
				"Read(~/.netrc)",
				"Read(./.env)",
				"Read(./.env.*)",
			},
			AdditionalDirectories: []string{},
		},
		// A run must not pick up MCP servers from the branch it is working on.
		// --strict-mcp-config is the flag that actually enforces this; these two
		// keys state the same intent in the file so the two cannot drift.
		EnableAllProjectMCPServers: false,
		EnabledMCPJSONServers:      []string{},
		// litefactory writes its own commit trailers (AC-3.21) and the summary
		// is untrusted; it does not want claude appending attribution of its own.
		IncludeCoAuthoredBy: false,
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode claude settings: %w", err)
	}
	return append(data, '\n'), nil
}

// WriteSettings writes the litefactory-controlled settings file at path,
// replacing whatever was there. `init` and `daemon run` startup call it; a run
// does not, because a run must not silently discard an operator's edit.
//
// The write is atomic — a temporary file in the same directory, then a rename —
// so a crash mid-write cannot leave claude pointed at half a JSON document.
func WriteSettings(path, home string) error {
	if path == "" || !filepath.IsAbs(path) {
		return invalidOptions("claude.settings_path %q must be an absolute path", path)
	}
	data, err := DefaultSettings(home)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data, settingsFileMode)
}

// EnsureSettings writes the settings file only when it does not already exist,
// and reports whether it wrote one.
//
// This runs before every claude invocation. --settings pointing at a file that
// is not there is not a safe fallback: it is the operator's personal
// configuration, ambient allow-rules and all, which is the exact condition SD-2
// says must not be reachable.
func EnsureSettings(path, home string) (bool, error) {
	if path == "" || !filepath.IsAbs(path) {
		return false, invalidOptions("claude.settings_path %q must be an absolute path", path)
	}
	switch _, err := os.Stat(path); {
	case err == nil:
		return false, nil
	case !errors.Is(err, fs.ErrNotExist):
		return false, fmt.Errorf("stat claude settings %s: %w", path, err)
	}
	if err := WriteSettings(path, home); err != nil {
		return false, err
	}
	return true, nil
}

// writeFileAtomic writes data to path via a same-directory temporary file and a
// rename. The temporary file is removed on every failure path, so a full disk
// does not accumulate litter next to the real file.
func writeFileAtomic(path string, data []byte, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, settingsDirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create temporary file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() {
		// Best effort: on the success path the file has already been renamed
		// away and this removes nothing.
		_ = os.Remove(tmpName)
	}()

	// On these two paths the close error is discarded deliberately: the write or
	// the chmod has already failed, the deferred os.Remove deletes the temporary
	// file either way, and nothing was renamed into place — so a close failure
	// can neither lose data nor leave litter. Reporting it would replace the
	// error that actually explains the failure with a less useful one.
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmpName, path, err)
	}
	return nil
}
