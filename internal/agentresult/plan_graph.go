package agentresult

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

const (
	// MaxPlanStorySetEntries bounds one proposed story set. A parent that needs
	// more is split, not squeezed into a longer list. It is the same bound the
	// graph checker derives the creation loop limit from.
	MaxPlanStorySetEntries = planbrief.MaxPlanStorySetEntries
	// MaxPlanStoryIntentRunes bounds a story-set entry's one-line intent.
	MaxPlanStoryIntentRunes = 280
	// MaxPlanStoryNameRunes bounds a story-set entry's short name.
	MaxPlanStoryNameRunes = 80
)

// PlanStoryRequest is the graph Plan story request an agent authors: one story
// created, updated or retired. The factory-owned members below the authored ones
// are never supplied by an agent — a result that carries any of them is refused
// by name — and are filled in by the factory when the result is accepted, so the
// retained result records what the factory bound rather than what was claimed.
type PlanStoryRequest struct {
	Operation        string              `json:"operation"`
	ExpectedRevision string              `json:"expected_revision,omitempty"`
	Draft            planbrief.PlanDraft `json:"draft"`
	ChangedSections  []string            `json:"changed_sections,omitempty"`
	Replacements     []string            `json:"replacements,omitempty"`

	// Factory-owned. Refused when authored; recorded when bound.
	RequestID      string   `json:"request_id,omitempty"`
	Stage          string   `json:"stage,omitempty"`
	Candidate      string   `json:"candidate,omitempty"`
	Upstream       []string `json:"upstream,omitempty"`
	SharedRevision string   `json:"shared_revision,omitempty"`
}

// planFactoryFields are the request members only the factory may supply.
var planFactoryFields = []string{"request_id", "stage", "candidate", "upstream", "shared_revision"}

// Effect converts the retained request into the request type the story effect
// owner acts on. Every member comes from the retained accepted result; the
// factory-owned members are the ones recorded when the result was accepted.
func (r PlanStoryRequest) Effect() graphexec.PlanStoryRequest {
	upstream := slices.Clone(r.Upstream)
	if upstream == nil {
		upstream = []string{}
	}
	return graphexec.PlanStoryRequest{RequestID: r.RequestID, Operation: r.Operation, Stage: r.Stage, Candidate: r.Candidate, ExpectedRevision: r.ExpectedRevision, Upstream: upstream, SharedRevision: r.SharedRevision, Draft: r.Draft, ChangedSections: slices.Clone(r.ChangedSections), Replacements: slices.Clone(r.Replacements)}
}

// PlanStorySet is a proposed decomposition of a scope into stories. It is shape
// validated here and acted on by nothing: the owner reviews it and only the
// factory-bound owner path promotes anything.
type PlanStorySet struct {
	Entries []PlanStorySetEntry `json:"entries"`
}

// PlanStorySetEntry names one proposed story under a temporary identity that
// exists only within the set. Real story keys are allocated by the factory later.
type PlanStorySetEntry struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Intent    string   `json:"intent"`
	DependsOn []string `json:"depends_on,omitempty"`
}

// PlanGraphBinding holds the values the factory owns for one graph Plan node. It
// is trusted input, resolved afresh for every validation, so preflight and submit
// see the same values for the same state and a changed state refuses old bytes.
type PlanGraphBinding struct {
	// Story is the factory-selected story key the draft must carry.
	Story string
	// Stage is the factory-selected stage: story, technical or acceptance.
	Stage string
	// Candidate names the exact candidate this output would become.
	Candidate string
	// RequestID is the payload identity of the request the factory binds.
	RequestID string
	// Upstream lists the exact upstream references, sorted.
	Upstream []string
	// SharedRevision is the current shared-decision revision.
	SharedRevision string
	// ExistingRevision is the content revision of the story's current issue
	// draft, empty when the story has none. It decides create against update.
	ExistingRevision string
	// Promoted reports that the story comes from a promoted story set and has no
	// draft yet. Its first draft then carries exactly PromotedDependencies: the
	// owner approved that dependency graph and dependency-ordered drafting relies
	// on it, so a draft may neither drop nor add a prerequisite story.
	Promoted             bool
	PromotedDependencies []string
}

