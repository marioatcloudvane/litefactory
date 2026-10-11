package ask

import (
	"context"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

type repoList []store.Repo

func (r repoList) ListRepos(context.Context) ([]store.Repo, error) { return r, nil }

func TestResolveProjectDefaultsOnlyProject(t *testing.T) {
	app := store.Repo{ID: 1, Forge: "github", Owner: "acme", Name: "app"}
	web := store.Repo{ID: 2, Forge: "github", Owner: "acme", Name: "web"}
	local := store.Repo{ID: 3, Forge: "local", Owner: "acme", Name: "tool"}
	got, err := ResolveProject(t.Context(), repoList{app, local}, "", nil)
	if err != nil || got.Refusal != "" || got.Repository.ID != 1 || got.Inherited {
		t.Fatalf("single project not defaulted: %+v %v", got, err)
	}
	got, err = ResolveProject(t.Context(), repoList{app, web}, " ", nil)
	if err != nil || got.Refusal != "Please name a project. Registered projects: acme/app, acme/web" {
		t.Fatalf("several projects did not ask: %+v %v", got, err)
	}
	for _, option := range []string{"web", "acme/web"} {
		got, err = ResolveProject(t.Context(), repoList{app, web}, option, nil)
		if err != nil || got.Repository.ID != 2 {
			t.Fatalf("%s not resolved: %+v %v", option, got, err)
		}
	}
	// Inherited history still decides, even when it names no project.
	got, err = ResolveProject(t.Context(), repoList{app}, "", []Turn{{Project: ""}})
	if err != nil || got.Refusal == "" {
		t.Fatalf("empty inherited project was replaced by default: %+v %v", got, err)
	}
	got, err = ResolveProject(t.Context(), repoList{}, "", nil)
	if err != nil || got.Refusal != "Please name a project. Registered projects: none" {
		t.Fatalf("empty catalogue: %+v %v", got, err)
	}
}
