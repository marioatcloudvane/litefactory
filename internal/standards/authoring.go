package standards

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/instructionpackage"
	"github.com/cloudvane/litefactory/internal/store"
)

// AuthoringRunner performs one isolated response-only call. It grants no tools.
type AuthoringRunner interface {
	Answer(context.Context, string, string) ([]byte, error)
}

// AuthoringStore is the durable session and fenced-attempt boundary.
type AuthoringStore interface {
	SaveStandardsAuthoring(context.Context, store.StandardsAuthoringSession, int64) (store.StandardsAuthoringSession, error)
	GetStandardsAuthoring(context.Context, string, int64) (store.StandardsAuthoringSession, error)
	ListStandardsAuthoring(context.Context, int) ([]store.StandardsAuthoringSession, error)
	BeginStandardsAuthoringAttempt(context.Context, store.StandardsAuthoringAttempt) (store.StandardsAuthoringAttempt, error)
	FinishStandardsAuthoringAttempt(context.Context, store.StandardsAuthoringAttempt, *store.StandardsAuthoringSession) (store.StandardsAuthoringAttempt, error)
	CompleteStandardsAuthoring(context.Context, string, int64, store.StandardsProposalVersion) (store.StandardsProposalVersion, error)
	GetStandardsAuthoringCompletion(context.Context, string) (store.StandardsProposalVersion, error)
	ListStandardsAuthoringAttempts(context.Context, string) ([]store.StandardsAuthoringAttempt, error)
}

// Authoring coordinates durable drafts without approval or publication authority.
type Authoring struct {
	Store        AuthoringStore
	Book         *Service
	Runner       AuthoringRunner
	Instructions *instructionpackage.Package
}

// AuthoringStart identifies one standard and its initial retained intent.
type AuthoringStart struct {
	ID, Mode, TargetID, Intent string
	CategoryIDs                []string
}

// AuthoringAnswer supplies an explicit operator answer or replacement.
type AuthoringAnswer struct {
	QuestionID string `json:"question_id"`
	Text       string `json:"text"`
	Replace    bool   `json:"replace"`
}

// AuthoringEdit revises authoritative draft fields or narrows retained context.
type AuthoringEdit struct {
	Title              *string   `json:"title,omitempty"`
	Body               *string   `json:"body,omitempty"`
	CategoryIDs        *[]string `json:"category_ids,omitempty"`
	ContextCategoryIDs *[]string `json:"context_category_ids,omitempty"`
}

// AuthoringQuestion retains a host-issued question and settled answer history.
type AuthoringQuestion struct {
	ID             string                 `json:"id"`
	Text           string                 `json:"text"`
	Options        []string               `json:"options,omitempty"`
	State          string                 `json:"state"`
	Answer         string                 `json:"answer,omitempty"`
	AnswerHistory  []string               `json:"answer_history,omitempty"`
	AnswerEvents   []AuthoringAnswerEvent `json:"answer_events,omitempty"`
	FollowUpTo     string                 `json:"follow_up_to,omitempty"`
	CreatedVersion int64                  `json:"created_version"`
}

// AuthoringAnswerEvent retains operator provenance and explicit replacement linkage.
type AuthoringAnswerEvent struct {
	ID             string `json:"id"`
	Text           string `json:"text"`
	Operator       string `json:"operator"`
	CreatedVersion int64  `json:"created_version"`
	Replaces       string `json:"replaces,omitempty"`
}

// AuthoringContext records selected categories and exact supplied standards.
type AuthoringContext struct {
	CategoryIDs []string   `json:"category_ids"`
	Reason      string     `json:"reason"`
	Standards   []Standard `json:"standards"`
	Coverage    []string   `json:"coverage"`
}

