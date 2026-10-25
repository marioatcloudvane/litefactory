package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// Finding states as the owner sees them. They are computed from records only.
const (
	findingSatisfied     = "satisfied"
	findingOwnerPassed   = "owner passed"
	findingAwaitingCheck = "awaiting owner check"
	findingOwnerFailed   = "owner failed"
	findingOpen          = "open"
	// findingNotReviewed marks an item whose owning reviewer has no usable
	// result. It is not an owner task: only a new review can settle it.
	findingNotReviewed = "not reviewed"
)

// ValidateFinding is one criterion or review of the joined assessment, with the
// owner's records applied. Its ID is stable: the story key, a colon, and the
// criterion id or the review name.
type ValidateFinding struct {
	ID        string
	Story     string
	Name      string
	Criterion bool
	// Role is the reviewer that owns the item.
	Role    string
	Status  string
	Class   string
	Summary string
	Checks  []string
	State   string
	// Blocking findings require a new tested snapshot or an eligible owner
	// verdict. A repair or an issue choice never clears one.
	Blocking bool
	// Optional findings are the advisory ones; only these may be deferred.
	Optional bool
	Choice   *store.ValidateChoice
	Verdict  *store.ValidateVerdict
}

// checkable preserves reviewer facts while allowing evidenced owner verdicts.
func (f ValidateFinding) checkable() bool {
	return f.State != findingNotReviewed && f.Status == "unverified" && (f.Criterion || f.Blocking)
}

// acceptable says the owner may accept the finding as a deliberate deviation:
// an open blocking review (architecture, plan or code), never a criterion.
func (f ValidateFinding) acceptable() bool {
	return !f.Criterion && f.Blocking && f.needsChoice()
}

// acceptedDeviation says the owner accepted this blocking review finding as a
// deliberate deviation: an owner pass on a review that awaited no check.
func (f ValidateFinding) acceptedDeviation() bool {
	return !f.Criterion && f.State == findingOwnerPassed && !f.checkable() && f.Verdict != nil
}

// needsChoice says the finding takes exactly one owner choice.
func (f ValidateFinding) needsChoice() bool {
	switch f.State {
	case findingOpen, findingOwnerFailed:
		return true
	}
	return false
}

// ValidateDeliveryRows holds the retained remote effects of a run by kind.
type ValidateDeliveryRows struct {
	Push, PR, Merge, Close *store.ValidateEffect
	Issues                 map[string]store.ValidateEffect
}

// ValidateState is everything the report, the commands and readiness read. It
// is built from records only and is never cached.
type ValidateState struct {
	InstanceID     int64
	Commit         string
	DecisionTask   string
	DeliveryCommit string
	Waiver         *store.ValidateShipWaiver
	Suite          validateSuiteOutput
	Assessed       bool
	Outcome        string
	Reason         string
	Handback       *ValidateHandback
	Rounds         []store.RepairRound
	Reviews        []store.ValidateReview
	Stories        []ValidateJoinedStory
	Findings       []ValidateFinding
	Effects        ValidateDeliveryRows
	Revision       string
	// Open preserves factual blockers even when an exact owner waiver permits shipping.
	Open []string
	// Ready is true when factual gates pass or a current exact owner waiver covers them.
	Ready bool
	// Reimplement says the graph declares the step that hands the blocking
	// findings back to Implement.
	Reimplement bool
	// Reimplementing says this run already handed the findings of its tested
	// commit back to Implement.
	Reimplementing bool
}

// finding looks a finding up by its stable id.
func (s ValidateState) finding(id string) (ValidateFinding, bool) {
	for _, f := range s.Findings {
		if f.ID == id {
			return f, true
		}
	}
	return ValidateFinding{}, false
}

// validateSuiteNodeName finds the node that runs the suite in a profile.
func validateSuiteNodeName(profile *graphexec.Profile) string {
	for _, n := range profile.Nodes {
		if n.Action == graphexec.ValidateSuiteAction {
			return n.Name
		}
	}
	return ""
}