// Clone returns a copy sharing no slices.
func (b *PlanGraphBinding) Clone() *PlanGraphBinding {
	if b == nil {
		return nil
	}
	c := *b
	c.Upstream = slices.Clone(b.Upstream)
	c.PromotedDependencies = slices.Clone(b.PromotedDependencies)
	return &c
}

// PlanGraphEditTargets are the extra editable fields of a graph Plan. They are
// deliberately not part of EditTargets("plan"): a legacy Plan run is granted
// exactly what it always was, and a graph Plan run is granted these in addition.
// Each is edited whole; the set's entries have no target of their own because a
// grant that names an absent nested collection cannot be read back as a baseline.
func PlanGraphEditTargets() []Target {
	return []Target{{Field: "payload.plan.storyrequest"}, {Field: "payload.plan.storyset"}}
}

// maximumTargets is every field an edit could ever name for a mode.
func maximumTargets(mode string) []Target {
	targets := EditTargets(mode)
	if mode == "plan" {
		targets = append(targets, PlanGraphEditTargets()...)
	}
	return targets
}

// bindPlanGraph records the factory's values on the request, replacing whatever
// was there. It is the only place those members are ever written.
func bindPlanGraph(r *Result, ctx Context) {
	b := ctx.PlanGraph
	if b == nil || r.Payload.Plan == nil || r.Payload.Plan.StoryRequest == nil {
		return
	}
	q := r.Payload.Plan.StoryRequest
	q.RequestID, q.Stage, q.Candidate = b.RequestID, b.Stage, b.Candidate
	q.Upstream, q.SharedRevision = slices.Clone(b.Upstream), b.SharedRevision
}

// unbindPlanGraph clears the factory-owned members, so an edit is judged on what
// the agent controls and the current binding is applied again afterwards.
func unbindPlanGraph(r *Result) {
	if r.Payload.Plan == nil || r.Payload.Plan.StoryRequest == nil {
		return
	}
	q := r.Payload.Plan.StoryRequest
	q.RequestID, q.Stage, q.Candidate, q.Upstream, q.SharedRevision = "", "", "", nil, ""
}

// validatePlanMode judges a Plan payload under the contract the trusted context
// selects. Both preflight and submit reach it through ValidateResult.
func validatePlanMode(p *Plan, outcome string, questions int, ctx Context, d *Diagnostics) {
	if ctx.PlanGraph == nil {
		base := "$.result.payload.plan"
		if p.StoryRequest != nil {
			add(d, base+".storyrequest", "scope", "legacy Plan payload (summary, context, stories)", "graph Plan story request", "Remove storyrequest: only a graph Plan node returns a story request. Legacy Plan uses summary, context and stories.")
		}
		if p.StorySet != nil {
			add(d, base+".storyset", "scope", "legacy Plan payload (summary, context, stories)", "graph Plan story set", "Remove storyset: only a graph Plan node returns a story set. Legacy Plan uses summary, context and stories.")
		}
		validatePlan(p, outcome, ctx, d)
		return
	}
	validatePlanGraph(p, outcome, questions, ctx, d)
}

