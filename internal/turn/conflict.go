package turn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// conflictBlobReader is the narrow read from internal/git this package needs
// to collect conflicting content: exactly [git.Worktree.ConflictBlob], named
// at the point of consumption the way [CloneGate] and [Runner] already are
// in this package.
type conflictBlobReader interface {
	ConflictBlob(ctx context.Context, treeSHA, path string) (string, error)
}

// conflictFilePerFileMaxBytes bounds one conflicting file's capped content,
// independently of the total [Options.ConflictContentMaxBytes] budget
// (AC-11.5): a single very large conflicting file must not be free to
// consume the whole total budget on its own and starve every other file out
// of the suggestion entirely.
const conflictFilePerFileMaxBytes = 8192

// conflictMarkers are the three lines a real merge conflict is delimited by.
var conflictMarkers = [3]string{"<<<<<<<", "=======", ">>>>>>>"}

// collectConflictContent reads, screens and caps the conflicting content for
// every path in conflicts, inside the same [Options.CloneGate] acquisition
// [git.Worktree.MergePreview] already ran under (S-10): it is called from
// [syncBase], never after the gate that guards it has been released.
//
// It never returns an error: a file this package cannot safely represent is
// excluded whole rather than turning a successful blocked-merge detection
// into a turn failure. A file is excluded whole, never truncated (watch-out
// #2 of US-11): one that could not be read at all, one whose content is
// binary or not valid UTF-8, one with no conflict-marker region to extract
// at all (a delete/modify or mode conflict "git merge-tree --write-tree"
// cannot represent with markers), one whose extracted hunks alone exceed
// [conflictFilePerFileMaxBytes], and one that would push the running total
// past totalMaxBytes. Files are visited, and the total budget is consumed,
// in the order conflicts lists them — deterministic, since
// [git.MergePreview.Conflicts] is itself in a deterministic order.
func collectConflictContent(ctx context.Context, reader conflictBlobReader, treeSHA string, conflicts []string, totalMaxBytes int) (files []agent.ConflictFileContent, excluded []string) {
	var total int
	for _, path := range conflicts {
		content, ok := capturedConflictContent(ctx, reader, treeSHA, path, totalMaxBytes, &total)
		if !ok {
			excluded = append(excluded, path)
			continue
		}
		files = append(files, agent.ConflictFileContent{Path: path, Content: content})
	}
	return files, excluded
}

// capturedConflictContent applies every exclusion rule [collectConflictContent]
// documents to one conflicting path, and — only when every rule is
// satisfied — adds its capped size to *total before returning it.
func capturedConflictContent(ctx context.Context, reader conflictBlobReader, treeSHA, path string, totalMaxBytes int, total *int) (string, bool) {
	blob, err := reader.ConflictBlob(ctx, treeSHA, path)
	if err != nil {
		return "", false
	}
	if strings.IndexByte(blob, 0) >= 0 {
		// A NUL byte is not valid text content; treat the file as binary.
		return "", false
	}
	if !utf8.ValidString(blob) {
		return "", false
	}
	hunks := extractConflictHunks(blob)
	if len(hunks) == 0 {
		return "", false
	}
	content := strings.Join(hunks, "\n...\n")
	if len(content) > conflictFilePerFileMaxBytes {
		return "", false
	}
	if *total+len(content) > totalMaxBytes {
		return "", false
	}
	*total += len(content)
	return content, true
}

// extractConflictHunks pulls every conflict-marker-delimited region out of
// content, each one starting at the line that opens with "<<<<<<<" and
// ending at the line that opens with ">>>>>>>". It is line-oriented rather
// than a regular expression: a conflict marker is a rule about whole lines,
// and a malformed or partial marker sequence simply yields no hunk rather
// than a best-effort guess at one.
func extractConflictHunks(content string) []string {
	var hunks []string
	var current []string
	inHunk := false
	for _, line := range strings.Split(content, "\n") {
		switch {
		case strings.HasPrefix(line, conflictMarkers[0]):
			inHunk = true
			current = []string{line}
		case strings.HasPrefix(line, conflictMarkers[2]) && inHunk:
			current = append(current, line)
			hunks = append(hunks, strings.Join(current, "\n"))
			current = nil
			inHunk = false
		case inHunk:
			current = append(current, line)
		}
	}
	return hunks
}

