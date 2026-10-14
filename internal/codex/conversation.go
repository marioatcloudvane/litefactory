package codex

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

// conversationPermissions denies all files except minimal runtime and this
// conversation's empty scratch. Native session files/auth remain runtime-only.
func conversationPermissions(scratch string) string {
	return `permissions={conversation={filesystem={":root"="deny",":minimal"="read",` + strconv.Quote(scratch) + `="write"},network={enabled=false}}}`
}

// ConversationIsolation requires a working named sandbox, including an actual
// forbidden-read/write probe. Unsupported platforms refuse launch visibly.
func (r *Runner) ConversationIsolation(ctx context.Context, state string) (string, error) {
	home, scratch, err := providerprocess.PrepareConversationDirectory(state)
	if err != nil {
		return "", err
	}
	env := append(providerprocess.DoctorEnvironment(r.environ, home), "CODEX_HOME="+home)
	help, err := providerprocess.ConversationProbe(ctx, r.binary.Path, []string{"exec", "--help"}, env, scratch)
	if err != nil {
		return "", errors.Join(agent.ErrConversationIsolation, err)
	}
	for _, flag := range []string{"--strict-config", "--ignore-user-config", "--ignore-rules", "--json"} {
		if !strings.Contains(help, flag) {
			return "", fmt.Errorf("%w: missing required flag %s", agent.ErrConversationIsolation, flag)
		}
	}
	sentinel := filepath.Join(home, "isolation-sentinel")
	if err = os.WriteFile(sentinel, []byte("harmless isolation sentinel"), 0600); err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(sentinel) }()
	script := `/bin/cat "$1" >/dev/null 2>&1 && exit 1; ( : > "$2" ) 2>/dev/null && exit 1; : > "$3" || exit 1; /bin/rm "$3" || exit 1; /bin/echo conversation-isolation-verified`
	args := []string{"sandbox", "-c", conversationPermissions(scratch), "-P", "conversation", "--", "/bin/sh", "-c", script, "conversation-probe", sentinel, filepath.Join(home, "isolation-write"), filepath.Join(scratch, "isolation-write")}
	output, err := providerprocess.ConversationProbe(ctx, r.binary.Path, args, env, scratch)
	if err != nil || strings.TrimSpace(output) != "conversation-isolation-verified" {
		return "", errors.Join(agent.ErrConversationIsolation, err)
	}
	return "codex-conversation-named-deny-root-sandbox-v1", nil
}

// RunConversation performs one isolated turn and retains exact native session state.
func (r *Runner) RunConversation(ctx context.Context, request agent.ConversationRequest) (_ agent.ConversationResult, failure error) {
	phase, beforeProcess := "validation", true
	defer func() { failure = agent.ConversationError(failure, phase, beforeProcess) }()
	if err := request.Validate(); err != nil {
		return agent.ConversationResult{}, err
	}
	phase = "isolation_probe"
	if _, err := r.ConversationIsolation(ctx, request.StateDirectory); err != nil {
		return agent.ConversationResult{}, err
	}
	phase = "directory_preparation"
	home, scratch, err := providerprocess.PrepareConversationDirectory(request.StateDirectory)
	if err != nil {
		return agent.ConversationResult{}, err
	}
	auth := filepath.Join(home, "auth.json")
	if info, e := os.Lstat(auth); e == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			return agent.ConversationResult{}, agent.ErrConversationIsolation
		}
		if err = os.Remove(auth); err != nil {
			return agent.ConversationResult{}, err
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return agent.ConversationResult{}, e
	}
	if err = r.linkDoctorAuth(home); err != nil {
		return agent.ConversationResult{}, err
	}
	env := append(providerprocess.DoctorEnvironment(r.environ, home), "CODEX_HOME="+home)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	result := agent.ConversationResult{Session: request.Session}
	identity, terminal := false, false
	var observedErr error
	observe := func(line []byte) {
		if observedErr != nil {
			return
		}
		var event struct {
			Type   string
			Thread string `json:"thread_id"`
			Item   struct{ Type, Text string }
		}
		if json.Unmarshal(line, &event) != nil {
			return
		}
		fail := func(e error) {
			failurePhase := "stream"
			if event.Type == "system" || event.Type == "thread.started" {
				failurePhase = "identity"
			}
			if event.Type == "result" || event.Type == "turn.failed" || event.Type == "turn.completed" {
				failurePhase = "terminal"
			}
			observedErr = agent.ConversationError(e, failurePhase, false)
			cancel()
		}
		if event.Type == "thread.started" {
			if !agent.ConversationIdentity(event.Thread) || (request.Session.Resume != "" && event.Thread != request.Session.Resume) {
				fail(agent.ErrConversationSessionMissing)
				return
			}
			result.Session.Resume = event.Thread
			if e := request.OnSession(runCtx, result.Session); e != nil {
				fail(e)
				return
			}
			identity = true
		}
		if event.Type == "item.completed" && event.Item.Type == "agent_message" {
			result.Answer = []byte(event.Item.Text)
		}
		if event.Type == "turn.failed" || event.Type == "error" {
			fail(errors.New("conversation native turn failed"))
			return
		}
		if event.Type == "turn.completed" {
			if !identity || len(result.Answer) == 0 || len(result.Answer) > 32<<10 {
				fail(errors.New("conversation did not complete the recorded native session"))
				return
			}
			terminal = true
		}
	}
	phase, beforeProcess = "stream", false
	err = providerprocess.RunConversationProcess(runCtx, r.binary.Path, conversationArguments(request, scratch), env, request, scratch, r.timeout, observe)
	if observedErr != nil {
		return result, observedErr
	}
	if err != nil {
		if request.Session.Resume != "" && !identity && ctx.Err() == nil {
			return result, errors.Join(agent.ErrConversationSessionMissing, err)
		}
		return result, err
	}
	phase = "terminal"
	if !terminal {
		return result, errors.New("conversation provider did not complete a turn")
	}
	return result, nil
}

func conversationArguments(request agent.ConversationRequest, scratch string) []string {
	args := []string{"exec"}
	if request.Session.Resume != "" {
		args = append(args, "resume")
	}
	args = append(args, "--json", "--strict-config", "--ignore-user-config", "--ignore-rules", "--skip-git-repo-check", "--model", request.Model, "-c", conversationPermissions(scratch), "-c", `default_permissions="conversation"`, "-c", `approval_policy="never"`, "-c", `web_search="disabled"`, "-c", `mcp_servers={}`, "-c", `project_doc_max_bytes=0`, "-c", `developer_instructions=`+strconv.Quote(agent.ConversationInstructions))
	for _, feature := range []string{"shell_tool", "unified_exec", "view_image", "image_generation", "multi_agent", "apps", "plugins", "browser_use", "browser_use_external", "browser_use_full_cdp_access", "computer_use", "code_mode", "code_mode_host", "in_app_browser", "enable_mcp_apps", "auth_elicitation"} {
		args = append(args, "-c", "features."+feature+"=false")
	}
	if request.Session.Resume != "" {
		args = append(args, request.Session.Resume)
	}
	return append(args, "-")
}
