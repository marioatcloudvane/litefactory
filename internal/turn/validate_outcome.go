package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// Handback triggers: why Validate hands the work back to Implement.
const (
	HandbackBoundaryExceeded = "boundary_exceeded"
	HandbackRepairHandback   = "repair_handback"
	HandbackStillFailing     = "still_failing"
)

// ValidateJoinedStory is the one assessment of a story that joins the three
// reviewers' parts. A part with no usable result stays unresolved: its findings
// are unverified, never satisfied, and the role is listed under Unresolved.
type ValidateJoinedStory struct {
	Story      string                      `json:"story"`
	SnapshotID string                      `json:"snapshot_id"`
	Criteria   []agent.ContributionFinding `json:"criteria"`
	Reviews    []agent.ContributionFinding `json:"reviews"`
	Unresolved []string                    `json:"unresolved,omitempty"`
}

// ValidateHandback tells Implement exactly what must change and where. Every
// field is taken from a retained record; nothing is invented.
type ValidateHandback struct {
	Commit           string                `json:"commit"`
	Trigger          string                `json:"trigger"`
	SuiteRecord      string                `json:"suite_record"`
	RepairRecords    []string              `json:"repair_records,omitempty"`
	Failures         []HandbackFailure     `json:"failures"`
	OwnerInstruction string                `json:"owner_instruction"`
	Findings         []ValidateJoinedStory `json:"reviewer_findings"`
}

// HandbackFailure is one failing suite command with its reproduction, the output
// that showed it, where the cause lies and the one objective that fixes it.
type HandbackFailure struct {
	Command      string                       `json:"command"`
	Argv         []string                     `json:"argv"`
	Exit         *int                         `json:"exit,omitempty"`
	Reason       string                       `json:"reason,omitempty"`
	OutputTail   string                       `json:"output_tail,omitempty"`
	Expected     string                       `json:"expected,omitempty"`
	Cause        string                       `json:"cause,omitempty"`
	Locations    []agentresult.RepairLocation `json:"locations,omitempty"`
	Objective    string                       `json:"objective"`
	Diagnostic   bool                         `json:"diagnostic,omitempty"`
	SuiteRecord  string                       `json:"suite_record"`
	RepairRecord string                       `json:"repair_record,omitempty"`
}

// ValidateOutcomeActivity joins the reviewers' parts into one assessment per
// story, builds the handback when the end needs one, and records the outcome. It
// runs no agent and has no remote effect. It returns the end:
//
//	assessed     the suite passed on the final commit
//	handed_back  the suite still fails: Implement must change something
//	blocked      the suite could not give a trustworthy answer, or a repair
//	             could not be carried out
//
// It never says accepted or approved: an assessment is a finding with evidence.
type ValidateOutcomeActivity struct{ b ValidateFlowBindings }

// Execute records one outcome per run and final commit.
func (h ValidateOutcomeActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	step, err := loadValidateStep(ctx, opts, req, h.b.Profile, graphexec.ValidateOutcomeAction)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	suite, err := latestValidateSuite(ctx, opts, req.InstanceID, step.node.Attrs["lf_suite"])
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	commit := suite.Output.Commit
	// A recorded outcome never changes: running again returns it.
	if prior, err := opts.DB.GetValidateOutcome(ctx, req.InstanceID, commit); err == nil {
		return validateOutcomeResult(prior)
	} else if !errors.Is(err, store.ErrNotFound) {
		return ActivityResult{}, err
	}
	rounds, err := opts.DB.ListRepairRounds(ctx, req.InstanceID)
	if err != nil {
		return ActivityResult{}, err
	}
	reviews, err := opts.DB.ListValidateReviews(ctx, req.InstanceID, commit)
	if err != nil {
		return ActivityResult{}, err
	}
	var stories []ValidateStory
	if h.b.Stories != nil {
		if stories, err = h.b.Stories(ctx, opts, req); err != nil {
			return ActivityResult{Diagnostic: err.Error()}, fmt.Errorf("read the stories under review: %w", err)
		}
	}
	joined := JoinValidateAssessments(stories, validateSnapshotID(commit), reviews)
	end, reason, trigger := decideValidateEnd(suite.Output, rounds)
	out := store.ValidateOutcome{InstanceID: req.InstanceID, Commit: commit, Outcome: end, Reason: clipText(reason, 500), CreatedAt: h.b.now(opts)}
	if out.Assessment, err = json.Marshal(joined); err != nil {
		return ActivityResult{}, err
	}
	if end == store.ValidateHandedBack {
		handback, err := buildValidateHandback(ctx, opts, suite, rounds, trigger, joined)
		if err != nil {
			return ActivityResult{Diagnostic: err.Error()}, err
		}
		if out.Handback, err = json.Marshal(handback); err != nil {
			return ActivityResult{}, err
		}
	}
	recorded, err := opts.DB.PutValidateOutcome(ctx, out)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	return validateOutcomeResult(recorded)
}

