package access

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// Entry kinds a Target may carry.
const (
	EntryStart  = "start"
	EntryAnswer = "answer"
)

const (
	agentAction        = "lf_agent"
	kindAttribute      = "lf_kind"
	approveReplyAttr   = "lf_approve_reply"
	defaultNodeAccess  = "read"
	writeNodeAccess    = "write"
	maxReachableAgents = 256
)

// NodeFact is one agent node the request could reach.
type NodeFact struct {
	Name   string `json:"name"`
	Kind   string `json:"kind,omitempty"`
	Action string `json:"action"`
	Access string `json:"access"`
}

// TargetFacts is what the classifier is told about the target, read from the
// installed team graph. It is stored verbatim as the decision's target JSON.
type TargetFacts struct {
	Workflow string     `json:"workflow"`
	Entry    string     `json:"entry"`
	Node     string     `json:"node"`
	Kind     string     `json:"kind,omitempty"`
	Action   string     `json:"action,omitempty"`
	Nodes    []NodeFact `json:"nodes"`
	Command  string     `json:"command,omitempty"`
}

// WorkflowSource resolves a repository's installed workflow definition. A
// turn.WorkflowSourceResolver is assignable to it.
type WorkflowSource = func(context.Context, store.Repo, agent.Workflow, graphexec.Registry) (*teamsource.WorkflowDefinition, error)

// SpecsFor returns the factory registry that admits the given workflow.
type SpecsFor func(agent.Workflow) (graphexec.Registry, error)

// ResolverFacts builds a Facts reader from the installed workflow source and
// the registry lookup the daemon already uses to admit workflows.
func ResolverFacts(resolve WorkflowSource, specs SpecsFor) Facts {
	return func(ctx context.Context, repo store.Repo, target Target) (TargetFacts, error) {
		if resolve == nil || specs == nil {
			return TargetFacts{}, errors.New("access: workflow source and registry are required")
		}
		registry, err := specs(target.Workflow)
		if err != nil {
			return TargetFacts{}, err
		}
		def, err := resolve(ctx, repo, target.Workflow, registry)
		if err != nil {
			return TargetFacts{}, err
		}
		return FactsFromDefinition(def, target)
	}
}

// FactsFromDefinition lists the target node and every agent node reachable
// from it, with the access each declares. A start targets the graph's start
// node; an answer targets target.Node, which must exist.
func FactsFromDefinition(def *teamsource.WorkflowDefinition, target Target) (TargetFacts, error) {
	if def == nil || def.Profile == nil {
		return TargetFacts{}, errors.New("access: workflow definition is missing")
	}
	profile := def.Profile
	var name string
	switch target.Entry {
	case EntryStart:
		name = profile.Start
	case EntryAnswer:
		name = target.Node
	default:
		return TargetFacts{}, fmt.Errorf("access: unknown entry %q", target.Entry)
	}
	if name == "" {
		return TargetFacts{}, errors.New("access: target node is missing")
	}
	node, ok := profile.Node(name)
	if !ok {
		return TargetFacts{}, fmt.Errorf("access: target node %q is not in the graph", name)
	}
	facts := TargetFacts{
		Workflow: string(target.Workflow),
		Entry:    target.Entry,
		Node:     node.Name,
		Kind:     node.Attrs[kindAttribute],
		Action:   node.Action,
		Nodes:    []NodeFact{},
	}
	next := map[string][]string{}
	for _, edge := range profile.Edges {
		next[edge.From] = append(next[edge.From], edge.To)
	}
	seen := map[string]bool{node.Name: true}
	queue := []string{node.Name}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		n, ok := profile.Node(current)
		if !ok {
			return TargetFacts{}, fmt.Errorf("access: graph node %q is missing or ambiguous", current)
		}
		if n.Action == agentAction {
			if len(facts.Nodes) >= maxReachableAgents {
				return TargetFacts{}, errors.New("access: graph has too many reachable agent nodes")
			}
			access, err := nodeAccess(n)
			if err != nil {
				return TargetFacts{}, err
			}
			facts.Nodes = append(facts.Nodes, NodeFact{Name: n.Name, Kind: n.Attrs[kindAttribute], Action: n.Action, Access: access})
		}
		for _, to := range next[current] {
			if !seen[to] {
				seen[to] = true
				queue = append(queue, to)
			}
		}
	}
	return facts, nil
}

func nodeAccess(n graphexec.Node) (string, error) {
	switch access := n.Attrs[graphexec.AccessAttribute]; access {
	case "":
		return defaultNodeAccess, nil
	case defaultNodeAccess, writeNodeAccess:
		return access, nil
	default:
		return "", fmt.Errorf("access: node %q declares unsupported access %q", n.Name, access)
	}
}

// ApproveReplyOf returns the node's approve-reply attribute, "" when the node
// or the attribute is absent.
func ApproveReplyOf(def *teamsource.WorkflowDefinition, node string) string {
	if def == nil || def.Profile == nil {
		return ""
	}
	n, ok := def.Profile.Node(node)
	if !ok {
		return ""
	}
	return n.Attrs[approveReplyAttr]
}