// validatePlanGraph enforces the graph Plan branch contract: done carries exactly
// one of a story request and a story set and no legacy planning field; ask carries
// neither, authorizes no story effect and carries at least one shared question, the
// same rule DecodePlanNode applies to the terminal envelope.
func validatePlanGraph(p *Plan, outcome string, questions int, ctx Context, d *Diagnostics) {
	base := "$.result.payload.plan"
	if p.Summary != "" {
		add(d, base+".summary", "legacy_field", "graph Plan without legacy planning fields", "summary present", "Remove summary: a graph Plan answers with a story request or a story set, not a planning summary.")
	}
	if p.Context != nil {
		add(d, base+".context", "legacy_field", "graph Plan without legacy planning fields", "context present", "Remove context: put questions in result.questions and decisions in the story draft.")
	}
	if len(p.Stories) > 0 {
		add(d, base+".stories", "legacy_field", "graph Plan without legacy planning fields", "stories present", "Remove stories: propose one story with storyrequest, or a decomposition with storyset.")
	}
	switch {
	case outcome != "completed":
		// An ask works with no draft at all, and only a completed result may name
		// a story effect: an unfinished one authorizes none.
		if p.StoryRequest != nil {
			add(d, base+".storyrequest", "branch", "no story branch on an unfinished result", "storyrequest present", "Remove storyrequest: an ask carries its questions in result.questions and no story request.")
		}
		if p.StorySet != nil {
			add(d, base+".storyset", "branch", "no story branch on an unfinished result", "storyset present", "Remove storyset: an ask carries its questions in result.questions and no story set.")
		}
		if questions == 0 {
			add(d, "$.result.questions", "questions", "at least one shared question on an unfinished graph Plan result", "no questions", "Add the owner question in result.questions, or finish with a completed result carrying one storyrequest or storyset.")
		}
		return
	case p.StoryRequest == nil && p.StorySet == nil:
		add(d, base, "branch", "exactly one of storyrequest or storyset", "neither", "Add storyrequest to create, update or retire one story, or storyset to propose several; or ask a question instead of finishing.")
	case p.StoryRequest != nil && p.StorySet != nil:
		add(d, base, "branch", "exactly one of storyrequest or storyset", "both", "Return one branch: remove storyrequest or remove storyset.")
	}
	if p.StoryRequest != nil {
		validatePlanStoryRequest(p.StoryRequest, ctx, d)
	}
	if p.StorySet != nil {
		validatePlanStorySet(p.StorySet, d)
	}
}

