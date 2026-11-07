package turn

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// brainstormSeedStore is the part of the store brief seeding reads and writes.
type brainstormSeedStore interface {
	BrainstormSeedForLaunch(ctx context.Context, launchID int64) (store.BrainstormPlanLaunchSeed, error)
	BrainstormSeedLedger(ctx context.Context, itemID string) ([]byte, string, error)
	RecordBrainstormSeedDigests(ctx context.Context, launchID int64, dir, brief, technical string) (store.BrainstormPlanLaunchSeed, error)
	FinishBrainstormSeed(ctx context.Context, launchID int64, state, reason string, at time.Time) error
}

// brainstormSeedResult says where the seeded brief is. Dir is "" when the
// launch has no brainstorm seed.
type brainstormSeedResult struct {
	Dir      string
	Brief    string
	Existing bool
}

func digestBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func brainstormSeedTemp(name string, launchID int64) string {
	return fmt.Sprintf(".%s.seed-%d.tmp", name, launchID)
}

// seedBrainstormBrief makes the brainstorm brief of an admitted Plan launch
// present in the worktree before an agent node builds its prompt. It writes
// the item's frozen ledger as Markdown, records its digest before writing,
// writes the brief through a seed-named temporary file, and marks the seed
// complete only after it reads back equal. A factory-owned partial write is
// finished; any other existing brief is never overwritten and the seed is
// recorded as skipped.
func seedBrainstormBrief(ctx context.Context, db brainstormSeedStore, worktree string, launchID int64, defaultDir string, now time.Time) (brainstormSeedResult, error) {
	if db == nil || launchID < 1 {
		return brainstormSeedResult{}, nil
	}
	seed, err := db.BrainstormSeedForLaunch(ctx, launchID)
	if errors.Is(err, store.ErrNotFound) {
		return brainstormSeedResult{}, nil
	}
	if err != nil {
		return brainstormSeedResult{}, err
	}
	if seed.State == store.BrainstormSeedSkippedExisting {
		return brainstormSeedResult{Dir: seed.SessionDir, Brief: ResearchBriefName, Existing: true}, nil
	}
	ledger, _, err := db.BrainstormSeedLedger(ctx, seed.ItemID)
	if errors.Is(err, store.ErrNotFound) {
		return brainstormSeedResult{}, errors.New("the brainstorm brief cannot be prepared: the draft's ledger is missing")
	}
	if err != nil {
		return brainstormSeedResult{}, err
	}
	if digestBytes(ledger) != seed.LedgerDigest {
		return brainstormSeedResult{}, errors.New("the brainstorm brief cannot be prepared: the draft's ledger changed after it was confirmed")
	}
	var research agentresult.Research
	if err = json.Unmarshal(ledger, &research); err != nil {
		return brainstormSeedResult{}, fmt.Errorf("the brainstorm brief cannot be prepared: the draft's ledger is unreadable: %w", err)
	}
	dir := seed.SessionDir
	if dir == "" {
		dir = defaultDir
	}
	if dir == "" || !agentresult.ValidPath(dir) {
		return brainstormSeedResult{}, errors.New("the brainstorm brief cannot be prepared: no session directory")
	}
	brief := []byte(brainstormBriefMarkdown(research))
	briefDigest := digestBytes(brief)
	if seed.State == store.BrainstormSeedComplete && seed.BriefDigest != briefDigest {
		// Recorded in an earlier format; the finished brief belongs to Plan.
		return brainstormSeedResult{Dir: dir, Brief: ResearchBriefName}, nil
	}
	if seed.State != store.BrainstormSeedPending && seed.BriefDigest != briefDigest {
		return brainstormSeedResult{}, errors.New("the brainstorm brief cannot be prepared: its rendered files differ from the recorded digests")
	}
	root, err := os.OpenRoot(worktree)
	if err != nil {
		return brainstormSeedResult{}, err
	}
	defer root.Close()
	files := []struct {
		name   string
		body   []byte
		digest string
	}{{ResearchBriefName, brief, briefDigest}}
	// An unrelated brief already in place is never overwritten. A target equal
	// to its recorded digest is factory-owned.
	for _, f := range files {
		current, readErr := readSeedFile(root, path.Join(dir, f.name))
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		if readErr != nil {
			return brainstormSeedResult{}, readErr
		}
		if digestBytes(current) != f.digest {
			if seed.State == store.BrainstormSeedComplete {
				// Plan itself may revise the seeded brief later; that is its file now.
				return brainstormSeedResult{Dir: dir, Brief: ResearchBriefName}, nil
			}
			if err = db.FinishBrainstormSeed(ctx, launchID, store.BrainstormSeedSkippedExisting, "an existing "+f.name+" was kept", now); err != nil {
				return brainstormSeedResult{}, err
			}
			removeSeedTemps(root, dir, launchID)
			return brainstormSeedResult{Dir: dir, Brief: ResearchBriefName, Existing: true}, nil
		}
	}
	if seed.State == store.BrainstormSeedPending {
		if _, err = db.RecordBrainstormSeedDigests(ctx, launchID, dir, briefDigest, ""); err != nil {
			return brainstormSeedResult{}, err
		}
	}
	if err = root.MkdirAll(dir, 0o750); err != nil {
		return brainstormSeedResult{}, fmt.Errorf("brainstorm brief directory: %w", err)
	}
	for _, f := range files {
		target := path.Join(dir, f.name)
		if current, readErr := readSeedFile(root, target); readErr == nil && bytes.Equal(current, f.body) {
			_ = root.Remove(path.Join(dir, brainstormSeedTemp(f.name, launchID)))
			continue
		}
		if err = writeSeedFile(root, dir, f.name, launchID, f.body); err != nil {
			return brainstormSeedResult{}, err
		}
	}
	for _, f := range files {
		current, readErr := readSeedFile(root, path.Join(dir, f.name))
		if readErr != nil || digestBytes(current) != f.digest {
			return brainstormSeedResult{}, fmt.Errorf("the brainstorm brief cannot be prepared: %s did not read back as written", f.name)
		}
	}
	if seed.State != store.BrainstormSeedComplete {
		if err = db.FinishBrainstormSeed(ctx, launchID, store.BrainstormSeedComplete, "", now); err != nil {
			return brainstormSeedResult{}, err
		}
	}
	return brainstormSeedResult{Dir: dir, Brief: ResearchBriefName}, nil
}

