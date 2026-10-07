package turn

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/store"
)

// The Validate owner commands. They are exact: one command, the report revision
// it was written for, and fixed tokens. They never collide with the /validate
// entry point or the legacy /deliver, /merge, /followup and /waive commands.
const (
	ValidateCheckCommand   = "/validate-check"
	ValidateFindingCommand = "/validate-finding"
	ValidatePushCommand    = "/validate-push"
	ValidateMergeCommand   = "/validate-merge"
	ValidateCloseCommand   = "/validate-close"
)

// ValidateControlCommands lists the Validate owner commands.
var ValidateControlCommands = []string{ValidateCheckCommand, ValidateFindingCommand, ValidatePushCommand, ValidateMergeCommand, ValidateCloseCommand}

// IsValidateControlCommand reports whether a command token is one of them.
func IsValidateControlCommand(name string) bool {
	for _, c := range ValidateControlCommands {
		if name == c {
			return true
		}
	}
	return false
}

// validateCommandLimit bounds a whole command.
const validateCommandLimit = 2048

// ValidateCommand is one parsed owner command.
type ValidateCommand struct {
	Name     string
	Revision string
	// Check
	Criterion string
	Verdict   string
	Evidence  string
	// Finding
	Finding   string
	Choice    string
	IssueKind string
	// Merge
	PR   int
	Head string
	// Close
	Issues []int
}

var (
	validateRevisionPattern = regexp.MustCompile(`^[0-9a-f]{12}$`)
	validateFindingPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}:[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
	validateHeadPattern     = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)
)

// ParseValidateCommand parses the exact grammar:
//
//	/validate-check   <revision> <criterion id> pass|fail -- <evidence>
//	/validate-finding <revision> <finding id> repair|defer
//	/validate-finding <revision> <finding id> issue bug|story
//	/validate-push    <revision>
//	/validate-merge   <revision> <pr number> <head sha>
//	/validate-close   <revision> <issue number>...
//
// The whole comment is the command. Anything else is refused and changes
// nothing.
func ParseValidateCommand(body string) (ValidateCommand, error) {
	body = strings.TrimSpace(body)
	var c ValidateCommand
	if body == "" || len(body) > validateCommandLimit || strings.ContainsAny(body, "\r\n\x00") {
		return c, errors.New("a Validate command is one line of at most 2048 characters")
	}
	head, evidence, hasEvidence := strings.Cut(body, " -- ")
	fields := strings.Fields(head)
	if len(fields) < 2 {
		return c, errors.New("a Validate command names the report revision after the command")
	}
	c.Name = strings.ToLower(fields[0])
	if !IsValidateControlCommand(c.Name) {
		return c, fmt.Errorf("%q is not a Validate command", fields[0])
	}
	if !validateRevisionPattern.MatchString(fields[1]) {
		return c, errors.New("the report revision is 12 lowercase hex characters")
	}
	c.Revision = fields[1]
	args := fields[2:]
	if hasEvidence != (c.Name == ValidateCheckCommand) {
		return c, errors.New("only /validate-check takes ` -- evidence`, and it requires it")
	}
	switch c.Name {
	case ValidateCheckCommand:
		if len(args) != 2 || !validateFindingPattern.MatchString(args[0]) || args[1] != store.VerdictPass && args[1] != store.VerdictFail {
			return c, errors.New("use /validate-check <revision> <criterion id> pass|fail -- <evidence>")
		}
		c.Criterion, c.Verdict, c.Evidence = args[0], args[1], strings.TrimSpace(evidence)
		if c.Evidence == "" {
			return c, errors.New("a check needs evidence after ` -- `: text or a link")
		}
	case ValidateFindingCommand:
		if len(args) < 2 || !validateFindingPattern.MatchString(args[0]) {
			return c, errors.New("use /validate-finding <revision> <finding id> repair|issue bug|story|defer")
		}
		c.Finding, c.Choice = args[0], args[1]
		switch {
		case (c.Choice == store.ChoiceRepair || c.Choice == store.ChoiceDefer) && len(args) == 2:
		case c.Choice == store.ChoiceIssue && len(args) == 3 && (args[2] == "bug" || args[2] == "story"):
			c.IssueKind = args[2]
		default:
			return c, errors.New("use /validate-finding <revision> <finding id> repair|issue bug|story|defer")
		}
	case ValidatePushCommand:
		if len(args) != 0 {
			return c, errors.New("/validate-push takes no argument after the revision")
		}
	case ValidateMergeCommand:
		if len(args) != 2 || !validateHeadPattern.MatchString(args[1]) {
			return c, errors.New("use /validate-merge <revision> <pr number> <head sha>")
		}
		n, err := strconv.Atoi(args[0])
		if err != nil || n < 1 {
			return c, errors.New("the pull request number is a positive number")
		}
		c.PR, c.Head = n, args[1]
	case ValidateCloseCommand:
		if len(args) < 1 || len(args) > 64 {
			return c, errors.New("use /validate-close <revision> <issue number>...")
		}
		seen := map[int]bool{}
		for _, a := range args {
			n, err := strconv.Atoi(a)
			if err != nil || n < 1 || seen[n] {
				return c, errors.New("the issues are distinct positive numbers")
			}
			seen[n] = true
			c.Issues = append(c.Issues, n)
		}
	}
	return c, nil
}

// NextCommands lists the exact commands the owner can give now. closeSet is the
// issue numbers a close command must list.
func (s ValidateState) NextCommands(closeSet []int) []string {
	var out []string
	rev := s.Revision
	for _, f := range s.Findings {
		switch {
		case f.State == findingAwaitingCheck:
			out = append(out, fmt.Sprintf("`%s %s %s pass -- <what you checked>` or `%s %s %s fail -- <what you saw>`", ValidateCheckCommand, rev, f.ID, ValidateCheckCommand, rev, f.ID))
		case f.needsChoice():
			line := fmt.Sprintf("`%s %s %s repair`, `%s %s %s issue bug`, `%s %s %s issue story`", ValidateFindingCommand, rev, f.ID, ValidateFindingCommand, rev, f.ID, ValidateFindingCommand, rev, f.ID)
			if f.Optional {
				line += fmt.Sprintf(", `%s %s %s defer`", ValidateFindingCommand, rev, f.ID)
			}
			out = append(out, line)
		}
	}
	switch {
	case !s.Ready:
	case !effectDone(s.Effects.PR) || !effectDone(s.Effects.Push):
		out = append(out, fmt.Sprintf("`%s %s`", ValidatePushCommand, rev))
	case !effectDone(s.Effects.Merge):
		r := decodeEffectResult(s.Effects.PR)
		out = append(out, fmt.Sprintf("`%s %s %d %s`", ValidateMergeCommand, rev, r.Number, r.Head))
	case !effectDone(s.Effects.Close):
		out = append(out, fmt.Sprintf("`%s %s %s`", ValidateCloseCommand, rev, validateNumbers(closeSet)))
	}
	return out
}
