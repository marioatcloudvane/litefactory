package loganalysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/store"
)

// Marker identifies the issue filed for one finding of one analysis. A retry
// finds the issue by it instead of filing a second one.
func Marker(analysisID, key string) string {
	return fmt.Sprintf("<!-- lf:log-analysis analysis=%s finding=%s -->", analysisID, key)
}

// Route says who can fix a finding and where its report belongs: "project"
// (the analysed project's ticket source), "upstream" (litefactory's maintainer)
// or "team" (a suggested change to the owner's own team, never filed).
func Route(scope Scope, f Finding) string {
	switch f.Kind {
	case FindingProjectSetup:
		return "project"
	case FindingRolePrompt:
		for _, r := range scope.Repositories {
			if r.TeamOwned && (f.Repository == "" && len(scope.Repositories) == 1 || strings.EqualFold(r.Name, f.Repository)) {
				return "team"
			}
		}
	}
	return "upstream"
}

// IssueText renders a finding as ready-to-file issue text. excerpts are short
// cited log excerpts; tasks names each incident's task where known.
func IssueText(analysisID string, f Finding, tasks map[string]string, excerpts []string) (string, string) {
	var b strings.Builder
	fmt.Fprintf(&b, "Found by litefactory log analysis `%s` (%s, %s).\n\n", analysisID, strings.ReplaceAll(f.Kind, "_", " "), f.Target)
	fmt.Fprintf(&b, "## Expected\n\n%s\n\n## Actual\n\n%s\n\n## Cause\n\n%s\n\n## Suggestion\n\n%s\n\n## Incidents\n\n", f.Expected, f.Actual, f.Cause, f.Suggestion)
	for _, id := range f.IncidentIDs {
		if task := tasks[id]; task != "" {
			fmt.Fprintf(&b, "- `%s` in %s\n", id, task)
		} else {
			fmt.Fprintf(&b, "- `%s`\n", id)
		}
	}
	if len(f.Citations) > 0 {
		b.WriteString("\nCited log ranges: ")
		parts := []string{}
		for _, c := range f.Citations {
			parts = append(parts, fmt.Sprintf("%s run %d [%d:%d]", c.TaskRef, c.RunID, c.Start, c.End))
		}
		b.WriteString(strings.Join(parts, ", ") + "\n")
	}
	for _, e := range excerpts {
		fmt.Fprintf(&b, "\n```text\n%s\n```\n", strings.ReplaceAll(e, "```", "'''"))
	}
	if f.Existing != "" {
		fmt.Fprintf(&b, "\nRelated existing issue: %s\n", f.Existing)
	}
	b.WriteString("\n" + Marker(analysisID, f.Key) + "\n")
	return f.Title, b.String()
}

// FileFindings executes the accepted report's filing decisions: each finding
// marked file goes to its destination once. Dry runs record what would be
// filed. Settled findings are skipped, so a retry never files twice; a
// destination failure is recorded on its finding, not returned.
func (s *Service) FileFindings(ctx context.Context, id string) error {
	a, e := s.DB.GetLogAnalysis(ctx, id)
	if e != nil {
		return e
	}
	if a.State != "completed" {
		return errors.New("only an accepted analysis files its findings")
	}
	var scope Scope
	if e = json.Unmarshal(a.Scope, &scope); e != nil {
		return e
	}
	body, e := s.DB.GetLogAnalysisReport(ctx, id)
	if e != nil {
		return e
	}
	var report Report
	if e = json.Unmarshal(body, &report); e != nil {
		return e
	}
	prior, e := s.DB.ListLogAnalysisFilings(ctx, id)
	if e != nil {
		return e
	}
	settled := map[string]bool{}
	for _, f := range prior {
		settled[f.FindingKey] = f.State == store.FilingFiled || f.State == store.FilingWouldFile
	}
	tasks, e := s.incidentTasks(ctx, id)
	if e != nil {
		return e
	}
	filed := 0
	for _, f := range report.Findings {
		if !f.File || f.Existing != "" || Route(scope, f) == "team" {
			continue
		}
		// Validation caps this already; the host never exceeds it either.
		if filed++; filed > maxFiled {
			break
		}
		if settled[f.Key] {
			continue
		}
		if e = s.fileFinding(ctx, a, scope, f, tasks); e != nil {
			return e
		}
	}
	return nil
}

