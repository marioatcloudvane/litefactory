package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/durableworkflow"
	"github.com/cloudvane/litefactory/internal/loganalysis"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

// standardsWorkflowAdapter retains the existing author session and effect receipts.
type standardsWorkflowAdapter struct {
	standards *ConversationStandards
	db        *store.DB
}

func (a standardsWorkflowAdapter) Resolve(ctx context.Context, p owneraction.Principal, d durableworkflow.Definition, r durableworkflow.Resolved, c durableworkflow.Continuation, at time.Time) (owneraction.Facts, error) {
	args := map[string]string{}
	for k, v := range r.Inputs {
		args[k] = fmt.Sprint(v)
	}
	if r.Project != "" {
		forge, slug, ok := strings.Cut(r.Project, ":")
		if !ok {
			forge = "github"
			slug = r.Project
		}
		owner, name, ok := strings.Cut(slug, "/")
		if !ok {
			return owneraction.Facts{}, errors.New("registered project required")
		}
		repo, e := a.db.GetRepoBySlug(ctx, forge, owner, name)
		if e != nil {
			return owneraction.Facts{}, e
		}
		args["repository_id"] = strconv.FormatInt(repo.ID, 10)
	}
	operation := "author.start"
	if d.Adapter == "log-analysis" {
		operation = "analysis.start"
	} else {
		if category := args["category"]; category != "" {
			args["category_ids"] = category
			delete(args, "category")
		}
		if target := args["target"]; target != "" {
			args["target_id"] = target
			delete(args, "target")
		}
		if c.Job != "" {
			job, e := a.db.ReadWorkflowJob(ctx, c.Job)
			if e != nil {
				return owneraction.Facts{}, e
			}
			operation = "author.answer"
			args = map[string]string{"id": job.DomainRef, "version": strconv.FormatInt(c.SessionRevision, 10)}
			raw, e := json.Marshal(c.Answers)
			if e != nil {
				return owneraction.Facts{}, e
			}
			args["workflow_answers"] = string(raw)
		}
	}
	if operation == "author.start" {
		mode := args["mode"]
		if mode == "" {
			mode = "create"
		}
		if mode == "create" && args["target_id"] != "" || mode == "edit" && args["target_id"] == "" {
			return owneraction.Facts{}, errors.New("authoring target does not match create or edit mode")
		}
		if r.Project != "" && a.standards.Authoring.Projects == nil {
			return owneraction.Facts{}, errors.New("project context reader unavailable")
		}
		revision, err := a.standards.Book.Current(ctx)
		if err != nil && !errors.Is(err, store.ErrNotFound) && !errors.Is(err, store.ErrStandardsUnpublished) {
			return owneraction.Facts{}, err
		}
		if mode == "edit" {
			found := false
			for _, standard := range revision.Snapshot.Standards {
				found = found || standard.ID == args["target_id"]
			}
			if !found {
				return owneraction.Facts{}, errors.New("unknown standard edit target")
			}
		}
		for _, id := range standardsCategories(args["category_ids"]) {
			found := false
			for _, category := range revision.Snapshot.Categories {
				found = found || category.ID == id
			}
			if !found {
				return owneraction.Facts{}, errors.New("unknown category filter")
			}
		}
	}
	return a.standards.ResolveFrozen(ctx, p, messaging.StandardsRequest{Operation: operation, Arguments: args, At: at})
}
func workflowStandardsRequest(f owneraction.Facts, key string, at time.Time) messaging.StandardsRequest {
	return messaging.StandardsRequest{Operation: strings.TrimPrefix(f.Invocation.Command, "standards-") + "." + f.Invocation.Arguments["action"], Arguments: maps.Clone(f.Invocation.Arguments), OperationKey: key, At: at}
}
func (a standardsWorkflowAdapter) Recheck(ctx context.Context, p owneraction.Principal, f owneraction.Facts, at time.Time) error {
	return a.standards.RecheckFrozen(ctx, p, workflowStandardsRequest(f, "", at), f)
}
func (a standardsWorkflowAdapter) Execute(ctx context.Context, p owneraction.Principal, f owneraction.Facts, key string, at time.Time) (durableworkflow.Output, error) {
	ctx = loganalysis.WithWorkflowDeadline(ctx)
	out, e := a.standards.ExecuteFrozen(ctx, p, workflowStandardsRequest(f, key, at), f)
	projection, read := a.project(ctx, f, out)
	return projection, errors.Join(e, read)
}
func (a standardsWorkflowAdapter) Recover(ctx context.Context, p owneraction.Principal, f owneraction.Facts, key string, at time.Time) (durableworkflow.Output, bool, error) {
	out, found, e := a.standards.RecoverFrozen(ctx, p, workflowStandardsRequest(f, key, at), f)
	if found && f.Invocation.Command == "standards-analysis" && out.References["id"] != "" && a.standards.Analyses.RecoverFilings != nil {
		e = errors.Join(e, a.standards.Analyses.RecoverFilings(ctx, out.References["id"]))
	}
	if !found {
		return durableworkflow.Output{}, false, e
	}
	projection, read := a.project(ctx, f, out)
	return projection, true, errors.Join(e, read)
}
func (a standardsWorkflowAdapter) project(ctx context.Context, f owneraction.Facts, result messaging.StandardsResult) (durableworkflow.Output, error) {
	out := durableworkflow.Output{State: result.Status, Summary: result.Text, DomainRef: result.References["id"]}
	if out.State == "" {
		out.State = "succeeded"
	}
	if result.Interrupted {
		out.State = "uncertain"
	}
	if f.Invocation.Command == "standards-author" {
		id := out.DomainRef
		if id == "" {
			id = f.TargetID
		}
		session, e := a.standards.Authoring.Show(ctx, id, 0)
		if e != nil {
			return out, e
		}
		out.DomainRef = session.ID
		out.SessionRevision = session.Version
		for _, q := range session.Questions {
			if q.State == "pending" {
				out.Questions = append(out.Questions, durableworkflow.Question{ID: q.ID, Text: q.Text})
			}
		}
		if len(out.Questions) > 0 && out.State == "succeeded" {
			out.State = "waiting_for_input"
		}
		// Explicit display projection excludes private role captures and the complete book.
		display := standardsDisplay(session)
		raw, e := json.Marshal(display)
		if e != nil {
			return out, e
		}
		var item map[string]any
		if e = json.Unmarshal(raw, &item); e != nil {
			return out, e
		}
		out.Items = []map[string]any{item}
		out.Summary = "Standards draft retained; publication requires its separate existing approval"
	} else if out.DomainRef != "" {
		out.Summary = "Saved log analysis " + out.DomainRef
		analysis, e := a.db.GetLogAnalysis(ctx, out.DomainRef)
		if e != nil {
			return out, e
		}
		if analysis.State != "completed" && analysis.State != "failed" {
			out.State = "uncertain"
		}
		raw, e := a.db.GetLogAnalysisReport(ctx, out.DomainRef)
		if e != nil {
			return out, e
		}
		var report loganalysis.Report
		if e = json.Unmarshal(raw, &report); e != nil {
			return out, e
		}
		out.Summary = report.Summary
		out.Items = append(out.Items, map[string]any{"analysis_summary": report.Summary, "no_change_reason": report.NoChangeReason})
		for _, finding := range report.Findings {
			out.Items = append(out.Items, map[string]any{"finding_key": finding.Key, "kind": finding.Kind, "title": finding.Title, "expected": finding.Expected, "actual": finding.Actual, "cause": finding.Cause, "suggestion": finding.Suggestion, "file_requested": finding.File, "repository": finding.Repository, "target": finding.Target, "incident_ids": finding.IncidentIDs, "existing_issue": finding.Existing})
		}
		for _, proposal := range report.Proposals {
			raw, e := json.Marshal(proposal)
			if e != nil {
				return out, e
			}
			var item map[string]any
			if e = json.Unmarshal(raw, &item); e != nil {
				return out, e
			}
			out.Items = append(out.Items, map[string]any{"proposed_standard": item})
		}
		var selection loganalysis.Scope
		if e = json.Unmarshal(analysis.Scope, &selection); e != nil {
			return out, e
		}
		filings, e := a.db.ListLogAnalysisFilings(ctx, out.DomainRef)
		if e != nil {
			return out, e
		}
		recorded := map[string]bool{}
		for _, filing := range filings {
			recorded[filing.FindingKey] = true
		}
		for _, finding := range report.Findings {
			if finding.File && finding.Existing == "" && loganalysis.Route(selection, finding) != "team" && !recorded[finding.Key] {
				out.State = "uncertain"
			}
		}
		for _, filing := range filings {
			item := map[string]any{"finding_key": filing.FindingKey, "state": filing.State, "destination": filing.Destination}
			if filing.Reference != "" {
				item["issue_url"] = filing.URL
				item["reference"] = filing.Reference
			}
			if filing.State == store.FilingStarted || filing.State == store.FilingFailed {
				out.State = "uncertain"
			}
			out.Items = append(out.Items, item)
		}
	}
	return out, nil
}

func installedWorkflows(cfg []config.WorkflowRegistration) (*durableworkflow.Registry, error) {
	definitions := durableworkflow.BuiltinDefinitions()
	for _, entry := range cfg {
		var definition durableworkflow.Definition
		for _, builtin := range durableworkflow.BuiltinDefinitions() {
			if builtin.Adapter == entry.Adapter {
				definition = builtin
				break
			}
		}
		definition.Ref = entry.Ref
		definition.Version = entry.Version
		definition.BindingProject = entry.BindingProject
		definition.ProjectMode = durableworkflow.ProjectMode(entry.ProjectMode)
		definition.FixedProject = entry.FixedProject
		if entry.Name != "" {
			definition.Name = entry.Name
		}
		if entry.Purpose != "" {
			definition.Purpose = entry.Purpose
		}
		definitions = append(definitions, definition)
	}
	return durableworkflow.NewRegistry(definitions)
}