func validateOutcomeResult(o store.ValidateOutcome) (ActivityResult, error) {
	return validateJSONResult(o.Outcome, map[string]any{"commit": o.Commit, "outcome": o.Outcome, "reason": o.Reason, "outcome_id": o.ID})
}

// decideValidateEnd derives the end from the retained facts alone.
func decideValidateEnd(suite validateSuiteOutput, rounds []store.RepairRound) (end, reason, trigger string) {
	switch suite.Outcome {
	case store.SuitePassed:
		return store.ValidateAssessed, "", ""
	case store.SuiteBlocked:
		return store.ValidateBlocked, firstNonBlank(suite.Reason, "the suite could not give a trustworthy answer"), ""
	}
	if len(rounds) > 0 {
		last := rounds[len(rounds)-1]
		switch last.State {
		case store.RepairBlocked:
			return store.ValidateBlocked, firstNonBlank(last.Reason, "the repair could not be carried out"), ""
		case store.RepairTooBig:
			return store.ValidateHandedBack, last.Reason, HandbackBoundaryExceeded
		case store.RepairHandback:
			return store.ValidateHandedBack, last.Reason, HandbackRepairHandback
		}
	}
	return store.ValidateHandedBack, firstNonBlank(suite.Reason, "the suite still fails after the repair rounds"), HandbackStillFailing
}

