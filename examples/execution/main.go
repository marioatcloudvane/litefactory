// Command execution demonstrates consumer-owned activities with no factory setup.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/pkg/execution/agent"
	"github.com/cloudvane/litefactory/pkg/execution/graph"
)

// These are this application's attributes and action names, not factory policy.
const workflow = `digraph shipping {
  graph [start="prepare"];
  prepare [action="collect", outcomes="ready"];
  summarize [action="summarize", outcomes="written"];
  done [terminal="true"];
  prepare -> summarize [on="ready"];
  summarize -> done [on="written"];
}`

type runner interface {
	Run(context.Context, agent.Request) (agent.Result, error)
}

type fakeRunner struct{}

func (fakeRunner) Run(ctx context.Context, _ agent.Request) (agent.Result, error) {
	if err := ctx.Err(); err != nil {
		return agent.Result{}, err
	}
	return agent.Result{Text: "Shipment A contains two parcels.", Session: "demo-session", Started: true, Terminal: true}, nil
}

// consumerGraph interprets only the vocabulary owned by this application.
// Parsing mechanics and structural checking come from the reusable library.
func consumerGraph() (*graph.Graph, error) {
	document, err := graph.Parse([]byte(workflow))
	if err != nil {
		return nil, err
	}
	g := &graph.Graph{Start: document.Attrs["start"]}
	for _, declaration := range document.Declarations {
		switch declaration.Kind {
		case "node":
			n := graph.Node{Name: declaration.Name, Action: declaration.Attrs["action"], Terminal: declaration.Attrs["terminal"] == "true"}
			if outcomes := declaration.Attrs["outcomes"]; outcomes != "" {
				n.Outcomes = strings.Split(outcomes, ",")
			}
			g.Nodes = append(g.Nodes, n)
		case "edge":
			g.Edges = append(g.Edges, graph.Edge{From: declaration.From, To: declaration.To, On: declaration.Attrs["on"]})
		}
	}
	var actions graph.Registry
	for _, spec := range []graph.ActionSpec{
		{Name: "collect", Outcomes: []string{"ready"}},
		{Name: "summarize", Outcomes: []string{"written"}},
	} {
		if err := actions.Register(spec); err != nil {
			return nil, err
		}
	}
	if err := graph.Validate(g, actions); err != nil {
		return nil, err
	}
	return g, nil
}

func run(ctx context.Context, output io.Writer, execution runner, directory string) error {
	g, err := consumerGraph()
	if err != nil {
		return err
	}
	var shipment string
	// Handlers, dispatch and application state belong to the consumer. The
	// library checks the graph and selects edges; it does not run a second engine.
	handlers := map[string]func() (string, error){
		"collect": func() (string, error) {
			shipment = "Shipment A: two parcels."
			return "ready", nil
		},
		"summarize": func() (string, error) {
			result, err := execution.Run(ctx, agent.Request{Prompt: shipment, WorkingDirectory: directory, Timeout: time.Minute})
			if err != nil {
				return "", err
			}
			if !result.Terminal || result.IsError {
				return "", fmt.Errorf("agent returned no successful terminal result")
			}
			if _, err := fmt.Fprintln(output, result.Text); err != nil {
				return "", err
			}
			return "written", nil
		},
	}
	for name := g.Start; ; {
		node, ok := g.Node(name)
		if !ok {
			return fmt.Errorf("unknown node %q", name)
		}
		if node.Terminal {
			return nil
		}
		handler, ok := handlers[node.Action]
		if !ok {
			return fmt.Errorf("no handler for %q", node.Action)
		}
		outcome, err := handler()
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if _, err := fmt.Fprintf(output, "%s: %s\n", name, outcome); err != nil {
			return err
		}
		next, ok := graph.Decide(g, name, outcome)
		if !ok {
			return fmt.Errorf("no unique edge for %s/%s", name, outcome)
		}
		name = next
	}
}

func main() {
	directory, err := os.Getwd()
	if err == nil {
		err = run(context.Background(), os.Stdout, fakeRunner{}, directory)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
