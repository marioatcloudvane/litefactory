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
	"syscall"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// ResearchBriefAction names the Research-only document generation effect.
const ResearchBriefAction = "lf_research_brief"

// ResearchBriefActivity is the scoped document effect. Destinations is supplied
// by the admitted invocation, never by the candidate or graph attributes.
type ResearchBriefActivity struct {
	Profile      *graphexec.Profile
	Context      func(context.Context, store.GraphNodeAttempt) (agentresult.Context, error)
	Destinations func() (string, string, error)
}

type researchGeneration struct {
	Action           string                 `json:"research_generation"`
	AttemptID        int64                  `json:"attempt_id"`
	SourceAttemptID  int64                  `json:"source_attempt_id"`
	SourceRunID      int64                  `json:"source_run_id"`
	DefinitionDigest string                 `json:"definition_digest"`
	CandidateDigest  string                 `json:"candidate_digest"`
	Session          string                 `json:"session"`
	Artifacts        []agentresult.Artifact `json:"artifacts"`
}

// ResearchBriefSpec declares successful and failed document generation outcomes.
func ResearchBriefSpec() graphexec.ActionSpec {
	return graphexec.ActionSpec{Name: ResearchBriefAction, Mode: agent.ModeResearch, Outcomes: []string{"success", "failed"}}
}

func registerResearchBrief(specs *graphexec.Registry, activities *ActivityRegistry, profile *graphexec.Profile, scope func(context.Context, store.GraphNodeAttempt) (agentresult.Context, error), destinations func() (string, string, error)) error {
	if err := specs.Register(ResearchBriefSpec()); err != nil {
		return err
	}
	return activities.Register(ResearchBriefAction, ResearchBriefActivity{Profile: profile, Context: scope, Destinations: destinations})
}

// Execute generates both documents and returns success bound to their retained source.
func (h ResearchBriefActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	fail := func(err error) (ActivityResult, error) {
		return ActivityResult{Outcome: "failed", Diagnostic: err.Error()}, err
	}
	if opts.DB == nil || h.Profile == nil || h.Context == nil || h.Destinations == nil || req.Mode != agent.ModeResearch {
		return fail(errors.New("research brief requires admitted Research context"))
	}
	instance, err := opts.DB.GetGraphInstance(ctx, req.InstanceID)
	if err != nil {
		return fail(err)
	}
	pin, err := opts.DB.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return fail(err)
	}
	node, ok := h.Profile.Node(req.Node)
	if !ok || node.Action != ResearchBriefAction || node.Attrs["lf_source"] == "" || pin.Digest != h.Profile.Digest || pin.Mode != "research" {
		return fail(errors.New("research brief profile binding mismatch"))
	}
	attempt, err := opts.DB.GetGraphNodeAttempt(ctx, req.NodeAttemptID)
	if err != nil {
		return fail(err)
	}
	if attempt.InstanceID != instance.ID || attempt.Node != req.Node || attempt.Status != store.GraphRunning || instance.CurrentNode != req.Node {
		return fail(errors.New("research brief attempt binding mismatch"))
	}
	source, err := latestResearchAgentAttempt(ctx, opts, h.Profile, instance.ID, node.Attrs["lf_source"])
	if err != nil {
		return fail(err)
	}
	if source.ID == 0 {
		return fail(errors.New("research brief has no retained source candidate"))
	}
	scope, err := h.Context(ctx, source)
	if err != nil {
		return fail(err)
	}
	candidate, err := LoadResearchNodeOutput(ctx, opts.DB, source.ID, scope)
	if err != nil {
		return fail(err)
	}
	root, session, err := h.Destinations()
	if err != nil {
		return fail(err)
	}
	if candidate.Result.Payload.Research == nil {
		return fail(errors.New("research brief requires Research payload"))
	}
	if err := ValidateResearchArtifactDeclarations(root, session, candidate.Result.Artifacts); err != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if err := WriteResearchBriefs(root, session, candidate.Result); err != nil {
		return fail(err)
	}
	if err := ValidateResearchArtifacts(root, session, candidate.Result.Artifacts); err != nil {
		return fail(err)
	}
	generated := []agentresult.Artifact{{ID: "brief", Kind: "brief", Path: path.Join(session, ResearchBriefName)}, {ID: "technical", Kind: "technical", Path: path.Join(session, ResearchTechnicalName)}}
	if err := ValidateResearchArtifacts(root, session, generated); err != nil {
		return fail(err)
	}
	digest, err := researchCandidateDigest(candidate.Result)
	if err != nil {
		return fail(err)
	}
	raw, err := json.Marshal(researchGeneration{Action: ResearchBriefAction, AttemptID: attempt.ID, SourceAttemptID: source.ID, SourceRunID: source.RunID, DefinitionDigest: pin.Digest, CandidateDigest: digest, Session: session, Artifacts: generated})
	if err != nil {
		return fail(err)
	}
	return ActivityResult{Outcome: "success", Kind: store.GraphOutputJSON, Output: raw}, nil
}

