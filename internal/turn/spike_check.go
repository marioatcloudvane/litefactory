package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// Bounds on what one retained check record carries.
const (
	spikeCheckOutputTail = 2048
	spikeCheckArgvItems  = 16
	spikeCheckArgvText   = 256
)

// SpikeCheckActivity measures the change a write-access step really made and, if
// it is still small, runs the recipe phases the graph names in the sandbox. It
// returns one outcome and retains its own record; it never chooses a step.
//
//	passed   small, and every named phase passed
//	failed   small, and a command reported a test failure
//	too_big  more files than allowed, or a forbidden kind; no command is run
//	blocked  anything that stops it measuring or running: no baseline, no
//	         change, unusable claim, git or recipe problem, sandbox or timeout
type SpikeCheckActivity struct {
	StackProfile *config.ResolvedStackProfile
	Profile      *graphexec.Profile
	Context      func(context.Context, store.GraphNodeAttempt) (agentresult.Context, error)
	Destinations func() (string, string, error)
	Worktree     func() (*git.Worktree, error)
}

// spikeCheckRecord is the Spike-owned record of one check: what was measured,
// what ran and what came of it. It is separate from the fixer's claim.
type spikeCheckRecord struct {
	Outcome         string              `json:"outcome"`
	Reason          string              `json:"reason,omitempty"`
	SourceAttemptID int64               `json:"source_attempt_id"`
	Baseline        string              `json:"baseline,omitempty"`
	MaxFiles        int                 `json:"max_files"`
	Files           []string            `json:"files"`
	Kinds           []string            `json:"kinds,omitempty"`
	Forbidden       []string            `json:"forbidden,omitempty"`
	Phases          []string            `json:"phases,omitempty"`
	RecipeDigest    string              `json:"recipe_digest,omitempty"`
	Commands        []spikeCheckCommand `json:"commands,omitempty"`
}

// spikeCheckCommand is one executed recipe command, with redacted, capped output.
type spikeCheckCommand struct {
	Phase           string   `json:"phase"`
	Argv            []string `json:"argv"`
	Outcome         string   `json:"outcome"`
	Exit            *int     `json:"exit,omitempty"`
	Reason          string   `json:"reason,omitempty"`
	Output          string   `json:"output,omitempty"`
	OutputTruncated bool     `json:"output_truncated,omitempty"`
}

func (r *spikeCheckRecord) view() *agentresult.SpikeCheckView {
	if r == nil {
		return nil
	}
	return &agentresult.SpikeCheckView{Outcome: r.Outcome, Files: slices.Clone(r.Files), Forbidden: slices.Clone(r.Forbidden), Phases: slices.Clone(r.Phases), Reason: r.Reason}
}

// retainedSpikeCheck reads the check record of exactly this source result, or
// nil when there is none. A record of another result is not evidence of this one.
func retainedSpikeCheck(ctx context.Context, db *store.DB, instanceID, sourceAttemptID int64) (*spikeCheckRecord, error) {
	raw, err := db.LatestSpikeRecord(ctx, instanceID, store.SpikeRecordCheck)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var record spikeCheckRecord
	if err := json.Unmarshal(raw.Payload, &record); err != nil {
		return nil, fmt.Errorf("retained spike check is unreadable: %w", err)
	}
	if record.SourceAttemptID != sourceAttemptID || !slices.Contains(SpikeCheckOutcomes, record.Outcome) {
		return nil, nil
	}
	return &record, nil
}

// DecodeSpikePhases reads the recipe phases a check node names, in order.
func DecodeSpikePhases(attrs map[string]string) ([]string, error) {
	var phases []string
	for name := range strings.SplitSeq(attrs["lf_phases"], ",") {
		name = strings.TrimSpace(name)
		if !slices.Contains(spikePhases, name) || slices.Contains(phases, name) {
			return nil, fmt.Errorf("lf_phases element %q is not a distinct declared phase", name)
		}
		phases = append(phases, name)
	}
	return phases, nil
}

