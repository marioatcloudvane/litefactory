package brainstorm

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"text/template"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
)

// PurposeDrafts marks a brainstorm_scan that prepares issue drafts.
const PurposeDrafts = store.BrainstormPurposeDrafts

// Draft bounds.
const (
	MaxDrafts         = 5
	MaxDraftTitle     = 256
	MaxIssueBodyBytes = 64 * 1024
	draftsSkillPath   = "skills/" + draftsSkill + "/SKILL.md"
	// DuplicateCheckUnavailable is stored when the factory's duplicate search failed.
	DuplicateCheckUnavailable = "duplicate check unavailable"
	draftsQuestion            = "Prepare issue drafts from this brainstorm."
	draftsPayloadPath         = "$.result.payload.brainstorm_drafts"
)

// Plain drafting refusals.
var (
	ErrDraftsSkillMissing = errors.New("the installed team version has no brainstorm-drafts skill; install a team version that has it")
	ErrNoDrafts           = errors.New("this brainstorm has no drafts yet; ask for drafts first")
	ErrDraftsChanged      = errors.New("the drafts changed; request a fresh proposal")
)

//go:embed templates/issue_body.md
var issueBodyTemplate string

var issueBody = template.Must(template.New("issue_body").Parse(issueBodyTemplate))

// DraftRequest admits one drafts scan.
type DraftRequest struct {
	BrainstormID string `json:"brainstorm"`
	RequestKey   string `json:"request_key"`
	Note         string `json:"note,omitempty"`
}

// DraftInput is one authored draft of payload.brainstorm_drafts.
type DraftInput struct {
	Key              string          `json:"key"`
	Title            string          `json:"title"`
	Size             string          `json:"size"`
	Impact           string          `json:"impact"`
	RelatedIssues    []string        `json:"related_issues"`
	DuplicateWarning string          `json:"duplicate_warning"`
	Ledger           json.RawMessage `json:"ledger"`
}

// DraftsPayload is payload.brainstorm_drafts.
type DraftsPayload struct {
	Drafts []DraftInput `json:"drafts"`
}

// DraftView is one stored draft as the client reads it.
type DraftView struct {
	ID                 string          `json:"draft"`
	Key                string          `json:"key"`
	Title              string          `json:"title"`
	Size               string          `json:"size"`
	Impact             string          `json:"impact"`
	RelatedIssues      []string        `json:"related_issues"`
	DuplicateWarning   string          `json:"duplicate_warning,omitempty"`
	PossibleDuplicates json.RawMessage `json:"possible_duplicates,omitempty"`
	Ledger             json.RawMessage `json:"ledger"`
}

// DraftSetView is brainstorm_drafts_get.
type DraftSetView struct {
	BrainstormID string      `json:"brainstorm"`
	DraftSetID   string      `json:"draft_set"`
	Seq          int64       `json:"seq"`
	State        string      `json:"state"`
	Summary      string      `json:"summary"`
	Drafts       []DraftView `json:"drafts"`
}

// TaskItemView is one draft's outcome in brainstorm_tasks_get.
type TaskItemView struct {
	DraftID     string `json:"draft"`
	Title       string `json:"title"`
	State       string `json:"state"`
	IssueNumber int    `json:"issue_number,omitempty"`
	IssueLink   string `json:"issue_link,omitempty"`
	Reason      string `json:"reason,omitempty"`
	NextStep    string `json:"next_step"`
}

// TasksView is brainstorm_tasks_get.
type TasksView struct {
	BatchID      string         `json:"batch"`
	BrainstormID string         `json:"brainstorm"`
	DraftSetID   string         `json:"draft_set"`
	State        string         `json:"state"`
	Items        []TaskItemView `json:"items"`
}

// IssueDraft is one rendered, protected issue for a kept draft.
type IssueDraft struct {
	DraftID, Key, Title, Body, Marker string
	Generation                        int
	Ledger                            json.RawMessage
	LedgerDigest                      string
}

