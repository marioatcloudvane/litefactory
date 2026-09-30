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

// doctorIsolation names the controls every diagnosis launch carries. It is
// recorded with the attempt; the observed binary version comes from the
// provider's own init event.
const doctorIsolation = "claude: --tools \"\" (no native tools), --restricted, strict litefactory-only MCP, --setting-sources \"\", --disable-slash-commands, private config dir holding only the credentials link, scratch working directory"

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
	// Operators who authenticate by API key opt in exactly as for workflow runs.
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
	// No --bare: it disables OAuth. The private config dir and empty setting
	// sources already keep hooks, plugins, memory and CLAUDE.md out.
	return []string{"-p", "--verbose", "--output-format", "stream-json", "--restricted", "--disable-slash-commands", "--no-session-persistence", "--no-chrome", "--setting-sources", "", "--tools", "", "--permission-mode", "dontAsk", "--strict-mcp-config", "--mcp-config", mcpPath, "--allowedTools", strings.Join(allowed, ","), "--model", request.Model}, nil
}
