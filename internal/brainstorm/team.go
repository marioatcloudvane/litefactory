package brainstorm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"

	"github.com/cloudvane/litefactory/internal/skillclosure"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teampackage"
)

// Captured team names: the research workflow, its role and the two scan skills.
const (
	researchWorkflow = "research"
	researchRole     = "research"
	evidenceSkill    = "research-evidence"
	brainstormSkill  = "brainstorm"
	draftsSkill      = "brainstorm-drafts"
)

// InstalledTeam captures the research role and scan skills from a repository's
// installed team version.
type InstalledTeam struct {
	DB       *store.DB
	Packages *teampackage.Manager
}

// Capture implements TeamCapturer.
func (t InstalledTeam) Capture(ctx context.Context, repositoryRef string) (TeamCapture, error) {
	var out TeamCapture
	if t.DB == nil || t.Packages == nil {
		return out, errors.New("brainstorm: installed team capture unavailable")
	}
	repo, err := repositoryByRef(ctx, t.DB, repositoryRef)
	if err != nil {
		return out, err
	}
	err = t.Packages.WithCatalogue(ctx, func(access *teampackage.CatalogueAccess) error {
		binding, e := t.DB.GetRepositoryTeamBinding(ctx, repo.ID)
		if e != nil {
			return e
		}
		return access.WithSelectedWorkflow(ctx, binding.Source, binding.Slug, binding.Version, researchWorkflow, func(entry teampackage.Installed, retained fs.FS) error {
			if binding.Kind == "fetched" && entry.Identity.Digest != binding.ReleaseDigest {
				return fmt.Errorf("repository %s linked release digest changed", repositoryRef)
			}
			graph, ok := entry.Workflows[researchWorkflow]
			if !ok {
				return fmt.Errorf("team %s %s has no research workflow", binding.Slug, binding.Version)
			}
			read, closeRoot, e := packageReader(entry, retained, path.Dir(graph))
			if e != nil {
				return e
			}
			defer closeRoot()
			capture, e := captureTeam(read)
			if e != nil {
				return e
			}
			capture.Version = binding.Source + "/" + binding.Slug + "@" + binding.Version
			out = capture
			return nil
		})
	})
	return out, err
}

// captureTeam reads the research role markdown and the closure of the
// research-evidence and brainstorm skills from one workflow directory.
func captureTeam(read func(string) ([]byte, error)) (TeamCapture, error) {
	var out TeamCapture
	if _, err := read("skills/" + brainstormSkill + "/SKILL.md"); errors.Is(err, fs.ErrNotExist) {
		return out, ErrSkillMissing
	} else if err != nil {
		return out, err
	}
	role, err := read(researchRole + ".md")
	if err != nil {
		return out, err
	}
	skills := []string{evidenceSkill, brainstormSkill}
	if _, err := read("skills/" + draftsSkill + "/SKILL.md"); err == nil {
		skills = append(skills, draftsSkill)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return out, err
	}
	_, files, err := skillclosure.Load(skills, read)
	if err != nil {
		return out, err
	}
	out.Role = string(role)
	out.Files = make(map[string]string, len(files))
	for name, raw := range files {
		out.Files[name] = string(raw)
	}
	return out, nil
}

// packageReader reads workflow-relative files, first from the validated
// retained capture, then from the confined installed package checked against
// its release inventory.
func packageReader(entry teampackage.Installed, retained fs.FS, dir string) (func(string) ([]byte, error), func(), error) {
	root, err := os.OpenRoot(entry.Path)
	if err != nil {
		return nil, nil, err
	}
	expected := map[string]string{}
	for _, f := range entry.Release.Files {
		expected[f.Path] = f.SHA256
	}
	read := func(name string) ([]byte, error) {
		full := path.Join(dir, name)
		if !fs.ValidPath(full) || strings.HasPrefix(full, "../") {
			return nil, fs.ErrInvalid
		}
		if raw, e := fs.ReadFile(retained, full); e == nil {
			return raw, nil
		}
		raw, e := fs.ReadFile(root.FS(), full)
		if e != nil {
			return nil, e
		}
		if len(raw) > MaxResultBytes*4 {
			return nil, fmt.Errorf("team file %s exceeds bounds", full)
		}
		sum := sha256.Sum256(raw)
		if want, ok := expected[full]; !entry.Editable && (!ok || want != hex.EncodeToString(sum[:])) {
			return nil, fmt.Errorf("package changed during capture at %s", full)
		}
		return raw, nil
	}
	return read, func() { _ = root.Close() }, nil
}

// repositoryByRef resolves a forge:owner/name repository reference.
func repositoryByRef(ctx context.Context, db *store.DB, ref string) (store.Repo, error) {
	forge, rest, ok := strings.Cut(ref, ":")
	owner, name, ok2 := strings.Cut(rest, "/")
	if !ok || !ok2 || forge == "" || owner == "" || name == "" {
		return store.Repo{}, ErrInvalid
	}
	return db.GetRepoBySlug(ctx, forge, owner, name)
}

var _ TeamCapturer = InstalledTeam{}
