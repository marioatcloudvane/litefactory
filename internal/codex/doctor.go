package codex

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	execution "github.com/cloudvane/litefactory/pkg/execution/agent"
	native "github.com/cloudvane/litefactory/pkg/execution/providers/codex"
)

// DoctorIsolation stays closed pending the owner's authentic isolation decision.
// Mapping a read-only shell profile is not certification of the installed binary.
func (r *Runner) DoctorIsolation(context.Context) (string, error) {
	return "", agent.ErrDoctorIsolationUnverified
}

// RunDoctor never substitutes workflow execution or a weaker provider/model.
func (r *Runner) RunDoctor(ctx context.Context, request agent.DoctorRequest) (agent.DoctorRunResult, error) {
	if err := request.Validate(); err != nil {
		return agent.DoctorRunResult{}, err
	}
	if _, err := r.DoctorIsolation(ctx); err != nil {
		return agent.DoctorRunResult{}, err
	}
	return r.runDoctorTransport(ctx, request)
}

// runDoctorTransport implements bounded native I/O without enabling unsupported
// isolation. Tests use a harmless process fixture. No ambient rollout directory
// is inspected: exec totals remain qualified, and absent model evidence unknown.
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
	args, err := r.scopedDoctorArguments(request, operation)
	if err != nil {
		return agent.DoctorRunResult{}, err
	}
	home, err := os.MkdirTemp(request.WorkingDirectory, "codex-private-")
	if err != nil {
		return agent.DoctorRunResult{}, err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(home)) }()
	if err = r.linkDoctorAuth(home); err != nil {
		return agent.DoctorRunResult{}, err
	}
	env := execution.IsolatedEnvironment(r.environ, home)
	env = append(env, "CODEX_HOME="+home)
	if operation == "" {
		env = append(env, "GIT_OPTIONAL_LOCKS=0")
		args = append(args[:len(args)-1], "-c", "permissions.doctor_diagnosis.filesystem."+strconv.Quote(home)+"=\"deny\"", "-")
	}
	for _, entry := range r.environ {
		if operation != "" && strings.HasPrefix(entry, "CODEX_API_KEY=") {
			env = append(env, entry)
		}
	}
	collector := newUsageCaptureCollector(ctx, request.Usage)
	state := streamState{}
	err = runScopedDoctorProcess(ctx, r.binary.Path, args, env, request, operation, r.timeout, func(line []byte) { collector.observe(line); state.consumeLine(line) })
	if err == nil && (!state.completed || state.failed) {
		err = errors.New("doctor provider did not report a successful terminal turn")
	}
	if err != nil && state.failureDetail != "" {
		err = fmt.Errorf("doctor provider turn failed: %s: %w", boundedDoctorDetail(state.failureDetail), err)
	}
	return agent.DoctorRunResult{Usage: collector.finish()}, err
}

// linkDoctorAuth symlinks the operator's Codex auth.json into the private
// home so subscription logins authenticate. A link, not a copy, keeps token
// refreshes on the operator's file. A missing source is skipped.
func (r *Runner) linkDoctorAuth(home string) error {
	var target string
	if codexHome := envValue(r.environ, "CODEX_HOME"); codexHome != "" {
		target = filepath.Join(codexHome, "auth.json")
	} else if userHome := envValue(r.environ, "HOME"); userHome != "" {
		target = filepath.Join(userHome, ".codex", "auth.json")
	} else {
		return nil
	}
	switch _, err := os.Stat(target); {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("stat codex auth: %w", err)
	}
	if err := os.Symlink(target, filepath.Join(home, "auth.json")); err != nil {
		return fmt.Errorf("link codex auth: %w", err)
	}
	return nil
}

func boundedDoctorDetail(detail string) string {
	const limit = 512
	detail = strings.TrimSpace(detail)
	if len(detail) > limit {
		detail = strings.ToValidUTF8(detail[:limit], "") + "…"
	}
	return detail
}

func (r *Runner) doctorArguments(request agent.DoctorRequest) ([]string, error) {
	return r.scopedDoctorArguments(request, "")
}

func (r *Runner) scopedDoctorArguments(request agent.DoctorRequest, operation string) ([]string, error) {
	if err := validateScopedDoctorRequest(request, operation); err != nil {
		return nil, err
	}
	overrides, err := r.mcpOverrides(request.MCPLaunch, true)
	if err != nil {
		return nil, err
	}
	var settings, nativeArgs []string
	if operation != "" {
		// Repair changes state only through the broker tools: the sandbox is
		// read-only and every native tool Codex can switch off is off. Codex
		// keeps tools it cannot disable (exec, apply_patch), which the
		// read-only sandbox confines.
		nativeArgs = []string{"--sandbox", "read-only"}
		settings = append(settings, "features.shell_tool=false", "features.unified_exec=false", "features.view_image=false", "features.image_generation=false", `web_search="disabled"`)
		// A conflicted sync_branch runs the conflict advisor inside one tool
		// call; Codex must wait just past the broker's repair tool timeout.
		settings = append(settings, "mcp_servers.litefactory.tool_timeout_sec=630")
	} else {
		settings = append(settings, "features.shell_tool=true", `approval_policy="never"`, `default_permissions="doctor_diagnosis"`, "permissions.doctor_diagnosis.network.enabled=false")
		for _, path := range request.Sandbox.ReadableRoots {
			settings = append(settings, "permissions.doctor_diagnosis.filesystem."+strconv.Quote(path)+"=\"read\"")
		}
		settings = append(settings, "permissions.doctor_diagnosis.filesystem."+strconv.Quote(request.WorkingDirectory)+"=\"write\"")
		for _, path := range request.Sandbox.DeniedReadPaths {
			settings = append(settings, "permissions.doctor_diagnosis.filesystem."+strconv.Quote(path)+"=\"deny\"")
		}
	}
	return native.ScopedArguments(native.ScopedOptions{Model: request.Model, BeforeModelConfigurations: []string{"features.multi_agent=false"}, NativeArguments: nativeArgs, Configurations: settings, ReasoningSummary: r.reasoningSummary(), ExtraArgs: overrides}), nil
}
