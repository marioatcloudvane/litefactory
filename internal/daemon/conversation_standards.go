package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/analystschedule"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/standardsexceptions"
	"github.com/cloudvane/litefactory/internal/store"
)

// ConversationStandards adapts authenticated owner requests to the same services
// used by the CLI. Discord identity never substitutes for the initialized owner.
type ConversationStandards struct {
	DB         *store.DB
	Book       *standards.Service
	Authoring  *standards.Authoring
	Exceptions *standardsexceptions.Service
	Schedules  *analystschedule.Service
	OwnerID    string
	Authorized func(context.Context, messaging.StandardsRequest) error
}

func (a *ConversationStandards) authorize(ctx context.Context, r messaging.StandardsRequest) error {
	if a.OwnerID == "" || r.OwnerID != a.OwnerID || r.Transport != "discord" || r.ChannelID == "" || r.MessageID == "" || a.Authorized == nil {
		return errors.New("standards: authorized owner conversation required")
	}
	return a.Authorized(ctx, r)
}
func standardsNumber(args map[string]string, key string) (int64, error) {
	v, e := strconv.ParseInt(args[key], 10, 64)
	if e != nil || v < 1 {
		return 0, fmt.Errorf("standards: exact positive %s required", key)
	}
	return v, nil
}
func standardsJSON(raw string, out any) error {
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(out); e != nil {
		return e
	}
	var extra any
	if e := d.Decode(&extra); !errors.Is(e, io.EOF) {
		return errors.New("standards: one JSON value required")
	}
	return nil
}
func standardsResult(v any) (messaging.StandardsResult, error) {
	b, e := json.MarshalIndent(standardsDisplay(v), "", "  ")
	if e != nil {
		return messaging.StandardsResult{}, e
	}
	refs := map[string]string{}
	switch x := v.(type) {
	case standards.AuthoringSession:
		refs["id"] = x.ID
		refs["version"] = strconv.FormatInt(x.Version, 10)
		refs["kind"] = "author"
		pending := []string{}
		for _, q := range x.Questions {
			if q.State == "pending" {
				pending = append(pending, q.ID)
			}
		}
		if len(pending) == 1 {
			refs["question_id"] = pending[0]
		}
		for i, id := range pending {
			refs["question_id_"+strconv.Itoa(i+1)] = id
		}
	case standards.Inspection:
		refs["id"] = x.Proposal.ProposalID
		refs["version"] = strconv.FormatInt(x.Proposal.Version, 10)
		refs["digest"] = x.Proposal.ChangeDigest
		refs["base"] = strconv.FormatInt(x.Proposal.BaseRevision, 10)
		refs["kind"] = "proposal"
	case store.StandardsProposalVersion:
		refs["id"] = x.ProposalID
		refs["version"] = strconv.FormatInt(x.Version, 10)
		refs["digest"] = x.ChangeDigest
		refs["base"] = strconv.FormatInt(x.BaseRevision, 10)
		refs["kind"] = "proposal"
	case store.StandardsRevision:
		refs["revision"] = strconv.FormatInt(x.ID, 10)
		refs["digest"] = x.Digest
		refs["kind"] = "book"
	case standardsexceptions.View:
		refs["id"] = x.Proposal.ID
		refs["digest"] = x.Proposal.Digest
		refs["kind"] = "exception"
	case store.AnalystSchedule:
		refs["id"] = x.ID
		refs["version"] = strconv.FormatInt(x.Version, 10)
		refs["kind"] = "schedule"
	}
	return messaging.StandardsResult{Text: string(b), References: refs}, nil
}