// LoadValidateState reads the records of one Validate run on its tested commit
// and computes readiness. It changes nothing.
func LoadValidateState(ctx context.Context, opts Options, profile *graphexec.Profile, instanceID int64) (ValidateState, error) {
	st := ValidateState{InstanceID: instanceID}
	suite, err := latestValidateSuite(ctx, opts, instanceID, validateSuiteNodeName(profile))
	if err != nil {
		return st, err
	}
	st.Suite, st.Commit = suite.Output, suite.Output.Commit
	st.DeliveryCommit = st.Commit
	if err := st.loadShippingWaiver(ctx, opts); err != nil {
		return st, err
	}
	if st.Rounds, err = opts.DB.ListRepairRounds(ctx, instanceID); err != nil {
		return st, err
	}
	if st.Reviews, err = opts.DB.ListValidateReviews(ctx, instanceID, st.Commit); err != nil {
		return st, err
	}
	outcome, err := opts.DB.GetValidateOutcome(ctx, instanceID, st.Commit)
	switch {
	case err == nil:
		st.Assessed, st.Outcome, st.Reason = true, outcome.Outcome, outcome.Reason
		if err := json.Unmarshal(outcome.Assessment, &st.Stories); err != nil {
			return st, fmt.Errorf("the stored assessment is unreadable: %w", err)
		}
		if len(outcome.Handback) > 0 {
			var h ValidateHandback
			if err := json.Unmarshal(outcome.Handback, &h); err == nil {
				st.Handback = &h
			}
		}
	case errors.Is(err, store.ErrNotFound):
	default:
		return st, err
	}
	verdicts, err := opts.DB.ListValidateVerdicts(ctx, instanceID, st.Commit, false)
	if err != nil {
		return st, err
	}
	choices, err := opts.DB.ListValidateChoices(ctx, instanceID, st.Commit, false)
	if err != nil {
		return st, err
	}
	effects, err := opts.DB.ListValidateEffects(ctx, instanceID)
	if err != nil {
		return st, err
	}
	st.Effects.Issues = map[string]store.ValidateEffect{}
	for i := range effects {
		e := effects[i]
		if (e.Kind == store.EffectIssue && e.Commit != st.Commit) || (e.Kind != store.EffectIssue && e.Commit != st.deliveryCommit()) {
			continue
		}
		switch e.Kind {
		case store.EffectIssue:
			st.Effects.Issues[e.Target] = e
		case store.EffectPush:
			st.Effects.Push = &e
		case store.EffectPR:
			st.Effects.PR = &e
		case store.EffectMerge:
			st.Effects.Merge = &e
		case store.EffectClose:
			st.Effects.Close = &e
		}
	}
	verdictOf := map[string]*store.ValidateVerdict{}
	for i := range verdicts {
		verdictOf[verdicts[i].Criterion] = &verdicts[i]
	}
	choiceOf := map[string]*store.ValidateChoice{}
	for i := range choices {
		choiceOf[choices[i].Finding] = &choices[i]
	}
	for _, story := range st.Stories {
		for _, c := range story.Criteria {
			f := ValidateFinding{ID: story.Story + ":" + c.Name, Story: story.Story, Name: c.Name, Criterion: true, Role: agent.ReviewRoleAcceptance, Status: c.Status, Class: c.Classification, Blocking: true}
			f.Summary, f.Checks = findingEvidence(c.Evidence)
			f.Verdict, f.Choice = verdictOf[f.ID], choiceOf[f.ID]
			switch {
			case slices.Contains(story.Unresolved, f.Role):
				f.State = findingNotReviewed
			case c.Status == "satisfied":
				f.State = findingSatisfied
			case c.Status == "unverified" && f.Verdict != nil && f.Verdict.Verdict == store.VerdictPass:
				f.State = findingOwnerPassed
			case c.Status == "unverified" && f.Verdict != nil:
				f.State = findingOwnerFailed
			case c.Status == "unverified":
				f.State = findingAwaitingCheck
			default:
				f.State = findingOpen
			}
			st.Findings = append(st.Findings, f)
		}
		for _, r := range story.Reviews {
			f := ValidateFinding{ID: story.Story + ":" + r.Name, Story: story.Story, Name: r.Name, Role: reviewOwner(r.Name), Status: r.Status, Class: r.Classification}
			f.Summary, f.Checks = findingEvidence(r.Evidence)
			f.Verdict, f.Choice = verdictOf[f.ID], choiceOf[f.ID]
			f.Blocking = r.Classification == "blocking"
			f.Optional = !f.Blocking
			switch {
			case slices.Contains(story.Unresolved, f.Role):
				f.State = findingNotReviewed
			case r.Status == "satisfied":
				f.State = findingSatisfied
			case f.checkable() && f.Verdict != nil && f.Verdict.Verdict == store.VerdictPass:
				f.State = findingOwnerPassed
			case f.checkable() && f.Verdict != nil:
				f.State = findingOwnerFailed
			case f.checkable():
				f.State = findingAwaitingCheck
			case f.Blocking && f.Verdict != nil && f.Verdict.Verdict == store.VerdictPass:
				// The owner accepted a deliberate deviation.
				f.State = findingOwnerPassed
			default:
				f.State = findingOpen
			}
			st.Findings = append(st.Findings, f)
		}
	}
	for _, n := range profile.Nodes {
		st.Reimplement = st.Reimplement || n.Action == graphexec.ValidateImplementAction
	}
	if st.Reimplement {
		if st.Reimplementing, err = validateImplementStarted(ctx, opts, instanceID, st.Commit); err != nil {
			return st, err
		}
	}
	st.computeReadiness()
	st.Revision = st.revision()
	return st, nil
}

