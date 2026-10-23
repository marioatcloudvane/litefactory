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
	Analyses   *ConversationAnalyses
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
	case standards.Review:
		refs["id"] = x.Proposal.ProposalID
		refs["version"] = strconv.FormatInt(x.Proposal.Version, 10)
		refs["digest"] = x.Proposal.ChangeDigest
		refs["base"] = strconv.FormatInt(x.Proposal.BaseRevision, 10)
		refs["kind"] = "proposal"
	case store.StandardsApproval:
		refs["id"] = x.ProposalID
		refs["version"] = strconv.FormatInt(x.Version, 10)
		refs["digest"] = x.ChangeDigest
		refs["base"] = strconv.FormatInt(x.BaseRevision, 10)
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
		return messaging.StandardsResult{Text: "Standards: inspect categories, standards, book history, proposals and authoring sessions; start, answer, edit, continue, validate, complete or rebase authoring; approve exact proposals or exceptions and separately publish an approved proposal, each as a confirmed action; start a log analysis of one project as a confirmed action and inspect saved analyses."}, nil
	case "book.current", "book.categories", "book.standards":
		var revision store.StandardsRevision
		revision, e = a.Book.Current(ctx)
		v = revision
		if e == nil && r.Operation == "book.categories" {
			v = revision.Snapshot.Categories
		}
		if e == nil && r.Operation == "book.standards" {
			return messaging.StandardsResult{Text: standards.Render(revision.Snapshot.Categories, revision.Snapshot.Standards) + standards.FormatLoad(revision.Snapshot), References: map[string]string{"kind": "book", "revision": strconv.FormatInt(revision.ID, 10), "digest": revision.Digest}}, nil
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
	case "proposal.review":
		var n int64
		n, e = standardsNumber(args, "version")
		if e == nil {
			v, e = a.Book.Review(ctx, args["id"], n)
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
	case "analysis.list":
		text, err := a.Analyses.list(ctx)
		return messaging.StandardsResult{Text: text}, err
	case "analysis.show":
		text, err := a.Analyses.Report(ctx, args["id"])
		return messaging.StandardsResult{Text: text, References: map[string]string{"kind": "analysis", "id": args["id"]}}, err
	default:
		e = errors.New("standards: unsupported read")
	}
	if e != nil {
		return messaging.StandardsResult{}, e
	}
	return standardsResult(v)
}

