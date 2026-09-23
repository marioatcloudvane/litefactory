package launchd

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// DefaultLabel is the launchd label litefactory installs itself under. It is
// also the plist file's base name and the last component of the service
// target, so changing it orphans an already-installed agent.
const DefaultLabel = "com.github.cloudvane.litefactory"

// PlistMode is the permission bits the plist is written with.
//
// Always 0600, not only when it holds a token. github.token_env materialises a
// live credential into EnvironmentVariables (D-D), and a mode that depended on
// the configuration would be one refactor away from writing a token
// world-readable. 0600 costs nothing in the keychain case.
const PlistMode fs.FileMode = 0o600

// LaunchdPATH is the PATH launchd gives an agent that names none. It is the
// whole reason [EffectivePATH] exists: /opt/homebrew/bin is not in it, so a
// Homebrew claude is invisible to a daemon that inherits it (AC-2.2, SD-7).
const LaunchdPATH = "/usr/bin:/bin:/usr/sbin:/sbin"

// PathEnvVar is the environment variable [Agent.Environment] must carry.
const PathEnvVar = "PATH"

// The files launchd redirects the agent's standard output and standard error
// into, created inside [Agent.LogsDir].
//
// They are deliberately not daemon.log. That file is a JSONL sink the daemon
// writes itself; appending launchd's capture of a panic trace into it would
// make it unparseable, and a crash is exactly when the log has to be readable.
const (
	StdoutLogName = "launchd.out.log"
	StderrLogName = "launchd.err.log"
)

// The plist values that are litefactory's policy rather than the caller's
// choice. They are constants, not [Agent] fields, because each one is a
// property of how litefactory's daemon is supervised and not a knob.
const (
	// runAtLoad starts the job when the agent is bootstrapped and again at
	// login. `daemon start` is a request for the daemon to be running, and
	// bootstrap alone starts nothing.
	runAtLoad = true

	// keepAlive is off. launchd would otherwise respawn the daemon every
	// throttle interval after a fatal misconfiguration — an unreadable
	// keychain item, a claude binary that is not there — which is how a
	// startup keychain lookup becomes the invisible prompt storm D-D warns
	// about, and how an unrotated daemon.log (accepted M0 debt) fills a disk.
	// A daemon that stopped because its configuration is wrong stays stopped
	// and says so through `daemon status`.
	keepAlive = false

	// workingDirectory is / so that nothing the daemon does can depend on the
	// directory `daemon start` happened to be run from. Every path litefactory
	// touches is absolute.
	workingDirectory = "/"

	// processType is Standard, not Background. The daemon itself is a
	// poller, but every agent run, build and race test it launches inherits
	// its policy, and Background confines them to throttled efficiency cores:
	// a measured third slower on single-threaded work, for jobs that run for
	// hours.
	processType = "Standard"

	// exitTimeoutSeconds is how long launchd waits after SIGTERM before
	// SIGKILL. It matches launchd's own default and is written explicitly
	// because D-B's "exits within 3 seconds" is a promise made against it.
	exitTimeoutSeconds = 20
)

// labelRE matches an acceptable launchd label. The label becomes a file name
// and a service-target component, so a slash or a leading dot in it would
// escape ~/Library/LaunchAgents or produce an unbootable target.
var labelRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// envNameRE matches a POSIX environment variable name.
var envNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ErrInvalidAgent is the cause of every [Agent.Validate] failure, so a caller
// can tell a malformed agent from a filesystem or launchctl error.
var ErrInvalidAgent = errors.New("invalid launch agent")

// Agent is the LaunchAgent litefactory writes.
//
// It describes only what varies between machines. RunAtLoad, KeepAlive, the
// working directory, the process type and the exit timeout are fixed policy —
// see the constants above.
type Agent struct {
	// Label is the launchd label. It is also the plist's base name and the
	// last component of the gui/$UID service target.
	Label string

	// ProgramArguments is the command launchd execs, argv[0] first. It is
	// `<absolute path to litefactory> daemon run`: the daemon runs in the
	// foreground under launchd and never self-daemonizes (D-8, AC-2.5).
	ProgramArguments []string

	// LogsDir is the directory launchd redirects the agent's stdout and stderr
	// into, as [StdoutLogName] and [StderrLogName]. It must exist before the
	// agent is bootstrapped: launchd creates the files but not the directory,
	// and a failed redirect kills the job before it runs.
	LogsDir string

	// Environment becomes EnvironmentVariables. It must carry [PathEnvVar],
	// because launchd's own PATH cannot find a Homebrew claude.
	//
	// It is also where github.token_env is materialised when that is the
	// configured credential source, which is what makes the plist a secret
	// file (D-D, AC-2.3).
	Environment map[string]string
}

// StandardOutPath is where launchd sends the agent's stdout.
func (a Agent) StandardOutPath() string { return filepath.Join(a.LogsDir, StdoutLogName) }

// StandardErrorPath is where launchd sends the agent's stderr.
func (a Agent) StandardErrorPath() string { return filepath.Join(a.LogsDir, StderrLogName) }

