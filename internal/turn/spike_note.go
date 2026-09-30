package turn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"slices"
	"strings"
	"syscall"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// SpikeNoteName is the note every Spike path writes into its session directory.
const SpikeNoteName = "SPIKE.md"

// SpikeNoteActivity writes SPIKE.md from the retained source result and the
// status the graph node declares, then records that status durably so the one
// report can name it. The status comes from the graph; this step never picks it.
type SpikeNoteActivity struct {
	Profile      *graphexec.Profile
	Context      func(context.Context, store.GraphNodeAttempt) (agentresult.Context, error)
	Destinations func() (string, string, error)
}

// spikeNoteProof is the durable record of one written note.
type spikeNoteProof struct {
	Action          string `json:"spike_note"`
	Status          string `json:"status"`
	AttemptID       int64  `json:"attempt_id"`
	SourceAttemptID int64  `json:"source_attempt_id"`
	SourceRunID     int64  `json:"source_run_id"`
	Digest          string `json:"definition_digest"`
	CandidateDigest string `json:"candidate_digest"`
	Path            string `json:"path"`
}

// Execute writes the note. Any failure is the failed outcome with its cause.
func (h SpikeNoteActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	fail := func(err error) (ActivityResult, error) {
		return ActivityResult{Outcome: "failed", Diagnostic: err.Error()}, err
	}
	if h.Destinations == nil {
		return fail(errors.New("spike note requires its session destination"))
	}
	step, err := loadSpikeStep(ctx, opts, req, h.Profile, h.Context, SpikeNoteAction)
	if err != nil {
		return fail(err)
	}
	status := step.node.Attrs["lf_status"]
	if !slices.Contains(agentresult.SpikeStatuses, status) {
		return fail(fmt.Errorf("spike note status %q is not a declared status", status))
	}
	result := step.candidate.Result
	// A note records what the source said. A finished result can never be
	// recorded as asking, and an unfinished one only as asking, or as blocked
	// when the graph has nobody to ask.
	asked := step.candidate.Response == "ask"
	if result.Payload.Spike == nil || (status == "asking" && !asked) || (asked && status != "asking" && status != "blocked") {
		return fail(fmt.Errorf("spike note status %q does not fit a %q result", status, step.candidate.Response))
	}
	check, err := retainedSpikeCheck(ctx, opts.DB, step.attempt.InstanceID, step.source.ID)
	if err != nil {
		return fail(err)
	}
	// A note never records a fix as made and checked unless the measured check
	// of exactly this result says it passed.
	if status == "fixed" && (check == nil || check.Outcome != "passed") {
		return fail(errors.New("spike note fixed requires a passed check of this result"))
	}
	root, session, err := h.Destinations()
	if err != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if err := writeSessionDocument(root, session, SpikeNoteName, renderSpikeNote(status, result, check.view())); err != nil {
		return fail(err)
	}
	digest, err := researchCandidateDigest(result)
	if err != nil {
		return fail(err)
	}
	raw, err := json.Marshal(spikeNoteProof{Action: SpikeNoteAction, Status: status, AttemptID: step.attempt.ID, SourceAttemptID: step.source.ID, SourceRunID: step.source.RunID, Digest: step.pin.Digest, CandidateDigest: digest, Path: path.Join(session, SpikeNoteName)})
	if err != nil {
		return fail(err)
	}
	return ActivityResult{Outcome: "success", Kind: store.GraphOutputJSON, Output: raw}, nil
}

// annotateSpike returns the candidate with the status of the note that was
// recorded for exactly this source and content. Without such a note the report
// is refused, so a report can never precede its note.
func annotateSpike(ctx context.Context, db *store.DB, source store.GraphNodeAttempt, kind string, candidate agentresult.Result) (agentresult.Result, error) {
	if candidate.Payload.Spike == nil {
		return candidate, errors.New("spike report requires a Spike result")
	}
	digest, err := researchCandidateDigest(candidate)
	if err != nil {
		return candidate, err
	}
	instance, err := db.GetGraphInstance(ctx, source.InstanceID)
	if err != nil {
		return candidate, err
	}
	pin, err := db.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return candidate, err
	}
	attempts, err := db.ListGraphNodeAttempts(ctx, source.InstanceID)
	if err != nil {
		return candidate, err
	}
	var recorded *spikeNoteProof
	for _, attempt := range attempts {
		if attempt.ID <= source.ID || attempt.Status != store.GraphSucceeded || attempt.Outcome != "success" {
			continue
		}
		outputs, err := db.ListGraphOutputsBounded(ctx, attempt.ID, agentresult.MaxResearchNodeBytes, 16)
		if err != nil {
			return candidate, err
		}
		for _, output := range outputs {
			if output.Kind != store.GraphOutputJSON {
				continue
			}
			var proof spikeNoteProof
			decoder := json.NewDecoder(bytes.NewReader(output.Payload))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&proof) != nil || proof.Action != SpikeNoteAction {
				continue
			}
			if proof.AttemptID != attempt.ID || proof.Digest != pin.Digest {
				return candidate, errors.New("spike note identity mismatch")
			}
			if proof.SourceAttemptID == source.ID && proof.SourceRunID == source.RunID && proof.CandidateDigest == digest {
				recorded = &proof
			}
		}
	}
	if recorded == nil {
		return candidate, errors.New("spike report requires a recorded note for this result")
	}
	if kind == "question" != (recorded.Status == "asking") {
		return candidate, fmt.Errorf("spike %s report does not fit the recorded %q note", kind, recorded.Status)
	}
	spike := *candidate.Payload.Spike
	spike.Status = recorded.Status
	candidate.Payload.Spike = &spike
	candidate = settleUnaskedQuestions(candidate, kind, recorded.Status)
	spike = *candidate.Payload.Spike
	check, err := retainedSpikeCheck(ctx, db, source.InstanceID, source.ID)
	if err != nil {
		return candidate, err
	}
	spike.Check = check.view()
	issue, err := retainedSpikeIssue(ctx, db, source.InstanceID, source.ID)
	if err != nil {
		return candidate, err
	}
	spike.Issue = issue.view()
	candidate.Payload.Spike = &spike
	return candidate, nil
}

