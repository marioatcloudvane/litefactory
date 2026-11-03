package featuredraft

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/factory/owneractions"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// Heading keeps the issue body from beginning with a model-supplied command.
const Heading = "# Feature request"

// MarkerPrefix identifies a confirmed preview for forge reconciliation.
const MarkerPrefix = "<!-- litefactory:chat-issue:"

// Draft is the strict tool-free model response, with exclusive clarification.
type Draft struct {
	Title         string `json:"title"`
	Summary       string `json:"summary"`
	Problem       string `json:"problem"`
	Wanted        string `json:"wanted_outcome"`
	Clarification string `json:"needs_clarification,omitempty"`
}

// Decode rejects unknown fields and permits only one of draft or clarification.
func Decode(raw []byte) (Draft, error) {
	var d Draft
	if len(raw) > 16<<10 || !utf8.Valid(raw) || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return d, errors.New("invalid draft encoding")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&d); err != nil {
		return d, err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return d, errors.New("trailing draft data")
	}
	if d.Clarification != "" {
		if len(d.Clarification) > 300 || strings.ContainsAny(d.Clarification, "\r\n") || d.Title != "" || d.Summary != "" || d.Problem != "" || d.Wanted != "" {
			return d, errors.New("invalid clarification")
		}
		return d, nil
	}
	if strings.TrimSpace(d.Title) == "" || utf8.RuneCountInString(d.Title) > 120 || strings.ContainsAny(d.Title, "\r\n") {
		return d, errors.New("invalid draft title")
	}
	for _, s := range []string{d.Summary, d.Problem, d.Wanted} {
		if strings.TrimSpace(s) == "" || len(s) > 6000 {
			return d, errors.New("invalid draft section")
		}
	}
	return d, nil
}

// Sanitize removes command and factory marker lines before hashing or display.
func Sanitize(s string) string {
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "<!-- lf:") || strings.HasPrefix(trimmed, "<!-- litefactory:") || strings.HasPrefix(trimmed, "[lf:ask:") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// Hash binds confirmation to the exact protected title and body.
func Hash(title, body string) string {
	sum := sha256.Sum256([]byte(title + "\x00" + body))
	return hex.EncodeToString(sum[:])
}

// Marker returns the exact hidden line searched before every create attempt.
func Marker(p contracts.ChatIssuePreview) string { return MarkerPrefix + p.ID + " -->" }

// IssueBody appends only the fixed marker and timestamped chat origin footer.
func IssueBody(p contracts.ChatIssuePreview) string {
	return p.Body + "\n\n" + Marker(p) + "\nCreated from chat at " + p.CreatedAt.UTC().Format(time.RFC3339) + "."
}

// Render refuses an oversized full preview rather than shortening confirmed text.
func Render(p contracts.ChatIssuePreview, project string) (string, error) {
	text := "Project: " + project + "\nTitle: " + p.Title + "\n\n" + p.Body + "\n\nConfirm separately with /confirm code:" + p.Code + " within 24 hours."
	if len(utf16.Encode([]rune(text))) > 2000 {
		return "", errors.New("preview does not fit one message")
	}
	return text, nil
}

// PreviewStore is limited to preview state; the drafter cannot create issues.
type PreviewStore interface {
	CurrentChatIssuePreview(context.Context, string, string) (contracts.ChatIssuePreview, error)
	ChatIssuePreviewForMessage(context.Context, string, string) (contracts.ChatIssuePreview, error)
	SaveChatIssuePreview(context.Context, contracts.ChatIssuePreview) error
}

// Instructions is one captured and validated instruction selection.
type Instructions struct{ Main, Correction string }

// Turn is a delivered conversation excerpt, supplied as untrusted model context.
type Turn struct {
	Question, Project, Answer string
	At                        time.Time
	SourceID, Revision        string `json:",omitempty"`
}

// History contains permitted recent conversation context, newest first.
type History struct{ Turns []Turn }

// RepositoryReader exposes registered project identity and display metadata.
type RepositoryReader interface {
	ListRepos(context.Context) ([]contracts.Project, error)
}

// Model is an isolated, response-only generation capability.
type Model interface {
	Answer(context.Context, int64, string) ([]byte, error)
}

// ErrUnavailable marks a model without currently available isolated execution.
var ErrUnavailable = errors.New("draft model unavailable")

// Status is the certainty of this bounded draft operation.
type Status string

