package turn

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/coverage"
	"github.com/cloudvane/litefactory/internal/store"
)

// ValidateCoverageBindings supply sandbox-derived facts and repository reads.
// They are installed by the factory, never decoded from an agent response.
type ValidateCoverageBindings struct {
	Discover   func(context.Context, ValidateSuiteScratch, []config.BugCheckCommand, coverage.DiscoveryRunner) (coverage.Inventory, map[int]coverage.Facts, error)
	Changes    func(context.Context, string, string) (coverage.Changes, error)
	PinBase    func(context.Context) (string, error)
	Conditions string
}

func (h ValidateSuiteActivity) executeCoverage(ctx context.Context, opts Options, req ActivityRequest, auth ValidateSuiteAuthority, locked bool) (res ActivityResult, err error) {
	if auth.RequestID < 1 || auth.Lineage == "" {
		return ActivityResult{}, errors.New("coverage requires a durable validation request")
	}
	request, requestErr := h.b.DB.GetValidationRequest(ctx, auth.RequestID)
	if requestErr != nil {
		return ActivityResult{}, requestErr
	}
	if request.Commit != auth.Commit || request.Lineage != auth.Lineage || request.Policy != auth.Policy {
		return ActivityResult{}, errors.New("coverage authority differs from its durable validation request")
	}
	if !locked {
		unlock, e := h.b.DB.LockSuite(ctx, h.b.LockDir)
		if errors.Is(e, store.ErrSuiteBusy) {
			return suiteResult("retry", validateSuiteOutput{Outcome: "retry", Commit: auth.Commit, Reason: "another suite is running"})
		}
		if e != nil {
			return ActivityResult{}, e
		}
		defer func() { err = errors.Join(err, unlock()) }()
	}
	// A source request owns its own result; another request on the same commit
	// cannot satisfy an explicit fresh run.
	records, e := h.b.DB.ValidationEvidenceForRequest(ctx, auth.RequestID)
	if e != nil {
		return ActivityResult{}, e
	}
	for i := len(records) - 1; i >= 0; i-- {
		r := records[i]
		if r.Kind != "assessment" {
			continue
		}
		var m coverage.Manifest
		if e = json.Unmarshal(r.Payload, &m); e != nil {
			return ActivityResult{}, e
		}
		if m.Commit != auth.Commit {
			continue
		}
		run, e := h.b.DB.GetSuiteRun(ctx, r.RunID)
		if e != nil {
			return ActivityResult{}, e
		}
		out := suiteOutput(run)
		out.Outcome = m.Outcome
		out.AssessmentID = r.ID
		out.AssessmentDigest = coverage.Digest(m)
		out.Assessment = &m
		verifyOpts := opts
		verifyOpts.DB = h.b.DB
		if run.Commit != auth.Commit || run.State == store.SuiteRunning || r.RequestID != auth.RequestID {
			return ActivityResult{}, errors.New("replayed coverage assessment is detached from its suite")
		}
		if _, e := validateRetainedAssessment(ctx, verifyOpts, out); e != nil {
			return ActivityResult{}, e
		}
		return suiteResult(m.Outcome, out)
	}
	if reason, e := h.recover(ctx, opts); e != nil {
		return ActivityResult{}, e
	} else if reason != "" {
		return suiteResult("blocked", validateSuiteOutput{Outcome: "blocked", Commit: auth.Commit, Reason: reason})
	}
	return h.runCoverage(ctx, opts, req, auth)
}

