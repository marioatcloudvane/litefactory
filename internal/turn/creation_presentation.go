package turn

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// creationPresentationPurpose prefixes the delivery purpose of a retained
// creation proposal's presentation comment. The full purpose is this prefix
// plus the proposal hash, so one proposal on one trigger has one delivery.
//
// This prefix, like source-bound creation intake outcomes, is exempt from the
// legacy runless-ambiguity probe (see dispatchDelivery): these deliveries exist
// solely after the delivery journal and always carry their delivery marker.
const creationPresentationPurpose = "creation-proposal:"

// CreationPresenter is the optional capability the daemon reaches through an
// interface assertion on its Reporter. *Reporter implements it.
type CreationPresenter interface {
	ReportCreationPresentation(ctx context.Context, runID int64) (Posted, error)
}

var _ CreationPresenter = (*Reporter)(nil)

// ReportCreationPresentation posts the owner-facing comment for the proposal
// the run retained: the exact envelope, its hash and the approval command.
//
// The comment is a run-less delivery (RunID 0) under the purpose
// "creation-proposal:<hash>", so it has its own identity. It reuses the
// existing creation-proposal reply body but, unlike [Reporter.ReportReply],
// never asks whether the run's trigger was already consumed: the trigger of a
// finished run has normally been consumed by the run's result comment, and that
// must not suppress the presentation. Nor does it touch that result comment,
// the run row or runs.result_comment_id.
//
// Ordering, none of it inside a transaction that spans the POST: render, durable
// intent, bind the delivery key into the bookkeeping row, POST, confirm the
// receipt, complete the delivery, and only then mark the row presented. A
// failure at any step leaves presented=0 and the next call resumes the same
// delivery; a crash between the POST and its confirmation is recovered by
// matching the existing comment on its delivery marker, so exactly one comment
// results. Nothing owed (no row, or already presented) returns a skipped
// [Posted] and no error.
//
// Completing a RunID-0 delivery is safe here. It advances the session watermark
// to the run's trigger with MAX semantics, so a trigger the run already
// consumed changes nothing and a newer, unconsumed owner comment stays
// unconsumed; a private (graph, automatic, schedule) trigger is skipped by the
// store and never touches the watermark. Callers should present after the run's
// own result was reported so the presentation is never the first thing to
// advance that watermark.
//
// Real GitHub outage behaviour (a POST accepted but its response lost, listing
// lag on recovery) is not observed; it is exercised only against the offline
// fake forge and remains unverified.
func (r *Reporter) ReportCreationPresentation(ctx context.Context, runID int64) (Posted, error) {
	if runID < 1 {
		return Posted{}, invalidRequest("a run id is required to present a creation proposal")
	}
	creation, err := store.NewCreationStore(r.db, nil)
	if err != nil {
		return Posted{}, fmt.Errorf("present creation proposal: %w", err)
	}
	row, err := creation.PendingCreationPresentation(ctx, runID)
	if errors.Is(err, store.ErrNotFound) {
		return Posted{Skipped: true}, nil
	}
	if err != nil {
		return Posted{}, err
	}
	run, err := r.db.GetRun(ctx, runID)
	if err != nil {
		return Posted{}, fmt.Errorf("present creation proposal: read run %d: %w", runID, err)
	}
	if run.SessionID != row.SessionID {
		return Posted{}, invalidRequest("run %d does not belong to the presentation's session", runID)
	}
	ref, err := naming.ParseRef(row.SourceTask)
	if err != nil {
		return Posted{}, fmt.Errorf("present creation proposal: %w", err)
	}
	proposal, err := creation.RetainedCreationProposal(ctx, row.ProposalHash)
	if err != nil {
		return Posted{}, fmt.Errorf("present creation proposal: %w", err)
	}
	if proposal.SourceTask != row.SourceTask || proposal.Hash != row.ProposalHash {
		return Posted{}, invalidRequest("retained proposal %s does not match its presentation row", row.ProposalHash)
	}
	publication := report.CreationPublicationUnknown
	if run.Mode == "research" {
		graph, graphErr := r.db.HasResearchGraphRun(ctx, run.ID)
		if graphErr != nil {
			return Posted{}, fmt.Errorf("present creation proposal: read workflow provenance: %w", graphErr)
		}
		publication = report.CreationPublicationLegacy
		if graph {
			publication = report.CreationPublicationGraph
		}
	}
	reply := report.Reply{Ref: ref, Kind: report.ReplyCreationProposal, Creation: &report.CreationReply{Envelope: string(proposal.Envelope), Hash: proposal.Hash, Relationship: proposal.Relationship, Publication: publication}}

	intent := r.deliveryIntent(ref, ref, row.SessionID, run.TriggerCommentID, 0, creationPresentationPurpose+proposal.Hash)
	if err := creation.BindCreationPresentationDelivery(ctx, runID, intent.Key); err != nil {
		return Posted{}, err
	}
	posted, err := r.deliver(ctx, intent, func() (redact.Body, error) {
		return r.renderer.RenderReply(ctx, reply)
	})
	if err != nil {
		return posted, err
	}
	if err := creation.MarkCreationPresented(context.WithoutCancel(ctx), runID); err != nil {
		return posted, err
	}
	return posted, nil
}

// isCreationPresentation reports whether purpose names a creation-proposal
// presentation delivery.
func isCreationPresentation(purpose string) bool {
	return strings.HasPrefix(purpose, creationPresentationPurpose)
}