// validatePlanStoryRequest applies the story, stage, candidate, upstream,
// shared-revision and section rules against the current binding.
func validatePlanStoryRequest(r *PlanStoryRequest, ctx Context, d *Diagnostics) {
	base := "$.result.payload.plan.storyrequest"
	b := ctx.PlanGraph
	// A recorded factory value is one the factory wrote at acceptance. It must
	// still be the binding's: if the state moved, the retained result is stale.
	stale := func(field string, matches bool) {
		if !matches {
			add(d, base+"."+field, "stale_binding", "the value the factory currently binds", "different recorded value", "The factory owns "+field+" and it has changed since this result was recorded; submit the result again so it is bound afresh.")
		}
	}
	if r.RequestID != "" {
		stale("request_id", r.RequestID == b.RequestID)
	}
	if r.Stage != "" {
		stale("stage", r.Stage == b.Stage)
	}
	if r.Candidate != "" {
		stale("candidate", r.Candidate == b.Candidate)
	}
	if r.SharedRevision != "" {
		stale("shared_revision", r.SharedRevision == b.SharedRevision)
	}
	if len(r.Upstream) > 0 {
		recorded := slices.Clone(r.Upstream)
		slices.Sort(recorded)
		bound := slices.Clone(b.Upstream)
		slices.Sort(bound)
		stale("upstream", slices.Equal(recorded, bound))
	}
	// Shape rules run over the request as the effect owner will see it: authored
	// members plus the factory's binding, so one function judges both.
	full := graphexec.PlanStoryRequest{RequestID: b.RequestID, Operation: r.Operation, Stage: b.Stage, Candidate: b.Candidate, ExpectedRevision: r.ExpectedRevision, Upstream: slices.Clone(b.Upstream), SharedRevision: b.SharedRevision, Draft: r.Draft, ChangedSections: r.ChangedSections, Replacements: r.Replacements}
	for _, fault := range full.Faults() {
		add(d, base+"."+fault.Field, "story_request", "valid story request for the factory-bound stage", "refused", fault.Message+".")
	}
	if r.Draft.Key != b.Story {
		add(d, base+".draft.key", "binding", fmt.Sprintf("factory-selected story %q", b.Story), "different story", fmt.Sprintf("Set draft.key to %q: the factory selects the story and the agent cannot change it.", b.Story))
	}
	switch r.Operation {
	case "create":
		if b.ExistingRevision != "" {
			add(d, base+".operation", "existing_story", "update or retire", "create", fmt.Sprintf("Story %q already has an issue draft: use operation update, or retire.", b.Story))
		}
		if b.Promoted {
			got, want := slices.Clone(r.Draft.Dependencies.StoryKeys), slices.Clone(b.PromotedDependencies)
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(slices.Compact(got), want) {
				add(d, base+".draft.dependencies.story_keys", "promoted_dependencies", fmt.Sprintf("exactly the promoted story dependencies %v", b.PromotedDependencies), fmt.Sprintf("%v", r.Draft.Dependencies.StoryKeys), fmt.Sprintf("Set draft.dependencies.story_keys to %s: the owner promoted this story with those prerequisites and only the story set changes them.", jsonKeys(b.PromotedDependencies)))
			}
		}
	case "update", "retire":
		if b.ExistingRevision == "" {
			add(d, base+".operation", "missing_story", "create", r.Operation, fmt.Sprintf("Story %q has no issue draft to change: use operation create at the story stage.", b.Story))
		} else if len(r.ExpectedRevision) == 64 && r.ExpectedRevision != b.ExistingRevision {
			add(d, base+".expected_revision", "stale_revision", "the current story revision", "different revision", "Set expected_revision to the current revision named in the factory story binding; the story changed since it was read.")
		}
	}
	for _, fault := range graphexec.PlanSectionFaults(b.Stage, r.ChangedSections) {
		add(d, base+"."+fault.Field, "section", "sections the stage may author", "unauthorized section", fault.Message+".")
	}
	if slices.Contains([]string{"create", "update", "retire"}, r.Operation) {
		var err error
		if r.Operation == "create" {
			err = r.Draft.Validate(b.Stage, ctx.PlanScope)
		} else {
			err = r.Draft.ValidateRetained(b.Stage, ctx.PlanScope)
		}
		if err != nil {
			for _, fault := range splitErrors(err) {
				add(d, base+".draft", "draft", "valid draft for the stage", "invalid draft", fault.Error()+".")
			}
		}
	}
}

func jsonKeys(keys []string) string {
	raw, err := json.Marshal(append([]string{}, keys...))
	if err != nil {
		return "the promoted keys"
	}
	return string(raw)
}

func splitErrors(err error) []error {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		return joined.Unwrap()
	}
	return []error{err}
}

