package turn

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/commandtext"
	"github.com/cloudvane/litefactory/internal/store"
)

// The Validate owner commands. They are exact: one command, the report revision
// it was written for, and fixed tokens. They never collide with the /validate
// entry point or the legacy /deliver, /merge, /followup and /waive commands.
const (
	ValidateRepairCommand  = "/validate-repair"
	ValidateCheckCommand   = "/validate-check"
	ValidateFindingCommand = "/validate-finding"
	ValidatePushCommand    = "/validate-push"
	ValidateMergeCommand   = "/validate-merge"
	ValidateCloseCommand   = "/validate-close"
	ValidateShipCommand    = "/validate-ship"
	// ValidateImplementCommand sends the blocking findings back to Implement.
	ValidateImplementCommand = "/validate-implement"
	// ValidateReturnCommand returns reviewed work with an owner explanation.
	ValidateReturnCommand = "/return"
)

// ValidateControlCommands lists the Validate owner commands.
var ValidateControlCommands = []string{ValidateRepairCommand, ValidateCheckCommand, ValidateFindingCommand, ValidatePushCommand, ValidateMergeCommand, ValidateCloseCommand, ValidateShipCommand, ValidateImplementCommand, ValidateReturnCommand, ValidateFactoryBugCommand}

// IsValidateControlCommand reports whether a command token is one of them.
func IsValidateControlCommand(name string) bool {
	for _, c := range ValidateControlCommands {
		if name == c {
			return true
		}
	}
	return false
}

// validateCommandLimit bounds a command line; validateBodyLimit the whole
// comment, guidance included.
const (
	validateCommandLimit = 2048
	validateBodyLimit    = 65536
)

// ValidateCommand is one parsed owner command.
type ValidateCommand struct {
	Candidate    string
	ReviewDigest string
	RepairAction string
	Name         string
	Revision     string
	// Reference is the optional short handle of a published exact target.
	Reference string
	// Check
	Criterion string
	Verdict   string
	Evidence  string
	// Finding
	Finding   string
	Choice    string
	IssueKind string
	// Implement: the owner's optional guidance for the repairing worker.
	Guidance string
	// Merge
	PR   int
	Head string
	// Close
	Issues []int
	// Factory Bug: the item the owner confirms an upstream factory Bug for.
	Item string
}

var (
	validateRevisionPattern = regexp.MustCompile(`^[0-9a-f]{12}$`)
	validateFindingPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}:[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
	validateHeadPattern     = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)
	// validateRevisionLike is a mistyped revision, refused rather than read as
	// guidance so a command never silently targets the current report instead.
	validateRevisionLike = regexp.MustCompile(`^[0-9a-fA-F]{8,16}$`)
)

// validateChoiceAccept is the owner's acceptance of a blocking review finding
// as a deliberate deviation. It is recorded as an owner pass verdict with the
// reason as its evidence, never as a choice.
const validateChoiceAccept = "accept"

