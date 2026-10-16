package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

type advisoryPublication struct {
	Intent       store.ReportDelivery `json:"intent"`
	Text         string               `json:"text"`
	Digest       string               `json:"digest"`
	RequestID    int64                `json:"request_id,omitempty"`
	ReportDigest string               `json:"report_digest,omitempty"`
}

// executeAdvisoryComment selects only the named completed judge attempt. It has
// no workflow result decoder, source effect or owner-wait authority.
func executeAdvisoryComment(ctx context.Context, opts Options, req ActivityRequest, profile *graphexec.Profile, reporter *Reporter) (ActivityResult, error) {
	if opts.DB == nil || reporter == nil || reporter.db != opts.DB || profile == nil || (req.Mode != agent.ModeResearch && req.Mode != agent.ModePlan) {
		return ActivityResult{}, errors.New("advisory publication requires Research or Plan factory context")
	}
	node, ok := profile.Node(req.Node)
	if !ok || node.Action != ResearchCommentAction || node.Attrs["lf_kind"] != "advisory" {
		return ActivityResult{}, errors.New("only an explicit advisory comment can publish judging")
	}
	sourceNode, ok := profile.Node(node.Attrs["lf_source"])
	if !ok || sourceNode.Action != graphexec.JudgeAction {
		return ActivityResult{}, errors.New("advisory source must name a judge node")
	}
	current, err := opts.DB.GetGraphNodeAttempt(ctx, req.NodeAttemptID)
	if err != nil {
		return ActivityResult{}, err
	}
	instance, err := opts.DB.GetGraphInstance(ctx, req.InstanceID)
	if err != nil {
		return ActivityResult{}, err
	}
	pin, err := opts.DB.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return ActivityResult{}, err
	}
	if current.InstanceID != instance.ID || current.Node != node.Name || current.Status != store.GraphRunning || instance.CurrentNode != node.Name || pin.Mode != string(req.Mode) || pin.Team != profile.Team || (req.Mode != agent.ModeResearch && pin.Digest != profile.Digest) {
		return ActivityResult{}, errors.New("advisory publication attempt binding mismatch")
	}
	// Reentry deliberately selects new advice; recovery of an existing publishing
	// attempt always uses its retained intent, even if newer source work exists.
	purposePrefix := fmt.Sprintf("advisory:%d:%d:", instance.ID, current.ID)
	after := ""
	for {
		comments, err := opts.DB.ListGraphComments(ctx, instance.ID, after, 100)
		if err != nil {
			return ActivityResult{}, err
		}
		for _, c := range comments {
			if c.Node != node.Name || c.Kind != "advisory" {
				continue
			}
			intent, err := opts.DB.GetGraphComment(ctx, c.Key)
			if err != nil {
				return ActivityResult{}, err
			}
			var retained advisoryPublication
			if json.Unmarshal(intent.Candidate, &retained) == nil && retained.Intent.Purpose == purposePrefix+retained.Digest {
				posted, err := reporter.RecoverAdvisoryComment(ctx, c.Key)
				return advisoryActivityResult(posted, c.Key, intent.SourceAttemptID, err)
			}
		}
		if len(comments) < 100 {
			break
		}
		after = comments[len(comments)-1].Key
	}
	source, err := opts.DB.LatestGraphNodeAttempt(ctx, instance.ID, sourceNode.Name)
	if err != nil {
		return ActivityResult{}, fmt.Errorf("advisory source unavailable: %w", err)
	}
	if source.Status != store.GraphSucceeded && source.Status != store.GraphFailed {
		return ActivityResult{}, errors.New("advisory source has no completed attempt")
	}
	outputs, err := opts.DB.ListGraphOutputsBounded(ctx, source.ID, 1<<20, 8)
	if err != nil {
		return ActivityResult{}, err
	}
	var selected workflowJudgeOutput
	count := 0
	for _, output := range outputs {
		if output.Kind != store.GraphOutputJSON {
			continue
		}
		var value workflowJudgeOutput
		if json.Unmarshal(output.Payload, &value) == nil && value.Execution != "" {
			selected = value
			count++
		}
	}
	if count != 1 {
		return ActivityResult{}, errors.New("advisory source has missing or ambiguous retained judge output")
	}
	text := selected.Failure
	if selected.RequestID != 0 {
		request, err := opts.DB.GetJudgeRequest(ctx, selected.RequestID)
		if err != nil {
			return ActivityResult{}, err
		}
		if request.CallerKind != "workflow" || request.GraphInstanceID != instance.ID || request.JudgeNodeAttemptID != source.ID || judgeDigest(request.Report) != selected.ReportDigest {
			return ActivityResult{}, errors.New("advisory report binding or digest mismatch")
		}
		text, err = savedJudgeText(request.Report)
		if err != nil {
			return ActivityResult{}, err
		}
	}
	if text == "" {
		return ActivityResult{}, errors.New("advisory source has no report or failure explanation")
	}
	session, err := opts.DB.GetSession(ctx, instance.SessionID)
	if err != nil {
		return ActivityResult{}, err
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		return ActivityResult{}, err
	}
	digest := judgeDigest([]byte(text))
	// Factory activities are runless: publication must not finalize or consume the
	// source agent run or any owner reply.
	intent := reporter.deliveryIntent(ref, ref, session.ID, 0, 0, purposePrefix+digest)
	publication := advisoryPublication{Intent: intent, Text: text, Digest: digest, RequestID: selected.RequestID, ReportDigest: selected.ReportDigest}
	raw, err := json.Marshal(publication)
	if err != nil {
		return ActivityResult{}, err
	}
	if err := opts.DB.RetainGraphComment(ctx, store.GraphCommentIntent{Key: intent.Key, InstanceID: instance.ID, SourceAttemptID: source.ID, Node: node.Name, Kind: "advisory", Candidate: raw}); err != nil {
		return ActivityResult{}, err
	}
	posted, err := reporter.deliver(ctx, intent, func() (redact.Body, error) { return reporter.renderer.RenderReportDelivery(ctx, text, "") })
	return advisoryActivityResult(posted, intent.Key, source.ID, err)
}