// spikeManifestFiles and spikeLockFiles are the base names that mean a change to
// what the project depends on.
var (
	spikeManifestFiles = []string{"go.mod", "package.json", "Cargo.toml", "pyproject.toml", "Pipfile", "Gemfile", "pom.xml", "build.gradle", "build.gradle.kts", "composer.json", "setup.py"}
	spikeLockFiles     = []string{"go.sum", "go.work.sum", "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "npm-shrinkwrap.json", "Cargo.lock", "poetry.lock", "Pipfile.lock", "Gemfile.lock", "composer.lock", "uv.lock", "bun.lockb"}
)

// spikePathKinds names the forbidden kinds of change a path shows by itself:
// manifests and lock files change dependencies, migrations change a schema. A
// new package or an API change cannot be told from a path; those count only
// when the fixer declares them.
func spikePathKinds(name string) []string {
	base := path.Base(name)
	var kinds []string
	if slices.Contains(spikeManifestFiles, base) || slices.Contains(spikeLockFiles, base) || (strings.HasPrefix(base, "requirements") && strings.HasSuffix(base, ".txt")) {
		kinds = append(kinds, "dependency")
	}
	for _, segment := range strings.Split(path.Dir(name), "/") {
		if segment == "migrations" || segment == "migrate" {
			kinds = append(kinds, "schema")
			break
		}
	}
	return kinds
}

// measureSpikeChange counts the unique changed paths outside the session
// artifact directory and finds which forbidden kinds they and the fixer's claim
// show. It is pure over its inputs.
func measureSpikeChange(changed []string, session string, declared, forbid []string) (files, kinds, forbidden []string) {
	found := map[string]bool{}
	for _, name := range changed {
		if session != "" && (name == session || strings.HasPrefix(name, strings.TrimSuffix(session, "/")+"/")) {
			continue
		}
		files = append(files, name)
		for _, kind := range spikePathKinds(name) {
			found[kind] = true
		}
	}
	for _, kind := range declared {
		found[kind] = true
	}
	for kind := range found {
		kinds = append(kinds, kind)
	}
	slices.Sort(kinds)
	for _, kind := range kinds {
		if slices.Contains(forbid, kind) {
			forbidden = append(forbidden, kind)
		}
	}
	slices.Sort(files)
	return files, kinds, forbidden
}

// Execute measures, sizes and, when small, runs the named phases.
func (h SpikeCheckActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	step, err := loadSpikeStep(ctx, opts, req, h.Profile, h.Context, SpikeCheckAction)
	var unusable *spikeResultError
	if err != nil && !errors.As(err, &unusable) {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	maxFiles, forbid, err := DecodeSpikeGateLimits(step.node.Attrs)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	phases, err := DecodeSpikePhases(step.node.Attrs)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	record := spikeCheckRecord{SourceAttemptID: step.source.ID, MaxFiles: maxFiles, Files: []string{}}
	finish := func(outcome, reason string) (ActivityResult, error) {
		record.Outcome, record.Reason = outcome, reason
		raw, err := marshalSpikeCheck(record)
		if err == nil {
			err = opts.DB.PutSpikeRecord(ctx, store.SpikeRecord{InstanceID: step.attempt.InstanceID, Node: step.node.Name, Kind: store.SpikeRecordCheck, Payload: raw, UpdatedAt: spikeNow(opts)})
		}
		if err != nil {
			return ActivityResult{Diagnostic: err.Error()}, fmt.Errorf("retain spike check: %w", err)
		}
		return ActivityResult{Outcome: outcome, Kind: store.GraphOutputJSON, Output: raw, Diagnostic: reason}, nil
	}
	blocked := func(reason string) (ActivityResult, error) { return finish("blocked", reason) }
	if unusable != nil {
		return blocked("the fixer's result is unusable: " + unusable.cause.Error())
	}
	candidate := step.candidate
	if candidate.Response != "done" || candidate.Result.Payload.Spike == nil || candidate.Result.Payload.Spike.FixReport == nil {
		return blocked("the fixer did not report a finished change")
	}
	claim := candidate.Result.Payload.Spike.FixReport
	baseline, err := retainedSpikeBaseline(ctx, opts.DB, step.attempt.InstanceID)
	if err != nil {
		return blocked(err.Error())
	}
	record.Baseline = baseline
	if h.Worktree == nil || h.Destinations == nil {
		return blocked("no worktree is admitted")
	}
	worktree, err := h.Worktree()
	if err != nil {
		return blocked("worktree unavailable: " + err.Error())
	}
	_, session, err := h.Destinations()
	if err != nil {
		return blocked("session directory unavailable: " + err.Error())
	}
	changed, err := worktree.ChangedSince(ctx, baseline)
	if err != nil {
		if ctx.Err() != nil {
			return ActivityResult{Diagnostic: ctx.Err().Error()}, ctx.Err()
		}
		return blocked("the change cannot be measured: " + err.Error())
	}
	var kinds, forbidden []string
	record.Files, kinds, forbidden = measureSpikeChange(changed, session, claim.ChangeKinds, forbid)
	record.Kinds, record.Forbidden = kinds, forbidden
	if len(record.Files) == 0 {
		return blocked("no change was found in the worktree")
	}
	if len(record.Files) > maxFiles || len(forbidden) > 0 {
		reason := fmt.Sprintf("%d files changed, at most %d allowed", len(record.Files), maxFiles)
		if len(forbidden) > 0 {
			reason += "; forbidden kinds of change: " + strings.Join(forbidden, ", ")
		}
		return finish("too_big", reason)
	}
	if !slices.Contains(record.Files, claim.RegressionTest) {
		return blocked("the claimed regression test is not among the files that changed")
	}
	recipeRaw, err := worktree.ReadBugCheckRecipe(ctx, baseline)
	if err != nil {
		return blocked("repository check recipe unavailable: " + err.Error())
	}
	recipe, err := config.ParseBugCheckRecipe(recipeRaw)
	if err != nil {
		return blocked("repository check recipe is invalid: " + err.Error())
	}
	record.RecipeDigest = config.BugRecipeDigest(recipeRaw)
	run := opts.spikeRunCommand
	if run == nil {
		if opts.MCP == nil {
			return blocked("the check sandbox is not configured")
		}
		isolation, err := opts.MCP.BugChecksIsolation.WithStackProfile(h.StackProfile)
		if err != nil {
			return blocked("stack.environment: retained configuration is invalid")
		}
		if err := isolation.Validate(); err != nil {
			return blocked("the check sandbox is unavailable: " + err.Error())
		}
		run = func(ctx context.Context, worktree string, command config.BugCheckCommand) bugchecks.CommandResult {
			return isolation.Run(ctx, worktree, command, func(bugchecks.CommandResult) error { return nil })
		}
	}
	for _, name := range phases {
		phase, err := recipe.Phase(name)
		if err != nil {
			return blocked(fmt.Sprintf("recipe phase %q: %v", name, err))
		}
		record.Phases = append(record.Phases, name)
		for _, command := range phase.Commands {
			command, err := pinnedStackCommand(h.StackProfile, command)
			if err != nil {
				return blocked(err.Error())
			}
			result := run(ctx, worktree.Path(), command)
			record.Commands = append(record.Commands, spikeCheckCommandOf(name, command, result))
			switch result.Outcome {
			case "passed":
			case "failed":
				return finish("failed", fmt.Sprintf("a %s check reported failure", name))
			case "cancelled":
				return ActivityResult{Diagnostic: "spike check cancelled"}, fmt.Errorf("spike check cancelled: %w", errors.Join(ctx.Err(), errors.New(result.Reason)))
			default:
				return blocked(fmt.Sprintf("a %s check could not complete: %s", name, result.Reason))
			}
		}
	}
	return finish("passed", "")
}

func spikeCheckCommandOf(phase string, command config.BugCheckCommand, result bugchecks.CommandResult) spikeCheckCommand {
	argv := make([]string, 0, min(len(command.Argv), spikeCheckArgvItems))
	for i, arg := range command.Argv {
		if i == spikeCheckArgvItems {
			break
		}
		argv = append(argv, clipText(arg, spikeCheckArgvText))
	}
	out := spikeCheckCommand{Phase: phase, Argv: argv, Outcome: result.Outcome, Exit: result.Exit, Reason: clipText(result.Reason, spikeCheckArgvText), Output: result.Output}
	if len(out.Output) > spikeCheckOutputTail {
		out.Output, out.OutputTruncated = strings.ToValidUTF8(out.Output[len(out.Output)-spikeCheckOutputTail:], ""), true
	}
	return out
}

func clipText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return strings.ToValidUTF8(text[:limit], "") + "..."
}

func marshalSpikeCheck(record spikeCheckRecord) ([]byte, error) {
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	if len(raw) <= store.MaxSpikeRecordBytes {
		return raw, nil
	}
	for i := range record.Commands {
		record.Commands[i].Output, record.Commands[i].OutputTruncated = "", true
	}
	if raw, err = json.Marshal(record); err != nil {
		return nil, err
	}
	if len(raw) > store.MaxSpikeRecordBytes {
		return nil, errors.New("spike check record exceeds its bound")
	}
	return raw, nil
}

// retainedSpikeBaseline reads the commit a gate saved for this instance.
func retainedSpikeBaseline(ctx context.Context, db *store.DB, instanceID int64) (string, error) {
	raw, err := db.LatestSpikeRecord(ctx, instanceID, store.SpikeRecordBaseline)
	if errors.Is(err, store.ErrNotFound) {
		return "", errors.New("no baseline was saved before the change")
	}
	if err != nil {
		return "", fmt.Errorf("baseline unreadable: %w", err)
	}
	var baseline spikeBaseline
	if err := json.Unmarshal(raw.Payload, &baseline); err != nil || baseline.Commit == "" {
		return "", errors.New("the saved baseline is unreadable")
	}
	return baseline.Commit, nil
}

// spikeNow is the run's clock, or the wall clock where no runtime is attached.
func spikeNow(opts Options) time.Time {
	if opts.MCP != nil && opts.MCP.Now != nil {
		return opts.MCP.Now()
	}
	return time.Now().UTC()
}
