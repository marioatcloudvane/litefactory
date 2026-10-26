// Package overview projects local factory facts through a read-only boundary.
package overview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/connector"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// Reader is deliberately limited to local, context-aware observations.
type Reader interface {
	OverviewRepositories(context.Context) ([]store.OverviewRepository, bool, error)
	OverviewHealth(context.Context) (store.HealthSnapshot, error)
	ListOverviewActivity(context.Context) ([]store.OverviewActivity, bool, error)
	ListOverviewWaits(context.Context) (store.OverviewWaits, error)
	ListOverviewPublications(context.Context, time.Time, time.Time) (store.OverviewPublications, error)
}

// Service builds a bounded read-only projection of registered factory state.
type Service struct {
	reader    Reader
	redactor  *redact.Redactor
	now       func() time.Time
	hostRoots []string
}

// New composes local reads, outbound protection and an injected clock.
func New(reader Reader, redactor *redact.Redactor, now func() time.Time, hostRoots ...string) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{reader: reader, redactor: redactor, now: now, hostRoots: slices.Clone(hostRoots)}
}

// List keeps omissions visible even when its retained items are empty.
type List struct {
	Items     []map[string]any `json:"items"`
	HasMore   bool             `json:"has_more"`
	Truncated bool             `json:"truncated"`
	Warnings  []string         `json:"warnings"`
}

// Source separates local read time from the source observation time.
type Source struct {
	Status     string     `json:"status"`
	ReadAt     time.Time  `json:"read_at"`
	ObservedAt *time.Time `json:"observed_at"`
	Reasons    []string   `json:"reasons"`
}

// Coverage reports unavailable, stale and omitted observations.
type Coverage struct {
	Sources             map[string]Source `json:"sources"`
	Repositories        []map[string]any  `json:"repositories"`
	RepositoriesHasMore bool              `json:"repositories_has_more"`
	Warnings            []string          `json:"warnings"`
}

// Window is the inclusive UTC confirmation interval.
type Window struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// Result is the versioned transient owner overview.
type Result struct {
	Version            int            `json:"version"`
	GeneratedAt        time.Time      `json:"generated_at"`
	Window             Window         `json:"window"`
	Health             map[string]any `json:"health"`
	Activity           List           `json:"activity"`
	OwnerWaits         List           `json:"owner_waits"`
	RecentPublications List           `json:"recent_publications"`
	Coverage           Coverage       `json:"coverage"`
}

func emptyList() List { return List{Items: []map[string]any{}, Warnings: []string{}} }
func optional(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	t = t.UTC()
	return &t
}
func age(now, then time.Time) *int64 {
	if then.IsZero() || then.After(now) {
		return nil
	}
	v := int64(now.Sub(then) / time.Second)
	return &v
}