func (s *Service) fileFinding(ctx context.Context, a store.LogAnalysis, scope Scope, f Finding, tasks map[string]string) error {
	destination, tracker, reason := s.destination(scope, f)
	titleText, bodyText := IssueText(a.ID, f, tasks, s.excerpts(ctx, a.ID, f))
	title, e := s.Redactor.Apply(ctx, titleText, 256)
	if e != nil {
		return e
	}
	body, e := s.Redactor.Apply(ctx, bodyText, 60<<10)
	if e != nil {
		return e
	}
	record := store.LogAnalysisFiling{AnalysisID: a.ID, FindingKey: f.Key, Destination: destination, Title: title.String(), Body: body.String()}
	save := func(state, reference, url, why string) error {
		record.State, record.Reference, record.URL, record.Reason = state, reference, url, why
		_, e := s.DB.SaveLogAnalysisFiling(ctx, record)
		return e
	}
	switch {
	case scope.Request.DryRun:
		return save(store.FilingWouldFile, "", "", reason)
	case tracker == nil:
		return save(store.FilingNotFiled, "", "", reason)
	}
	if e = save(store.FilingStarted, "", "", ""); e != nil {
		return e
	}
	hit, e := tracker.FileBug(ctx, title, body, Marker(a.ID, f.Key), a.CreatedAt)
	var refused NotFiled
	switch {
	case errors.As(e, &refused):
		return save(store.FilingNotFiled, "", "", string(refused))
	case e != nil && hit.Reference != "":
		return save(store.FilingFailed, hit.Reference, hit.URL, s.safeReason(ctx, e))
	case e != nil:
		return save(store.FilingFailed, "", "", s.safeReason(ctx, e))
	}
	return save(store.FilingFiled, hit.Reference, hit.URL, "")
}

// destination returns where a finding is filed, or why it cannot be.
func (s *Service) destination(scope Scope, f Finding) (string, Tracker, string) {
	if Route(scope, f) == "upstream" {
		switch {
		case scope.Upstream == "":
			return "upstream", nil, "no upstream tracker is configured (standards.log_analysis.upstream); this is a ready-to-file upstream report"
		case s.Upstream == nil:
			return scope.Upstream, nil, "the upstream tracker " + scope.Upstream + " is unavailable (no GitHub credential)"
		}
		return scope.Upstream, s.Upstream, ""
	}
	for _, r := range scope.Repositories {
		if f.Repository == "" && len(scope.Repositories) == 1 || strings.EqualFold(r.Name, f.Repository) {
			if t := s.Projects[r.ID]; t != nil {
				return t.Name(), t, ""
			}
			return r.Name, nil, "the ticket source of " + r.Name + " is unavailable"
		}
	}
	return "project", nil, "the finding names no analysed repository"
}

func (s *Service) safeReason(ctx context.Context, e error) string {
	safe, err := s.Redactor.Apply(ctx, e.Error(), 512)
	if err != nil {
		return "filing failed"
	}
	return safe.String()
}

// incidentTasks maps each incident offered by this analysis to its task.
func (s *Service) incidentTasks(ctx context.Context, id string) (map[string]string, error) {
	rows, e := allEvidence(ctx, s.DB, id)
	if e != nil {
		return nil, e
	}
	out := map[string]string{}
	for _, row := range rows {
		var v Evidence
		if json.Unmarshal(row.Metadata, &v) == nil {
			for k, task := range v.IncidentTasks {
				out[k] = task
			}
		}
	}
	return out, nil
}

// excerpts rereads at most two cited ranges, shortened, for the issue text.
// A reread that fails leaves the excerpt out; the citation still names it.
func (s *Service) excerpts(ctx context.Context, id string, f Finding) []string {
	out := []string{}
	for _, c := range f.Citations {
		if len(out) == 2 {
			break
		}
		r, e := s.Reread(ctx, id, c)
		if e != nil || len(r.Records) == 0 {
			continue
		}
		text := strings.TrimSpace(r.Records[0].Text)
		if len(text) > 400 {
			text = strings.ToValidUTF8(text[:400], "") + " …"
		}
		out = append(out, text)
	}
	return out
}
