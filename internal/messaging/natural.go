package messaging

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/store"
)

type naturalPayload struct {
	Turns                                      []DialogueTurn `json:"turns,omitempty"`
	Invocation                                 Invocation     `json:"invocation"`
	Prompt                                     string         `json:"prompt,omitempty"`
	PreviewID, PreviewHash, Question, Revision string
	RepoID                                     int64
	Project, Channel                           string
	ProjectPending                             bool `json:"project_pending,omitempty"`
	// focus is the conversation's last discussed project. It only breaks ties
	// and fills a missing project for reads; it is never part of the digest.
	focus string
}

func naturalDigest(p naturalPayload) (string, string, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return "", "", err
	}
	h := sha256.Sum256(raw)
	return string(raw), hex.EncodeToString(h[:]), nil
}
func naturalLater(id, previous string) bool {
	n, e := strconv.ParseUint(id, 10, 64)
	p, f := strconv.ParseUint(previous, 10, 64)
	return e == nil && f == nil && n > p
}
func naturalConfirmation(text string, d store.NaturalDialogue, reply string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if d.Code != "" && (text == "confirm "+strings.ToLower(d.Code) || naturalConfirmationCode(text) == strings.ToLower(d.Code)) {
		return true
	}
	return reply == d.PromptID && d.PromptID != "" && (text == "yes" || text == "yes please" || text == "confirm" || text == "do it")
}

// naturalConfirmationCode recognizes only a whole proposal-code message.
func naturalConfirmationCode(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	for _, prefix := range []string{"confirm ", "yes ", "ok "} {
		if strings.HasPrefix(text, prefix) {
			text = strings.TrimPrefix(text, prefix)
			break
		}
	}
	if len(text) != 16 {
		return ""
	}
	if _, err := hex.DecodeString(text); err != nil {
		return ""
	}
	return text
}

func naturalCancel(text string) bool {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "cancel", "no", "never mind", "nevermind":
		return true
	}
	return false
}