// Build chooses the window once. Sources are consistent individually, not one
// atomic cross-source snapshot. Source errors contain no outbound diagnostics.
func (s *Service) Build(ctx context.Context) (Result, error) {
	if s.reader == nil || s.redactor == nil {
		return Result{}, errors.New("overview unavailable")
	}
	now := s.now().UTC()
	r := Result{Version: 1, GeneratedAt: now, Window: Window{now.Add(-168 * time.Hour), now}, Health: map[string]any{"status": "unknown"}, Activity: emptyList(), OwnerWaits: emptyList(), RecentPublications: emptyList(), Coverage: Coverage{Sources: map[string]Source{}, Repositories: []map[string]any{}, Warnings: []string{"local_observations_not_remote_poll"}}}
	readable := 0
	source := func(name string, err error, observed *time.Time, status string, reasons ...string) {
		if err != nil {
			status = "unavailable"
			reasons = []string{"source_read_failed"}
		} else {
			readable++
		}
		if reasons == nil {
			reasons = []string{}
		}
		r.Coverage.Sources[name] = Source{Status: status, ReadAt: s.now().UTC(), ObservedAt: observed, Reasons: reasons}
	}
	repos, more, err := s.reader.OverviewRepositories(ctx)
	source("registry", err, nil, "complete")
	if err == nil {
		if len(repos) > store.OverviewLimit {
			repos = repos[:store.OverviewLimit]
			more = true
		}
		r.Coverage.RepositoriesHasMore = more
		if more {
			r.Coverage.Warnings = append(r.Coverage.Warnings, "repository_coverage_truncated")
			v := r.Coverage.Sources["registry"]
			v.Status = "truncated"
			r.Coverage.Sources["registry"] = v
		}
		for _, repo := range repos {
			repoStatus := "complete"
			repoReasons := []string{}
			if repo.ObservedAt.IsZero() || repo.ObservedAt.After(now) {
				repoStatus = "unknown"
				repoReasons = append(repoReasons, "connection_observation_unknown")
			} else if now.Sub(repo.ObservedAt) > 5*time.Minute {
				repoStatus = "stale"
				repoReasons = append(repoReasons, "connection_observation_stale")
			}
			if repo.ConnectionStatus == "missing_connection" || repo.ConnectionStatus == "disabled_connection" {
				repoStatus = "unavailable"
				repoReasons = append(repoReasons, repo.ConnectionStatus)
			}
			r.Coverage.Repositories = append(r.Coverage.Repositories, map[string]any{"repository_id": repo.ID, "repository": repo.Name, "connection_status": repo.ConnectionStatus, "observed_at": optional(repo.ObservedAt), "read_at": r.Coverage.Sources["registry"].ReadAt, "status": repoStatus, "reasons": repoReasons})
		}
	}
	health, err := s.reader.OverviewHealth(ctx)
	status := "unknown"
	var observed *time.Time
	reasons := []string{}
	if err == nil && health.Found {
		h := health.Listener
		observed = optional(h.WrittenAt)
		status = "complete"
		if h.WrittenAt.IsZero() || h.WrittenAt.After(now) {
			status = "unknown"
			reasons = append(reasons, "unknown_snapshot_time")
		} else if now.Sub(h.WrittenAt) > 5*time.Minute {
			status = "stale"
			reasons = append(reasons, "snapshot_older_than_five_minutes")
		}
		var slotsUsed, slotsTotal *int
		if h.SlotsTotal > 0 {
			slotsUsed = &h.SlotsUsed
			slotsTotal = &h.SlotsTotal
		}
		r.Health = map[string]any{"status": status, "written_at": observed, "heartbeat_at": optional(h.LastHeartbeatStartedAt), "heartbeat_age_seconds": age(now, h.LastHeartbeatStartedAt), "runs_in_flight": h.RunsInFlight, "slots_used": slotsUsed, "slots_total": slotsTotal, "discovery_paused": h.DiscoveryPaused, "pause_reason": h.PauseReason, "expected_resume_at": optional(h.ExpectedResumeAt), "runs_held": h.RunsHeld}
		if h.LastHeartbeatStartedAt.IsZero() {
			reasons = append(reasons, "heartbeat_unknown")
		}
		if h.SlotsTotal == 0 {
			reasons = append(reasons, "worker_capacity_unknown")
		}
	} else if err == nil {
		reasons = append(reasons, "snapshot_missing")
	}
	source("health", err, observed, status, reasons...)
	if err != nil {
		r.Health["status"] = "unavailable"
	}
	activity, more, err := s.reader.ListOverviewActivity(ctx)
	source("activity", err, nil, "complete")
	if err == nil {
		r.Activity.HasMore = more
		for _, a := range activity {
			r.Activity.Items = append(r.Activity.Items, map[string]any{"activity_id": a.ID, "kind": a.Kind, "repository_id": a.RepoID, "repository": a.Repository, "session_id": a.SessionID, "run_id": a.RunID, "task": a.TaskRef, "task_url": nullable(a.TaskURL), "workflow": a.Workflow, "stage": a.Stage, "state": a.State, "started_at": optional(a.StartedAt), "ticket": a.Ticket})
		}
	}
	waits, err := s.reader.ListOverviewWaits(ctx)
	source("waits", err, nil, "complete")
	if err == nil {
		r.OwnerWaits.HasMore = waits.HasMore
		r.OwnerWaits.Warnings = append(r.OwnerWaits.Warnings, waits.Warnings...)
		for _, w := range waits.Entries {
			var since time.Time
			if w.Since != nil {
				since = *w.Since
			}
			r.OwnerWaits.Items = append(r.OwnerWaits.Items, map[string]any{"wait_id": w.WaitID, "kind": w.Kind, "repository_id": w.RepoID, "repository": w.Repository, "session_id": w.SessionID, "task": w.TaskRef, "task_url": nullable(w.TaskURL), "workflow": w.Workflow, "stage": w.Stage, "summary": w.Summary, "url": nullable(w.URL), "since": optional(since), "waiting_age_seconds": age(now, since), "time_provenance": w.SinceProvenance, "ticket": w.Ticket})
		}
	}
	pubs, err := s.reader.ListOverviewPublications(ctx, r.Window.From, r.Window.To)
	source("publications", err, nil, "complete")
	if err == nil {
		r.RecentPublications.HasMore = pubs.HasMore
		r.RecentPublications.Warnings = append(r.RecentPublications.Warnings, pubs.Warnings...)
		if pubs.Undated {
			r.RecentPublications.Warnings = append(r.RecentPublications.Warnings, "undated_historical_confirmations_excluded")
		}
		for _, p := range pubs.Entries {
			r.RecentPublications.Items = append(r.RecentPublications.Items, map[string]any{"delivery_key": p.Key, "kind": p.Kind, "repository_id": p.RepoID, "repository": p.Repository, "session_id": p.SessionID, "run_id": p.RunID, "task": p.TaskRef, "url": nullable(p.URL), "namespace": p.Namespace, "workflow": p.Workflow, "graph_instance_id": p.GraphInstanceID, "source_attempt_id": p.SourceAttemptID, "purpose": p.Purpose, "stage": p.Stage, "summary": p.Summary, "confirmed_at": p.ConfirmedAt.UTC(), "ticket": p.Ticket})
		}
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if readable == 0 {
		return Result{}, errors.New("overview sources unavailable")
	}
	for name, list := range map[string]*List{"activity": &r.Activity, "waits": &r.OwnerWaits, "publications": &r.RecentPublications} {
		if len(list.Items) > store.OverviewLimit {
			list.Items = list.Items[:store.OverviewLimit]
			list.HasMore = true
		}
		if list.HasMore {
			list.Truncated = true
			reason := "entries_omitted"
			if len(list.Items) == store.OverviewLimit {
				reason = "entry_limit"
			}
			list.Warnings = appendUnique(list.Warnings, reason)
		}
		for _, item := range list.Items {
			for _, key := range []string{"url", "task_url"} {
				if text, ok := item[key].(string); ok && !safeLink(text) {
					item[key] = nil
					list.Warnings = appendUnique(list.Warnings, "unsafe_link_omitted")
				}
			}
			if value, ok := item["url"]; ok && value == nil {
				list.Warnings = appendUnique(list.Warnings, "publication_link_missing")
			}
			if _, ok := item["task_url"]; ok && item["task_url"] == nil {
				list.Warnings = appendUnique(list.Warnings, "task_link_missing")
			}
		}
		if len(list.Warnings) > 0 {
			v := r.Coverage.Sources[name]
			if v.Status == "complete" {
				v.Status = "unknown"
			}
			if list.HasMore {
				v.Status = "truncated"
			}
			v.Reasons = append(v.Reasons, list.Warnings...)
			r.Coverage.Sources[name] = v
		}
	}
	// Protect every variable string, including retained destination context.
	if err := s.protect(ctx, &r); err != nil {
		return Result{}, err
	}
	for name, list := range map[string]*List{"activity": &r.Activity, "waits": &r.OwnerWaits, "publications": &r.RecentPublications} {
		if serializationCost(list) <= 44*1024 {
			continue
		}
		items := list.Items
		list.HasMore = true
		list.Truncated = true
		list.Warnings = appendUnique(list.Warnings, "byte_limit")
		low, high := 0, len(items)
		for low < high {
			mid := (low + high + 1) / 2
			list.Items = items[:mid]
			if serializationCost(list) <= 44*1024 {
				low = mid
			} else {
				high = mid - 1
			}
		}
		list.Items = items[:low]
		if serializationCost(list) > 44*1024 {
			return Result{}, errors.New("overview coverage exceeds budget")
		}
		v := r.Coverage.Sources[name]
		v.Status = "truncated"
		v.Reasons = appendUnique(v.Reasons, "byte_limit")
		r.Coverage.Sources[name] = v
	}
	if serializationCost(r.Coverage) > 32*1024 {
		repos := r.Coverage.Repositories
		r.Coverage.RepositoriesHasMore = true
		r.Coverage.Warnings = appendUnique(r.Coverage.Warnings, "repository_coverage_byte_limit")
		v := r.Coverage.Sources["registry"]
		v.Status = "truncated"
		r.Coverage.Sources["registry"] = v
		low, high := 0, len(repos)
		for low < high {
			mid := (low + high + 1) / 2
			r.Coverage.Repositories = repos[:mid]
			if serializationCost(r.Coverage) <= 32*1024 {
				low = mid
			} else {
				high = mid - 1
			}
		}
		r.Coverage.Repositories = repos[:low]
		if serializationCost(r.Coverage) > 32*1024 {
			return Result{}, errors.New("overview coverage exceeds budget")
		}
	}
	return r, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func appendUnique(xs []string, s string) []string {
	for _, x := range xs {
		if x == s {
			return xs
		}
	}
	return append(xs, s)
}
func bounded(s string, n int) string {
	s = strings.ToValidUTF8(s, "")
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

func (s *Service) protect(ctx context.Context, r *Result) error {
	// Round-tripping through JSON makes the string hygiene exhaustive, including
	// fields added later. Fixed scalar timestamps remain unchanged by redaction.
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	var v any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err = decoder.Decode(&v); err != nil {
		return err
	}
	displayOmitted := false
	type displayKey struct {
		text  string
		limit int
	}
	cached := map[displayKey]string{}
	var clean func(any, string) (any, error)
	clean = func(value any, key string) (any, error) {
		switch x := value.(type) {
		case string:
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			limit := 2048
			if key == "summary" || key == "pause_reason" || key == "runs_held" {
				limit = 512
			}
			if (key == "url" || key == "task_url") && x != "" && !safeLink(x) {
				return "", nil
			}
			cacheKey := displayKey{x, limit}
			if previous, ok := cached[cacheKey]; ok {
				return previous, nil
			}
			b, e := s.redactor.ApplyReviewDisplay(ctx, report.StripHostPaths(bounded(x, 8192), s.hostRoots), 16384)
			if e != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				displayOmitted = true
				cached[cacheKey] = "[display unavailable]"
				return "[display unavailable]", nil
			}
			protected := bounded(b.String(), limit)
			cached[cacheKey] = protected
			return protected, nil
		case []any:
			for i := range x {
				var e error
				x[i], e = clean(x[i], key)
				if e != nil {
					return nil, e
				}
			}
		case map[string]any:
			for k := range x {
				var e error
				x[k], e = clean(x[k], k)
				if e != nil {
					return nil, e
				}
			}
		}
		return value, nil
	}
	v, err = clean(v, "")
	if err != nil {
		return err
	}
	raw, err = json.Marshal(v)
	if err != nil {
		return err
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err = decoder.Decode(r); err != nil {
		return err
	}
	if displayOmitted {
		r.Coverage.Warnings = appendUnique(r.Coverage.Warnings, "display_protection_omitted_fields")
	}
	return nil
}

// Overview returns the same retained facts in JSON text and structured content.
// Independent list budgets reserve room for health and coverage in both copies.
func (s *Service) Overview(ctx context.Context, args json.RawMessage) (connector.ToolResult, error) {
	var input map[string]json.RawMessage
	if len(bytes.TrimSpace(args)) == 0 || json.Unmarshal(args, &input) != nil || input == nil || len(input) != 0 {
		return connector.ToolResult{}, errors.New("overview requires empty object")
	}
	r, err := s.Build(ctx)
	if err != nil {
		return connector.ToolResult{}, err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return connector.ToolResult{}, err
	}
	result := connector.ToolResult{Content: []connector.TextContent{{Type: "text", Text: string(raw)}}, StructuredContent: raw}
	encoded, err := json.Marshal(result)
	if err != nil {
		return connector.ToolResult{}, err
	}
	if len(encoded) > connector.MaxResponseBytes-connector.MaxRequestBytes-1024 {
		return connector.ToolResult{}, errors.New("overview exceeds response budget")
	}
	return result, nil
}

func safeLink(s string) bool {
	if len(s) > 2048 || strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return false
	}
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.RawQuery == "" && !u.ForceQuery && u.Opaque == ""
}

// Values passed here have already survived JSON encoding during protection.
// Include the escaped text copy as well as the structured representation.
func serializationCost(value any) int {
	raw, _ := json.Marshal(value)
	escaped, _ := json.Marshal(string(raw))
	return len(raw) + len(escaped)
}