// Draft operation statuses do not confer execution authority.
const (
	Success         Status = "success"
	Refusal         Status = "refusal"
	ReportableError Status = "error"
	Uncertain       Status = "uncertain"
)

// Result is a protected draft response and its resolved repository attribution.
type Result struct {
	Text   string
	RepoID int64
	Status Status
}

// Service owns project selection, preview identity, expiry and durable writes.
// Dependencies supply content, isolated generation, protection and persistence;
// none decides whether a draft or a pending create may proceed.
type Service struct {
	Instructions func(context.Context) (Instructions, error)
	Repositories RepositoryReader
	History      func(context.Context, string) (History, error)
	Model        Model
	Redactor     *redact.Redactor
	Store        PreviewStore
	Now          func() time.Time
	Bind         func(context.Context, int64) error
	Protect      func(context.Context, string) (string, error)
}

// Available reports whether isolated generation and its required primitive
// dependencies have been supplied. It does not admit or execute a draft.
func (d *Service) Available() bool {
	return d != nil && d.Model != nil && d.Redactor != nil && d.Store != nil && d.Repositories != nil && d.Instructions != nil
}

// Reply uses bounded live history and a current preview, never an archive.
func (d *Service) Reply(ctx context.Context, transport, channel, message, project, note string, at time.Time) (Result, error) {
	if d.Redactor == nil || d.Store == nil || d.Repositories == nil || d.Instructions == nil {
		return Result{Text: "", RepoID: 0, Status: ReportableError}, errors.New("draft dependencies missing")
	}
	if old, err := d.Store.ChatIssuePreviewForMessage(ctx, transport, message); err == nil {
		if old.State == "previewed" && at.Before(old.ExpiresAt) {
			repos, e := d.Repositories.ListRepos(ctx)
			if e != nil {
				return Result{Text: "", RepoID: 0, Status: ReportableError}, e
			}
			for _, repo := range repos {
				if repo.ID == old.RepoID {
					text, e := Render(old, repo.Slug())
					return Result{Text: text, RepoID: repo.ID, Status: Success}, e
				}
			}
		}
		return Result{Text: "This draft has ended. Ask for a new /draft.", RepoID: old.RepoID, Status: Refusal}, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return Result{Text: "", RepoID: 0, Status: ReportableError}, err
	}
	history := History{}
	if d.History != nil {
		h, e := d.History(ctx, channel)
		if e == nil {
			history = h
		}
	}
	resolved, err := resolveProject(ctx, d.Repositories, project, history.Turns)
	if err != nil {
		return Result{Text: "", RepoID: 0, Status: ReportableError}, err
	}
	if resolved.Refusal != "" {
		return Result{Text: resolved.Refusal, RepoID: 0, Status: Refusal}, nil
	}
	repo := resolved.Repository
	if d.Bind != nil {
		if e := d.Bind(ctx, repo.ID); e != nil {
			return Result{Text: "", RepoID: 0, Status: ReportableError}, e
		}
	}
	current, err := d.Store.CurrentChatIssuePreview(ctx, transport, channel)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return Result{Text: "", RepoID: repo.ID, Status: ReportableError}, err
	}
	if err == nil && current.State == "creating" && (at.Before(current.ExpiresAt) || at.Before(current.LeaseUntil)) {
		return Result{Text: "Creation is still in progress for code " + current.Code + ". Please try /draft later.", RepoID: repo.ID, Status: Uncertain}, nil
	}
	if len(history.Turns) == 0 && strings.TrimSpace(note) == "" {
		return Result{Text: "Please describe the feature idea in /draft note.", RepoID: repo.ID, Status: Refusal}, nil
	}
	if d.Model == nil {
		return Result{Text: "Drafting unavailable right now.", RepoID: repo.ID, Status: ReportableError}, nil
	}
	type prior struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	var preview *prior
	if err == nil && current.RepoID == repo.ID && at.Before(current.ExpiresAt) {
		preview = &prior{current.Title, current.Body}
	}
	data := struct {
		Project string `json:"project"`
		History []Turn `json:"recent_ask_turns"`
		Note    string `json:"note"`
		Current *prior `json:"current_preview,omitempty"`
	}{repo.Slug(), history.Turns, note, preview}
	encoded, err := json.Marshal(data)
	if err != nil {
		return Result{Text: "", RepoID: repo.ID, Status: ReportableError}, err
	}
	instructions, err := d.Instructions(ctx)
	if err != nil {
		return Result{Text: "", RepoID: repo.ID, Status: ReportableError}, err
	}
	prompt := instructions.Main + "\nUNTRUSTED_CONTEXT_JSON\n" + string(encoded)
	for attempt := 0; attempt < 2; attempt++ {
		raw, e := d.Model.Answer(ctx, repo.ID, prompt)
		if errors.Is(e, ErrUnavailable) {
			return Result{Text: "Drafting unavailable right now.", RepoID: repo.ID, Status: ReportableError}, nil
		}
		if e != nil {
			return Result{Text: "", RepoID: repo.ID, Status: ReportableError}, e
		}
		draft, e := Decode(raw)
		if e == nil && draft.Clarification != "" {
			return Result{Text: "Please clarify: " + draft.Clarification, RepoID: repo.ID, Status: Refusal}, nil
		}
		var p contracts.ChatIssuePreview
		if e == nil {
			p, e = d.preview(ctx, draft, transport, channel, message, repo.ID, at)
		}
		var text string
		if e == nil {
			text, e = Render(p, repo.Slug())
		}
		if e == nil {
			if e = d.Store.SaveChatIssuePreview(ctx, p); e != nil {
				return Result{Text: "", RepoID: repo.ID, Status: ReportableError}, e
			}
			return Result{Text: text, RepoID: repo.ID, Status: Success}, nil
		}
		prompt += "\n" + instructions.Correction
	}
	return Result{Text: "Could not build a clear draft. Please describe the idea more briefly.", RepoID: repo.ID, Status: Refusal}, nil
}