func firstNonBlank(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// JoinValidateAssessments joins the reviewers' recorded parts into one
// assessment per story. The acceptance part supplies every criterion and the
// plan review, the code part the code review and the architecture part the
// architecture review. A part with no usable result stays unresolved.
func JoinValidateAssessments(stories []ValidateStory, snapshot string, reviews []store.ValidateReview) []ValidateJoinedStory {
	type part struct {
		state    string
		reason   string
		perStory map[string]agentresult.AssessmentStory
	}
	parts := map[string]*part{}
	for _, r := range reviews {
		p := &part{state: r.State, reason: r.Reason, perStory: map[string]agentresult.AssessmentStory{}}
		if r.State == store.ReviewAccepted {
			var found []agentresult.AssessmentStory
			if err := json.Unmarshal(r.Findings, &found); err != nil {
				p.state, p.reason = store.ReviewMalformed, "the stored findings are unreadable"
			}
			for _, s := range found {
				p.perStory[s.Story] = s
			}
		}
		parts[r.Role] = p
	}
	unresolved := func(role, name, class string) agent.ContributionFinding {
		why := "no usable result was recorded"
		if p, ok := parts[role]; ok {
			why = firstNonBlank(p.reason, "no usable result was recorded")
		}
		return agent.ContributionFinding{Name: name, Status: "unverified", Classification: class, Evidence: []agent.EvidenceExplanation{{Summary: "Unresolved: the " + role + " reviewer's part has no usable result (" + clipText(why, 200) + ").", CheckIDs: []string{}}}}
	}
	pick := func(role, story, name string, criteria bool, class string) agent.ContributionFinding {
		if p, ok := parts[role]; ok && p.state == store.ReviewAccepted {
			if s, ok := p.perStory[story]; ok {
				group := s.Reviews
				if criteria {
					group = s.Criteria
				}
				for _, f := range group {
					if f.Name == name {
						return f
					}
				}
			}
		}
		return unresolved(role, name, class)
	}
	out := make([]ValidateJoinedStory, 0, len(stories))
	for _, s := range stories {
		j := ValidateJoinedStory{Story: s.Key, SnapshotID: snapshot, Criteria: []agent.ContributionFinding{}, Reviews: []agent.ContributionFinding{}}
		for _, c := range s.Criteria {
			j.Criteria = append(j.Criteria, pick(agent.ReviewRoleAcceptance, s.Key, c, true, "blocking"))
		}
		for _, r := range []struct{ role, name string }{{agent.ReviewRoleAcceptance, "plan"}, {agent.ReviewRoleCode, "code"}, {agent.ReviewRoleArchitecture, "architecture"}} {
			j.Reviews = append(j.Reviews, pick(r.role, s.Key, r.name, false, "advisory"))
		}
		for _, role := range ValidateReviewRoles {
			if p, ok := parts[role]; !ok || p.state != store.ReviewAccepted {
				j.Unresolved = append(j.Unresolved, role)
			} else if _, ok := p.perStory[s.Key]; !ok {
				j.Unresolved = append(j.Unresolved, role)
			}
		}
		out = append(out, j)
	}
	return out
}

// buildValidateHandback builds the handback from retained records only: the
// failing suite commands of the final commit, the repair agent's diagnosis and
// the repair rounds. Each failing command gets one objective; when the cause is
// unknown the objective is a concrete diagnostic step, never a guess.
func buildValidateHandback(ctx context.Context, opts Options, suite validateSuiteView, rounds []store.RepairRound, trigger string, findings []ValidateJoinedStory) (ValidateHandback, error) {
	h := ValidateHandback{Commit: suite.Output.Commit, Trigger: trigger, Findings: findings, Failures: []HandbackFailure{}}
	if suite.Output.RunID != 0 {
		h.SuiteRecord = fmt.Sprintf("suite run %d", suite.Output.RunID)
	}
	var diagnoses []agentresult.RepairFailure
	repairLink := ""
	for _, r := range rounds {
		link := fmt.Sprintf("repair round %d (record %d)", r.Round, r.ID)
		h.RepairRecords = append(h.RepairRecords, link)
		if r.Diagnosis == "" {
			continue
		}
		var d repairDiagnosis
		if err := json.Unmarshal([]byte(r.Diagnosis), &d); err != nil {
			continue
		}
		// The newest diagnosis wins: it saw the code the earlier rounds left.
		if len(d.Failures) > 0 {
			diagnoses, repairLink = d.Failures, link
		}
	}
	var commands []store.SuiteCommand
	if suite.Output.RunID != 0 {
		var err error
		if commands, err = opts.DB.SuiteCommands(ctx, suite.Output.RunID); err != nil {
			return h, err
		}
	}
	used := map[int]bool{}
	for _, c := range commands {
		if c.State == store.SuitePassed {
			continue
		}
		f := HandbackFailure{Command: strings.Join(c.Argv, " "), Argv: slices.Clone(c.Argv), Exit: c.Exit, Reason: c.Reason, OutputTail: c.OutputTail, SuiteRecord: fmt.Sprintf("suite run %d, command %d", c.RunID, c.Position)}
		for i, d := range diagnoses {
			if !used[i] && (strings.TrimSpace(d.Command) == f.Command || len(diagnoses) == 1 && countFailing(commands) == 1) {
				used[i] = true
				f.Expected, f.Cause, f.Locations, f.RepairRecord = d.Expected, d.Cause, slices.Clone(d.Locations), repairLink
				f.Objective = strings.TrimSpace(d.Objective)
				break
			}
		}
		if f.Objective == "" || f.Cause == "" || strings.EqualFold(strings.TrimSpace(f.Cause), agentresult.RepairUnknownCause) {
			f.Diagnostic = true
		}
		if f.Objective == "" {
			f.Objective = "Run `" + f.Command + "`, read its output, and find the cause of the failure. The cause is not known yet, so do not guess; report what you find before you change code."
		}
		h.Failures = append(h.Failures, f)
	}
	if len(h.Failures) == 0 {
		// The suite failed without a failing command on record (for example a
		// command that could not complete). Say so, and ask for the diagnosis.
		h.Failures = append(h.Failures, HandbackFailure{Command: "full suite", Reason: firstNonBlank(suite.Output.Reason, "the suite failed"), Diagnostic: true, SuiteRecord: h.SuiteRecord,
			Objective: "Run the full suite on this commit, find which command fails and why. No failing command was recorded, so the cause is not known yet."})
	}
	h.OwnerInstruction = ownerHandbackInstruction(h)
	return h, nil
}

func countFailing(commands []store.SuiteCommand) int {
	n := 0
	for _, c := range commands {
		if c.State != store.SuitePassed {
			n++
		}
	}
	return n
}

// handbackInstructionTail is how many characters of a failure's retained
// output the owner instruction shows.
const handbackInstructionTail = 300

// tailRunes keeps at most limit characters from the end of text.
func tailRunes(text string, limit int) string {
	r := []rune(text)
	if len(r) <= limit {
		return text
	}
	return string(r[len(r)-limit:])
}

// ownerHandbackInstruction is the text an owner can copy to start the repair
// run. It names the commit, the failures and one objective for each, and the
// factory path that sends them back to Implement.
func ownerHandbackInstruction(h ValidateHandback) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Fix the failing tests found on commit %s on the feature branch.", h.Commit)
	for i, f := range h.Failures {
		fmt.Fprintf(&b, "\n%d. `%s`: %s", i+1, f.Command, strings.TrimSpace(f.Objective))
		if len(f.Locations) > 0 {
			var where []string
			for _, l := range f.Locations {
				if l.Line > 0 {
					where = append(where, fmt.Sprintf("%s:%d", l.Path, l.Line))
				} else {
					where = append(where, l.Path)
				}
			}
			fmt.Fprintf(&b, " (look at %s)", strings.Join(where, ", "))
		}
		if cause := strings.TrimSpace(f.Cause); cause != "" && !strings.EqualFold(cause, agentresult.RepairUnknownCause) {
			b.WriteString(" Cause: " + strings.Join(strings.Fields(cause), " "))
		}
		if tail := strings.Join(strings.Fields(tailRunes(f.OutputTail, handbackInstructionTail)), " "); tail != "" {
			b.WriteString(" Output tail: " + tail)
		}
	}
	b.WriteString("\nThen comment `" + ValidateStartCommand + "` to run Validate again.")
	b.WriteString(" Or comment `" + ValidateImplementCommand + " <revision>` with the revision of this report to send the stories with blocking findings back to Implement on the same feature branch; Validate runs again on the new head.")
	return b.String()
}