// execute accepts only host-confirmed mutations; approval is one of them.
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
	if r.Operation != "author.start" && r.Operation != "exception.propose" && r.Operation != "exception.approve" && r.Operation != "exception.revoke" {
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
		var project int64
		if raw := args["repository_id"]; raw != "" {
			if project, e = standardsNumber(args, "repository_id"); e != nil {
				return messaging.StandardsResult{}, e
			}
		}
		v, e = a.Authoring.Start(ctx, standards.AuthoringStart{ID: id, Mode: mode, TargetID: args["target_id"], Intent: args["intent"], CategoryIDs: standardsCategories(args["category_ids"]), ProjectID: project})
	case "author.answer":
		v, e = a.Authoring.Answer(ctx, args["id"], version, []standards.AuthoringAnswer{{QuestionID: args["question_id"], Text: args["text"], Replace: args["replace"] == "true"}})
	case "author.edit":
		edit := standards.AuthoringEdit{}
		for name, field := range map[string]**string{"category_id": &edit.CategoryID, "rule": &edit.Rule, "description": &edit.Description, "evidence": &edit.Evidence, "scope": &edit.Scope, "check": &edit.Check} {
			if x, ok := args[name]; ok {
				*field = &x
			}
		}
		if x, ok := args["applies_to"]; ok {
			roles := standardsCategories(x)
			edit.AppliesTo = &roles
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

// Execute saves intent before work and consults the durable receipt before any
// expected-version check, so an identical admitted retry never repeats an effect.
func (a *ConversationStandards) Execute(ctx context.Context, r messaging.StandardsRequest) (messaging.StandardsResult, error) {
	if e := a.authorize(ctx, r); e != nil {
		return messaging.StandardsResult{}, e
	}
	if r.Operation == "analysis.start" && r.OperationKey != "" {
		// The analysis keeps its own record, keyed by this operation, so a
		// repeated confirmation never starts a second one.
		if e := a.capability(r.Operation); e != nil {
			return messaging.StandardsResult{}, e
		}
		return a.Analyses.begin(r)
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
	if r.Operation == "author.continue" {
		if _, e = a.DB.StartStandardsConversationGeneration(ctx, r.OperationKey, d); e != nil {
			return messaging.StandardsResult{Interrupted: true}, e
		}
		// The author can take minutes, longer than a conversation turn: it
		// works in the background and its draft is posted here.
		if a.Analyses != nil && a.Analyses.later(ctx, r.ChannelID, "standards-author:"+r.OperationKey, r.OperationKey, func(ctx context.Context) string {
			_, err := a.run(ctx, r, d)
			if err != nil {
				return "The standards author couldn't finish the draft: " + strings.TrimPrefix(err.Error(), "standards: ") + ". Nothing was changed. Say \"again\" and I'll ask it once more."
			}
			v, err := a.Authoring.Show(ctx, r.Arguments["id"], 0)
			if err != nil {
				return "The standards author has finished, but I couldn't read its draft. Ask me for it in a moment."
			}
			return authorDraftText(v)
		}) {
			return messaging.StandardsResult{Text: "The standards author is working on the next draft. It takes a few minutes; I'll post it here.", References: map[string]string{"kind": "author", "id": r.Arguments["id"]}}, nil
		}
	}
	return a.run(ctx, r, d)
}

// run executes one admitted operation and records its outcome.
func (a *ConversationStandards) run(ctx context.Context, r messaging.StandardsRequest, d string) (messaging.StandardsResult, error) {
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

// authorDraftText shows an authoring session's draft for the owner in plain
// words: the rule, what it means, the author's questions and what to do next.
func authorDraftText(v standards.AuthoringSession) string {
	var b strings.Builder
	d := v.Draft
	b.WriteString("The standards author has a draft for you:\n\n" + strings.TrimSpace(d.Rule) + "\n")
	if text := strings.TrimSpace(d.Description); text != "" {
		b.WriteString(text + "\n")
	}
	var about []string
	if c := v.NewCategory; c != nil {
		about = append(about, "new category "+c.Name)
	} else {
		for _, c := range v.Base.Categories {
			if c.ID == d.CategoryID {
				about = append(about, "category "+c.Name)
			}
		}
	}
	if len(d.AppliesTo) > 0 {
		about = append(about, "applies to "+strings.Join(d.AppliesTo, ", "))
	}
	if d.Check != "" {
		about = append(about, "check: "+strings.ToLower(standards.CheckLabel(d.Check)))
	}
	if len(about) > 0 {
		b.WriteString("(" + strings.Join(about, "; ") + ")\n")
	}
	if v.Notes != "" {
		b.WriteString("\n" + strings.TrimSpace(v.Notes) + "\n")
	}
	asked := false
	for _, q := range v.Questions {
		if q.State != "pending" {
			continue
		}
		if !asked {
			b.WriteString("\nIt asks:\n")
			asked = true
		}
		b.WriteString("- " + strings.TrimSpace(q.Text))
		if len(q.Options) > 0 {
			b.WriteString(" (" + strings.Join(q.Options, " / ") + ")")
		}
		b.WriteString("\n")
	}
	for _, p := range v.Diagnostics {
		// Pending questions are already asked above.
		if !strings.HasPrefix(p.Field, "questions.") {
			b.WriteString("\nStill open: " + p.Message + "\n")
		}
	}
	next := "Tell me what to change, or say it's good and I'll turn it into a proposal."
	if asked {
		next = "Answer here, tell me what to change, or say it's good and I'll turn it into a proposal."
	}
	return b.String() + "\n" + next
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
	for name, field := range map[string]*string{"category_id": &standard.CategoryID, "rule": &standard.Rule, "description": &standard.Description, "evidence": &standard.Evidence, "scope": &standard.Scope, "check": &standard.Check} {
		if value, ok := args[name]; ok {
			*field = value
			changed = true
		}
	}
	if value, ok := args["applies_to"]; ok {
		standard.AppliesTo = standardsCategories(value)
		changed = true
	}
	if !changed {
		return nil, errors.New("standards: clarify which proposed standard field to edit")
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
	case strings.HasPrefix(operation, "analysis."):
		unavailable = a.Analyses == nil
	}
	if unavailable {
		return errors.New("standards: requested shared service is unavailable")
	}
	return nil
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
