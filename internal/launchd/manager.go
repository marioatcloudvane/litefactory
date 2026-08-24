package launchd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// launchAgentsDir is the directory a user's LaunchAgents live in, relative to
// the home directory. It is ~/Library/LaunchAgents and never
// /Library/LaunchDaemons: the daemon needs the user's GUI security session for
// keychain access, which a system daemon does not have (SD-8).
var launchAgentsDir = filepath.Join("Library", "LaunchAgents")

// Manager installs, removes and inspects litefactory's LaunchAgent.
//
// The zero Manager manages [DefaultLabel] for the current user through the
// real launchctl and the real home directory. Every field is an override for
// one of those, which is how a test drives the whole flow without touching the
// machine it runs on.
type Manager struct {
	// Label is the launchd label. Empty means [DefaultLabel].
	Label string
	// UID is the GUI domain the agent is bootstrapped into. Zero means the
	// current user. It is a uid, not a name, because that is what launchd's
	// gui/<uid> target takes.
	UID int
	// Home is the home directory whose Library/LaunchAgents holds the plist.
	// Empty means the current user's home.
	Home string
	// Runner executes launchctl. Nil means the real one.
	Runner Runner
	// Logger records each launchctl invocation. Nil discards them.
	//
	// Nothing logged here can carry a credential: the arguments are a service
	// target and a plist path, never the plist's contents.
	Logger *slog.Logger
}

func (m *Manager) label() string {
	if m.Label == "" {
		return DefaultLabel
	}
	return m.Label
}

func (m *Manager) uid() int {
	if m.UID == 0 {
		return os.Getuid()
	}
	return m.UID
}

func (m *Manager) runner() Runner {
	if m.Runner == nil {
		return Exec{}
	}
	return m.Runner
}

func (m *Manager) logger() *slog.Logger {
	if m.Logger == nil {
		return slog.New(slog.DiscardHandler)
	}
	return m.Logger
}

func (m *Manager) home() (string, error) {
	if m.Home != "" {
		return m.Home, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return home, nil
}

// Domain is the launchd domain target the agent lives in: gui/<uid>.
func (m *Manager) Domain() string { return "gui/" + strconv.Itoa(m.uid()) }

// ServiceTarget is the launchd service target: gui/<uid>/<label>.
func (m *Manager) ServiceTarget() string { return m.Domain() + "/" + m.label() }

// PlistPath is ~/Library/LaunchAgents/<label>.plist.
func (m *Manager) PlistPath() (string, error) {
	home, err := m.home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, launchAgentsDir, m.label()+".plist"), nil
}

// NewAgent describes the agent this manager installs.
//
// program is the full command launchd execs, argv[0] being litefactory's own
// absolute path; logsDir is where the agent's stdout and stderr are redirected;
// environment becomes EnvironmentVariables and must carry [PathEnvVar].
func (m *Manager) NewAgent(program []string, logsDir string, environment map[string]string) Agent {
	return Agent{
		Label:            m.label(),
		ProgramArguments: program,
		LogsDir:          logsDir,
		Environment:      environment,
	}
}

// InstallResult describes what [Manager.Install] did.
type InstallResult struct {
	// PlistPath is the file that was written.
	PlistPath string
	// Domain is the launchd domain the agent was bootstrapped into.
	Domain string
	// Replaced reports that an already-loaded agent was booted out first, so
	// the caller can say "reloaded" rather than "loaded".
	Replaced bool
}

// Install writes the plist and loads the agent.
//
// Re-running it is the supported way to apply a changed configuration
// (AC-2.2): the plist is rewritten, an already-loaded agent is booted out, and
// the new one is bootstrapped. An agent that was not loaded is not an error.
//
// The plist is written atomically and 0600 ([PlistMode]) — it may hold a bot
// token (D-D) — and the write happens before the bootout so that a failure to
// write leaves the running agent alone.
func (m *Manager) Install(ctx context.Context, agent Agent) (InstallResult, error) {
	if agent.Label != m.label() {
		return InstallResult{}, fmt.Errorf("%w: label %q does not match the manager's %q",
			ErrInvalidAgent, agent.Label, m.label())
	}
	plist, err := agent.Render()
	if err != nil {
		return InstallResult{}, err
	}
	path, err := m.PlistPath()
	if err != nil {
		return InstallResult{}, err
	}
	if writeErr := writeFileAtomic(path, plist, PlistMode); writeErr != nil {
		return InstallResult{}, writeErr
	}

	replaced, err := m.bootout(ctx)
	if err != nil {
		return InstallResult{}, err
	}
	result := InstallResult{PlistPath: path, Domain: m.Domain(), Replaced: replaced}

	if _, err := m.run(ctx, "bootstrap", m.Domain(), path); err != nil {
		return InstallResult{}, fmt.Errorf("load the launch agent: %w", err)
	}
	return result, nil
}

