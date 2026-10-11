package ask

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// Turn is ephemeral conversation context, read from the transport, never saved.
type Turn struct {
	Question, Project, Answer string
	At                        time.Time
}

// RepositoryReader exposes only the registered project catalog.
type RepositoryReader interface {
	ListRepos(context.Context) ([]store.Repo, error)
}

// Resolution contains either one repository or a short catalog reply.
type Resolution struct {
	Repository store.Repo
	Inherited  bool
	Refusal    string
}

// ResolveProject matches exact owner/name or a unique name. History must be
// newest first and already filtered by the transport to its own ask turns.
// The newest turn alone determines the inherited project, even if it no longer
// exists; falling back to an older turn would silently change the target.
// With no option and no history, the only registered project is the default.
func ResolveProject(ctx context.Context, reader RepositoryReader, option string, history []Turn) (Resolution, error) {
	if reader == nil {
		return Resolution{}, errors.New("ask: repository reader is missing")
	}
	repos, err := reader.ListRepos(ctx)
	if err != nil {
		return Resolution{}, err
	}
	option = strings.TrimSpace(option)
	inherited := option == "" && len(history) > 0
	if inherited {
		option = history[0].Project
	}
	var matches []store.Repo
	for _, repo := range repos {
		// With nothing named or inherited, the only registered project is the default.
		if repo.Forge == "github" && (option == "" && len(history) == 0 || option != "" && (option == repo.Slug() || (!strings.Contains(option, "/") && option == repo.Name))) {
			matches = append(matches, repo)
		}
	}
	if len(matches) == 1 {
		return Resolution{Repository: matches[0], Inherited: inherited}, nil
	}
	reason := "Unknown project."
	if option == "" {
		reason = "Please name a project."
	} else if len(matches) > 1 {
		reason = "Ambiguous project; use owner/name."
	}
	names := make([]string, 0, len(repos))
	for _, repo := range repos {
		if repo.Forge == "github" {
			names = append(names, repo.Slug())
		}
	}
	sort.Strings(names)
	catalog := ""
	for _, name := range names {
		if len(catalog)+len(name)+2 > 800 {
			catalog += ", … (more projects left out)"
			break
		}
		if catalog != "" {
			catalog += ", "
		}
		catalog += name
	}
	if catalog == "" {
		catalog = "none"
	}
	return Resolution{Refusal: reason + " Registered projects: " + catalog}, nil
}
