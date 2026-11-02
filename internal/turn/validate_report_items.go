package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
)

// ValidateFactoryBugCommand files one owner-confirmed upstream factory Bug for
// an item the summary proposed it for:
//
//	/validate-factory-bug <revision> <item>
const ValidateFactoryBugCommand = "/validate-factory-bug"

// ValidateBringMainCommand brings the target branch into the feature branch
// through Implement once main is fixed.
const ValidateBringMainCommand = "/implement Bring the target branch into the feature branch."

// validateFailureRefPattern is the reference of one failing suite command.
var validateFailureRefPattern = regexp.MustCompile(`^failure-[1-9][0-9]{0,2}$`)

// validateFailureRef names failure i (0-based) of the tested commit.
func validateFailureRef(i int) string { return fmt.Sprintf("failure-%d", i+1) }

// loadFailures keeps the failing suite commands of the tested commit: the
// handback's when there is one, else the retained suite run's.
func (s *ValidateState) loadFailures(ctx context.Context, opts Options) error {
	s.Failures = nil
	if s.Handback != nil && len(s.Handback.Failures) > 0 {
		s.Failures = slices.Clone(s.Handback.Failures)
		return nil
	}
	if suiteCoverageSatisfied(s.Suite) || s.Suite.Outcome == "" {
		return nil
	}
	if s.Suite.RunID > 0 {
		commands, err := opts.DB.SuiteCommands(ctx, s.Suite.RunID)
		if err != nil {
			return err
		}
		for _, c := range commands {
			if c.State == store.SuitePassed {
				continue
			}
			s.Failures = append(s.Failures, HandbackFailure{Command: strings.Join(c.Argv, " "), Argv: slices.Clone(c.Argv), Exit: c.Exit, Reason: c.Reason, OutputTail: c.OutputTail, SuiteRecord: fmt.Sprintf("suite run %d, command %d", c.RunID, c.Position)})
		}
	}
	if len(s.Failures) == 0 {
		s.Failures = []HandbackFailure{{Command: "full suite", Reason: firstNonBlank(s.Suite.Reason, "the suite did not pass")}}
	}
	return nil
}

// loadReporter reads the reporter's stored summaries, answers and the factory
// Bug rows of the run. The summary in force is the newest accepted one of the
// tested commit; a follow-up without a revised summary keeps the earlier one.
func (s *ValidateState) loadReporter(ctx context.Context, opts Options) error {
	s.Summary, s.Advice, s.ReporterFallback, s.Answers = nil, 0, "", nil
	rows, err := opts.DB.ListValidateReporter(ctx, s.InstanceID)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r.Mode == store.ReporterModeFollowUp {
			s.Answers = append(s.Answers, r)
		}
		if r.Commit != s.Commit {
			continue
		}
		if r.Mode == store.ReporterModeReport && r.State == store.ReporterFallback {
			s.ReporterFallback = firstNonBlank(r.Reason, "the reporter gave no usable summary")
		}
		if r.State != store.ReporterAccepted {
			continue
		}
		var report agentresult.ValidateReport
		if err := json.Unmarshal([]byte(r.Result), &report); err != nil {
			return fmt.Errorf("the stored reporter summary is unreadable: %w", err)
		}
		if report.Next == nil || strings.TrimSpace(report.Headline) == "" {
			continue
		}
		s.Summary = &report
		s.Advice++
	}
	bugs, err := opts.DB.ListValidateFactoryBugs(ctx, s.InstanceID, s.Commit)
	if err != nil {
		return err
	}
	s.FactoryBugs = map[string]store.ValidateFactoryBug{}
	for _, b := range bugs {
		s.FactoryBugs[b.Item] = b
	}
	return nil
}

// followUpsUsed counts the owner comments the reporter has taken in this run.
func (s ValidateState) followUpsUsed() int { return len(s.Answers) }