// Uninstall unloads the agent and reports whether it had been loaded.
//
// The plist is deliberately left on disk: `daemon stop` is "stop the daemon",
// not "forget how to start it", and keeping the file means `daemon start` can
// reload the same configuration.
func (m *Manager) Uninstall(ctx context.Context) (bool, error) {
	return m.bootout(ctx)
}

// bootout removes the agent from the GUI domain, tolerating one that is not
// there. It reports whether anything was actually unloaded.
func (m *Manager) bootout(ctx context.Context) (bool, error) {
	switch _, err := m.run(ctx, "bootout", m.ServiceTarget()); {
	case err == nil:
		return true, nil
	case isNotLoaded(err):
		return false, nil
	default:
		return false, fmt.Errorf("unload the launch agent: %w", err)
	}
}

// Status is what launchd knows about the agent.
type Status struct {
	// Label is the agent's launchd label.
	Label string
	// Domain is the GUI domain it belongs in.
	Domain string
	// PlistPath is where its plist would be.
	PlistPath string
	// Installed reports that the plist exists on disk. An installed but
	// unloaded agent is the state `daemon stop` leaves behind.
	Installed bool
	// Loaded reports that launchd knows about the agent.
	Loaded bool
	// Running reports that a process is alive right now. A loaded agent that
	// is not running is normal: the daemon exited and KeepAlive is off.
	Running bool
	// PID is the daemon's process id, or 0 when it is not running. It comes
	// from launchctl, never from a pid file — the daemon writes none (AC-2.5).
	PID int
	// State is launchd's own word for the job's state, for example "running"
	// or "not running". Empty when the agent is not loaded.
	State string
}

// Status reports whether the agent is loaded, whether it is running and under
// which pid (AC-2.4).
func (m *Manager) Status(ctx context.Context) (Status, error) {
	path, err := m.PlistPath()
	if err != nil {
		return Status{}, err
	}
	status := Status{Label: m.label(), Domain: m.Domain(), PlistPath: path}

	switch _, statErr := os.Stat(path); {
	case statErr == nil:
		status.Installed = true
	case errors.Is(statErr, fs.ErrNotExist):
	default:
		return Status{}, fmt.Errorf("read %s: %w", path, statErr)
	}

	out, err := m.run(ctx, "print", m.ServiceTarget())
	switch {
	case err == nil:
		status.Loaded = true
	case isNotLoaded(err):
		return status, nil
	default:
		return Status{}, fmt.Errorf("inspect the launch agent: %w", err)
	}

	status.State, status.PID = parsePrint(out)
	status.Running = status.PID != 0 || status.State == stateRunning
	return status, nil
}

// stateRunning is launchd's word for a job with a live process.
const stateRunning = "running"

// parsePrint pulls the state and the pid out of `launchctl print`.
//
// Only top-level keys count, which is what the single leading tab is doing:
// the output nests whole dictionaries for event triggers and endpoints, and
// those carry their own "state = active" lines that have nothing to do with
// whether the daemon is up.
func parsePrint(out string) (state string, pid int) {
	for line := range strings.SplitSeq(out, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimRight(line, "\r"), "\t")
		if !ok || strings.HasPrefix(rest, "\t") {
			continue
		}
		key, value, ok := strings.Cut(rest, " = ")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "state":
			if state == "" {
				state = strings.TrimSpace(value)
			}
		case "pid":
			if pid == 0 {
				// A pid launchd cannot express as a number is not one worth
				// reporting; leaving it zero reads as "not running", which is
				// the safe answer.
				pid, _ = strconv.Atoi(strings.TrimSpace(value))
			}
		}
	}
	return state, pid
}

// run executes one launchctl subcommand, logging it first.
func (m *Manager) run(ctx context.Context, args ...string) (string, error) {
	m.logger().Debug("running launchctl", "args", args)
	return m.runner().Run(ctx, args...)
}

// writeFileAtomic writes data to path through a temporary file in the same
// directory, so a reader — launchd at the next login, or a concurrent
// `daemon status` — never sees a half-written plist.
//
// os.CreateTemp creates its file 0600 regardless of the process umask, and the
// mode is then set explicitly rather than inherited: a plist carrying a
// materialised token must not depend on the caller's umask for its
// permissions.
func writeFileAtomic(path string, data []byte, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	//nolint:gosec // G301: 0700 in practice under the daemon's umask; ~/Library/LaunchAgents is normally 0755 and pre-existing.
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create a temporary file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() {
		// Best effort: on the success path the file has already been renamed
		// away, and on the failure path there is nothing useful to say about a
		// leftover temporary file that the returned error does not say better.
		_ = os.Remove(tmpName)
	}()

	if _, err := tmp.Write(data); err != nil {
		return errors.Join(fmt.Errorf("write %s: %w", tmpName, err), tmp.Close())
	}
	if err := tmp.Chmod(mode); err != nil {
		return errors.Join(fmt.Errorf("set the mode of %s: %w", tmpName, err), tmp.Close())
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("install %s: %w", path, err)
	}
	return nil
}