// ParseValidateCommand parses the exact grammar:
//
//	/validate-check   <revision> <criterion or review id> pass|fail -- <evidence>
//	/validate-finding <revision> <finding id> repair|defer
//	/validate-finding <revision> <finding id> issue bug|story
//	/validate-finding <revision> <finding id> accept -- <reason>
//	/validate-push    <revision>
//	/validate-merge   <revision> <pr number> <head sha>
//	/validate-close   <revision> <issue number>...
//	/validate-ship    <revision> -- <evidence>
//	/validate-implement <revision> [-- <guidance>]
//	/validate-factory-bug <revision> <finding id or failure-N>
//
// The command is the first line's tokens. Text after them, on the same line
// or below, is kept as the owner's guidance instead of being refused.
func ParseValidateCommand(body string) (ValidateCommand, error) {
	body = strings.TrimSpace(commandtext.Normalize(body))
	var c ValidateCommand
	if body == "" || len(body) > validateBodyLimit || strings.ContainsRune(body, 0) {
		return c, errors.New("a Validate command is at most 64 KiB")
	}
	// The command is the first line's tokens. Any text after them, on that line
	// or below it, is the owner's guidance and is never a reason to refuse.
	line, below, _ := strings.Cut(body, "\n")
	line = strings.TrimSpace(line)
	if len(line) > validateCommandLimit {
		return c, errors.New("a Validate command line is at most 2048 characters")
	}
	head, evidence, hasEvidence := strings.Cut(line+" ", " -- ")
	fields := strings.Fields(head)
	if len(fields) < 1 {
		return c, errors.New("a Validate command needs an action")
	}
	c.Name = strings.ToLower(fields[0])
	if !IsValidateControlCommand(c.Name) {
		return c, fmt.Errorf("%q is not a Validate command", fields[0])
	}
	args := fields[1:]
	switch {
	case len(args) > 0 && validateRevisionPattern.MatchString(args[0]):
		c.Revision = args[0]
		args = args[1:]
	case len(args) > 0 && strings.HasPrefix(args[0], "d-"):
		c.Reference = args[0]
		args = args[1:]
	case len(args) > 0 && validateRevisionLike.MatchString(args[0]):
		return c, fmt.Errorf("%q is not a report revision: use the 12-character revision the report shows", args[0])
	}
	// extra keeps the first n arguments as the command and the rest as guidance.
	var trailing string
	extra := func(n int) {
		if len(args) > n {
			trailing = strings.Join(args[n:], " ")
			args = args[:n]
		}
	}
	evidence = strings.TrimSpace(evidence)
	guidance := func(parts ...string) string {
		var kept []string
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				kept = append(kept, p)
			}
		}
		return strings.Join(kept, "\n\n")
	}
	switch c.Name {
	case ValidateRepairCommand:
		extra(3)
		if len(args) != 3 || !validateHeadPattern.MatchString(args[0]) || len(args[1]) != 64 || !validateHeadPattern.MatchString(args[1]) || !slices.Contains([]string{"inspect", "accept", "discard"}, args[2]) {
			return c, errors.New("use /validate-repair <revision> <candidate id> <review digest> inspect|accept|discard")
		}
		c.Candidate, c.ReviewDigest, c.RepairAction = args[0], args[1], args[2]
	case ValidateCheckCommand:
		extra(2)
		if !hasEvidence {
			return c, errors.New("/validate-check and /validate-ship require ` -- evidence`")
		}
		if len(args) != 2 || !validateFindingPattern.MatchString(args[0]) || args[1] != store.VerdictPass && args[1] != store.VerdictFail {
			return c, errors.New("use /validate-check <revision> <criterion or review id> pass|fail -- <evidence>")
		}
		c.Criterion, c.Verdict, c.Evidence = args[0], args[1], guidance(trailing, evidence, below)
		trailing, below = "", ""
		evidence = ""
		if c.Evidence == "" {
			return c, errors.New("a check needs evidence after ` -- `: text or a link")
		}
	case ValidateFindingCommand:
		if len(args) < 2 || !validateFindingPattern.MatchString(args[0]) {
			return c, errors.New("use /validate-finding <revision> <finding id> repair|issue bug|story|defer, or accept -- <reason>")
		}
		c.Finding, c.Choice = args[0], args[1]
		switch {
		case c.Choice == validateChoiceAccept:
			extra(2)
			if c.Evidence = guidance(trailing, evidence, below); c.Evidence == "" {
				return c, errors.New("accept needs a reason after ` -- `: why the deviation is deliberate")
			}
			trailing, below = "", ""
			evidence = ""
		case c.Choice == store.ChoiceRepair || c.Choice == store.ChoiceDefer:
			extra(2)
		case c.Choice == store.ChoiceIssue && len(args) >= 3 && (args[2] == "bug" || args[2] == "story"):
			extra(3)
			c.IssueKind = args[2]
		default:
			return c, errors.New("use /validate-finding <revision> <finding id> repair|issue bug|story|defer, or accept -- <reason>")
		}
		if c.Choice != validateChoiceAccept {
			c.Guidance = guidance(trailing, evidence, below)
		}
		return c, nil
	case ValidatePushCommand:
		extra(0)
	case ValidateImplementCommand, ValidateReturnCommand:
		extra(0)
		if c.Guidance = guidance(evidence, trailing, below); c.Name == ValidateReturnCommand && c.Guidance == "" {
			return c, errors.New("/return needs guidance text after the command")
		}
		return c, nil
	case ValidateMergeCommand:
		extra(2)
		if len(args) != 2 || !validateHeadPattern.MatchString(args[1]) {
			return c, errors.New("use /validate-merge <revision> <pr number> <head sha>")
		}
		n, err := strconv.Atoi(args[0])
		if err != nil || n < 1 {
			return c, errors.New("the pull request number is a positive number")
		}
		c.PR, c.Head = n, args[1]
	case ValidateShipCommand:
		extra(0)
		c.Evidence = guidance(evidence, trailing, below)
		trailing, below, evidence = "", "", ""
		if c.Evidence == "" {
			return c, errors.New("/validate-ship needs a reason after ` -- `")
		}
	case ValidateFactoryBugCommand:
		extra(1)
		if len(args) != 1 || !validateFindingPattern.MatchString(args[0]) && !validateFailureRefPattern.MatchString(args[0]) {
			return c, errors.New("use /validate-factory-bug <revision> <finding id or failure-N>")
		}
		c.Item = args[0]
	case ValidateCloseCommand:
		seen := map[int]bool{}
		for i, a := range args {
			n, err := strconv.Atoi(a)
			if err != nil {
				extra(i)
				break
			}
			if n < 1 || seen[n] {
				return c, errors.New("the issues are distinct positive numbers")
			}
			seen[n] = true
			c.Issues = append(c.Issues, n)
		}
		if len(c.Issues) < 1 || len(c.Issues) > 64 {
			return c, errors.New("use /validate-close <revision> followed by one or more issue numbers")
		}
	}
	c.Guidance = guidance(evidence, trailing, below)
	return c, nil
}