// Marker is the issue body's last line that reconciliation looks for.
func Marker(brainstormID, draftID string, generation int) string {
	return "<!-- lf:brainstorm-draft brainstorm=" + brainstormID + " draft=" + draftID + " gen=" + strconv.Itoa(generation) + " -->"
}

func diag(path, code, expected, actual, message string) agentresult.Diagnostic {
	return agentresult.Diagnostic{Path: path, Code: code, Expected: expected, Actual: actual, Message: message}
}

// ValidateDraftsResult checks one authored drafts scan result: the shared
// result envelope and spoken summary rules, and per draft the shape, bounds
// and Research ledger. It returns the decoded result only when valid.
func ValidateDraftsResult(data []byte) (RunResult, agentresult.Diagnostics) {
	fail := func(ds ...agentresult.Diagnostic) (RunResult, agentresult.Diagnostics) {
		return RunResult{}, agentresult.Diagnostics{Complete: true, Diagnostics: ds, UncheckedPaths: []string{}}
	}
	if len(data) > MaxResultBytes {
		return fail(diag("$", "limit", "at most 120 KiB", "oversized result", "Keep the whole result within 120 KiB."))
	}
	var root map[string]json.RawMessage
	var result map[string]json.RawMessage
	var payload map[string]json.RawMessage
	if json.Unmarshal(data, &root) != nil || json.Unmarshal(root["result"], &result) != nil || json.Unmarshal(result["payload"], &payload) != nil {
		return fail(diag("$.result.payload", "shape", "an object with brainstorm_drafts", "not an object", "Send result.payload as {\"brainstorm_drafts\": {\"drafts\": [...]}}."))
	}
	raw, ok := payload["brainstorm_drafts"]
	if !ok || len(payload) != 1 {
		return fail(diag("$.result.payload", "payload_key", "exactly brainstorm_drafts", "other payload keys", "A drafts scan submits only payload.brainstorm_drafts."))
	}
	var p DraftsPayload
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return fail(diag(draftsPayloadPath, "shape", "{drafts:[{key,title,size,impact,related_issues,duplicate_warning,ledger}]}", "unknown or mistyped fields", "Use only the documented draft fields."))
	}
	var out agentresult.Diagnostics
	out.Complete, out.UncheckedPaths = true, []string{}
	add := func(d ...agentresult.Diagnostic) { out.Diagnostics = append(out.Diagnostics, d...) }
	if len(p.Drafts) < 1 || len(p.Drafts) > MaxDrafts {
		add(diag(draftsPayloadPath+".drafts", "count", "1 to 5 drafts", strconv.Itoa(len(p.Drafts))+" drafts", "Send between one and five drafts."))
	}
	// The envelope is checked once with a neutral research payload; each ledger
	// is checked as a research payload of the same envelope.
	envelope := func(ledger json.RawMessage) []byte {
		copyResult := make(map[string]json.RawMessage, len(result))
		for k, v := range result {
			copyResult[k] = v
		}
		copyResult["payload"], _ = json.Marshal(map[string]json.RawMessage{"research": ledger})
		copyRoot := make(map[string]json.RawMessage, len(root))
		for k, v := range root {
			copyRoot[k] = v
		}
		copyRoot["result"], _ = json.Marshal(copyResult)
		b, _ := json.Marshal(copyRoot)
		return b
	}
	ctx := agentresult.Context{SchemaVersion: agentresult.SchemaVersion, WorkflowVersion: 1, Mode: "research", QuestionsAllowed: true}
	var answer string
	var questions []string
	keys := map[string]bool{}
	drafts := make([]DraftInput, 0, len(p.Drafts))
	for i, d := range p.Drafts {
		at := fmt.Sprintf("%s.drafts[%d]", draftsPayloadPath, i)
		if !validText(d.Key, 64) || keys[d.Key] {
			add(diag(at+".key", "key", "a unique non-empty key of at most 64 bytes", "missing, long or repeated", "Give every draft its own short key."))
		}
		keys[d.Key] = true
		if !validText(d.Title, MaxDraftTitle) {
			add(diag(at+".title", "title", "a non-empty title of at most 256 bytes", "missing or too long", "Shorten the title."))
		}
		if d.Size != "S" && d.Size != "M" && d.Size != "L" {
			add(diag(at+".size", "size", "S, M or L", d.Size, "Use size S, M or L."))
		}
		if !validText(d.Impact, MaxEntryBytes) {
			add(diag(at+".impact", "impact", "a non-empty impact of at most 4 KiB", "missing or too long", "State the impact briefly."))
		}
		for j, r := range d.RelatedIssues {
			if !validText(r, 512) {
				add(diag(fmt.Sprintf("%s.related_issues[%d]", at, j), "related_issue", "a non-empty reference of at most 512 bytes", "invalid", "Name each related issue by reference."))
			}
		}
		if len(d.RelatedIssues) > 50 {
			add(diag(at+".related_issues", "count", "at most 50", strconv.Itoa(len(d.RelatedIssues)), "Keep the related issues short."))
		}
		if len(d.DuplicateWarning) > MaxEntryBytes || !utf8.ValidString(d.DuplicateWarning) {
			add(diag(at+".duplicate_warning", "duplicate_warning", "at most 4 KiB", "too long", "Shorten the duplicate warning."))
		}
		if len(bytes.TrimSpace(d.Ledger)) == 0 {
			add(diag(at+".ledger", "ledger", "a research ledger", "missing", "Add the draft ledger."))
			continue
		}
		r, ld := agentresult.Create(envelope(d.Ledger), ctx)
		for _, x := range ld.Diagnostics {
			if strings.HasPrefix(x.Path, "$.result.payload.research") {
				x.Path = at + ".ledger" + strings.TrimPrefix(x.Path, "$.result.payload.research")
				add(x)
			} else if i == 0 {
				add(x)
			}
		}
		if !ld.Complete {
			out.Complete = false
		}
		if !ld.Valid || r.Payload.Research == nil {
			continue
		}
		if i == 0 {
			answer = r.Comment
			for _, q := range r.Questions {
				questions = append(questions, q.Text)
			}
		}
		add(ledgerShapeDiagnostics(at+".ledger", *r.Payload.Research)...)
		add(ledgerDiagnostics(at+".ledger", *r.Payload.Research)...)
		canonical, err := json.Marshal(r.Payload.Research)
		if err != nil {
			out.Complete = false
			continue
		}
		d.Ledger = canonical
		body, err := renderIssueBody(issueContent{Marker: Marker("brainstorm", "draft", 1)}, d, nil, "")
		if err != nil || len(body) > MaxIssueBodyBytes {
			add(diag(at, "body_size", "a rendered issue body of at most 64 KiB", "too large", "Shorten the ledger of this draft."))
		}
		drafts = append(drafts, d)
	}
	var authored struct {
		Result struct {
			Comment string `json:"comment"`
		} `json:"result"`
	}
	if json.Unmarshal(data, &authored) == nil {
		add(AnswerDiagnostics(authored.Result.Comment)...)
	}
	out.Valid = out.Complete && len(out.Diagnostics) == 0
	if !out.Valid {
		return RunResult{}, out
	}
	encoded, err := json.Marshal(DraftsPayload{Drafts: drafts})
	if err != nil {
		out.Valid = false
		return RunResult{}, out
	}
	return RunResult{Answer: answer, Questions: questions, Drafts: encoded}, out
}