// validatePlanStorySet applies the story-set rules. Identity faults are reported
// first and end the pass before any dependency is resolved, because a dependency
// cannot be resolved against identities that are empty or ambiguous.
func validatePlanStorySet(set *PlanStorySet, d *Diagnostics) {
	base := "$.result.payload.plan.storyset.entries"
	n := len(set.Entries)
	if n == 0 {
		add(d, base, "limit", "1 to 12 entries", "empty", "Propose at least one story, or ask a question instead.")
		return
	}
	if n > MaxPlanStorySetEntries {
		add(d, base, "limit", "1 to 12 entries", fmt.Sprintf("%d entries", n), fmt.Sprintf("Propose at most %d stories: this parent is too large for one plan, so split the parent into smaller parents.", MaxPlanStorySetEntries))
		return
	}
	identityFault := false
	first := map[string]int{}
	names := map[string]int{}
	for i, e := range set.Entries {
		p := fmt.Sprintf("%s[%d]", base, i)
		switch {
		case strings.TrimSpace(e.ID) == "":
			add(d, p+".id", "blank", "nonempty temporary id", "empty", "Give the entry a temporary id such as s1; it names the entry inside this set only.")
			identityFault = true
		case !ValidID(e.ID):
			add(d, p+".id", "identity", "stable identifier", "invalid identifier", "Use a letter followed by letters, digits, underscores or hyphens, up to 128 bytes.")
			identityFault = true
		default:
			if at, seen := first[e.ID]; seen {
				add(d, p+".id", "duplicate", "unique temporary id", fmt.Sprintf("same as entries[%d]", at), fmt.Sprintf("Give this entry an id not used by entries[%d].", at))
				identityFault = true
			} else {
				first[e.ID] = i
			}
		}
		name := strings.TrimSpace(e.Name)
		switch {
		case name == "" || strings.ContainsAny(e.Name, "\r\n") || utf8.RuneCountInString(name) > MaxPlanStoryNameRunes:
			add(d, p+".name", "name", fmt.Sprintf("nonblank single-line name of at most %d characters", MaxPlanStoryNameRunes), "invalid name", "Give the entry a short single-line name.")
		default:
			key := strings.ToLower(name)
			if at, seen := names[key]; seen {
				add(d, p+".name", "duplicate", "unique name", fmt.Sprintf("same as entries[%d]", at), fmt.Sprintf("Give this entry a name that differs from entries[%d].", at))
			} else {
				names[key] = i
			}
		}
		intent := strings.TrimSpace(e.Intent)
		if intent == "" || strings.ContainsAny(e.Intent, "\r\n") || utf8.RuneCountInString(intent) > MaxPlanStoryIntentRunes {
			add(d, p+".intent", "intent", fmt.Sprintf("nonblank one-line intent of at most %d characters", MaxPlanStoryIntentRunes), "invalid intent", "State the entry's intent in one line of at most 280 characters.")
		}
	}
	if identityFault {
		return
	}
	pending := make([]int, n)
	edges := make([][]int, n)
	for i, e := range set.Entries {
		p := fmt.Sprintf("%s[%d]", base, i)
		listed := map[string]bool{}
		for j, dep := range e.DependsOn {
			dp := fmt.Sprintf("%s.depends_on[%d]", p, j)
			at, exists := first[dep]
			switch {
			case dep == e.ID:
				add(d, dp, "self_reference", "another entry", "itself", "An entry cannot depend on itself; remove it.")
			case !exists:
				add(d, dp, "reference", "an id in this set", "unknown id", "Depend only on the temporary id of another entry in this set.")
			case listed[dep]:
				add(d, dp, "duplicate", "unique dependencies", "duplicate reference", "List each dependency once.")
			default:
				edges[at] = append(edges[at], i)
				pending[i]++
			}
			listed[dep] = true
		}
	}
	// Whatever cannot be ordered after removing every entry with no unmet
	// dependency lies on, or behind, a cycle.
	ready := []int{}
	for i := range pending {
		if pending[i] == 0 {
			ready = append(ready, i)
		}
	}
	resolved := 0
	for len(ready) > 0 {
		i := ready[0]
		ready = ready[1:]
		resolved++
		for _, next := range edges[i] {
			if pending[next]--; pending[next] == 0 {
				ready = append(ready, next)
			}
		}
	}
	if resolved < n {
		for i, left := range pending {
			if left > 0 {
				add(d, fmt.Sprintf("%s[%d].depends_on", base, i), "cycle", "acyclic dependencies", "cycle", "This entry is on or behind a dependency cycle; remove a dependency so the set can be ordered.")
			}
		}
	}
}

// SameResult reports whether a terminal envelope's result states the same
// application content as the retained accepted result. It is the consistency
// check of the terminal route: the terminal result is compared against the
// retained one and is never a source for it. The factory-bound members of a graph
// Plan story request are excluded, because an agent never supplies them and so
// its terminal result cannot carry them.
func SameResult(retained, terminal Result) bool {
	left, right := comparisonBytes(retained), comparisonBytes(terminal)
	return left != nil && right != nil && bytes.Equal(left, right)
}

func comparisonBytes(r Result) []byte {
	cloned, err := Clone(r)
	if err != nil {
		return nil
	}
	unbindPlanGraph(&cloned)
	data, err := json.Marshal(cloned)
	if err != nil {
		return nil
	}
	return data
}
