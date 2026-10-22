package messaging

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/meterview"
)

var meterArgumentNames = []string{"project", "task", "mode", "agent", "model", "from", "to", "by", "details", "page"}
var priceArgumentNames = []string{"agent", "model", "page"}

func (s *Service) registerMeter() {
	s.RegisterCommand(CommandDefinition{Name: "meter", NaturalLanguage: true, Effect: ReadEffect, Description: "Read usage, cost estimates and coverage", Arguments: []Argument{
		{Name: "project", Type: TextArgument, Description: "Registered owner/name or forge:owner/name"},
		{Name: "task", Type: IntegerArgument, Description: "Positive issue number within the project"},
		{Name: "mode", Type: TextArgument, Description: "research, plan, implement or spike"},
		{Name: "agent", Type: TextArgument, Description: "claude or codex"},
		{Name: "model", Type: TextArgument, Description: "Actual model filter"},
		{Name: "from", Type: TextArgument, Description: "Inclusive RFC3339 time or YYYY-MM-DD (UTC)"},
		{Name: "to", Type: TextArgument, Description: "Exclusive RFC3339 time or YYYY-MM-DD (UTC)"},
		{Name: "by", Type: TextArgument, Description: "Split cost by mode, agent, model and/or run (comma-separated), with total and unavailable rows"},
		{Name: "details", Type: BooleanArgument, Description: "Include invocation detail rows"},
		{Name: "page", Type: IntegerArgument, Description: "Report page number, starting at 1"},
	}}, s.meter)
	s.RegisterCommand(CommandDefinition{Name: "prices", NaturalLanguage: true, Effect: ReadEffect, Description: "Read recorded price catalogue in USD per million tokens", Arguments: []Argument{
		{Name: "agent", Type: TextArgument, Description: "claude or codex"},
		{Name: "model", Type: TextArgument, Description: "Model filter"},
		{Name: "page", Type: IntegerArgument, Description: "Catalogue page number, starting at 1"},
	}}, s.prices)
}