// reviewOwner names the reviewer that owns a review, as JoinValidateAssessments
// assigns them: the acceptance reviewer owns the plan review.
func reviewOwner(name string) string {
	switch name {
	case "code":
		return agent.ReviewRoleCode
	case "architecture":
		return agent.ReviewRoleArchitecture
	}
	return agent.ReviewRoleAcceptance
}

// notReviewed lists the not-reviewed items of each reviewer role, in role order.
func (s ValidateState) notReviewed() (roles []string, items map[string][]string) {
	items = map[string][]string{}
	for _, f := range s.Findings {
		if f.State == findingNotReviewed {
			items[f.Role] = append(items[f.Role], f.ID)
		}
	}
	for _, role := range ValidateReviewRoles {
		if len(items[role]) > 0 {
			roles = append(roles, role)
		}
	}
	return roles, items
}

// reviewerReason is the recorded reason a reviewer has no usable result.
func reviewerReason(reviews []store.ValidateReview, role string) string {
	for _, r := range reviews {
		if r.Role == role {
			if strings.TrimSpace(r.Reason) != "" {
				return r.State + ": " + clipText(r.Reason, 160)
			}
			return r.State
		}
	}
	return "not run"
}

func findingEvidence(ev []agent.EvidenceExplanation) (string, []string) {
	var parts, checks []string
	for _, e := range ev {
		if s := strings.TrimSpace(e.Summary); s != "" {
			parts = append(parts, s)
		}
		checks = append(checks, e.CheckIDs...)
	}
	return shortenFindingText(strings.Join(parts, " "), validateFindingTextLimit), checks
}

// validateFindingTextLimit bounds one finding's text as it is kept and shown;
// a story comment may shorten further to fit the forge's comment limit.
const validateFindingTextLimit = 4096

// findingTextShortened marks finding text that was cut to fit.
const findingTextShortened = " (text shortened)"

// shortenFindingText returns text unchanged when it fits limit bytes.
// Otherwise it cuts at the last sentence end or line break before limit,
// falling back to the last space and then to the byte limit, keeps valid
// UTF-8 and appends a visible note that the text was shortened.
func shortenFindingText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	head := text[:limit]
	cut := -1
	for _, end := range []string{". ", "! ", "? "} {
		if i := strings.LastIndex(head, end); i >= 0 && i+1 > cut {
			cut = i + 1
		}
	}
	if i := strings.LastIndex(head, "\n"); i > cut {
		cut = i
	}
	if cut <= 0 {
		cut = strings.LastIndex(head, " ")
	}
	if cut <= 0 {
		cut = limit
	}
	return strings.TrimRight(strings.ToValidUTF8(text[:cut], ""), " \t\r\n") + findingTextShortened
}

