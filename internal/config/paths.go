package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnvHome is the environment variable that overrides the default $LF_HOME.
const EnvHome = "LF_HOME"

// Names of the things litefactory owns inside $LF_HOME (S-1). They are
// constants rather than literals scattered through the tree so that the disk
// layout has exactly one definition.
const (
	homeDirName        = ".litefactory"
	configFileName     = "config.yaml"
	databaseFileName   = "litefactory.db"
	logsDirName        = "logs"
	runsDirName        = "runs"
	daemonLogFileName  = "daemon.log"
	daemonLockFileName = "daemon.lock"
	settingsFileName   = "claude-settings.json"
	claudeHomeDirName  = "claude-home"
)

// Paths resolves every location litefactory owns from a single root,
// $LF_HOME. Nothing else in the tree may join a path against the home
// directory itself: add a method here instead, so the S-1 layout stays
// described in one place.
//
// The zero Paths is not useful. Build one with [Locate] in production code and
// with [NewPaths] in tests, which is how a test points litefactory at a
// t.TempDir() without touching the real home directory.
type Paths struct {
	home string
}

// NewPaths returns the layout rooted at home.
func NewPaths(home string) Paths {
	return Paths{home: filepath.Clean(home)}
}

// Locate resolves $LF_HOME: the LF_HOME environment variable when it is set to
// a non-empty value, otherwise ~/.litefactory. The result is absolute, so a
// relative LF_HOME cannot make the daemon's idea of its own state depend on
// the working directory it was started from.
func Locate() (Paths, error) {
	if home := strings.TrimSpace(os.Getenv(EnvHome)); home != "" {
		abs, err := filepath.Abs(home)
		if err != nil {
			return Paths{}, fmt.Errorf("resolve %s=%q: %w", EnvHome, home, err)
		}
		return NewPaths(abs), nil
	}
	dir, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve home directory: %w", err)
	}
	return NewPaths(filepath.Join(dir, homeDirName)), nil
}

// Home is the root directory, $LF_HOME.
func (p Paths) Home() string { return p.home }

// TeamsDir holds the central source catalog, never CLI-generated artifacts.
func (p Paths) TeamsDir() string { return filepath.Join(p.home, "teams") }

// TeamsIndexPath is the central registry document.
func (p Paths) TeamsIndexPath() string { return filepath.Join(p.TeamsDir(), "index.json") }

// SharedAgentPath locates a shared agent definition for a validated identifier.
func (p Paths) SharedAgentPath(id string) string {
	return filepath.Join(p.TeamsDir(), "agents", "shared", id+".md")
}

// TeamAgentPath locates a team-owned definition for validated identifiers.
func (p Paths) TeamAgentPath(name, id string) string {
	return filepath.Join(p.TeamsDir(), "agents", name, id+".md")
}

// TeamSkillDir contains a skill's instructions and relative resources.
func (p Paths) TeamSkillDir(id string) string { return filepath.Join(p.TeamsDir(), "skills", id) }

// TeamsMetadataDir separates management records from catalog source files.
func (p Paths) TeamsMetadataDir() string { return filepath.Join(p.TeamsDir(), ".litefactory") }

// TeamsCacheDir holds verified remote releases, never the installed catalog.
func (p Paths) TeamsCacheDir() string { return filepath.Join(p.home, "cache", "teams") }

// BundlesDir separates immutable rendered snapshots from editable sources.
func (p Paths) BundlesDir() string { return filepath.Join(p.home, "bundles") }

// ConfigPath is $LF_HOME/config.yaml.
func (p Paths) ConfigPath() string { return filepath.Join(p.home, configFileName) }

// DBPath is $LF_HOME/litefactory.db.
func (p Paths) DBPath() string { return filepath.Join(p.home, databaseFileName) }

// LogsDir is $LF_HOME/logs.
func (p Paths) LogsDir() string { return filepath.Join(p.home, logsDirName) }

// DaemonLogPath is $LF_HOME/logs/daemon.log. The file is line-oriented and
// deliberately unredacted: the local copy is always the full truth (S-7).
func (p Paths) DaemonLogPath() string { return filepath.Join(p.LogsDir(), daemonLogFileName) }

// RunsDir is $LF_HOME/logs/runs, the root under which each run's JSONL event
// stream and metadata file live. The per-task subtree below it
// (<forge>/<owner>/<repo>/<issue>/NNNN.jsonl) is built by internal/naming,
// which owns ordinal formatting.
func (p Paths) RunsDir() string { return filepath.Join(p.LogsDir(), runsDirName) }

// LockPath is $LF_HOME/daemon.lock, the dedicated file the daemon flocks to
// enforce a single instance. It is deliberately not the database file, which
// would fight SQLite's WAL locking.
func (p Paths) LockPath() string { return filepath.Join(p.home, daemonLockFileName) }

// SettingsPath is $LF_HOME/claude-settings.json, the default location of the
// litefactory-controlled settings file passed to claude as --settings. See
// [Claude] for why that file exists.
func (p Paths) SettingsPath() string { return filepath.Join(p.home, settingsFileName) }

// ClaudeConfigDir is $LF_HOME/claude-home, the CLAUDE_CONFIG_DIR the agent
// child runs under. It exists so that the daemon's claude reads litefactory's
// own configuration rather than the operator's ~/.claude — see
// [harness.EnsureClaudeConfigDir] for why that matters.
func (p Paths) ClaudeConfigDir() string { return filepath.Join(p.home, claudeHomeDirName) }

// EnsureHome creates $LF_HOME if it is missing.
//
// The mode requested is 0777 on purpose: the process umask of 0o077, installed
// as the first statement of main, turns it into 0700. Nothing in litefactory
// calls Chmod (AC-B.5).
func (p Paths) EnsureHome() error {
	//nolint:gosec // 0700 in practice: the process umask is 0o077 (see cmd/litefactory/main.go).
	if err := os.MkdirAll(p.home, 0o777); err != nil {
		return fmt.Errorf("create %s: %w", p.home, err)
	}
	return nil
}
