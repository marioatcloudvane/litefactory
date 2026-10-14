package harness

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/providerprocess"
)

// ConversationIsolation checks this path's controls independently of diagnosis.
func (r *Runner) ConversationIsolation(ctx context.Context) (string, error) {
	help, err := providerprocess.ConversationProbe(ctx, r.binary.Path, []string{"--help"}, providerprocess.DoctorEnvironment(r.environ, os.TempDir()), os.TempDir())
	if err != nil {
		return "", agent.ErrConversationIsolation
	}
	for _, flag := range []string{"--tools", "--safe-mode", "--restricted", "--safe-mode", "--disable-slash-commands", "--strict-mcp-config", "--setting-sources", "--session-id", "--resume", "--system-prompt", "--settings", "--agents", "--no-chrome"} {
		if !strings.Contains(help, flag) {
			return "", agent.ErrConversationIsolation
		}
	}
	version, err := providerprocess.ConversationProbe(ctx, r.binary.Path, []string{"--version"}, providerprocess.DoctorEnvironment(r.environ, os.TempDir()), os.TempDir())
	number, found := strings.CutSuffix(strings.TrimSpace(version), " (Claude Code)")
	if err != nil || !found || !conversationVersionSupported(number) {
		return "", agent.ErrConversationIsolation
	}
	return "claude-" + number + "-conversation-empty-tools-and-mcp-v1", nil
}

// RunConversation performs one isolated turn and retains exact native session state.
func (r *Runner) RunConversation(ctx context.Context, request agent.ConversationRequest) (agent.ConversationResult, error) {
	if err := request.Validate(); err != nil {
		return agent.ConversationResult{}, err
	}
	if _, err := r.ConversationIsolation(ctx); err != nil {
		return agent.ConversationResult{}, err
	}
	home, scratch, err := providerprocess.PrepareConversationDirectory(request.StateDirectory)
	if err != nil {
		return agent.ConversationResult{}, err
	}
	if r.osHome != "" {
		if err = linkCredentials(home, r.osHome); err != nil {
			return agent.ConversationResult{}, err
		}
	}
	mcp := filepath.Join(home, "mcp.json")
	if err = os.WriteFile(mcp, []byte(`{"mcpServers":{}}`), 0600); err != nil {
		return agent.ConversationResult{}, err
	}
	args := conversationArguments(request, mcp)
	env := append(providerprocess.DoctorEnvironment(r.environ, home), "CLAUDE_CONFIG_DIR="+home, "CLAUDE_CODE_DISABLE_AUTO_MEMORY=1")
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	result := agent.ConversationResult{Session: request.Session}
	var observedErr error
	terminal, identity := false, false
	observe := func(line []byte) {
		if observedErr != nil {
			return
		}
		var event struct {
			Type, Subtype string
			Session       string `json:"session_id"`
			Result        string
			IsError       bool `json:"is_error"`
			Tools         *[]string
			MCP           *[]json.RawMessage `json:"mcp_servers"`
			Version       string             `json:"claude_code_version"`
			Message       struct{ Content []struct{ Type string } }
		}
		if json.Unmarshal(line, &event) != nil {
			return
		}
		fail := func(e error) { observedErr = e; cancel() }
		if event.Tools != nil && len(*event.Tools) > 0 {
			fail(agent.ErrConversationIsolation)
			return
		}
		for _, block := range event.Message.Content {
			if block.Type == "tool_use" {
				fail(agent.ErrConversationIsolation)
				return
			}
		}
		if event.Type == "system" && event.Subtype == "init" {
			if event.Tools == nil || event.MCP == nil || len(*event.MCP) != 0 || !conversationVersionSupported(event.Version) {
				fail(agent.ErrConversationIsolation)
				return
			}
			expected := request.Session.Ref
			if request.Session.Resume != "" {
				expected = request.Session.Resume
			}
			if event.Session != expected || !agent.ConversationIdentity(event.Session) {
				fail(agent.ErrConversationSessionMissing)
				return
			}
			result.Session.Resume = event.Session
			if e := request.OnSession(runCtx, result.Session); e != nil {
				fail(e)
				return
			}
			identity = true
		}
		if event.Type == "result" {
			if !identity || event.IsError || event.Session != result.Session.Resume {
				fail(errors.New("conversation did not complete the recorded native session"))
				return
			}
			terminal = true
			result.Answer = []byte(event.Result)
			if len(result.Answer) == 0 || len(result.Answer) > 32<<10 {
				fail(errors.New("conversation answer byte budget exceeded"))
			}
		}
	}
	err = providerprocess.RunConversationProcess(runCtx, r.binary.Path, args, env, request, scratch, r.timeout, observe)
	if observedErr != nil {
		return result, observedErr
	}
	if err != nil {
		if request.Session.Resume != "" && !identity && ctx.Err() == nil {
			return result, errors.Join(agent.ErrConversationSessionMissing, err)
		}
		return result, err
	}
	if !terminal {
		return result, errors.New("conversation provider did not complete a turn")
	}
	return result, nil
}

// minimumConversationVersion is the first Claude Code release whose isolation
// flags and empty tool/MCP init were recorded. Later releases are accepted while
// the required flags stay present and the init event still proves isolation;
// the CLI updates itself, so an exact pin breaks on every release.
var minimumConversationVersion = [3]int{2, 1, 296}

func conversationVersionSupported(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}
	var got [3]int
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return false
		}
		got[i] = n
	}
	for i := range got {
		if got[i] != minimumConversationVersion[i] {
			return got[i] > minimumConversationVersion[i]
		}
	}
	return true
}

func conversationArguments(request agent.ConversationRequest, mcp string) []string {
	args := []string{"-p", "--verbose", "--output-format", "stream-json", "--restricted", "--safe-mode", "--disable-slash-commands", "--no-chrome", "--setting-sources", "", "--tools", "", "--permission-mode", "dontAsk", "--strict-mcp-config", "--mcp-config", mcp, "--agents", "{}", "--settings", `{"disableAllHooks":true}`, "--system-prompt", agent.ConversationInstructions, "--model", request.Model}
	if request.Session.Resume != "" {
		return append(args, "--resume", request.Session.Resume)
	}
	return append(args, "--session-id", request.Session.Ref)
}
