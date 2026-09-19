package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// ResearchCommentAction names the explicit durable publication activity.
const ResearchCommentAction = "lf_research_comment"

// ResearchCommentActivity publishes only explicit graph comments. Failure, when
// configured, supplies an already-authored application candidate for a failed
// attempt; it must not invoke an agent or change the original process outcome.
type ResearchCommentActivity struct {
	Profile  *graphexec.Profile
	Reporter *Reporter
	Context  func(context.Context, store.GraphNodeAttempt) (agentresult.Context, error)
	Failure  func(context.Context, store.GraphNodeAttempt) (agentresult.Result, error)
}

// ResearchCommentSpec declares the closed publication kinds and confirmed outcome.
func ResearchCommentSpec() graphexec.ActionSpec {
	return graphexec.ActionSpec{Name: ResearchCommentAction, Mode: agent.ModeResearch, Outcomes: []string{"published"}, Attributes: map[string]graphexec.AttributeSpec{"lf_kind": {Required: true, Values: []string{"question", "review", "final", "failure"}}}}
}

// RegisterResearchComment installs the validated specification and its publication handler.
func RegisterResearchComment(specs *graphexec.Registry, activities *ActivityRegistry, h ResearchCommentActivity) error {
	if specs == nil || activities == nil || h.Profile == nil || h.Reporter == nil || h.Context == nil {
		return errors.New("research comment requires pinned profile, reporter and trusted context")
	}
	if _, ok := activities.Resolve(ResearchCommentAction); ok {
		return errors.New("research comment already registered")
	}
	if err := specs.Register(ResearchCommentSpec()); err != nil {
		return err
	}
	return activities.Register(ResearchCommentAction, h)
}

// Execute publishes the validated retained candidate bound to this reserved graph activity.
func (h ResearchCommentActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	if opts.DB == nil || h.Reporter == nil || h.Reporter.db != opts.DB || h.Profile == nil || h.Context == nil || req.Mode != agent.ModeResearch {
		return ActivityResult{}, errors.New("research comment requires factory Research context")
	}
	instance, err := opts.DB.GetGraphInstance(ctx, req.InstanceID)
	if err != nil {
		return ActivityResult{}, err
	}
	definition, err := opts.DB.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return ActivityResult{}, err
	}
	node, ok := h.Profile.Node(req.Node)
	if !ok || node.Action != ResearchCommentAction || definition.Digest != h.Profile.Digest || definition.Mode != "research" {
		return ActivityResult{}, errors.New("research comment profile binding mismatch")
	}
	current, err := opts.DB.GetGraphNodeAttempt(ctx, req.NodeAttemptID)
	if err != nil {
		return ActivityResult{}, err
	}
	if current.InstanceID != instance.ID || current.Node != req.Node || current.Status != store.GraphRunning || instance.CurrentNode != req.Node {
		return ActivityResult{}, errors.New("research comment attempt binding mismatch")
	}
	source, err := latestResearchAgentAttempt(ctx, opts, h.Profile, instance.ID, graphexec.ResearchSource(node.Attrs))
	if err != nil {
		return ActivityResult{}, err
	}
	if source.ID == 0 {
		return ActivityResult{}, errors.New("research comment has no source attempt")
	}
	scope, err := h.Context(ctx, source)
	if err != nil {
		return ActivityResult{}, err
	}
	kind := node.Attrs["lf_kind"]
	var candidate agentresult.Result
	if kind == "failure" {
		if h.Failure == nil || source.Status != store.GraphFailed {
			return ActivityResult{}, errors.New("research failure comment requires retained failed-attempt candidate")
		}
		candidate, err = h.Failure(ctx, source)
	} else {
		var output agentresult.ResearchNode
		output, err = LoadResearchNodeOutput(ctx, opts.DB, source.ID, scope)
		candidate = output.Result
	}
	if err != nil {
		return ActivityResult{}, err
	}
	posted, key, err := h.Reporter.ReportResearchCandidate(ctx, source, req.Node, kind, candidate, scope, researchNextStep(h.Profile, req.Node, kind))
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	payload, err := json.Marshal(struct {
		DeliveryKey     string `json:"delivery_key"`
		CommentID       int64  `json:"comment_id"`
		SourceAttemptID int64  `json:"source_attempt_id"`
	}{key, posted.CommentID, source.ID})
	if err != nil {
		return ActivityResult{}, err
	}
	return ActivityResult{Outcome: "published", Kind: store.GraphOutputJSON, Output: payload}, nil
}