func validArguments(args map[string]string, names []string) bool {
	for name := range args {
		found := false
		for _, allowed := range names {
			if name == allowed {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func meterRequest(args map[string]string) (meterview.Request, int, error) {
	r := meterview.Request{Project: args["project"], Mode: args["mode"], Agent: args["agent"], Model: args["model"], From: args["from"], To: args["to"]}
	if !validArguments(args, meterArgumentNames) {
		return r, 0, errors.New("unknown argument")
	}
	if raw, ok := args["task"]; ok {
		task, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || task <= 0 {
			return r, 0, errors.New("task must be a positive issue number")
		}
		r.Task = task
	}
	if raw, ok := args["details"]; ok {
		if raw != "true" && raw != "false" {
			return r, 0, errors.New("details must be true or false")
		}
		r.Details = raw == "true"
	}
	if raw, ok := args["by"]; ok {
		by, err := meterview.ParseBreakdown(raw)
		if err != nil || len(by) == 0 {
			return r, 0, errors.New("by accepts mode, agent, model and run")
		}
		r.By = by
	}
	page, err := requestedPage(args)
	return r, page, err
}

func (s *Service) meter(ctx context.Context, m Message, at time.Time) (string, error) {
	request, page, err := meterRequest(m.Arguments)
	if err != nil {
		return assessed(ctx, "usage: /meter — "+err.Error(), presentation.Refusal)
	}
	// Deferred delivery owns bounded reporting work; admission never queries usage.
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	view, err := meterview.Read(bounded, s.db, request)
	if err != nil {
		return assessed(ctx, s.reportError(ctx, "meter", err), presentation.ReportableError)
	}
	if repoID, resolveErr := meterview.ResolveProject(ctx, s.db, request.Project); resolveErr == nil {
		presentation.AssessRepository(ctx, repoID)
	}
	lines := view.SummaryLines()
	if request.From != "" || request.To != "" {
		from, to := request.From, request.To
		if from == "" {
			from = "beginning"
		}
		if to == "" {
			to = "now"
		}
		lines = append([]string{"Time range: " + from + " – " + to}, lines...)
	}
	summary, err := s.protectReportLines(bounded, lines)
	if err != nil {
		return assessed(ctx, s.reportError(ctx, "meter", err), presentation.ReportableError)
	}
	rowLines := append(view.GroupLines(), view.DetailLines()...)
	if len(request.By) > 0 {
		breakdown, breakdownErr := meterview.ReadBreakdown(bounded, s.db, request, request.By)
		if breakdownErr != nil {
			return assessed(ctx, s.reportError(ctx, "meter", breakdownErr), presentation.ReportableError)
		}
		rowLines = append(breakdown.Lines(), view.DetailLines()...)
	}
	rows, err := s.protectReportLines(bounded, rowLines)
	if err != nil {
		return assessed(ctx, s.reportError(ctx, "meter", err), presentation.ReportableError)
	}
	pages, err := buildPages(summary, rows, s.ReplyLimit(), at, func(n int) string { return nextCommand("meter", m.Arguments, meterArgumentNames, n) })
	if err != nil {
		return assessed(ctx, tooLargeReply, presentation.Refusal)
	}
	if page > len(pages) {
		return assessed(ctx, pageReply(pages, page), presentation.Refusal)
	}
	return assessed(ctx, pageReply(pages, page), presentation.Success)
}

const tooLargeReply = "report too large, narrow the filter; try a project, task or from/to time filter"

func (s *Service) reportError(ctx context.Context, command string, err error) string {
	var viewErr *meterview.Error
	if errors.As(err, &viewErr) {
		switch viewErr.Kind {
		case meterview.InvalidFilter, meterview.UnknownProject, meterview.AmbiguousProject, meterview.TaskWithoutProject:
			return "usage: /" + command + " — " + viewErr.Message
		case meterview.TooLarge:
			return tooLargeReply
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return tooLargeReply
	}
	if s.Logger != nil {
		s.Logger.WarnContext(ctx, "chat report unavailable", "command", command)
	}
	return "report unavailable"
}

func (s *Service) prices(ctx context.Context, m Message, at time.Time) (string, error) {
	if !validArguments(m.Arguments, priceArgumentNames) {
		return assessed(ctx, "usage: /prices — unknown argument", presentation.Refusal)
	}
	page, err := requestedPage(m.Arguments)
	if err != nil {
		return assessed(ctx, "usage: /prices — "+err.Error(), presentation.Refusal)
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	catalogue, err := meterview.Prices(bounded, s.db, m.Arguments["agent"], m.Arguments["model"])
	if err != nil {
		return assessed(ctx, s.reportError(ctx, "prices", err), presentation.ReportableError)
	}
	lines, err := s.protectReportLines(bounded, meterview.PriceLines(catalogue))
	if err != nil {
		return assessed(ctx, s.reportError(ctx, "prices", err), presentation.ReportableError)
	}
	pages, err := buildPages(lines[:1], lines[1:], s.ReplyLimit(), at, func(n int) string { return nextCommand("prices", m.Arguments, priceArgumentNames, n) })
	if err != nil {
		return assessed(ctx, "report too large, narrow the filter; try agent or model", presentation.Refusal)
	}
	if page > len(pages) {
		return assessed(ctx, pageReply(pages, page), presentation.Refusal)
	}
	return assessed(ctx, pageReply(pages, page), presentation.Success)
}

// Protect before pagination so redaction expansion cannot displace page footers.
func (s *Service) protectReportLines(ctx context.Context, lines []string) ([]string, error) {
	if len(lines) == 0 || s.Protect == nil {
		return lines, nil
	}
	text, err := s.Protect(ctx, strings.Join(lines, "\n"))
	if err != nil {
		return nil, err
	}
	return strings.Split(text, "\n"), nil
}
