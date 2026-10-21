// Package analystschedule coordinates opt-in recurring bounded analysts.
package analystschedule

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/drift"
	"github.com/cloudvane/litefactory/internal/loganalysis"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/prevention"
	"github.com/cloudvane/litefactory/internal/store"
)

// Config is immutable for an occurrence. All limits are lower-only upstream limits.
type Config struct {
	Name            string             `json:"name"`
	Kind            string             `json:"kind"`
	RepositoryID    int64              `json:"repository_id"`
	IntervalSeconds int64              `json:"interval_seconds"`
	After           time.Time          `json:"after,omitempty"`
	TaskRefs        []string           `json:"task_refs,omitempty"`
	Modes           []string           `json:"modes,omitempty"`
	RunLimit        int                `json:"run_limit"`
	LogLimits       loganalysis.Limits `json:"log_limits,omitempty"`
	BugLimits       prevention.Limits  `json:"bug_limits,omitempty"`
	DriftLimits     store.DriftLimits  `json:"drift_limits,omitempty"`
	Base            string             `json:"base,omitempty"`
	Ref             string             `json:"ref,omitempty"`
	Paths           []string           `json:"paths,omitempty"`
	Issues          []int              `json:"issues,omitempty"`
}

func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func digest(v any) string       { h := sha256.Sum256(raw(v)); return hex.EncodeToString(h[:]) }

func normalize(c Config) (Config, error) {
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" || len(c.Name) > 128 || strings.ContainsAny(c.Name, "\x00\r\n") || c.RepositoryID < 1 {
		return c, errors.New("schedule needs a name and registered repository")
	}
	if c.IntervalSeconds == 0 {
		c.IntervalSeconds = 86400
	}
	if c.IntervalSeconds < 3600 || c.IntervalSeconds > (1<<63-1)/int64(time.Second) {
		return c, errors.New("interval must be at least one hour and within the supported time range")
	}
	if c.RunLimit == 0 {
		c.RunLimit = 5
	}
	if c.RunLimit < 1 || c.RunLimit > 5 || len(c.TaskRefs) > 50 || len(c.Modes) > 20 {
		return c, errors.New("selection exceeds finite schedule limits")
	}
	for _, ref := range c.TaskRefs {
		if _, e := naming.ParseRef(ref); e != nil {
			return c, e
		}
	}
	for _, mode := range c.Modes {
		switch mode {
		case "research", "plan", "implement", "validate", "spike", "bug":
		default:
			return c, errors.New("invalid mode filter")
		}
	}
	if !c.After.IsZero() && c.After.Location() != time.UTC {
		return c, errors.New("initial start must be UTC")
	}
	switch c.Kind {
	case "logs":
		q, e := loganalysis.NormalizeRequest(loganalysis.Request{RepositoryIDs: []int64{c.RepositoryID}, RunIDs: []int64{1}, TaskRefs: c.TaskRefs, Limits: c.LogLimits})
		if e != nil {
			return c, e
		}
		c.LogLimits = q.Limits
		if c.RunLimit > c.LogLimits.Evidence.Runs {
			c.RunLimit = c.LogLimits.Evidence.Runs
		}
	case "bugs":
		if c.BugLimits == (prevention.Limits{}) {
			c.BugLimits = prevention.DefaultLimits()
		}
		if e := c.BugLimits.Validate(); e != nil {
			return c, e
		}
		if c.RunLimit > c.BugLimits.Issues {
			c.RunLimit = c.BugLimits.Issues
		}
		if len(c.Modes) > 0 && (len(c.Modes) != 1 || c.Modes[0] != "bug") {
			return c, errors.New("bug schedules select only Bug runs")
		}
		c.Modes = []string{"bug"}
	case "drift":
		if c.Base == "" || c.Ref == "" {
			return c, errors.New("drift requires an explicit initial base and local ref")
		}
		q, e := drift.NormalizeRequest(drift.Request{Repository: "owner/repo", Base: c.Base, Head: c.Ref, Paths: c.Paths, Issues: c.Issues, Limits: c.DriftLimits})
		if e != nil {
			return c, e
		}
		c.DriftLimits = q.Limits
		c.Paths = q.Paths
		c.Issues = q.Issues
	default:
		return c, errors.New("analyst must be logs, bugs or drift")
	}
	return c, nil
}

