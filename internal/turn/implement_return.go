package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/store"
)

type reviewedReturnContext struct {
	State  ValidateState
	Frozen []store.ApprovedStoryRevision
}

// returnSelection validates optional advice against the exact retained frozen
// set. Missing or unusable advice selects a unique named story, then plan order.
func returnSelection(r store.ValidateReturn, advice []agentresult.ReturnAllocation) ([]agentresult.ReturnAllocation, string, error) {
	var c reviewedReturnContext
	if err := json.Unmarshal([]byte(r.Context), &c); err != nil {
		return nil, "", err
	}
	keys := map[string]bool{}
	var order []string
	for _, s := range c.Frozen {
		keys[s.Key] = true
		order = append(order, s.Key)
	}
	if len(order) == 0 {
		for _, s := range c.State.Stories {
			keys[s.Story] = true
			order = append(order, s.Story)
		}
	}
	if len(order) == 0 {
		return nil, "", errors.New("reviewed return has no frozen stories")
	}
	valid := len(advice) > 0 && len(advice) <= len(order)
	seen := map[string]bool{}
	changes := map[string]bool{}
	scopes := map[string]bool{}
	pathPattern := regexp.MustCompile(`(?:[A-Za-z0-9_.-]+/)*[A-Za-z0-9_-]+\.[A-Za-z0-9]+`)
	for _, a := range advice {
		change := strings.TrimSpace(a.Change)
		if len(advice) > 1 {
			paths := pathPattern.FindAllString(change, -1)
			if len(paths) == 0 {
				valid = false
			}
			for _, path := range paths {
				if scopes[path] {
					valid = false
				}
				scopes[path] = true
			}
		}
		if !keys[a.Story] || seen[a.Story] || len(advice) > 1 && (change == "" || changes[change]) {
			valid = false
		}
		seen[a.Story] = true
		changes[change] = true
	}
	if valid {
		return advice, "agent", nil
	}
	named := map[string]bool{}
	unknown := false
	for _, word := range strings.FieldsFunc(r.Explanation, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' }) {
		if keys[word] {
			named[word] = true
		} else if strings.HasPrefix(word, "story-") {
			unknown = true
		}
	}
	if len(named) == 1 && !unknown {
		for k := range named {
			return []agentresult.ReturnAllocation{{Story: k}}, "named-story", nil
		}
	}
	return []agentresult.ReturnAllocation{{Story: order[0]}}, "first-plan-story", nil
}

func (b ImplementBindings) applyReviewedReturn(ctx context.Context, opts Options, req ActivityRequest, a ImplementAuthority) error {
	r, err := b.DB.LatestValidateReturn(ctx, a.Parent.String(), a.Generation)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if r.Applied {
		return nil
	}
	var selection []agentresult.ReturnAllocation
	if r.Selection == "" {
		var advice []agentresult.ReturnAllocation
		if b.ReturnAllocation != nil {
			advice, err = b.ReturnAllocation(ctx, req)
			if err != nil {
				return err
			}
		}
		picked, source, e := returnSelection(r, advice)
		if e != nil {
			return e
		}
		raw, e := json.Marshal(picked)
		if e != nil {
			return e
		}
		r, e = b.DB.SelectValidateReturn(ctx, r.InstanceID, string(raw), source)
		if e != nil {
			return e
		}
	}
	if err := json.Unmarshal([]byte(r.Selection), &selection); err != nil {
		return err
	}
	for _, s := range selection {
		row, e := b.DB.GetImplementStory(ctx, r.Parent, r.Generation, s.Story)
		if e != nil {
			return e
		}
		guidance := fmt.Sprintf("The owner returned reviewed work from Validate %d at commit %s.\nThe owner's explanation:\n%s", r.InstanceID, r.Commit, r.Explanation)
		if len(selection) > 1 {
			guidance += "\nYour assigned part of the change:\n" + s.Change
		}
		if len(guidance) > validateImplementGuidanceLimit {
			return errors.New("return explanation exceeds the worker guidance limit")
		}
		if row.State == store.ImplementWaiting && row.Guidance == guidance {
			continue
		}
		if row.State != store.ImplementMerged {
			return fmt.Errorf("returned story %s is %s, not merged", s.Story, row.State)
		}
		if b.AdvanceReturn == nil {
			return errors.New("return lacks guarded story-branch advancement")
		}
		if b.AdvanceReturn != nil {
			moved, e := b.AdvanceReturn(ctx, a.IntegrationBranch, row.Branch, r.Commit)
			if e != nil {
				return e
			}
			if !moved {
				return fmt.Errorf("returned story %s cannot advance to reviewed work", s.Story)
			}
		}
		if _, e = b.DB.TransitionImplementStory(ctx, store.ImplementTransition{Parent: r.Parent, Generation: r.Generation, Key: s.Story, From: store.ImplementMerged, To: store.ImplementWaiting, ExpectedRevision: row.Revision, Guidance: guidance, At: b.now(opts)}); e != nil {
			return e
		}
	}
	return b.DB.ApplyValidateReturn(ctx, r.InstanceID)
}

