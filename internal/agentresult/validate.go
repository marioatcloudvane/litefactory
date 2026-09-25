package agentresult

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

var stableID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,127}$`)

// ValidID checks collection-local stable identity syntax.
func ValidID(id string) bool { return stableID.MatchString(id) }

// ValidPath checks syntax; the factory must additionally enforce rooted access.
func ValidPath(p string) bool {
	return p != "" && p != "." && !strings.ContainsAny(p, "\\\x00") && !strings.HasPrefix(p, "/") && path.Clean(p) == p && p != ".." && !strings.HasPrefix(p, "../") && !strings.Contains(p, ":")
}
func required(d *Diagnostics, p, v string) {
	if strings.TrimSpace(v) == "" {
		add(d, p, "blank", "nonblank string", "blank", "Supply meaningful content.")
	}
}
func identity(d *Diagnostics, p, id string, seen map[string]bool) {
	if !ValidID(id) {
		add(d, p, "identity", "stable identifier", "invalid identifier", "Use a letter followed by letters, digits, underscores or hyphens, up to 128 bytes.")
	}
	if seen[id] {
		add(d, p, "duplicate", "unused identifier", "duplicate identifier", "Use collection-local unique identities.")
	}
	seen[id] = true
}
func mergeDiagnostics(d *Diagnostics, other Diagnostics, prefix string) {
	for _, v := range other.Diagnostics {
		v.Path = prefix + strings.TrimPrefix(v.Path, "$")
		d.Diagnostics = append(d.Diagnostics, v)
	}
	if !other.Complete {
		d.Complete = false
	}
	for _, p := range other.UncheckedPaths {
		d.UncheckedPaths = append(d.UncheckedPaths, prefix+strings.TrimPrefix(p, "$"))
	}
}

// ValidateResult checks the entire result, including unchanged siblings. It never
// accepts authored execution facts or performs evidence capture or I/O.
func ValidateResult(r Result, ctx Context) Diagnostics {
	d := newDiagnostics()
	if ctx.SchemaVersion != SchemaVersion {
		add(&d, "$", "version", "application schema 1", "unsupported version", "Use an explicitly supported schema without implicit migration.")
	}
	if ctx.WorkflowVersion != 1 && ctx.WorkflowVersion != 2 {
		add(&d, "$", "scope", "workflow 1 or 2", "unsupported workflow", "Supply the persisted workflow version.")
	}
	required(&d, "$.result.comment", r.Comment)
	switch r.Outcome {
	case "completed":
		if r.Reason != "" {
			add(&d, "$.result.reason", "forbidden", "absent reason", "present reason", "Omit reason for completed outcomes.")
		}
	case "incomplete", "blocked":
		required(&d, "$.result.reason", r.Reason)
	default:
		add(&d, "$.result.outcome", "enum", "completed, incomplete or blocked", "unsupported outcome", "Select a documented outcome.")
	}
	if r.Recommendation != nil {
		required(&d, "$.result.recommendation.text", r.Recommendation.Text)
		if r.Recommendation.Action != "" && r.Recommendation.Action != "action.moveto.planmode" {
			add(&d, "$.result.recommendation.action", "enum", "action.moveto.planmode or omitted", "unsupported action", "Omit an unsupported advisory action.")
		}
	}
	qs := map[string]bool{}
	for i, q := range r.Questions {
		p := fmt.Sprintf("$.result.questions[%d]", i)
		identity(&d, p+".id", q.ID, qs)
		required(&d, p+".text", q.Text)
		if !slices.Contains(QuestionAudiences, q.Audience) {
			add(&d, p+".audience", "enum", "business or technical", "unsupported audience", "Name the role that can settle this question.")
		}
		opts := map[string]bool{}
		for j, o := range q.Options {
			op := fmt.Sprintf("%s.options[%d]", p, j)
			identity(&d, op+".id", o.ID, opts)
			required(&d, op+".text", o.Text)
		}
		if q.RecommendedOption != "" && !opts[q.RecommendedOption] {
			add(&d, p+".recommended_option", "reference", "existing option ID", "unresolved", "Reference a supplied option.")
		}
	}
	if len(r.Questions) > 0 && (!ctx.QuestionsAllowed || (ctx.Mode != "research" && ctx.Mode != "plan") || r.Outcome == "completed") {
		add(&d, "$.result.questions", "authorization", "permitted questions on unfinished Research or Plan", "disallowed questions", "Follow trusted question permission and report an unfinished outcome.")
	}
	arts := map[string]bool{}
	for i, a := range r.Artifacts {
		p := fmt.Sprintf("$.result.artifacts[%d]", i)
		identity(&d, p+".id", a.ID, arts)
		required(&d, p+".kind", a.Kind)
		if ctx.Mode == "research" && a.Kind != "" && !slices.Contains(ResearchArtifactKinds, a.Kind) {
			add(&d, p+".kind", "enum", "brief, technical or data", "unsupported artifact kind", "Name the document this is: the owner's brief, its technical evidence, or supporting data.")
		}
		if !ValidPath(a.Path) {
			add(&d, p+".path", "path", "rooted relative path syntax", "unsafe path", "Use a clean relative path within the authorized root.")
		}
	}
	modes := []struct {
		name    string
		present bool
	}{{"research", r.Payload.Research != nil}, {"plan", r.Payload.Plan != nil}, {"implement", r.Payload.Implement != nil}, {"validate", r.Payload.Validate != nil}, {"spike", r.Payload.Spike != nil}}
	count := 0
	matching := false
	for _, m := range modes {
		if m.present {
			count++
			matching = matching || m.name == ctx.Mode
		}
	}
	if count != 1 || !matching {
		add(&d, "$.result.payload", "mode", "exactly one payload matching trusted mode", "mismatched payload", "Use only the factory-selected mode payload.")
	}
	if p := r.Payload.Research; p != nil {
		required(&d, "$.result.payload.research.request", p.Request)
		seen := map[string]bool{}
		for i, f := range p.Findings {
			fp := fmt.Sprintf("$.result.payload.research.findings[%d]", i)
			identity(&d, fp+".id", f.ID, seen)
			required(&d, fp+".text", f.Text)
			for j, e := range f.Evidence {
				if !ValidPath(e.Path) || e.Line < 0 {
					add(&d, fmt.Sprintf("%s.evidence[%d]", fp, j), "reference", "relative path and nonnegative line", "invalid reference", "Supply a rooted source reference.")
				}
			}
		}
		for name, items := range map[string][]Statement{"decisions": p.Decisions, "assumptions": p.Assumptions} {
			ids := map[string]bool{}
			for i, s := range items {
				sp := fmt.Sprintf("$.result.payload.research.%s[%d]", name, i)
				identity(&d, sp+".id", s.ID, ids)
				required(&d, sp+".text", s.Text)
			}
		}
	}
	if p := r.Payload.Spike; p != nil {
		required(&d, "$.result.payload.spike.request", p.Request)
		for name, items := range map[string][]string{"work_done": p.WorkDone, "findings": p.Findings, "decisions": p.Decisions, "not_done": p.NotDone} {
			for i, v := range items {
				required(&d, fmt.Sprintf("$.result.payload.spike.%s[%d]", name, i), v)
			}
		}
	}
	if p := r.Payload.Plan; p != nil {
		validatePlanMode(p, r.Outcome, len(r.Questions), ctx, &d)
	}
	if p := r.Payload.Implement; p != nil {
		validateImplement(p, r.Outcome, ctx, &d)
	}
	if p := r.Payload.Validate; p != nil {
		validateAssessments(p, r.Outcome, ctx, &d)
	}
	finish(&d)
	return d
}
func contribution(c agent.Contribution, ctx Context, p string, d *Diagnostics) {
	b, err := json.Marshal(c)
	if err != nil {
		add(d, p, "encoding", "serializable contribution", "invalid", "Correct the contribution.")
		return
	}
	_, cd := agent.ParseContribution(b, agent.ContributionScope{ContractVersion: agent.SubmissionContractVersion, WorkflowVersion: ctx.WorkflowVersion, Stage: c.Stage, Stories: ctx.Stories})
	mergeDiagnostics(d, cd, p)
}
func validateImplement(p *Implement, outcome string, ctx Context, d *Diagnostics) {
	seen := map[string]bool{}
	for i, s := range p.Stories {
		sp := fmt.Sprintf("$.result.payload.implement.stories[%d]", i)
		identity(d, sp+".story", s.Story, seen)
		stage := "engineering"
		if ctx.WorkflowVersion == 1 {
			stage = "legacy"
		}
		contribution(agent.Contribution{Stage: stage, Story: s.Story, Outcome: s.Outcome, Reason: s.Reason, Engineering: s.Engineering, Assessment: s.Assessment}, ctx, sp, d)
		if outcome == "completed" && s.Outcome != "engineering_complete" && s.Outcome != "verified" {
			add(d, sp+".outcome", "incomplete", "completed engineering", "unfinished story", "Complete selected engineering before claiming completion.")
		}
	}
	if outcome == "completed" {
		if len(ctx.Stories) == 0 {
			add(d, "$.result.payload.implement.stories", "scope", "selected frozen stories", "empty scope", "Supply authorized obligations.")
		}
		for key := range ctx.Stories {
			if !seen[key] && !ctx.EngineeringComplete[key] {
				add(d, "$.result.payload.implement.stories", "coverage", "all selected engineering obligations", "missing story", "Reference selected work or supply retained completion through trusted context.")
			}
		}
	}
	if p.AggregateChecks != nil {
		if ctx.AggregateValidator == nil {
			add(d, "$.result.payload.implement.aggregate_checks", "missing_evidence", "trusted aggregate resolver", "unavailable", "Resolve retained aggregate facts before claiming proof.")
			d.Complete = false
			d.UncheckedPaths = append(d.UncheckedPaths, "$.result.payload.implement.aggregate_checks")
		} else {
			reference := *p.AggregateChecks
			reference.CheckIDs = slices.Clone(reference.CheckIDs)
			mergeDiagnostics(d, ctx.AggregateValidator(reference), "$.result.payload.implement.aggregate_checks")
		}
	} else if outcome == "completed" && ctx.RequireAggregate {
		add(d, "$.result.payload.implement.aggregate_checks", "required", "retained aggregate proof", "absent", "Reference final aggregate checks.")
	}
}
func validateAssessments(p *Validate, outcome string, ctx Context, d *Diagnostics) {
	seen := map[string]bool{}
	for i, s := range p.Stories {
		sp := fmt.Sprintf("$.result.payload.validate.stories[%d]", i)
		identity(d, sp+".story", s.Story, seen)
		contribution(agent.Contribution{Stage: "validate", Story: s.Story, Outcome: "assessed", Assessment: &agent.AssessmentContribution{SnapshotID: s.SnapshotID, Criteria: s.Criteria, Reviews: s.Reviews}}, ctx, sp, d)
	}
	if outcome == "completed" {
		if len(ctx.Stories) == 0 {
			add(d, "$.result.payload.validate.stories", "scope", "authorized assessments", "empty scope", "Supply selected frozen stories.")
		}
		for key := range ctx.Stories {
			if !seen[key] {
				add(d, "$.result.payload.validate.stories", "coverage", "complete selected assessments", "missing story", "Assess all selected stories, preserving unmet and unverified findings.")
			}
		}
	}
}
func validatePlan(p *Plan, outcome string, ctx Context, d *Diagnostics) {
	base := "$.result.payload.plan"
	graph := map[string][]string{}
	seen := map[string]bool{}
	for i, s := range p.Stories {
		sp := fmt.Sprintf("%s.stories[%d]", base, i)
		identity(d, sp+".key", s.Key, seen)
		required(d, sp+".title", s.Title)
		required(d, sp+".user_story.want", s.UserStory.Want)
		graph[s.Key] = nil
		if s.Dependencies != nil {
			graph[s.Key] = s.Dependencies.StoryKeys
			for _, ref := range s.Dependencies.IssueRefs {
				if !slices.Contains(ctx.PlanScope.IssueRefs, ref) {
					add(d, sp+".dependencies.issue_refs", "authorization", "resolved external references", "unknown reference", "Use factory-authorized external dependencies.")
				}
			}
		}
		criteria := map[string]bool{}
		for j, c := range s.AcceptanceCriteria {
			cp := fmt.Sprintf("%s.acceptance_criteria[%d]", sp, j)
			identity(d, cp+".id", c.ID, criteria)
			if !planbrief.ValidCriterionID(c.ID) {
				add(d, cp+".id", "identity", "canonical criterion ID", "invalid ID", "Use AC-001 style positive IDs.")
			}
			required(d, cp+".text", c.Text)
		}
		tasks := map[string][]string{}
		ids := map[string]bool{}
		for j, t := range s.Tasks {
			tp := fmt.Sprintf("%s.tasks[%d]", sp, j)
			identity(d, tp+".id", t.ID, ids)
			required(d, tp+".description", t.Description)
			if !slices.Contains(ctx.PlanScope.Roles, t.OwnerRole) {
				add(d, tp+".owner_role", "authorization", "rostered role", "unknown role", "Assign a supplied role.")
			}
			for _, id := range t.CriterionIDs {
				if !criteria[id] {
					add(d, tp+".criterion_ids", "reference", "supplied criterion", "unresolved", "Supply referenced criteria in the same story.")
				}
			}
			tasks[t.ID] = t.DependsOn
		}
		checkGraph(tasks, sp+".tasks", d)
	}
	checkGraph(graph, base+".stories", d)
	if outcome == "completed" {
		typed := planbrief.TypedPlan{Summary: p.Summary}
		for _, s := range p.Stories {
			t := planbrief.TypedStory{Key: s.Key, Intent: s.Intent, Title: s.Title, UserStory: planbrief.UserStoryContent{Actor: s.UserStory.Actor, Want: s.UserStory.Want, Benefit: s.UserStory.Benefit, Notes: s.UserStory.Notes}, AcceptanceCriteria: s.AcceptanceCriteria, Tasks: s.Tasks}
			// Notes are optional in the application contract; the legacy complete
			// typed validator expects an explicit empty collection.
			if t.UserStory.Notes == nil {
				t.UserStory.Notes = []string{}
			}
			if s.Architecture != nil {
				t.Architecture = *s.Architecture
			}
			if s.Dependencies != nil {
				t.Dependencies = *s.Dependencies
			}
			typed.Stories = append(typed.Stories, t)
		}
		if err := typed.Validate(ctx.PlanScope); err != nil {
			add(d, base, "completeness", "complete typed Plan with valid roles and graphs", "invalid complete Plan", "Supply complete summary, stories, criteria, tasks, architecture and dependency sections.")
		}
	}
}
func checkGraph(graph map[string][]string, p string, d *Diagnostics) {
	state := map[string]int{}
	var visit func(string)
	visit = func(key string) {
		if state[key] == 1 {
			add(d, p, "cycle", "acyclic dependencies", "cycle", "Remove circular dependencies.")
			return
		}
		if state[key] == 2 {
			return
		}
		state[key] = 1
		refs := map[string]bool{}
		for _, dep := range graph[key] {
			if refs[dep] {
				add(d, p, "duplicate", "unique dependencies", "duplicate reference", "Reference each dependency once.")
			}
			refs[dep] = true
			if _, ok := graph[dep]; !ok {
				add(d, p, "reference", "supplied dependency", "unresolved reference", "Supply each dependency target.")
			} else {
				visit(dep)
			}
		}
		state[key] = 2
	}
	keys := make([]string, 0, len(graph))
	for key := range graph {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		visit(key)
	}
}
