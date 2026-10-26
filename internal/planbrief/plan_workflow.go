package planbrief

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/cloudvane/litefactory/internal/commandtext"
)

// PlanState is retained factory policy state, never an agent authority payload.
// A full copy is journaled after every transition; drafts are still editable only
// on their issues. Approvals refer to semantic content, not presentation bytes.
type PlanState struct {
	SharedRevision    string           `json:"shared_revision"`
	Stories           []PlanStoryState `json:"stories"`
	AssembledApproval string           `json:"assembled_approval,omitempty"`
	AssembledWait     string           `json:"assembled_wait,omitempty"`
	ConsumedReplies   []int64          `json:"consumed_replies,omitempty"`
	PublishedRevision string           `json:"published_revision,omitempty"`
	SavedRevision     string           `json:"saved_revision,omitempty"`
	// StorySet is the proposed or promoted story set. It is absent in a Plan saved
	// before story sets existed, and in one whose scope needed no decomposition.
	StorySet *PlanStorySetState `json:"story_set,omitempty"`
	// SummaryDigest identifies the progress summary the parent last published, so
	// an unchanged summary is not edited again. Only the parent writes it.
	SummaryDigest string `json:"summary_digest,omitempty"`
}

// PlanStoryState retains one story's current stage, candidate and owner gates.
type PlanStoryState struct {
	Key       string            `json:"key"`
	Stage     string            `json:"stage"`
	Candidate string            `json:"candidate"`
	Draft     PlanDraft         `json:"draft"`
	Approvals map[string]string `json:"approvals"`
	// ApprovedBy records who gave the current approval of a stage: owner or graph.
	// A missing entry reads as owner, which holds for every earlier saved state.
	ApprovedBy    map[string]string `json:"approved_by,omitempty"`
	Wait          *PlanWait         `json:"wait,omitempty"`
	Retired       bool              `json:"retired"`
	Replacements  []string          `json:"replacements,omitempty"`
	Hold          string            `json:"hold,omitempty"`
	Lifecycle     PlanLifecycle     `json:"lifecycle,omitempty"`
	LifecycleHold string            `json:"lifecycle_hold,omitempty"`
	EffectHolds   map[string]string `json:"effect_holds,omitempty"`
	EffectRequest string            `json:"effect_request,omitempty"`
	EffectHold    string            `json:"effect_hold,omitempty"`
	Effect        string            `json:"effect,omitempty"`
}

// PlanWait binds a displayed candidate and dependency/shared revision set.
type PlanWait struct {
	ID        string `json:"id"`
	Stage     string `json:"stage"`
	Candidate string `json:"candidate"`
	Revision  string `json:"revision"`
	Kind      string `json:"kind"`
	IssueID   int64  `json:"issue_id"`
}

// PlanOwnerReply is factory-authenticated evidence. Body alone grants no authority.
type PlanOwnerReply struct {
	ID, OwnerID, ActorID, IssueID       int64
	Key, Stage, Candidate, WaitID, Body string
}

var planStages = []string{"story", "technical", "acceptance"}