func (h ValidateSuiteActivity) runCoverage(ctx context.Context, opts Options, req ActivityRequest, auth ValidateSuiteAuthority) (res ActivityResult, err error) {
	b := h.b
	scratch, e := b.Repo.Begin(ctx, auth.Commit)
	if e != nil {
		return ActivityResult{}, e
	}
	preserveScratch := false
	scratchCloseAttempted := false
	defer func() {
		if !preserveScratch && !scratchCloseAttempted {
			err = errors.Join(err, scratch.Close(context.WithoutCancel(ctx)))
		}
	}()
	before, e := scratch.Observe(ctx)
	if e != nil {
		return ActivityResult{}, e
	}
	raw, e := scratch.Recipe(ctx)
	if e != nil {
		return ActivityResult{}, e
	}
	recipe, e := config.ParseBugCheckRecipe(raw)
	if e != nil {
		return ActivityResult{}, e
	}
	commands := slices.Clone(recipe.Phases.Full.Commands)
	for i, c := range commands {
		commands[i], e = pinnedStackCommand(b.StackProfile, c)
		if e != nil {
			return ActivityResult{}, e
		}
	}
	run, e := b.DB.BeginSuiteRun(ctx, store.SuiteRun{InstanceID: req.InstanceID, Node: req.Node, Commit: auth.Commit, RecipeDigest: config.BugRecipeDigest(raw), TreeBefore: before, StartedAt: h.now(opts)})
	if e != nil {
		return ActivityResult{}, e
	}
	runCommand, refusal := h.runner(opts)
	m := coverage.Manifest{Version: coverage.Version, Commit: auth.Commit, Recipe: run.RecipeDigest}
	rawState := store.SuitePassed
	rawReason := ""
	var refs []string
	position := 0
	const maxCoverageInvocations = 2000
	type guardedSnapshot struct {
		scratch ValidateSuiteScratch
		tree    string
	}
	snapshots := map[string]guardedSnapshot{scratch.Path(): {scratch: scratch, tree: before}}
	guardSnapshot := func(path string) error {
		snapshot, ok := snapshots[path]
		if !ok {
			return errors.New("coverage snapshot has no unchanged-tree guard")
		}
		current, e := snapshot.scratch.Observe(context.WithoutCancel(ctx))
		if e != nil || current != snapshot.tree {
			return errors.New("coverage snapshot changed or could not be observed")
		}
		return nil
	}
	inDiscovery := false
	// All discovery and execution share the persisted process claim and command
	// ledger. Capture integrity is retained separately from the display tail.
	invoke := func(c config.BugCheckCommand, path string) (bugchecks.CommandResult, error) {
		if position >= maxCoverageInvocations {
			return bugchecks.CommandResult{}, errors.New("coverage invocation limit reached")
		}
		if e := guardSnapshot(path); e != nil {
			rawState = store.SuiteBlocked
			return bugchecks.CommandResult{}, e
		}
		pos := position
		position++
		claimed := store.SuiteCommand{RunID: run.ID, Position: pos, Argv: c.Argv, StartedAt: h.now(opts)}
		if e := b.DB.ClaimSuiteCommand(ctx, claimed); e != nil {
			return bugchecks.CommandResult{}, e
		}
		r := runCommand(ctx, path, c, func(r bugchecks.CommandResult) error {
			claimed.StartedAt = r.Started
			claimed.ProcessIdentity = r.ProcessIdentity
			return b.DB.ClaimSuiteCommand(ctx, claimed)
		})
		state := r.Outcome
		if state != "passed" && state != "failed" && state != "cancelled" {
			state = store.SuiteBlocked
		}
		if state != store.SuiteCancelled && (!r.CleanupConfirmed || !r.CaptureComplete || r.Exit == nil || state == "passed" && *r.Exit != 0) {
			state = store.SuiteBlocked
		}
		claimed.State = state
		claimed.Exit = r.Exit
		claimed.Reason = r.Reason
		claimed.FinishedAt = h.now(opts)
		claimed.CleanupConfirmed = r.CleanupConfirmed
		claimed.OutputTail = tailText(r.Output, store.MaxSuiteOutputTail)
		claimed.OutputTruncated = len(r.Output) > store.MaxSuiteOutputTail
		claimed.OutputDigest = coverage.Digest(r.Output)
		if state == store.SuiteFailed && !inDiscovery {
			rawState = store.SuiteFailed
			rawReason = firstNonBlank(r.Reason, "a full-phase command reported test failure")
		}
		if (state == store.SuiteBlocked || state == store.SuiteCancelled) && !inDiscovery {
			rawState = state
			rawReason = firstNonBlank(r.Reason, "a command could not complete trustworthy execution")
		}
		if !r.CleanupConfirmed {
			// Keep the process claim live for recovery. Closing its scratch or
			// making the suite final would allow a later request to overlap it.
			preserveScratch = true
			rawState = store.SuiteBlocked
			return r, errors.New("coverage command process cleanup is unconfirmed")
		}
		if e := b.DB.FinishSuiteCommand(context.WithoutCancel(ctx), claimed); e != nil {
			return r, e
		}
		if ctx.Err() != nil {
			return r, ctx.Err()
		}
		if e := guardSnapshot(path); e != nil {
			rawState = store.SuiteBlocked
			return r, e
		}
		return r, nil
	}
	settled := false
	var finishInputCheck func() error
	fullExecution := false
	finish := func(reason string) (ActivityResult, error) {
		settled = true
		if preserveScratch {
			out := suiteOutput(run)
			out.Outcome = "blocked"
			out.Reason = reason
			return suiteResult("blocked", out)
		}
		after, observeErr := scratch.Observe(context.WithoutCancel(ctx))
		if observeErr != nil || after != before {
			m.Complete = false
			m.Outcome = "blocked"
			reason = "tested snapshot changed or could not be observed"
			rawState = store.SuiteBlocked
		}
		if finishInputCheck != nil {
			if e := finishInputCheck(); e != nil {
				reason = e.Error()
				rawState = store.SuiteBlocked
				m.Complete = false
				m.Outcome = "blocked"
			}
		}
		if reason == "" && (rawState == store.SuiteBlocked || rawState == store.SuiteCancelled) {
			reason = rawReason
		}
		if reason != "" {
			if rawState != store.SuiteCancelled {
				rawState = store.SuiteBlocked
			}
			m.Complete = false
			if m.Outcome == "" || m.Outcome == "passed" || m.Outcome == "covered" {
				m.Outcome = "blocked"
			}
			m.FullReason = reason
		}
		m.Duration = h.now(opts).Sub(run.StartedAt)
		if m.Duration < 0 {
			m.Duration = 0
		}
		if rawState == store.SuiteBlocked || rawState == store.SuiteCancelled {
			m.Complete = false
			m.Outcome = "blocked"
		}
		for _, a := range m.Attempts {
			if !a.Complete {
				m.Complete = false
				m.Outcome = "blocked"
			}
		}
		if m.Outcome == "" {
			m.Outcome = "blocked"
		}
		// Checkout disposal is part of execution completion. Persist no passing
		// assessment before its cancellation-safe cleanup result is known.
		scratchCloseAttempted = true
		if closeErr := scratch.Close(context.WithoutCancel(ctx)); closeErr != nil {
			rawState = store.SuiteBlocked
			m.Complete = false
			m.Outcome = "blocked"
			reason = "coverage checkout cleanup failed: " + closeErr.Error()
			m.FullReason = reason
		}
		m.Duration = h.now(opts).Sub(run.StartedAt)
		if m.Duration < 0 {
			m.Duration = 0
		}
		if fullExecution && m.Complete {
			m.FullDuration = m.Duration
		}
		if err := b.DB.FinishSuiteRun(context.WithoutCancel(ctx), run.ID, rawState, after, firstNonBlank(reason, rawReason), h.now(opts)); err != nil {
			return ActivityResult{}, err
		}
		data, e := json.Marshal(m)
		if e != nil {
			return ActivityResult{}, e
		}
		if len(data) > store.MaxValidationEvidenceBytes {
			// Individual observations are already durable. Keep a bounded
			// unresolved summary rather than duplicating their complete bodies.
			m.Complete = false
			m.Outcome = "blocked"
			m.FullReason = "coverage assessment exceeded retained evidence bound"
			m.Attempts = nil
			m.Results = nil
			m.Comparisons = nil
			m.RequiredFailures = nil
			m.Inventory = coverage.Inventory{Commit: auth.Commit, Conditions: m.Inventory.Conditions, Reason: m.FullReason}
			data, e = json.Marshal(m)
			if e != nil {
				return ActivityResult{}, e
			}
		}
		evidence, e := b.DB.FinalizeValidationAssessment(context.WithoutCancel(ctx), store.ValidationEvidence{RequestID: auth.RequestID, RunID: run.ID, Kind: "assessment", Version: 1, Complete: m.Complete, Payload: data, References: refs})
		if e != nil {
			return ActivityResult{}, e
		}
		final, e := b.DB.GetSuiteRun(context.WithoutCancel(ctx), run.ID)
		if e != nil {
			return ActivityResult{}, e
		}
		out := suiteOutput(final)
		out.Outcome = m.Outcome
		out.AssessmentID = evidence.ID
		out.AssessmentDigest = coverage.Digest(m)
		out.Assessment = &m
		if ctx.Err() != nil {
			return ActivityResult{}, ctx.Err()
		}
		return suiteResult(m.Outcome, out)
	}
	defer func() {
		if err != nil && !settled {
			cause := err
			rawState = store.SuiteBlocked
			res, err = finish(cause.Error())
			if err != nil {
				err = errors.Join(cause, err)
			}
		}
	}()
	// Mark the contract before any repository execution. Even an interrupted
	// or metadata-failed attempt must never be reused as a legacy full suite.
	markerPayload, markerErr := json.Marshal(struct {
		Version int    `json:"version"`
		Commit  string `json:"commit"`
		State   string `json:"state"`
	}{coverage.Version, auth.Commit, "started"})
	if markerErr != nil {
		return ActivityResult{}, markerErr
	}
	if _, markerErr = b.DB.AppendValidationEvidence(context.WithoutCancel(ctx), store.ValidationEvidence{RequestID: auth.RequestID, RunID: run.ID, Kind: "manifest", Version: store.ValidationEvidenceVersion, Complete: false, Payload: markerPayload}); markerErr != nil {
		return ActivityResult{}, markerErr
	}
	if runCommand == nil {
		rawState = store.SuiteBlocked
		return finish(refusal)
	}
	discovery := func(ctx context.Context, c config.BugCheckCommand) (coverage.DiscoveryResult, error) {
		r, e := invoke(c, scratch.Path())
		return coverage.DiscoveryResult{Output: []byte(r.Output), Complete: r.CaptureComplete && r.CleanupConfirmed && r.Outcome == "passed"}, e
	}
	inDiscovery = true
	var inv coverage.Inventory
	facts := map[int]coverage.Facts{}
	if b.Coverage != nil && b.Coverage.Discover != nil {
		inv, facts, e = b.Coverage.Discover(ctx, scratch, commands, discovery)
	} else {
		inv = coverage.Inventory{Commit: auth.Commit, Complete: true}
		for i, c := range commands {
			one, f, de := coverage.Discover(ctx, c, i, auth.Commit, scratch.Path(), discovery)
			if de != nil || !one.Complete {
				inv.Complete = false
				inv.Reason = firstNonBlank(one.Reason, "runner dependency discovery unavailable")
			}
			inv.Units = append(inv.Units, one.Units...)
			facts[i] = f
		}
	}
	if e != nil {
		rawState = store.SuiteBlocked
		return finish(e.Error())
	}
	inDiscovery = false
	if e := guardSnapshot(scratch.Path()); e != nil {
		rawState = store.SuiteBlocked
		return finish(e.Error())
	}
	inv.Commit = auth.Commit
	conditions := ""
	if b.Coverage != nil {
		conditions = b.Coverage.Conditions
	}
	inv.Conditions = coverage.Digest(struct {
		Recipe, Isolation string
		Facts             map[int]coverage.Facts
	}{run.RecipeDigest, conditions, facts})
	// Fingerprint actual executable bytes and privately hashed configured
	// inputs before admitting inheritance. Fixture runners supply their own
	// trusted conditions and keep their existing deterministic contract.
	observeCommandInputs := func(path string) ([]string, error) {
		if b.Run != nil {
			return nil, nil
		}
		if opts.MCP == nil {
			return nil, errors.New("execution input observation requires isolation")
		}
		isolation, inputErr := opts.MCP.BugChecksIsolation.WithStackProfile(b.StackProfile)
		if inputErr != nil {
			return nil, inputErr
		}
		fingerprints := make([]string, 0, len(commands))
		for _, command := range commands {
			fingerprint, inputErr := isolation.ObserveInputs(path, command)
			if inputErr != nil {
				return nil, inputErr
			}
			fingerprints = append(fingerprints, fingerprint)
		}
		return fingerprints, nil
	}
	executionInputs, inputErr := observeCommandInputs(scratch.Path())
	if b.Run == nil {
		inv.Conditions = coverage.Digest(struct {
			Conditions string
			Inputs     []string
		}{inv.Conditions, executionInputs})
		if inputErr != nil {
			inv.Complete = false
			inv.Reason = "actual execution input identity is unavailable"
		}
	}
	if b.Run == nil && inputErr == nil {
		finishInputCheck = func() error {
			after, e := observeCommandInputs(scratch.Path())
			if e != nil {
				return e
			}
			if !slices.Equal(after, executionInputs) {
				return errors.New("execution inputs changed during validation")
			}
			return nil
		}
	}
	if b.Coverage == nil || b.Coverage.Discover == nil {
		for _, command := range commands {
			for _, name := range command.Env {
				// These closed toolchain/locale settings are privately fingerprinted by
				// ObserveInputs. Other references may name unversioned external fixtures.
				if !slices.Contains([]string{"GOTOOLCHAIN", "GOOS", "GOARCH", "CGO_ENABLED", "GOMAXPROCS", "LANG", "LC_ALL", "TZ", "PYTHONDONTWRITEBYTECODE", "PYTHONHASHSEED"}, name) {
					inv.Complete = false
					inv.Reason = "external fixture equivalence is unproven"
					break
				}
			}
		}
	}
	// The recipe has no dependency declaration between distinct commands.
	// Preserve its full order when prerequisite equivalence is unproven.
	if len(commands) > 1 {
		inv.Complete = false
		inv.Reason = "command prerequisites require ordered full execution"
	}
	for _, command := range commands {
		if slices.Contains(command.Argv, "--no-build") {
			inv.Complete = false
			inv.Reason = "no-build prerequisite freshness is unproven"
			break
		}
	}
	m.Inventory = inv
	var previous *coverage.Manifest
	previousID := ""
	if prior, e := b.DB.LatestValidationEvidence(ctx, auth.Lineage, "assessment"); e == nil {
		var old coverage.Manifest
		if json.Unmarshal(prior.Payload, &old) == nil && old.Version == coverage.Version {
			previous = &old
			previousID = prior.ID
			m.PreviousCommit = old.Commit
			if old.Recipe == m.Recipe && old.Inventory.Conditions == inv.Conditions && old.Inventory.Complete {
				m.FullDuration = old.FullDuration
			}
		}
	} else if !errors.Is(e, store.ErrNotFound) {
		return ActivityResult{}, e
	}
	changes := map[string]coverage.Changes{}
	if previous != nil && b.Coverage != nil && b.Coverage.Changes != nil {
		for _, r := range previous.Results {
			if r.Origin.Commit == "" {
				continue
			}
			if _, ok := changes[r.Origin.Commit]; ok {
				continue
			}
			c, ce := b.Coverage.Changes(ctx, r.Origin.Commit, auth.Commit)
			if ce == nil {
				changes[r.Origin.Commit] = c
			}
		}
	}
	plan := coverage.Build(coverage.PlanInput{Current: inv, Previous: previous, Commands: commands, Changes: changes, Full: auth.Policy == "full", Recipe: m.Recipe})
	fullExecution = plan.Full
	m.FullReason = plan.Reason
	m.Results = append(m.Results, plan.Inherited...)
	m.Results = append(m.Results, plan.Unresolved...)
	if len(plan.Inherited) > 0 && previousID != "" {
		refs = append(refs, previousID)
	}
	if previous != nil {
		m.RequiredFailures = append(m.RequiredFailures, previous.RequiredFailures...)
		for _, r := range previous.Results {
			for _, o := range r.Cases {
				if o.Status == "fail" {
					m.RequiredFailures = append(m.RequiredFailures, o)
				}
			}
		}
	}
	executeCommand := func(c config.BugCheckCommand, units []coverage.Unit, requested map[string][]string, purpose, commit, path string) error {
		r, re := invoke(c, path)
		if re != nil {
			return re
		}
		rawEvidence := struct {
			Format           string    `json:"format"`
			Output           string    `json:"output,omitempty"`
			Structured       string    `json:"structured,omitempty"`
			CaptureComplete  bool      `json:"capture_complete"`
			CleanupConfirmed bool      `json:"cleanup_confirmed"`
			Exit             *int      `json:"exit"`
			Argv             []string  `json:"argv"`
			Commit           string    `json:"commit"`
			Purpose          string    `json:"purpose"`
			Executable       string    `json:"executable"`
			ExecutableDigest string    `json:"executable_digest"`
			CWD              string    `json:"cwd"`
			EnvironmentNames []string  `json:"environment_names"`
			Started          time.Time `json:"started"`
			Ended            time.Time `json:"ended"`
		}{Format: c.Observe, Output: r.Output, Structured: r.Structured, CaptureComplete: r.CaptureComplete, CleanupConfirmed: r.CleanupConfirmed, Exit: r.Exit, Argv: c.Argv, Commit: commit, Purpose: purpose, Executable: r.Executable, ExecutableDigest: r.ExecutableDigest, CWD: r.CWD, EnvironmentNames: slices.Clone(r.Environment), Started: r.Started, Ended: r.Ended}
		rawPayload, marshalErr := json.Marshal(rawEvidence)
		if marshalErr != nil {
			return marshalErr
		}
		rawComplete := r.CaptureComplete && r.CleanupConfirmed
		if len(rawPayload) > store.MaxValidationEvidenceBytes {
			rawComplete = false
			rawEvidence.Output = ""
			rawEvidence.Structured = ""
			rawEvidence.CaptureComplete = false
			rawPayload, marshalErr = json.Marshal(struct {
				Reason, OutputDigest, StructuredDigest string
				Metadata                               any
			}{"raw coverage evidence exceeded retained artifact bound", coverage.Digest(r.Output), coverage.Digest(r.Structured), rawEvidence})
			if marshalErr != nil {
				return marshalErr
			}
		}
		rawRecord, retainErr := b.DB.AppendValidationEvidence(context.WithoutCancel(ctx), store.ValidationEvidence{RequestID: auth.RequestID, RunID: run.ID, Kind: "observation", Version: 1, Complete: rawComplete, Payload: rawPayload})
		if retainErr != nil {
			return retainErr
		}
		refs = append(refs, rawRecord.ID)
		dataOutput := r.Structured
		if dataOutput == "" {
			dataOutput = r.Output
		}
		var observations []coverage.Observation
		complete := rawComplete && r.Exit != nil && ((r.Outcome == "passed" && *r.Exit == 0) || (r.Outcome == "failed" && *r.Exit != 0))
		for _, u := range units {
			obs, done, pe := coverage.ParseResults(u.Runner, u.Key, []byte(dataOutput))
			if pe != nil || !done {
				complete = false
			}
			for _, wanted := range requested[u.Key] {
				found := false
				for _, o := range obs {
					if o.Case == wanted && o.Status != "skip" {
						found = true
					}
				}
				if !found {
					complete = false
				}
			}
			observations = append(observations, obs...)
		}
		if len(units) == 0 {
			complete = false
		}
		observations = coverage.NormalizeDiagnostics(observations, path)
		a := coverage.Attempt{Commit: commit, Purpose: purpose, Conditions: inv.Conditions, Observations: observations, Complete: complete, Fresh: true}
		a.ID = coverage.Digest(a)
		data, me := json.Marshal(a)
		if me != nil {
			return me
		}
		record, me := b.DB.AppendValidationEvidence(context.WithoutCancel(ctx), store.ValidationEvidence{RequestID: auth.RequestID, RunID: run.ID, Kind: "observation", Version: 1, Complete: a.Complete, Payload: data, References: []string{rawRecord.ID}})
		if me != nil {
			return me
		}
		a.ID = record.ID
		m.Attempts = append(m.Attempts, a)
		refs = append(refs, record.ID)
		return nil
	}
	executeWork := func(w coverage.Work, commit, path string) error {
		c, se := coverage.Select(commands[w.Unit.Command], w.Unit, w.Cases, facts[w.Unit.Command])
		if se != nil {
			return se
		}
		return executeCommand(c, []coverage.Unit{w.Unit}, map[string][]string{w.Unit.Key: w.Cases}, w.Purpose, commit, path)
	}
	if plan.Full {
		// Identified failures precede broader coverage when their runner can select
		// them safely. The full request still invokes every original full command.
		for _, w := range plan.Work {
			if w.Purpose == "prior-failure" && inv.Complete && len(commands) == 1 {
				if _, se := coverage.Select(commands[w.Unit.Command], w.Unit, w.Cases, facts[w.Unit.Command]); se == nil {
					if e := executeWork(w, auth.Commit, scratch.Path()); e != nil {
						return finish(e.Error())
					}
				}
			}
		}
		opaque := false
		for i, c := range commands {
			full, fe := coverage.FullCommand(c, facts[i])
			units := []coverage.Unit{}
			for _, u := range inv.Units {
				if u.Command == i {
					units = append(units, u)
				}
			}
			opaqueCommand := fe != nil || len(units) == 0 || !inv.Complete
			if opaqueCommand {
				opaque = true
				if _, e := invoke(c, scratch.Path()); e != nil {
					return finish(e.Error())
				}
			} else if e := executeCommand(full, units, nil, "full", auth.Commit, scratch.Path()); e != nil {
				return finish(e.Error())
			}
			if rawState == store.SuiteBlocked || rawState == store.SuiteCancelled || opaqueCommand && rawState == store.SuiteFailed {
				break
			}
		}
		if opaque {
			m.Outcome = rawState
			m.Complete = false
			if len(m.RequiredFailures) > 0 || len(plan.Unresolved) > 0 {
				m.Outcome = "blocked"
				m.FullReason = "opaque full execution cannot reconcile prior failing identities"
			}
			return finish("")
		}
	} else {
		for _, w := range plan.Work {
			if e := executeWork(w, auth.Commit, scratch.Path()); e != nil {
				return finish(e.Error())
			}
		}
	}
	// Retry each discovered identity once. Containing selectors may reveal new
	// failures; rescan after every round, with a finite identity and work bound.
	retried := map[string]map[string]bool{}
	distinctFailures := 0
	for {
		pending := map[string][]string{}
		for _, a := range m.Attempts {
			if a.Purpose == "base" {
				continue
			}
			for _, o := range a.Observations {
				if o.Status != "fail" || o.Case == "" || retried[o.Unit][o.Case] || slices.Contains(pending[o.Unit], o.Case) {
					continue
				}
				pending[o.Unit] = append(pending[o.Unit], o.Case)
			}
		}
		if len(pending) == 0 {
			break
		}
		progressed := false
		for _, u := range inv.Units {
			cases := pending[u.Key]
			if len(cases) == 0 {
				continue
			}
			if retried[u.Key] == nil {
				retried[u.Key] = map[string]bool{}
			}
			for _, c := range cases {
				retried[u.Key][c] = true
				distinctFailures++
			}
			if distinctFailures > 10000 {
				return finish("coverage failure identity bound exceeded")
			}
			if e := executeWork(coverage.Work{Unit: u, Cases: cases, Purpose: "retry"}, auth.Commit, scratch.Path()); e != nil {
				return finish(e.Error())
			}
			progressed = true
		}
		if !progressed {
			return finish("failing identity has no current coverage unit")
		}
	}
	persistent := map[string][]string{}
	for _, a := range m.Attempts {
		if a.Purpose != "retry" {
			continue
		}
		for _, o := range a.Observations {
			if o.Status == "fail" {
				persistent[o.Unit] = append(persistent[o.Unit], o.Case)
			}
		}
	}
	if len(persistent) > 0 && b.Coverage != nil && b.Coverage.PinBase != nil {
		base, be := b.Coverage.PinBase(ctx)
		if be == nil && isFullSHA(base) {
			m.BaseCommit = base
			baseScratch, be := b.Repo.Begin(ctx, base)
			if be == nil {
				baseBefore, oe := baseScratch.Observe(ctx)
				if oe == nil {
					snapshots[baseScratch.Path()] = guardedSnapshot{scratch: baseScratch, tree: baseBefore}
				}
				baseRecipe, pe := baseScratch.Recipe(ctx)
				// Different recipes cannot establish equivalent base execution.
				equivalent := false
				var baseInv coverage.Inventory
				if oe == nil && pe == nil && config.BugRecipeDigest(baseRecipe) == m.Recipe {
					inDiscovery = true
					baseDiscovery := func(ctx context.Context, c config.BugCheckCommand) (coverage.DiscoveryResult, error) {
						r, e := invoke(c, baseScratch.Path())
						return coverage.DiscoveryResult{Output: []byte(r.Output), Complete: r.CaptureComplete && r.CleanupConfirmed && r.Outcome == "passed"}, e
					}
					baseFacts := map[int]coverage.Facts{}
					if b.Coverage.Discover != nil {
						baseInv, baseFacts, be = b.Coverage.Discover(ctx, baseScratch, commands, baseDiscovery)
					} else {
						baseInv.Complete = true
						for i, c := range commands {
							one, f, e := coverage.Discover(ctx, c, i, base, baseScratch.Path(), baseDiscovery)
							if e != nil || !one.Complete {
								baseInv.Complete = false
							}
							baseInv.Units = append(baseInv.Units, one.Units...)
							baseFacts[i] = f
						}
					}
					inDiscovery = false
					baseInputs, baseInputErr := observeCommandInputs(baseScratch.Path())
					equivalent = be == nil && baseInv.Complete && coverage.Digest(baseFacts) == coverage.Digest(facts) && inputErr == nil && baseInputErr == nil && coverage.Digest(baseInputs) == coverage.Digest(executionInputs)
					if equivalent {
						for _, u := range inv.Units {
							if len(persistent[u.Key]) == 0 {
								continue
							}
							matched := slices.ContainsFunc(baseInv.Units, func(baseUnit coverage.Unit) bool {
								return baseUnit.Key == u.Key && baseUnit.Command == u.Command && baseUnit.Runner == u.Runner && baseUnit.Selector == u.Selector
							})
							if !matched {
								equivalent = false
								break
							}
						}
					}
					if guardSnapshot(baseScratch.Path()) != nil {
						equivalent = false
					}
				}
				if equivalent {
					for _, u := range inv.Units {
						if cases := persistent[u.Key]; len(cases) > 0 {
							if be = executeWork(coverage.Work{Unit: u, Cases: cases, Purpose: "base"}, base, baseScratch.Path()); be != nil {
								break
							}
						}
					}
				}
				after, ae := baseScratch.Observe(context.WithoutCancel(ctx))
				if ae != nil || after != baseBefore || be != nil {
					for i := range m.Attempts {
						if m.Attempts[i].Purpose == "base" {
							m.Attempts[i].Complete = false
						}
					}
				}
				if !preserveScratch {
					if ce := baseScratch.Close(context.WithoutCancel(ctx)); ce != nil {
						rawState = store.SuiteBlocked
						return finish(ce.Error())
					}
				}
			}
		}
	}
	m = coverage.Assess(m)
	if plan.Full && m.Complete {
		m.FullDuration = h.now(opts).Sub(run.StartedAt)
	}
	return finish("")
}
