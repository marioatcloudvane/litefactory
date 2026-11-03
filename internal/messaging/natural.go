package messaging

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/factory/owneractions"
	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/owneraction"
)

type naturalPayload struct {
	Turns                                      []DialogueTurn `json:"turns,omitempty"`
	Invocation                                 Invocation     `json:"invocation"`
	Prompt                                     string         `json:"prompt,omitempty"`
	PreviewID, PreviewHash, Question, Revision string
	RepoID                                     int64
	Project, Channel, Transport                string
	ProjectPending                             bool `json:"project_pending,omitempty"`
	// focus is the conversation's last discussed project. It only breaks ties
	// and fills a missing project for reads; it is never part of the digest.
	focus         string
	freezeTargets bool
	// window is the owner's own window phrase, echoed in the proposal text; the
	// digest binds the exact bounds it resolved to.
	window string
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

// naturalYes is the small set of whole messages that mean yes; a message that
// says anything more is an ordinary message.
func naturalYes(text string) bool {
	switch strings.Trim(strings.ToLower(strings.TrimSpace(text)), "!. ") {
	case "yes", "yes please", "ja", "ok", "okay", "go", "go ahead", "do it", "start", "mach", "mach das", "confirm":
		return true
	}
	return false
}

// naturalDetailsRequest asks to see the exact proposal.
func naturalDetailsRequest(text string) bool {
	switch strings.Trim(strings.ToLower(strings.TrimSpace(text)), "!?. ") {
	case "details", "show details", "show me the details", "exact details":
		return true
	}
	return false
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

// naturalFreshConversation is the owner's whole-message command to start a
// fresh native conversation.
func naturalFreshConversation(text string) bool {
	return strings.Trim(strings.ToLower(strings.TrimSpace(text)), "`\"!. ") == "new conversation"
}

func (s *Service) resolveNatural(ctx context.Context, p *naturalPayload, at time.Time) (string, error) {
	if p.Invocation.Command == "draft" || p.Invocation.Command == "confirm" || p.Invocation.Command == "answer" {
		selection := actionSelection(*p, s.transport)
		refusal, err := (selectionDomains{Service: s}).actionDomain().Normalize(ctx, &selection, at)
		p.RepoID, p.Project, p.ProjectPending = selection.RepoID, selection.Project, selection.ProjectPending
		p.PreviewID, p.PreviewHash, p.Question, p.Revision = selection.PreviewID, selection.PreviewHash, selection.Question, selection.Revision
		return refusal, err
	}

	if standardsCommand(p.Invocation.Command) {
		selection := owneractions.StandardsSelection{Invocation: owneraction.Invocation{Command: p.Invocation.Command, Arguments: p.Invocation.Arguments}, RepoID: p.RepoID, Project: p.Project, Window: p.window}
		refusal, err := (selectionDomains{Service: s}).standardsDomain().Normalize(ctx, &selection, p.freezeTargets, at)
		p.RepoID, p.Project, p.window = selection.RepoID, selection.Project, selection.Window
		if errors.Is(err, owneractions.ErrInvalidSelection) {
			err = ErrInvalidInvocation
		}
		return refusal, err
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
	if p.Invocation.Command == "ask" || p.Invocation.Command == "diagnose" || p.Invocation.Command == "question" || (p.Invocation.Command == "meter" || p.Invocation.Command == "status") && (args["task"] != "" || args["issue"] != "") || args["project"] != "" {
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
		if err = s.factory.AuthorizeProject(ctx, resolution.Repository.ID, resolution.Repository.Forge+":"+resolution.Repository.Slug()); err != nil {
			return "", err
		}
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
	return "", nil
}

func relativeBound(phrase string, now time.Time) (string, string, bool) {
	return owneractions.RelativeBound(phrase, now)
}

func analysisWindow(phrase string, now time.Time) (string, string, bool) {
	return owneractions.AnalysisWindow(phrase, now)
}
func (s *Service) recheckNatural(ctx context.Context, p naturalPayload, at time.Time) (string, error) {
	if p.Invocation.Command == "draft" || p.Invocation.Command == "confirm" || p.Invocation.Command == "answer" {
		return (selectionDomains{Service: s}).actionDomain().RecheckSelection(ctx, actionSelection(p, s.transport), at)
	}

	if standardsCommand(p.Invocation.Command) {
		return (selectionDomains{Service: s}).standardsDomain().RecheckSelection(ctx, owneractions.StandardsSelection{Invocation: owneraction.Invocation{Command: p.Invocation.Command, Arguments: p.Invocation.Arguments}, RepoID: p.RepoID, Project: p.Project})
	}

	_, _, err := s.ValidateInvocation(p.Invocation)
	if err != nil {
		return "This action is no longer enabled; request a fresh proposal.", nil //nolint:nilerr // A stale catalogue entry is a saved refusal, not retryable execution work.
	}
	if p.RepoID > 0 {
		resolution, e := ask.ResolveProject(ctx, projectCatalog{s.factory}, p.Project, nil)
		if e != nil {
			return "", e
		}
		if resolution.Refusal != "" || resolution.Repository.ID != p.RepoID {
			return "The project changed; request a fresh proposal.", nil
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
	selected, err := s.factory.Targets.ResolveProject(ctx, args, focus)
	out := ask.Resolution{Inherited: selected.Inherited, Refusal: selected.Refusal}
	if err == nil && selected.Refusal == "" {
		out.Repository, err = s.factory.Project(ctx, selected.Repository.ID)
	}
	return out, err
}

// knownNumericTask retains the legacy row bridge for existing presentation callers.
func (s *Service) knownNumericTask(ctx context.Context, repo contracts.Project, number int) (owneractions.Task, error) {
	task, err := s.factory.Targets.NumericTask(ctx, repo.ID, number)
	if err != nil {
		return owneractions.Task{}, err
	}
	return task, nil
}

func naturalTransport(p naturalPayload, fallback string) string {
	if p.Transport != "" {
		return p.Transport
	}
	return fallback
}