func (s *Service) resolveNatural(ctx context.Context, p *naturalPayload, at time.Time) (string, error) {
	if standardsCommand(p.Invocation.Command) {
		op, err := standardsOperation(p.Invocation)
		if err != nil {
			return err.Error(), nil //nolint:nilerr // Invalid selections are owner-visible clarifications, not retryable failures.
		}
		if s.standards == nil {
			return "Standards operations are unavailable.", nil
		}
		args := p.Invocation.Arguments
		if args["project"] != "" && args["repository_id"] == "" {
			resolution, e := s.resolveNaturalProject(ctx, args)
			if e != nil {
				return "", e
			}
			if resolution.Refusal != "" {
				return resolution.Refusal, nil
			}
			p.RepoID = resolution.Repository.ID
			p.Project = resolution.Repository.Slug()
			args["project"] = p.Project
			args["repository_id"] = strconv.FormatInt(p.RepoID, 10)
		}
		if strings.HasPrefix(op, "schedule.") && args["interval_seconds"] != "" {
			args["interval_seconds"] = standardsInterval(args["interval_seconds"])
		}
		if p.Invocation.Command != "standards" {
			return standardsMissingTerms(op, p.Invocation.Arguments), nil
		}
		return "", nil
	}
	args := p.Invocation.Arguments
	if p.Invocation.Command == "meter" {
		now := at.In(time.Local)
		if from, to, ok := relativeBound(args["from"], now); ok {
			args["from"] = from
			if to != "" && args["to"] == "" {
				args["to"] = to
			}
		}
		if to, _, ok := relativeBound(args["to"], now); ok {
			args["to"] = to
		}
	}
	if p.Invocation.Command == "ask" || p.Invocation.Command == "draft" || p.Invocation.Command == "diagnose" || p.Invocation.Command == "question" || p.Invocation.Command == "answer" || (p.Invocation.Command == "meter" || p.Invocation.Command == "status") && (args["task"] != "" || args["issue"] != "") || args["project"] != "" {
		resolution, err := s.resolveNaturalProjectFocus(ctx, args, p.focus)
		if err != nil {
			return "", err
		}
		if resolution.Refusal != "" {
			p.ProjectPending = true
			return resolution.Refusal, nil
		}
		args["project"] = resolution.Repository.Slug()
		p.Project = args["project"]
		p.RepoID = resolution.Repository.ID
	}
	for _, field := range []string{"note", "text", "question"} {
		if args[field] != "" && s.Protect != nil {
			v, err := s.Protect(ctx, args[field])
			if err != nil {
				return "", err
			}
			args[field] = v
		}
	}
	switch p.Invocation.Command {
	case "answer":
		ref, refusal := s.researchTarget(ctx, Message{Arguments: args}, true)
		if refusal != "" {
			return refusal, nil
		}
		if s.research == nil {
			return "No research answer is available here.", nil
		}
		q, err := s.research.Current(ctx, ref)
		if err != nil {
			return "", err
		}
		if q.Text == "" || q.Token != args["revision"] {
			return "Please provide the current research question revision from /question.", nil
		}
		p.Question, p.Revision = q.Text, q.Token
	case "confirm":
		preview, err := s.db.ChatIssuePreviewByCode(ctx, s.transport, p.Channel, args["code"])
		if errors.Is(err, store.ErrNotFound) {
			return "Please provide the current preview code from /draft.", nil
		}
		if err != nil {
			return "", err
		}
		if preview.State != "previewed" || !at.Before(preview.ExpiresAt) {
			return "The saved preview is no longer available; create a new draft.", nil
		}
		repos, err := s.db.ListRepos(ctx)
		if err != nil {
			return "", err
		}
		for _, repo := range repos {
			if repo.ID == preview.RepoID {
				p.Project = repo.Slug()
			}
		}
		if p.Project == "" {
			return "The preview project is no longer registered.", nil
		}
		p.RepoID = preview.RepoID
		p.PreviewID = preview.ID
		p.PreviewHash = preview.ContentHash
		p.Question = preview.Title + "\n" + preview.Body
	}
	return "", nil
}

// relativeBound turns a spanned relative time phrase into RFC3339 local-midnight
// bounds of now's location. A closed period without "since" also returns its end;
// unrecognised text is not ok and stays for meterview to reject.
func relativeBound(phrase string, now time.Time) (string, string, bool) {
	phrase = strings.ToLower(strings.TrimSpace(phrase))
	since := strings.HasPrefix(phrase, "since ")
	phrase = strings.TrimSpace(strings.TrimPrefix(phrase, "since "))
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	week := day.AddDate(0, 0, -(int(day.Weekday())+6)%7)
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	var from, to time.Time
	switch phrase {
	case "today":
		from = day
	case "yesterday":
		from, to = day.AddDate(0, 0, -1), day
	case "this week":
		from = week
	case "last week":
		from, to = week.AddDate(0, 0, -7), week
	case "this month":
		from = month
	case "last month":
		from, to = month.AddDate(0, -1, 0), month
	default:
		found := false
		for d := time.Sunday; d <= time.Saturday; d++ {
			if phrase == strings.ToLower(d.String()) {
				from, found = day.AddDate(0, 0, -(int(day.Weekday())-int(d)+7)%7), true
			}
		}
		if !found {
			return "", "", false
		}
	}
	if since || to.IsZero() {
		return from.Format(time.RFC3339), "", true
	}
	return from.Format(time.RFC3339), to.Format(time.RFC3339), true
}