// ConflictAdvisor is the narrow, tool-free advisory capability US-11 needs
// from a coding-agent-tool [Runner]: propose a resolution for already-
// collected, already-capped conflicting content, and nothing else. It is
// declared here, at the point of consumption, the same way [Runner] and
// [CloneGate] already are in this package — internal/agent is where the
// request/response types that cross this boundary live, exactly as it is
// for [agent.ScheduleProposalRequest]/[agent.ScheduleProposal] and
// internal/daemon's own scheduleAdvisor.
type ConflictAdvisor interface {
	ProposeConflictResolution(ctx context.Context, req agent.ConflictResolutionRequest) (agent.ConflictResolution, error)
}

// conflictAdvisorFor narrows the [Runner] registered for kind into a
// [ConflictAdvisor] by type assertion, mirroring internal/daemon's
// newProposalGenerator for its own scheduleAdvisor exactly: a Runner that
// does not additionally implement the advisory method reports "no advisor
// available" rather than reaching a nil interface value three calls later.
func conflictAdvisorFor(kind agent.Kind, runners RunnerSet) (ConflictAdvisor, bool) {
	runner, ok := runners.runner(kind)
	if !ok {
		return nil, false
	}
	advisor, ok := runner.(ConflictAdvisor)
	return advisor, ok
}

// ConflictPreviewer prepares a reviewable sanitized preview before persistence.
type ConflictPreviewer interface {
	PrepareConflictPreview(context.Context, naming.Ref, report.ConflictProposal) (report.ConflictProposal, error)
}

// validateConflictResolution re-checks a model's answer locally before
// anything is persisted or posted (T-11.8): every path must be one of the
// paths that actually conflicted, every path may appear at most once, and no
// resolution may still carry a conflict marker. Any violation rejects the
// whole response — never the good files out of it — the same reject-don't-
// salvage discipline internal/report already applies to untrusted text.
//
// [agent.ParseConflictResolution] already refuses a response carrying a
// conflict marker; the check is repeated here as defence in depth (S-9) and
// because it is this package, not internal/agent, that knows which paths
// were actually allowed to answer for.
func validateConflictResolution(resolution agent.ConflictResolution, allowed []string) error {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, path := range allowed {
		allowedSet[path] = struct{}{}
	}
	seen := make(map[string]struct{}, len(resolution.Files))
	for _, f := range resolution.Files {
		if _, ok := allowedSet[f.Path]; !ok {
			return fmt.Errorf("conflict resolution names %q, which is not one of the conflicting paths", f.Path)
		}
		if _, dup := seen[f.Path]; dup {
			return fmt.Errorf("conflict resolution names %q more than once", f.Path)
		}
		seen[f.Path] = struct{}{}
		for _, marker := range conflictMarkers {
			if strings.Contains(f.Resolution, marker) {
				return fmt.Errorf("conflict resolution for %q still carries a conflict marker", f.Path)
			}
		}
	}
	return nil
}

