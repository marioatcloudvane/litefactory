package planbrief

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// PlanParallelVersion versions optional parallel planning state independently of legacy plans.
const PlanParallelVersion = 1

// PlanRouteState records graph decisions without owning the graph's routing policy.
type PlanRouteState struct {
	Version        int              `json:"version"`
	Cycle          string           `json:"cycle"`
	Capture        string           `json:"capture"`
	SourceRevision string           `json:"source_revision"`
	EntryID        string           `json:"entry_id"`
	Current        string           `json:"current"`
	Fallback       string           `json:"fallback,omitempty"`
	Events         []PlanRouteEvent `json:"events,omitempty"`
}

// PlanRouteEvent retains one captured graph comparison or growth transition.
type PlanRouteEvent struct {
	Route  string `json:"route"`
	Node   string `json:"node"`
	Fact   string `json:"fact,omitempty"`
	Value  string `json:"value,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// PlanParallelContribution contains authored sections only. The runtime binds
// role, attempt and basis separately; agents cannot select those identities.
type PlanParallelContribution struct {
	draftFields map[string]bool
	Draft       PlanDraft `json:"draft"`
	Agreement   string    `json:"agreement,omitempty"`
	Questions   []string  `json:"questions,omitempty"`
	Growth      string    `json:"growth,omitempty"`
	Failure     string    `json:"failure,omitempty"`
	Conflicts   []string  `json:"conflicts,omitempty"`
}

// PlanParallelMember attributes a retained role projection to its accepted result.
type PlanParallelMember struct {
	Role           string                   `json:"role"`
	AttemptID      int64                    `json:"attempt_id"`
	ResultRevision string                   `json:"result_revision"`
	Contribution   PlanParallelContribution `json:"contribution"`
}

// PlanParallelGroup retains one immutable input basis and its bounded reconciliation.
type PlanParallelGroup struct {
	ReviewRequired        bool                          `json:"review_required,omitempty"`
	ParentAttemptID       int64                         `json:"parent_attempt_id,omitempty"`
	SourceDraftRevision   string                        `json:"source_draft_revision,omitempty"`
	ExpectedDraftRevision string                        `json:"expected_draft_revision,omitempty"`
	Projections           map[string]json.RawMessage    `json:"projections"`
	Base                  PlanDraft                     `json:"base"`
	Version               int                           `json:"version"`
	ID                    string                        `json:"id"`
	Basis                 string                        `json:"basis"`
	Limit                 int                           `json:"limit"`
	Reconciled            string                        `json:"reconciled,omitempty"`
	Round                 int                           `json:"round"`
	Members               map[string]PlanParallelMember `json:"members"`
	Proposal              PlanDraft                     `json:"proposal"`
	Digest                string                        `json:"digest,omitempty"`
	Sealed                bool                          `json:"sealed,omitempty"`
	Blockers              []string                      `json:"blockers,omitempty"`
	Applied               map[string]string             `json:"applied"`
}

// NewPlanParallelGroup starts collection without granting publication or approval authority.
func NewPlanParallelGroup(id, basis string, limit int, draft PlanDraft) (*PlanParallelGroup, error) {
	if id == "" || basis == "" || limit <= 0 {
		return nil, errors.New("invalid parallel Plan identity or continuation limit")
	}
	raw, _ := json.Marshal(draft)
	var clone PlanDraft
	if err := json.Unmarshal(raw, &clone); err != nil {
		return nil, err
	}
	draft = clone
	var proposal PlanDraft
	if err := json.Unmarshal(raw, &proposal); err != nil {
		return nil, err
	}
	return &PlanParallelGroup{Base: draft, SourceDraftRevision: draft.Revision(), ExpectedDraftRevision: draft.Revision(), Projections: map[string]json.RawMessage{}, Version: PlanParallelVersion, ID: id, Basis: basis, Limit: limit, Members: map[string]PlanParallelMember{}, Proposal: proposal, Applied: map[string]string{}}, nil
}

// ValidateParallelContribution rejects cross-role writes. A section omission
// means reuse the basis; a role may otherwise replace only its own sections.
func ValidateParallelContribution(role string, c PlanParallelContribution) error {
	if !validContentUTF8(reflect.ValueOf(c)) {
		return errors.New("parallel contribution contains invalid UTF-8")
	}
	if !slices.Contains(planStages, role) {
		return errors.New("invalid parallel Plan role")
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if len(raw) > 1024*1024 {
		return errors.New("parallel contribution exceeds 1 MiB")
	}
	d := c.Draft
	owned := map[string][]string{"story": {"name", "user_story", "problem", "in_scope", "not_in_scope", "dependencies", "frontend"}, "technical": {"architecture", "design", "guidance"}, "acceptance": {"tasks", "criteria"}}
	for owner, fields := range owned {
		if owner == role {
			continue
		}
		for _, field := range fields {
			if c.draftFields[field] {
				return errors.New("parallel contribution writes another role's section")
			}
		}
	}
	if role != "story" && (d.Name != "" || !reflect.DeepEqual(d.UserStory, UserStoryContent{}) || d.Problem != "" || d.InScope != nil || d.NotInScope != nil || !reflect.DeepEqual(d.Dependencies, DependencyContent{}) || d.Frontend) {
		return errors.New("parallel contribution writes another role's intent")
	}
	if role != "technical" && (d.Architecture != nil || d.Design != nil || d.Guidance != nil) {
		return errors.New("parallel contribution writes another role's technical sections")
	}
	if role != "acceptance" && (d.Tasks != nil || d.Criteria != nil) {
		return errors.New("parallel contribution writes another role's acceptance sections")
	}
	return nil
}

// Collect is idempotent for a retained result and never lets a later success
// clear a durable question, failure, conflict or growth signal.
func (g *PlanParallelGroup) Collect(basis, role string, attempt int64, revision string, c PlanParallelContribution) error {
	if g.Version != PlanParallelVersion || basis != g.Basis || attempt <= 0 || revision == "" {
		return errors.New("stale or invalid parallel contribution binding")
	}
	if err := ValidateParallelContribution(role, c); err != nil {
		return err
	}
	raw, _ := json.Marshal(c)
	var clone PlanParallelContribution
	if err := json.Unmarshal(raw, &clone); err != nil {
		return err
	}
	c = clone
	if old, ok := g.Members[role]; ok {
		c = mergeParallelContribution(old.Contribution, c)
	}
	if old, ok := g.Members[role]; ok && old.AttemptID == attempt {
		if old.ResultRevision == revision && reflect.DeepEqual(old.Contribution, c) {
			return nil
		}
		return errors.New("parallel attempt already has a different result")
	}
	if g.Sealed {
		return errors.New("parallel Plan is already sealed")
	}
	if old, ok := g.Members[role]; ok && old.AttemptID > attempt {
		return errors.New("expired parallel member attempt")
	}
	if c.Draft.Key != "" && c.Draft.Key != g.Proposal.Key {
		return errors.New("parallel contribution has another story key")
	}
	g.Members[role] = PlanParallelMember{role, attempt, revision, c}
	for _, b := range append(append(append([]string{}, c.Questions...), c.Conflicts...), c.Growth, c.Failure) {
		if strings.TrimSpace(b) != "" && !slices.Contains(g.Blockers, b) {
			g.Blockers = append(g.Blockers, b)
		}
	}
	for _, question := range c.Draft.Questions {
		if strings.TrimSpace(question) != "" && !slices.Contains(g.Blockers, question) {
			g.Blockers = append(g.Blockers, question)
		}
	}
	return nil
}

// Reconcile assembles in fixed ownership order, so completion order has no effect.
// A changed proposal invalidates every prior agreement through its digest.
func (g *PlanParallelGroup) Reconcile(scope ContentScope) (bool, error) {
	if g.Version != PlanParallelVersion {
		return false, errors.New("unsupported parallel Plan state version")
	}
	if len(g.Blockers) > 0 {
		return false, errors.New("parallel Plan has unresolved blockers")
	}
	if len(g.Members) != 3 {
		return false, nil
	}
	refs := map[string]string{}
	for _, role := range planStages {
		member, ok := g.Members[role]
		if !ok {
			return false, nil
		}
		refs[role] = fmt.Sprintf("%d:%s", member.AttemptID, member.ResultRevision)
	}
	reconciled := planHash(refs)
	if reconciled != g.Reconciled {
		if g.Round >= g.Limit {
			g.Blockers = append(g.Blockers, "parallel Plan continuation limit exhausted")
			return false, errors.New("parallel Plan continuation limit exhausted")
		}
		g.Round++
		g.Reconciled = reconciled
	}
	d := g.assembledDraft()
	if err := d.Validate("acceptance", scope); err != nil {
		return false, err
	}
	digest := d.Revision()
	if digest != g.Digest {
		g.Proposal = d
		g.Digest = digest
	}
	for _, role := range planStages {
		if g.Members[role].Contribution.Agreement != digest {
			return false, nil
		}
	}
	return true, nil
}

// Seal freezes a complete proposal only after every role agrees on its current digest.
func (g *PlanParallelGroup) Seal(basis string, scope ContentScope) error {
	if g.Version != PlanParallelVersion {
		return errors.New("unsupported parallel Plan state version")
	}
	if basis != g.Basis {
		return errors.New("parallel Plan basis changed")
	}
	ready, err := g.Reconcile(scope)
	if err != nil {
		return err
	}
	if !ready {
		return errors.New("parallel Plan lacks current agreement")
	}
	g.Sealed = true
	return nil
}

// Projection exposes only the cumulative stage content. Application still uses
// the ordinary effects, revision identities and approval guards.
func (g *PlanParallelGroup) Projection(basis, stage string) (PlanDraft, error) {
	if g.Version != PlanParallelVersion || !g.Sealed || basis != g.Basis || len(g.Blockers) > 0 || g.Proposal.Revision() != g.Digest {
		return PlanDraft{}, errors.New("parallel Plan seal is not current")
	}
	if !slices.Contains(planStages, stage) {
		return PlanDraft{}, errors.New("invalid parallel projection stage")
	}
	for _, prior := range planStages {
		if prior == stage {
			break
		}
		if g.Applied[prior] == "" {
			return PlanDraft{}, fmt.Errorf("parallel Plan requires %s application first", prior)
		}
	}
	d := g.Proposal
	if stage != "acceptance" {
		d.Tasks = nil
		d.Criteria = nil
	}
	if stage == "story" {
		d.Architecture = nil
		d.Design = nil
		d.Guidance = nil
		d.Decisions = nil
		d.Assumptions = nil
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return PlanDraft{}, err
	}
	var projection PlanDraft
	if err = json.Unmarshal(raw, &projection); err != nil {
		return PlanDraft{}, err
	}
	return projection, nil
}

// RecordPlanRoute retains a graph decision within the existing planning cycle.
func (s *PlanState) RecordPlanRoute(key string, route PlanRouteState, event PlanRouteEvent) error {
	item, err := s.story(key)
	if err != nil {
		return err
	}
	if item.Route != nil && item.Route.Version != PlanParallelVersion {
		return errors.New("unsupported Plan route state version")
	}
	if item.Route == nil {
		if route.Cycle == "" || route.Capture == "" {
			return errors.New("route requires cycle and capture")
		}
		route.Version = PlanParallelVersion
		item.Route = &route
	}
	if item.Route.Fallback != "" && event.Route != "normal" {
		return errors.New("story has sticky normal fallback")
	}
	if len(item.Route.Events) > 0 && item.Route.Events[len(item.Route.Events)-1] == event && item.Route.Current == event.Route {
		return nil
	}
	item.Route.Current = event.Route
	item.Route.Events = append(item.Route.Events, event)
	s.invalidateAssembly()
	return nil
}

// RecordPlanGrowth preserves unaffected approvals and invalidates only the
// affected stage and its existing dependent closure.
func (s *PlanState) RecordPlanGrowth(key, stage, reason, source string) error {
	item, err := s.story(key)
	if err != nil {
		return err
	}
	if item.Route == nil || item.Route.Version != PlanParallelVersion || strings.TrimSpace(reason) == "" || !slices.Contains(planStages, stage) {
		return errors.New("growth requires route, reason and affected stage")
	}
	item.Route.Fallback = reason
	item.Route.Current = "normal"
	item.Route.Events = append(item.Route.Events, PlanRouteEvent{Route: "normal", Node: source, Reason: reason})
	if item.Parallel != nil {
		item.Parallel.Sealed = false
		item.Parallel.Blockers = append(item.Parallel.Blockers, reason)
	}
	s.Invalidate(key, stage, "")
	return nil
}

// Route reason kinds explain a route in owner text without exposing graph evidence.
const (
	PlanRouteReasonFastTrack = "fast_track"
	PlanRouteReasonSize      = "size"
	PlanRouteReasonSizing    = "sizing"
	PlanRouteReasonGrowth    = "growth"
)

// PlanRouteReview is a rendering projection supplied by the captured graph.
// Sizing remains a reference to the original set entry, never a second source.
// Reason and FastTrackSize explain Route; FastTrackSize is the graph's comparison.
type PlanRouteReview struct {
	Story         string           `json:"story"`
	Size          string           `json:"size,omitempty"`
	SizeReason    string           `json:"size_reason,omitempty"`
	Route         string           `json:"route"`
	Reason        string           `json:"reason,omitempty"`
	FastTrackSize string           `json:"fast_track_size,omitempty"`
	Growth        string           `json:"growth,omitempty"`
	History       []PlanRouteEvent `json:"history,omitempty"`
	GraphApproved []string         `json:"graph_approved,omitempty"`
}

// RouteReviews projects original sizing evidence alongside retained route history.
func (s *PlanState) RouteReviews() []PlanRouteReview {
	out := []PlanRouteReview{}
	for _, item := range s.Stories {
		r := PlanRouteReview{Story: item.Key, Route: "normal"}
		if s.StorySet != nil {
			for _, entry := range s.StorySet.Entries {
				if s.StorySet.Keys[entry.ID] == item.Key {
					r.Size = entry.Size
					r.SizeReason = entry.SizeReason
					break
				}
			}
		}
		if item.Route != nil {
			r.Route = item.Route.Current
			r.Growth = item.Route.Fallback
			if r.Growth != "" {
				r.Reason = PlanRouteReasonGrowth
			}
			r.History = slices.Clone(item.Route.Events)
		}
		for _, stage := range planStages {
			if item.ApprovedBy[stage] == "graph" && item.Approvals[stage] != "" {
				r.GraphApproved = append(r.GraphApproved, stage)
			}
		}
		out = append(out, r)
	}
	return out
}

// planRouteLine states a route and its reason in one plain sentence.
func planRouteLine(r PlanRouteReview) string {
	if r.Route == "parallel" {
		if r.Size != "" {
			return "Fast track (size " + r.Size + ")"
		}
		return "Fast track"
	}
	if r.Route != "" && r.Route != "normal" {
		return r.Route
	}
	switch r.Reason {
	case PlanRouteReasonSize:
		if r.FastTrackSize != "" {
			return "Normal path: size " + r.Size + "; the fast track is only for size " + r.FastTrackSize + " stories"
		}
		return "Normal path: size " + r.Size
	case PlanRouteReasonSizing:
		return "Normal path: the size or its reason is missing or invalid"
	case PlanRouteReasonGrowth:
		if r.Growth != "" {
			return "Normal path: the story grew beyond the fast track: " + r.Growth
		}
		return "Normal path: the story moved off the fast track"
	}
	return "Normal path"
}

// RenderPlanRoutes shows each story's route with one plain reason and approval
// provenance. Graph evidence stays in the retained route state.
func RenderPlanRoutes(reviews []PlanRouteReview) string {
	approved := slices.ContainsFunc(reviews, func(r PlanRouteReview) bool { return len(r.GraphApproved) > 0 })
	var b strings.Builder
	if approved {
		b.WriteString("| Story | Size | Size reason | Route | Graph-approved stages |\n|---|---|---|---|---|\n")
	} else {
		b.WriteString("| Story | Size | Size reason | Route |\n|---|---|---|---|\n")
	}
	for _, r := range reviews {
		fmt.Fprintf(&b, "| %s | %s | %s | %s |", tableCell(r.Story), tableCell(r.Size), tableCell(r.SizeReason), tableCell(planRouteLine(r)))
		if approved {
			fmt.Fprintf(&b, " %s |", tableCell(strings.Join(r.GraphApproved, ", ")))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// MarshalJSON keeps role projections sparse: the ordinary PlanDraft wire shape
// includes required intent fields, while a technical or acceptance contribution
// must omit fields owned by the story role. Explicit empty arrays remain edits.
func (c PlanParallelContribution) MarshalJSON() ([]byte, error) {
	draft := map[string]any{}
	value := reflect.ValueOf(c.Draft)
	typ := value.Type()
	for i := 0; i < value.NumField(); i++ {
		field := value.Field(i)
		name := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if field.IsZero() && !c.draftFields[name] {
			continue
		}
		draft[name] = field.Interface()
		if name == "user_story" {
			v := c.Draft.UserStory
			if v.Notes == nil {
				v.Notes = []string{}
			}
			draft[name] = v
		}
		if name == "tasks" {
			tasks := slices.Clone(c.Draft.Tasks)
			for i := range tasks {
				if tasks[i].DependsOn == nil {
					tasks[i].DependsOn = []string{}
				}
				if tasks[i].CriterionIDs == nil {
					tasks[i].CriterionIDs = []string{}
				}
			}
			draft[name] = tasks
		}
		if name == "dependencies" {
			v := c.Draft.Dependencies
			if v.StoryKeys == nil {
				v.StoryKeys = []string{}
			}
			if v.IssueRefs == nil {
				v.IssueRefs = []string{}
			}
			draft[name] = v
		}
	}
	type wire struct {
		Draft     map[string]any `json:"draft"`
		Agreement string         `json:"agreement,omitempty"`
		Questions []string       `json:"questions,omitempty"`
		Growth    string         `json:"growth,omitempty"`
		Failure   string         `json:"failure,omitempty"`
		Conflicts []string       `json:"conflicts,omitempty"`
	}
	return json.Marshal(wire{draft, c.Agreement, c.Questions, c.Growth, c.Failure, c.Conflicts})
}

// UnmarshalJSON preserves explicit zero-valued edits such as frontend=false,
// and distinguishes an omitted section from a cross-role zero-valued write.
func (c *PlanParallelContribution) UnmarshalJSON(raw []byte) error {
	type contribution PlanParallelContribution
	var decoded contribution
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	var shape struct {
		Draft map[string]json.RawMessage `json:"draft"`
	}
	if err := json.Unmarshal(raw, &shape); err != nil {
		return err
	}
	decoded.draftFields = map[string]bool{}
	for field := range shape.Draft {
		decoded.draftFields[field] = true
	}
	*c = PlanParallelContribution(decoded)
	return nil
}

func mergeParallelContribution(previous, current PlanParallelContribution) PlanParallelContribution {
	merged := previous.Draft
	source := reflect.ValueOf(current.Draft)
	target := reflect.ValueOf(&merged).Elem()
	typ := source.Type()
	fields := map[string]bool{}
	for name, present := range previous.draftFields {
		fields[name] = present
	}
	for i := 0; i < source.NumField(); i++ {
		name := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if current.draftFields[name] || !source.Field(i).IsZero() {
			target.Field(i).Set(source.Field(i))
			fields[name] = true
		}
	}
	current.Draft = merged
	current.draftFields = fields
	if current.Growth == "" {
		current.Growth = previous.Growth
	}
	if current.Failure == "" {
		current.Failure = previous.Failure
	}
	for _, question := range previous.Questions {
		if !slices.Contains(current.Questions, question) {
			current.Questions = append(current.Questions, question)
		}
	}
	for _, conflict := range previous.Conflicts {
		if !slices.Contains(current.Conflicts, conflict) {
			current.Conflicts = append(current.Conflicts, conflict)
		}
	}
	return current
}

func (g *PlanParallelGroup) assembledDraft() PlanDraft {
	d := g.Base
	for _, role := range planStages {
		m, ok := g.Members[role]
		if !ok {
			continue
		}
		c := m.Contribution.Draft
		switch role {
		case "story":
			if c.Name != "" {
				d.Name = c.Name
			}
			if !reflect.DeepEqual(c.UserStory, UserStoryContent{}) {
				d.UserStory = c.UserStory
			}
			if c.Problem != "" {
				d.Problem = c.Problem
			}
			if c.InScope != nil {
				d.InScope = c.InScope
			}
			if c.NotInScope != nil {
				d.NotInScope = c.NotInScope
			}
			if !reflect.DeepEqual(c.Dependencies, DependencyContent{}) {
				d.Dependencies = c.Dependencies
			}
			if c.Frontend || m.Contribution.draftFields["frontend"] {
				d.Frontend = c.Frontend
			}
		case "technical":
			if c.Architecture != nil {
				d.Architecture = c.Architecture
			}
			if c.Design != nil {
				d.Design = c.Design
			}
			if c.Guidance != nil {
				d.Guidance = c.Guidance
			}
		case "acceptance":
			if c.Tasks != nil {
				d.Tasks = c.Tasks
			}
			if c.Criteria != nil {
				d.Criteria = c.Criteria
			}
		}
	}
	d.Decisions = slices.Clone(g.Base.Decisions)
	d.Assumptions = slices.Clone(g.Base.Assumptions)
	d.Questions = nil
	for _, role := range planStages {
		c := g.Members[role].Contribution.Draft
		for _, v := range c.Decisions {
			if !slices.Contains(d.Decisions, v) {
				d.Decisions = append(d.Decisions, v)
			}
		}
		for _, v := range c.Assumptions {
			if !slices.Contains(d.Assumptions, v) {
				d.Assumptions = append(d.Assumptions, v)
			}
		}
	}
	return d
}

// GrowthStage returns the first affected or unproven stage, capped at the
// current stage. Earlier approvals survive only with current unchanged evidence.
func (g *PlanParallelGroup) GrowthStage(current string) string {
	position := slices.Index(planStages, current)
	if g.Version != PlanParallelVersion || position <= 0 {
		return "story"
	}
	draft := g.assembledDraft()
	for _, stage := range planStages[:position] {
		member, present := g.Members[stage]
		if !present || member.Role != stage || member.AttemptID <= 0 || member.ResultRevision == "" {
			return stage
		}
		c := member.Contribution
		if c.Growth != "" || c.Failure != "" || len(c.Questions) > 0 || len(c.Conflicts) > 0 || len(c.Draft.Questions) > 0 {
			return stage
		}
		if draft.StageRevision(stage) != g.Base.StageRevision(stage) {
			return stage
		}
	}
	return current
}
