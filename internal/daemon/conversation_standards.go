package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
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
		answers := []standards.AuthoringAnswer{{QuestionID: args["question_id"], Text: args["text"], Replace: args["replace"] == "true"}}
		if args["question_id"] == "" {
			// One reply such as "1a, 2b" answers all open questions; the
			// author reads which part answers which.
			var session standards.AuthoringSession
			session, e = a.Authoring.Show(ctx, args["id"], version)
			answers = answers[:0]
			for _, q := range session.Questions {
				if q.State == "pending" {
					answers = append(answers, standards.AuthoringAnswer{QuestionID: q.ID, Text: args["text"]})
				}
			}
		}
		if e == nil {
			v, e = a.Authoring.Answer(ctx, args["id"], version, answers)
		}
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
	switch r.Operation {
	case "author.answer", "author.continue":
		// The owner's answers and "again" go to the newest draft.
		if r.Arguments["version"] == "" && a.Authoring != nil {
			v, e := a.Authoring.Show(ctx, r.Arguments["id"], 0)
			if e != nil {
				return messaging.StandardsResult{}, e
			}
			r.Arguments = maps.Clone(r.Arguments)
			r.Arguments["version"] = strconv.FormatInt(v.Version, 10)
		}
		if a.Analyses != nil && r.Operation == "author.answer" && a.Analyses.queueAnswer(r) {
			return messaging.StandardsResult{Text: "Got it. The author is still working on the current draft; I'll pass your answers on when it's done and post the next draft here."}, nil
		}
		if a.Analyses != nil && r.Operation == "author.continue" && a.Analyses.authorRunning(r.Arguments["id"]) {
			return messaging.StandardsResult{Text: "The author is already working on the next draft; I'll post it here."}, nil
		}
	}
	result, d, done, e := a.admit(ctx, r)
	if done || e != nil {
		return result, e
	}
	if r.Operation == "author.continue" {
		// The author can take minutes, longer than a conversation turn: it
		// works in the background and its draft is posted here.
		if a.draftLater(ctx, r, d, r.OperationKey) {
			return messaging.StandardsResult{Text: "The standards author is working on the next draft. It takes a few minutes; I'll post it here.", References: map[string]string{"kind": "author", "id": r.Arguments["id"]}}, nil
		}
		return a.run(ctx, r, d)
	}
	result, e = a.run(ctx, r, d)
	if e != nil || (r.Operation != "author.start" && r.Operation != "author.answer") {
		return result, e
	}
	// Starting a session and answering its questions both lead straight to
	// the author's next draft; neither needs a further confirmation.
	id := result.References["id"]
	if a.nextDraft(ctx, r, id, result.References["version"], r.OperationKey) {
		if r.Operation == "author.start" {
			result.Text = "On it. The standards author is working on a first draft; it takes a few minutes and I'll post it here."
		} else {
			result.Text = "Thanks, I've passed that to the author. It's working on the next draft; I'll post it here."
		}
		return result, nil
	}
	if r.Operation == "author.start" {
		result.Text = "I've set up the standard, but the author can't start right now. Say \"again\" and I'll ask it for a first draft."
	} else {
		result.Text = "I've saved your answers, but the author can't start right now. Say \"again\" and I'll ask it for the next draft."
	}
	return result, nil
}