func readSeedFile(root *os.Root, name string) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", name)
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, 16<<20))
}

func writeSeedFile(root *os.Root, dir, name string, launchID int64, body []byte) error {
	temp := path.Join(dir, brainstormSeedTemp(name, launchID))
	f, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	_, writeErr := f.Write(body)
	if err = errors.Join(writeErr, f.Sync(), f.Close()); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	if err = root.Rename(temp, path.Join(dir, name)); err != nil {
		return fmt.Errorf("place %s: %w", name, err)
	}
	return nil
}

func removeSeedTemps(root *os.Root, dir string, launchID int64) {
	for _, name := range []string{ResearchBriefName} {
		_ = root.Remove(path.Join(dir, brainstormSeedTemp(name, launchID)))
	}
}

// brainstormSeedDefaultDir is the session directory a first seed writes to.
func brainstormSeedDefaultDir(prep *preparation) (string, error) {
	if prep == nil {
		return "", errors.New("plan preparation is missing")
	}
	if dir := strings.TrimSpace(prep.promptInput.SessionArtifactDir); dir != "" {
		return dir, nil
	}
	return naming.AITeamSessionDir(prep.session.Branch, prep.run.StartedAt, prep.session.FeatureSlug)
}

// prepareBrainstormSeed runs seeding for one Plan agent node and points the
// prompt at the seeded brief. It returns the binding line naming the brief.
func (w *researchWorkflow) prepareBrainstormSeed(ctx context.Context) (string, error) {
	if w.opts.DB == nil || w.admission.LaunchID < 1 || w.prep == nil || w.prep.worktree == nil {
		return "", nil
	}
	defaultDir, err := brainstormSeedDefaultDir(w.prep)
	if err != nil {
		return "", err
	}
	seeded, err := seedBrainstormBrief(ctx, w.opts.DB, w.prep.worktree.Path(), w.admission.LaunchID, defaultDir, time.Now().UTC())
	if err != nil || seeded.Dir == "" {
		return "", err
	}
	w.prep.promptInput.SessionArtifactDir = seeded.Dir
	w.prep.promptInput.SessionArtifactBrief = seeded.Brief
	return fmt.Sprintf("Brainstorm brief for this Plan: %s\n", path.Join(seeded.Dir, seeded.Brief)), nil
}

// brainstormLaunchNotePurpose keys the factory note of one launch attempt.
const brainstormLaunchNotePurpose = "brainstorm-launch-note"

// ReportBrainstormLaunchNote posts the short factory note on an issue a
// confirmed brainstorm created, as factory output through the delivery
// journal of the launch attempt's private trigger. A failed post stays
// pending under the same key and is resumed by a later call.
func (r *Reporter) ReportBrainstormLaunchNote(ctx context.Context, ref naming.Ref, sessionID, triggerID int64) (Posted, error) {
	if !store.IsBrainstormLaunchTrigger(triggerID) {
		return Posted{}, errors.New("brainstorm launch note requires a launch trigger")
	}
	intent := r.deliveryIntent(ref, ref, sessionID, triggerID, 0, brainstormLaunchNotePurpose)
	return r.deliver(ctx, intent, func() (redact.Body, error) {
		return r.renderer.RenderReportDelivery(ctx, "Plan started from a brainstorm brief. Your spoken yes approved only creating this issue and starting Plan; every Plan stage still waits for your usual approvals.\n", "")
	})
}

// brainstormBriefMarkdown writes a brainstorm draft's ledger as the same
// RESEARCH_BRIEF.md a Research agent writes: the request, the outcome ledger
// with one heading per ID, the assumptions and the findings with their sources.
func brainstormBriefMarkdown(r agentresult.Research) string {
	var b strings.Builder
	b.WriteString("# Research brief\n\n## Request\n\n")
	if request := strings.TrimSpace(r.Request); request != "" {
		b.WriteString(request)
	} else {
		b.WriteString("_The request was not recorded._")
	}
	b.WriteString("\n")
	entries := func(heading string, items []agentresult.Statement) {
		if len(items) == 0 {
			return
		}
		b.WriteString("\n## " + heading + "\n")
		for _, s := range items {
			b.WriteString("\n### " + s.ID + " " + strings.TrimSpace(s.Text) + "\n")
		}
	}
	entries("Outcome ledger", r.Decisions)
	entries("Assumptions", r.Assumptions)
	if len(r.Findings) > 0 {
		b.WriteString("\n## Findings\n")
		for _, f := range r.Findings {
			b.WriteString("\n### " + f.ID + "\n\n" + strings.TrimSpace(f.Text) + "\n")
			for _, e := range f.Evidence {
				b.WriteString("\n- `" + e.Path)
				if e.Line > 0 {
					fmt.Fprintf(&b, ":%d", e.Line)
				}
				b.WriteString("`")
				if e.Revision != "" {
					b.WriteString(" @ `" + e.Revision + "`")
				}
			}
			if len(f.Evidence) > 0 {
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}
