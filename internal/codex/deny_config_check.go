package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	execution "github.com/cloudvane/litefactory/pkg/execution/agent"
	native "github.com/cloudvane/litefactory/pkg/execution/providers/codex"
)

// checkSelectedConfiguration uses app-server's strict startup loader and a
// read-only config/read exchange. It never sends thread/start or a model request.
// Codex 0.160.0 rejects --strict-config on the features subcommand.
func (r *Runner) checkSelectedConfiguration(ctx context.Context, req agent.Request, env, args []string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	check := []string{"app-server", "--strict-config", "--listen", "stdio://"}
	option := ""
	for _, arg := range args {
		if option != "" {
			value := arg
			if option == "--model" {
				value = "model=" + mcpQuote(arg)
			}
			check = append(check, "-c", value)
			option = ""
			continue
		}
		if arg == "--model" || arg == "-c" {
			option = arg
		}
	}
	if option != "" {
		return errors.New("invalid native configuration override")
	}
	cmd := native.Command(ctx, r.binary.Path, check...)
	cmd.Env, cmd.Dir, cmd.Stderr = env, req.WorktreePath, io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 5 * time.Second
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		stopCtx, stopCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stopCancel()
		return execution.KillSubtree(stopCtx, cmd.Process.Pid, r.logger)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return errors.New("native configuration validation transport unavailable")
	}
	defer func() { _ = stdin.Close() }()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return errors.New("native configuration validation transport unavailable")
	}
	defer func() { _ = stdout.Close() }()
	if err := cmd.Start(); err != nil {
		return errors.New("native configuration validation could not start")
	}
	snapshot, readErr := readSelectedConfiguration(stdin, stdout, req.WorktreePath)
	if readErr == nil {
		readErr = validateSelectedConfiguration(snapshot, envValue(env, "CODEX_HOME"), args)
	}
	_ = stdin.Close()
	if readErr != nil {
		cancel()
	}
	waitErr := cmd.Wait()
	if readErr != nil {
		return readErr
	}
	if waitErr != nil {
		return errors.New("native strict configuration or managed permission requirements rejected the selected launch")
	}
	return nil
}

type selectedConfigSnapshot struct {
	Config map[string]any `json:"config"`
	Layers []struct {
		Name           struct{ Type, File string } `json:"name"`
		DisabledReason string                      `json:"disabledReason"`
		Config         map[string]any              `json:"config"`
	} `json:"layers"`
}

func readSelectedConfiguration(input io.Writer, output io.Reader, cwd string) (selectedConfigSnapshot, error) {
	var result selectedConfigSnapshot
	send := json.NewEncoder(input)
	scanner := bufio.NewScanner(io.LimitReader(output, 8<<20))
	scanner.Buffer(make([]byte, 4096), 2<<20)
	receive := func(id int, target any) error {
		for scanner.Scan() {
			var message struct {
				ID     int             `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  json.RawMessage `json:"error"`
			}
			if json.Unmarshal(scanner.Bytes(), &message) != nil {
				return errors.New("invalid native configuration response")
			}
			if message.ID != id {
				continue
			}
			if len(message.Error) > 0 && string(message.Error) != "null" || len(message.Result) == 0 {
				return errors.New("native configuration inspection refused")
			}
			if target != nil && json.Unmarshal(message.Result, target) != nil {
				return errors.New("invalid native configuration result")
			}
			return nil
		}
		return errors.New("native configuration inspection ended without complete evidence")
	}
	if send.Encode(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{"clientInfo": map[string]string{"name": "litefactory", "version": "1"}, "capabilities": map[string]any{}}}) != nil {
		return result, errors.New("native configuration initialization failed")
	}
	if err := receive(1, nil); err != nil {
		return result, err
	}
	if send.Encode(map[string]any{"method": "initialized", "params": map[string]any{}}) != nil || send.Encode(map[string]any{"id": 2, "method": "config/read", "params": map[string]any{"includeLayers": true, "cwd": cwd}}) != nil {
		return result, errors.New("native configuration request failed")
	}
	if err := receive(2, &result); err != nil {
		return result, err
	}
	return result, nil
}

func validateSelectedConfiguration(snapshot selectedConfigSnapshot, home string, args []string) error {
	config := snapshot.Config
	if store := config["experimental_thread_store"]; store != nil && configMap(store)["type"] != "local" {
		return errors.New("selected groups require the canonical local thread store")
	}
	if config["approval_policy"] != "never" || config["default_permissions"] != "factory_selected" || config["cli_auth_credentials_store"] != "file" {
		return errors.New("managed configuration conflicts with selected approval, permission or credential settings")
	}
	var generated map[string]any
	for _, layer := range snapshot.Layers {
		if layer.Name.Type == "project" && layer.DisabledReason == "" {
			return errors.New("managed project trust would load unrelated project policy")
		}
		if layer.Name.Type == "user" {
			if layer.Name.File != filepath.Join(home, "config.toml") || generated != nil || layer.DisabledReason != "" {
				return errors.New("native configuration imported an unrelated user layer")
			}
			generated = layer.Config
		}
	}
	if generated == nil {
		return errors.New("protected configuration layer is missing")
	}
	if config["sqlite_home"] != generated["sqlite_home"] || generated["sqlite_home"] == nil {
		return errors.New("managed configuration changed the canonical session store")
	}
	expectedProfiles := configMap(generated["permissions"])
	if len(configMap(expectedProfiles["factory_selected"])) == 0 {
		return errors.New("protected permission profile is missing")
	}
	for name, profile := range expectedProfiles {
		expected := configMap(profile)
		if parent, _ := expected["extends"].(string); parent != "" && parent != ":workspace" && parent != ":read-only" && expectedProfiles[parent] == nil {
			return errors.New("protected permission profile inherits an unverified parent")
		}
		actual := configMap(configMap(config["permissions"])[name])
		if !reflect.DeepEqual(withoutConfigNulls(expected), withoutConfigNulls(actual)) {
			return errors.New("managed configuration changed a protected permission profile")
		}
	}
	for path, project := range configMap(generated["projects"]) {
		if configMap(project)["trust_level"] != "untrusted" || configMap(configMap(config["projects"])[path])["trust_level"] != "untrusted" {
			return errors.New("managed configuration changed project trust")
		}
	}
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-c" && strings.TrimSpace(args[i+1]) == "features.multi_agent=false" && configMap(config["features"])["multi_agent"] != false {
			return errors.New("managed configuration changed the native delegation restriction")
		}
	}
	return nil
}
func configMap(value any) map[string]any { result, _ := value.(map[string]any); return result }
func withoutConfigNulls(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, item := range value {
		if item == nil {
			continue
		}
		if nested, ok := item.(map[string]any); ok {
			item = withoutConfigNulls(nested)
		}
		result[key] = item
	}
	return result
}