// admit records r durably before its effect. done reports a retry whose
// retained outcome is the result.
func (a *ConversationStandards) admit(ctx context.Context, r messaging.StandardsRequest) (messaging.StandardsResult, string, bool, error) {
	canonical := struct {
		Operation                                string
		Arguments                                map[string]string
		Transport, OwnerID, ChannelID, MessageID string
	}{r.Operation, r.Arguments, r.Transport, r.OwnerID, r.ChannelID, r.MessageID}
	raw, e := json.Marshal(canonical)
	if e != nil {
		return messaging.StandardsResult{}, "", true, e
	}
	sum := sha256.Sum256(raw)
	d := hex.EncodeToString(sum[:])
	receipt, e := a.DB.AdmitStandardsConversationOperation(ctx, store.StandardsConversationOperation{Key: r.OperationKey, RequestDigest: d, Kind: r.Operation, Transport: r.Transport, OwnerID: r.OwnerID, ChannelID: r.ChannelID, SourceID: r.MessageID, RequestJSON: raw})
	if e != nil {
		return messaging.StandardsResult{}, d, true, e
	}
	if receipt.Status == "completed" {
		var result messaging.StandardsResult
		e = json.Unmarshal(receipt.ResultJSON, &result)
		return result, d, true, e
	}
	if receipt.Status == "failed" || receipt.Status == "conflict" || receipt.Status == "interrupted" {
		return messaging.StandardsResult{Interrupted: receipt.Status == "interrupted"}, d, true, fmt.Errorf("standards: retained %s operation: %s", receipt.Status, receipt.Error)
	}
	if receipt.EffectCommitted {
		result, e := a.recoverEffect(ctx, r, receipt)
		return result, d, true, e
	}
	if receipt.Status != "saved" {
		return messaging.StandardsResult{}, d, true, fmt.Errorf("standards: retained %s operation: %s", receipt.Status, receipt.Error)
	}
	if r.Operation == "author.continue" {
		if _, e = a.DB.StartStandardsConversationGeneration(ctx, r.OperationKey, d); e != nil {
			return messaging.StandardsResult{Interrupted: true}, d, true, e
		}
	}
	return messaging.StandardsResult{}, d, false, nil
}

// nextDraft asks the author for the next draft of session id in the
// background, after the step r that led to it, and posts the draft after the
// confirmation of operation after. It reports false when nothing runs.
func (a *ConversationStandards) nextDraft(ctx context.Context, r messaging.StandardsRequest, id, version, after string) bool {
	if a.Analyses == nil || !a.Analyses.available() || id == "" || version == "" {
		return false
	}
	next := r
	next.Operation = "author.continue"
	next.Arguments = map[string]string{"action": "continue", "id": id, "version": version}
	next.OperationKey = r.OperationKey + "/draft-" + version
	_, d, done, e := a.admit(ctx, next)
	if done || e != nil {
		return false
	}
	return a.draftLater(ctx, next, d, after)
}

// draftLater runs the admitted continuation r in the background, one per
// session, and posts the draft after the confirmation of operation after.
// Answers that arrive meanwhile go into the next draft.
func (a *ConversationStandards) draftLater(ctx context.Context, r messaging.StandardsRequest, d, after string) bool {
	id := r.Arguments["id"]
	if a.Analyses == nil || !a.Analyses.claimAuthor(id) {
		return false
	}
	started := a.Analyses.later(ctx, r.ChannelID, "standards-author:"+r.OperationKey, after, func(ctx context.Context) string {
		_, err := a.run(ctx, r, d)
		queued := a.Analyses.releaseAuthor(id)
		answered := a.answerQueued(ctx, queued)
		if err != nil {
			text := "The standards author couldn't finish the draft: " + plainFailure(err.Error()) + ". Nothing was changed. Say \"again\" and I'll ask it once more."
			if answered != nil {
				text += " Your answers are saved for it."
			}
			return text
		}
		if answered != nil {
			// The draft just made is already behind the owner's answers.
			version := strconv.FormatInt(answered.Version, 10)
			if a.nextDraft(ctx, queued[len(queued)-1], id, version, queued[len(queued)-1].OperationKey) {
				return ""
			}
		}
		v, err := a.Authoring.Show(ctx, id, 0)
		if err != nil {
			return "The standards author has finished, but I couldn't read its draft. Ask me for it in a moment."
		}
		return authorDraftText(v)
	})
	if !started {
		a.Analyses.releaseAuthor(id)
	}
	return started
}

// answerQueued records answers that arrived while the author was drafting,
// on the newest draft. It returns the session after the last answer, or nil.
func (a *ConversationStandards) answerQueued(ctx context.Context, queued []messaging.StandardsRequest) *standards.AuthoringSession {
	var last *standards.AuthoringSession
	for _, q := range queued {
		v, e := a.Authoring.Show(ctx, q.Arguments["id"], 0)
		if e != nil {
			continue
		}
		q.Arguments = maps.Clone(q.Arguments)
		q.Arguments["version"] = strconv.FormatInt(v.Version, 10)
		_, d, done, e := a.admit(ctx, q)
		if done || e != nil {
			continue
		}
		result, e := a.run(ctx, q, d)
		if e != nil {
			if a.Analyses.Log != nil {
				a.Analyses.Log.WarnContext(ctx, "queued author answer could not be recorded", slog.String("session", q.Arguments["id"]), slog.Any("error", e))
			}
			continue
		}
		if version, e := strconv.ParseInt(result.References["version"], 10, 64); e == nil {
			last = &standards.AuthoringSession{ID: q.Arguments["id"], Version: version}
		}
	}
	return last
}