// validateReportItem is one item the summary can recommend an action for:
// a failing suite command, an open finding or a pending repair candidate.
type validateReportItem struct {
	Ref       string
	Failure   *HandbackFailure
	Finding   *ValidateFinding
	Candidate *store.RepairCandidate
	Actions   []string
}

// closed says delivery or a handback ended the owner's part of this run.
func (s ValidateState) closed() bool {
	return s.Reimplementing || effectDone(s.Effects.Close)
}

// reportItems lists the items of the current state in a fixed order, each
// with the actions it admits now. Every action renders an exact command.
func (s ValidateState) reportItems() []validateReportItem {
	if s.closed() {
		return nil
	}
	var out []validateReportItem
	sendBack := s.implementLine() != ""
	for i := range s.Failures {
		f := s.Failures[i]
		actions := []string{}
		if sendBack {
			actions = append(actions, agentresult.ReportActionSendBack)
		}
		actions = append(actions, agentresult.ReportActionRerun, agentresult.ReportActionWaitForMain)
		if s.FactoryBugFiling {
			actions = append(actions, agentresult.ReportActionFactoryBug)
		}
		out = append(out, validateReportItem{Ref: validateFailureRef(i), Failure: &f, Actions: actions})
	}
	for i := range s.Findings {
		f := s.Findings[i]
		if f.State == findingNotReviewed || (!validateOwnerItem(s, f) && !validateItemBlocks(f)) {
			continue
		}
		var actions []string
		if f.State == findingAwaitingCheck || f.State == findingOwnerFailed && f.checkable() {
			actions = append(actions, agentresult.ReportActionCheckPass, agentresult.ReportActionCheckFail)
		}
		switch {
		case f.needsChoice() && f.Choice == nil && f.State != findingAwaitingCheck:
			actions = append(actions, agentresult.ReportActionRepair, agentresult.ReportActionIssueBug, agentresult.ReportActionIssueStory)
			if f.Optional {
				actions = append(actions, agentresult.ReportActionDefer)
			}
		case f.Choice != nil && f.Choice.Choice == store.ChoiceIssue:
			if e, ok := s.Effects.Issues[f.ID]; ok && (e.State == store.EffectFailed || e.State == store.EffectUncertain) {
				actions = append(actions, "issue-"+f.Choice.IssueKind)
			}
		}
		if sendBack && implementBlocks(f) {
			actions = append(actions, agentresult.ReportActionSendBack)
		}
		if s.FactoryBugFiling {
			actions = append(actions, agentresult.ReportActionFactoryBug)
		}
		if len(actions) == 0 {
			continue
		}
		out = append(out, validateReportItem{Ref: f.ID, Finding: &f, Actions: actions})
	}
	for i := range s.Candidates {
		c := s.Candidates[i]
		if c.Resolution != "pending" {
			continue
		}
		actions := []string{agentresult.ReportActionRepair}
		if sendBack {
			actions = append(actions, agentresult.ReportActionSendBack)
		}
		out = append(out, validateReportItem{Ref: c.ID, Candidate: &c, Actions: actions})
	}
	return out
}

// reportItem finds one item of the current state by reference.
func (s ValidateState) reportItem(ref string) (validateReportItem, bool) {
	for _, item := range s.reportItems() {
		if item.Ref == ref {
			return item, true
		}
	}
	return validateReportItem{}, false
}

// reportGlobalActions are the actions the next step may take for the whole
// run, without an item.
func (s ValidateState) reportGlobalActions() []string {
	if s.closed() {
		return nil
	}
	var out []string
	if len(s.shipBlockers()) == 0 {
		out = append(out, agentresult.ReportActionShip)
	}
	if s.implementLine() != "" {
		out = append(out, agentresult.ReportActionSendBack)
	}
	out = append(out, agentresult.ReportActionRerun)
	if !suiteCoverageSatisfied(s.Suite) {
		out = append(out, agentresult.ReportActionWaitForMain)
	}
	return out
}