func researchCandidateDigest(candidate agentresult.Result) (string, error) {
	raw, err := json.Marshal(candidate)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// requireResearchBrief checks retained generation identity and current bytes.
// Existing files, another candidate's success, and an earlier success followed
// by a failed replay cannot authorize publication or its stage handoff.
func requireResearchBrief(ctx context.Context, db *store.DB, source store.GraphNodeAttempt, candidate agentresult.Result, scope agentresult.Context) error {
	return requireResearchBriefContinuation(ctx, db, source, candidate, scope, 0)
}

func requireResearchBriefContinuation(ctx context.Context, db *store.DB, source store.GraphNodeAttempt, candidate agentresult.Result, scope agentresult.Context, continuing int64) error {
	stored, err := db.GetGraphNodeAttempt(ctx, source.ID)
	if err != nil {
		return err
	}
	if stored.InstanceID != source.InstanceID || stored.Node != source.Node || stored.RunID != source.RunID || stored.Status != store.GraphSucceeded {
		return errors.New("research brief source identity mismatch")
	}
	retained, err := LoadResearchNodeOutput(ctx, db, source.ID, scope)
	if err != nil {
		return err
	}
	digest, err := researchCandidateDigest(candidate)
	if err != nil {
		return err
	}
	retainedDigest, err := researchCandidateDigest(retained.Result)
	if err != nil {
		return err
	}
	if digest != retainedDigest {
		return errors.New("research brief candidate differs from retained source")
	}
	instance, err := db.GetGraphInstance(ctx, source.InstanceID)
	if err != nil {
		return err
	}
	pin, err := db.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return err
	}
	attempts, err := db.ListGraphNodeAttempts(ctx, source.InstanceID)
	if err != nil {
		return err
	}
	var generation researchGeneration
	var generationNode string
	for _, attempt := range attempts {
		if attempt.Node == source.Node && attempt.ID > source.ID && attempt.ID != continuing {
			return errors.New("research brief source candidate is stale")
		}
		if attempt.ID <= source.ID {
			continue
		}
		if attempt.Node == generationNode {
			generation = researchGeneration{}
		}
		if attempt.Status != store.GraphSucceeded || attempt.Outcome != "success" {
			continue
		}
		outputs, err := db.ListGraphOutputsBounded(ctx, attempt.ID, agentresult.MaxResearchNodeBytes, 16)
		if err != nil {
			return err
		}
		for _, output := range outputs {
			if output.Kind != store.GraphOutputJSON {
				continue
			}
			var proof researchGeneration
			decoder := json.NewDecoder(bytes.NewReader(output.Payload))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&proof); err != nil || proof.Action != ResearchBriefAction {
				continue
			}
			if proof.AttemptID != attempt.ID || proof.DefinitionDigest != pin.Digest {
				return errors.New("research brief generation identity mismatch")
			}
			generation, generationNode = proof, attempt.Node
		}
	}
	if generation.SourceAttemptID != source.ID || generation.SourceRunID != source.RunID || generation.CandidateDigest != digest {
		return errors.New("research publication requires matching successful brief generation")
	}
	session, err := db.GetSession(ctx, instance.SessionID)
	if err != nil {
		return err
	}
	if err := ValidateResearchArtifacts(session.WorktreePath, generation.Session, candidate.Artifacts); err != nil {
		return err
	}
	if err := ValidateResearchArtifacts(session.WorktreePath, generation.Session, []agentresult.Artifact{{Path: path.Join(generation.Session, ResearchBriefName)}, {Path: path.Join(generation.Session, ResearchTechnicalName)}}); err != nil {
		return err
	}
	root, err := os.OpenRoot(session.WorktreePath)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, document := range []struct{ name, body string }{{ResearchBriefName, renderResearchBrief(candidate)}, {ResearchTechnicalName, renderResearchTechnical(candidate)}} {
		file, err := root.OpenFile(path.Join(generation.Session, document.name), os.O_RDONLY|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return err
		}
		actual, readErr := io.ReadAll(io.LimitReader(file, int64(len(document.body))+1))
		if err := errors.Join(readErr, file.Close()); err != nil {
			return err
		}
		if string(actual) != document.body {
			return fmt.Errorf("research brief %s differs from generated candidate", document.name)
		}
	}
	return nil
}