// ledgerDiagnostics checks ids unique per draft and at least one O- decision.
func ledgerDiagnostics(at string, r agentresult.Research) []agentresult.Diagnostic {
	var out []agentresult.Diagnostic
	seen := map[string]bool{}
	goal := false
	check := func(path, id string) {
		if seen[id] {
			out = append(out, diag(path, "duplicate_id", "ids unique within the draft", id, "Give every ledger item its own id."))
		}
		seen[id] = true
	}
	for i, f := range r.Findings {
		check(fmt.Sprintf("%s.findings[%d].id", at, i), f.ID)
	}
	for i, s := range r.Decisions {
		check(fmt.Sprintf("%s.decisions[%d].id", at, i), s.ID)
		if strings.HasPrefix(s.ID, "O-") {
			goal = true
		}
	}
	for i, s := range r.Assumptions {
		check(fmt.Sprintf("%s.assumptions[%d].id", at, i), s.ID)
	}
	if !goal {
		out = append(out, diag(at+".decisions", "goal", "at least one O- decision", "none", "State the draft's goal as an O- decision."))
	}
	return out
}

type issueContent struct {
	Summary  string
	Sources  []string
	Marker   string
	SearchOK bool
}

type statement struct{ ID, Text string }

// renderIssueBody renders one issue body from the embedded markdown template.
func renderIssueBody(c issueContent, d DraftInput, hits []IssueMatch, check string) (string, error) {
	var ledger agentresult.Research
	if err := json.Unmarshal(d.Ledger, &ledger); err != nil {
		return "", err
	}
	data := struct {
		Goal, NonGoals, MustNotChange, OpenQuestions []statement
		Size, Impact, Summary                        string
		RelatedIssues, PossibleDuplicates, Sources   []string
		DuplicateWarning, DuplicateCheck, Marker     string
	}{Size: d.Size, Impact: d.Impact, Summary: c.Summary, RelatedIssues: d.RelatedIssues, Sources: c.Sources, DuplicateWarning: d.DuplicateWarning, DuplicateCheck: check, Marker: c.Marker}
	for _, s := range ledger.Decisions {
		st := statement{s.ID, s.Text}
		switch {
		case strings.HasPrefix(s.ID, "N-"):
			data.NonGoals = append(data.NonGoals, st)
		case strings.HasPrefix(s.ID, "K-"):
			data.MustNotChange = append(data.MustNotChange, st)
		case strings.HasPrefix(s.ID, "A-"):
			data.OpenQuestions = append(data.OpenQuestions, st)
		default:
			data.Goal = append(data.Goal, st)
		}
	}
	for _, s := range ledger.Assumptions {
		data.OpenQuestions = append(data.OpenQuestions, statement{s.ID, s.Text})
	}
	for _, h := range hits {
		ref := h.Reference
		if h.Title != "" {
			ref += " " + h.Title
		}
		data.PossibleDuplicates = append(data.PossibleDuplicates, ref)
	}
	var b strings.Builder
	if err := issueBody.Execute(&b, data); err != nil {
		return "", err
	}
	return strings.TrimRight(b.String(), "\n") + "\n", nil
}