// attemptConflictSuggestion is US-11's whole orchestration: ask the resolved
// coding-agent tool's advisor for a suggested resolution, revalidate it
// locally, and persist it as a single, durable proposal — called from
// [Execute], after [prepareClone]'s [Options.CloneGate] acquisition has
// already been released by its own deferred release (S-10, AC-11.7): the
// model call this function may make never runs while another turn's git
// operations are serialised behind this one.
//
// ctx is the turn's own context, passed to advisor.ProposeConflictResolution
// exactly as received: internal/turn imposes no deadline of its own on any
// call, the same invariant [TestTurnImposesNoAmbientDeadline] already holds
// this package to for git (AC-PAR-3.6) — a model call is not a git command,
// but the reasoning is the same: a turn that waited behind a busy clone
// would otherwise hand this call less time than it needed for reasons that
// have nothing to do with the call itself. Bounding how long a
// [ConflictAdvisor] implementation may run is that implementation's own job,
// the same way [Options.CloneGate] bounds fetch and push at exec time rather
// than here (see internal/harness's ProposeSchedule for the pattern this
// story's own advisor implementation, when it exists, is expected to
// follow).
//
// It never turns a blocked-merge refusal into anything but a refusal
// (watch-out #4 of US-11): every way this can fail to produce a usable
// suggestion — suggestions disabled, nothing was collected to send, no
// advisor is registered for sr's agent kind, the call itself failed or was
// cancelled, local revalidation rejected the answer, or persistence failed —
// leaves result exactly as [syncRefused] built it, plus
// [Result.ConflictSuggestionFailed] set so the reply adds its one line
// (AC-11.2). Only a successful, persisted proposal sets
// [Result.ConflictProposal].
func attemptConflictSuggestion(ctx context.Context, opts Options, req Request, result Result, sr *syncRefusal, logger *slog.Logger) Result {
	if !opts.SuggestConflictResolution {
		// AC-11.3: nothing here may run, not even a log line that could be
		// mistaken for an attempt, when the feature is off.
		return result
	}
	if result.TriggerCommentID == 0 {
		// The issue-body trigger is the one path [Reporter.reportSyncRefusal]
		// posts nothing for at all (AC-2.16c's issue-body convention,
		// mirrored from [Reporter.reportAgentSwitchRefusal]): there is no
		// comment to attach a suggestion to, so nothing is collected,
		// nothing is sent to a model, and nothing is persisted for it.
		return result
	}
	if len(sr.files) == 0 || len(sr.excluded) != 0 {
		result.ConflictSuggestionReason = sr.collectionReason
		logger.Info("conflict-resolution suggestion: nothing usable was collected to send",
			slog.Int("excluded", len(sr.excluded)))
		result.ConflictSuggestionFailed = true
		return result
	}
	advisor, ok := conflictAdvisorFor(req.Agent, opts.Runners)
	if !ok {
		logger.Info("conflict-resolution suggestion: no advisor is registered for this coding-agent tool",
			slog.String("agent", string(req.Agent)))
		result.ConflictSuggestionFailed = true
		return result
	}

	resolution, err := advisor.ProposeConflictResolution(ctx, agent.ConflictResolutionRequest{
		BaseInputRef: sr.inputs.BaseRef, BaseInputSHA: sr.inputs.BaseSHA, TaskInputRef: sr.inputs.TaskRef, TaskInputSHA: sr.inputs.TaskSHA, InputsObserved: sr.inputs.Observed,
		Branch: sr.branch, Base: sr.base, TargetPhase: sr.inputs.TargetPhase, TargetRef: sr.inputs.TargetRef, LocalSHA: sr.branchSHA, TargetSHA: sr.baseSHA, Files: conflictAdvisorFiles(sr.snapshots),
	})
	if err != nil {
		logger.Warn("conflict-resolution advisor failed", slog.Any("error", err))
		result.ConflictSuggestionFailed = true
		return result
	}
	if err := agent.ValidateRegionConflictResolution(resolution, agent.ConflictResolutionRequest{Branch: sr.branch, Base: sr.base, Files: sr.files}); err != nil {
		result.ConflictSuggestionReason = err.Error()
		logger.Warn("conflict-resolution advisor produced an unusable response", slog.Any("error", err))
		result.ConflictSuggestionFailed = true
		return result
	}

	if opts.ConflictProposalTTL > 0 {
		now := time.Now().UTC()
		if _, err := opts.DB.ExpireConflictProposals(ctx, now.Add(-opts.ConflictProposalTTL), now); err != nil {
			logger.Warn("could not expire stale conflict proposals", slog.Any("error", err))
			result.ConflictSuggestionFailed = true
			return result
		}
	}

	validated, err := validateCollectedConflictResolution(sr.snapshots, resolution)
	if err != nil {
		result.ConflictSuggestionReason = err.Error()
		logger.Warn("conflict content refused", slog.Any("error", err))
		result.ConflictSuggestionFailed = true
		return result
	}
	if opts.ConflictPreviewer == nil {
		result.ConflictSuggestionFailed = true
		result.ConflictSuggestionReason = "preview renderer unavailable; resolve manually or regenerate"
		return result
	}

	rawPreview := rawConflictPreview(validated, string(req.Agent), sr.branchSHA)
	rawPreview.TargetPhase, rawPreview.TargetRef, rawPreview.TargetSHA = sr.inputs.TargetPhase, sr.inputs.TargetRef, sr.baseSHA
	rawPreview.BaseRef, rawPreview.BaseSHA, rawPreview.TaskRef, rawPreview.TaskSHA = sr.inputs.BaseRef, sr.inputs.BaseSHA, sr.inputs.TaskRef, sr.inputs.TaskSHA
	preview, previewErr := opts.ConflictPreviewer.PrepareConflictPreview(ctx, result.Ref, rawPreview)
	if previewErr != nil {
		result.ConflictSuggestionFailed = true
		result.ConflictSuggestionReason = previewErr.Error()
		return result
	}

	storeFiles := make([]store.ConflictProposalFile, 0, len(validated))
	for i, f := range validated {
		file := storedRegionFile(f)
		file.Details.Diff = preview.Files[i].Diff
		storeFiles = append(storeFiles, file)
	}
	proposal, _, err := opts.DB.CreateOrGetConflictProposal(ctx, store.CreateConflictProposalParams{
		SessionID: result.SessionID, TriggerCommentID: result.TriggerCommentID,
		Model: string(req.Agent), Files: storeFiles, Format: store.ConflictRegionFormat,
		BaseSHA: sr.baseSHA, BranchSHA: sr.branchSHA, Inputs: sr.inputs,
	})
	if err != nil {
		logger.Warn("could not persist the conflict-resolution proposal", slog.Any("error", err))
		result.ConflictSuggestionFailed = true
		return result
	}
	result.ConflictProposal = &proposal
	return result
}