// Read returns retained facts; it does not invoke an authoring provider.
func (a *ConversationStandards) Read(ctx context.Context, r messaging.StandardsRequest) (messaging.StandardsResult, error) {
	if e := a.authorize(ctx, r); e != nil {
		return messaging.StandardsResult{}, e
	}
	if e := a.capability(r.Operation); e != nil {
		return messaging.StandardsResult{}, e
	}
	args := r.Arguments
	var v any
	var e error
	switch r.Operation {
	case "discover":
		return messaging.StandardsResult{Text: "Standards: inspect categories, standards, book history, proposals and authoring sessions; start, answer, edit, continue, validate, complete or rebase authoring; review exact proposals or exceptions before explicit approval; separately confirm publication; create disabled analyst schedules, inspect activity, change, enable or disable them; inspect retained notices."}, nil
	case "book.current", "book.categories", "book.standards":
		var revision store.StandardsRevision
		revision, e = a.Book.Current(ctx)
		v = revision
		if e == nil && r.Operation == "book.categories" {
			v = revision.Snapshot.Categories
		}
		if e == nil && r.Operation == "book.standards" {
			v = revision.Snapshot.Standards
		}
	case "book.history":
		v, e = a.Book.History(ctx)
	case "book.show":
		var n int64
		n, e = standardsNumber(args, "revision")
		if e == nil {
			v, e = a.Book.Revision(ctx, n)
		}
	case "proposal.list", "proposals.list":
		v, e = a.Book.List(ctx)
	case "proposal.inspect", "proposal.show":
		var n int64
		n, e = standardsNumber(args, "version")
		if e == nil {
			v, e = a.Book.Inspect(ctx, args["id"], n)
		}
	case "author.list":
		v, e = a.Authoring.List(ctx, 25)
	case "author.show", "author.inspect":
		var n int64
		n, e = standardsNumber(args, "version")
		if e == nil {
			v, e = a.Authoring.Show(ctx, args["id"], n)
		}
	case "exception.list":
		v, e = a.Exceptions.List(ctx, 25, 0)
	case "exception.show", "exception.inspect":
		v, e = a.Exceptions.Show(ctx, args["id"])
	case "schedule.list":
		v, e = a.DB.ListAnalystSchedules(ctx, "", 25)
	case "schedule.show":
		v, e = a.DB.GetAnalystSchedule(ctx, args["id"])
	case "schedule.activity":
		var after int64
		after, e = standardsAfter(args)
		if e == nil {
			v, e = a.DB.ListAnalystOccurrences(ctx, args["id"], after, 25)
		}
	case "notice.list":
		var after int64
		after, e = standardsAfter(args)
		if e == nil {
			v, e = a.DB.ListAnalystProposalNotices(ctx, args["id"], after, 25)
		}
	default:
		e = errors.New("standards: unsupported read")
	}
	if e != nil {
		return messaging.StandardsResult{}, e
	}
	return standardsResult(v)
}