// draftSetInput turns a validated drafts result into the store's set input.
func draftSetInput(r RunResult, protect func(string) (string, error)) (*store.BrainstormDraftSetInput, error) {
	var p DraftsPayload
	if err := json.Unmarshal(r.Drafts, &p); err != nil {
		return nil, err
	}
	in := &store.BrainstormDraftSetInput{Summary: r.Answer}
	for _, d := range p.Drafts {
		out := store.BrainstormDraft{Key: d.Key, Size: d.Size, RelatedIssues: d.RelatedIssues}
		var err error
		if out.Title, err = protect(d.Title); err == nil {
			if out.Impact, err = protect(d.Impact); err == nil {
				if out.DuplicateWarning, err = protect(d.DuplicateWarning); err == nil {
					var ledger string
					if ledger, err = protect(string(d.Ledger)); err == nil {
						if !json.Valid([]byte(ledger)) {
							err = errors.New("protected ledger is not JSON")
						}
						out.Ledger = json.RawMessage(ledger)
					}
				}
			}
		}
		if err != nil {
			return nil, err
		}
		in.Drafts = append(in.Drafts, out)
	}
	return in, nil
}

// searchDuplicates runs the issue search once per draft title of a stored
// set and records the hits; an error is recorded as unavailable.
func (s *Service) searchDuplicates(ctx context.Context, scanID, repositoryRef string) error {
	set, err := s.db.BrainstormDraftSetForScan(ctx, scanID)
	if err != nil {
		return err
	}
	for _, d := range set.Drafts {
		if len(d.SearchHits) > 0 {
			continue
		}
		hits := json.RawMessage(`{"unavailable":"` + DuplicateCheckUnavailable + `"}`)
		if s.issues != nil {
			text := strings.Join(strings.Fields(d.Title), " ")
			for len(text) > MaxIssueSearchText || utf8.RuneCountInString(text) > MaxIssueSearchText {
				_, size := utf8.DecodeLastRuneInString(text)
				text = text[:len(text)-size]
			}
			if matches, e := s.issues.SearchIssues(ctx, repositoryRef, text, 10); e == nil {
				if matches == nil {
					matches = []IssueMatch{}
				}
				if raw, e := json.Marshal(struct {
					Matches []IssueMatch `json:"matches"`
				}{matches}); e == nil {
					hits = raw
				}
			}
		}
		if _, err = s.db.SetBrainstormDraftSearchHits(ctx, d.ID, hits); err != nil {
			return err
		}
	}
	return nil
}