func conflictAdvisorFiles(snapshots []git.ConflictSnapshot) []agent.ConflictFileContent {
	files := make([]agent.ConflictFileContent, 0, len(snapshots))
	for _, s := range snapshots {
		f := agent.ConflictFileContent{Path: s.Path}
		for _, r := range s.Regions {
			f.Regions = append(f.Regions, agent.ConflictRegionContent{ID: r.ID, Content: r.Content})
		}
		files = append(files, f)
	}
	return files
}
func validateCollectedConflictResolution(snapshots []git.ConflictSnapshot, response agent.ConflictResolution) ([]git.ValidatedConflict, error) {
	if len(snapshots) == 0 || len(snapshots) != len(response.Files) {
		return nil, fmt.Errorf("incomplete original snapshots")
	}
	byPath := map[string]agent.ConflictResolutionFile{}
	for _, f := range response.Files {
		byPath[f.Path] = f
	}
	var validated []git.ValidatedConflict
	var faults []error
	for _, s := range snapshots {
		f, ok := byPath[s.Path]
		if !ok {
			faults = append(faults, fmt.Errorf("missing path %q", s.Path))
			continue
		}
		var regions []git.RegionResolution
		for _, r := range f.Regions {
			if r.Resolution == nil {
				faults = append(faults, fmt.Errorf("missing resolution for %q", s.Path))
				continue
			}
			regions = append(regions, git.RegionResolution{ID: r.ID, Resolution: *r.Resolution})
		}
		v, err := git.ValidateConflict(s, regions)
		if err != nil {
			faults = append(faults, err)
			continue
		}
		validated = append(validated, v)
	}
	if err := errors.Join(faults...); err != nil {
		return nil, err
	}
	return validated, nil
}