// computeReadiness preserves every factual blocker. Shipping requires either
// a current exact owner waiver or all factual gates: a passed suite, each criterion
// satisfied or owner-passed, no open blocking finding, each optional finding
// has a choice and no issue creation is pending, failed or uncertain.
func (s *ValidateState) computeReadiness() {
	s.Open = nil
	suiteFailed := s.Suite.Outcome != store.SuitePassed
	if suiteFailed {
		blocker := fmt.Sprintf("The suite did not pass on commit `%s` (%s).", short(s.Commit), s.Suite.Outcome)
		if cause := s.suiteCause(); cause != "" {
			blocker = fmt.Sprintf("The suite did not pass on commit `%s` (%s): %s", short(s.Commit), s.Suite.Outcome, cause)
		}
		s.Open = append(s.Open, blocker)
	}
	// While the suite fails, items awaiting the owner's check cannot be
	// settled; they are named together on one line after the suite blocker.
	var waiting []string
	if !s.Assessed {
		s.Open = append(s.Open, "The assessment of this commit is not recorded yet.")
	}
	roles, items := s.notReviewed()
	for _, role := range roles {
		s.Open = append(s.Open, fmt.Sprintf("The %s reviewer has no usable result (%s); %d items are not reviewed.", role, reviewerReason(s.Reviews, role), len(items[role])))
	}
	for _, f := range s.Findings {
		switch {
		case f.State == findingAwaitingCheck && suiteFailed:
			waiting = append(waiting, f.ID)
		case f.State == findingAwaitingCheck:
			s.Open = append(s.Open, fmt.Sprintf("Item `%s` awaits your check.", f.ID))
		case f.State == findingOwnerFailed && f.checkable():
			s.Open = append(s.Open, fmt.Sprintf("Blocking finding `%s` is open (your check failed). A later evidenced /validate-check verdict can clear it.", f.ID))
		case f.acceptable():
			s.Open = append(s.Open, fmt.Sprintf("Blocking finding `%s` is open (%s). A repair or an issue does not clear it; a new tested snapshot does, or accept a deliberate deviation with `%s <revision> %s accept -- <reason>`.", f.ID, choiceWords(f), ValidateFindingCommand, f.ID))
		case f.needsChoice() && f.Blocking:
			s.Open = append(s.Open, fmt.Sprintf("Blocking finding `%s` is open (%s). A repair or an issue does not clear it; only a new tested snapshot does.", f.ID, choiceWords(f)))
		case f.needsChoice() && f.Choice == nil:
			s.Open = append(s.Open, fmt.Sprintf("Optional finding `%s` needs a choice.", f.ID))
		}
		if f.Choice != nil && f.Choice.Choice == store.ChoiceIssue {
			if e, ok := s.Effects.Issues[f.ID]; !ok {
				s.Open = append(s.Open, fmt.Sprintf("The issue for `%s` is pending.", f.ID))
			} else if e.State != store.EffectSucceeded {
				s.Open = append(s.Open, fmt.Sprintf("The issue for `%s` is %s.", f.ID, e.State))
			}
		}
	}
	if len(waiting) > 0 {
		line := fmt.Sprintf("%d items await your check (%s); they cannot be settled while the suite fails.", len(waiting), strings.Join(waiting, ", "))
		if len(waiting) == 1 {
			line = fmt.Sprintf("1 item awaits your check (%s); it cannot be settled while the suite fails.", waiting[0])
		}
		s.Open = slices.Insert(s.Open, 1, line)
	}
	s.Ready = len(s.Open) == 0 || s.waiverCurrent()
}

// suiteCauseLimit bounds the cause the suite blocker carries.
const suiteCauseLimit = 200