// AuthoringState is the immutable saved authoring input and draft.
type AuthoringState struct {
	Intent         string              `json:"intent"`
	Operator       string              `json:"operator"`
	BaseRevision   int64               `json:"base_revision"`
	BaseDigest     string              `json:"base_digest"`
	BaseSchema     int                 `json:"base_schema"`
	Base           Snapshot            `json:"base"`
	Original       *Standard           `json:"original,omitempty"`
	Draft          Standard            `json:"draft"`
	Overrides      []string            `json:"operator_fields"`
	Questions      []AuthoringQuestion `json:"questions"`
	Diagnostics    []Problem           `json:"diagnostics"`
	Context        AuthoringContext    `json:"context"`
	CategoryFilter []string            `json:"category_filter,omitempty"`
	RebaseChanges  []Change            `json:"rebase_changes,omitempty"`
	RoleVersion    string              `json:"role_version"`
	RoleDigest     string              `json:"role_digest"`
	Notes          string              `json:"notes,omitempty"`
	RoleCapture    json.RawMessage     `json:"role_capture"`
}

// AuthoringSession exposes a saved version with its durable diagnostics and handoff.
type AuthoringSession struct {
	ID       string                          `json:"id"`
	Version  int64                           `json:"version"`
	Mode     string                          `json:"mode"`
	TargetID string                          `json:"target_id"`
	Status   string                          `json:"status"`
	Proposal *store.StandardsProposalVersion `json:"proposal,omitempty"`
	AuthoringState
	Attempts []store.StandardsAuthoringAttempt `json:"attempts,omitempty"`
}

func authorID(prefix string) string { return prefix + rand.Text() }
func authorDecode(r store.StandardsAuthoringSession) (AuthoringSession, error) {
	s := AuthoringSession{ID: r.ID, Version: r.Version, Mode: r.Mode, TargetID: r.TargetID, Status: r.Status}
	err := json.Unmarshal(r.Data, &s.AuthoringState)
	if err != nil {
		return s, fmt.Errorf("%w: invalid authoring data", store.ErrStandardsCorrupt)
	}
	return s, nil
}
func authorRecord(s AuthoringSession) store.StandardsAuthoringSession {
	b, _ := json.Marshal(s.AuthoringState)
	return store.StandardsAuthoringSession{ID: s.ID, Version: s.Version, Mode: s.Mode, TargetID: s.TargetID, Status: s.Status, Data: b}
}

// Show reads a saved version and its retained attempts and proposal link.
func (s *Authoring) Show(ctx context.Context, id string, version int64) (AuthoringSession, error) {
	r, e := s.Store.GetStandardsAuthoring(ctx, id, version)
	if e != nil {
		return AuthoringSession{}, e
	}
	v, e := authorDecode(r)
	if e != nil {
		return v, e
	}
	p, e := s.Store.GetStandardsAuthoringCompletion(ctx, id)
	if e == nil {
		v.Proposal = &p
		v.Status = "completed"
	} else if !errors.Is(e, store.ErrNotFound) {
		return v, e
	}
	v.Attempts, e = s.Store.ListStandardsAuthoringAttempts(ctx, id)
	return v, e
}

