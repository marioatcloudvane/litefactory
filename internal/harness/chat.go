package harness

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/providerprocess"
)

// RunChat narrows the existing doctor gate to a response-only invocation: empty
// native tools and MCP catalog, isolated settings, ephemeral private home, no
// repository context and no conversation persistence. Owner text is stdin only.
func (r *Runner) RunChat(ctx context.Context, request agent.ChatRequest) (_ agent.ChatResult, err error) {
	if err := request.Validate(); err != nil {
		return agent.ChatResult{}, err
	}
	if proof, e := r.DoctorIsolation(ctx); e != nil || proof == "" {
		return agent.ChatResult{}, agent.ErrDoctorIsolationUnverified
	}
	home, err := os.MkdirTemp(request.WorkingDirectory, "chat-private-")
	if err != nil {
		return agent.ChatResult{}, err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(home)) }()
	if r.osHome != "" {
		if err = linkCredentials(home, r.osHome); err != nil {
			return agent.ChatResult{}, err
		}
	}
	mcpPath := filepath.Join(home, "mcp.json")
	if err = os.WriteFile(mcpPath, []byte(`{"mcpServers":{}}`), 0600); err != nil {
		return agent.ChatResult{}, err
	}
	args := chatArguments(request.Model, mcpPath)
	env := append(providerprocess.DoctorEnvironment(r.environ, home), "CLAUDE_CONFIG_DIR="+home)
	collector := newUsageCaptureCollector(ctx, request.Usage, "", false)
	var result agent.ChatResult
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
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &event) != nil {
			return
		}
		if event.Type == "system" && event.Subtype == "init" {
			tools = tools || len(event.Tools) > 0
			if collector != nil && usageIdentity(event.Session) {
				collector.session = event.Session
			}
		}
		for _, block := range event.Message.Content {
			tools = tools || block.Type == "tool_use"
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
	err = providerprocess.RunChatProcess(ctx, r.binary.Path, args, env, request, r.timeout, observe)
	result.Usage = collector.finish()
	if err == nil && (!terminal || failed || tools || len(result.Answer) > 32<<10) {
		err = errors.New("chat provider response failed or exposed tools")
	}
	return result, err
}

func chatArguments(model, mcpPath string) []string {
	// An explicit empty --tools disables native tools; omitting it enables the
	// defaults. Strict empty MCP also prevents ambient user tools from loading.
	return []string{"-p", "--verbose", "--output-format", "stream-json", "--restricted", "--disable-slash-commands", "--no-session-persistence", "--no-chrome", "--setting-sources", "", "--tools", "", "--permission-mode", "dontAsk", "--strict-mcp-config", "--mcp-config", mcpPath, "--model", model}
}