// ReportResearchCandidate retains exact authored bytes before rendering. The
// content digest is part of the operation identity: corrections cannot overwrite
// or replay an earlier uncertain effect. No run finalization or git save occurs.
func (r *Reporter) ReportResearchCandidate(ctx context.Context, source store.GraphNodeAttempt, node, kind string, candidate agentresult.Result, scope agentresult.Context, next string) (Posted, string, error) {
	if scope.Mode != "research" {
		return Posted{}, "", errors.New("research comment context has different mode")
	}
	d := agentresult.ValidateResult(candidate, scope)
	if !d.Valid || !d.Complete {
		return Posted{}, "", resultDiagnosticError(d)
	}
	switch kind {
	case "question":
		if candidate.Outcome != "incomplete" || len(candidate.Questions) == 0 || source.Status != store.GraphSucceeded {
			return Posted{}, "", errors.New("question requires successful incomplete Research with questions")
		}
	case "review":
		// The work the agent would otherwise have called done, shown to the
		// owner before it counts. Complete like a final, but settling nothing.
		if candidate.Outcome != "completed" || len(candidate.Questions) != 0 || source.Status != store.GraphSucceeded {
			return Posted{}, "", errors.New("review requires successful completed Research without questions")
		}
	case "final":
		if candidate.Outcome != "completed" || len(candidate.Questions) != 0 || source.Status != store.GraphSucceeded {
			return Posted{}, "", errors.New("final requires successful completed Research without questions")
		}
	case "failure":
		if candidate.Outcome == "completed" || source.Status != store.GraphFailed || candidate.Reason == "" {
			return Posted{}, "", errors.New("failure requires failed attempt and actionable incomplete candidate")
		}
	default:
		return Posted{}, "", errors.New("unknown Research comment kind")
	}
	raw, err := json.Marshal(candidate)
	if err != nil {
		return Posted{}, "", err
	}
	if len(raw) > agentresult.MaxResearchNodeBytes {
		return Posted{}, "", errors.New("research comment candidate exceeds bound")
	}
	instance, err := r.db.GetGraphInstance(ctx, source.InstanceID)
	if err != nil {
		return Posted{}, "", err
	}
	run, err := r.db.GetRun(ctx, source.RunID)
	if err != nil {
		return Posted{}, "", err
	}
	session, err := r.db.GetSession(ctx, instance.SessionID)
	if err != nil {
		return Posted{}, "", err
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		return Posted{}, "", err
	}
	sum := sha256.Sum256(raw)
	purpose := fmt.Sprintf("research-comment:%d:%d:%s:%s:%s", source.InstanceID, source.ID, node, kind, hex.EncodeToString(sum[:]))
	intent := r.deliveryIntent(ref, ref, instance.SessionID, run.TriggerCommentID, run.ID, purpose)
	if err := r.db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: intent.Key, InstanceID: source.InstanceID, SourceAttemptID: source.ID, Node: node, Kind: kind, Candidate: raw}); err != nil {
		return Posted{}, intent.Key, err
	}
	posted, err := r.deliver(ctx, intent, func() (redact.Body, error) {
		return r.renderer.RenderReportDelivery(ctx, renderResearchComment(candidate, next, researchArtifactLinks(ref, session.Branch, candidate.Artifacts)), "")
	})
	return posted, intent.Key, err
}