func advisoryActivityResult(posted Posted, key string, sourceID int64, err error) (ActivityResult, error) {
	if err != nil {
		return ActivityResult{}, err
	}
	raw, err := json.Marshal(struct {
		DeliveryKey     string `json:"delivery_key"`
		CommentID       int64  `json:"comment_id"`
		SourceAttemptID int64  `json:"source_attempt_id"`
	}{key, posted.CommentID, sourceID})
	return ActivityResult{Outcome: "published", Kind: store.GraphOutputJSON, Output: raw}, err
}

// RecoverAdvisoryComment uses the exact retained bytes through the delivery
// journal. It does not invoke judging, source hooks or an owner gate.
func (r *Reporter) RecoverAdvisoryComment(ctx context.Context, key string) (Posted, error) {
	intent, err := r.db.GetGraphComment(ctx, key)
	if err != nil {
		return Posted{}, err
	}
	if intent.Kind != "advisory" {
		return Posted{}, errors.New("not an advisory publication")
	}
	delivery, err := r.db.GetReportDelivery(ctx, key)
	if err == nil {
		return r.dispatchDelivery(ctx, delivery)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return Posted{}, err
	}
	var retained advisoryPublication
	if json.Unmarshal(intent.Candidate, &retained) != nil || retained.Intent.Key != key || retained.Text == "" || retained.Digest != judgeDigest([]byte(retained.Text)) {
		return Posted{}, errors.New("advisory retained bytes or identity mismatch")
	}
	instance, err := r.db.GetGraphInstance(ctx, intent.InstanceID)
	if err != nil {
		return Posted{}, err
	}
	if retained.Intent.SessionID != instance.SessionID || retained.Intent.RunID != 0 {
		return Posted{}, errors.New("advisory delivery scope mismatch")
	}
	return r.deliver(ctx, retained.Intent, func() (redact.Body, error) { return r.renderer.RenderReportDelivery(ctx, retained.Text, "") })
}
