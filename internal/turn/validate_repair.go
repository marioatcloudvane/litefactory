package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// ValidateRepairBudgetActivity starts one repair round. Before anything else it
// reconciles retained evidence for an unfinished round before a guarded reset;
// the round still counts as used. It then returns run
// while the graph's declared bound allows another round, spent when it does not.
type ValidateRepairBudgetActivity struct{ b ValidateFlowBindings }

// Execute returns run or spent.
func (h ValidateRepairBudgetActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	step, err := loadValidateStep(ctx, opts, req, h.b.Profile, graphexec.ValidateRepairBudgetAction)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	bound, err := strconv.Atoi(step.node.Attrs["lf_rounds"])
	if err != nil || bound < 1 || bound > store.MaxRepairRounds {
		err = fmt.Errorf("lf_rounds %q must be 1 to %d", step.node.Attrs["lf_rounds"], store.MaxRepairRounds)
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	worktree, session, err := h.open()
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	instance := req.InstanceID
	if running, err := opts.DB.RunningRepairRound(ctx, instance); err == nil {
		if err := h.settleInterrupted(ctx, opts, running, worktree, session); err != nil {
			return ActivityResult{Diagnostic: err.Error()}, err
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return ActivityResult{}, err
	}
	rounds, err := opts.DB.ListRepairRounds(ctx, instance)
	if err != nil {
		return ActivityResult{}, err
	}
	if len(rounds) >= bound {
		return validateJSONResult("spent", map[string]any{"rounds_used": len(rounds), "bound": bound})
	}
	suite, err := latestValidateSuite(ctx, opts, instance, step.node.Attrs["lf_suite"])
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	if suite.Output.Outcome != store.SuiteFailed {
		chosen, err := ownerRepairChosen(ctx, opts, instance, suite.Output)
		if err != nil {
			return ActivityResult{Diagnostic: err.Error()}, err
		}
		if !chosen {
			err := fmt.Errorf("a repair round starts only on a failed suite or on findings the owner chose to repair, not on a %q suite", suite.Output.Outcome)
			return ActivityResult{Diagnostic: err.Error()}, err
		}
	}
	// The round starts from exactly the tested commit and a clean tree.
	if err := worktree.ResetHard(ctx, suite.Output.Commit, session); err != nil {
		return ActivityResult{Diagnostic: err.Error()}, fmt.Errorf("prepare the repair worktree: %w", err)
	}
	round, err := opts.DB.BeginRepairRound(ctx, store.RepairRound{InstanceID: instance, SuiteRunID: suite.Output.RunID, PreCommit: suite.Output.Commit, StartedAt: h.b.now(opts)})
	if err != nil {
		if errors.Is(err, store.ErrRepairRoundLimit) {
			return validateJSONResult("spent", map[string]any{"rounds_used": len(rounds), "bound": store.MaxRepairRounds})
		}
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	return validateJSONResult("run", map[string]any{"round": round.Round, "pre_commit": round.PreCommit, "rounds_used": round.Round, "bound": bound})
}

// ownerRepairChosen says the owner chose repair for at least one finding of a
// passed suite's tested commit.
func ownerRepairChosen(ctx context.Context, opts Options, instance int64, suite validateSuiteOutput) (bool, error) {
	if suite.Outcome != store.SuitePassed {
		return false, nil
	}
	choices, err := opts.DB.ListValidateChoices(ctx, instance, suite.Commit, false)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(choices, func(c store.ValidateChoice) bool { return c.Choice == store.ChoiceRepair }), nil
}

func (h ValidateRepairBudgetActivity) open() (*git.Worktree, string, error) {
	return openValidateWorktree(h.b)
}

func openValidateWorktree(b ValidateFlowBindings) (*git.Worktree, string, error) {
	if b.Worktree == nil {
		return nil, "", errors.New("no worktree is admitted for the repair")
	}
	worktree, err := b.Worktree()
	if err != nil {
		return nil, "", fmt.Errorf("worktree unavailable: %w", err)
	}
	session := ""
	if b.Session != nil {
		if session, err = b.Session(); err != nil {
			return nil, "", fmt.Errorf("session directory unavailable: %w", err)
		}
	}
	return worktree, session, nil
}

// settleInterrupted completes saved capture and reset boundaries before settling
// the round. A saved flagged outcome remains historical; an unmeasured round is
// retained as interrupted and cannot become eligible through missing analysis.
func (h ValidateRepairBudgetActivity) settleInterrupted(ctx context.Context, opts Options, round store.RepairRound, worktree *git.Worktree, session string) error {
	req := ActivityRequest{InstanceID: round.InstanceID}
	var snapshot git.RepairSnapshot
	candidates, err := opts.DB.ListRepairCandidates(ctx, round.InstanceID)
	if err != nil {
		return err
	}
	var candidate store.RepairCandidate
	for _, c := range candidates {
		if c.RoundID == round.ID {
			candidate = c
			snapshot, err = candidateSnapshot(c)
			if err != nil {
				return err
			}
		}
	}
	finish := store.RepairFinish{State: store.RepairInterrupted, Reason: "the round was interrupted before completion; retained for inspection", At: h.b.now(opts)}
	if snapshot.Tree == "" {
		snapshot, err = worktree.CaptureRepairCandidate(ctx, round.PreCommit, fmt.Sprintf("%d:%d", round.InstanceID, round.ID))
		if err != nil {
			return err
		}
		candidate, err = h.b.retainRepair(ctx, opts, req, round, snapshot, finish, false)
		if err != nil {
			return err
		}
	}
	if err = h.b.completeRepairCapture(ctx, opts, candidate); err != nil {
		return err
	}
	var evidence candidateEvidence
	if err = json.Unmarshal(candidate.Evidence, &evidence); err != nil {
		return err
	}
	finish = evidence.Finish
	finish.At = h.b.now(opts)
	journal := store.RepairCandidateJournal{InstanceID: round.InstanceID, RoundID: round.ID, Phase: "reset", State: "intent", ExpectedBase: round.PreCommit, At: h.b.now(opts)}
	if err = opts.DB.JournalRepairCandidate(ctx, candidate.ID, journal); err != nil {
		return err
	}
	if err = worktree.VerifyRepairCandidate(ctx, snapshot); err != nil {
		return err
	}
	if err = worktree.ResetRepairCandidate(ctx, snapshot, session); err != nil {
		return err
	}
	journal.State = "complete"
	if err = opts.DB.JournalRepairCandidate(ctx, candidate.ID, journal); err != nil {
		return err
	}
	return opts.DB.FinishRepairRound(ctx, round.ID, finish)
}

func validateJSONResult(outcome string, v any) (ActivityResult, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return ActivityResult{}, err
	}
	return ActivityResult{Outcome: outcome, Kind: store.GraphOutputJSON, Output: raw}, nil
}

// ValidateRepairCheckActivity measures the change a repair round really made
// against the commit before it, with the limits its graph node declares. A small
// change is kept as a new commit; a change over the limits is retained as a diff
// outside adopted work before reset. It never trusts the agent's account of its own change.
//
//	kept        a small change, committed; the suite runs again on the new commit
//	too_big     over the file limit or a forbidden kind of change; reset
//	handback    the agent reported the fix as too large or uncertain; reset
//	unfinished  the round did not finish (timeout, failure, unusable or empty
//	            result); reset, and the round counts as used
//	blocked     the agent could not work, or the change cannot be measured; reset
type ValidateRepairCheckActivity struct{ b ValidateFlowBindings }

// repairDiagnosis is what the round retains of the agent's account.
type repairDiagnosis struct {
	Response string                      `json:"response"`
	Reason   string                      `json:"reason,omitempty"`
	Failures []agentresult.RepairFailure `json:"failures,omitempty"`
}

// Execute settles the running round of the instance.
func (h ValidateRepairCheckActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (res ActivityResult, err error) {
	step, err := loadValidateStep(ctx, opts, req, h.b.Profile, graphexec.ValidateRepairCheckAction)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	maxFiles, forbid, err := decodeValidateLimits(step.node.Attrs)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	round, err := opts.DB.RunningRepairRound(ctx, req.InstanceID)
	if errors.Is(err, store.ErrNotFound) {
		return h.replay(ctx, opts, req)
	}
	if err != nil {
		return ActivityResult{}, err
	}
	worktree, session, err := openValidateWorktree(h.b)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	// A saved candidate is authoritative after a reset interruption. Otherwise
	// capture the complete immutable tree before measuring or resetting it.
	var snapshot git.RepairSnapshot
	candidates, err := opts.DB.ListRepairCandidates(ctx, req.InstanceID)
	if err != nil {
		return ActivityResult{}, err
	}
	for _, c := range candidates {
		if c.RoundID == round.ID {
			snapshot, err = candidateSnapshot(c)
			if err != nil {
				return ActivityResult{}, err
			}
		}
	}
	if snapshot.Tree == "" {
		snapshot, err = worktree.CaptureRepairCandidate(ctx, round.PreCommit, fmt.Sprintf("%d:%d", req.InstanceID, round.ID))
		if err != nil {
			return ActivityResult{Diagnostic: err.Error()}, fmt.Errorf("retain repair before reset: %w", err)
		}
	}
	// finish saves complete evidence before reset, journals the verified reset,
	// and settles the immutable round. An unfinished boundary holds the worktree.
	finish := func(outcome string, in store.RepairFinish) (ActivityResult, error) {
		in.At = h.b.now(opts)
		if outcome != "kept" {
			var changed []string
			for _, change := range snapshot.Changes {
				changed = append(changed, change.Path)
			}
			files, kinds, _ := measureValidateChange(changed, snapshot.Patch, session, in.Kinds, forbid)
			in.Files, in.Kinds, in.Diff = files, kinds, snapshot.Patch
			sum := sha256.Sum256([]byte(snapshot.Patch))
			in.DiffDigest = hex.EncodeToString(sum[:])
			c, err := h.b.retainRepair(ctx, opts, req, round, snapshot, in, candidateMeasurementComplete(snapshot) && (outcome == "too_big" || outcome == "handback"))
			if err != nil {
				return ActivityResult{Diagnostic: err.Error()}, err
			}
			journal := store.RepairCandidateJournal{InstanceID: req.InstanceID, RoundID: round.ID, Phase: "reset", State: "intent", ExpectedBase: round.PreCommit, At: h.b.now(opts)}
			if err := opts.DB.JournalRepairCandidate(ctx, c.ID, journal); err != nil {
				return ActivityResult{}, err
			}
			if err := worktree.VerifyRepairCandidate(ctx, snapshot); err != nil {
				return ActivityResult{}, err
			}
			if err := worktree.ResetRepairCandidate(ctx, snapshot, session); err != nil {
				return ActivityResult{Diagnostic: err.Error()}, fmt.Errorf("reset retained repair: %w", err)
			}
			journal.State = "complete"
			if err := opts.DB.JournalRepairCandidate(ctx, c.ID, journal); err != nil {
				return ActivityResult{}, err
			}
		}
		if err := opts.DB.FinishRepairRound(ctx, round.ID, in); err != nil {
			return ActivityResult{Diagnostic: err.Error()}, err
		}
		return validateJSONResult(outcome, map[string]any{"round": round.Round, "state": in.State, "reason": in.Reason, "files": in.Files, "result_commit": in.ResultCommit, "diff_digest": in.DiffDigest})
	}

	source, err := opts.DB.LatestGraphNodeAttempt(ctx, req.InstanceID, step.node.Attrs["lf_source"])
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, fmt.Errorf("find the repair attempt: %w", err)
	}
	if source.Status != store.GraphSucceeded || source.Outcome != "success" {
		state, reason := store.RepairInterrupted, "the repair agent did not finish"
		if _, tErr := opts.DB.GetAttemptTimeout(ctx, source.ID); tErr == nil {
			state, reason = store.RepairTimedOut, "the repair agent ran out of its time budget and was cancelled"
		}
		return finish("unfinished", store.RepairFinish{NodeAttemptID: source.ID, State: state, Reason: reason})
	}
	scope, err := h.scope(ctx, source)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	node, err := ValidateWorkflowPolicy().load(ctx, opts.DB, source.ID, scope)
	if err != nil || node.Result.Payload.Validate == nil || node.Result.Payload.Validate.Repair == nil {
		return finish("unfinished", store.RepairFinish{NodeAttemptID: source.ID, State: store.RepairUnusable, Reason: "the repair result is unusable: " + clipText(fmt.Sprint(err), 300)})
	}
	report := *node.Result.Payload.Validate.Repair
	diagnosis, _ := json.Marshal(repairDiagnosis{Response: report.Response, Reason: report.Reason, Failures: report.Failures})
	base := store.RepairFinish{NodeAttemptID: source.ID, Response: report.Response, Diagnosis: string(diagnosis)}
	for _, f := range report.Failures {
		base.Kinds = append(base.Kinds, f.ChangeKinds...)
	}
	switch report.Response {
	case agentresult.RepairResponseBlocked:
		in := base
		in.State, in.Reason = store.RepairBlocked, clipText(report.Reason, 500)
		return finish("blocked", in)
	case agentresult.RepairResponseHandback:
		in := base
		in.State, in.Reason = store.RepairHandback, "the repair agent reported the fix as too large or uncertain"
		if report.Reason != "" {
			in.Reason += ": " + report.Reason
		}
		return finish("handback", in)
	}

	var changed []string
	for _, change := range snapshot.Changes {
		changed = append(changed, change.Path)
	}
	diff := snapshot.Patch
	var declared []string
	for _, f := range report.Failures {
		declared = append(declared, f.ChangeKinds...)
	}
	files, kinds, forbidden := measureValidateChange(changed, diff, session, declared, forbid)
	sum := sha256.Sum256([]byte(diff))
	base.Files, base.Kinds, base.Diff, base.DiffDigest = files, kinds, diff, hex.EncodeToString(sum[:])
	if len(files) == 0 && len(forbidden) == 0 {
		in := base
		in.State, in.Reason = store.RepairUnusable, "the agent reported a repair but no change was found in the worktree"
		return finish("unfinished", in)
	}
	if len(files) > maxFiles || len(forbidden) > 0 || !candidateMeasurementComplete(snapshot) {
		in := base
		in.State = store.RepairTooBig
		in.Reason = fmt.Sprintf("%d files changed, at most %d allowed", len(files), maxFiles)
		if len(forbidden) > 0 {
			in.Reason += "; forbidden kinds of change: " + strings.Join(forbidden, ", ")
		}
		return finish("too_big", in)
	}
	if h.b.KeepChange == nil {
		in := base
		in.State, in.Reason = store.RepairBlocked, "no commit service is admitted"
		return finish("blocked", in)
	}
	head, err := h.b.KeepChange(ctx, fmt.Sprintf("Repair failing tests (Validate round %d)", round.Round))
	if err != nil {
		if ctx.Err() != nil {
			return ActivityResult{Diagnostic: ctx.Err().Error()}, ctx.Err()
		}
		in := base
		in.State, in.Reason = store.RepairBlocked, "the repair could not be committed: "+clipText(err.Error(), 300)
		return finish("blocked", in)
	}
	if !isFullSHA(head) || head == round.PreCommit {
		in := base
		in.State, in.Reason = store.RepairBlocked, "the commit service returned no new commit"
		return finish("blocked", in)
	}
	in := base
	in.State, in.ResultCommit = store.RepairKept, head
	return finish("kept", in)
}

func (h ValidateRepairCheckActivity) scope(ctx context.Context, source store.GraphNodeAttempt) (agentresult.Context, error) {
	if h.b.Scope == nil {
		return agentresult.Context{}, errors.New("no result scope is admitted for the repair")
	}
	scope, err := h.b.Scope(ctx, source)
	if err != nil {
		return scope, err
	}
	scope.RepairRole, scope.WriteAccess = true, true
	return scope, nil
}

// replay answers a step that runs again after its round was already settled with
// the same outcome, so a lost reply never measures or commits twice.
func (h ValidateRepairCheckActivity) replay(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	rounds, err := opts.DB.ListRepairRounds(ctx, req.InstanceID)
	if err != nil {
		return ActivityResult{}, err
	}
	if len(rounds) == 0 {
		err := errors.New("no repair round is running, so there is nothing to measure")
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	last := rounds[len(rounds)-1]
	outcome, ok := repairOutcomeOf(last)
	if !ok {
		err := fmt.Errorf("repair round %d is %s, not settled", last.Round, last.State)
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	return validateJSONResult(outcome, map[string]any{"round": last.Round, "state": last.State, "reason": last.Reason, "files": last.Files, "result_commit": last.ResultCommit, "diff_digest": last.DiffDigest})
}

// repairOutcomeOf maps a settled round back to the outcome its check returned.
func repairOutcomeOf(r store.RepairRound) (string, bool) {
	switch r.State {
	case store.RepairKept:
		return "kept", true
	case store.RepairTooBig:
		return "too_big", true
	case store.RepairHandback:
		return "handback", true
	case store.RepairBlocked:
		return "blocked", true
	case store.RepairTimedOut, store.RepairInterrupted, store.RepairUnusable:
		return "unfinished", true
	}
	return "", false
}

// decodeValidateLimits reads the limits a repair check declares.
func decodeValidateLimits(attrs map[string]string) (int, []string, error) {
	maxFiles, err := strconv.Atoi(attrs["lf_max_files"])
	if err != nil || maxFiles < 1 || maxFiles > 5 {
		return 0, nil, fmt.Errorf("lf_max_files %q must be a whole number from 1 to 5", attrs["lf_max_files"])
	}
	var forbid []string
	for kind := range strings.SplitSeq(attrs["lf_forbid"], ",") {
		kind = strings.TrimSpace(kind)
		if !slices.Contains(graphexec.ValidateForbidKinds, kind) {
			return 0, nil, fmt.Errorf("lf_forbid element %q is outside the declared kinds", kind)
		}
		forbid = append(forbid, kind)
	}
	return maxFiles, forbid, nil
}

// measureValidateChange counts the unique changed paths outside the session
// directory and finds which forbidden kinds the paths, the diff and the agent's
// declaration show. It is pure over its inputs.
func measureValidateChange(changed []string, diff, session string, declared, forbid []string) (files, kinds, forbidden []string) {
	found := map[string]bool{}
	inSession := func(name string) bool {
		s := strings.TrimSuffix(session, "/")
		return s != "" && (name == s || strings.HasPrefix(name, s+"/"))
	}
	for _, name := range changed {
		if inSession(name) {
			// The session directory holds the approved scope. A repair has no
			// business there, and a change in it is a changed criterion.
			found["criteria"] = true
			continue
		}
		files = append(files, name)
		for _, kind := range spikePathKinds(name) {
			found[kind] = true
		}
		if isCriteriaFile(name) {
			found["criteria"] = true
		}
	}
	for _, kind := range declared {
		found[kind] = true
	}
	if repairChangesTests(diff) {
		found["tests"] = true
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

// isCriteriaFile names the files that hold approved acceptance criteria.
func isCriteriaFile(name string) bool {
	base := strings.ToLower(path.Base(name))
	return base == "plan_brief.md" || base == "research_brief.md" || base == "research_technical.md" || strings.Contains(base, "acceptance")
}

var (
	testFilePattern   = regexp.MustCompile(`(?i)(_test\.go|(^|/)test_[^/]*\.py|_test\.py|\.(test|spec)\.[a-z]+|(^|/)(tests?|__tests__)/|(^|/)[A-Za-z0-9]*Tests?\.(java|kt|cs)$)`)
	testMarkerPattern = regexp.MustCompile(`^\s*(func (Test|Benchmark|Fuzz)[A-Za-z0-9_]*\(|def test_|(it|test|describe)\(|@Test\b|\[Fact\]|#\[test\])`)
	skipPattern       = regexp.MustCompile(`(\.Skip(Now|f)?\(|\.skip\(|\bxit\(|\bxdescribe\(|@pytest\.mark\.skip|pytest\.skip\(|@unittest\.skip|@Disabled\b|@Ignore\b|#\[ignore\])`)
)

// repairChangesTests reports whether a diff deletes a test, deletes a test file
// or adds a skip. It reads only the diff text.
func repairChangesTests(diff string) bool {
	file, removed, added, deleted := "", 0, 0, false
	flush := func() bool {
		if file != "" && testFilePattern.MatchString(file) && (deleted || removed > added) {
			return true
		}
		return false
	}
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			if flush() {
				return true
			}
			removed, added, deleted = 0, 0, false
			fields := strings.Fields(line)
			file = strings.TrimPrefix(fields[len(fields)-1], "b/")
		case strings.HasPrefix(line, "deleted file mode"):
			deleted = true
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
		case strings.HasPrefix(line, "-") && testMarkerPattern.MatchString(line[1:]):
			removed++
		case strings.HasPrefix(line, "+") && testMarkerPattern.MatchString(line[1:]):
			added++
		}
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") && skipPattern.MatchString(line[1:]) {
			return true
		}
	}
	return flush()
}