func (s *Service) naturalProposal(p naturalPayload, code string) string {
	args, err := json.Marshal(p.Invocation.Arguments)
	if err != nil {
		return ""
	}
	scope := ""
	switch p.Invocation.Command {
	case "draft":
		scope = "Create or replace a feature preview only. Issue creation needs a separate confirmation."
	case "diagnose":
		scope = "Queue diagnosis work. Any repair remains subject to review on the issue."
	case "answer":
		scope = "Record this answer to the current research question: " + p.Question + "\nRevision: " + p.Revision
	case "confirm":
		scope = "Create an issue from this exact saved feature preview: " + p.Question
	}
	return fmt.Sprintf("Proposed action: %s\nProject: %s\n%s\nExact protected arguments: %s\nConfirmation code: %s\nReply directly to this message with yes, or send %s alone or prefixed by confirm, yes or ok. Send cancel to cancel. Expires in 15 minutes.", p.Invocation.Command, p.Project, scope, args, code, code)
}
func (s *Service) recheckNatural(ctx context.Context, p naturalPayload, at time.Time) (string, error) {
	if standardsCommand(p.Invocation.Command) {
		if s.standards == nil {
			return "Standards operations are unavailable.", nil
		}
		if _, err := standardsOperation(p.Invocation); err != nil {
			return err.Error(), nil //nolint:nilerr // A retired selection is a saved refusal, not retryable work.
		}
		if p.RepoID > 0 {
			resolution, e := ask.ResolveProject(ctx, s.db, p.Project, nil)
			if e != nil {
				return "", e
			}
			if resolution.Refusal != "" || resolution.Repository.ID != p.RepoID {
				return "The registered standards project changed. Request a fresh proposal.", nil
			}
		}
		for _, value := range p.Invocation.Arguments {
			protected, err := s.Protect(ctx, value)
			if err != nil {
				return "", err
			}
			if protected != value {
				return "Protection changed the exact standards request. Request a fresh proposal.", nil
			}
		}
		return "", nil
	}
	_, _, err := s.ValidateInvocation(p.Invocation)
	if err != nil {
		return "This action is no longer enabled; request a fresh proposal.", nil //nolint:nilerr // A stale catalogue entry is a saved refusal, not retryable execution work.
	}
	if p.RepoID > 0 {
		resolution, e := ask.ResolveProject(ctx, s.db, p.Project, nil)
		if e != nil {
			return "", e
		}
		if resolution.Refusal != "" || resolution.Repository.ID != p.RepoID {
			return "The project changed; request a fresh proposal.", nil
		}
	}
	if p.Invocation.Command == "answer" {
		ref, refusal := s.researchTarget(ctx, Message{Arguments: p.Invocation.Arguments}, true)
		if refusal != "" {
			return refusal, nil
		}
		if s.research == nil {
			return "Research answers are unavailable.", nil
		}
		q, e := s.research.Current(ctx, ref)
		if e != nil {
			return "", e
		}
		if q.Token != p.Revision || q.Text != p.Question {
			return "The research question changed; request a fresh proposal.", nil
		}
	}
	if p.Invocation.Command == "confirm" {
		preview, e := s.db.ChatIssuePreviewByCode(ctx, s.transport, p.Channel, p.Invocation.Arguments["code"])
		if errors.Is(e, store.ErrNotFound) {
			return "The preview changed; request a fresh proposal.", nil
		}
		if e != nil {
			return "", e
		}
		if preview.ID != p.PreviewID || preview.ContentHash != p.PreviewHash || preview.Title+"\n"+preview.Body != p.Question || preview.RepoID != p.RepoID || preview.State != "previewed" || !at.Before(preview.ExpiresAt) {
			return "The preview changed; request a fresh proposal.", nil
		}
	}
	if s.Protect != nil {
		for _, field := range []string{"note", "text", "question"} {
			if value := p.Invocation.Arguments[field]; value != "" {
				protected, e := s.Protect(ctx, value)
				if e != nil {
					return "", e
				}
				if protected != value {
					return "Protection changed; request a fresh proposal.", nil
				}
			}
		}
	}
	return "", nil
}

// Explicit project names win. Task inference requires complete reads across all
// registered projects, so an unavailable project can never create uniqueness.
func (s *Service) resolveNaturalProject(ctx context.Context, args map[string]string) (ask.Resolution, error) {
	return s.resolveNaturalProjectFocus(ctx, args, "")
}

