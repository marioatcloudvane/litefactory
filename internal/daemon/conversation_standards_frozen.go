package daemon

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/loganalysis"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

// frozenStandards retains the state used to select a target, separately from
// executable arguments. An action cannot silently adopt a newer session or book.
type frozenStandards struct {
	Operation        string `json:"operation"`
	BookHead         int64  `json:"book_head"`
	TargetDigest     string `json:"target_digest"`
	RepositoryDigest string `json:"repository_digest,omitempty"`
}

type frozenStandardsContextKey struct{}
type frozenStandardsAuthority struct {
	Principal    owneraction.Principal
	Actor        string
	OperationKey string
}

func standardsFrozen(ctx context.Context) (frozenStandardsAuthority, bool) {
	v, ok := ctx.Value(frozenStandardsContextKey{}).(frozenStandardsAuthority)
	return v, ok
}

func frozenDigest(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (a *ConversationStandards) frozenAuthority(ctx context.Context, p owneraction.Principal) error {
	if a.DB == nil || a.Book == nil || p.OwnerID != strconv.FormatInt(a.Book.OwnerID, 10) || a.Book.OwnerID < 1 {
		return errors.New("standards: initialized owner authority required")
	}
	switch p.Transport {
	case "discord":
		if p.DiscordOwnerID != a.OwnerID || p.ChannelID == "" || a.Authorized == nil {
			return errors.New("standards: configured Discord owner pairing required")
		}
		return a.Authorized(ctx, messaging.StandardsRequest{Transport: "discord", OwnerID: p.DiscordOwnerID, ChannelID: p.ChannelID})
	case "mcp":
		if p.ClientID == "" || p.GrantID == "" || p.OwnerGeneration < 1 {
			return errors.New("standards: authenticated action grant required")
		}
		return nil
	default:
		return errors.New("standards: unsupported owner transport")
	}
}

// ResolveFrozen fixes implicit author/proposal selections before owner review.
func (a *ConversationStandards) ResolveFrozen(ctx context.Context, p owneraction.Principal, r messaging.StandardsRequest) (owneraction.Facts, error) {
	var facts owneraction.Facts
	if err := a.frozenAuthority(ctx, p); err != nil {
		return facts, err
	}
	if err := a.capability(r.Operation); err != nil {
		return facts, err
	}
	args := maps.Clone(r.Arguments)
	if args == nil {
		args = map[string]string{}
	}
	head, err := a.DB.StandardsHead(ctx)
	if err != nil {
		return facts, err
	}
	fixed := frozenStandards{Operation: r.Operation, BookHead: head}
	kind, action, ok := strings.Cut(r.Operation, ".")
	if !ok {
		return facts, errors.New("standards: exact operation required")
	}
	facts.Invocation = owneraction.Invocation{Command: "standards-" + kind, Arguments: args}
	args["action"] = action
	facts.Estimate = "unknown"
	facts.TargetKind = "standards-" + kind
	switch kind {
	case "author":
		if action == "start" {
			if args["id"] == "" {
				args["id"] = "author-" + rand.Text()
				if p.Transport == "discord" {
					args["id"] = "conversation-discord:" + p.ChannelID + ":owner-action:" + rand.Text()
				}
			}
			if _, e := a.Authoring.Show(ctx, args["id"], 0); !errors.Is(e, store.ErrNotFound) {
				if e != nil {
					return facts, e
				}
				return facts, store.ErrStandardsConflict
			}
			fixed.TargetDigest = "absent"
			facts.TargetID = args["id"]
			facts.TargetRevision = fmt.Sprintf("new; book revision %d", head)
		} else {
			args["id"] = strings.TrimPrefix(args["id"], "authoring-")
			if args["id"] == "" {
				args["id"] = a.conversationSession(ctx, p.ChannelID)
			}
			if args["id"] == "" {
				return facts, errors.New("standards: choose a saved authoring session")
			}
			latest, e := a.Authoring.Show(ctx, args["id"], 0)
			if e != nil {
				return facts, e
			}
			if supplied := args["version"]; supplied != "" && supplied != strconv.FormatInt(latest.Version, 10) {
				return facts, store.ErrStandardsConflict
			}
			args["version"] = strconv.FormatInt(latest.Version, 10)
			args["base"] = strconv.FormatInt(latest.BaseRevision, 10)
			if action == "rebase" {
				args["base"] = strconv.FormatInt(head, 10)
			}
			fixed.TargetDigest = frozenDigest(struct {
				Version int64
				Status  string
				State   standards.AuthoringState
			}{latest.Version, latest.Status, latest.AuthoringState})
			facts.TargetID = latest.ID
			facts.TargetRevision = fmt.Sprintf("version %d; base %d; book revision %d", latest.Version, latest.BaseRevision, head)
			if latest.Project != nil {
				facts.RepositoryID = latest.Project.ID
			}
		}
	case "proposal":
		proposal, e := a.Book.Proposal(ctx, args["id"], 0)
		if e != nil {
			return facts, e
		}
		if supplied := args["version"]; supplied != "" && supplied != strconv.FormatInt(proposal.Version, 10) {
			return facts, store.ErrStandardsConflict
		}
		if supplied := args["digest"]; supplied != "" && supplied != proposal.ChangeDigest {
			return facts, store.ErrStandardsConflict
		}
		args["version"] = strconv.FormatInt(proposal.Version, 10)
		args["digest"] = proposal.ChangeDigest
		if args["base"] == "" {
			args["base"] = strconv.FormatInt(proposal.BaseRevision, 10)
		}
		fixed.TargetDigest = frozenDigest(proposal)
		facts.TargetID = proposal.ProposalID
		facts.TargetRevision = fmt.Sprintf("version %d; digest %s; base %d", proposal.Version, proposal.ChangeDigest, proposal.BaseRevision)
	case "exception":
		if action == "propose" {
			request, e := standardsExceptionRequest(args)
			if e != nil {
				return facts, e
			}
			facts.RepositoryID = request.RepositoryID
			facts.TargetID = request.StandardID
			facts.TargetRevision = fmt.Sprintf("book revision %d; terms %s", request.BookRevision, frozenDigest(request))
			fixed.TargetDigest = frozenDigest(request)
		} else {
			record, e := a.DB.GetStandardsException(ctx, args["id"])
			if e != nil {
				return facts, e
			}
			if supplied := args["digest"]; supplied != "" && supplied != record.Proposal.Digest {
				return facts, store.ErrStandardsConflict
			}
			args["digest"] = record.Proposal.Digest
			fixed.TargetDigest = frozenDigest(record)
			facts.RepositoryID = record.Proposal.RepositoryID
			facts.TargetID = record.Proposal.ID
			facts.TargetRevision = fmt.Sprintf("terms %s; book revision %d", record.Proposal.Digest, record.Proposal.BookRevision)
		}
	case "analysis":
		if action != "start" {
			return facts, errors.New("standards: unsupported analysis mutation")
		}
		if a.Analyses.Start == nil {
			return facts, errors.New("standards: analysis execution unavailable")
		}
		repository, e := standardsNumber(args, "repository_id")
		if e != nil {
			return facts, e
		}
		after, e := time.Parse(time.RFC3339, args["after"])
		if e != nil {
			return facts, e
		}
		before, e := time.Parse(time.RFC3339, args["before"])
		if e != nil {
			return facts, e
		}
		if _, e = loganalysis.NormalizeRequest(loganalysis.Request{RepositoryIDs: []int64{repository}, After: after.UTC(), Before: before.UTC()}); e != nil {
			return facts, e
		}
		facts.TargetID = args["repository_id"]
		facts.TargetRevision = args["after"] + " / " + args["before"]
		fixed.TargetDigest = frozenDigest(args)
	default:
		return facts, errors.New("standards: unsupported target kind")
	}
	if raw := args["repository_id"]; raw != "" {
		facts.RepositoryID, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || facts.RepositoryID < 1 {
			return facts, errors.New("standards: registered repository required")
		}
	}
	if facts.RepositoryID != 0 {
		repo, e := a.DB.GetRepo(ctx, facts.RepositoryID)
		if e != nil {
			return facts, e
		}
		facts.RepositoryRef = repo.Forge + ":" + repo.Owner + "/" + repo.Name
		fixed.RepositoryDigest = frozenDigest(struct {
			ID          int64
			Forge       string
			Owner, Name string
		}{repo.ID, repo.Forge, repo.Owner, repo.Name})
	}
	facts.Summary = r.Operation + " " + facts.TargetID
	raw, err := json.Marshal(fixed)
	facts.Payload = string(raw)
	return facts, err
}

// RecheckFrozen refuses any change to the reviewed selection and book head.
func (a *ConversationStandards) RecheckFrozen(ctx context.Context, p owneraction.Principal, r messaging.StandardsRequest, facts owneraction.Facts) error {
	if err := a.frozenAuthority(ctx, p); err != nil {
		return err
	}
	var fixed frozenStandards
	if err := json.Unmarshal([]byte(facts.Payload), &fixed); err != nil {
		return err
	}
	if fixed.Operation != r.Operation {
		return store.ErrStandardsConflict
	}
	r.Arguments = maps.Clone(facts.Invocation.Arguments)
	// A generated create ID must remain absent; no re-resolution is allowed.
	fresh, err := a.ResolveFrozen(ctx, p, r)
	if err != nil {
		return err
	}
	if fresh.Payload != facts.Payload || !maps.Equal(fresh.Invocation.Arguments, facts.Invocation.Arguments) || fresh.TargetID != facts.TargetID || fresh.RepositoryID != facts.RepositoryID {
		return store.ErrStandardsConflict
	}
	return nil
}

func (a *ConversationStandards) frozenRequest(p owneraction.Principal, r messaging.StandardsRequest, facts owneraction.Facts) messaging.StandardsRequest {
	r.Arguments = maps.Clone(facts.Invocation.Arguments)
	r.Transport = p.Transport
	r.OwnerID = p.OwnerID
	r.ChannelID = p.ChannelID
	// This is a durable action source identity, not a Discord message identity.
	r.MessageID = r.OperationKey
	return r
}

func (a *ConversationStandards) frozenExecutionContext(ctx context.Context, p owneraction.Principal, r messaging.StandardsRequest, facts owneraction.Facts) (context.Context, error) {
	if err := a.frozenAuthority(ctx, p); err != nil {
		return ctx, err
	}
	parent, _, _ := strings.Cut(r.OperationKey, "/")
	valid, err := a.DB.OwnerActionProvenanceValid(ctx, parent, parent)
	if err != nil {
		return ctx, err
	}
	if !valid {
		return ctx, errors.New("standards: admitted owner action required")
	}
	var fixed frozenStandards
	if err = json.Unmarshal([]byte(facts.Payload), &fixed); err != nil {
		return ctx, err
	}
	ctx = context.WithValue(ctx, frozenStandardsContextKey{}, frozenStandardsAuthority{Principal: p, Actor: parent, OperationKey: parent})
	ctx = store.WithStandardsExpectedHead(ctx, fixed.BookHead)
	ctx = store.WithStandardsOwnerActionTime(ctx, r.At)
	ctx = store.WithStandardsProvenance(ctx, parent)
	return ctx, nil
}

// ExecuteFrozen uses the existing operation journal and domain handlers with
// the selection already fixed. It does not pass through latest-draft selection.
func (a *ConversationStandards) ExecuteFrozen(ctx context.Context, p owneraction.Principal, r messaging.StandardsRequest, facts owneraction.Facts) (messaging.StandardsResult, error) {
	r = a.frozenRequest(p, r, facts)
	ctx, err := a.frozenExecutionContext(ctx, p, r, facts)
	if err != nil {
		return messaging.StandardsResult{}, err
	}
	if saved, found, e := a.RecoverFrozen(ctx, p, r, facts); found || e != nil {
		return saved, e
	}
	if err = a.RecheckFrozen(ctx, p, r, facts); err != nil {
		return messaging.StandardsResult{}, err
	}
	if r.Operation == "author.publish" {
		return a.publishFrozen(ctx, r)
	}
	if r.Operation == "analysis.start" {
		repository, err := standardsNumber(r.Arguments, "repository_id")
		if err != nil {
			return messaging.StandardsResult{Status: "failed"}, err
		}
		after, err := time.Parse(time.RFC3339, r.Arguments["after"])
		if err != nil {
			return messaging.StandardsResult{Status: "failed"}, err
		}
		before, err := time.Parse(time.RFC3339, r.Arguments["before"])
		if err != nil {
			return messaging.StandardsResult{Status: "failed"}, err
		}
		analysis, err := a.Analyses.Start(ctx, loganalysis.Request{RepositoryIDs: []int64{repository}, After: after.UTC(), Before: before.UTC(), StartKey: r.OperationKey}, r.Arguments["dry_run"] == "true")
		if analysis.ID == "" {
			return messaging.StandardsResult{Status: "failed"}, err
		}
		result, read := a.frozenAnalysisResult(ctx, analysis)
		return result, errors.Join(err, read)
	}
	result, d, done, err := a.admit(ctx, r)
	if done || err != nil {
		return result, err
	}
	result, err = a.run(ctx, r, d)
	if err != nil {
		if errors.Is(err, store.ErrStandardsConflict) && result.Text == "" {
			result.Status = "failed"
		}
		return result, err
	}
	if r.Operation != "author.start" && r.Operation != "author.answer" && r.Operation != "author.revise" {
		return result, nil
	}
	if a.Authoring == nil || a.Authoring.Runner == nil {
		return result, nil
	}
	// These existing authoring commands include producing the next draft. The
	// derived generation uses the exact version just saved and its own receipt.
	next := r
	next.Operation = "author.continue"
	next.OperationKey += "/draft"
	next.Arguments = map[string]string{"id": result.References["id"], "version": result.References["version"]}
	continued, digest, done, err := a.admit(ctx, next)
	if done || err != nil {
		return continued, err
	}
	return a.run(ctx, next, digest)
}

// RecoverFrozen reads existing receipts; uncertain effects are never repeated.
func (a *ConversationStandards) RecoverFrozen(ctx context.Context, p owneraction.Principal, r messaging.StandardsRequest, facts owneraction.Facts) (messaging.StandardsResult, bool, error) {
	r = a.frozenRequest(p, r, facts)
	if err := a.frozenAuthority(ctx, p); err != nil {
		return messaging.StandardsResult{}, false, err
	}
	if r.Operation == "analysis.start" {
		analysis, err := a.DB.LogAnalysisByStartKey(ctx, r.OperationKey)
		if errors.Is(err, store.ErrNotFound) {
			return messaging.StandardsResult{}, false, nil
		}
		if err != nil {
			return messaging.StandardsResult{}, false, err
		}
		result, err := a.frozenAnalysisResult(ctx, analysis)
		return result, true, err
	}
	if r.Operation == "author.publish" {
		step := r
		step.Operation = "proposal.publish"
		step.OperationKey += "/publish"
		o, e := a.DB.GetStandardsConversationOperation(ctx, step.OperationKey)
		if e == nil {
			if o.Status == "completed" {
				var result messaging.StandardsResult
				e = json.Unmarshal(o.ResultJSON, &result)
				return result, true, e
			}
			if o.EffectCommitted {
				result, e := a.recoverEffect(ctx, step, o)
				return result, true, e
			}
			return messaging.StandardsResult{Text: "Publication was interrupted; inspect the retained substep receipts before a fresh proposal.", Interrupted: true}, true, nil
		}
		if !errors.Is(e, store.ErrNotFound) {
			return messaging.StandardsResult{}, false, e
		}
		for _, suffix := range []string{"/approve", "/complete"} {
			if _, e = a.DB.GetStandardsConversationOperation(ctx, r.OperationKey+suffix); e == nil {
				return messaging.StandardsResult{Text: "Publication completed only some steps; inspect retained state before a fresh proposal.", Interrupted: true}, true, nil
			} else if !errors.Is(e, store.ErrNotFound) {
				return messaging.StandardsResult{}, false, e
			}
		}
		return messaging.StandardsResult{}, false, nil
	}
	if (r.Operation == "author.start" || r.Operation == "author.answer" || r.Operation == "author.revise") && a.Authoring != nil && a.Authoring.Runner != nil {
		step := r
		step.Operation = "author.continue"
		step.OperationKey += "/draft"
		o, e := a.DB.GetStandardsConversationOperation(ctx, step.OperationKey)
		if e == nil {
			if o.Status == "completed" {
				var result messaging.StandardsResult
				e = json.Unmarshal(o.ResultJSON, &result)
				return result, true, e
			}
			if o.EffectCommitted {
				result, e := a.recoverEffect(ctx, step, o)
				return result, true, e
			}
			return messaging.StandardsResult{Text: "The author was interrupted; inspect the retained draft before continuing.", Interrupted: true}, true, nil
		}
		if !errors.Is(e, store.ErrNotFound) {
			return messaging.StandardsResult{}, false, e
		}
		if _, e = a.DB.GetStandardsConversationOperation(ctx, r.OperationKey); e == nil {
			return messaging.StandardsResult{Text: "The input was saved but the next draft was interrupted; inspect the retained authoring session.", Interrupted: true}, true, nil
		} else if !errors.Is(e, store.ErrNotFound) {
			return messaging.StandardsResult{}, false, e
		}
	}
	o, err := a.DB.GetStandardsConversationOperation(ctx, r.OperationKey)
	if errors.Is(err, store.ErrNotFound) {
		return messaging.StandardsResult{}, false, nil
	}
	if err != nil {
		return messaging.StandardsResult{}, false, err
	}
	var saved struct {
		Operation                                string
		Arguments                                map[string]string
		Transport, OwnerID, ChannelID, MessageID string
	}
	if err = json.Unmarshal(o.RequestJSON, &saved); err != nil {
		return messaging.StandardsResult{}, true, err
	}
	if saved.Operation != r.Operation || saved.Transport != r.Transport || saved.OwnerID != r.OwnerID || saved.ChannelID != r.ChannelID || !maps.Equal(saved.Arguments, r.Arguments) {
		return messaging.StandardsResult{}, true, store.ErrStandardsConflict
	}
	if o.Status == "completed" {
		var result messaging.StandardsResult
		err = json.Unmarshal(o.ResultJSON, &result)
		return result, true, err
	}
	if o.EffectCommitted {
		result, err := a.recoverEffect(ctx, r, o)
		return result, true, err
	}
	if o.Status == "failed" || o.Status == "conflict" {
		return messaging.StandardsResult{Text: o.Error, Status: "failed"}, true, nil
	}
	return messaging.StandardsResult{Text: "The admitted operation was interrupted; inspect retained state before a fresh proposal.", Interrupted: true}, true, nil
}

func (a *ConversationStandards) publishFrozen(ctx context.Context, r messaging.StandardsRequest) (messaging.StandardsResult, error) {
	version, err := standardsNumber(r.Arguments, "version")
	if err != nil {
		return messaging.StandardsResult{}, err
	}
	v, err := a.Authoring.Show(ctx, r.Arguments["id"], version)
	if err != nil {
		return messaging.StandardsResult{}, err
	}
	var proposal store.StandardsProposalVersion
	if v.Status == "completed" && v.Proposal != nil {
		proposal = *v.Proposal
	} else {
		if !v.Ready() {
			return messaging.StandardsResult{}, errors.New("standards: reviewed draft is not ready for publication")
		}
		if _, err = a.once(ctx, r, "author.complete", "complete", map[string]string{"id": v.ID, "version": strconv.FormatInt(version, 10)}); err != nil {
			return messaging.StandardsResult{}, err
		}
		proposal, err = a.DB.GetStandardsAuthoringCompletion(ctx, v.ID)
		if err != nil {
			return messaging.StandardsResult{Interrupted: true}, err
		}
	}
	args := map[string]string{"id": proposal.ProposalID, "version": strconv.FormatInt(proposal.Version, 10), "digest": proposal.ChangeDigest}
	if _, err = a.once(ctx, r, "proposal.approve", "approve", args); err != nil {
		return messaging.StandardsResult{Text: "Draft completion is retained; approval did not complete.", Interrupted: true}, err
	}
	result, err := a.once(ctx, r, "proposal.publish", "publish", args)
	if err != nil {
		result.Interrupted = true
	}
	return result, err
}

func (a *ConversationStandards) frozenAnalysisResult(ctx context.Context, analysis store.LogAnalysis) (messaging.StandardsResult, error) {
	result := messaging.StandardsResult{Status: "uncertain", References: map[string]string{"kind": "analysis", "id": analysis.ID}}
	switch analysis.State {
	case "completed":
		result.Status = "succeeded"
	case "failed":
		result.Status = "failed"
	case "interrupted":
		result.Interrupted = true
	}
	if a.Analyses.Report != nil {
		text, err := a.Analyses.Report(ctx, analysis.ID)
		result.Text = text
		return result, err
	}
	result.Text = "Analysis " + analysis.ID + ": " + analysis.State
	return result, nil
}
