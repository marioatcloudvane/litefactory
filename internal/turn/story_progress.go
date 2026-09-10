package turn

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

var errRejectedStoryEvent = errors.New("rejected story event")

// StoryProgressForge is the narrow forge surface required by durable checkpoints.
type StoryProgressForge interface {
	GetIssue(context.Context, github.Repo, int) (github.Issue, error)
	GetParentIssue(context.Context, github.Repo, int) (github.Issue, error)
	ListCommentsAfter(context.Context, github.Repo, int, int64) ([]github.Comment, error)
	PostComment(context.Context, github.Repo, int, redact.Body) (github.Comment, error)
	SetStoryState(context.Context, github.Repo, int, int64, int, int64, bool) error
}

// StoryProgressRenderer keeps formatting and redaction outside the progress service.
type StoryProgressRenderer interface {
	RenderStoryProgress(context.Context, report.StoryProgressNotice) (redact.Body, error)
}

// StoryProgress is shared by the daemon tick and final harvest. The mutex
// orders local calls; the store parent flock orders independent services.
// It launches no goroutines and never executes an agent or sleeps.
type StoryProgress struct {
	mu       sync.Mutex
	db       *store.DB
	forge    StoryProgressForge
	renderer StoryProgressRenderer
	botID    int64
	lockDir  string
}

// NewStoryProgress validates shared dependencies once at daemon startup.
// lockDir is the trusted config.Paths.StoryProgressLocksDir; every service
// observing this database must share it. It must not be inside an agent worktree.
func NewStoryProgress(db *store.DB, forge StoryProgressForge, renderer StoryProgressRenderer, botID int64, lockDir string) (*StoryProgress, error) {
	if db == nil || forge == nil || renderer == nil || botID < 1 || lockDir == "" {
		return nil, fmt.Errorf("story progress dependencies missing")
	}
	if err := os.MkdirAll(lockDir, 0o700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(lockDir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("story progress lock directory is not a real directory")
	}
	return &StoryProgress{db: db, forge: forge, renderer: renderer, botID: botID, lockDir: lockDir}, nil
}

// Register binds the factory-selected bundle to its trusted worktree transport.
func (p *StoryProgress) Register(ctx context.Context, b *agent.ApprovedBundle, worktree string) (err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if b == nil {
		return nil
	}
	release, err := p.db.LockStoryProgress(ctx, b.Parent, p.lockDir)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, release()) }()
	root, err := os.OpenRoot(worktree)
	if err != nil {
		return err
	}
	defer root.Close()
	dir := filepath.Join(agent.StoryEventsDirectory, strconv.FormatInt(b.RunID, 10))
	if err := root.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create story event transport: %w", err)
	}
	if err := checkStoryComponents(root, dir); err != nil {
		return err
	}
	return p.db.RegisterStoryProgress(ctx, store.StoryProgressRun{RunID: b.RunID, Parent: b.Parent, Generation: b.Generation, Worktree: worktree, Active: true})
}

// Poll is called by the daemon before discovery, even when all workers are busy.
func (p *StoryProgress) Poll(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.poll(ctx, 0, "")
}

// Finish drains the final atomic event files before retiring transport. Pending
// network effects remain in SQLite and are retried by subsequent ticks/restarts.
func (p *StoryProgress) Finish(ctx context.Context, runID int64, reason string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.poll(ctx, runID, reason)
}
func (p *StoryProgress) poll(ctx context.Context, finishID int64, reason string) error {
	runs, err := p.db.ListStoryProgressRuns(ctx)
	if err != nil {
		return err
	}
	effects, err := p.db.ListStoryProgressEffects(ctx)
	if err != nil {
		return err
	}
	parents := map[string]bool{}
	for _, r := range runs {
		parents[r.Parent] = true
	}
	for _, f := range effects {
		parents[f.Parent] = true
	}
	assessments, err := p.db.ListUnprojectedValidationAssessments(ctx)
	if err != nil {
		return err
	}
	for _, a := range assessments {
		parents[a.Parent] = true
	}
	decisions, err := p.db.ListPendingAcceptance(ctx)
	if err != nil {
		return err
	}
	for _, d := range decisions {
		parents[d.Parent] = true
	}
	followupParents, err := p.db.ListPendingFollowupParents(ctx)
	if err != nil {
		return err
	}
	for _, parent := range followupParents {
		parents[parent] = true
	}
	deliveryParents, err := p.db.ListDeliveryParents(ctx)
	if err != nil {
		return err
	}
	for _, parent := range deliveryParents {
		parents[parent] = true
	}
	completed, err := p.db.ListStoryAggregateParents(ctx)
	if err != nil {
		return err
	}
	for _, parent := range completed {
		parents[parent] = true
	}
	var faults []error
	for parent := range parents {
		err := p.pollParent(ctx, parent, finishID, reason)
		if err != nil {
			faults = append(faults, err)
		}
	}
	return errors.Join(faults...)
}

