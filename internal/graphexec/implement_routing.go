package graphexec

import (
	"errors"
	"maps"
	"slices"
	"strings"
)

func decodeImplementFact(action string, attrs map[string]string) (ResearchCondition, error) {
	c := ResearchCondition{Field: attrs["lf_field"], Condition: attrs["lf_condition"]}
	valid := c.Field == "story.size" && slices.Contains([]string{"S", "M", "L"}, c.Condition) || c.Field == "story.sizing_valid" && slices.Contains([]string{"true", "false"}, c.Condition)
	_, source := attrs["lf_source"]
	_, fallback := attrs["lf_default"]
	if action != ResearchIfAction || !valid || source || fallback {
		return c, errors.New("invalid Implement fact comparison")
	}
	return c, nil
}

// EvaluateImplementFacts compares only the saved fact named by the graph.
// An absent fact never matches, including a comparison against false.
func (c ResearchCondition) EvaluateImplementFacts(facts map[string]string) (string, error) {
	if _, err := decodeImplementFact(ResearchIfAction, map[string]string{"lf_field": c.Field, "lf_condition": c.Condition}); err != nil {
		return "", err
	}
	if value, ok := facts[c.Field]; ok && value == c.Condition {
		return "true", nil
	}
	return "false", nil
}

// workerRouting admits a closed decision region, not a size-to-tier policy.
// Every path through it must end at one worker after the same budget and sync.
func (c *implementCheck) workerRouting() {
	var workers, budgets []Node
	for _, n := range c.p.Nodes {
		if n.Action == "lf_agent" && !c.isResolver(n) {
			workers = append(workers, n)
		}
		if n.Action == ImplementBudgetAction {
			budgets = append(budgets, n)
		}
	}
	if len(budgets) != 1 {
		c.fail("routed workers require one shared budget")
		return
	}
	selector, _ := c.p.Node(c.p.Start)
	selected, routed := Decide(c.p, selector.Name, "worker")
	if !routed || selected != budgets[0].Name {
		c.fail("routed worker selection must enter the shared budget")
	}
	gate, ok := ImplementWorkerSyncNode(c.p)
	if !ok {
		c.fail("implement graph requires exactly one worker agent unless two workers share a budget and worker sync")
		return
	}
	root, ok := Decide(c.p, gate.Name, "synced")
	if !ok {
		return
	}
	region, active, reached := map[string]bool{}, map[string]bool{}, map[string]bool{}
	var visit func(string)
	visit = func(name string) {
		n, exists := c.p.Node(name)
		if !exists {
			c.fail("worker routing reaches missing node %s", name)
			return
		}
		if n.Action == "lf_agent" && !c.isResolver(n) {
			reached[name] = true
			return
		}
		if active[name] {
			c.fail("worker routing contains a cycle at %s", name)
			return
		}
		if region[name] {
			return
		}
		if n.Action != ResearchIfAction {
			c.fail("worker routing must contain only fact conditions before a worker, not %s", name)
			return
		}
		region[name], active[name] = true, true
		if _, err := decodeImplementFact(n.Action, n.Attrs); err != nil {
			c.fail("worker routing condition %s: %v", name, err)
		}
		if len(n.Outcomes) != 2 || !slices.Contains(n.Outcomes, "true") || !slices.Contains(n.Outcomes, "false") {
			c.fail("worker routing condition %s requires true and false outcomes", name)
		}
		for _, outcome := range []string{"true", "false"} {
			to, routed := Decide(c.p, name, outcome)
			if !routed {
				c.fail("worker routing condition %s requires one %s route", name, outcome)
				continue
			}
			visit(to)
		}
		delete(active, name)
	}
	visit(root)
	if len(region) == 0 || len(reached) != 2 {
		c.fail("worker routing must select exactly one of two reachable workers")
	}
	for _, n := range c.p.Nodes {
		if n.Action == ResearchIfAction && !region[n.Name] {
			c.fail("condition %s is outside the worker routing region", n.Name)
		}
	}
	for _, e := range c.p.Edges {
		if !region[e.To] && !reached[e.To] {
			continue
		}
		if region[e.From] {
			continue
		}
		if e.From == gate.Name && e.To == root && slices.Contains([]string{"synced", "current", "deferred"}, e.On) {
			continue
		}
		c.fail("worker routing incoming path to %s must pass its worker sync and fact region", e.To)
	}
	// All operational attributes must match; model tier is deliberately selected
	// by the graph and presentation attributes carry no execution authority.
	attrs := func(n Node) map[string]string {
		out := map[string]string{}
		for k, v := range n.Attrs {
			if strings.HasPrefix(k, "lf_") && k != "lf_model_tier" {
				out[k] = v
			}
		}
		return out
	}
	if !maps.Equal(attrs(workers[0]), attrs(workers[1])) || workers[0].Limit != workers[1].Limit || !slices.Equal(workers[0].Outcomes, workers[1].Outcomes) {
		c.fail("routed workers require equivalent role, skills, permissions and bounds")
	}
	results := map[string]string{}
	var save, park string
	for _, w := range workers {
		to, _ := Decide(c.p, w.Name, "success")
		if prior := results[to]; prior != "" {
			c.fail("routed workers must have distinct result decisions")
		}
		results[to] = w.Name
		failed, _ := Decide(c.p, w.Name, "failed")
		if failed != budgets[0].Name {
			c.fail("worker %s must return to the shared budget", w.Name)
		}
		r, _ := c.p.Node(to)
		if r.Limit != w.Limit {
			c.fail("worker %s result must retain its attempt bound", w.Name)
		}
		for _, outcome := range []string{"completed", "incomplete", "blocked", "stopped"} {
			target, routed := Decide(c.p, r.Name, outcome)
			if !routed {
				continue
			}
			switch outcome {
			case "completed":
				if save != "" && save != target {
					c.fail("routed workers require one shared save")
				}
				save = target
			case "incomplete":
				if target != budgets[0].Name {
					c.fail("worker results require the shared budget")
				}
			case "blocked", "stopped":
				if park != "" && park != target {
					c.fail("routed workers require one shared park")
				}
				park = target
			}
		}
	}
	for _, n := range c.p.Nodes {
		if n.Action == ImplementResultAction {
			if _, ok := results[n.Name]; !ok {
				c.fail("routed result %s is not owned by a selected worker", n.Name)
			}
		}
	}
	budgetPark, _ := Decide(c.p, budgets[0].Name, "park")
	syncPark, _ := Decide(c.p, gate.Name, "conflict")
	if budgetPark != park || syncPark != park {
		c.fail("routed workers require the shared budget, sync and results to park together")
	}
	for _, e := range c.p.Edges {
		if worker := results[e.To]; worker != "" && (e.From != worker || e.On != "success") {
			c.fail("result %s may only read its own selected worker", e.To)
		}
		if e.To == save {
			if _, ok := results[e.From]; !ok || e.On != "completed" {
				c.fail("shared save may only follow a completed selected worker result")
			}
		}
	}
	if done, ok := c.p.Node(save); ok {
		retry, _ := Decide(c.p, done.Name, "retry")
		if retry != budgets[0].Name {
			c.fail("shared save must retry through the shared budget")
		}
	}
}
