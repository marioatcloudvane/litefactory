package codex

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/providerprocess"
)

// DoctorIsolation stays closed: current exec controls disable shell/delegation,
// but expose no proven deny-all native filesystem/search tool catalog. A
// read-only sandbox and strict configuration do not prove issue-only reads.
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
	env := providerprocess.DoctorEnvironment(r.environ, home)
	env = append(env, "CODEX_HOME="+home)
	for _, entry := range r.environ {
		if strings.HasPrefix(entry, "CODEX_API_KEY=") {
			env = append(env, entry)
		}
	}
	collector := newUsageCaptureCollector(ctx, request.Usage)
	state := streamState{}
	err = runScopedDoctorProcess(ctx, r.binary.Path, args, env, request, operation, r.timeout, func(line []byte) { collector.observe(line); state.consumeLine(line) })
	if err == nil && (!state.completed || state.failed) {
		err = errors.New("doctor provider did not report a successful terminal turn")
	}
	return agent.DoctorRunResult{Usage: collector.finish()}, err
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
	args := []string{"exec", "--json", "--strict-config", "--ignore-user-config", "--ignore-rules", "--sandbox", "read-only", "--skip-git-repo-check", "--ephemeral", "-c", "features.shell_tool=false", "-c", "features.multi_agent=false", "--model", request.Model}
	args = append(args, overrides...)
	return append(args, "-"), nil
}