// Only the existing pre-dispatch resolver in this coordinator instance may
// supply allocation advice. A normal sync has no agent and uses the fallback.
func (rt *implementRuntime) returnAllocation(ctx context.Context, req ActivityRequest) ([]agentresult.ReturnAllocation, error) {
	attempts, err := rt.opts.DB.ListGraphNodeAttempts(ctx, req.InstanceID)
	if err != nil {
		return nil, err
	}
	for _, a := range slices.Backward(attempts) {
		if a.Node != "sync_start_resolve" || a.Status != store.GraphSucceeded {
			continue
		}
		outputs, e := rt.opts.DB.ListGraphOutputsBounded(ctx, a.ID, agentresult.MaxResearchNodeBytes, 16)
		if e != nil {
			return nil, e
		}
		for _, o := range outputs {
			if o.Kind != store.GraphOutputJSON {
				continue
			}
			node, d := agentresult.DecodeImplementNode(o.Payload, agentresult.Context{SchemaVersion: agentresult.SchemaVersion, Mode: string(agent.ModeImplement), WorkflowVersion: store.WorkflowSeparated, CoordinatorRole: true})
			if !d.Valid || node.Response != agentresult.ImplementResponseCompleted {
				return nil, nil
			}
			if node.Result.Payload.Implement != nil {
				return node.Result.Payload.Implement.ReturnAllocation, nil
			}
		}
		return nil, nil
	}
	return nil, nil
}

func (l implementRepoLoader) advanceReturn(ctx context.Context, integration, branch, reviewed string) (moved bool, err error) {
	scratch, err := l.Begin(ctx, integration)
	if err != nil {
		return false, err
	}
	defer func() { err = errors.Join(err, scratch.Close(context.WithoutCancel(ctx))) }()
	wrapped, ok := scratch.(gatedScratch)
	if !ok {
		return false, errors.New("return requires a guarded git scratch")
	}
	tree, ok := wrapped.ImplementScratch.(*gitScratch)
	if !ok {
		return false, errors.New("return requires a git scratch")
	}
	if contains, e := tree.wt.IsAncestor(ctx, reviewed, tree.head); e != nil || !contains {
		return false, e
	}
	current, err := tree.wt.RevParse(ctx, "refs/remotes/"+tree.repo.Remote+"/"+branch, git.RevParseOptions{})
	if err != nil {
		return false, err
	}
	if current == tree.head {
		return true, nil
	}
	if contained, e := tree.wt.IsAncestor(ctx, current, tree.head); e != nil || !contained {
		return false, e
	}
	if err := scratch.Publish(ctx, branch, current); err != nil {
		return false, err
	}
	return true, nil
}