// suiteCause names why the suite did not pass: the first handed-back failure's
// command and diagnosed cause, otherwise the suite's own reason.
func (s *ValidateState) suiteCause() string {
	if s.Handback != nil && len(s.Handback.Failures) > 0 {
		f := s.Handback.Failures[0]
		text := "`" + f.Command + "`"
		if cause := strings.TrimSpace(f.Cause); cause != "" && !strings.EqualFold(cause, agentresult.RepairUnknownCause) {
			text += ": " + clipRunes(strings.Join(strings.Fields(cause), " "), suiteCauseLimit)
		}
		return text
	}
	return clipRunes(strings.Join(strings.Fields(s.Suite.Reason), " "), suiteCauseLimit)
}

// clipRunes keeps text within limit characters, marking a cut with an ellipsis.
func clipRunes(text string, limit int) string {
	r := []rune(text)
	if len(r) <= limit {
		return text
	}
	return string(r[:limit-1]) + "…"
}

func choiceWords(f ValidateFinding) string {
	if f.Choice == nil {
		return "no choice yet"
	}
	if f.Choice.Choice == store.ChoiceIssue {
		return "your choice: issue " + f.Choice.IssueKind
	}
	return "your choice: " + f.Choice.Choice
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// revision is a short digest of everything the owner's commands depend on: the
// tested commit, the findings and their states, the verdicts, the choices and
// the effect states. Any change makes earlier commands stale.
func (s ValidateState) revision() string {
	h := sha256.New()
	_, factualDigest := s.shippingEvidence()
	fmt.Fprintf(h, "evidence=%s\n", factualDigest)
	fmt.Fprintf(h, "run=%d\ncommit=%s\nsuite=%s\noutcome=%s\n", s.InstanceID, s.Commit, s.Suite.Outcome, s.Outcome)
	for _, f := range s.Findings {
		fmt.Fprintf(h, "finding=%s|%s|%s", f.ID, f.Status, f.State)
		if f.Verdict != nil {
			fmt.Fprintf(h, "|verdict=%s@%d", f.Verdict.Verdict, f.Verdict.CommentID)
		}
		if f.Choice != nil {
			fmt.Fprintf(h, "|choice=%s/%s@%d", f.Choice.Choice, f.Choice.IssueKind, f.Choice.CommentID)
		}
		h.Write([]byte("\n"))
	}
	var effects []store.ValidateEffect
	for _, e := range s.Effects.Issues {
		effects = append(effects, e)
	}
	for _, e := range []*store.ValidateEffect{s.Effects.Push, s.Effects.PR, s.Effects.Merge, s.Effects.Close} {
		if e != nil {
			effects = append(effects, *e)
		}
	}
	sort.Slice(effects, func(i, j int) bool { return effects[i].ID < effects[j].ID })
	for _, e := range effects {
		fmt.Fprintf(h, "effect=%s|%s|%s|%d\n", e.Kind, e.Target, e.State, e.ConfirmCommentID)
		raw, _ := json.Marshal(e)
		fmt.Fprintf(h, "effect_evidence=%s\n", raw)
	}
	if s.Reimplementing {
		h.Write([]byte("reimplementing\n"))
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// effectResult decodes the result of a succeeded or partial effect.
type validateEffectResult struct {
	Head           string `json:"head,omitempty"`
	Number         int    `json:"number,omitempty"`
	MergeSHA       string `json:"merge_sha,omitempty"`
	Issue          int    `json:"issue,omitempty"`
	IssueReference string `json:"issue_reference,omitempty"`
	Closed         []int  `json:"closed,omitempty"`
}

func decodeEffectResult(e *store.ValidateEffect) validateEffectResult {
	var r validateEffectResult
	if e != nil {
		_ = json.Unmarshal(e.Result, &r)
	}
	return r
}

func effectDone(e *store.ValidateEffect) bool { return e != nil && e.State == store.EffectSucceeded }

// validateNumbers renders issue numbers the way the close command lists them.
func validateNumbers(numbers []int) string {
	sorted := slices.Clone(numbers)
	slices.Sort(sorted)
	parts := make([]string, len(sorted))
	for i, n := range sorted {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, " ")
}