func searchHits(raw json.RawMessage) ([]IssueMatch, string) {
	var h struct {
		Matches     []IssueMatch `json:"matches"`
		Unavailable string       `json:"unavailable"`
	}
	if len(raw) == 0 {
		return nil, DuplicateCheckUnavailable
	}
	if json.Unmarshal(raw, &h) != nil {
		return nil, DuplicateCheckUnavailable
	}
	return h.Matches, h.Unavailable
}

// Draft admits one drafts scan of the brainstorm, reusing the scan queue.
func (s *Service) Draft(ctx context.Context, p Principal, r DraftRequest) (ScanView, error) {
	if !validText(r.RequestKey, 128) || (r.Note != "" && !validText(r.Note, MaxQuestionBytes)) {
		return ScanView{}, ErrInvalid
	}
	o, b, err := s.brainstorm(ctx, p, r.BrainstormID)
	if err != nil {
		return ScanView{}, err
	}
	question := draftsQuestion
	if r.Note != "" {
		question = r.Note
	}
	digest := requestDigest(struct{ Purpose, Note string }{PurposeDrafts, r.Note})
	if old, err := s.db.BrainstormScanByRequest(ctx, o, b.ID, r.RequestKey); err == nil {
		if old.Digest != digest || old.Purpose != PurposeDrafts {
			return ScanView{}, ErrRequestReuse
		}
		v, err := s.scanView(p, old, "", MaxPageLimit)
		v.Replayed = true
		return v, err
	} else if !errors.Is(err, store.ErrNotFound) {
		return ScanView{}, err
	}
	if b.State != "open" {
		return ScanView{}, ErrClosed
	}
	if s.team == nil {
		return ScanView{}, ErrSkillMissing
	}
	team, err := s.team.Capture(ctx, b.RepositoryRef)
	if err != nil {
		return ScanView{}, err
	}
	if team.Version == "" {
		return ScanView{}, ErrSkillMissing
	}
	if _, ok := team.Files[draftsSkillPath]; !ok {
		return ScanView{}, ErrDraftsSkillMissing
	}
	files, err := json.Marshal(team)
	if err != nil {
		return ScanView{}, err
	}
	if err = s.checkConsent(ctx, p, b.RepositoryRef); err != nil {
		return ScanView{}, err
	}
	consent, _ := json.Marshal(p)
	sc, replayed, err := s.db.AdmitBrainstormScan(ctx, store.BrainstormScan{BrainstormOwner: o, ID: mintID("scan-"), BrainstormID: b.ID, RequestKey: r.RequestKey, Digest: digest, Question: question, Purpose: PurposeDrafts, TeamVersion: team.Version, TeamFiles: string(files), GrantID: p.GrantID, ConsentRevision: string(consent), JobID: mintID("brainstorm-scan-"), ExecutionID: mintID("execution-")}, s.now())
	if err != nil {
		return ScanView{}, storeError(err)
	}
	v, err := s.scanView(p, sc, "", MaxPageLimit)
	v.Replayed = replayed
	if err == nil && scanFinished(v.Scan.State) {
		v.NextActions = append(v.NextActions, "brainstorm_drafts_get")
	}
	return v, err
}