// NextCommands lists the exact commands the owner can give now. closeSet is the
// issue numbers a close command must list.
func (s ValidateState) NextCommands(closeSet []int) []string {
	var out []string
	rev := s.Revision
	for _, f := range s.Findings {
		if f.State == findingAwaitingCheck || f.State == findingOwnerFailed && f.checkable() {
			out = append(out, fmt.Sprintf("`%s %s %s pass -- <what you checked>` or `%s %s %s fail -- <what you saw>`", ValidateCheckCommand, rev, f.ID, ValidateCheckCommand, rev, f.ID))
		}
		if f.needsChoice() {
			line := fmt.Sprintf("`%s %s %s repair`, `%s %s %s issue bug`, `%s %s %s issue story`", ValidateFindingCommand, rev, f.ID, ValidateFindingCommand, rev, f.ID, ValidateFindingCommand, rev, f.ID)
			if f.Optional {
				line += fmt.Sprintf(", `%s %s %s defer`", ValidateFindingCommand, rev, f.ID)
			}
			if f.acceptable() {
				line += fmt.Sprintf(", or `%s %s %s accept -- <reason>` to accept a deliberate deviation", ValidateFindingCommand, rev, f.ID)
			}
			out = append(out, line)
		}
	}
	if c := s.deliveryCommand(closeSet); c != "" {
		out = append(out, c)
	}
	return out
}

// deliveryCommand is the one delivery command the owner can give now: push,
// then merge, then close. It is empty while the run is not ready or delivery
// is complete.
func (s ValidateState) deliveryCommand(closeSet []int) string {
	rev := s.Revision
	switch {
	case !s.Ready:
	case !effectDone(s.Effects.PR) || !effectDone(s.Effects.Push):
		return fmt.Sprintf("`%s %s` pushes the tested commit and opens or recovers its pull request; it does not authorize merge or close.", ValidatePushCommand, rev)
	case !effectDone(s.Effects.Merge):
		r := decodeEffectResult(s.Effects.PR)
		return fmt.Sprintf("`%s %s %d %s` merges only this displayed pull request and exact head after push; it does not authorize closing issues.", ValidateMergeCommand, rev, r.Number, r.Head)
	case !effectDone(s.Effects.Close):
		return fmt.Sprintf("`%s %s %s` closes exactly the displayed accepted issue set after merge; already closed issues are not repeated.", ValidateCloseCommand, rev, validateNumbers(closeSet))
	}
	return ""
}

// shipBlockers lists admission prerequisites that owner shipping cannot waive.
// All assessed factual blockers remain visible and may be covered by one exact
// owner exception; delivery operations must still succeed in fact.
func (s ValidateState) shipBlockers() []string {
	if !s.Assessed {
		return []string{"The assessment of this commit is not recorded yet."}
	}
	return nil
}

// shipLine is the /validate-ship command for the current revision.
func (s ValidateState) shipLine() string {
	return fmt.Sprintf("`%s %s -- <reason>`", ValidateShipCommand, s.Revision)
}

// validateShipConsequence describes the existing combined action; it adds no
// confirmation and does not change the separate delivery commands.
const validateShipConsequence = "This one explicit reasoned command authorizes the existing ordered sequence: push the displayed commit, open its pull request, merge its exact head, then close the displayed accepted issues. It stops if a step fails or remains uncertain. Its waiver keeps factual test and review results unchanged; no additional confirmation is required for the remaining successful steps of this same shipping decision."

// mergeConflicted reports a merge GitHub refused because the pull request
// conflicts with its target (405 "Pull Request has merge conflicts"). Repeating
// the ship cannot clear that.
func (s ValidateState) mergeConflicted() bool {
	e := s.Effects.Merge
	if e == nil || e.State != store.EffectFailed {
		return false
	}
	reason := strings.ToLower(e.Error)
	return strings.Contains(reason, "405") && strings.Contains(reason, "merge conflict")
}

