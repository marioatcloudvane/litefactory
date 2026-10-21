// Package drift owns bounded, retained analysis of pinned changes. It grants no
// source mutation, approval, publication or workflow transition authority.
package drift

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

// Request declares immutable source selectors and lower-only work ceilings.
type Request struct {
	Repository string            `json:"repository"`
	Base       string            `json:"base"`
	Head       string            `json:"head"`
	Paths      []string          `json:"paths"`
	Issues     []int             `json:"issues"`
	Key        string            `json:"key,omitempty"`
	Limits     store.DriftLimits `json:"limits"`
}

// Runner receives only the bound invocation, never source or store access.
type Runner interface {
	Isolation(context.Context) (string, error)
	Run(context.Context, *Invocation) error
}

// Service owns standalone analysis allocation, acquisition and finalization.
type Service struct {
	Store         *store.DB
	Acquisition   *Acquirer
	Runner        Runner
	OnCreated     func(store.DriftAnalysis) error
	LaunchRefusal string
}

// Inspection joins retained analysis facts with freshly verified standards history.
type Inspection struct {
	Analysis  store.DriftAnalysis     `json:"analysis"`
	Standards standards.RunInspection `json:"standards"`
}

// DefaultLimits returns the fixed hard ceilings for one bounded analysis.
func DefaultLimits() store.DriftLimits {
	return store.DriftLimits{Paths: 200, Issues: 10, Comments: 100, Requests: 20, SourceBytes: 8 << 20, ItemBytes: 256 << 10, AcquisitionSeconds: 300, AgentSeconds: 600, Calls: 40, OutboundBytes: 256 << 10, ResultBytes: 128 << 10, Proposals: 10}
}