func (p *StoryProgress) pollParent(ctx context.Context, parent string, finishID int64, reason string) (err error) {
	release, err := p.db.LockStoryProgress(ctx, parent, p.lockDir)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, release()) }()
	// Reload under cross-process ownership; the initial list was discovery only.
	runs, err := p.db.ListStoryProgressRuns(ctx)
	if err != nil {
		return err
	}
	var faults []error
	complete := true
	for _, r := range runs {
		if r.Parent != parent {
			continue
		}
		if err := p.ingest(ctx, r); err != nil {
			faults = append(faults, fmt.Errorf("story progress run %d: %w", r.RunID, err))
			if !errors.Is(err, errRejectedStoryEvent) {
				complete = false
				continue
			}
		}
		if r.RunID == finishID || r.Status != store.RunRunning {
			why := reason
			if why == "" {
				why = "Parent attempt ended without scoped verification for this story. Scope remains locked; no completion is claimed."
			}
			if err := p.db.EndStoryProgress(ctx, r.RunID, why); err != nil {
				faults = append(faults, err)
				complete = false
			} else if err := retireStoryTransport(r); err != nil {
				faults = append(faults, err)
			}
		}
	}
	// Any unread input may contain invalidation. Never dispatch an older close
	// until all ordered streams for this parent have been reliably exhausted.
	if complete {
		if err := p.deliverParent(ctx, parent); err != nil {
			faults = append(faults, err)
		}
		if err := p.deliverAcceptance(ctx, parent); err != nil {
			faults = append(faults, err)
		}
		if err := p.deliverFollowups(ctx, parent); err != nil {
			faults = append(faults, err)
		}
		if err := p.pollDelivery(ctx, parent); err != nil {
			faults = append(faults, err)
		}
		if err := p.db.CompleteManagedFeature(ctx, parent); err != nil {
			faults = append(faults, err)
		}
	}
	return errors.Join(faults...)
}

