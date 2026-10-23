package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/providerprocess"
	"github.com/cloudvane/litefactory/internal/redact"
)

// doctorIsolation names the controls every diagnosis launch carries. It is
// recorded with the attempt; the observed binary version comes from the
// provider's own init event.
const doctorIsolation = "claude: doctor-read-only-shell-v1; Bash only, required OS sandbox, scratch-only writes, no shell network or credentials, GIT_OPTIONAL_LOCKS=0; exact five-tool broker; isolated settings, factory claude home unreadable, per-run scratch"

// DoctorIsolation is open for diagnosis (owner-approved in #430). The tool
// ceiling is enforced by the launch arguments below, not by --bare: bare mode
// refuses OAuth, so it would require an API key the operator does not use.
func (r *Runner) DoctorIsolation(context.Context) (string, error) {
	return doctorIsolation, nil
}

// RunDoctor never substitutes the broader workflow runner for diagnosis.
func (r *Runner) RunDoctor(ctx context.Context, request agent.DoctorRequest) (agent.DoctorRunResult, error) {
	if err := request.Validate(); err != nil {
		return agent.DoctorRunResult{}, err
	}
	if _, err := r.DoctorIsolation(ctx); err != nil {
		return agent.DoctorRunResult{}, err
	}
	return r.runDoctorTransport(ctx, request)
}

// runDoctorTransport is deliberately private. Tests exercise transport with a
// harmless fixture, not an installed agent; production reaches it only through
// DoctorIsolation. Native flags alone are not a capability certificate.
func (r *Runner) runDoctorTransport(ctx context.Context, request agent.DoctorRequest) (agent.DoctorRunResult, error) {
	if err := request.Validate(); err != nil {
		return agent.DoctorRunResult{}, err
	}
	return r.runScopedDoctorTransport(ctx, request, "")
}