func normalize(r Request) (Request, error) {
	if strings.TrimSpace(r.Repository) == "" || r.Base == "" || r.Head == "" || len(r.Base) > 1024 || len(r.Head) > 1024 || len(r.Key) > 256 || strings.ContainsAny(r.Base+r.Head, "\x00\r\n") {
		return r, errors.New("repository and explicit base/head are required")
	}
	if len(strings.Split(r.Repository, "/")) != 2 {
		return r, errors.New("repository must be owner/name")
	}
	d := DefaultLimits()
	if r.Limits == (store.DriftLimits{}) {
		r.Limits = d
	}
	l := r.Limits
	if l.Paths < 1 || l.Paths > d.Paths || l.Issues < 0 || l.Issues > d.Issues || l.Comments < 0 || l.Comments > d.Comments || l.Requests < 0 || l.Requests > d.Requests || l.SourceBytes < 1 || l.SourceBytes > d.SourceBytes || l.ItemBytes < 1 || l.ItemBytes > d.ItemBytes || l.AcquisitionSeconds < 1 || l.AcquisitionSeconds > d.AcquisitionSeconds || l.AgentSeconds < 1 || l.AgentSeconds > d.AgentSeconds || l.Calls < 4 || l.Calls > d.Calls || l.OutboundBytes < 8192 || l.OutboundBytes > d.OutboundBytes || l.ResultBytes < 1 || l.ResultBytes > d.ResultBytes || l.Proposals < 0 || l.Proposals > d.Proposals {
		return r, errors.New("analysis limits may only lower the hard ceilings and must preserve delivery reserves")
	}
	r.Paths = slices.Clone(r.Paths)
	r.Issues = slices.Clone(r.Issues)
	if len(r.Paths) > 200 {
		return r, errors.New("too many literal path prefixes")
	}
	for n, p := range r.Paths {
		p = strings.TrimSuffix(p, "/")
		r.Paths[n] = p
		if p == "" || len(p) > 4096 || path.IsAbs(p) || path.Clean(p) != p || p == ".." || strings.HasPrefix(p, "../") || strings.ContainsRune(p, '\x00') {
			return r, errors.New("paths must be clean relative literal prefixes")
		}
	}
	sort.Strings(r.Paths)
	r.Paths = slices.Compact(r.Paths)
	sort.Ints(r.Issues)
	r.Issues = slices.Compact(r.Issues)
	if len(r.Issues) > l.Issues {
		return r, errors.New("issue selector ceiling exceeded")
	}
	for _, n := range r.Issues {
		if n < 1 {
			return r, errors.New("issue numbers must be positive")
		}
	}
	return r, nil
}
func bytesJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func digest(b []byte) string          { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func newID() (string, error) {
	var b [16]byte
	_, e := rand.Read(b[:])
	return "drift-" + hex.EncodeToString(b[:]), e
}
func runtimeFor(db *store.DB, a store.DriftAnalysis) *standards.Runtime {
	return &standards.Runtime{Store: db, RootRunID: a.RunID, ConsumerKey: "drift-analyst", AttemptID: fmt.Sprintf("%s/%d", a.ID, a.Generation)}
}

// Start allocates before external work, launches at most once, and never resumes
// an existing start key with a fresh budget or newly published book.
func (s *Service) Start(ctx context.Context, r Request) (out store.DriftAnalysis, failure error) {
	r, err := normalize(r)
	if err != nil {
		return out, err
	}
	if s.Store == nil || s.Acquisition == nil {
		return out, errors.New("drift storage and acquisition are required")
	}
	id, err := newID()
	if err != nil {
		return out, err
	}
	release, err := s.ownAnalysis(ctx, id)
	if err != nil {
		return out, err
	}
	defer func() { failure = errors.Join(failure, release()) }()
	raw := bytesJSON(r)
	a, created, err := s.Store.CreateDriftAnalysis(ctx, store.DriftAnalysis{ID: id, RunID: id, Repository: r.Repository, RequestKey: r.Key, RequestDigest: digest(raw), Scope: raw, Limits: r.Limits})
	if err != nil {
		return a, err
	}
	if !created {
		if err := s.reconcile(ctx, a.ID); err != nil {
			return a, err
		}
		return s.Store.GetDriftAnalysis(ctx, a.ID)
	}
	if s.OnCreated != nil {
		if err = s.OnCreated(a); err != nil {
			_, e := s.Store.InterruptDriftAnalysis(context.WithoutCancel(ctx), a.ID, a.Generation, "analysis output interrupted")
			return a, errors.Join(err, e)
		}
	}
	defer func() {
		current, e := s.Store.GetDriftAnalysis(context.WithoutCancel(ctx), a.ID)
		if e != nil {
			failure = errors.Join(failure, e)
			return
		}
		if current.Execution == "running" {
			reason := "analyst returned without an accepted report"
			state := "failed"
			if failure != nil {
				reason = "analysis failed before accepted report"
				if s.Runner == nil && s.LaunchRefusal != "" {
					reason = s.LaunchRefusal
				}
			}
			if ctx.Err() != nil || errors.Is(failure, store.ErrDriftBudget) || errors.Is(failure, context.Canceled) || errors.Is(failure, context.DeadlineExceeded) {
				reason = "analysis interrupted or work allowance exhausted"
				state = "incomplete"
			}
			current.Execution = state
			current.Disposition = "not_evaluated"
			current.Reason = reason
			current, e = s.Store.UpdateDriftAnalysis(context.WithoutCancel(ctx), current, current.Generation, current.Version)
			failure = errors.Join(failure, e)
		}
		out = current
	}()
	rt := runtimeFor(s.Store, a)
	status, err := rt.Bind(ctx, true)
	if err != nil {
		return a, err
	}
	a.Standards = bytesJSON(status)
	a, err = s.Store.UpdateDriftAnalysis(ctx, a, a.Generation, a.Version)
	if err != nil {
		return a, err
	}
	acquireCtx, cancel := context.WithTimeout(ctx, time.Duration(r.Limits.AcquisitionSeconds)*time.Second)
	pinned, coverage, acquireErr := s.Acquisition.Acquire(acquireCtx, a, r)
	cancel()
	a, err = s.Store.GetDriftAnalysis(context.WithoutCancel(ctx), a.ID)
	if err != nil {
		return a, err
	}
	a.Pinned = pinned
	a.Coverage = coverage
	a, err = s.Store.UpdateDriftAnalysis(context.WithoutCancel(ctx), a, a.Generation, a.Version)
	if err != nil {
		return a, err
	}
	if acquireErr != nil {
		return a, acquireErr
	}
	if s.Runner == nil {
		if s.LaunchRefusal != "" {
			return a, errors.New(s.LaunchRefusal)
		}
		return a, errors.New("restricted drift provider unavailable; no broader runner fallback")
	}
	runCtx, stop := context.WithTimeout(ctx, time.Duration(a.Limits.AgentSeconds)*time.Second)
	defer stop()
	profile, err := s.Runner.Isolation(runCtx)
	if err != nil {
		return a, err
	}
	a.Provenance = bytesJSON(map[string]string{"role": "drift-analyst-v1", "profile": profile, "role_digest": digest([]byte(analystRole)), "schema_digest": digest(resultSchema)})
	a, err = s.Store.UpdateDriftAnalysis(runCtx, a, a.Generation, a.Version)
	if err != nil {
		return a, err
	}
	a, err = s.Store.ChargeDriftBudget(runCtx, a.ID, a.Generation, store.DriftUsage{Invocations: 1}, nil, true)
	if err != nil {
		return a, err
	}
	invocation := &Invocation{store: s.Store, analysis: a, runtime: rt}
	err = s.Runner.Run(runCtx, invocation)
	if runCtx.Err() != nil {
		err = errors.Join(err, store.ErrDriftBudget)
	} else if invocation.exhausted {
		current, readErr := s.Store.GetDriftAnalysis(ctx, a.ID)
		if readErr != nil {
			err = errors.Join(err, readErr)
		} else if len(current.Report) == 0 {
			err = errors.Join(err, store.ErrDriftBudget)
		}
	}
	return a, err
}

// Inspect never launches, repins or reacquires. Running records are visibly unfinished.
func (s *Service) Inspect(ctx context.Context, id string) (Inspection, error) {
	if err := s.reconcile(ctx, id); err != nil {
		return Inspection{}, err
	}
	a, e := s.Store.GetDriftAnalysis(ctx, id)
	if e != nil {
		return Inspection{}, e
	}
	v, e := runtimeFor(s.Store, a).Inspect(ctx)
	return Inspection{Analysis: a, Standards: v}, e
}

// ExpireEvidence applies explicit retention to a terminal standalone invocation.
// The ownership lease prevents removal while an analyst is still consuming data.
func (s *Service) ExpireEvidence(ctx context.Context, id string) (failure error) {
	release, err := s.ownAnalysis(ctx, id)
	if err != nil {
		return err
	}
	defer func() { failure = errors.Join(failure, release()) }()
	_, err = s.Store.ExpireDriftEvidence(ctx, id)
	return err
}

// NormalizeRequest validates and fills lower-only limits without launching work.
func NormalizeRequest(q Request) (Request, error) { return normalize(q) }
