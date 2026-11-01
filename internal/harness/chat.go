package harness

import (
	"context"
	"encoding/json"
	"errors"
	native "github.com/cloudvane/litefactory/pkg/execution/providers/claude"
	"os"
	"path/filepath"
	"slices"

	"github.com/cloudvane/litefactory/internal/agent"
)

// RunChat narrows the existing doctor gate to a response-only invocation: empty
// native tools, an MCP catalog of exactly the request's own factory tools (none
// by default), isolated settings, the factory's persistent claude home, no
// repository context and no conversation persistence. Owner text is stdin only.
func (r *Runner) RunChat(ctx context.Context, request agent.ChatRequest) (_ agent.ChatResult, err error) {
	if err := request.Validate(); err != nil {
		return agent.ChatResult{}, err
	}
	if proof, e := r.DoctorIsolation(ctx); e != nil || proof == "" {
		return agent.ChatResult{}, agent.ErrDoctorIsolationUnverified
	}
	// The factory's persistent claude home, never removed; chat has no native
	// tools, so nothing in the run can read it. Per-run files stay in scratch.
	configDir, err := r.persistentClaudeHome()
	if err != nil {
		return agent.ChatResult{}, err
	}
	scratch, err := os.MkdirTemp(request.WorkingDirectory, "chat-run-")
	if err != nil {
		return agent.ChatResult{}, err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(scratch)) }()
	servers := map[string]any{}
	permitted := []string{}
	if launch := request.MCPLaunch; launch != nil {
		servers["litefactory"] = map[string]any{"type": "stdio", "command": launch.Executable(), "args": launch.Arguments(), "env": map[string]string{}}
		for _, name := range launch.PermittedTools() {
			permitted = append(permitted, "mcp__litefactory__"+name)
		}
		slices.Sort(permitted)
	}
	registration, err := json.Marshal(map[string]any{"mcpServers": servers})
	if err != nil {
		return agent.ChatResult{}, err
	}
	mcpPath := filepath.Join(scratch, "mcp.json")
	if err = os.WriteFile(mcpPath, registration, 0600); err != nil {
		return agent.ChatResult{}, err
	}
	args := append(chatArguments(request.Model, mcpPath, permitted), r.thinkingArgs(ctx)...)
	env := r.persistentClaudeEnv(scratch, configDir)
	collector := newUsageCaptureCollector(ctx, request.Usage, "", false)
	var result agent.ChatResult
	// tools marks any tool outside the granted catalog, offered or used.
	terminal, failed, tools := false, false, false
	observe := func(line []byte) {
		collector.observe(line)
		var event struct {
			Type       string          `json:"type"`
			Session    string          `json:"session_id"`
			Subtype    string          `json:"subtype"`
			IsError    bool            `json:"is_error"`
			Result     string          `json:"result"`
			Structured json.RawMessage `json:"structured_output"`
			Tools      []string        `json:"tools"`
			Message    struct {
				Content []struct {
					Type string `json:"type"`
					Name string `json:"name"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &event) != nil {
			return
		}
		if event.Type == "system" && event.Subtype == "init" {
			offered := slices.Clone(event.Tools)
			slices.Sort(offered)
			tools = tools || !slices.Equal(offered, permitted)
			if collector != nil && usageIdentity(event.Session) {
				collector.session = event.Session
			}
		}
		for _, block := range event.Message.Content {
			tools = tools || block.Type == "tool_use" && !slices.Contains(permitted, block.Name)
		}
		if event.Type == "result" {
			terminal = true
			failed = failed || event.IsError
			if len(event.Structured) > 0 {
				result.Answer = append([]byte(nil), event.Structured...)
			} else {
				result.Answer = []byte(event.Result)
			}
		}
	}
	err = native.RunBounded(ctx, r.binary.Path, args, env, request.Prompt, request.WorkingDirectory, r.timeout, observe, nil, nil, false, nil)
	result.Usage = collector.finish()
	if err == nil && (!terminal || failed || tools || len(result.Answer) > 32<<10) {
		err = errors.New("chat provider response failed or its tools differ from the grant")
	}
	return result, err
}

func chatArguments(model, mcpPath string, permitted []string) []string {
	return native.ChatArguments(model, mcpPath, permitted)
}