// RecoverResearchComment retries only the original durable publication. It does
// not reserve a graph attempt, invoke a process, save artifacts or alter outcomes.
func (r *Reporter) RecoverResearchComment(ctx context.Context, key string, scope agentresult.Context) (Posted, error) {
	intent, err := r.db.GetGraphComment(ctx, key)
	if err != nil {
		return Posted{}, err
	}
	delivery, err := r.db.GetReportDelivery(ctx, key)
	if err == nil {
		return r.dispatchDelivery(ctx, delivery)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return Posted{}, err
	}
	// A render failure still leaves the candidate durable. Revalidate it under
	// factory context and reconstruct the exact operation, never an agent call.
	var candidate agentresult.Result
	diagnostics := agentresult.Decode(intent.Candidate, &candidate)
	if !diagnostics.Valid || !diagnostics.Complete {
		return Posted{}, resultDiagnosticError(diagnostics)
	}
	source, err := r.db.GetGraphNodeAttempt(ctx, intent.SourceAttemptID)
	if err != nil {
		return Posted{}, err
	}
	// Recovery re-posts a comment whose first delivery was uncertain. It has no
	// parsed graph, so it omits the footer rather than guessing one: the
	// delivery identity is the candidate's bytes, not the rendered body, so a
	// recovered comment is still the same effect.
	posted, recoveredKey, err := r.ReportResearchCandidate(ctx, source, intent.Node, intent.Kind, candidate, scope, "")
	if recoveredKey != key && err == nil {
		return Posted{}, errors.New("research recovery identity changed")
	}
	return posted, err
}

// latestResearchAgentAttempt resolves the agent result a control or comment node
// reads. Naming a source is what keeps a later stage from inheriting an earlier
// one's response: an unnamed source reads whichever agent ran last, which is the
// single-loop behaviour, while a named one has no attempt of its own until its
// stage actually runs and so bootstraps from the node's own default.
func latestResearchAgentAttempt(ctx context.Context, opts Options, profile *graphexec.Profile, instanceID int64, scope string) (store.GraphNodeAttempt, error) {
	var latest store.GraphNodeAttempt
	for _, n := range profile.Nodes {
		if n.Action != ResearchAgentAction || (scope != "" && n.Name != scope) {
			continue
		}
		candidate, err := opts.DB.LatestGraphNodeAttempt(ctx, instanceID, n.Name)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return store.GraphNodeAttempt{}, err
		}
		if candidate.ID > latest.ID {
			latest = candidate
		}
	}
	return latest, nil
}

// researchAudienceHeadings names each audience for the reader. The order is
// fixed rather than taken from the candidate, so the same question set always
// renders the same way and two people can be told where their part starts.
var researchAudienceHeadings = []struct{ audience, heading string }{
	{"business", "Product Owner Questions"},
	{"technical", "Technical Questions"},
}

// renderResearchComment lays out an agent's candidate for the owner. The agent
// supplies prose and a structured question set; the shape is the factory's, so
// every report reads the same way. Identities and the agent's recommendation are
// rendered because they are what the owner answers with: without them a reply
// has to quote text back, and a proposed answer nobody sees is not a proposal.
// artifactLink is one saved document the owner can open.
type artifactLink struct{ label, path, url string }

var researchArtifactLabels = map[string]string{
	"brief":     "Research brief",
	"technical": "Technical findings",
	"data":      "Reference data",
}

// researchArtifactLinks points at the branch rather than a commit, which is the
// opposite of what a run report does and is right for a different reason. A run
// report is a statement about one run, so it pins to that run's sha. These
// documents are not: the factory renders them from the retained result and
// replaces them in place, so the current branch copy is always the one the
// reader wants. Pinning would hand them a superseded draft.
//
// It also could not pin if it wanted to. A comment is published mid-run, before
// the run saves, so at this moment there is no commit holding these files.
func researchArtifactLinks(ref naming.Ref, branch string, artifacts []agentresult.Artifact) []artifactLink {
	if ref.Forge != naming.ForgeGitHub || ref.Validate() != nil || branch == "" {
		return nil
	}
	out := make([]artifactLink, 0, len(artifacts))
	for _, a := range artifacts {
		label, known := researchArtifactLabels[a.Kind]
		if !known || !agentresult.ValidPath(a.Path) {
			continue
		}
		out = append(out, artifactLink{
			label: label,
			path:  a.Path,
			url:   "https://github.com/" + ref.Owner + "/" + ref.Repo + "/blob/" + branch + "/" + a.Path,
		})
	}
	return out
}