// ValidateSyncCommand is the exact parent comment that sends the work to
// Implement, whose coordinator merges the target branch into the feature branch
// and hands a conflict to its sync-resolver.
const ValidateSyncCommand = "/implement Bring the target branch into the feature branch and resolve the merge conflict."

// mergeConflictStep is the way out of a conflicted merge: the factory's own
// Implement sync-resolver path, then Validate on the new head, or
// /validate-implement when this run would accept it.
func (s ValidateState) mergeConflictStep() string {
	step := fmt.Sprintf("Pull request #%d has merge conflicts with its target branch, so repeating the ship cannot merge it. Comment `%s`: Implement merges the target branch into the feature branch and its sync-resolver resolves the conflict. When Implement reports finished, comment `%s` to validate the new head, unless that reply says Validate starts automatically.", decodeEffectResult(s.Effects.PR).Number, ValidateSyncCommand, ValidateStartCommand)
	if s.implementLine() != "" {
		step += fmt.Sprintf(" Or comment `%s %s` to send the work back to Implement on the same feature branch; Validate runs again on the new head.", ValidateImplementCommand, s.Revision)
	}
	return step
}

// NextStep is the one next step the parent report and the owner wait name: the
// retry when a reviewer has no usable result, /validate-ship when nothing
// blocks and delivery is unfinished, the sub-issues while the owner has other
// checks or choices open, otherwise the next delivery command. Per-item command
// templates stay on the story sub-issues.
func (s ValidateState) NextStep(closeSet []int) string {
	if s.Reimplementing {
		return "Nothing here: the reviewed work was sent back to Implement on the same feature branch. Validate runs again on the new head when Implement finishes."
	}
	if s.mergeConflicted() {
		return s.mergeConflictStep()
	}
	if len(s.shipBlockers()) == 0 && !effectDone(s.Effects.Close) {
		return s.shipLine() + " " + validateShipConsequence
	}
	roles, _ := s.notReviewed()
	owner := false
	for _, f := range s.Findings {
		if validateOwnerItem(s, f) {
			owner = true
		}
	}
	switch {
	case s.Suite.Outcome != store.SuitePassed && s.implementLine() != "":
		return "The suite did not pass. " + s.implementLine()
	case s.Suite.Outcome != store.SuitePassed:
		return "The suite did not pass. " + validateNewSnapshotStep
	case len(roles) > 0:
		return validateRetryStep(s.Commit)
	case !s.Assessed:
		return "Wait for the assessment of this commit; the report updates by itself."
	case owner && s.implementLine() != "":
		return "Work through the sub-issues above: each lists its open checks and choices with the command to give here. " + s.implementLine()
	case owner:
		return "Work through the sub-issues above: each lists its open checks and choices with the command to give here."
	case !s.Ready && len(s.blockingIDs()) > 0 && s.implementLine() != "":
		return "A failed criterion or blocking finding remains. " + s.implementLine()
	case !s.Ready && len(s.blockingIDs()) > 0:
		return "A failed criterion or blocking finding remains. " + validateNewSnapshotStep
	case !s.Ready:
		return "Wait: an issue you chose is still being created; the report updates by itself."
	}
	if c := s.deliveryCommand(closeSet); c != "" {
		return c
	}
	return "Nothing. Delivery is complete."
}

// blockingIDs lists the items that clear only on a new tested snapshot.
func (s ValidateState) blockingIDs() []string {
	var out []string
	for _, f := range s.Findings {
		if f.State != findingNotReviewed && validateItemBlocks(f) {
			out = append(out, f.ID)
		}
	}
	return out
}

// validateNewSnapshotStep names the only factory step that clears a failed
// suite or a blocking finding: a fix on the feature branch, then Validate on
// the new head.
const validateNewSnapshotStep = "It clears only on a new tested snapshot: push a fix to the feature branch, then comment `" + ValidateStartCommand + "` to validate the new head."

// implementLine offers /validate-implement when the graph declares the step and
// a story has a blocking finding on the tested commit, or the commit was handed
// back with suite failures. It is empty otherwise.
func (s ValidateState) implementLine() string {
	if !s.Reimplement || s.Reimplementing || !s.Assessed || len(s.implementTargets()) == 0 {
		return ""
	}
	return fmt.Sprintf("To send the blocking findings or the failed suite back to Implement on the same feature branch, comment `%s %s`; Validate runs again on the new head. Or push a fix to the feature branch yourself and comment `%s`.", ValidateImplementCommand, s.Revision, ValidateStartCommand)
}