// Validate reports everything wrong with the agent at once.
//
// Control characters are refused rather than escaped. XML 1.0 cannot represent
// most of them at all, and encoding/xml would quietly substitute a replacement
// rune — turning a hostile path into a plist that parses and points somewhere
// else.
func (a Agent) Validate() error {
	var errs []error

	switch {
	case a.Label == "":
		errs = append(errs, errors.New("label: must not be empty"))
	case !labelRE.MatchString(a.Label):
		errs = append(errs, fmt.Errorf("label: %q must be letters, digits, dot, dash or underscore and start with a letter or digit", a.Label))
	}

	switch {
	case len(a.ProgramArguments) == 0:
		errs = append(errs, errors.New("program arguments: must not be empty"))
	case !filepath.IsAbs(a.ProgramArguments[0]):
		errs = append(errs, fmt.Errorf("program arguments: %q must be an absolute path; launchd resolves nothing on PATH", a.ProgramArguments[0]))
	}
	for i, arg := range a.ProgramArguments {
		errs = append(errs, checkPlistValue(fmt.Sprintf("program arguments[%d]", i), arg))
	}

	if !filepath.IsAbs(a.LogsDir) {
		errs = append(errs, fmt.Errorf("logs directory: %q must be an absolute path", a.LogsDir))
	}
	errs = append(errs, checkPlistValue("logs directory", a.LogsDir))

	if path, ok := a.Environment[PathEnvVar]; !ok || path == "" {
		errs = append(errs, fmt.Errorf("environment: %s must be set; launchd's own PATH is %s and does not contain a Homebrew install",
			PathEnvVar, LaunchdPATH))
	}
	for _, name := range slices.Sorted(maps.Keys(a.Environment)) {
		if !envNameRE.MatchString(name) {
			errs = append(errs, fmt.Errorf("environment: %q is not an environment variable name", name))
			continue
		}
		errs = append(errs, checkPlistValue("environment "+name, a.Environment[name]))
	}

	if joined := errors.Join(errs...); joined != nil {
		return fmt.Errorf("%w: %w", ErrInvalidAgent, joined)
	}
	return nil
}

// checkPlistValue refuses a value that cannot be written into XML honestly.
// It returns nil for an acceptable value so callers can append it directly.
func checkPlistValue(what, value string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s: is not valid UTF-8", what)
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%s: contains the control character %q, which XML cannot carry", what, r)
		}
	}
	return nil
}

// Render returns the plist for the agent.
//
// The output is deterministic: the dictionary keys are written in a fixed
// order and the environment is sorted by name, so re-running `daemon start`
// with an unchanged configuration produces a byte-identical file.
func (a Agent) Render() ([]byte, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}

	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	b.WriteString("<plist version=\"1.0\">\n")
	b.WriteString("<dict>\n")

	writeString(&b, "Label", a.Label)
	writeStringArray(&b, "ProgramArguments", a.ProgramArguments)
	writeStringDict(&b, "EnvironmentVariables", a.Environment)
	writeString(&b, "WorkingDirectory", workingDirectory)
	writeString(&b, "StandardOutPath", a.StandardOutPath())
	writeString(&b, "StandardErrorPath", a.StandardErrorPath())
	writeString(&b, "ProcessType", processType)
	writeBool(&b, "RunAtLoad", runAtLoad)
	writeBool(&b, "KeepAlive", keepAlive)
	writeInteger(&b, "ExitTimeOut", exitTimeoutSeconds)

	b.WriteString("</dict>\n</plist>\n")
	return []byte(b.String()), nil
}

// EffectivePATH returns the PATH to write into the plist: the installing
// shell's PATH, or launchd's own when the installing process somehow has none.
//
// This is one half of the AC-2.2 fix. Without it the daemon searches
// /usr/bin:/bin:/usr/sbin:/sbin, which contains neither a Homebrew claude nor a
// Homebrew git, and the first real run fails on a machine where every command
// works fine in a terminal.
func EffectivePATH(shellPATH string) string {
	if strings.TrimSpace(shellPATH) == "" {
		return LaunchdPATH
	}
	return shellPATH
}

// The plist writers. Every dynamic value passes through escape; none of them
// concatenates a caller-supplied string into markup.

func writeString(b *strings.Builder, key, value string) {
	writeKey(b, "\t", key)
	fmt.Fprintf(b, "\t<string>%s</string>\n", escape(value))
}

func writeInteger(b *strings.Builder, key string, value int) {
	writeKey(b, "\t", key)
	fmt.Fprintf(b, "\t<integer>%s</integer>\n", strconv.Itoa(value))
}

func writeBool(b *strings.Builder, key string, value bool) {
	writeKey(b, "\t", key)
	if value {
		b.WriteString("\t<true/>\n")
		return
	}
	b.WriteString("\t<false/>\n")
}

func writeStringArray(b *strings.Builder, key string, values []string) {
	writeKey(b, "\t", key)
	b.WriteString("\t<array>\n")
	for _, value := range values {
		fmt.Fprintf(b, "\t\t<string>%s</string>\n", escape(value))
	}
	b.WriteString("\t</array>\n")
}

func writeStringDict(b *strings.Builder, key string, values map[string]string) {
	writeKey(b, "\t", key)
	b.WriteString("\t<dict>\n")
	for _, name := range slices.Sorted(maps.Keys(values)) {
		writeKey(b, "\t\t", name)
		fmt.Fprintf(b, "\t\t<string>%s</string>\n", escape(values[name]))
	}
	b.WriteString("\t</dict>\n")
}

func writeKey(b *strings.Builder, indent, key string) {
	fmt.Fprintf(b, "%s<key>%s</key>\n", indent, escape(key))
}

// escape renders s as XML character data.
//
// A home directory, a PATH entry or an environment value may legitimately
// contain &, < or a quote, and hand-rolled concatenation would produce a plist
// that either fails to parse or — worse — parses into something else.
func escape(s string) string {
	var b strings.Builder
	// strings.Builder never returns a write error, so EscapeText cannot fail
	// here; there is no error path to report.
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