// Execute accepts only host-confirmed mutations; complete exact approval is
// enforced by messaging before the specialized approve operation is dispatched.
func (a *ConversationStandards) execute(ctx context.Context, r messaging.StandardsRequest) (messaging.StandardsResult, error) {
	if e := a.authorize(ctx, r); e != nil {
		return messaging.StandardsResult{}, e
	}
	if r.OperationKey == "" {
		return messaging.StandardsResult{}, errors.New("standards: durable operation identity required")
	}
	if e := a.capability(r.Operation); e != nil {
		return messaging.StandardsResult{}, e
	}
	args := r.Arguments
	var v any
	var e error
	var version int64
	if r.Operation != "author.start" && r.Operation != "exception.propose" && r.Operation != "exception.approve" && r.Operation != "exception.revoke" && r.Operation != "schedule.create" {
		version, e = standardsNumber(args, "version")
		if e != nil {
			return messaging.StandardsResult{}, e
		}
	}
	switch r.Operation {
	case "author.start":
		id := args["id"]
		if id == "" {
			id = "conversation-" + r.OperationKey
		}
		mode := args["mode"]
		if mode == "" {
			mode = "create"
		}
		v, e = a.Authoring.Start(ctx, standards.AuthoringStart{ID: id, Mode: mode, TargetID: args["target_id"], Intent: args["intent"], CategoryIDs: standardsCategories(args["category_ids"])})
	case "author.answer":
		v, e = a.Authoring.Answer(ctx, args["id"], version, []standards.AuthoringAnswer{{QuestionID: args["question_id"], Text: args["text"], Replace: args["replace"] == "true"}})
	case "author.edit":
		edit := standards.AuthoringEdit{}
		if x, ok := args["title"]; ok {
			edit.Title = &x
		}
		if x, ok := args["body"]; ok {
			edit.Body = &x
		}
		if x, ok := args["category_ids"]; ok {
			c := standardsCategories(x)
			edit.CategoryIDs = &c
		}
		v, e = a.Authoring.Edit(ctx, args["id"], version, edit)
	case "author.continue":
		v, e = a.Authoring.Continue(ctx, args["id"], version)
	case "author.validate":
		v, e = a.Authoring.Validate(ctx, args["id"], version)
	case "author.complete":
		v, e = a.Authoring.Complete(ctx, args["id"], version)
	case "author.rebase":
		v, e = a.Authoring.Rebase(ctx, args["id"], version)
	case "proposal.edit":
		var proposal store.StandardsProposalVersion
		proposal, e = a.Book.Proposal(ctx, args["id"], version)
		ops := proposal.Operations
		if e == nil {
			if raw := args["operations"]; raw != "" {
				e = standardsJSON(raw, &ops)
			} else {
				ops, e = standardsEditOperations(ops, args)
			}
		}
		base := proposal.BaseRevision
		if raw := args["base"]; raw != "" && e == nil {
			var parsed int64
			parsed, e = strconv.ParseInt(raw, 10, 64)
			if e == nil && parsed >= 0 {
				base = parsed
			} else {
				e = errors.New("standards: explicit nonnegative proposal base required")
			}
		}
		if e == nil {
			v, e = a.Book.Edit(ctx, args["id"], version, base, ops)
		}
	case "proposal.approve":
		v, e = a.Book.Approve(ctx, args["id"], version, args["digest"])
	case "proposal.publish":
		v, e = a.Book.Publish(ctx, args["id"], version, args["digest"])
	case "exception.propose":
		var request standardsexceptions.Request
		request, e = standardsExceptionRequest(args)
		if e == nil {
			v, e = a.Exceptions.Propose(ctx, r.OperationKey, request)
		}
	case "exception.approve":
		v, e = a.Exceptions.Approve(ctx, args["id"], args["digest"])
	case "exception.revoke":
		v, e = a.Exceptions.Revoke(ctx, args["id"], args["reason"])
	case "schedule.create", "schedule.change":
		var c analystschedule.Config
		c, e = standardsScheduleConfig(args)
		if e == nil {
			if r.Operation == "schedule.create" {
				v, e = a.Schedules.Create(ctx, r.OperationKey, "discord-owner:"+r.OwnerID, c, r.At)
			} else {
				v, e = a.Schedules.Change(ctx, args["id"], version, "discord-owner:"+r.OwnerID, c, r.At)
			}
		}
	case "schedule.enable":
		v, e = a.Schedules.Enable(ctx, args["id"], version, "discord-owner:"+r.OwnerID, r.At)
	case "schedule.disable":
		v, e = a.Schedules.Disable(ctx, args["id"], version, "discord-owner:"+r.OwnerID, r.At)
	default:
		e = errors.New("standards: unsupported mutation")
	}
	if e != nil {
		return messaging.StandardsResult{}, e
	}
	return standardsResult(v)
}
func standardsCategories(v string) []string {
	if strings.TrimSpace(v) == "" {
		return []string{}
	}
	parts := strings.Split(v, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// Review returns the full authoritative before/after data, never a model summary.
func (a *ConversationStandards) Review(ctx context.Context, r messaging.StandardsRequest) (messaging.StandardsReview, error) {
	if e := a.authorize(ctx, r); e != nil {
		return messaging.StandardsReview{}, e
	}
	if r.Operation != "proposal.review" && r.Operation != "exception.review" {
		return messaging.StandardsReview{}, errors.New("standards: exact review operation required")
	}
	if e := a.capability(r.Operation); e != nil {
		return messaging.StandardsReview{}, e
	}
	args := r.Arguments
	if r.Operation == "exception.review" {
		v, e := a.Exceptions.Show(ctx, args["id"])
		if e != nil {
			return messaging.StandardsReview{}, e
		}
		b, e := json.MarshalIndent(struct {
			Proposal standardsexceptions.Proposal `json:"proposal"`
			Standard *store.StandardsStandard     `json:"standard"`
		}{v.Proposal, v.Standard}, "", "  ")
		return messaging.StandardsReview{Kind: "exception", TargetID: v.Proposal.ID, TargetVersion: 1, BaseRevision: v.Proposal.Terms.BookRevision, ChangeDigest: v.Proposal.Digest, ResultDigest: v.Proposal.Digest, Text: string(b)}, e
	}
	n, e := standardsNumber(args, "version")
	if e != nil {
		return messaging.StandardsReview{}, e
	}
	v, e := a.Book.Review(ctx, args["id"], n)
	if e != nil {
		return messaging.StandardsReview{}, e
	}
	b, e := json.MarshalIndent(v, "", "  ")
	return messaging.StandardsReview{Kind: "proposal", TargetID: v.Proposal.ProposalID, TargetVersion: v.Proposal.Version, BaseRevision: v.Proposal.BaseRevision, ChangeDigest: v.Proposal.ChangeDigest, ResultDigest: v.Proposal.ResultDigest, Text: string(b)}, e
}

// Execute saves intent before work and consults the durable receipt before any
// expected-version check, so an identical admitted retry never repeats an effect.
func (a *ConversationStandards) Execute(ctx context.Context, r messaging.StandardsRequest) (messaging.StandardsResult, error) {
	if e := a.authorize(ctx, r); e != nil {
		return messaging.StandardsResult{}, e
	}
	if a.DB == nil || r.OperationKey == "" {
		return messaging.StandardsResult{}, errors.New("standards: durable operation storage required")
	}
	canonical := struct {
		Operation                                string
		Arguments                                map[string]string
		Transport, OwnerID, ChannelID, MessageID string
	}{r.Operation, r.Arguments, r.Transport, r.OwnerID, r.ChannelID, r.MessageID}
	raw, e := json.Marshal(canonical)
	if e != nil {
		return messaging.StandardsResult{}, e
	}
	sum := sha256.Sum256(raw)
	d := hex.EncodeToString(sum[:])
	receipt, e := a.DB.AdmitStandardsConversationOperation(ctx, store.StandardsConversationOperation{Key: r.OperationKey, RequestDigest: d, Kind: r.Operation, Transport: r.Transport, OwnerID: r.OwnerID, ChannelID: r.ChannelID, SourceID: r.MessageID, RequestJSON: raw})
	if e != nil {
		return messaging.StandardsResult{}, e
	}
	if receipt.Status == "completed" {
		var result messaging.StandardsResult
		e = json.Unmarshal(receipt.ResultJSON, &result)
		return result, e
	}
	if receipt.Status == "failed" || receipt.Status == "conflict" || receipt.Status == "interrupted" {
		return messaging.StandardsResult{Interrupted: receipt.Status == "interrupted"}, fmt.Errorf("standards: retained %s operation: %s", receipt.Status, receipt.Error)
	}
	if receipt.EffectCommitted {
		return a.recoverEffect(ctx, r, receipt)
	}
	if receipt.Status != "saved" {
		return messaging.StandardsResult{}, fmt.Errorf("standards: retained %s operation: %s", receipt.Status, receipt.Error)
	}
	if r.Operation == "proposal.approve" || r.Operation == "exception.approve" {
		version, parse := standardsNumber(r.Arguments, "version")
		if parse != nil {
			return messaging.StandardsResult{}, parse
		}
		kind := strings.TrimSuffix(r.Operation, ".approve")
		if e = a.DB.ValidateStandardsConversationApproval(ctx, kind, r.Arguments["id"], version, r.Arguments["digest"], r.OwnerID, r.ChannelID); e != nil {
			return messaging.StandardsResult{}, e
		}
	}
	if r.Operation == "author.continue" {
		if _, e = a.DB.StartStandardsConversationGeneration(ctx, r.OperationKey, d); e != nil {
			return messaging.StandardsResult{Interrupted: true}, e
		}
	}
	provenance := "discord-owner:" + r.OwnerID + ":" + r.ChannelID + ":" + r.MessageID
	ctx = store.WithStandardsProvenance(ctx, provenance)
	ctx = store.WithStandardsConversationOperation(ctx, r.OperationKey, d)
	local := *a
	if a.Book != nil {
		book := *a.Book
		book.Provenance = provenance
		local.Book = &book
	}
	if a.Authoring != nil {
		author := *a.Authoring
		author.Book = local.Book
		local.Authoring = &author
	}
	result, e := local.execute(ctx, r)
	if errors.Is(e, store.ErrStandardsOperationCommitted) {
		receipt, lookup := a.DB.GetStandardsConversationOperation(ctx, r.OperationKey)
		if lookup != nil {
			return result, lookup
		}
		return a.recoverEffect(ctx, r, receipt)
	}
	status := "completed"
	diagnostic := ""
	if e != nil {
		status = "failed"
		diagnostic = e.Error()
		if r.Operation == "author.continue" {
			status = "interrupted"
		}
		if errors.Is(e, store.ErrStandardsConflict) {
			status = "conflict"
		}
	}
	encoded, _ := json.Marshal(result)
	_, save := a.DB.FinishStandardsConversationOperation(context.WithoutCancel(ctx), r.OperationKey, d, status, encoded, diagnostic)
	return result, errors.Join(e, save)
}
func (a *ConversationStandards) recoverEffect(ctx context.Context, r messaging.StandardsRequest, o store.StandardsConversationOperation) (messaging.StandardsResult, error) {
	// The transactional effect reference is recovered independently of any chat body.
	var link struct {
		DomainKind string `json:"domain_kind"`
		ID         string `json:"id"`
		Version    int64  `json:"version"`
		Revision   int64  `json:"revision"`
	}
	if e := json.Unmarshal(o.EffectJSON, &link); e != nil {
		return messaging.StandardsResult{}, fmt.Errorf("standards: committed effect needs inspection: %w", e)
	}
	if link.DomainKind == "author-interrupted" || (r.Operation == "author.complete" && link.DomainKind != "proposal") {
		status := "failed"
		if link.DomainKind == "author-interrupted" {
			status = "interrupted"
		}
		diagnostic := "the requested operation retained diagnostics without completing its intended action; inspect the authoring session and explicitly continue"
		_, save := a.DB.FinishStandardsConversationOperation(ctx, o.Key, o.RequestDigest, status, nil, diagnostic)
		return messaging.StandardsResult{Text: "Retained standards operation " + status + ": " + diagnostic, Interrupted: true}, save
	}
	var value any
	var e error
	switch link.DomainKind {
	case "author":
		value, e = a.Authoring.Show(ctx, link.ID, link.Version)
	case "proposal":
		value, e = a.Book.Proposal(ctx, link.ID, link.Version)
	case "approval":
		value, e = a.DB.GetStandardsApproval(ctx, link.ID, link.Version)
	case "book":
		value, e = a.Book.Revision(ctx, link.Revision)
	case "exception":
		value, e = a.Exceptions.Show(ctx, link.ID)
	case "schedule":
		value, e = a.DB.GetAnalystScheduleVersion(ctx, link.ID, link.Version)
	default:
		e = errors.New("standards: committed effect reference unavailable; inspect operation before retry")
	}
	if e != nil {
		return messaging.StandardsResult{}, e
	}
	result, e := standardsResult(value)
	if e != nil {
		return result, e
	}
	raw, _ := json.Marshal(result)
	_, e = a.DB.FinishStandardsConversationOperation(ctx, o.Key, o.RequestDigest, "completed", raw, "")
	return result, e
}

// Notices projects immutable retained analyst notices without inspecting mutable
// proposal state or invoking a provider. The delivery worker owns its cursor.
func (a *ConversationStandards) Notices(ctx context.Context, r messaging.StandardsRequest) ([]messaging.StandardsNotice, error) {
	if e := a.authorize(ctx, r); e != nil {
		return nil, e
	}
	after := int64(0)
	if raw := r.Arguments["after"]; raw != "" {
		var e error
		after, e = strconv.ParseInt(raw, 10, 64)
		if e != nil || after < 0 {
			return nil, errors.New("standards: invalid notice cursor")
		}
	}
	notices, e := a.DB.ListAnalystProposalNotices(ctx, "", after, 25)
	if e != nil {
		return nil, e
	}
	out := make([]messaging.StandardsNotice, 0, len(notices))
	for _, notice := range notices {
		payload, e := json.MarshalIndent(notice, "", "  ")
		if e != nil {
			return nil, e
		}
		out = append(out, messaging.StandardsNotice{ID: notice.ID, Payload: "Retained standards analyst notice. Proposal was unapproved at origin; this notice grants no approval. Inspect current status separately.\n" + string(payload)})
	}
	return out, nil
}

// Recover consults the original receipt without rerunning committed work. A new
// delivery source can inspect the same exact action, but cannot rewrite it.
func (a *ConversationStandards) Recover(ctx context.Context, r messaging.StandardsRequest) (messaging.StandardsResult, bool, error) {
	if e := a.authorize(ctx, r); e != nil {
		return messaging.StandardsResult{}, false, e
	}
	if a.DB == nil || r.OperationKey == "" {
		return messaging.StandardsResult{}, false, nil
	}
	o, e := a.DB.GetStandardsConversationOperation(ctx, r.OperationKey)
	if errors.Is(e, store.ErrNotFound) {
		return messaging.StandardsResult{}, false, nil
	}
	if e != nil {
		return messaging.StandardsResult{}, true, e
	}
	var saved struct {
		Operation                                string
		Arguments                                map[string]string
		Transport, OwnerID, ChannelID, MessageID string
	}
	if e = json.Unmarshal(o.RequestJSON, &saved); e != nil {
		return messaging.StandardsResult{}, true, e
	}
	requestedArgs, _ := json.Marshal(r.Arguments)
	savedArgs, _ := json.Marshal(saved.Arguments)
	if saved.Operation != r.Operation || saved.OwnerID != r.OwnerID || saved.ChannelID != r.ChannelID || saved.Transport != r.Transport || string(requestedArgs) != string(savedArgs) {
		return messaging.StandardsResult{}, true, store.ErrStandardsConflict
	}
	if o.Status == "completed" {
		var result messaging.StandardsResult
		e = json.Unmarshal(o.ResultJSON, &result)
		return result, true, e
	}
	if o.Status == "failed" || o.Status == "conflict" || o.Status == "interrupted" {
		return messaging.StandardsResult{Text: fmt.Sprintf("Retained standards operation %s: %s. Inspect saved state before a new request.", o.Status, o.Error), Interrupted: true}, true, nil
	}
	if o.EffectCommitted {
		result, e := a.recoverEffect(ctx, r, o)
		return result, true, e
	}
	if o.Started || o.Status != "saved" {
		return messaging.StandardsResult{Text: fmt.Sprintf("Retained standards operation %s: generation may be interrupted; inspect the retained attempt before explicit continuation.", o.Status), Interrupted: true}, true, nil
	}
	r.MessageID = saved.MessageID
	result, e := a.Execute(ctx, r)
	if e != nil {
		retained, lookup := a.DB.GetStandardsConversationOperation(ctx, r.OperationKey)
		if lookup == nil && (retained.Status == "failed" || retained.Status == "conflict" || retained.Status == "interrupted") {
			return messaging.StandardsResult{Text: fmt.Sprintf("Retained standards operation %s: %s. Inspect saved state before a new request.", retained.Status, retained.Error), Interrupted: true}, true, nil
		}
	}
	return result, true, e
}

func standardsExceptionRequest(args map[string]string) (standardsexceptions.Request, error) {
	var request standardsexceptions.Request
	if raw := args["request"]; raw != "" {
		e := standardsJSON(raw, &request)
		return request, e
	}
	var e error
	request.BookRevision, e = standardsNumber(args, "book_revision")
	if e != nil {
		return request, e
	}
	request.RepositoryID, e = standardsNumber(args, "repository_id")
	if e != nil {
		return request, e
	}
	request.StandardID = args["standard_id"]
	request.TaskRef = args["task_ref"]
	request.Departure = args["departure"]
	request.Reason = args["reason"]
	request.Replaces = args["replaces"]
	request.ExpiresAt, e = time.Parse(time.RFC3339, args["expires_at"])
	if e != nil {
		return request, errors.New("standards: explicit absolute exception expiry required")
	}
	return request, nil
}
func standardsScheduleConfig(args map[string]string) (analystschedule.Config, error) {
	var c analystschedule.Config
	if raw := args["config"]; raw != "" {
		e := standardsJSON(raw, &c)
		return c, e
	}
	c.Name = args["name"]
	c.Kind = args["kind"]
	if c.Name == "" || c.Kind == "" {
		return c, errors.New("standards: clarify schedule name and analyst kind")
	}
	var e error
	c.RepositoryID, e = standardsNumber(args, "repository_id")
	if e != nil {
		return c, e
	}
	c.IntervalSeconds, e = standardsNumber(args, "interval_seconds")
	if e != nil {
		return c, e
	}
	limit, e := standardsNumber(args, "run_limit")
	if e != nil || limit > 5 {
		return c, errors.New("standards: explicit finite run limit between one and five required")
	}
	c.RunLimit = int(limit)
	c.TaskRefs = standardsCategories(args["task_refs"])
	c.Modes = standardsCategories(args["modes"])
	c.Paths = standardsCategories(args["paths"])
	c.Base = args["base"]
	c.Ref = args["ref"]
	if raw := args["after"]; raw != "" {
		c.After, e = time.Parse(time.RFC3339, raw)
		if e != nil {
			return c, e
		}
		c.After = c.After.UTC()
	}
	for _, issue := range standardsCategories(args["issues"]) {
		n, e := strconv.Atoi(issue)
		if e != nil || n < 1 {
			return c, errors.New("standards: positive issue numbers required")
		}
		c.Issues = append(c.Issues, n)
	}
	return c, nil
}

func standardsEditOperations(ops []standards.Operation, args map[string]string) ([]standards.Operation, error) {
	selected := -1
	for i, op := range ops {
		if op.Standard != nil && (args["target_id"] == "" || op.Standard.ID == args["target_id"]) {
			if selected >= 0 {
				return nil, errors.New("standards: clarify which proposed standard to edit")
			}
			selected = i
		}
	}
	if selected < 0 {
		return nil, errors.New("standards: exact proposed standard target required")
	}
	standard := *ops[selected].Standard
	changed := false
	if value, ok := args["title"]; ok {
		standard.Title = value
		changed = true
	}
	if value, ok := args["body"]; ok {
		standard.Body = value
		changed = true
	}
	if value, ok := args["category_ids"]; ok {
		standard.CategoryIDs = standardsCategories(value)
		changed = true
	}
	if !changed {
		return nil, errors.New("standards: clarify proposed title, body or category edit")
	}
	out := append([]standards.Operation(nil), ops...)
	out[selected].Standard = &standard
	return out, nil
}

func (a *ConversationStandards) capability(operation string) error {
	unavailable := false
	switch {
	case strings.HasPrefix(operation, "book."), strings.HasPrefix(operation, "proposal."), operation == "proposals.list":
		unavailable = a.Book == nil
	case strings.HasPrefix(operation, "author."):
		unavailable = a.Authoring == nil || a.Authoring.Book == nil
	case strings.HasPrefix(operation, "exception."):
		unavailable = a.Exceptions == nil
	case strings.HasPrefix(operation, "schedule."):
		unavailable = a.DB == nil || a.Schedules == nil
	case operation == "notice.list":
		unavailable = a.DB == nil
	}
	if unavailable {
		return errors.New("standards: requested shared service is unavailable")
	}
	return nil
}

func standardsAfter(args map[string]string) (int64, error) {
	if args["after"] == "" {
		return 0, nil
	}
	v, e := strconv.ParseInt(args["after"], 10, 64)
	if e != nil || v < 0 {
		return 0, errors.New("standards: nonnegative inspection cursor required")
	}
	return v, nil
}
func standardsDisplay(v any) any {
	switch session := v.(type) {
	case standards.AuthoringSession:
		return struct {
			ID           string                          `json:"id"`
			Version      int64                           `json:"version"`
			Mode         string                          `json:"mode"`
			TargetID     string                          `json:"target_id"`
			Status       string                          `json:"status"`
			Intent       string                          `json:"intent"`
			BaseRevision int64                           `json:"base_revision"`
			BaseDigest   string                          `json:"base_digest"`
			Draft        standards.Standard              `json:"draft"`
			Questions    []standards.AuthoringQuestion   `json:"questions"`
			Diagnostics  []standards.Problem             `json:"diagnostics"`
			Notes        string                          `json:"notes,omitempty"`
			Proposal     *store.StandardsProposalVersion `json:"proposal,omitempty"`
		}{session.ID, session.Version, session.Mode, session.TargetID, session.Status, session.Intent, session.BaseRevision, session.BaseDigest, session.Draft, session.Questions, session.Diagnostics, session.Notes, session.Proposal}
	case []standards.AuthoringSession:
		out := make([]any, 0, len(session))
		for _, item := range session {
			out = append(out, standardsDisplay(item))
		}
		return out
	default:
		return v
	}
}
