package report

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
)

const conflictDiffOmission = "\nDiff content omitted to fit the comment limit; all raw change statistics are shown.\n"

// PrepareConflictPreview sanitizes bounded diff text before persistence and
// refuses proposals whose complete metadata cannot be rendered.
func (r *Reporter) PrepareConflictPreview(ctx context.Context, ref naming.Ref, proposal ConflictProposal) (ConflictProposal, error) {
	p := proposal
	p.Files = append([]ConflictProposalFile(nil), proposal.Files...)
	for i, f := range p.Files {
		body, err := r.redactor.Apply(ctx, sanitize(f.Diff), 12<<10)
		if err != nil {
			return ConflictProposal{}, err
		}
		p.Files[i].Diff = body.String()
	}
	// Reserve the largest eventual numeric ID before the store allocates it.
	p.ID = 9223372036854775807
	if _, err := r.renderConflictPreview(ctx, Reply{Ref: ref, Kind: ReplySuggestsResolution, ConflictProposal: &p}); err != nil {
		return ConflictProposal{}, err
	}
	p.ID = proposal.ID
	return p, nil
}

func conflictPreviewMetadata(reply Reply) (string, error) {
	p := reply.ConflictProposal
	if p == nil || p.Format != 1 || len(p.Files) == 0 || p.BranchSHA == "" {
		return "", fmt.Errorf("report: %w: unsupported or incomplete region preview; regenerate", ErrInvalidArgument)
	}
	var rows strings.Builder
	for _, f := range p.Files {
		m := f.Metrics
		fmt.Fprintf(&rows, "%s | bytes %d -> %d | logical lines %d -> %d | +%d / -%d\n", strconv.Quote(sanitize(f.Path)), m.BeforeBytes, m.AfterBytes, m.BeforeLines, m.AfterLines, m.Additions, m.Deletions)
	}
	var b strings.Builder
	b.WriteString(ReplyMarker(reply.Ref) + "\n\n")
	target := "base branch"
	if p.TargetPhase == "task" {
		target = "remote task branch"
	}
	fmt.Fprintf(&b, "This task's branch conflicts with the %s. Conflicting path(s) (%d).\n\nSuggested resolution from %s (proposal %d).\n", target, len(p.Files), strconv.Quote(oneLine(p.Model)), p.ID)
	if p.TargetRef != "" {
		fmt.Fprintf(&b, "Target: %s at %s.\nBase input: %s at %s.\nTask input: %s at %s.\n", strconv.Quote(oneLine(p.TargetRef)), strconv.Quote(oneLine(p.TargetSHA)), strconv.Quote(oneLine(p.BaseRef)), strconv.Quote(oneLine(p.BaseSHA)), strconv.Quote(oneLine(p.TaskRef)), strconv.Quote(oneLine(taskInputSHA(p.TaskSHA))))
	}
	b.WriteString("Task-branch baseline SHA: " + strconv.Quote(oneLine(p.BranchSHA)) + "\n\n")
	fence := fenceFor(rows.String())
	b.WriteString(fence + "\n" + rows.String() + fence + "\n\n")
	b.WriteString("Statistics describe the exact raw result validated at confirmation. Go files passed syntax parsing only; no type checking or parsing of other languages.\n")
	b.WriteString("Diff is sanitized and possibly incomplete; displayed text is not the full raw source.\n")
	b.WriteString("This is a suggestion only: litefactory has not applied it. Surrounding file bytes are preserved; only identified conflict regions are resolved.\n")
	fmt.Fprintf(&b, "To apply the validated result, comment `/confirm %d apply this resolution`.\n", p.ID)
	return b.String(), nil
}

func (r *Reporter) renderConflictPreview(ctx context.Context, reply Reply) (redact.Body, error) {
	metadata, err := conflictPreviewMetadata(reply)
	if err != nil {
		return redact.Body{}, err
	}
	// Redact before budgeting so metadata expansion cannot erase later rows.
	safe, err := r.redactor.Apply(ctx, metadata, 4<<20)
	if err != nil {
		return redact.Body{}, err
	}
	metadata = safe.String()
	if len(metadata)+len(conflictDiffOmission) > r.budget {
		return redact.Body{}, fmt.Errorf("report: complete conflict metadata exceeds outbound limit; resolve manually or regenerate")
	}
	var diffs strings.Builder
	omitted := false
	for _, f := range reply.ConflictProposal.Files {
		content := sanitize(f.Diff)
		if content == "" {
			continue
		}
		fence := fenceFor(content)
		block := "\n" + fence + "\n" + strconv.Quote(sanitize(f.Path)) + "\n" + content + "\n" + fence + "\n"
		safeBlock, err := r.redactor.Apply(ctx, block, 64<<10)
		if err != nil {
			return redact.Body{}, err
		}
		block = safeBlock.String()
		if len(metadata)+diffs.Len()+len(block)+len(conflictDiffOmission) > r.budget {
			omitted = true
			continue
		}
		diffs.WriteString(block)
	}
	if omitted {
		diffs.WriteString(conflictDiffOmission)
	}
	return r.apply(ctx, metadata+diffs.String())
}

func taskInputSHA(sha string) string {
	if sha == "" {
		return "absent"
	}
	return sha
}
