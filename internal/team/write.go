package team

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/cloudvane/litefactory/internal/config"
)

// standaloneWrite is one already-validated, already-hashed standalone file
// waiting to be written, paired with the lockfile record it will produce.
type standaloneWrite struct {
	path   string
	body   []byte
	record LockRecord
}

// rootSection is one agent's marker-tagged section destined for a root file
// (RootFileClaude / RootFileOpencode), paired with the lockfile record it
// will produce once its RenderedHash (the marker span hash) is known.
type rootSection struct {
	teams    []string
	agentKey string
	body     []byte
	record   LockRecord
}

// Add installs every already-rendered AgentRender into root: standalone
// files at their Owned Paths, and root-file-targeted files as marker-tagged
// sections upserted into the shared root file they name.
//
// The whole call is atomic in the sense the story requires: every render is
// validated (harness known, path owned, no literal secret) before anything
// is written, and the provenance lockfile is saved last, so a failure at any
// point — a bad path, a detected secret, an I/O error partway through
// writing — leaves either nothing changed or a partial set of artifacts on
// disk with a lockfile that still describes the previous true state (it is
// only rewritten after every write in the run has already landed).
//
// A fresh render that matches an existing lockfile record's SourceHash and
// RenderedHash exactly is a no-op: nothing is written for it, and its
// existing record is reported in AddResult.Skipped rather than Written.
//
// Concurrent Add calls against the same root serialize on a per-repo flock
// held for the duration of the call.
func Add(root string, renders []AgentRender) (*AddResult, error) {
	lock, err := acquireRepoLock(root)
	if err != nil {
		return nil, fmt.Errorf("acquire repo lock: %w", err)
	}
	defer func() {
		_ = lock.release()
	}()

	lf, loadErr := loadLockfile(root)
	if loadErr != nil && !errors.Is(loadErr, ErrNoLockfile) {
		return nil, loadErr
	}

	var candidates []standaloneWrite
	rootCandidates := map[string][]rootSection{}

	// Validate every artifact from every render before writing anything.
	// Every problem found is collected rather than returned on the first
	// one, so a caller with several bad renders in one batch sees all of
	// them together (plan_brief.md Cross-Story Constraints, Platform and
	// Security). The write phase below still never starts unless the whole
	// batch validates cleanly.
	var errs []error
	for _, ar := range renders {
		if err := validateHarness(ar.Harness); err != nil {
			errs = append(errs, err)
			continue
		}
		if !KeyPattern.MatchString(ar.AgentKey) {
			errs = append(errs, &KeyError{Path: "AgentRender.AgentKey", Key: ar.AgentKey})
			continue
		}
		for _, f := range ar.Files {
			if err := validateOwnedPath(ar.Harness, ar.AgentKey, f.Path); err != nil {
				errs = append(errs, err)
				continue
			}
			if class, ok := config.ClassifySecret(string(f.Bytes)); ok {
				errs = append(errs, &SecretDetectedError{Path: f.Path, Class: string(class)})
				continue
			}

			if isRootFileTarget(f.Path) {
				rootCandidates[f.Path] = append(rootCandidates[f.Path], rootSection{
					teams:    ar.Teams,
					agentKey: ar.AgentKey,
					body:     f.Bytes,
					record: LockRecord{
						AgentKey:   ar.AgentKey,
						Harness:    ar.Harness,
						DestPath:   f.Path,
						SourcePath: ar.SourcePath,
						SourceHash: ar.SourceHash,
						Teams:      encodeTeams(ar.Teams),
					},
				})
				continue
			}

			candidates = append(candidates, standaloneWrite{
				path: f.Path,
				body: f.Bytes,
				record: LockRecord{
					AgentKey:     ar.AgentKey,
					Harness:      ar.Harness,
					DestPath:     f.Path,
					SourcePath:   ar.SourcePath,
					SourceHash:   ar.SourceHash,
					RenderedHash: HashBytes(f.Bytes),
					Teams:        encodeTeams(ar.Teams),
				},
			})
		}
	}
	if err := joinSorted(errs); err != nil {
		return nil, err
	}

	result := &AddResult{}

	// Split standalone candidates into no-ops and actual writes, against the
	// lockfile as it stood before this call.
	var standaloneWrites []standaloneWrite
	for _, c := range candidates {
		if existing, found := lf.findRecord(c.record); found && existing.matches(c.record) {
			result.Skipped = append(result.Skipped, existing)
			continue
		}
		standaloneWrites = append(standaloneWrites, c)
	}

	// Do the same for root-file sections, now that each section's
	// RenderedHash (the marker span hash) can be computed.
	changedSections := map[string][]rootSection{}
	for rootFile, sections := range rootCandidates {
		for _, sec := range sections {
			sec.record.RenderedHash = markerSpanHash(sec.teams, sec.agentKey, sec.body)
			if existing, found := lf.findRecord(sec.record); found && existing.matches(sec.record) {
				result.Skipped = append(result.Skipped, existing)
				continue
			}
			changedSections[rootFile] = append(changedSections[rootFile], sec)
		}
	}

	// Write every standalone file that changed.
	for _, w := range standaloneWrites {
		if err := refuseSymlink(root, w.path); err != nil {
			return nil, err
		}
		absPath := filepath.Join(root, w.path)
		if err := writeFileAtomic(absPath, w.body, 0o666); err != nil {
			return nil, fmt.Errorf("write %s: %w", w.path, err)
		}
		lf.upsertRecord(w.record)
		result.Written = append(result.Written, w.record)
	}

	// Write every root file that has at least one changed section, in a
	// deterministic order.
	rootFiles := make([]string, 0, len(changedSections))
	for rootFile := range changedSections {
		rootFiles = append(rootFiles, rootFile)
	}
	sort.Strings(rootFiles)

	for _, rootFile := range rootFiles {
		if err := refuseSymlink(root, rootFile); err != nil {
			return nil, err
		}
		absPath := filepath.Join(root, rootFile)
		content, err := os.ReadFile(absPath)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("read %s: %w", rootFile, err)
			}
			content = nil
		}

		sections := changedSections[rootFile]
		sort.Slice(sections, func(i, j int) bool {
			return sections[i].agentKey < sections[j].agentKey
		})
		for _, sec := range sections {
			content = upsertMarkerSection(content, sec.teams, sec.agentKey, sec.body)
		}

		if err := writeFileAtomic(absPath, content, 0o666); err != nil {
			return nil, fmt.Errorf("write %s: %w", rootFile, err)
		}
		for _, sec := range sections {
			lf.upsertRecord(sec.record)
			result.Written = append(result.Written, sec.record)
		}
	}

	// The lockfile is saved last: every artifact this call is going to write
	// has already landed by this point, so a mid-run death before this line
	// leaves the lockfile describing the previous true state rather than
	// claiming an artifact exists that was never written.
	if err := lf.save(root); err != nil {
		return nil, fmt.Errorf("save lockfile: %w", err)
	}

	return result, nil
}