func (r *Runner) runScopedDoctorTransport(ctx context.Context, request agent.DoctorRequest, operation string) (_ agent.DoctorRunResult, err error) {

	if err = validateScopedDoctorRequest(request, operation); err != nil {
		return agent.DoctorRunResult{}, err
	}
	// The judge, the doctor and repair run under the factory's persistent
	// Claude home, the same CLAUDE_CONFIG_DIR every workflow run uses (#1057).
	// On macOS Claude keeps its refreshed OAuth tokens in a keychain item named
	// after the config dir; a throwaway dir per run has no such item, so every
	// run fell back to the linked .credentials.json, spent its rotating refresh
	// token, and stored the new tokens under a dir that was then deleted.
	configDir, err := r.persistentClaudeHome()
	if err != nil {
		return agent.DoctorRunResult{}, err
	}
	if operation == "judge" {
		// The persistent home holds the credentials link; it must sit outside
		// every root the judge may read.
		for _, root := range append([]string{request.WorkingDirectory}, request.ReadableDirectories...) {
			if pathsOverlap(configDir, root) {
				return agent.DoctorRunResult{}, errors.New("judge readable roots include the factory claude config dir")
			}
		}
	}
	// Per-run files (mcp.json, TMPDIR) live in a scratch dir that is removed
	// afterwards. The judge reads its working directory, so its scratch dir
	// lives in the system temp directory instead.
	scratchParent := request.WorkingDirectory
	if operation == "judge" {
		scratchParent = ""
	}
	scratch, err := os.MkdirTemp(scratchParent, "claude-run-")
	if err != nil {
		return agent.DoctorRunResult{}, err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(scratch)) }()
	mcpPath := filepath.Join(scratch, "mcp.json")
	args, err := scopedDoctorArguments(request, mcpPath, configDir, operation)
	if err != nil {
		return agent.DoctorRunResult{}, err
	}
	registration := map[string]any{"mcpServers": map[string]any{"litefactory": map[string]any{"type": "stdio", "command": request.MCPLaunch.Executable(), "args": request.MCPLaunch.Arguments(), "env": map[string]string{}}}}
	encoded, err := json.Marshal(registration)
	if err != nil {
		return agent.DoctorRunResult{}, err
	}
	if err = os.WriteFile(mcpPath, encoded, 0600); err != nil {
		return agent.DoctorRunResult{}, err
	}
	env := r.persistentClaudeEnv(scratch, configDir)
	if operation == "" {
		env = append(env, "GIT_OPTIONAL_LOCKS=0", "CLAUDE_CODE_TMPDIR="+request.WorkingDirectory, "CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=1")
	}
	// Repair retains its existing auth handling. Diagnosis carries no API token;
	// subscription credentials are read by the provider outside the shell sandbox.
	if r.forwardEnv && operation != "" {
		for _, entry := range r.environ {
			if strings.HasPrefix(entry, "ANTHROPIC_API_KEY=") {
				env = append(env, entry)
			}
		}
	}
	collector := newUsageCaptureCollector(ctx, request.Usage, "", false)
	// Establish the empty private-source baseline before the provider starts.
	if collector != nil && operation == "" {
		collector.prepareTranscripts(configDir, request.WorkingDirectory)
	}
	result := agent.DoctorRunResult{}
	session := ""
	modelConflict := false
	terminal, failed := false, false
	var terminalErrors []string
	observe := func(line []byte) {
		var event struct {
			Type    string   `json:"type"`
			Session string   `json:"session_id"`
			Parent  string   `json:"parent_tool_use_id"`
			Subtype string   `json:"subtype"`
			IsError bool     `json:"is_error"`
			Errors  []string `json:"errors"`
			Version string   `json:"claude_code_version"`
			Message struct {
				Model string `json:"model"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &event) != nil {
			collector.observe(line)
			return
		}
		if event.Type == "system" && event.Subtype == "init" && session == "" && usageIdentity(event.Session) {
			session = event.Session
			if collector != nil {
				collector.session = session
			}
		}
		collector.observe(line)
		if event.Type == "result" {
			terminal = true
			failed = failed || event.IsError
			if event.IsError {
				terminalErrors = append(terminalErrors, event.Errors...)
			}
		}
		if event.Type == "system" && event.Subtype == "init" && usageIdentity(event.Version) {
			result.ProviderVersion = event.Version
		}
		if event.Type == "assistant" && event.Session == session && session != "" && event.Parent == "" && usageIdentity(event.Message.Model) {
			if result.ObservedModel != "" && result.ObservedModel != event.Message.Model {
				modelConflict = true
			}
			if !modelConflict {
				result.ObservedModel = event.Message.Model
			} else {
				result.ObservedModel = ""
			}
		}
	}
	var exitCode int
	var stderrTail []byte
	exited := false
	failure := func(code int, tail []byte) { exited, exitCode, stderrTail = true, code, tail }
	err = runScopedDoctorProcess(ctx, r.binary.Path, args, env, request, operation, r.timeout, observe, failure, r.logger)
	if exited {
		r.logDoctorFailure(ctx, request, operation, exitCode, stderrTail, terminalErrors)
	}
	if operation == "judge" && terminal && failed && ctx.Err() == nil && (err == nil || errors.Is(err, providerprocess.ErrProviderExit)) && judgeCapacity(terminalErrors) {
		err = agent.ErrJudgeUnavailable
	}
	if err == nil && (!terminal || failed) {
		err = errors.New("doctor provider did not report a successful terminal result")
	}
	result.Usage = collector.finish()
	return result, err
}

func doctorArguments(request agent.DoctorRequest, mcpPath, configDir string) ([]string, error) {
	return scopedDoctorArguments(request, mcpPath, configDir, "")
}

func scopedDoctorArguments(request agent.DoctorRequest, mcpPath, configDir, operation string) ([]string, error) {
	if err := validateScopedDoctorRequest(request, operation); err != nil {
		return nil, err
	}
	allowed := request.MCPLaunch.PermittedTools()
	for i := range allowed {
		allowed[i] = "mcp__litefactory__" + allowed[i]
	}
	// Diagnosis exposes only Bash; the judge only Read, Grep and Glob; repair's
	// explicit empty --tools removes every native tool. Omission would restore the provider default. MCP stays exact.
	// No --bare: it disables OAuth. Empty setting sources, strict MCP config
	// and the tool ceiling keep hooks, plugins, memory and CLAUDE.md out.
	tools := ""
	args := []string{}
	if operation != "" {
		args = append(args, "--no-session-persistence")
	}
	if operation == "" {
		tools = "Bash"
		allowed = append(allowed, "Bash")
		settings, err := doctorSandboxSettings(request, configDir, filepath.Dir(mcpPath))
		if err != nil {
			return nil, err
		}
		args = append(args, "--settings", settings)
	}
	if operation == "judge" {
		// The judge reads the pinned checkout (its working directory) and the
		// context directories with Read, Grep and Glob only. Reads are allowed
		// beneath those roots; dontAsk denies everything else.
		tools = "Read,Grep,Glob"
		for _, dir := range append([]string{request.WorkingDirectory}, request.ReadableDirectories...) {
			allowed = append(allowed, "Read(/"+dir+"/**)")
		}
		for _, dir := range request.ReadableDirectories {
			args = append(args, "--add-dir", dir)
		}
		// Deny rules win over allow rules: the factory claude home (with the
		// credentials link) stays unreadable even if a root ever contained it.
		if configDir != "" {
			args = append(args, "--disallowedTools", "Read(/"+configDir+"/**)")
		}
	}
	if request.Thinking != "" {
		args = append(args, "--effort", string(request.Thinking))
	}
	return append(args, "-p", "--verbose", "--output-format", "stream-json", "--restricted", "--disable-slash-commands", "--no-chrome", "--setting-sources", "", "--tools", tools, "--permission-mode", "dontAsk", "--strict-mcp-config", "--mcp-config", mcpPath, "--allowedTools", strings.Join(allowed, ","), "--model", request.Model), nil
}

func doctorSandboxSettings(request agent.DoctorRequest, private ...string) (string, error) {
	if err := request.Sandbox.Validate(request.WorkingDirectory); err != nil {
		return "", err
	}
	denied := append([]string(nil), request.Sandbox.DeniedReadPaths...)
	for _, path := range private {
		if path != "" && !slices.Contains(denied, path) {
			denied = append(denied, path)
		}
	}
	// Claude may choose a short system-temp fallback when the private override
	// is long. Deny that default explicitly rather than granting another root.
	uid := fmt.Sprint(os.Getuid())
	settings := map[string]any{"sandbox": map[string]any{
		"enabled": true, "failIfUnavailable": true, "allowUnsandboxedCommands": false, "autoAllowBashIfSandboxed": true, "excludedCommands": []string{},
		"filesystem": map[string]any{"allowWrite": []string{request.WorkingDirectory}, "denyWrite": []string{"/tmp/claude-" + uid, "/private/tmp/claude-" + uid}, "denyRead": denied, "allowRead": request.Sandbox.ReadableRoots},
		"network":    map[string]any{"allowedDomains": []string{}, "deniedDomains": []string{"*"}, "allowAllUnixSockets": false, "allowUnixSockets": []string{}, "allowLocalBinding": false},
	}}
	data, err := json.Marshal(settings)
	return string(data), err
}

// judgeCapacity reads native error diagnostics only, never generated terminal prose.
func judgeCapacity(diagnostics []string) bool {
	detail := strings.Join(diagnostics, "\n")
	lower := strings.ToLower(detail)
	for _, marker := range []string{"unsupported", "not supported", "does not support", "model not found", "unknown model", "invalid model", "budget", "context length", "max tokens", "token limit"} {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	return agent.ProviderCapacity(detail) != nil
}

// pathsOverlap reports whether one path is the other or lies beneath it.
func pathsOverlap(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	within := func(child, parent string) bool {
		rel, err := filepath.Rel(parent, child)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	return within(a, b) || within(b, a)
}

var (
	anthropicKey    = regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]{8,}`)
	oauthTokenField = regexp.MustCompile(`(?i)("?(?:access|refresh|id)_?token"?\s*[:=]\s*"?)[^\s",}]+`)
)

// doctorFailureExcerptLimit bounds the redacted stderr kept in the daemon log.
const doctorFailureExcerptLimit = 1024

// logDoctorFailure writes why a judge, doctor or repair provider exited
// nonzero to the daemon log (#1057). Stderr is redacted, flattened and bounded
// here and goes nowhere else: not into the returned error, not into the store.
func (r *Runner) logDoctorFailure(ctx context.Context, request agent.DoctorRequest, operation string, exitCode int, stderrTail []byte, providerErrors []string) {
	if r.logger == nil {
		return
	}
	kind := operation
	if kind == "" {
		kind = "diagnosis"
	}
	redactor := redact.New(redact.Options{Environ: r.environ})
	clean := func(text string) string {
		text = strings.TrimSpace(text)
		if text == "" {
			return "(none)"
		}
		body, err := redactor.Apply(ctx, text, 64<<10)
		if err != nil {
			return "(withheld: redaction failed)"
		}
		// The shared table targets forge bodies; provider stderr can also
		// carry Anthropic keys and OAuth token fields.
		text = anthropicKey.ReplaceAllString(body.String(), "[redacted]")
		text = oauthTokenField.ReplaceAllString(text, "${1}[redacted]")
		return tailExcerpt(text, doctorFailureExcerptLimit)
	}
	r.logger.Warn("doctor provider exited unsuccessfully",
		slog.String("operation", kind),
		slog.String("attempt", request.Attempt),
		slog.Int("exit_code", exitCode),
		slog.String("stderr", clean(string(stderrTail))),
		slog.String("provider_errors", clean(strings.Join(providerErrors, "; "))))
}

// tailExcerpt flattens text to one line and keeps its last limit bytes: a
// provider's final stderr lines are the ones that say why it stopped.
func tailExcerpt(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= limit {
		return text
	}
	cut := len(text) - limit
	for cut < len(text) && !utf8.RuneStart(text[cut]) {
		cut++
	}
	return "…" + text[cut:]
}

// persistentClaudeHome returns the factory's persistent Claude config dir, the
// same CLAUDE_CONFIG_DIR every workflow run uses, after ensuring it exists with
// its credentials link. Isolated provider paths (judge, doctor, repair, chat,
// conversation) use it too (#1057): on macOS Claude keeps refreshed OAuth
// tokens in a keychain item named after the config dir, so a throwaway dir
// falls back to the linked .credentials.json, spends its rotating refresh
// token, and strands the new tokens under a dir that is then deleted. The
// caller must never remove it, and must keep it out of every tool's reach.
func (r *Runner) persistentClaudeHome() (string, error) {
	if r.claudeConfigDir == "" {
		return "", errors.New("isolated claude provider requires the factory claude config dir")
	}
	if _, err := EnsureClaudeConfigDir(r.claudeConfigDir, r.osHome); err != nil {
		return "", err
	}
	return r.claudeConfigDir, nil
}

// persistentClaudeEnv is the isolated provider environment: DoctorEnvironment
// with TMPDIR in the per-run scratch dir, HOME the operator's (macOS `security`
// finds the login keychain, and so the config dir's credentials, only through
// the real HOME), and CLAUDE_CONFIG_DIR the persistent home.
func (r *Runner) persistentClaudeEnv(scratch, configDir string) []string {
	env := providerprocess.DoctorEnvironment(r.environ, scratch)
	if r.osHome != "" {
		env = overrideEnv(env, []string{"HOME=" + r.osHome})
	}
	return append(env, ClaudeConfigDirEnv+"="+configDir)
}