// AuthorSession reports the newest unfinished author session started in
// channel, so the conversation can take the owner's answers to it.
func (a *ConversationStandards) AuthorSession(ctx context.Context, channel string) (messaging.StandardsAuthorSession, bool, error) {
	if a.Authoring == nil || a.Authoring.Store == nil {
		return messaging.StandardsAuthorSession{}, false, nil
	}
	sessions, e := a.Authoring.List(ctx, 25)
	if e != nil {
		return messaging.StandardsAuthorSession{}, false, e
	}
	for _, listed := range sessions {
		if !strings.HasPrefix(listed.ID, "conversation-discord:"+channel+":") {
			continue
		}
		v, e := a.Authoring.Show(ctx, listed.ID, 0)
		if e != nil {
			return messaging.StandardsAuthorSession{}, false, e
		}
		if v.Status == "completed" {
			return messaging.StandardsAuthorSession{}, false, nil
		}
		out := messaging.StandardsAuthorSession{ID: v.ID, Version: v.Version, Rule: strings.TrimSpace(v.Draft.Rule), Running: a.Analyses != nil && a.Analyses.authorRunning(v.ID)}
		for _, q := range v.Questions {
			if q.State == "pending" {
				out.QuestionIDs = append(out.QuestionIDs, q.ID)
				out.Questions = append(out.Questions, authorQuestion(len(out.Questions)+1, q))
			}
		}
		return out, true, nil
	}
	return messaging.StandardsAuthorSession{}, false, nil
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

// authorDraftText shows an authoring session's draft for the owner in a few
// lines: the rule, whom it applies to and how it is checked, a short
// description and the numbered questions with lettered options. The full
// draft is a lookup away.
func authorDraftText(v standards.AuthoringSession) string {
	var b strings.Builder
	d := v.Draft
	b.WriteString("The standards author has a draft:\n\n" + strings.TrimSpace(d.Rule) + "\n")
	var about []string
	if len(d.AppliesTo) > 0 {
		about = append(about, "Applies to: "+strings.Join(d.AppliesTo, ", "))
	}
	if d.Check != "" {
		about = append(about, "Check: "+strings.ToLower(standards.CheckLabel(d.Check)))
	}
	if len(about) > 0 {
		b.WriteString(strings.Join(about, " · ") + "\n")
	}
	if text := shortDescription(d.Description); text != "" {
		b.WriteString(text + "\n")
	}
	n := 0
	for _, q := range v.Questions {
		if q.State != "pending" {
			continue
		}
		if n == 0 {
			b.WriteString("\nQuestions:\n")
		}
		n++
		b.WriteString(authorQuestion(n, q) + "\n")
	}
	next := "Say it's good and I'll turn it into a proposal, tell me what to change, or ask for the full draft."
	if n > 0 {
		next = "Answer here, for example \"1a, 2b\" or in your own words, and the author works them in. Ask for the full draft if you want it."
	}
	return b.String() + "\n" + next
}

// authorQuestion numbers one question and letters its options.
func authorQuestion(n int, q standards.AuthoringQuestion) string {
	text := fmt.Sprintf("%d. %s", n, strings.TrimSpace(q.Text))
	for i, option := range q.Options {
		if i < 26 {
			text += fmt.Sprintf(" (%c) %s", 'a'+i, strings.TrimSpace(option))
		}
	}
	return text
}

// shortDescription keeps the first sentence or two of a description.
func shortDescription(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= 280 {
		return text
	}
	cut := strings.LastIndex(text[:280], ". ")
	if cut < 80 {
		return strings.ToValidUTF8(text[:277], "") + "…"
	}
	return text[:cut+1]
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
