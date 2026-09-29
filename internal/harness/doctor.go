package harness

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/providerprocess"
)

// DoctorIsolation remains closed until the exact binary, bare/restricted tool
// ceiling, MCP inheritance and skills isolation have an authentic observation.
// No configuration, prompt or test fixture can enable this gate.
func (r *Runner) DoctorIsolation(context.Context) (string, error) {
	return "", agent.ErrDoctorIsolationUnverified
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
	// Bare mode cannot load OAuth/keychain credentials. An explicitly forwarded
	// provider key is the only supported authentication route in this transport.
	if r.forwardEnv {
		for _, entry := range r.environ {
			if strings.HasPrefix(entry, "ANTHROPIC_API_KEY=") {
				env = append(env, entry)
			}
		}
	}
	collector := newUsageCaptureCollector(ctx, request.Usage, "", false)
	result := agent.DoctorRunResult{}
	session := ""
	modelConflict := false
	terminal, failed := false, false
	observe := func(line []byte) {
		var event struct {
			Type    string `json:"type"`
			Session string `json:"session_id"`
			Parent  string `json:"parent_tool_use_id"`
			Subtype string `json:"subtype"`
			IsError bool   `json:"is_error"`
			Version string `json:"claude_code_version"`
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
	err = runScopedDoctorProcess(ctx, r.binary.Path, args, env, request, operation, r.timeout, observe)
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
	// An explicit empty --tools value removes every native tool; omission would
	// restore the provider default. MCP tools are separately enumerated exactly.
	return []string{"-p", "--verbose", "--output-format", "stream-json", "--bare", "--restricted", "--disable-slash-commands", "--no-session-persistence", "--no-chrome", "--setting-sources", "", "--tools", "", "--permission-mode", "dontAsk", "--strict-mcp-config", "--mcp-config", mcpPath, "--allowedTools", strings.Join(allowed, ","), "--model", request.Model}, nil
}
