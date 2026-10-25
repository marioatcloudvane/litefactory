package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// ImplementStory is one story of the frozen story set, as the factory knows it.
// Dependencies are the keys of the stories that must be merged before it starts.
type ImplementStory struct {
	Key             string
	Name            string
	Branch          string
	AlreadyComplete bool
	IssueNumber     int
	Dependencies    []string
}

// ImplementAuthority is factory-only context for the Implement activities. It
// is never decoded from agent text or node attributes: Stories are in plan
// order and IntegrationBranch is the feature line stories merge into, never the
// repository default branch. BaseBranch is the repository's base branch the base
// sync brings into the integration branch.
type ImplementAuthority struct {
	Parent            naming.Ref
	Generation        int64
	Stories           []ImplementStory
	IntegrationBranch string
	BaseBranch        string
}

func (a ImplementAuthority) validate() error {
	if err := a.Parent.Validate(); err != nil {
		return fmt.Errorf("implement parent: %w", err)
	}
	if a.Generation < 1 {
		return errors.New("implement authority needs a generation")
	}
	seen := map[string]bool{}
	for _, s := range a.Stories {
		if strings.TrimSpace(s.Key) == "" || strings.TrimSpace(s.Branch) == "" || seen[s.Key] {
			return fmt.Errorf("implement story %q is blank, has no branch or repeats", s.Key)
		}
		seen[s.Key] = true
	}
	return nil
}

func (a ImplementAuthority) story(key string) (ImplementStory, bool) {
	for _, s := range a.Stories {
		if s.Key == key {
			return s, true
		}
	}
	return ImplementStory{}, false
}

func (a ImplementAuthority) seeds() []store.ImplementStorySeed {
	seeds := make([]store.ImplementStorySeed, len(a.Stories))
	for i, s := range a.Stories {
		seeds[i] = store.ImplementStorySeed{Key: s.Key, Position: i, Branch: s.Branch}
	}
	return seeds
}

// ImplementBind supplies the authority from factory state.
type ImplementBind func(context.Context, Options, ActivityRequest) (ImplementAuthority, error)

// ImplementForge is the narrow, redacted forge surface the merge note and the
// hand-off report use.
type ImplementForge interface {
	ListCommentsPage(ctx context.Context, repo github.Repo, issue, page, limit int) (github.CommentPage, error)
	PostComment(context.Context, github.Repo, int, redact.Body) (github.Comment, error)
	EditComment(context.Context, github.Repo, int64, redact.Body) error
}

// ImplementBindings carries what the three activities need. Bind is the one
// trusted source of the parent, the story set and the integration branch.
type ImplementBindings struct {
	DB       *store.DB
	Forge    ImplementForge
	Redactor *redact.Redactor
	LockDir  string
	Repo     ImplementRepo
	// Now overrides the clock; nil uses the run's clock.
	Now              func() time.Time
	Bind             ImplementBind
	ProgressRead     func(context.Context, string, int64) ([]store.ImplementProgressReport, error)
	StatusBind       ImplementBind
	Status           ImplementStatusRefresher
	AdvanceReturn    func(context.Context, string, string, string) (bool, error)
	ReturnAllocation func(context.Context, ActivityRequest) ([]agentresult.ReturnAllocation, error)
}

// RegisterImplementActivities registers the three Implement factory activities
// and the coordinator's binding of the generic base sync in both registries. It
// adds no workflow policy and no graph.
func RegisterImplementActivities(specs *graphexec.Registry, activities *ActivityRegistry, b ImplementBindings) error {
	if specs == nil || activities == nil || b.DB == nil || b.Forge == nil || b.Redactor == nil || b.LockDir == "" || b.Repo == nil || b.Bind == nil {
		return errors.New("implement activities require registries, store, forge, redactor, lock directory, repository and bind")
	}
	for _, step := range []struct {
		spec    graphexec.ActionSpec
		handler ActivityHandler
	}{
		{graphexec.ImplementReadySpec(), ImplementReadyActivity{b}},
		{graphexec.ImplementMergeSpec(), ImplementMergeActivity{b}},
		{graphexec.ImplementHandoffSpec(), ImplementHandoffActivity{b}},
	} {
		if err := specs.Register(step.spec); err != nil {
			return err
		}
		if err := activities.Register(step.spec.Name, step.handler); err != nil {
			return err
		}
	}
	return RegisterSyncBase(specs, activities, agent.ModeImplement, SyncBaseBinding{Repo: b.Repo, Bind: b.implementSyncBase})
}

// implementSyncBase binds lf_sync_base for the Implement coordinator: the
// working branch is the feature (integration) branch, the base is the
// repository's base branch, and the lock is the parent's story progress lock the
// merge step holds too.
func (b ImplementBindings) implementSyncBase(ctx context.Context, opts Options, req ActivityRequest) (SyncBaseTarget, error) {
	a, err := b.bind(ctx, opts, req)
	if err != nil {
		return SyncBaseTarget{}, err
	}
	parent := a.Parent.String()
	return SyncBaseTarget{Working: a.IntegrationBranch, Base: a.BaseBranch, Lock: func(ctx context.Context) (func() error, bool, error) {
		unlock, err := b.DB.LockStoryProgress(ctx, parent, b.LockDir)
		if errors.Is(err, store.ErrStoryProgressBusy) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		return unlock, true, nil
	}}, nil
}

