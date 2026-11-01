package meterview

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// ErrorKind defines the shared meter view value.
type ErrorKind string

// Error kinds distinguish invalid requests from bounded report failures.
const (
	InvalidFilter      ErrorKind = "invalid_filter"
	UnknownProject     ErrorKind = "unknown_project"
	AmbiguousProject   ErrorKind = "ambiguous_project"
	TaskWithoutProject ErrorKind = "task_without_project"
	TooLarge           ErrorKind = "too_large"
)

// Error defines the shared meter view value.
type Error struct {
	Kind    ErrorKind
	Message string
	Cause   error
}

// Error describes the failure without report content.
func (e *Error) Error() string { return e.Message }

// Unwrap preserves the underlying failure.
func (e *Error) Unwrap() error     { return e.Cause }
func invalid(message string) error { return &Error{Kind: InvalidFilter, Message: message} }
func classify(err error) error {
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "meter snapshot exceeds limits") {
		return &Error{Kind: TooLarge, Message: "report too large, narrow the filter", Cause: err}
	}
	return err
}

// Request defines the shared meter view value.
type Request struct {
	Project                      string
	Task                         int64
	Mode, Agent, Model, From, To string
	Details                      bool
	// By selects breakdown dimensions (see BreakdownDimensions); empty means
	// the plain total.
	By []string
}

// Validate returns the corresponding meter view or wording.
func Validate(r Request, plainDates bool) (store.MeterFilter, error) {
	f := store.MeterFilter{Issue: r.Task, Mode: r.Mode, CodingAgent: r.Agent, Model: r.Model}
	if r.Mode != "" && r.Mode != "research" && r.Mode != "plan" && r.Mode != "implement" && r.Mode != "spike" && r.Mode != "advisory" {
		return f, invalid("invalid mode")
	}
	if r.Agent != "" && r.Agent != "claude" && r.Agent != "codex" {
		return f, invalid("invalid coding agent")
	}
	if len(r.Model) > 256 || strings.ContainsAny(r.Model, "\x00\r\n\t") {
		return f, invalid("invalid model")
	}
	if r.Task < 0 {
		return f, invalid("issue must be positive")
	}
	if r.Task > 0 && r.Project == "" {
		return f, &Error{Kind: TaskWithoutProject, Message: "task requires a project"}
	}
	for _, bound := range []struct {
		raw  string
		dest **time.Time
	}{{r.From, &f.TimeFrom}, {r.To, &f.TimeTo}} {
		if bound.raw == "" {
			continue
		}
		t, e := time.Parse(time.RFC3339Nano, bound.raw)
		if e != nil && plainDates {
			t, e = time.Parse("2006-01-02", bound.raw)
		}
		if e != nil {
			return f, invalid("time bounds must be timezone-aware RFC3339")
		}
		t = t.UTC()
		*bound.dest = &t
	}
	if f.TimeFrom != nil && f.TimeTo != nil && !f.TimeFrom.Before(*f.TimeTo) {
		return f, invalid("time_from must precede time_to")
	}
	return f, nil
}

// ResolveProject returns the corresponding meter view or wording.
func ResolveProject(ctx context.Context, db Reader, selector string) (int64, error) {
	if selector == "" {
		return 0, nil
	}
	repos, err := db.ListRepos(ctx)
	if err != nil {
		return 0, err
	}
	var matched int64
	for _, r := range repos {
		if selector == r.Path || selector == r.Slug() || selector == r.Forge+":"+r.Slug() {
			if matched != 0 {
				return 0, &Error{Kind: AmbiguousProject, Message: "repository selector is ambiguous"}
			}
			matched = r.ID
		}
	}
	if matched == 0 {
		return 0, &Error{Kind: UnknownProject, Message: "unknown registered repository"}
	}
	return matched, nil
}

// Read returns the corresponding meter view or wording.
func Read(ctx context.Context, db Reader, r Request) (View, error) {
	f, err := Validate(r, true)
	if err != nil {
		return View{}, err
	}
	f.RepoID, err = ResolveProject(ctx, db, r.Project)
	if err != nil {
		return View{}, err
	}
	return Build(ctx, db, f, r.Details)
}

// Prices returns the corresponding meter view or wording.
func Prices(ctx context.Context, db Reader, agent, model string) (store.MeterCatalog, error) {
	if _, err := Validate(Request{Agent: agent, Model: model}, true); err != nil {
		return store.MeterCatalog{}, err
	}
	catalog, err := db.ListMeterPrices(ctx)
	if err != nil {
		return catalog, err
	}
	if agent == "" && model == "" {
		return catalog, nil
	}
	rates := make([]store.MeterPriceRate, 0, len(catalog.Rates))
	for _, rate := range catalog.Rates {
		if (agent == "" || rate.CodingAgent == agent) && (model == "" || rate.Model == model) {
			rates = append(rates, rate)
		}
	}
	catalog.Rates = rates
	return catalog, nil
}

// PriceLines returns the corresponding meter view or wording.
func PriceLines(catalog store.MeterCatalog) []string {
	lines := []string{fmt.Sprintf("Price revision %d — USD per million tokens", catalog.RevisionID)}
	for _, rate := range catalog.Rates {
		lines = append(lines, fmt.Sprintf("%s\t%s\t%s\t%s", rate.CodingAgent, rate.Model, rate.Dimension, rate.USDPerMillion))
	}
	return lines
}