// Service shares the upstream services; scheduling adds no tools or provider authority.
type Service struct {
	DB    *store.DB
	Logs  *loganalysis.Service
	Bugs  *prevention.Service
	Drift func(context.Context, store.Repo) (*drift.Service, error)
	// Authorize checks current registration, credentials, connection and provider admission.
	Authorize func(context.Context, store.Repo, string) error
}

// Create saves a disabled schedule. It cannot start an analyst.
func (s *Service) Create(ctx context.Context, key, operator string, c Config, now time.Time) (store.AnalystSchedule, error) {
	c, e := normalize(c)
	if e != nil {
		return store.AnalystSchedule{}, e
	}
	if _, e = s.DB.GetRepo(ctx, c.RepositoryID); e != nil {
		return store.AnalystSchedule{}, e
	}
	return s.DB.CreateAnalystSchedule(ctx, store.AnalystSchedule{ID: rand.Text(), CreationKey: key, RequestDigest: digest(c), Name: c.Name, Kind: c.Kind, RepositoryID: c.RepositoryID, Config: raw(c), IntervalSeconds: c.IntervalSeconds, Operator: operator})
}

// Change affects future occurrences; repository and analyst identity are stable.
func (s *Service) Change(ctx context.Context, id string, version int64, operator string, c Config, now time.Time) (store.AnalystSchedule, error) {
	c, e := normalize(c)
	if e != nil {
		return store.AnalystSchedule{}, e
	}
	a, e := s.DB.GetAnalystSchedule(ctx, id)
	if e != nil {
		return a, e
	}
	if a.Kind != c.Kind || a.RepositoryID != c.RepositoryID {
		return a, errors.New("create another schedule to change its repository or analyst")
	}
	var old Config
	if e = json.Unmarshal(a.Config, &old); e != nil {
		return a, e
	}
	if c.After.IsZero() {
		c.After = old.After
	}
	return s.DB.MutateAnalystSchedule(ctx, id, version, store.AnalystScheduleMutation{Config: raw(c), Name: c.Name, IntervalSeconds: c.IntervalSeconds, Operator: operator}, now.UTC())
}

// Enable records explicit opt-in and the retained initial lower bound.
func (s *Service) Enable(ctx context.Context, id string, version int64, operator string, now time.Time) (store.AnalystSchedule, error) {
	a, e := s.DB.GetAnalystSchedule(ctx, id)
	if e != nil {
		return a, e
	}
	var c Config
	if e = json.Unmarshal(a.Config, &c); e != nil {
		return a, e
	}
	if c.After.IsZero() {
		c.After = now.UTC().Add(-7 * 24 * time.Hour)
	}
	enabled := true
	return s.DB.MutateAnalystSchedule(ctx, id, version, store.AnalystScheduleMutation{Config: raw(c), Enabled: &enabled, Operator: operator}, now.UTC())
}

// Disable prevents new upstream allocation without cancelling launched work.
func (s *Service) Disable(ctx context.Context, id string, version int64, operator string, now time.Time) (store.AnalystSchedule, error) {
	disabled := false
	return s.DB.MutateAnalystSchedule(ctx, id, version, store.AnalystScheduleMutation{Enabled: &disabled, Operator: operator}, now.UTC())
}

// Reanalyze records a deliberate new generation for one prior occurrence.
func (s *Service) Reanalyze(ctx context.Context, id string, version, occurrence int64, reason, operator string, now time.Time) (store.AnalystSchedule, error) {
	if strings.TrimSpace(reason) == "" || len(reason) > 1024 {
		return store.AnalystSchedule{}, errors.New("reanalysis requires a bounded reason")
	}
	return s.DB.MutateAnalystSchedule(ctx, id, version, store.AnalystScheduleMutation{Operator: operator, ReanalysisOf: occurrence, Reason: reason}, now.UTC())
}
