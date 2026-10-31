package taskdiscussion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/skillclosure"
	"github.com/cloudvane/litefactory/internal/store"
)

// StoreCapture resolves only current saved analysis and pins its owned resources.
// Guidance captures the dedicated advisory contract; the analytical bytes are evidence.
type StoreCapture struct {
	DB       *store.DB
	Git      *git.Binary
	Guidance func(context.Context, string, string) (json.RawMessage, error)
}

// Capture freezes the current saved target, committed basis and resource closure.
func (c StoreCapture) Capture(ctx context.Context, task, operation string) (Basis, error) {
	if c.DB == nil || c.Guidance == nil || (operation != "ask" && operation != "judge") {
		return Basis{}, ErrTargetUnavailable
	}
	snapshot, err := c.DB.ReadTaskAdvisorySnapshot(ctx, task)
	if err != nil {
		return Basis{}, errors.Join(ErrTargetUnavailable, err)
	}
	var files map[string][]byte
	if err = json.Unmarshal(snapshot.Files, &files); err != nil {
		return Basis{}, errors.Join(ErrTargetUnavailable, err)
	}
	profile, err := graphexec.ParseProfile(files[path.Base(snapshot.Definition.Path)])
	if err != nil {
		return Basis{}, errors.Join(ErrTargetUnavailable, err)
	}
	node, stage, evidence, sources, err := selectAdvisoryTarget(snapshot, profile)
	if err != nil {
		return Basis{}, err
	}
	role := node.Attrs["lf_agent_markdown"]
	if operation == "judge" && !eligibleAdvisoryJudgment(evidence, role) {
		return Basis{}, ErrTargetUnavailable
	}
	rawRole, ok := files[role+".md"]
	if !ok || len(rawRole) == 0 {
		return Basis{}, ErrTargetUnavailable
	}
	_, resources, err := skillclosure.Load(splitSkills(node.Attrs["lf_skills"]), func(name string) ([]byte, error) {
		raw, found := files[name]
		if !found {
			return nil, ErrTargetUnavailable
		}
		return raw, nil
	})
	if err != nil {
		return Basis{}, errors.Join(ErrTargetUnavailable, err)
	}
	resources[role+".md"] = rawRole
	// String-valued entries keep the captured analytical evidence readable to the model.
	analytical := make(map[string]string, len(resources))
	for name, raw := range resources {
		analytical[name] = string(raw)
	}
	analysis, _ := json.Marshal(analytical)
	repositoryPath := snapshot.Session.WorktreePath
	if repositoryPath == "" {
		repositoryPath = snapshot.Repository.Path
	}
	b := Basis{StoreGuard: snapshot.Guard, TaskRef: task, RepositoryRef: snapshot.Repository.Forge + ":" + snapshot.Repository.Owner + "/" + snapshot.Repository.Name, Stage: stage, Role: role, Evidence: evidence, Sources: sources, Gaps: []string{}}
	if c.Git != nil && repositoryPath != "" {
		clone, e := c.Git.Open(ctx, repositoryPath)
		if e == nil {
			b.Commit, e = clone.RevParse(ctx, "HEAD", git.RevParseOptions{})
		}
		if e != nil {
			if ctx.Err() != nil {
				return Basis{}, ctx.Err()
			}
			b.Gaps = append(b.Gaps, "The current committed repository basis could not be read.")
			repositoryPath = ""
		}
	} else {
		b.Gaps = append(b.Gaps, "No committed repository checkout was available.")
		repositoryPath = ""
	}
	b.Guidance, err = c.Guidance(ctx, string(analysis), repositoryPath)
	if err != nil || !json.Valid(b.Guidance) {
		return Basis{}, errors.Join(ErrTargetUnavailable, err)
	}
	if len(evidence) > 4<<20 || len(b.Guidance) > 16<<20 {
		return Basis{}, store.ErrConnectorCapacity
	}
	// Exclude mutable bookkeeping timestamps from freshness; pin the exact current
	// graph, wait, accepted source, Plan state, resource bytes and committed basis.
	b.Revision = hash(struct {
		Instance     store.GraphInstance
		Definition   store.GraphDefinition
		Wait         *store.GraphOwnerWait
		PlanRevision int64
		Evidence     json.RawMessage
		Resources    map[string]string
		Commit       string
	}{snapshot.Instance, snapshot.Definition, snapshot.Wait, snapshot.PlanRevision, evidence, analytical, b.Commit})
	fresh, err := c.DB.ReadTaskAdvisorySnapshot(ctx, task)
	if err != nil || fresh.Guard != snapshot.Guard {
		return Basis{}, errors.Join(ErrBasisChanged, err)
	}
	return b, nil
}
func splitSkills(raw string) []string {
	var out []string
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func selectAdvisoryTarget(s store.TaskAdvisorySnapshot, p *graphexec.Profile) (graphexec.Node, string, json.RawMessage, []string, error) {
	if p == nil {
		return graphexec.Node{}, "", nil, nil, ErrTargetUnavailable
	}
	workflow := s.Definition.Workflow
	if workflow == "" {
		workflow = s.Definition.Mode
	}
	if workflow == "plan" {
		return selectPlanAdvisoryTarget(s, p)
	}
	if workflow != "research" && workflow != "bug" {
		return graphexec.Node{}, "", nil, nil, ErrTargetUnavailable
	}
	nodeName := s.Instance.CurrentNode
	// Bug's owner-conversation gate can follow either analyst or fixer. Only a
	// latest analyst source is eligible; a later repair cannot resurrect it.
	if workflow == "bug" && nodeName == "conversation" {
		newest := int64(0)
		for _, output := range s.Outputs {
			candidate, ok := p.Node(output.Node)
			if ok && candidate.Action == "lf_agent" && output.AttemptID > newest {
				newest = output.AttemptID
				nodeName = candidate.Name
			}
		}
		selected, ok := p.Node(nodeName)
		if !ok || selected.Attrs["lf_agent_markdown"] != "bug-analyst" {
			return graphexec.Node{}, "", nil, nil, ErrTargetUnavailable
		}
	}
	if workflow != "bug" && s.Wait != nil && s.Wait.SourceAttemptID > 0 {
		found := false
		for _, o := range s.Outputs {
			if o.AttemptID == s.Wait.SourceAttemptID {
				nodeName = o.Node
				found = true
				break
			}
		}
		if !found {
			return graphexec.Node{}, "", nil, nil, ErrTargetUnavailable
		}
	}
	node, err := currentAnalysisNode(p, nodeName, map[string]bool{})
	if err != nil {
		return graphexec.Node{}, "", nil, nil, err
	}
	role := node.Attrs["lf_agent_markdown"]
	stage := ""
	switch workflow {
	case "research":
		switch role {
		case "research":
			stage = "investigation"
		case "shaping":
			stage = "shaping"
		default:
			return graphexec.Node{}, "", nil, nil, ErrTargetUnavailable
		}
	case "bug":
		if role != "bug-analyst" {
			return graphexec.Node{}, "", nil, nil, ErrTargetUnavailable
		}
		stage = "analysis"
	}
	for _, o := range s.Outputs {
		if o.Node != node.Name {
			continue
		}
		if o.Status != store.GraphSucceeded || o.OutputID == 0 || !json.Valid(o.Payload) {
			return graphexec.Node{}, "", nil, nil, ErrTargetUnavailable
		}
		// A Bug repair projection must never become an analyst answer source.
		if workflow == "bug" {
			var envelope struct {
				Result struct {
					Payload struct {
						Bug struct {
							Analysis json.RawMessage `json:"analysis"`
						} `json:"bug"`
					} `json:"payload"`
				} `json:"result"`
			}
			raw := o.Payload
			var retained struct {
				Node json.RawMessage `json:"node"`
			}
			if json.Unmarshal(raw, &retained) == nil && len(retained.Node) > 0 {
				raw = retained.Node
			}
			if json.Unmarshal(raw, &envelope) != nil || len(envelope.Result.Payload.Bug.Analysis) == 0 {
				return graphexec.Node{}, "", nil, nil, ErrTargetUnavailable
			}
		}
		evidence, _ := json.Marshal(struct {
			InstanceID   int64                      `json:"instance_id"`
			DefinitionID int64                      `json:"definition_id"`
			Source       store.AdvisorySourceOutput `json:"source"`
		}{s.Instance.ID, s.Definition.ID, o})
		return node, stage, evidence, []string{fmt.Sprintf("Saved %s analysis, graph %d, attempt %d, output %d", workflow, s.Instance.ID, o.AttemptID, o.OutputID)}, nil
	}
	return graphexec.Node{}, "", nil, nil, ErrTargetUnavailable
}

// currentAnalysisNode follows only an explicit captured source binding. A
// mechanical node without one cannot select a past analytical stage.
func currentAnalysisNode(p *graphexec.Profile, name string, seen map[string]bool) (graphexec.Node, error) {
	if seen[name] {
		return graphexec.Node{}, ErrTargetUnavailable
	}
	seen[name] = true
	node, ok := p.Node(name)
	if !ok {
		return graphexec.Node{}, ErrTargetUnavailable
	}
	if node.Action == "lf_agent" {
		return node, nil
	}
	if source := node.Attrs["lf_source"]; source != "" {
		return currentAnalysisNode(p, source, seen)
	}
	return graphexec.Node{}, ErrTargetUnavailable
}

func selectPlanAdvisoryTarget(s store.TaskAdvisorySnapshot, p *graphexec.Profile) (graphexec.Node, string, json.RawMessage, []string, error) {
	var state planbrief.PlanState
	if json.Unmarshal(s.PlanState, &state) != nil || s.PlanRevision < 1 {
		return graphexec.Node{}, "", nil, nil, ErrTargetUnavailable
	}
	stage := "set"
	nodeName := "set_agent"
	if s.StoryKey == "" {
		if state.AssembledWait != "" {
			stage = "assembled"
		} else if state.StorySet == nil || state.StorySet.Promoted || len(state.StorySet.Entries) == 0 {
			return graphexec.Node{}, "", nil, nil, ErrTargetUnavailable
		}
	} else {
		found := false
		for _, story := range state.Stories {
			if story.Key != s.StoryKey {
				continue
			}
			if found {
				return graphexec.Node{}, "", nil, nil, ErrTargetUnavailable
			}
			found = true
			if story.Retired || story.Candidate == "" || story.Draft.Name == "" || story.Hold != "" || story.LifecycleHold != "" || story.EffectHold != "" || len(story.EffectHolds) > 0 || story.Effect == "pending" {
				return graphexec.Node{}, "", nil, nil, ErrTargetUnavailable
			}
			if story.Parallel != nil && !story.Parallel.Sealed {
				return graphexec.Node{}, "", nil, nil, ErrTargetUnavailable
			}
			stage = story.Stage
			if story.Wait == nil || story.Wait.Stage != stage || story.Wait.Candidate != story.Candidate {
				return graphexec.Node{}, "", nil, nil, ErrTargetUnavailable
			}
			switch stage {
			case "story", "technical", "acceptance":
			default:
				return graphexec.Node{}, "", nil, nil, ErrTargetUnavailable
			}
			if revision, err := state.ReviewRevision(story.Key, stage); err != nil || revision != story.Wait.Revision {
				return graphexec.Node{}, "", nil, nil, ErrTargetUnavailable
			}
			nodeName = stage + "_agent"
		}
		if !found {
			return graphexec.Node{}, "", nil, nil, ErrTargetUnavailable
		}
	}
	node, ok := p.Node(nodeName)
	if !ok || node.Action != "lf_agent" {
		return graphexec.Node{}, "", nil, nil, ErrTargetUnavailable
	}
	want := "plan-" + stage
	if stage == "set" || stage == "assembled" {
		want = "plan-set"
	}
	if node.Attrs["lf_agent_markdown"] != want {
		return graphexec.Node{}, "", nil, nil, ErrTargetUnavailable
	}
	// Retain the entire exact state so upstream and shared references accompany
	// the selected draft; only the selected stage resources become expertise.
	var refs []string
	refs = append(refs, fmt.Sprintf("Saved Plan state for %s, revision %d", s.PlanParent, s.PlanRevision))
	if s.StoryKey != "" {
		refs = append(refs, "Current Plan story "+s.StoryKey)
	}
	sort.Strings(refs)
	evidence, _ := json.Marshal(struct {
		Parent   string              `json:"parent"`
		Story    string              `json:"story,omitempty"`
		Revision int64               `json:"revision"`
		State    planbrief.PlanState `json:"state"`
	}{s.PlanParent, s.StoryKey, s.PlanRevision, state})
	return node, stage, evidence, refs, nil
}

func eligibleAdvisoryJudgment(evidence json.RawMessage, role string) bool {
	if strings.HasPrefix(role, "plan-") {
		return true
	}
	var captured struct {
		Source store.AdvisorySourceOutput `json:"source"`
	}
	if json.Unmarshal(evidence, &captured) != nil {
		return false
	}
	raw := captured.Source.Payload
	var retained struct {
		Node json.RawMessage `json:"node"`
	}
	if json.Unmarshal(raw, &retained) == nil && len(retained.Node) > 0 {
		raw = retained.Node
	}
	var envelope struct {
		Result struct {
			Payload map[string]json.RawMessage `json:"payload"`
		} `json:"result"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return false
	}
	if role == "bug-analyst" {
		var bug struct {
			Analysis struct {
				Cause string `json:"cause"`
			} `json:"analysis"`
		}
		return json.Unmarshal(envelope.Result.Payload["bug"], &bug) == nil && strings.TrimSpace(bug.Analysis.Cause) != ""
	}
	var research map[string]json.RawMessage
	if json.Unmarshal(envelope.Result.Payload["research"], &research) != nil {
		return false
	}
	for _, key := range []string{"findings", "decisions", "assumptions"} {
		var values []json.RawMessage
		if json.Unmarshal(research[key], &values) == nil && len(values) > 0 {
			return true
		}
	}
	return false
}