func (d *Service) preview(ctx context.Context, draft Draft, transport, channel, message string, repoID int64, at time.Time) (contracts.ChatIssuePreview, error) {
	p := contracts.ChatIssuePreview{Transport: transport, ChannelRef: channel, SourceMessageID: message, RepoID: repoID, CreatedAt: at, ExpiresAt: at.Add(24 * time.Hour)}
	summary, problem, wanted := Sanitize(draft.Summary), Sanitize(draft.Problem), Sanitize(draft.Wanted)
	if summary == "" || problem == "" || wanted == "" {
		return p, errors.New("empty protected section")
	}
	title, err := d.Redactor.Apply(ctx, Sanitize(draft.Title), 64<<10)
	if err != nil {
		return p, err
	}
	body, err := d.Redactor.Apply(ctx, fmt.Sprintf("%s\n\n## Summary\n%s\n\n## Problem\n%s\n\n## Wanted outcome\n%s\n\n## Origin", Heading, summary, problem, wanted), 64<<10)
	if err != nil {
		return p, err
	}
	p.Title, p.Body = title.String(), body.String()
	if d.Protect != nil {
		p.Title, err = d.Protect(ctx, p.Title)
		if err != nil {
			return p, err
		}
		p.Body, err = d.Protect(ctx, p.Body)
		if err != nil {
			return p, err
		}
	}
	if p.Title == "" || utf8.RuneCountInString(p.Title) > 120 || utf8.RuneCountInString(p.Body) > 1500 {
		return p, errors.New("protected draft too large")
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return p, err
	}
	p.ID = hex.EncodeToString(id)
	p.ContentHash = Hash(p.Title, p.Body)
	code := sha256.Sum256([]byte(p.ID + p.ContentHash))
	p.Code = hex.EncodeToString(code[:4])
	if d.Now != nil {
		p.CreatedAt = d.Now()
		p.ExpiresAt = p.CreatedAt.Add(24 * time.Hour)
	}
	return p, nil
}

// resolveProject applies the shared exact, inherited and short-name selection
// rules using only registered projects and supplied conversation excerpts.
func resolveProject(ctx context.Context, reader RepositoryReader, option string, history []Turn) (owneractions.Resolution, error) {
	repos, err := reader.ListRepos(ctx)
	if err != nil {
		return owneractions.Resolution{}, err
	}
	refs := make([]owneractions.Repository, len(repos))
	for i, repo := range repos {
		refs[i] = owneractions.Repository{ID: repo.ID, Forge: repo.Forge, Owner: repo.Owner, Name: repo.Name}
	}
	inherited := make([]string, len(history))
	for i, turn := range history {
		inherited[i] = turn.Project
	}
	return owneractions.SelectProject(refs, option, inherited), nil
}