// renderSpikeNote is SPIKE.md: a note someone can read a week later.
func renderSpikeNote(status string, r agentresult.Result, check *agentresult.SpikeCheckView) string {
	spike := r.Payload.Spike
	var b strings.Builder
	b.WriteString("# Spike\n\nStatus: `" + status + "`\n\n## Request\n\n")
	b.WriteString(blankFallback(spike.Request, "_The request was not recorded._"))
	list := func(title string, items []string, empty string) {
		b.WriteString("\n\n## " + title + "\n\n")
		if len(items) == 0 {
			b.WriteString(empty)
			return
		}
		for i, item := range items {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString("- " + strings.TrimSpace(item))
		}
	}
	list("What was done", spike.WorkDone, "_Nothing was recorded._")
	list("What was found", spike.Findings, "_Nothing was recorded._")
	list("Decisions worth remembering", spike.Decisions, "_None._")
	list("Not done", spike.NotDone, "_Nothing was left undone._")
	if fix := spike.ProposedFix; fix != nil {
		list("Proposed change", []string{
			"Defects: " + strings.Join(fix.Defects, "; "),
			"Files: " + strings.Join(fix.Files, ", "),
			"Kinds of change: " + strings.Join(fix.ChangeKinds, ", "),
			"Regression test: " + strings.TrimSpace(fix.RegressionTest),
		}, "")
	}
	if fix := spike.FixReport; fix != nil {
		list("Change made", []string{
			"Defect: " + strings.TrimSpace(fix.Defect),
			"Files claimed: " + strings.Join(fix.Files, ", "),
			"Regression test: " + strings.TrimSpace(fix.RegressionTest),
			"Summary: " + strings.TrimSpace(fix.Summary),
		}, "")
	}
	if check != nil {
		b.WriteString("\n\n## Measured change and checks\n\n" + renderSpikeCheck(check))
	}
	if len(r.Questions) > 0 {
		b.WriteString("\n\n## Open questions\n")
		for _, q := range r.Questions {
			b.WriteString("\n- **" + q.ID + "** " + strings.TrimSpace(q.Text))
			for _, o := range q.Options {
				b.WriteString("\n  - `" + o.ID + "` " + strings.TrimSpace(o.Text))
				if o.ID == q.RecommendedOption {
					b.WriteString(" (recommended)")
				}
			}
		}
		if reason := strings.TrimSpace(r.Reason); reason != "" {
			b.WriteString("\n\nWhy the owner is asked: " + reason)
		}
	}
	b.WriteString("\n\n## Recommendation\n\n")
	if r.Recommendation != nil && strings.TrimSpace(r.Recommendation.Text) != "" {
		b.WriteString(strings.TrimSpace(r.Recommendation.Text))
	} else {
		b.WriteString("_None._")
	}
	return strings.TrimSpace(b.String()) + "\n"
}

// writeSessionDocument writes one document into the admitted session directory
// of a trusted worktree. Resolution is confined to the worktree, symlinks are
// refused at every component and the file must be regular.
func writeSessionDocument(worktree, session, name, body string) error {
	if !agentresult.ValidPath(session) {
		return errors.New("spike note: invalid session path")
	}
	root, err := os.OpenRoot(worktree)
	if err != nil {
		return fmt.Errorf("spike note root: %w", err)
	}
	defer root.Close()
	parts := strings.Split(session, "/")
	for i := range parts {
		info, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			return fmt.Errorf("inspect spike note directory: %w", err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("spike note directory has unsafe ancestry")
		}
	}
	if err := root.MkdirAll(session, 0o750); err != nil {
		return fmt.Errorf("spike note directory: %w", err)
	}
	dir, err := root.OpenRoot(session)
	if err != nil {
		return fmt.Errorf("open spike note directory: %w", err)
	}
	defer dir.Close()
	if info, err := dir.Lstat(name); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("spike note %s is not a regular file", name)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect %s: %w", name, err)
	}
	file, err := dir.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	_, writeErr := file.WriteString(body)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}

// settleUnaskedQuestions turns the questions of a step that wanted to ask but has
// nobody to ask into work left undone. What it wanted to know is kept as not
// done, and no question is put to anyone. Any other report is returned as is.
func settleUnaskedQuestions(candidate agentresult.Result, kind, status string) agentresult.Result {
	if kind != "final" || status != "blocked" || candidate.Outcome != "incomplete" || len(candidate.Questions) == 0 || candidate.Payload.Spike == nil {
		return candidate
	}
	spike := *candidate.Payload.Spike
	spike.NotDone = slices.Clone(spike.NotDone)
	for _, q := range candidate.Questions {
		spike.NotDone = append(spike.NotDone, "Not asked, nobody was available to answer: "+strings.TrimSpace(q.Text))
	}
	candidate.Questions = nil
	candidate.Payload.Spike = &spike
	return candidate
}