// resolveNaturalProjectFocus also takes the conversation's last discussed
// project: it picks among several projects holding the same task number, and
// stands in when the number is unknown everywhere or no project is named.
func (s *Service) resolveNaturalProjectFocus(ctx context.Context, args map[string]string, focus string) (ask.Resolution, error) {
	option := strings.TrimRight(strings.TrimSpace(args["project"]), "!?.,;: ")
	resolution, err := ask.ResolveProject(ctx, s.db, option, nil)
	if err != nil || option != "" {
		return resolution, err
	}
	issue, _ := strconv.Atoi(args["issue"])
	if issue == 0 {
		issue, _ = strconv.Atoi(args["task"])
	}
	if issue == 0 {
		targets := ask.ExplicitIssues(args["question"])
		if len(targets) == 1 {
			issue = targets[0]
		}
	}
	repos, err := s.db.ListRepos(ctx)
	if err != nil {
		return resolution, err
	}
	var focused *store.Repo
	for i := range repos {
		if focus != "" && repos[i].Forge == "github" && repos[i].Slug() == focus {
			focused = &repos[i]
		}
	}
	if issue > 0 {
		var exact []store.Repo
		var sessions []store.Session
		for _, repo := range repos {
			if repo.Forge != "github" {
				continue
			}
			session, e := s.knownNumericTask(ctx, repo, issue)
			if e == nil {
				exact = append(exact, repo)
				sessions = append(sessions, session)
			} else if !errors.Is(e, store.ErrNotFound) {
				return ask.Resolution{Refusal: "Task lookup failed; project could not be inferred."}, e
			}
		}
		switch len(exact) {
		case 1:
			return ask.Resolution{Repository: exact[0]}, nil
		case 0:
			if focused != nil {
				return ask.Resolution{Repository: *focused}, nil
			}
			return ask.Resolution{Refusal: "No known task matches that number. Please name a project and task."}, nil
		default:
			if focused != nil {
				for _, repo := range exact {
					if repo.ID == focused.ID {
						return ask.Resolution{Repository: repo}, nil
					}
				}
			}
			names := make([]string, 0, len(exact))
			for i, repo := range exact {
				name := fmt.Sprintf("%s #%d", repo.Slug(), issue)
				if title := strings.TrimSpace(strings.ReplaceAll(sessions[i].FeatureSlug, "-", " ")); title != "" {
					name += " (" + title + ")"
				}
				names = append(names, name)
			}
			return ask.Resolution{Refusal: "Task " + strconv.Itoa(issue) + " exists in more than one project: " + strings.Join(names, " and ") + ". Which one do you mean?"}, nil
		}
	}
	if resolution.Refusal == "" {
		return resolution, nil
	}
	if focused != nil {
		return ask.Resolution{Repository: *focused}, nil
	}
	var active []store.Repo
	for _, repo := range repos {
		if repo.Forge != "github" {
			continue
		}
		sessions, _, e := s.db.ListTaskSessionsByRepo(ctx, repo.ID, 1)
		if e != nil {
			return ask.Resolution{Refusal: "Task lookup failed; project could not be inferred."}, e
		}
		if len(sessions) > 0 {
			active = append(active, repo)
		}
	}
	if len(active) == 1 {
		return ask.Resolution{Repository: active[0]}, nil
	}
	return resolution, nil
}

// Numeric GitHub references cannot select a Linear task's internal numeric alias.
func (s *Service) knownNumericTask(ctx context.Context, repo store.Repo, number int) (store.Session, error) {
	session, err := s.db.GetSessionByTaskRef(ctx, store.TaskRef(repo.Forge, repo.Owner, repo.Name, number))
	if err != nil {
		return store.Session{}, err
	}
	binding, err := s.db.GetSessionTicket(ctx, session.ID)
	if errors.Is(err, store.ErrNotFound) {
		return session, nil
	}
	if err != nil {
		return store.Session{}, err
	}
	if string(binding.Locator.Provider) != "github" || binding.Locator.Container != repo.Slug() || binding.Locator.Key != strconv.Itoa(number) {
		return store.Session{}, store.ErrNotFound
	}
	return session, nil
}