func renderResearchComment(candidate agentresult.Result, next string, artifacts []artifactLink) string {
	var body strings.Builder
	body.WriteString(strings.TrimSpace(candidate.Comment))
	if reason := strings.TrimSpace(candidate.Reason); reason != "" {
		// The same field means two things. With questions below it, it explains
		// why the owner is being asked; without them, why the work stopped
		// short. Naming it accurately is the difference between a heading that
		// orients the reader and one they have to decode.
		heading := "Why this is unfinished"
		if len(candidate.Questions) > 0 {
			heading = "Why I need your input"
		}
		fmt.Fprintf(&body, "\n\n## %s\n\n%s", heading, reason)
	}
	// Numbered across the whole report rather than within each group, so "the
	// third question" means one thing no matter who is reading. The identifier
	// stays visible beside it because that is what an answer refers to.
	asked := 0
	for _, group := range researchAudienceHeadings {
		questions := make([]agentresult.Question, 0, len(candidate.Questions))
		for _, q := range candidate.Questions {
			if q.Audience == group.audience {
				questions = append(questions, q)
			}
		}
		if len(questions) == 0 {
			continue
		}
		body.WriteString("\n\n# ")
		body.WriteString(group.heading)
		for _, q := range questions {
			asked++
			fmt.Fprintf(&body, "\n\n## Question %d  ·  %s", asked, strings.TrimSpace(q.Text))
			fmt.Fprintf(&body, "\n\n_Answer with `%s`_", q.ID)
			if len(q.Options) == 0 {
				continue
			}
			body.WriteString("\n\n### Options\n")
			for _, o := range q.Options {
				fmt.Fprintf(&body, "\n- **`%s`** — %s", o.ID, strings.TrimSpace(o.Text))
				if o.ID == q.RecommendedOption {
					body.WriteString("  ✅ **recommended**")
				}
			}
		}
	}
	// An audience outside the rendered set would otherwise vanish from the
	// report while still counting as an asked question. Validation keeps the
	// set closed; this keeps a future widening from losing one silently.
	for _, q := range candidate.Questions {
		if !slices.ContainsFunc(researchAudienceHeadings, func(g struct{ audience, heading string }) bool { return g.audience == q.Audience }) {
			asked++
			fmt.Fprintf(&body, "\n\n## Question %d  ·  %s\n\n_Answer with `%s`_", asked, strings.TrimSpace(q.Text), q.ID)
		}
	}
	if len(artifacts) > 0 {
		body.WriteString("\n\n## Saved documents\n")
		for _, a := range artifacts {
			fmt.Fprintf(&body, "\n- [%s](%s)", a.label, a.url)
		}
	}
	if next != "" {
		body.WriteString("\n\n---\n\n**Next:** ")
		body.WriteString(next)
	}
	return body.String()
}

// researchNextStep says, in the owner's terms, what the factory does after this
// publication. It is derived from the graph rather than written by an agent, so
// it cannot drift, be forgotten, or disagree with what actually happens.
//
// It exists because a published comment is indistinguishable from a finished
// one. An owner who replies to a report while the graph is mid-flight does not
// nudge it along: admission refuses a running instance outright, and a parked
// one without a matching claimed wait is refused too. Saying which state this
// is turns a natural action into an informed one.
func researchNextStep(profile *graphexec.Profile, node, kind string) string {
	if profile == nil {
		return ""
	}
	switch kind {
	case "review":
		return "more questions, want me to dig deeper, or shall we plan this out?\n" +
			"Reply with what you want changed, say **`done`** to close this out as it stands, or comment **`/plan`** to approve it and start planning straight away.\n" +
			"Nothing moves on until you say so."
	case "question":
		return "reply on this issue to continue. Answer by option id, answer in your own words, or say `proceed` to let the factory assume the recommended answers. Nothing runs until you reply."
	case "failure":
		return "this attempt failed and no further work is queued. Comment again to start a fresh one."
	case "final":
		target, ok := graphexec.Decide(profile, node, "published")
		if !ok {
			return ""
		}
		if after, found := profile.Node(target); found && after.Terminal {
			return "this workflow is finished. Comment `/plan` to turn the agreed scope into stories."
		}
		return "the next stage starts on its own within a minute or two. No action is needed, and commenting before it finishes will interrupt it."
	}
	return ""
}

// researchNodeIsReview reports whether a node publishes a review gate.
func researchNodeIsReview(profile *graphexec.Profile, node string) bool {
	if profile == nil {
		return false
	}
	n, ok := profile.Node(node)
	return ok && n.Attrs["lf_kind"] == "review"
}