// DraftsGet returns the latest draft set of the brainstorm.
func (s *Service) DraftsGet(ctx context.Context, p Principal, brainstormID string) (DraftSetView, error) {
	_, b, err := s.brainstorm(ctx, p, brainstormID)
	if err != nil {
		return DraftSetView{}, err
	}
	set, err := s.db.LatestBrainstormDraftSet(ctx, b.ID)
	if errors.Is(err, store.ErrNotFound) {
		return DraftSetView{}, ErrNoDrafts
	}
	if err != nil {
		return DraftSetView{}, err
	}
	v := DraftSetView{BrainstormID: b.ID, DraftSetID: set.ID, Seq: set.Seq, State: set.State, Summary: set.Summary, Drafts: []DraftView{}}
	for _, d := range set.Drafts {
		v.Drafts = append(v.Drafts, DraftView{ID: d.ID, Key: d.Key, Title: d.Title, Size: d.Size, Impact: d.Impact, RelatedIssues: append([]string{}, d.RelatedIssues...), DuplicateWarning: d.DuplicateWarning, PossibleDuplicates: d.SearchHits, Ledger: d.Ledger})
	}
	return v, nil
}

// TaskBatchRepository names the repository of a batch the owner holds.
func (s *Service) TaskBatchRepository(ctx context.Context, p Principal, batchID string) (string, error) {
	o, err := ownerOf(p)
	if err != nil {
		return "", err
	}
	if !validText(batchID, 256) {
		return "", ErrNotFound
	}
	batch, err := s.db.ReadBrainstormTaskBatch(ctx, batchID)
	if err != nil {
		return "", storeError(err)
	}
	if batch.OwnerID != strconv.FormatInt(o.OwnerID, 10) || batch.OwnerGeneration != o.OwnerGeneration {
		return "", ErrNotFound
	}
	return batch.RepositoryRef, nil
}

// TasksGet returns per-draft state, issue number and a plain next step.
func (s *Service) TasksGet(ctx context.Context, p Principal, batchID string) (TasksView, error) {
	repository, err := s.TaskBatchRepository(ctx, p, batchID)
	if err != nil {
		return TasksView{}, err
	}
	if err = s.checkConsent(ctx, p, repository); err != nil {
		return TasksView{}, err
	}
	batch, err := s.db.ReadBrainstormTaskBatch(ctx, batchID)
	if err != nil {
		return TasksView{}, storeError(err)
	}
	items, err := s.db.BrainstormTaskItems(ctx, batch.ID)
	if err != nil {
		return TasksView{}, err
	}
	v := TasksView{BatchID: batch.ID, BrainstormID: batch.BrainstormID, DraftSetID: batch.DraftSetID, State: batch.State, Items: []TaskItemView{}}
	for _, it := range items {
		v.Items = append(v.Items, TaskItemView{DraftID: it.DraftID, Title: it.Title, State: it.State, IssueNumber: it.IssueNumber, IssueLink: it.IssueLink, Reason: it.Reason, NextStep: nextStep(it.State)})
	}
	return v, nil
}