func (b ImplementBindings) now(opts Options) time.Time {
	if b.Now != nil {
		return b.Now().UTC()
	}
	return spikeNow(opts)
}

// bind refuses a request that is not Implement and returns validated authority.
func (b ImplementBindings) bind(ctx context.Context, opts Options, req ActivityRequest) (ImplementAuthority, error) {
	if req.Mode != agent.ModeImplement || b.Bind == nil {
		return ImplementAuthority{}, errors.New("implement activity requires admitted Implement authority")
	}
	a, err := b.Bind(ctx, opts, req)
	if err != nil {
		return ImplementAuthority{}, err
	}
	if err := a.validate(); err != nil {
		return ImplementAuthority{}, err
	}
	// Completed stories may leave the unfinished authority empty. Retained
	// ledger rows still need reconciliation before the coordinator can finish.
	if len(a.Stories) == 0 {
		rows, err := b.DB.ListImplementStories(ctx, a.Parent.String(), a.Generation)
		if err != nil {
			return ImplementAuthority{}, err
		}
		if len(rows) == 0 {
			return ImplementAuthority{}, errors.New("implement authority needs a story set or retained ledger")
		}
	}
	return a, nil
}

func implementResult(outcome string, payload any) (ActivityResult, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return ActivityResult{}, err
	}
	return ActivityResult{Outcome: outcome, Kind: store.GraphOutputJSON, Output: data}, nil
}

// ImplementReadyActivity starts the stories whose prerequisites are merged.
type ImplementReadyActivity struct{ b ImplementBindings }

type implementStartedStory struct {
	Key    string `json:"key"`
	Branch string `json:"branch"`
}

type implementReadyOutput struct {
	Started []implementStartedStory `json:"started"`
	Running int                     `json:"running"`
	Waiting []string                `json:"waiting"`
}

// Execute moves up to (cap - running) ready stories to running, in plan order.
//
//	started  at least one story began
//	waiting  none could begin, but stories are running or done
//	drained  no story can ever begin
func (h ImplementReadyActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	a, err := h.b.bind(ctx, opts, req)
	if err != nil {
		return ActivityResult{}, err
	}
	defer bestEffortImplementStatus(ctx, opts, h.b.Status, a.Parent.String(), a.Generation)
	parent := a.Parent.String()
	if err := h.b.applyReviewedReturn(ctx, opts, req, a); err != nil {
		return ActivityResult{}, err
	}
	now := h.b.now(opts)
	if err := h.b.DB.EnsureImplementStories(ctx, parent, a.Generation, a.seeds(), now); err != nil {
		return ActivityResult{}, err
	}
	rows, err := h.b.DB.ListImplementStories(ctx, parent, a.Generation)
	if err != nil {
		return ActivityResult{}, err
	}
	state := map[string]store.ImplementStory{}
	running := 0
	for _, row := range rows {
		state[row.Key] = row
		if row.State == store.ImplementRunning {
			running++
		}
	}
	out := implementReadyOutput{Started: []implementStartedStory{}, Waiting: []string{}}
	for _, story := range a.Stories {
		row, ok := state[story.Key]
		if !ok || row.State != store.ImplementWaiting || !dependenciesMerged(story, state) {
			continue
		}
		if running >= store.MaxImplementRunning {
			break
		}
		moved, err := h.b.DB.TransitionImplementStory(ctx, store.ImplementTransition{
			Parent: parent, Generation: a.Generation, Key: story.Key,
			From: store.ImplementWaiting, To: store.ImplementRunning, ExpectedRevision: row.Revision, At: now,
		})
		if errors.Is(err, store.ErrImplementCap) {
			break
		}
		if errors.Is(err, store.ErrImplementConflict) {
			continue
		}
		if err != nil {
			return ActivityResult{}, err
		}
		state[story.Key] = moved
		running++
		out.Started = append(out.Started, implementStartedStory{Key: story.Key, Branch: story.Branch})
	}
	out.Running = running
	idle := true
	for _, row := range state {
		switch row.State {
		case store.ImplementRunning, store.ImplementDone:
			idle = false
		}
	}
	for _, story := range a.Stories {
		if state[story.Key].State == store.ImplementWaiting {
			out.Waiting = append(out.Waiting, story.Key)
		}
	}
	switch {
	case len(out.Started) > 0:
		return implementResult("started", out)
	case !idle:
		return implementResult("waiting", out)
	}
	return implementResult("drained", out)
}

func dependenciesMerged(s ImplementStory, state map[string]store.ImplementStory) bool {
	for _, dep := range s.Dependencies {
		if row, ok := state[dep]; !ok || row.State != store.ImplementMerged {
			return false
		}
	}
	return true
}

// oldestDone returns the done story that has been done longest, ties broken by
// plan order.
func oldestDone(rows []store.ImplementStory) (store.ImplementStory, bool) {
	done := slices.DeleteFunc(slices.Clone(rows), func(r store.ImplementStory) bool { return r.State != store.ImplementDone })
	if len(done) == 0 {
		return store.ImplementStory{}, false
	}
	slices.SortStableFunc(done, func(x, y store.ImplementStory) int {
		if c := x.StateAt.Compare(y.StateAt); c != 0 {
			return c
		}
		return x.Position - y.Position
	})
	return done[0], true
}