// List returns a bounded collection of saved sessions.
func (s *Authoring) List(ctx context.Context, limit int) ([]AuthoringSession, error) {
	rs, e := s.Store.ListStandardsAuthoring(ctx, limit)
	if e != nil {
		return nil, e
	}
	out := []AuthoringSession{}
	for _, r := range rs {
		v, e := authorDecode(r)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, nil
}
func (s *Authoring) save(ctx context.Context, v AuthoringSession) (AuthoringSession, error) {
	v.Draft.CategoryIDs = append([]string{}, v.Draft.CategoryIDs...)
	slices.Sort(v.Draft.CategoryIDs)
	expected := v.Version
	v.Version++
	r, e := s.Store.SaveStandardsAuthoring(ctx, authorRecord(v), expected)
	if e != nil {
		return v, e
	}
	return authorDecode(r)
}
func (s *Authoring) input(ctx context.Context, id string, expected int64) (AuthoringSession, error) {
	v, e := s.Show(ctx, id, 0)
	if e != nil {
		return v, e
	}
	if expected < 1 || v.Version != expected || v.Status == "completed" {
		return v, store.ErrStandardsConflict
	}
	return v, nil
}
func (s *Authoring) capture(ctx context.Context) (Snapshot, int64, string, error) {
	h, e := s.Book.Store.StandardsHead(ctx)
	if e != nil {
		return Snapshot{}, 0, "", e
	}
	if h == 0 {
		return canonical(Snapshot{}), 0, snapshotDigest(Snapshot{}), nil
	}
	r, e := s.Book.Revision(ctx, h)
	if e != nil {
		return Snapshot{}, 0, "", e
	}
	b, _ := json.Marshal(r.Snapshot)
	if len(b) > 16<<20 {
		return Snapshot{}, 0, "", fmt.Errorf("needs-context: snapshot exceeds 16 MiB capture limit")
	}
	return r.Snapshot, h, r.Digest, nil
}

// Start saves an incomplete draft and verified context before any provider call.
func (s *Authoring) Start(ctx context.Context, in AuthoringStart) (AuthoringSession, error) {
	if strings.TrimSpace(in.Intent) == "" || (in.Mode != "create" && in.Mode != "edit") {
		return AuthoringSession{}, ErrInvalid
	}
	base, rev, d, e := s.capture(ctx)
	if e != nil {
		return AuthoringSession{}, e
	}
	if in.ID == "" {
		in.ID = authorID("author-")
	}
	if in.Mode == "create" {
		if in.TargetID != "" {
			return AuthoringSession{}, ErrInvalid
		}
		in.TargetID = authorID("standard-")
	}
	v := AuthoringSession{ID: in.ID, Mode: in.Mode, TargetID: in.TargetID, Status: "draft", AuthoringState: AuthoringState{Intent: in.Intent, Operator: strconv.FormatInt(s.Book.OwnerID, 10), Base: base, BaseRevision: rev, BaseDigest: d, BaseSchema: 1, Draft: Standard{ID: in.TargetID}, CategoryFilter: in.CategoryIDs}}
	if in.Mode == "edit" {
		for _, x := range base.Standards {
			if x.ID == in.TargetID {
				y := x
				v.Original = &y
				v.Draft = x
			}
		}
		if v.Original == nil {
			return v, fmt.Errorf("%w: unknown edit target", store.ErrNotFound)
		}
	}
	instructions := s.Instructions
	if instructions == nil {
		instructions, e = instructionpackage.LoadShipped(ctx, "")
		if e != nil {
			return v, e
		}
	}
	capture, _, e := instructions.Capture(ctx, "standards-author")
	if e != nil {
		return v, e
	}
	v.RoleCapture, e = capture.Encode()
	if e != nil {
		return v, e
	}
	meta := capture.Snapshot()
	v.RoleVersion = meta.ShippedVersion
	v.RoleDigest = meta.EffectiveDigest
	if e = authorCategories(base, in.CategoryIDs); e != nil {
		return v, e
	}
	return s.save(ctx, v)
}
func authorCategories(base Snapshot, ids []string) error {
	seen := map[string]bool{}
	var problems []error
	for _, id := range ids {
		if seen[id] {
			problems = append(problems, fmt.Errorf("%w: duplicate category %s", ErrInvalid, id))
		}
		seen[id] = true
		if !slices.ContainsFunc(base.Categories, func(c Category) bool { return c.ID == id }) {
			problems = append(problems, fmt.Errorf("%w: unknown category %s", ErrInvalid, id))
		}
	}
	return errors.Join(problems...)
}

// Answer atomically saves explicit answers without invoking a provider.
func (s *Authoring) Answer(ctx context.Context, id string, expected int64, answers []AuthoringAnswer) (AuthoringSession, error) {
	v, e := s.input(ctx, id, expected)
	if e != nil {
		return v, e
	}
	if len(answers) == 0 {
		return v, ErrInvalid
	}
	seen := map[string]bool{}
	var problems []error
	for _, a := range answers {
		if seen[a.QuestionID] || strings.TrimSpace(a.Text) == "" {
			problems = append(problems, fmt.Errorf("%w: duplicate question or blank answer %s", ErrInvalid, a.QuestionID))
			continue
		}
		seen[a.QuestionID] = true
		i := slices.IndexFunc(v.Questions, func(q AuthoringQuestion) bool { return q.ID == a.QuestionID })
		if i < 0 {
			problems = append(problems, fmt.Errorf("%w: unknown question %s", ErrInvalid, a.QuestionID))
			continue
		}
		q := &v.Questions[i]
		if q.State == "superseded" || (q.State == "answered" && !a.Replace) {
			problems = append(problems, fmt.Errorf("%w: question %s requires explicit answer replacement", ErrInvalid, a.QuestionID))
			continue
		}
		if q.State == "answered" {
			q.AnswerHistory = append(q.AnswerHistory, q.Answer)
		}
		replaces := ""
		if len(q.AnswerEvents) > 0 {
			replaces = q.AnswerEvents[len(q.AnswerEvents)-1].ID
		}
		q.AnswerEvents = append(q.AnswerEvents, AuthoringAnswerEvent{ID: authorID("answer-"), Text: a.Text, Operator: strconv.FormatInt(s.Book.OwnerID, 10), CreatedVersion: v.Version + 1, Replaces: replaces})
		q.Answer = a.Text
		q.State = "answered"
	}
	if e = errors.Join(problems...); e != nil {
		return v, e
	}
	v.Status = "draft"
	return s.save(ctx, v)
}

// Edit saves authoritative operator fields without invoking a provider.
func (s *Authoring) Edit(ctx context.Context, id string, expected int64, in AuthoringEdit) (AuthoringSession, error) {
	v, e := s.input(ctx, id, expected)
	if e != nil {
		return v, e
	}
	if in.ContextCategoryIDs != nil {
		if e = authorCategories(v.Base, *in.ContextCategoryIDs); e != nil {
			return v, e
		}
		v.CategoryFilter = append([]string{}, *in.ContextCategoryIDs...)
		v.Context = AuthoringContext{}
	}
	if in.Title == nil && in.Body == nil && in.CategoryIDs == nil && in.ContextCategoryIDs == nil {
		return v, ErrInvalid
	}
	for _, f := range []string{"title", "body", "category_ids"} {
		set := false
		switch f {
		case "title":
			if in.Title != nil {
				v.Draft.Title = *in.Title
				set = true
			}
		case "body":
			if in.Body != nil {
				v.Draft.Body = *in.Body
				set = true
			}
		case "category_ids":
			if in.CategoryIDs != nil {
				if e = authorCategories(v.Base, *in.CategoryIDs); e != nil {
					return v, e
				}
				v.Draft.CategoryIDs = *in.CategoryIDs
				set = true
			}
		}
		if set && !slices.Contains(v.Overrides, f) {
			v.Overrides = append(v.Overrides, f)
		}
	}
	v.Diagnostics = nil
	v.Status = "draft"
	return s.save(ctx, v)
}
func authorVerify(v AuthoringSession) error {
	if v.Draft.ID != v.TargetID || v.TargetID == "" || (v.Mode != "create" && v.Mode != "edit") {
		return fmt.Errorf("%w: fixed authoring target identity mismatch", store.ErrStandardsCorrupt)
	}
	b, _ := json.Marshal(v.Base)
	if len(b) > 16<<20 || v.BaseSchema != 1 || snapshotDigest(v.Base) != v.BaseDigest {
		return fmt.Errorf("%w: retained authoring base invalid", store.ErrStandardsCorrupt)
	}
	return validateSnapshot(v.Base)
}
func authorOps(v AuthoringSession) []Operation {
	kind := "standard.add"
	if v.Mode == "edit" {
		kind = "standard.replace"
	}
	x := v.Draft
	return []Operation{{Kind: kind, Standard: &x}}
}
func authorValidate(v AuthoringSession) (Snapshot, []Problem) {
	problems := []Problem{}
	if e := authorCategories(v.Base, v.CategoryFilter); e != nil {
		problems = append(problems, Problem{"category_filter", e.Error() + "; revise context_category_ids"})
	}
	for _, q := range v.Questions {
		if q.State == "pending" {
			problems = append(problems, Problem{"questions." + q.ID, "answer this blocking question"})
		}
	}
	after, e := apply(v.Base, authorOps(v))
	if e != nil {
		var ve *ValidationError
		if errors.As(e, &ve) {
			problems = append(problems, ve.Problems...)
		} else {
			problems = append(problems, Problem{"draft", e.Error()})
		}
	}
	return after, problems
}

// Validate retains upstream validation diagnostics without discarding draft text.
func (s *Authoring) Validate(ctx context.Context, id string, expected int64) (AuthoringSession, error) {
	v, e := s.input(ctx, id, expected)
	if e != nil {
		return v, e
	}
	if e = authorVerify(v); e != nil {
		return v, e
	}
	_, v.Diagnostics = authorValidate(v)
	return s.save(ctx, v)
}

// Complete atomically hands the exact inspected draft to separate proposal review.
func (s *Authoring) Complete(ctx context.Context, id string, expected int64) (store.StandardsProposalVersion, error) {
	v, e := s.Show(ctx, id, expected)
	if e != nil {
		return store.StandardsProposalVersion{}, e
	}
	if v.Version != expected {
		return store.StandardsProposalVersion{}, store.ErrStandardsConflict
	}
	if e = authorVerify(v); e != nil {
		return store.StandardsProposalVersion{}, e
	}
	after, problems := authorValidate(v)
	if len(problems) > 0 {
		invalid := &ValidationError{problems}
		cur, ce := s.input(ctx, id, expected)
		if ce != nil {
			return store.StandardsProposalVersion{}, errors.Join(invalid, ce)
		}
		cur.Diagnostics = problems
		if _, ce = s.save(ctx, cur); ce != nil {
			return store.StandardsProposalVersion{}, errors.Join(invalid, ce)
		}
		return store.StandardsProposalVersion{}, invalid
	}
	p := store.StandardsProposalVersion{ProposalID: "authoring-" + id, Version: 1, BaseRevision: v.BaseRevision, SchemaVersion: 1, Operations: canonicalOps(authorOps(v)), ChangeDigest: changeDigest(v.BaseRevision, authorOps(v)), ResultDigest: snapshotDigest(after), Author: strconv.FormatInt(s.Book.OwnerID, 10)}
	return s.Store.CompleteStandardsAuthoring(ctx, id, expected, p)
}

// Rebase deliberately captures a chosen current head while retaining draft text.
func (s *Authoring) Rebase(ctx context.Context, id string, expected int64, chosenHead ...int64) (AuthoringSession, error) {
	v, e := s.input(ctx, id, expected)
	if e != nil {
		return v, e
	}
	base, rev, d, e := s.capture(ctx)
	if e != nil {
		return v, e
	}
	if len(chosenHead) > 0 && chosenHead[0] != rev {
		return v, fmt.Errorf("%w: chosen revision is no longer current", store.ErrStandardsConflict)
	}
	changes := diff(v.Base, base)
	if v.Mode == "edit" {
		found := false
		for _, x := range base.Standards {
			if x.ID == v.TargetID {
				found = true
				y := x
				v.Original = &y
			}
		}
		if !found {
			return v, fmt.Errorf("%w: target removed; retain this session and create a separate session", store.ErrStandardsConflict)
		}
	}
	v.RebaseChanges = changes
	v.Base = base
	v.BaseRevision = rev
	v.BaseDigest = d
	v.Context = AuthoringContext{}
	_, v.Diagnostics = authorValidate(v)
	for _, c := range changes {
		if c.Entity == "standard" && c.ID == v.TargetID || c.Entity == "category" && slices.Contains(v.Draft.CategoryIDs, c.ID) {
			v.Questions = append(v.Questions, AuthoringQuestion{ID: authorID("q-"), Text: "Confirm the retained draft remains appropriate after rebase change to " + c.Entity + " " + c.ID, State: "pending", CreatedVersion: v.Version + 1})
		}
	}
	return s.save(ctx, v)
}

// strictAuthorJSON rejects duplicate members, unknown fields and trailing values.
func strictAuthorJSON(raw []byte, out any) error {
	if len(raw) > 32<<10 {
		return fmt.Errorf("response exceeds 32 KiB")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	var walk func() error
	walk = func() error {
		t, e := d.Token()
		if e != nil {
			return e
		}
		if t == nil {
			return errors.New("null JSON value is not allowed")
		}
		if x, ok := t.(json.Delim); ok {
			switch x {
			case '{':
				seen := map[string]bool{}
				for d.More() {
					k, e := d.Token()
					if e != nil {
						return e
					}
					key, ok := k.(string)
					if !ok || seen[key] {
						return fmt.Errorf("duplicate JSON member")
					}
					seen[key] = true
					if e = walk(); e != nil {
						return e
					}
				}
				_, e = d.Token()
				return e
			case '[':
				for d.More() {
					if e = walk(); e != nil {
						return e
					}
				}
				_, e = d.Token()
				return e
			default:
				return fmt.Errorf("invalid delimiter")
			}
		}
		return nil
	}
	if e := walk(); e != nil {
		return e
	}
	if _, e := d.Token(); !errors.Is(e, io.EOF) {
		return fmt.Errorf("trailing JSON")
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(out)
}

type authorSelection struct {
	SchemaVersion int      `json:"schema_version"`
	CategoryIDs   []string `json:"category_ids"`
	Reason        string   `json:"reason"`
}
type authorQuestionOutput struct {
	Text       string   `json:"text"`
	Options    []string `json:"options,omitempty"`
	FollowUpTo string   `json:"follow_up_to,omitempty"`
}
type authorDraftOutput struct {
	SchemaVersion        int                    `json:"schema_version"`
	Title                string                 `json:"title"`
	Body                 string                 `json:"body"`
	CategoryIDs          []string               `json:"category_ids"`
	Questions            []authorQuestionOutput `json:"questions"`
	SupersedeQuestionIDs []string               `json:"supersede_question_ids,omitempty"`
	Notes                string                 `json:"notes,omitempty"`
}

func authorPrompt(role, kind string, data any) (string, error) {
	b, e := json.Marshal(data)
	if e != nil {
		return "", e
	}
	schema := `{"schema_version":1,"category_ids":["offered category ID"],"reason":"why relevant"}`
	if kind == "draft" {
		schema = `{"schema_version":1,"title":"text","body":"Markdown","category_ids":["supplied category ID"],"questions":[{"text":"missing information","options":[],"follow_up_to":"optional existing question ID"}],"supersede_question_ids":["optional pending question ID"],"notes":"brief explanation"}`
	}
	p := role + "\nOperation: " + kind + "\nRequired response schema: " + schema + "\nUntrusted data:\n" + string(b)
	if len(p) > 256<<10 {
		return "", fmt.Errorf("needs-context: prompt exceeds 256 KiB; narrow category filters")
	}
	return p, nil
}

// Continue runs at most two bounded response-only calls against durable inputs.
func (s *Authoring) Continue(ctx context.Context, id string, expected int64) (v AuthoringSession, err error) {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	v, err = s.input(ctx, id, expected)
	if err != nil {
		return
	}
	if err = authorVerify(v); err != nil {
		return
	}
	if s.Runner == nil {
		return v, fmt.Errorf("provider unavailable: configure an eligible response-only provider")
	}
	capture, e := instructionpackage.Restore(v.RoleCapture)
	if e != nil {
		return v, e
	}
	meta := capture.Snapshot()
	if meta.Role != "standards-author" || meta.EffectiveDigest != v.RoleDigest || meta.ShippedVersion != v.RoleVersion {
		return v, fmt.Errorf("%w: authoring role binding invalid", store.ErrStandardsCorrupt)
	}
	role := string(capture.Instructions())
	for _, f := range meta.Files {
		if f.Path == instructionpackage.StandardsAuthorSchemaPath {
			role += "\nCaptured response schema:\n" + string(f.Content)
		}
	}
	inputRecord := authorRecord(v)
	attempt := store.StandardsAuthoringAttempt{ID: authorID("attempt-"), SessionID: id, InputVersion: expected, Token: authorID("fence-"), LeaseUntil: time.Now().UTC().Add(125 * time.Second), InputDigest: digest(v), RoleVersion: v.RoleVersion}
	attempt, err = s.Store.BeginStandardsAuthoringAttempt(store.WithoutStandardsConversationOperation(ctx), attempt)
	if err != nil {
		return
	}
	defer func() {
		if err != nil {
			var saved *store.StandardsAuthoringSession
			if strings.Contains(err.Error(), "needs-context:") {
				recovery, e := authorDecode(inputRecord)
				if e != nil {
					err = errors.Join(err, e)
				} else {
					recovery.Status = "needs-context"
					recovery.Diagnostics = []Problem{{"context", err.Error()}}
					recovery.Version = expected + 1
					v = recovery
					r := authorRecord(recovery)
					saved = &r
				}
			}
			attempt.Outcome = "failed"
			attempt.Diagnostic = boundedReason(err.Error())
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if _, e := s.Store.FinishStandardsAuthoringAttempt(cleanup, attempt, saved); e != nil {
				err = errors.Join(err, e)
			}
		}
	}()
	call := func(kind string, data any, out any) error {
		prompt, e := authorPrompt(role, kind, data)
		if e != nil {
			return e
		}
		attempt.InputDigest = digest(struct{ Previous, Prompt string }{attempt.InputDigest, prompt})
		var raw []byte
		if fenced, ok := s.Runner.(interface {
			AnswerAttempt(context.Context, string, string, string, string) ([]byte, error)
		}); ok {
			raw, e = fenced.AnswerAttempt(ctx, id, attempt.ID, attempt.Token, prompt)
		} else {
			raw, e = s.Runner.Answer(ctx, id, prompt)
		}
		if metadata, ok := s.Runner.(interface {
			Metadata() (string, string, string)
		}); ok {
			attempt.Provider, attempt.Model, attempt.UsageReference = metadata.Metadata()
		}
		if e != nil {
			return e
		}
		if len(raw) > 32<<10 {
			return fmt.Errorf("invalid model response: response exceeds 32 KiB")
		}
		var fields map[string]json.RawMessage
		if e = json.Unmarshal(raw, &fields); e == nil {
			required := []string{"schema_version", "category_ids", "reason"}
			if kind == "draft" {
				required = []string{"schema_version", "title", "body", "category_ids", "questions"}
			}
			for _, key := range required {
				value, ok := fields[key]
				if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
					return fmt.Errorf("invalid model response: missing required %s", key)
				}
			}
		}
		if e = strictAuthorJSON(raw, out); e != nil {
			return fmt.Errorf("invalid model response: %w", e)
		}
		return nil
	}
	ids := append([]string{}, v.CategoryFilter...)
	reason := "operator category filter"
	if len(ids) == 0 && len(v.Base.Categories) > 0 {
		var chosen authorSelection
		err = call("select", struct {
			Intent     string     `json:"intent"`
			Categories []Category `json:"categories"`
		}{v.Intent, v.Base.Categories}, &chosen)
		if err != nil {
			return
		}
		if chosen.SchemaVersion != 1 || strings.TrimSpace(chosen.Reason) == "" || len(chosen.Reason) > 4096 {
			err = fmt.Errorf("invalid model response: selection schema/reason")
			return
		}
		ids = chosen.CategoryIDs
		reason = chosen.Reason
	}
	if err = authorCategories(v.Base, ids); err != nil {
		return
	}
	slices.Sort(ids)
	v.Context = AuthoringContext{CategoryIDs: ids, Reason: reason, Standards: []Standard{}, Coverage: []string{}}
	for _, item := range v.Base.Standards {
		selected := item.ID == v.TargetID
		for _, c := range item.CategoryIDs {
			if slices.Contains(ids, c) {
				selected = true
			}
		}
		if selected {
			v.Context.Standards = append(v.Context.Standards, item)
			v.Context.Coverage = append(v.Context.Coverage, item.ID)
		}
	}
	offered := []Category{}
	for _, c := range v.Base.Categories {
		if slices.Contains(ids, c.ID) {
			offered = append(offered, c)
		}
	}
	var output authorDraftOutput
	err = call("draft", struct {
		Intent         string              `json:"intent"`
		Operator       string              `json:"operator"`
		Draft          Standard            `json:"draft"`
		OperatorFields []string            `json:"operator_fields"`
		Questions      []AuthoringQuestion `json:"questions"`
		Categories     []Category          `json:"categories"`
		Context        AuthoringContext    `json:"context"`
	}{v.Intent, v.Operator, v.Draft, v.Overrides, v.Questions, offered, v.Context}, &output)
	if err != nil {
		return
	}
	if output.SchemaVersion != 1 || len(output.Questions) > 100 || len(output.Notes) > 4096 {
		err = fmt.Errorf("invalid model response: draft schema/limits")
		return
	}
	if err = authorCategories(Snapshot{Categories: offered}, output.CategoryIDs); err != nil {
		return
	}
	for _, qid := range output.SupersedeQuestionIDs {
		i := slices.IndexFunc(v.Questions, func(q AuthoringQuestion) bool { return q.ID == qid })
		if i < 0 || v.Questions[i].State != "pending" {
			err = fmt.Errorf("invalid model response: only pending supplied questions may be superseded")
			return
		}
		v.Questions[i].State = "superseded"
	}
	for _, q := range output.Questions {
		if strings.TrimSpace(q.Text) == "" || len(q.Text) > 4096 || len(q.Options) > 20 {
			err = fmt.Errorf("invalid model response: question limits")
			return
		}
		if q.FollowUpTo != "" && !slices.ContainsFunc(v.Questions, func(x AuthoringQuestion) bool { return x.ID == q.FollowUpTo }) {
			err = fmt.Errorf("invalid model response: unknown follow-up question")
			return
		}
		if slices.ContainsFunc(v.Questions, func(x AuthoringQuestion) bool { return x.Text == q.Text && reflect.DeepEqual(x.Options, q.Options) }) {
			continue
		}
		if slices.ContainsFunc(v.Questions, func(x AuthoringQuestion) bool {
			return x.State == "answered" && x.Text == q.Text && q.FollowUpTo != x.ID
		}) {
			err = fmt.Errorf("invalid model response: an answered question requires an explicitly linked follow-up")
			return
		}
		v.Questions = append(v.Questions, AuthoringQuestion{ID: authorID("q-"), Text: q.Text, Options: q.Options, FollowUpTo: q.FollowUpTo, State: "pending", CreatedVersion: expected + 1})
	}
	if !slices.Contains(v.Overrides, "title") {
		v.Draft.Title = output.Title
	}
	if !slices.Contains(v.Overrides, "body") {
		v.Draft.Body = output.Body
	}
	if !slices.Contains(v.Overrides, "category_ids") {
		v.Draft.CategoryIDs = output.CategoryIDs
	}
	v.Draft.CategoryIDs = append([]string{}, v.Draft.CategoryIDs...)
	slices.Sort(v.Draft.CategoryIDs)
	_, v.Diagnostics = authorValidate(v)
	v.Notes = output.Notes
	v.Status = "draft"
	v.Version++
	record := authorRecord(v)
	attempt.Outcome = "accepted"
	attempt.ResultVersion = v.Version
	_, err = s.Store.FinishStandardsAuthoringAttempt(ctx, attempt, &record)
	return
}