func planHash(v any) string {
	raw, _ := json.Marshal(v)
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

func (s *PlanState) story(key string) (*PlanStoryState, error) {
	for i := range s.Stories {
		if s.Stories[i].Key == key {
			return &s.Stories[i], nil
		}
	}
	return nil, fmt.Errorf("unknown Plan story %q", key)
}

// StageRevision includes only this stage and approved upstream semantics.
func (d PlanDraft) StageRevision(stage string) string {
	d.Questions = nil
	if stage == "story" {
		d.Decisions, d.Assumptions = nil, nil
		d.Architecture = nil
		d.Design = nil
		d.Guidance = nil
	}
	if stage != "acceptance" {
		d.Tasks = nil
		d.Criteria = nil
	}
	return d.Revision()
}

// ErrPlanUpstreamPublishing matches the error ReviewRevision returns while a
// dependency is only in its publication window: its stage result is being
// published (Effect "pending") and nothing holds it. It is transient, unlike
// every other dependency problem, which stays "inactive or unresolved".
var ErrPlanUpstreamPublishing = errors.New("a plan dependency is still publishing its stage")

// PlanUpstreamPublishingText is the fixed part of the publishing error's text.
const PlanUpstreamPublishingText = "is still publishing its stage"

type planUpstreamPublishing struct{ key string }

func (e planUpstreamPublishing) Error() string {
	return "plan dependency " + e.key + " " + PlanUpstreamPublishingText
}

func (e planUpstreamPublishing) Is(target error) bool { return target == ErrPlanUpstreamPublishing }

// publishingOnly reports that a story is blocked by nothing but its own stage
// result being published.
func publishingOnly(item *PlanStoryState) bool {
	return item.Active() && item.Effect == "pending" && item.Hold == "" && item.LifecycleHold == "" && item.EffectHold == "" && len(item.EffectHolds) == 0
}

// ReviewRevision binds a story to current shared decisions and transitive inputs.
func (s *PlanState) ReviewRevision(key, stage string) (string, error) {
	if !slices.Contains(planStages, stage) {
		return "", errors.New("invalid Plan review stage")
	}
	refs := map[string]string{}
	visiting := map[string]bool{}
	var walk func(string) error
	walk = func(k string) error {
		if visiting[k] {
			return errors.New("plan dependencies contain a cycle")
		}
		if _, seen := refs[k]; seen {
			return nil
		}
		item, err := s.story(k)
		if err != nil {
			return err
		}
		if !item.Active() || item.Blocked() {
			if k != key && publishingOnly(item) {
				return planUpstreamPublishing{key: k}
			}
			return fmt.Errorf("plan dependency %s is inactive or unresolved", k)
		}
		visiting[k] = true
		for _, dep := range item.Draft.Dependencies.StoryKeys {
			if err := walk(dep); err != nil {
				return err
			}
		}
		visiting[k] = false
		refs[k] = item.Draft.StageRevision(stage)
		return nil
	}
	if err := walk(key); err != nil {
		return "", err
	}
	return planHash(struct {
		Shared string
		Stage  string
		Refs   map[string]string
	}{s.SharedRevision, stage, refs}), nil
}

// PutCandidate retains settled later-stage sections through earlier-stage edits.
// The caller supplies merged, validated activity content after confirmed effects.
func (s *PlanState) PutCandidate(d PlanDraft, stage, candidate string) error {
	if !slices.Contains(planStages, stage) || d.Key == "" || candidate == "" || s.SharedRevision == "" {
		return errors.New("invalid Plan candidate state")
	}
	var item *PlanStoryState
	for i := range s.Stories {
		if s.Stories[i].Key == d.Key {
			item = &s.Stories[i]
			break
		}
	}
	if item == nil {
		s.Stories = append(s.Stories, PlanStoryState{Key: d.Key, Stage: stage, Candidate: candidate, Draft: d, Approvals: map[string]string{}})
		s.invalidateAssembly()
		return nil
	}
	if item.Retired {
		return errors.New("retired Plan story cannot be revised")
	}
	before := item.Stage
	earliest := ""
	for _, st := range planStages {
		if item.Draft.StageRevision(st) != d.StageRevision(st) {
			earliest = st
			break
		}
	}
	item.Candidate = candidate
	item.Draft = d
	item.Effect = "applied"
	if earliest != "" {
		s.Invalidate(d.Key, earliest, "")
	}
	// Filling a new stage does not move an earlier unapproved story forward, and
	// neither does a change to a later stage than the story stands at: the story
	// never moves past a stage the owner has not approved.
	if slices.Index(planStages, stage) < slices.Index(planStages, item.Stage) {
		item.Stage = stage
	}
	if slices.Contains(planStages, before) && slices.Index(planStages, before) < slices.Index(planStages, item.Stage) {
		item.Stage = before
	}
	return nil
}

// Invalidate follows recorded dependency edges; unrelated approvals survive.
// Unknown impact is represented as a hold, never silently treated as unrelated.
func (s *PlanState) Invalidate(key, stage, hold string) {
	from := slices.Index(planStages, stage)
	if from < 0 {
		from = 0
		stage = "story"
	}
	affected := map[string]bool{key: true}
	for changed := true; changed; {
		changed = false
		for _, st := range s.Stories {
			for _, dep := range st.Draft.Dependencies.StoryKeys {
				if affected[dep] && !affected[st.Key] {
					affected[st.Key] = true
					changed = true
				}
			}
		}
	}
	for i := range s.Stories {
		st := &s.Stories[i]
		if !affected[st.Key] {
			continue
		}
		// A dependant that has not reached the invalidated stage holds nothing
		// from it: no approval to drop and no review that the change outdates.
		// Moving it forward to that stage would skip its own earlier review, and
		// clearing its wait would lose the request the owner is answering, so
		// an unrelated story waiting at an earlier stage stays exactly as it is.
		// Unknown impact (a hold) still applies to every dependant.
		if st.Key != key && hold == "" && slices.Index(planStages, st.Stage) < from {
			continue
		}
		for _, phase := range planStages[from:] {
			delete(st.Approvals, phase)
			delete(st.ApprovedBy, phase)
		}
		if len(st.ApprovedBy) == 0 {
			st.ApprovedBy = nil
		}
		st.Wait = nil
		st.Stage = stage
		st.Hold = hold
	}
	s.invalidateAssembly()
}

func (s *PlanState) invalidateAssembly() {
	s.AssembledApproval = ""
	s.AssembledWait = ""
	s.PublishedRevision = ""
	s.SavedRevision = ""
}

// SetSharedRevision invalidates every bound approval when shared semantics change.
func (s *PlanState) SetSharedRevision(revision string) error {
	if strings.TrimSpace(revision) == "" {
		return errors.New("shared decision revision required")
	}
	if revision == s.SharedRevision {
		return nil
	}
	s.SharedRevision = revision
	for i := range s.Stories {
		s.Stories[i].Approvals = map[string]string{}
		s.Stories[i].ApprovedBy = nil
		s.Stories[i].Wait = nil
		s.Stories[i].Stage = "story"
	}
	s.invalidateAssembly()
	return nil
}

// UpstreamApproved reports whether every story the story depends on has a
// current approval for that same stage: story-002's technical stage waits until
// story-001's technical stage is approved. A story without upstream stories is
// always ready. It is the one rule that decides whether a stage may run, for
// both a first draft and a later stage.
func (s *PlanState) UpstreamApproved(key, stage string) bool {
	item, err := s.story(key)
	if err != nil {
		return false
	}
	for _, dep := range item.Draft.Dependencies.StoryKeys {
		upstream, err := s.story(dep)
		if err != nil {
			return false
		}
		ref, err := s.ReviewRevision(dep, stage)
		if err != nil || upstream.Approvals[stage] != ref {
			return false
		}
	}
	return true
}

// StoryNextStep names the stage a story may run now, and whether it is the
// story's first draft. ok is false while the story waits for the owner, for an
// upstream approval, or has nothing to do; it is the same answer for the
// daemon's offer and for the run's pick, so one rule decides who may run.
func (s *PlanState) StoryNextStep(key string) (stage string, create, ok bool) {
	item, err := s.story(key)
	if err != nil || !item.Active() || item.Blocked() {
		return "", false, false
	}
	if !s.StoryDrafted(key) {
		if s.StorySet == nil || !s.StorySet.Promoted || !s.UpstreamApproved(key, "story") {
			return "", false, false
		}
		return "story", true, true
	}
	if slices.Contains(s.EligiblePlanStories(), key) {
		return item.Stage, false, true
	}
	// A story that lost an earlier stage's approval without returning to that
	// stage, and has no review waiting, re-offers the earliest unapproved stage
	// so an owner reply can move it again instead of leaving it stranded.
	if item.Wait != nil {
		return "", false, false
	}
	for _, up := range planStages[:max(0, slices.Index(planStages, item.Stage))] {
		ref, err := s.ReviewRevision(key, up)
		if err != nil {
			return "", false, false
		}
		if item.Approvals[up] != ref {
			if !s.UpstreamApproved(key, up) {
				return "", false, false
			}
			return up, false, true
		}
	}
	return "", false, false
}

// EligiblePlanStories returns independent ready work in stable key order.
// A waiting or held story does not stop an unrelated story from progressing.
func (s *PlanState) EligiblePlanStories() []string {
	var keys []string
	for _, item := range s.Stories {
		if !item.Active() || item.Wait != nil || item.Blocked() {
			continue
		}
		if _, err := s.ReviewRevision(item.Key, item.Stage); err != nil {
			continue
		}
		if item.Stage == "acceptance" {
			ref, err := s.ReviewRevision(item.Key, "acceptance")
			if err == nil && item.Approvals["acceptance"] == ref {
				continue
			}
		}
		if !s.UpstreamApproved(item.Key, item.Stage) {
			continue
		}
		eligible := true
		for _, up := range planStages[:max(0, slices.Index(planStages, item.Stage))] {
			ref, err := s.ReviewRevision(item.Key, up)
			if err != nil || item.Approvals[up] != ref {
				eligible = false
			}
		}
		if eligible {
			keys = append(keys, item.Key)
		}
	}
	sort.Strings(keys)
	return keys
}

// OpenPlanWait saves the exact displayed candidate before asking for promotion.
func (s *PlanState) OpenPlanWait(key, id, kind string, issueID int64) error {
	item, err := s.story(key)
	if err != nil {
		return err
	}
	if id == "" || issueID < 1 || !slices.Contains([]string{"question", "review", "retire"}, kind) || !item.Active() || item.Blocked() {
		return errors.New("invalid Plan wait")
	}
	ref, err := s.ReviewRevision(key, item.Stage)
	if err != nil {
		return err
	}
	// No command could satisfy a wait behind a computably stale earlier
	// approval. None is opened; StoryNextStep re-offers that earlier stage, and
	// its review shows the command that works.
	if kind != "retire" && s.staleEarlierApproval(item, item.Stage) {
		return nil
	}
	item.Wait = &PlanWait{ID: id, Stage: item.Stage, Candidate: item.Candidate, Revision: ref, Kind: kind, IssueID: issueID}
	return nil
}

// staleEarlierApproval reports that some stage before stage has an approval
// that differs from that stage's review revision. It reports false whenever any
// of those revisions cannot be computed (an upstream story publishing, held,
// closed or retired): an uncomputable revision is no evidence of staleness.
func (s *PlanState) staleEarlierApproval(item *PlanStoryState, stage string) bool {
	stale := false
	for _, up := range planStages[:max(0, slices.Index(planStages, stage))] {
		ref, err := s.ReviewRevision(item.Key, up)
		if err != nil {
			return false
		}
		if item.Approvals[up] != ref {
			stale = true
		}
	}
	return stale
}

// ClearStaleWaits removes every open question or review wait that sits behind
// a computably stale earlier approval, because no command can satisfy it. The
// story keeps its stage, approvals and candidate; StoryNextStep then re-offers
// the earliest unapproved stage, whose review shows the working command. A
// wait whose earlier revisions cannot be computed is left exactly as it is.
// It reports whether any wait was cleared.
func (s *PlanState) ClearStaleWaits() bool {
	changed := false
	for i := range s.Stories {
		item := &s.Stories[i]
		if item.Wait == nil || item.Wait.Kind == "retire" {
			continue
		}
		if s.staleEarlierApproval(item, item.Wait.Stage) {
			item.Wait = nil
			changed = true
		}
	}
	return changed
}

// approvableWait reports whether the story's open wait can accept an exact
// approval. A review wait can. A question wait can too when it names the
// story's current stage and candidate and the saved draft already holds that
// stage's content; revision, question and upstream checks still apply.
func approvableWait(item *PlanStoryState) bool {
	w := item.Wait
	if w == nil {
		return false
	}
	if w.Kind == "review" {
		return true
	}
	if w.Kind != "question" || w.Stage != item.Stage || item.Candidate == "" || w.Candidate != item.Candidate {
		return false
	}
	switch item.Stage {
	case "story":
		return true
	case "technical":
		return item.Draft.Architecture != nil
	case "acceptance":
		return len(item.Draft.Tasks) > 0 && len(item.Draft.Criteria) > 0
	}
	return false
}

// ApplyOwnerReply consumes exact authenticated replies once. Only the exact
// command promotes; questions, prose and mixed edit/approval text remain feedback.
func (s *PlanState) ApplyOwnerReply(r PlanOwnerReply) (bool, error) {
	if r.ID < 1 || r.OwnerID < 1 || r.ActorID != r.OwnerID {
		return false, errors.New("plan reply is not authenticated owner evidence")
	}
	if slices.Contains(s.ConsumedReplies, r.ID) {
		return false, nil
	}
	item, err := s.story(r.Key)
	if err != nil {
		return false, err
	}
	w := item.Wait
	if w == nil || w.ID != r.WaitID || w.IssueID != r.IssueID || w.Stage != r.Stage || w.Candidate != r.Candidate || item.Candidate != r.Candidate {
		return false, errors.New("stale or wrong-story Plan reply")
	}
	ref, err := s.ReviewRevision(item.Key, item.Stage)
	if err != nil {
		return false, err
	}
	if ref != w.Revision {
		return false, errors.New("plan reply dependencies or shared decisions changed")
	}
	command := "/approve-plan " + r.Key + " " + r.Stage + " " + r.Candidate
	if !approvableWait(item) || strings.TrimSpace(commandtext.Normalize(r.Body)) != command {
		s.ConsumedReplies = append(s.ConsumedReplies, r.ID)
		item.Wait = nil
		return false, nil
	}
	if err := s.approvalChecks(item); err != nil {
		return false, err
	}
	s.ConsumedReplies = append(s.ConsumedReplies, r.ID)
	item.Wait = nil
	approve(item, item.Stage, ref, ApprovalByOwner)
	return true, nil
}

// Approval sources recorded next to a stage approval.
const (
	ApprovalByOwner = "owner"
	ApprovalByGraph = "graph"
)

// approvalChecks holds the rules both approvers share: no hold, an applied
// effect, no open questions, and a current approval of every earlier stage.
func (s *PlanState) approvalChecks(item *PlanStoryState) error {
	if item.Hold != "" || (item.Effect != "" && item.Effect != "applied") || len(item.Draft.Questions) > 0 {
		return errors.New("plan review has unresolved questions or effects")
	}
	for _, stage := range planStages[:max(0, slices.Index(planStages, item.Stage))] {
		upstream, e := s.ReviewRevision(item.Key, stage)
		if e != nil || item.Approvals[stage] != upstream {
			return errors.New("plan promotion lacks current upstream approval")
		}
	}
	return nil
}

// approve is the one place an approval is written: the stage approval, who gave
// it, and the move to the next stage.
func approve(item *PlanStoryState, stage, ref, source string) {
	if item.Approvals == nil {
		item.Approvals = map[string]string{}
	}
	if item.ApprovedBy == nil {
		item.ApprovedBy = map[string]string{}
	}
	item.Approvals[stage] = ref
	item.ApprovedBy[stage] = source
	if n := slices.Index(planStages, stage); n >= 0 && n < 2 && stage == item.Stage {
		item.Stage = planStages[n+1]
	}
}

// GraphApproval is the outcome of ApproveByGraph: approved, review, superseded or
// blocked, with a short factual reason for the last three.
type GraphApproval struct {
	Outcome string
	Reason  string
}

// ApproveByGraph records a stage approval given by an ungated graph stage. It
// is tied to the identity the run applied (stage, candidate, effect request) and
// must run inside one saved-state update, so every guard is read at the moment
// of recording. Only a new approval writes anything.
func (s *PlanState) ApproveByGraph(key, stage, candidate, request string) (GraphApproval, error) {
	if !slices.Contains(planStages, stage) {
		return GraphApproval{}, errors.New("invalid Plan approval stage")
	}
	item, err := s.story(key)
	if err != nil {
		return GraphApproval{}, err
	}
	same := item.Candidate == candidate && item.EffectRequest == request
	ref, refErr := s.ReviewRevision(key, stage)
	// (a) Replay of an approval that already stands for this exact applied draft.
	if same && refErr == nil && item.Approvals[stage] == ref {
		return GraphApproval{Outcome: "approved"}, nil
	}
	// (b) The saved story is no longer the one this run applied.
	if item.Stage != stage || !same {
		return GraphApproval{Outcome: "superseded", Reason: "the story's stage, candidate or effect request changed since this draft was applied"}, nil
	}
	// (c) Held, blocked or without a computable revision.
	if !item.Active() || item.Blocked() {
		return GraphApproval{Outcome: "blocked", Reason: "the story is held or its stage result is not applied"}, nil
	}
	if refErr != nil {
		return GraphApproval{Outcome: "blocked", Reason: "the review revision cannot be computed: " + refErr.Error()}, nil
	}
	// (d) An earlier stage's approval no longer matches.
	for _, up := range planStages[:slices.Index(planStages, stage)] {
		upRef, e := s.ReviewRevision(key, up)
		if e != nil {
			return GraphApproval{Outcome: "blocked", Reason: "the review revision of the " + up + " stage cannot be computed: " + e.Error()}, nil
		}
		if item.Approvals[up] != upRef {
			return GraphApproval{Outcome: "superseded", Reason: "the " + up + " stage approval is no longer current"}, nil
		}
	}
	// (e) Anything else that needs the owner.
	switch {
	case !s.UpstreamApproved(key, stage):
		return GraphApproval{Outcome: "review", Reason: "an upstream story is not approved at this stage"}, nil
	case item.Wait != nil:
		return GraphApproval{Outcome: "review", Reason: "a wait is already open"}, nil
	case len(item.Draft.Questions) > 0:
		return GraphApproval{Outcome: "review", Reason: "the draft has open questions"}, nil
	}
	if err := s.approvalChecks(item); err != nil {
		return GraphApproval{Outcome: "blocked", Reason: err.Error()}, nil
	}
	approve(item, stage, ref, ApprovalByGraph)
	return GraphApproval{Outcome: "approved"}, nil
}

// GraphApprovedStage names one stage of one story whose current approval came
// from the graph.
type GraphApprovedStage struct{ Key, Stage string }

// GraphApprovedStages lists, in story and stage order, the approvals the graph
// gave that are still current.
func (s *PlanState) GraphApprovedStages() []GraphApprovedStage {
	var out []GraphApprovedStage
	for _, item := range s.Stories {
		if !item.Active() {
			continue
		}
		for _, stage := range planStages {
			if item.ApprovedBy[stage] != ApprovalByGraph {
				continue
			}
			if ref, err := s.ReviewRevision(item.Key, stage); err == nil && item.Approvals[stage] == ref {
				out = append(out, GraphApprovedStage{Key: item.Key, Stage: stage})
			}
		}
	}
	return out
}

// ConfirmRetirement validates owner evidence; the activity still owns remote closure.
func (s *PlanState) ConfirmRetirement(r PlanOwnerReply) error {
	item, err := s.story(r.Key)
	if err != nil {
		return err
	}
	w := item.Wait
	if w == nil || w.Kind != "retire" || r.OwnerID < 1 || r.OwnerID != r.ActorID || r.ID < 1 || slices.Contains(s.ConsumedReplies, r.ID) || w.ID != r.WaitID || w.IssueID != r.IssueID || w.Candidate != r.Candidate || w.Stage != r.Stage || item.Candidate != r.Candidate || strings.TrimSpace(commandtext.Normalize(r.Body)) != "/retire-plan "+r.Key+" "+r.Candidate {
		return errors.New("retirement requires exact current owner confirmation")
	}
	ref, err := s.ReviewRevision(item.Key, item.Stage)
	if err != nil {
		return err
	}
	if ref != w.Revision || item.Hold != "" {
		return errors.New("retirement candidate is stale or held")
	}
	return nil
}

// MarkRetired follows confirmed not-planned closure, preserving candidate history.
func (s *PlanState) MarkRetired(key string, replacements []string, replyID int64) error {
	item, err := s.story(key)
	if err != nil {
		return err
	}
	for _, key := range replacements {
		other, err := s.story(key)
		if err != nil || !other.Active() || other.Blocked() || key == item.Key {
			return errors.New("invalid retirement replacement")
		}
	}
	item.Retired = true
	item.Replacements = slices.Clone(replacements)
	item.Wait = nil
	s.ConsumedReplies = append(s.ConsumedReplies, replyID)
	s.Invalidate(key, "technical", "retired dependency requires owner resolution")
	item.Hold = ""
	return nil
}

// PlanSetRevision binds the full active candidate/dependency/shared decision set.
func (s *PlanState) PlanSetRevision() string {
	refs := map[string]string{}
	for _, item := range s.Stories {
		if item.Active() {
			refs[item.Key] = item.Draft.Revision()
		}
	}
	return planHash(struct {
		Shared  string
		Stories map[string]string
	}{s.SharedRevision, refs})
}

// ValidateComplete checks complete progressive content and all active dependencies.
func (s *PlanState) ValidateComplete(scope ContentScope) error {
	active := 0
	for _, item := range s.Stories {
		if item.Blocked() {
			return fmt.Errorf("plan story %s has unresolved work", item.Key)
		}
		if !item.Active() {
			continue
		}
		active++
		if item.Hold != "" || item.Wait != nil || (item.Effect != "" && item.Effect != "applied") || len(item.Draft.Questions) > 0 {
			return fmt.Errorf("plan story %s has unresolved work", item.Key)
		}
		if err := item.Draft.Validate("acceptance", scope); err != nil {
			return fmt.Errorf("plan story %s: %w", item.Key, err)
		}
		for _, stage := range planStages {
			ref, err := s.ReviewRevision(item.Key, stage)
			if err != nil {
				return err
			}
			if item.Approvals[stage] != ref {
				return fmt.Errorf("plan story %s lacks current %s approval", item.Key, stage)
			}
		}
	}
	if active == 0 {
		return errors.New("plan active scope is empty")
	}
	return nil
}

// OpenAssembledReview is still stage three, with no additional workflow stage.
func (s *PlanState) OpenAssembledReview(scope ContentScope) (string, error) {
	if err := s.ValidateComplete(scope); err != nil {
		return "", err
	}
	s.AssembledWait = s.PlanSetRevision()
	return s.AssembledWait, nil
}

// ErrPlanAssembledStale reports a final plan approval that does not name the
// plan set as it stands now, or arrives while no final review is waiting.
var ErrPlanAssembledStale = errors.New("stale or ambiguous assembled Plan approval")

// ApproveAssembled binds authenticated parent approval to the exact displayed set.
func (s *PlanState) ApproveAssembled(ownerID, actorID, replyID int64, body string, scope ContentScope) error {
	if ownerID < 1 || actorID != ownerID || replyID < 1 || slices.Contains(s.ConsumedReplies, replyID) {
		return errors.New("invalid assembled Plan owner evidence")
	}
	if err := s.ValidateComplete(scope); err != nil {
		return err
	}
	ref := s.PlanSetRevision()
	if s.AssembledWait != ref || strings.TrimSpace(commandtext.Normalize(body)) != "/approve-plan-set "+ref {
		return ErrPlanAssembledStale
	}
	s.AssembledApproval = ref
	s.AssembledWait = ""
	s.ConsumedReplies = append(s.ConsumedReplies, replyID)
	return nil
}

// AssemblyDue reports that every active story has all three approvals and the
// assembled review for the plan as it stands now has not been posted or approved
// yet, and names the revision that review would carry. The parent posts it.
func (s *PlanState) AssemblyDue(scope ContentScope) (string, bool) {
	if len(s.Stories) == 0 || s.ValidateComplete(scope) != nil {
		return "", false
	}
	ref := s.PlanSetRevision()
	if s.AssembledWait == ref || s.AssembledApproval == ref {
		return "", false
	}
	return ref, true
}

// PlanReady separates owner approval, confirmed publication and successful save.
func (s *PlanState) PlanReady(scope ContentScope) bool {
	if s.ValidateComplete(scope) != nil {
		return false
	}
	ref := s.PlanSetRevision()
	return s.AssembledApproval == ref && s.PublishedRevision == ref && s.SavedRevision == ref
}

// ValidateRetained validates a merged progressive draft without treating settled
// later-stage content as newly authored by an earlier-stage role. Runtime must
// separately constrain the changed sections to that role's authority.
func (d PlanDraft) ValidateRetained(stage string, scope ContentScope) error {
	if !slices.Contains(planStages, stage) {
		return errors.New("invalid retained Plan stage")
	}
	contentStage := stage
	if len(d.Tasks) > 0 || len(d.Criteria) > 0 {
		contentStage = "acceptance"
	} else if d.Architecture != nil && stage == "story" {
		contentStage = "technical"
	}
	return d.Validate(contentStage, scope)
}

// CompletePlanDrafts adapts complete graph content to unchanged frozen story
// semantics. The graph renderer remains the visible projection owner. Structured
// graph content is retained in workflow snapshots and its neutral details also
// survive the legacy-compatible typed handoff in notes and architecture fields.
func CompletePlanDrafts(summary string, drafts []PlanDraft, scope ContentScope) (TypedPlan, error) {
	out := TypedPlan{Summary: summary, Stories: []TypedStory{}}
	for _, d := range drafts {
		if err := d.Validate("acceptance", scope); err != nil {
			return TypedPlan{}, err
		}
		st := TypedStory{Key: d.Key, Intent: "update", Title: d.Name, UserStory: d.UserStory, AcceptanceCriteria: []CriterionContent{}, Tasks: slices.Clone(d.Tasks), Dependencies: d.Dependencies,
			Architecture: ArchitectureContent{Components: []ComponentContent{}, Interfaces: []InterfaceContent{}, Constraints: []string{}, Decisions: []DecisionContent{}}}
		st.UserStory.Notes = append(slices.Clone(d.UserStory.Notes), "What this story solves: "+d.Problem, "In-Scope: "+strings.Join(d.InScope, "; "), "Not in Scope: "+strings.Join(d.NotInScope, "; "))
		if st.Dependencies.StoryKeys == nil {
			st.Dependencies.StoryKeys = []string{}
		}
		if st.Dependencies.IssueRefs == nil {
			st.Dependencies.IssueRefs = []string{}
		}
		for i := range st.Tasks {
			if st.Tasks[i].DependsOn == nil {
				st.Tasks[i].DependsOn = []string{}
			}
		}
		var general []string
		for _, row := range d.Architecture.General {
			general = append(general, row.Area+": "+row.Decision)
			st.Architecture.Decisions = append(st.Architecture.Decisions, DecisionContent{Decision: row.Area + ": " + row.Decision, Reason: row.Rationale})
		}
		st.Architecture.Summary = strings.Join(general, "\n")
		for _, row := range d.Architecture.DataModel {
			st.Architecture.Constraints = append(st.Architecture.Constraints, "Data Model — "+row.Entity+": "+row.Definition+"; "+row.Constraints)
		}
		for _, row := range d.Architecture.ServiceDesign {
			st.Architecture.Components = append(st.Architecture.Components, ComponentContent{Name: row.Component, Responsibility: row.Responsibility + "; Dependencies / Access: " + row.Access})
		}
		for _, row := range d.Architecture.SolutionDesign {
			st.Architecture.Interfaces = append(st.Architecture.Interfaces, InterfaceContent{Name: row.Element, Input: row.Contract, Output: row.Rationale})
		}
		for _, ref := range d.Design {
			st.Architecture.Constraints = append(st.Architecture.Constraints, "Frontend Design reference: "+ref)
		}
		for _, g := range d.Guidance {
			st.Architecture.Constraints = append(st.Architecture.Constraints, "Selected guidance: "+g)
		}
		for _, a := range d.Assumptions {
			st.Architecture.Constraints = append(st.Architecture.Constraints, "Assumption: "+a)
		}
		for _, decision := range d.Decisions {
			st.UserStory.Notes = append(st.UserStory.Notes, "Settled decision: "+decision)
		}
		for _, c := range d.Criteria {
			st.AcceptanceCriteria = append(st.AcceptanceCriteria, CriterionContent{ID: c.ID, Text: c.Text})
			st.Architecture.Constraints = append(st.Architecture.Constraints, "Proof method "+c.ID+": "+c.ProofMethod)
		}
		out.Stories = append(out.Stories, st)
	}
	if err := out.Validate(scope); err != nil {
		return TypedPlan{}, err
	}
	return out, nil
}

// UpstreamReferences names exact retained candidates consumed by an invocation.
// The selected prior candidate is included so repeated cycles cannot overwrite
// an unseen update; transitive dependencies are bound independently of ordering.
func (s *PlanState) UpstreamReferences(key, stage string) ([]string, error) {
	if !slices.Contains(planStages, stage) {
		return nil, errors.New("invalid Plan upstream stage")
	}
	refs := []string{}
	item, err := s.story(key)
	if err != nil {
		if stage == "story" {
			return refs, nil
		}
		return nil, err
	}
	if _, err = s.ReviewRevision(key, stage); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var visit func(*PlanStoryState) error
	visit = func(current *PlanStoryState) error {
		if seen[current.Key] {
			return nil
		}
		seen[current.Key] = true
		if current.Candidate != "" {
			refs = append(refs, current.Key+":"+current.Candidate)
		}
		for _, key := range current.Draft.Dependencies.StoryKeys {
			dep, err := s.story(key)
			if err != nil {
				return err
			}
			if err = visit(dep); err != nil {
				return err
			}
		}
		return nil
	}
	if err = visit(item); err != nil {
		return nil, err
	}
	sort.Strings(refs)
	return refs, nil
}