func nextStep(state string) string {
	switch state {
	case store.BrainstormItemPending, store.BrainstormItemCreating:
		return "wait; the issue is being created"
	case store.BrainstormItemUncertain:
		return "wait; the factory is checking whether the issue exists, nothing was duplicated"
	case store.BrainstormItemUncertainExhausted:
		return "check the repository's issues; not found in search is not proof it was not created; propose brainstorm_task_recreate only if it is missing"
	case store.BrainstormItemNotCreated:
		return "propose brainstorm_create_tasks again to continue this batch"
	case store.BrainstormItemCreated, store.BrainstormItemPlanStarting:
		return "wait; Plan is starting"
	case store.BrainstormItemPlanStarted:
		return "none; Plan has started"
	case store.BrainstormItemPlanStartFailed:
		return "propose brainstorm_create_tasks again to retry starting Plan"
	}
	return "read the batch again"
}

// IssueDrafts renders and protects the issue of each kept draft of a set.
func (s *Service) IssueDrafts(ctx context.Context, p Principal, brainstormID, draftSetID string, keys []string, generation int) (store.BrainstormDraftSet, []IssueDraft, error) {
	_, b, err := s.brainstorm(ctx, p, brainstormID)
	if err != nil {
		return store.BrainstormDraftSet{}, nil, err
	}
	if generation < 1 || len(keys) == 0 || len(keys) > MaxDrafts {
		return store.BrainstormDraftSet{}, nil, ErrInvalid
	}
	set, err := s.db.ReadBrainstormDraftSet(ctx, draftSetID)
	if err != nil {
		return store.BrainstormDraftSet{}, nil, storeError(err)
	}
	if set.BrainstormID != b.ID {
		return store.BrainstormDraftSet{}, nil, ErrNotFound
	}
	entries, _, err := s.db.BrainstormScanContext(ctx, b.ID, "")
	if err != nil {
		return store.BrainstormDraftSet{}, nil, err
	}
	var sources []string
	seen := map[string]bool{}
	for _, e := range entries {
		if e.Kind == KindDocumentNote && e.DocumentName != "" && !seen[e.DocumentName] {
			seen[e.DocumentName] = true
			sources = append(sources, e.DocumentName)
		}
	}
	byKey := map[string]store.BrainstormDraft{}
	for _, d := range set.Drafts {
		byKey[d.Key] = d
	}
	var out []IssueDraft
	used := map[string]bool{}
	for _, key := range keys {
		d, ok := byKey[key]
		if !ok || used[key] {
			return set, nil, fmt.Errorf("%w: unknown or repeated draft %q", ErrInvalid, key)
		}
		used[key] = true
		hits, check := searchHits(d.SearchHits)
		marker := Marker(b.ID, d.ID, generation)
		body, err := renderIssueBody(issueContent{Summary: set.Summary, Sources: sources, Marker: marker}, DraftInput{Key: d.Key, Title: d.Title, Size: d.Size, Impact: d.Impact, RelatedIssues: d.RelatedIssues, DuplicateWarning: d.DuplicateWarning, Ledger: d.Ledger}, hits, check)
		if err != nil {
			return set, nil, err
		}
		if len(body) > MaxIssueBodyBytes {
			return set, nil, fmt.Errorf("%w: the issue body of draft %q is larger than 64 KiB", ErrInvalid, key)
		}
		title := d.Title
		if s.protect != nil {
			safeTitle, e := s.protect(ctx, title)
			if e != nil {
				return set, nil, e
			}
			safeBody, e := s.protect(ctx, body)
			if e != nil {
				return set, nil, e
			}
			if safeTitle != title || safeBody != body {
				return set, nil, ErrDraftsChanged
			}
		}
		sum := sha256.Sum256(d.Ledger)
		out = append(out, IssueDraft{DraftID: d.ID, Key: d.Key, Title: title, Body: body, Marker: marker, Generation: generation, Ledger: d.Ledger, LedgerDigest: hex.EncodeToString(sum[:])})
	}
	return set, out, nil
}
