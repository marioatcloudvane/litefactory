package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/providerprocess"
)

// doctorIsolation names the controls every diagnosis launch carries. It is
// recorded with the attempt; the observed binary version comes from the
// provider's own init event.
const doctorIsolation = "claude: doctor-read-only-shell-v1; Bash only, required OS sandbox, scratch-only writes, no shell network or credentials, GIT_OPTIONAL_LOCKS=0; exact five-tool broker; isolated settings and private credential home"

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
	home, err := os.MkdirTemp(request.WorkingDirectory, "claude-private-")
	if err != nil {
		return agent.DoctorRunResult{}, err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(home)) }()
	// The private config dir gets the same credentials link as a workflow run's
	// claude-home, so subscription (OAuth) login works; nothing else is seeded.
	// RemoveAll removes the link, never its target.
	if r.osHome != "" {
		if err = linkCredentials(home, r.osHome); err != nil {
			return agent.DoctorRunResult{}, err
		}
	}
	args, err := scopedDoctorArguments(request, filepath.Join(home, "mcp.json"), operation)
	if err != nil {
		return agent.DoctorRunResult{}, err
	}
	registration := map[string]any{"mcpServers": map[string]any{"litefactory": map[string]any{"type": "stdio", "command": request.MCPLaunch.Executable(), "args": request.MCPLaunch.Arguments(), "env": map[string]string{}}}}
	encoded, err := json.Marshal(registration)
	if err != nil {
		return agent.DoctorRunResult{}, err
	}
	if err = os.WriteFile(filepath.Join(home, "mcp.json"), encoded, 0600); err != nil {
		return agent.DoctorRunResult{}, err
	}
	env := providerprocess.DoctorEnvironment(r.environ, home)
	env = append(env, "CLAUDE_CONFIG_DIR="+home)
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
		collector.prepareTranscripts(home, request.WorkingDirectory)
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
	err = runScopedDoctorProcess(ctx, r.binary.Path, args, env, request, operation, r.timeout, observe, r.logger)
	if operation == "judge" && terminal && failed && ctx.Err() == nil && (err == nil || errors.Is(err, providerprocess.ErrProviderExit)) && judgeCapacity(terminalErrors) {
		err = agent.ErrJudgeUnavailable
	}
	if err == nil && (!terminal || failed) {
		err = errors.New("doctor provider did not report a successful terminal result")
	}
	result.Usage = collector.finish()
	return result, err
}

func doctorArguments(request agent.DoctorRequest, mcpPath string) ([]string, error) {
	return scopedDoctorArguments(request, mcpPath, "")
}

func scopedDoctorArguments(request agent.DoctorRequest, mcpPath, operation string) ([]string, error) {
	if err := validateScopedDoctorRequest(request, operation); err != nil {
		return nil, err
	}
	allowed := request.MCPLaunch.PermittedTools()
	for i := range allowed {
		allowed[i] = "mcp__litefactory__" + allowed[i]
	}
	// Diagnosis exposes only Bash; repair's explicit empty --tools removes every
	// native tool. Omission would restore the provider default. MCP stays exact.
	// No --bare: it disables OAuth. The private config dir and empty setting
	// sources already keep hooks, plugins, memory and CLAUDE.md out.
	tools := ""
	args := []string{}
	if operation == "prevention" {
		args = append(args, "--max-turns", strconv.Itoa(request.TurnLimit))
	}
	if operation != "" {
		args = append(args, "--no-session-persistence")
	}
	if operation == "" {
		tools = "Bash"
		allowed = append(allowed, "Bash")
		settings, err := doctorSandboxSettings(request, filepath.Dir(mcpPath))
		if err != nil {
			return nil, err
		}
		args = append(args, "--settings", settings)
	}
	if request.Thinking != "" {
		args = append(args, "--effort", string(request.Thinking))
	}
	return append(args, "-p", "--verbose", "--output-format", "stream-json", "--restricted", "--disable-slash-commands", "--no-chrome", "--setting-sources", "", "--tools", tools, "--permission-mode", "dontAsk", "--strict-mcp-config", "--mcp-config", mcpPath, "--allowedTools", strings.Join(allowed, ","), "--model", request.Model), nil
}

func doctorSandboxSettings(request agent.DoctorRequest, privateHome string) (string, error) {
	if err := request.Sandbox.Validate(request.WorkingDirectory); err != nil {
		return "", err
	}
	denied := append([]string(nil), request.Sandbox.DeniedReadPaths...)
	denied = append(denied, privateHome)
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
