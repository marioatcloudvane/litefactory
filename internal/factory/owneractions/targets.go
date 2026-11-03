package owneractions

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/store"
)

// Targets owns registered-project and numeric task selection over retained bindings.
type Targets struct{ db *store.DB }

// NewTargets binds the trusted persistence adapter for target selection.
func NewTargets(db *store.DB) *Targets { return &Targets{db: db} }
func repository(r store.Repo) Repository {
	return Repository{ID: r.ID, Forge: r.Forge, Owner: r.Owner, Name: r.Name}
}
func (t *Targets) selectProject(ctx context.Context, option string) (Resolution, error) {
	repos, err := t.db.ListOwnerActionResolutionRepositories(ctx)
	if err != nil {
		return Resolution{}, err
	}
	refs := make([]Repository, len(repos))
	for i, r := range repos {
		refs[i] = repository(r)
	}
	return SelectProject(refs, option, nil), nil
}

// Task identifies the exact saved task and revision required for an effect.
type Task struct {
	ID            int64
	Ref, Revision string
}

// NumericTask resolves a numeric GitHub task without selecting another provider alias.
func (t *Targets) NumericTask(ctx context.Context, repoID int64, number int) (Task, error) {
	repo, err := t.db.GetRepo(ctx, repoID)
	if err != nil {
		return Task{}, err
	}
	session, err := t.knownNumericTask(ctx, repo, number)
	return Task{ID: session.ID, Ref: session.TaskRef, Revision: store.OwnerActionSessionRevision(session)}, err
}

// CheckResolution checks repository coverage before target disclosure.
func (t *Targets) CheckResolution(ctx context.Context, repo Repository) error {
	return t.db.CheckOwnerActionResolution(ctx, repo.ID, repo.Forge+":"+repo.Slug())
}

// ErrProjectChanged preserves the existing changed-project refusal.
var ErrProjectChanged = errors.New("The project changed; request a fresh proposal.") //nolint:staticcheck // Preserve the existing owner-visible refusal exactly.

// RecheckProject verifies that the saved project still identifies its registered repository.
func (t *Targets) RecheckProject(ctx context.Context, project string, repoID int64) error {
	repos, err := t.db.ListRepos(ctx)
	if err != nil {
		return err
	}
	refs := make([]Repository, len(repos))
	for i, r := range repos {
		refs[i] = repository(r)
	}
	selected := SelectProject(refs, project, nil)
	if selected.Refusal != "" || selected.Repository.ID != repoID {
		return ErrProjectChanged
	}
	return nil
}

var issueReference = regexp.MustCompile(`(?i)(?:\b(?:issue|bug)\s*#?\s*|#)([1-9][0-9]*)\b`)

// ExplicitIssues parses distinct qualified issue mentions in encounter order.
func ExplicitIssues(text string) []int {
	var out []int
	for _, m := range issueReference.FindAllStringSubmatch(text, -1) {
		n, err := strconv.Atoi(m[1])
		if err == nil && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

// ResolveProject resolves an explicit project or infers it from qualified saved tasks.
func (t *Targets) ResolveProject(ctx context.Context, args map[string]string, focus string) (Resolution, error) {
	option := strings.TrimRight(strings.TrimSpace(args["project"]), "!?.,;: ")
	resolution, err := t.selectProject(ctx, option)
	if err != nil || option != "" {
		return resolution, err
	}
	issue, _ := strconv.Atoi(args["issue"])
	if issue == 0 {
		issue, _ = strconv.Atoi(args["task"])
	}
	if issue == 0 {
		targets := ExplicitIssues(args["question"])
		if len(targets) == 1 {
			issue = targets[0]
		}
	}
	repos, err := t.db.ListOwnerActionResolutionRepositories(ctx)
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
			session, e := t.knownNumericTask(ctx, repo, issue)
			if e == nil {
				exact = append(exact, repo)
				sessions = append(sessions, session)
			} else if !errors.Is(e, store.ErrNotFound) {
				return Resolution{Refusal: "Task lookup failed; project could not be inferred."}, e
			}
		}
		switch len(exact) {
		case 1:
			return Resolution{Repository: repository(exact[0])}, nil
		case 0:
			if focused != nil {
				return Resolution{Repository: repository(*focused)}, nil
			}
			return Resolution{Refusal: "No known task matches that number. Please name a project and task."}, nil
		default:
			if focused != nil {
				for _, repo := range exact {
					if repo.ID == focused.ID {
						return Resolution{Repository: repository(repo)}, nil
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
			return Resolution{Refusal: "Task " + strconv.Itoa(issue) + " exists in more than one project: " + strings.Join(names, " and ") + ". Which one do you mean?"}, nil
		}
	}
	if resolution.Refusal == "" {
		return resolution, nil
	}
	if focused != nil {
		return Resolution{Repository: repository(*focused)}, nil
	}
	var active []store.Repo
	for _, repo := range repos {
		if repo.Forge != "github" {
			continue
		}
		sessions, _, e := t.db.ListTaskSessionsByRepo(ctx, repo.ID, 1)
		if e != nil {
			return Resolution{Refusal: "Task lookup failed; project could not be inferred."}, e
		}
		if len(sessions) > 0 {
			active = append(active, repo)
		}
	}
	if len(active) == 1 {
		return Resolution{Repository: repository(active[0])}, nil
	}
	return resolution, nil
}

// Numeric GitHub references cannot select a Linear task's internal numeric alias.
func (t *Targets) knownNumericTask(ctx context.Context, repo store.Repo, number int) (store.Session, error) {
	session, err := t.db.GetSessionByTaskRef(ctx, store.TaskRef(repo.Forge, repo.Owner, repo.Name, number))
	if err != nil {
		return store.Session{}, err
	}
	binding, err := t.db.GetSessionTicket(ctx, session.ID)
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