// shipReference is the displayed decision reference of the ship command.
func (s ValidateState) shipReference() string {
	return store.OwnerDecisionReference(s.DecisionTask, ValidateShipCommand, validateDecisionTarget(s), s.Revision)
}

// reportCommand renders the exact command of one action for one item (nil
// for an action on the whole run). ok is false when the action does not apply
// to the item in the current state, so a stale recommendation never renders
// a command the factory would refuse.
func (s ValidateState) reportCommand(item *validateReportItem, action string) (string, bool) {
	if item != nil && !slices.Contains(item.Actions, action) {
		return "", false
	}
	if item == nil && !slices.Contains(s.reportGlobalActions(), action) {
		return "", false
	}
	rev := s.Revision
	finding := func(choice string) (string, bool) {
		if item == nil || item.Finding == nil {
			return "", false
		}
		return fmt.Sprintf("%s %s %s %s", ValidateFindingCommand, rev, item.Ref, choice), true
	}
	switch action {
	case agentresult.ReportActionCheckPass, agentresult.ReportActionCheckFail:
		if item == nil || item.Finding == nil {
			return "", false
		}
		if action == agentresult.ReportActionCheckPass {
			return fmt.Sprintf("%s %s %s pass -- <what you checked>", ValidateCheckCommand, rev, item.Ref), true
		}
		return fmt.Sprintf("%s %s %s fail -- <what you saw>", ValidateCheckCommand, rev, item.Ref), true
	case agentresult.ReportActionRepair:
		if item != nil && item.Candidate != nil {
			c := item.Candidate
			verb := "inspect"
			if c.Eligible && c.ReviewComplete && s.RepairAdoption {
				verb = "accept"
			}
			return fmt.Sprintf("%s %s %s %s %s", ValidateRepairCommand, rev, c.ID, c.ReviewDigest, verb), true
		}
		return finding(store.ChoiceRepair)
	case agentresult.ReportActionDefer:
		return finding(store.ChoiceDefer)
	case agentresult.ReportActionIssueBug:
		return finding("issue bug")
	case agentresult.ReportActionIssueStory:
		return finding("issue story")
	case agentresult.ReportActionSendBack:
		return fmt.Sprintf("%s %s", ValidateImplementCommand, rev), true
	case agentresult.ReportActionShip:
		return fmt.Sprintf("%s %s -- <reason>", ValidateShipCommand, s.shipReference()), true
	case agentresult.ReportActionRerun:
		return ValidateStartCommand, true
	case agentresult.ReportActionFactoryBug:
		if item == nil {
			return "", false
		}
		if b, ok := s.FactoryBugs[item.Ref]; ok && b.State != store.FactoryBugFailed {
			return "", false
		}
		return fmt.Sprintf("%s %s %s", ValidateFactoryBugCommand, rev, item.Ref), true
	case agentresult.ReportActionWaitForMain:
		return ValidateBringMainCommand, true
	}
	return "", false
}

// validateReportScope is the trusted context the reporter's result is judged
// against: every current item with the actions it admits, the run's actions,
// the citable checks and the evidence permalink prefixes.
func validateReportScope(st ValidateState, followUp bool, permalinks []string) *agentresult.ReportScope {
	scope := &agentresult.ReportScope{FollowUp: followUp, Items: map[string][]string{}, Global: st.reportGlobalActions(), Evidence: map[string]bool{}, Permalinks: slices.Clone(permalinks)}
	for _, item := range st.reportItems() {
		var actions []string
		for _, a := range item.Actions {
			if _, ok := st.reportCommand(&item, a); ok {
				actions = append(actions, a)
			}
		}
		if len(actions) > 0 {
			scope.Items[item.Ref] = actions
		}
	}
	for _, f := range st.Findings {
		for _, c := range f.Checks {
			scope.Evidence[c] = true
		}
		scope.Evidence[f.ID] = true
	}
	for i := range st.Failures {
		scope.Evidence[validateFailureRef(i)] = true
	}
	return scope
}
