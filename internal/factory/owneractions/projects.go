package owneractions

import (
	"sort"
	"strings"
)

// Repository is the identity needed for project selection, without storage rows.
type Repository struct {
	ID                 int64
	Forge, Owner, Name string
}

// Slug returns the registered owner/name locator.
func (r Repository) Slug() string { return r.Owner + "/" + r.Name }

// Resolution is a selected registered project or a bounded clarification.
type Resolution struct {
	Repository Repository
	Inherited  bool
	Refusal    string
}

// SelectProject applies the shared exact, short and inherited project rules.
func SelectProject(repos []Repository, option string, history []string) Resolution {
	option = strings.TrimSpace(option)
	inherited := option == "" && len(history) > 0
	if inherited {
		option = history[0]
	}
	// An exact name wins; otherwise a short name ("litefactory", "Factory")
	// selects the one project whose owner/name ends with it, ignoring case.
	lower := strings.ToLower(option)
	var matches []Repository
	for _, exact := range []bool{true, false} {
		for _, repo := range repos {
			if repo.Forge != "github" {
				continue
			}
			// With nothing named or inherited, the only registered project is the default.
			if exact && (option == "" && len(history) == 0 || option != "" && (option == repo.Slug() || (!strings.Contains(option, "/") && option == repo.Name))) ||
				!exact && (lower == strings.ToLower(repo.Slug()) || len(lower) >= 3 && !strings.Contains(lower, "/") && strings.HasSuffix(strings.ToLower(repo.Name), lower)) {
				matches = append(matches, repo)
			}
		}
		if len(matches) > 0 || option == "" {
			break
		}
	}
	if len(matches) == 1 {
		return Resolution{Repository: matches[0], Inherited: inherited}
	}
	reason := "Unknown project."
	if option == "" {
		reason = "Please name a project."
	} else if len(matches) > 1 {
		names := make([]string, len(matches))
		for i, repo := range matches {
			names[i] = repo.Slug()
		}
		sort.Strings(names)
		return Resolution{Refusal: "Which project do you mean: " + strings.Join(names, " or ") + "?"}
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
	return Resolution{Refusal: reason + " Registered projects: " + catalog}
}
