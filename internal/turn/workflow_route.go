package turn

import (
	"context"
	"errors"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/harness"
)

// resultRoute is the shared result service of one node attempt together with the
// shared-route guidance it produced. Every graph agent node of every mode is
// built from one, so no mode carries a route of its own.
type resultRoute struct {
	prepared *preparedMCP
	scope    agentresult.Context
	guidance string
}

// abort releases the route's transports and attempt authority after a failure
// while the invocation is still being built, and returns the original cause.
func (r *resultRoute) abort(ctx context.Context, cause error) error {
	return errors.Join(cause, r.prepared.close(context.WithoutCancel(ctx)))
}

// openResultRoute binds the shared result service for the prepared run and
// resolves the guidance and trusted scope every node invocation carries. A nil
// override selects the generic resolver and grants; a mode with a richer trusted
// context supplies its own. On failure nothing stays open.
func (w *researchWorkflow) openResultRoute(ctx context.Context, req *agent.Request, override *resultOverride) (*resultRoute, error) {
	prep := w.prep
	if prep == nil {
		return nil, errors.New("workflow invocation preparation is missing")
	}
	p, spec, err := prepareMCPWith(ctx, prep, *req, nil, override)
	if err != nil {
		return nil, err
	}
	route := &resultRoute{prepared: p}
	if host, ok := prep.runner.(interface{ SupportsMCP() bool }); ok && host.SupportsMCP() && !w.opts.MCP.DisableMCP {
		req.MCPLaunch = spec
	}
	route.guidance, err = mcpGuidance(ctx, req.MCPLaunch, p.service, nil)
	if err != nil {
		return nil, route.abort(ctx, err)
	}
	route.scope, err = p.service.trustedContext(ctx)
	if err != nil {
		return nil, route.abort(ctx, err)
	}
	return route, nil
}

// finishRoute appends the node's own prompt text and the route's guidance,
// binds the final route, and builds the invocation with the cold fallback. The
// invocation carries the result service, so the agent activity can require
// matching authority and read back the retained result.
func (w *researchWorkflow) finishRoute(ctx context.Context, route *resultRoute, req *agent.Request, text, sessionPath string) (ResearchInvocation, error) {
	prep := w.prep
	suffix := text + route.guidance
	req.Prompt += suffix
	beforeRoute := len(req.Prompt)
	if err := route.prepared.bindFinalRoute(ctx, req, nil); err != nil {
		return ResearchInvocation{}, route.abort(ctx, err)
	}
	coldInput := prep.promptInput
	coldInput.Resume = false
	coldPrompt, err := harness.BuildPrompt(coldInput)
	if err != nil {
		return ResearchInvocation{}, route.abort(ctx, err)
	}
	cold := *req
	cold.Prompt = coldPrompt + suffix + req.Prompt[beforeRoute:]
	p := route.prepared
	return ResearchInvocation{Request: *req, ColdRequest: &cold, Kind: w.req.Agent, Observe: func(actual agent.Request) { prep.request = actual; prep.prompt = actual.Prompt }, SessionPath: sessionPath, Results: p.service, Close: func() error { return p.closeTransports(context.WithoutCancel(ctx)) }, Release: func() error { return w.opts.DB.EndResultAttempt(context.WithoutCancel(ctx), p.service.Binding()) }}, nil
}
