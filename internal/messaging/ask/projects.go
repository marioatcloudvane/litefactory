package ask

import (
	"context"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/factory/owneractions"
)

// Turn is a permitted projection of a delivered local Ask reply. The question
// is the stored legacy excerpt, never a reconstructed original question.
type Turn struct {
	Question, Project, Answer string
	At                        time.Time
	SourceID, Revision        string `json:",omitempty"`
}

// RepositoryReader exposes only the registered project catalog.
type RepositoryReader interface {
	ListRepos(context.Context) ([]contracts.Project, error)
}

// Resolution contains either one repository or a short catalog reply.
type Resolution struct {
	Repository contracts.Project
	Inherited  bool
	Refusal    string
}

// ResolveProject retains the Ask contract while the factory owns project selection.
func ResolveProject(ctx context.Context, reader RepositoryReader, option string, history []Turn) (Resolution, error) {
	if reader == nil {
		return Resolution{}, errors.New("ask: repository reader is missing")
	}
	repos, err := reader.ListRepos(ctx)
	if err != nil {
		return Resolution{}, err
	}
	refs := make([]owneractions.Repository, len(repos))
	for i, repo := range repos {
		refs[i] = owneractions.Repository{ID: repo.ID, Forge: repo.Forge, Owner: repo.Owner, Name: repo.Name}
	}
	inherited := make([]string, len(history))
	for i, turn := range history {
		inherited[i] = turn.Project
	}
	selected := owneractions.SelectProject(refs, option, inherited)
	out := Resolution{Inherited: selected.Inherited, Refusal: selected.Refusal}
	for _, repo := range repos {
		if repo.ID == selected.Repository.ID {
			out.Repository = repo
			break
		}
	}
	return out, nil
}