func (p *StoryProgress) ingest(ctx context.Context, r store.StoryProgressRun) error {
	feature, err := p.db.GetManagedFeature(ctx, r.Parent)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(r.Worktree)
	if err != nil {
		return err
	}
	defer root.Close()
	directory := filepath.Join(agent.StoryEventsDirectory, strconv.FormatInt(r.RunID, 10))
	if err := checkStoryComponents(root, directory); err != nil {
		return err
	}
	dir, err := root.Open(directory)
	if err != nil {
		return err
	}
	entries, readErr := dir.ReadDir(agent.MaxStoryEvents + 1)
	closeErr := dir.Close()
	if err := errors.Join(readErr, closeErr); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if len(entries) > agent.MaxStoryEvents {
		return fmt.Errorf("too many story event files")
	}
	revisions, err := p.db.ListApprovedStoryRevisions(ctx, r.Parent, r.Generation)
	if err != nil {
		return err
	}
	scope := map[string]store.ApprovedStoryRevision{}
	for _, s := range revisions {
		scope[s.Key] = s
	}
	// Strict numeric names are ordered independently of filesystem enumeration.
	files := map[int]string{}
	var rejected []error
	var totalBytes int64
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		seq, err := strconv.Atoi(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil || seq < 1 || seq > agent.MaxStoryEvents || entry.Name() != fmt.Sprintf("%06d.json", seq) {
			return fmt.Errorf("invalid story event filename")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		totalBytes += info.Size()
		if totalBytes > agent.MaxStoryRunBytes {
			return fmt.Errorf("story event transport exceeds total byte limit")
		}
		files[seq] = entry.Name()
	}
	for seq := 1; seq <= agent.MaxStoryEvents; seq++ {
		name, ok := files[seq]
		if !ok {
			if seq <= r.LastSequence {
				continue
			}
			for later := range files {
				if later > seq {
					return fmt.Errorf("story event sequence gap at %d", seq)
				}
			}
			break
		}
		data, err := readStoryFile(root, filepath.Join(directory, name), agent.MaxStoryEventBytes)
		if err != nil {
			return err
		}
		old, err := p.db.GetStoryProgressEvent(ctx, r.RunID, seq)
		if err == nil {
			if old.Digest != agent.StoryDigest(data) && old.Kind != "rejected" {
				if err := p.db.ConflictStoryProgress(ctx, r.RunID, seq); err != nil {
					return err
				}
				continue
			}
			continue
		}
		if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		consume := func() error {
			e, err := agent.ParseStoryEvent(data)
			if err != nil {
				return err
			}
			s, ok := scope[e.Story]
			if !ok || e.RunID != r.RunID || e.Parent != r.Parent || e.Generation != r.Generation || e.Sequence != seq || e.IssueID != s.IssueID || e.Revision != s.Hash {
				return fmt.Errorf("story event identity is outside frozen run scope")
			}
			authorized, err := p.db.StoryProgressAuthorized(ctx, r.RunID, e.Story)
			if err != nil {
				return err
			}
			if !authorized {
				return fmt.Errorf("read-only dependency cannot publish outcomes")
			}
			if feature.WorkflowVersion == store.WorkflowSeparated && e.Kind == "verified" || feature.WorkflowVersion != store.WorkflowSeparated && e.Kind == "engineering_complete" {
				return fmt.Errorf("story event kind does not match approved workflow")
			}
			if e.Kind == "verified" {
				if err := storyAcceptanceCoveredFrozen(ctx, p.db, r.Parent, r.Generation, s.Key, s.Hash, s.Body, e.Evidence.Acceptance); err != nil {
					return err
				}
				if err := verifyStorySnapshot(root, *e.Evidence); err != nil {
					return err
				}
			}
			f := store.StoryProgressEffect{RunID: e.RunID, Sequence: e.Sequence, Parent: e.Parent, Generation: e.Generation, Story: e.Story, IssueNumber: s.IssueNumber, IssueID: s.IssueID, ParentID: s.ParentID, Revision: s.Hash, Kind: e.Kind, Detail: e.Reason}
			if e.Evidence != nil {
				f.ImplementationDigest = e.Evidence.ImplementationDigest
				var details strings.Builder
				fmt.Fprintf(&details, "Attempt %d; immutable scoped snapshot and validation bundle retained by the factory.\n", e.Attempt)
				for _, group := range [][]agent.StoryCheck{e.Evidence.Acceptance, e.Evidence.Review, e.Evidence.Tests} {
					for _, c := range group {
						fmt.Fprintf(&details, "PASS %s: %s\n", c.Name, c.Evidence)
					}
				}
				f.Detail = details.String()
				if len(f.Detail) > 20000 {
					return fmt.Errorf("verification summary exceeds report limit")
				}
				// The event file is itself immutable transport. A second read prevents a
				// writer replacing it during the worktree comparison from being accepted.
				again, err := readStoryFile(root, filepath.Join(directory, name), agent.MaxStoryEventBytes)
				if err != nil {
					return err
				}
				if agent.StoryDigest(again) != agent.StoryDigest(data) {
					return fmt.Errorf("unstable validation bundle")
				}
				if err := verifyStorySnapshot(root, *e.Evidence); err != nil {
					return err
				}
			}
			if err := p.db.AcceptStoryProgress(ctx, store.StoryProgressEvent{RunID: e.RunID, Sequence: e.Sequence, Story: e.Story, Attempt: e.Attempt, Kind: e.Kind, Digest: agent.StoryDigest(data), Payload: data}, f); err != nil {
				return err
			}
			return nil
		}
		if err := consume(); err != nil {
			if recordErr := p.db.RejectStoryProgress(ctx, r.RunID, seq, agent.StoryDigest(data), data); recordErr != nil {
				return errors.Join(err, recordErr)
			}
			rejected = append(rejected, fmt.Errorf("%w: event %d: %w", errRejectedStoryEvent, seq, err))
		}
	}
	return errors.Join(rejected...)
}
func verifyStorySnapshot(root *os.Root, e agent.StoryEvidence) error {
	for _, f := range e.Files {
		if err := checkStoryComponents(root, filepath.Dir(f.Path)); err != nil {
			return err
		}
		if f.Deleted {
			if _, err := root.Lstat(f.Path); !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("deleted snapshot path exists or is unreadable")
			}
			continue
		}
		data, err := readStoryFile(root, f.Path, agent.MaxStoryEventBytes)
		if err != nil {
			return err
		}
		if agent.StoryDigest(data) != f.SHA256 {
			return fmt.Errorf("implementation changed after scoped checks: %s", f.Path)
		}
	}
	return nil
}
func checkStoryComponents(root *os.Root, p string) error {
	current := ""
	for _, part := range strings.Split(filepath.ToSlash(p), "/") {
		if part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := root.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("story transport or snapshot contains symlink")
		}
	}
	return nil
}
func readStoryFile(root *os.Root, path string, limit int64) ([]byte, error) {
	if err := checkStoryComponents(root, path); err != nil {
		return nil, err
	}
	before, err := root.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("story input is not regular")
	}
	f, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	info, statErr := f.Stat()
	data, readErr := io.ReadAll(io.LimitReader(f, limit+1))
	closeErr := f.Close()
	if err := errors.Join(statErr, readErr, closeErr); err != nil {
		return nil, err
	}
	if !os.SameFile(before, info) || !info.Mode().IsRegular() || len(data) > int(limit) {
		return nil, fmt.Errorf("unstable or oversized story input")
	}
	return data, nil
}
func (p *StoryProgress) deliverParent(ctx context.Context, parent string) error {
	effects, err := p.db.ListStoryProgressEffects(ctx)
	if err != nil {
		return err
	}
	var faults []error
	blocked := map[string]bool{}
	for _, f := range effects {
		if f.Parent != parent {
			continue
		}
		key := fmt.Sprintf("%s/%d/%s", f.Parent, f.Generation, f.Story)
		if blocked[key] {
			continue
		}
		if err := p.deliverOne(ctx, f); err != nil {
			blocked[key] = true
			faults = append(faults, err)
		}
	}
	return errors.Join(faults...)
}
func (p *StoryProgress) deliverOne(ctx context.Context, f store.StoryProgressEffect) error {
	ref, err := naming.ParseRef(f.Parent)
	if err != nil {
		return err
	}
	repo := github.RepoFromRef(ref)
	live, err := p.forge.GetIssue(ctx, repo, f.IssueNumber)
	if err != nil {
		return err
	}
	parent, err := p.forge.GetParentIssue(ctx, repo, f.IssueNumber)
	if err != nil {
		return err
	}
	if live.ID != f.IssueID || parent.ID != f.ParentID || parent.Number != ref.Issue {
		return fmt.Errorf("progress target identity changed")
	}
	n := report.StoryProgressNotice{Parent: ref, Generation: f.Generation, RunID: f.RunID, EffectID: f.ID, Sequence: f.Sequence, Story: f.Story, Revision: f.Revision, Kind: f.Kind, Detail: f.Detail, ImplementationDigest: f.ImplementationDigest}
	if f.CommentID == 0 {
		comments, err := p.forge.ListCommentsAfter(ctx, repo, f.IssueNumber, 0)
		if err != nil {
			return err
		}
		for _, c := range comments {
			if c.Author.ID == p.botID && exactProgressMarker(c.Body, n.Marker()) {
				f.CommentID = c.ID
				break
			}
		}
		if f.CommentID == 0 {
			body, err := p.renderer.RenderStoryProgress(ctx, n)
			if err != nil {
				return err
			}
			posted, err := p.forge.PostComment(ctx, repo, f.IssueNumber, body)
			if err != nil {
				return err
			}
			f.CommentID = posted.ID
		}
		if err := p.db.SaveStoryProgressComment(ctx, f.ID, f.CommentID); err != nil {
			return err
		}
	}
	feature, err := p.db.GetManagedFeature(ctx, f.Parent)
	if err != nil {
		return err
	}
	if feature.WorkflowVersion != store.WorkflowSeparated && (f.Kind == "verified" || f.Kind == "invalidated") {
		desired := "open"
		if f.Kind == "verified" {
			desired = "closed"
		}
		// Read-after-uncertain-response handles a successful remote mutation whose
		// response or following local acknowledgement was lost.
		if live.State != desired || (f.Kind == "verified" && live.StateReason != "completed") {
			if err := p.forge.SetStoryState(ctx, repo, ref.Issue, f.ParentID, f.IssueNumber, f.IssueID, f.Kind == "verified"); err != nil {
				return err
			}
		}
	}
	return p.db.DeliverStoryProgressEffect(ctx, f)
}
func exactProgressMarker(body, marker string) bool {
	for line := range strings.SplitSeq(body, "\n") {
		if line == marker {
			return true
		}
	}
	return false
}

// Retire only the consumed per-run directory, after exclusive final drain. All
// accepted snapshot bytes and pending effects are already durable in SQLite.
func retireStoryTransport(r store.StoryProgressRun) error {
	root, err := os.OpenRoot(r.Worktree)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.RemoveAll(filepath.Join(agent.StoryEventsDirectory, strconv.FormatInt(r.RunID, 10)))
}

func storyAcceptanceCovered(body string, checks []agent.StoryCheck) error {
	required, err := planbrief.AcceptanceCriteria(body)
	if err != nil {
		return err
	}
	names := map[string]bool{}
	for _, c := range checks {
		names[c.Name] = true
	}
	for _, name := range required {
		if !names[name] {
			return fmt.Errorf("missing scoped evidence for %s", name)
		}
	}
	return nil
}
