// Package teamrelease deterministically packages the selected canonical team sources.
// Generation is an offline development operation; runtime uses installed bytes.
package teamrelease

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/teampackage"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/turn"
)

var workflows = []string{"bug", "implement", "plan", "research", "spike", "validate"}

// Generate validates a complete candidate before replacing any generated output.
// Check mode reports every drifted path and never writes source or release files.
// Prior output is restored if publication fails partway through the batch.
func Generate(ctx context.Context, repo string, check bool) error {
	return GenerateSources(ctx, repo, check, []string{"internal/teamsource"})
}

// GenerateSources publishes a combined catalogue from explicit repository-relative
// canonical directories. Existing release identities and bytes stay immutable.
func GenerateSources(ctx context.Context, repo string, check bool, sources []string) error {
	root, err := os.OpenRoot(repo)
	if err != nil {
		return err
	}
	defer root.Close()
	if len(sources) == 0 {
		return errors.New("sources: at least one canonical source is required")
	}
	var catalogueData teampackage.Catalogue
	catalogueData.Schema = 1
	oldCatalogue, err := read(root, "teams/teams.json")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if len(oldCatalogue) != 0 {
		catalogueData, err = teampackage.DecodeCatalogue(oldCatalogue)
		if err != nil {
			return err
		}
	}
	outputs := map[string]map[string][]byte{}
	seen := map[string]bool{}
	candidates := map[string]teampackage.Release{}
	for _, source := range sources {
		if !safe(source) {
			return errors.New("sources: unsafe canonical source path")
		}
		files, release, err := candidateSource(ctx, root, source)
		if err != nil {
			return err
		}
		identity := release.Slug + "/" + release.Version
		if seen[identity] {
			return errors.New("sources: duplicate release identity")
		}
		seen[identity] = true
		found := false
		for i, old := range catalogueData.Releases {
			if old.Slug == release.Slug && old.Version == release.Version {
				if old.Checksum != release.Checksum || old.Download != release.Download {
					return fmt.Errorf("%s: published release is immutable; use a new version", source)
				}
				found = true
				catalogueData.Releases[i] = old
			}
		}
		if !found {
			catalogueData.Releases = append(catalogueData.Releases, release)
		}
		candidates["teams/"+release.Download] = release
		if _, exists := outputs["teams/"+release.Download]; exists {
			return errors.New("sources: duplicate download target")
		}
		for _, old := range catalogueData.Releases {
			if (old.Download == release.Download || strings.HasPrefix(old.Download, release.Download+"/") || strings.HasPrefix(release.Download, old.Download+"/")) && (old.Slug != release.Slug || old.Version != release.Version) {
				return errors.New("sources: download target collides with existing release")
			}
		}
		outputs["teams/"+release.Download] = files
	}
	slices.SortFunc(catalogueData.Releases, func(a, b teampackage.Release) int { return strings.Compare(a.Slug+"/"+a.Version, b.Slug+"/"+b.Version) })
	catalogue, err := json.MarshalIndent(catalogueData, "", "  ")
	if err != nil {
		return err
	}
	catalogue = append(catalogue, '\n')
	candidateTargets := make([]string, 0, len(candidates))
	for target := range candidates {
		candidateTargets = append(candidateTargets, target)
	}
	slices.Sort(candidateTargets)
	for _, target := range candidateTargets {
		release := candidates[target]
		if err := validatePackage(ctx, outputs[target], release, catalogue); err != nil {
			return err
		}
	}
	var faults []error
	keys := make([]string, 0, len(outputs))
	for target := range outputs {
		keys = append(keys, target)
	}
	slices.Sort(keys)
	for _, target := range keys {
		if err := safeAncestors(root, target); err != nil {
			return err
		}
		old, err := tree(root, target)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		for name, raw := range outputs[target] {
			if !bytes.Equal(raw, old[name]) {
				faults = append(faults, fmt.Errorf("drift: %s/%s", target, name))
			}
		}
		for name := range old {
			if _, ok := outputs[target][name]; !ok {
				faults = append(faults, fmt.Errorf("drift: unexpected %s/%s", target, name))
			}
		}
	}
	if err := safeAncestors(root, "teams/teams.json"); err != nil {
		return err
	}
	if !bytes.Equal(catalogue, oldCatalogue) {
		faults = append(faults, errors.New("drift: teams/teams.json"))
	}
	if check {
		slices.SortFunc(faults, func(a, b error) int { return strings.Compare(a.Error(), b.Error()) })
		return errors.Join(faults...)
	}
	if len(faults) == 0 {
		return nil
	}
	return publish(ctx, root, repo, outputs, catalogue, root.Rename)
}

func candidateSource(ctx context.Context, root *os.Root, sourceRoot string) (map[string][]byte, teampackage.Release, error) {
	var release teampackage.Release
	raw, err := read(root, sourceRoot+"/release.json")
	if err != nil {
		return nil, release, err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&release); err != nil {
		return nil, release, fmt.Errorf("internal/teamsource/release.json: %w", err)
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, release, errors.New("internal/teamsource/release.json: trailing JSON")
	}
	if !safe(release.Slug) || strings.Contains(release.Slug, "/") || !safe(release.Download) || !strings.HasPrefix(release.Download, release.Slug+"/") || release.Download == "go-development/v1" || len(release.Files) != 0 || release.Checksum != "" {
		return nil, release, errors.New("internal/teamsource/release.json: invalid identity, unsafe/immutable download path or authored hashes")
	}
	files := map[string][]byte{}
	profile, err := read(root, sourceRoot+"/stack-profile.json")
	if err != nil {
		return nil, release, fmt.Errorf("stack-profile.json: %w", err)
	}
	if _, err := config.DecodeStackProfile(profile); err != nil {
		return nil, release, err
	}
	files["stack-profile.json"] = profile
	for _, w := range workflows {
		if err := ctx.Err(); err != nil {
			return nil, release, err
		}
		source, err := tree(root, sourceRoot+"/"+w)
		if err != nil {
			return nil, release, err
		}
		for name, raw := range source {
			files[w+"/"+name] = raw
		}
		graph, ok := source[w+".dot"]
		if !ok || !bytes.Contains(graph, []byte(`team="`+release.Slug+`"`)) {
			return nil, release, fmt.Errorf("%s/%s.dot: missing selected team graph identity", w, w)
		}
	}
	files["team.config"] = []byte("# Generated by go run ./cmd/teamrelease. Edit the selected canonical source only.\n")
	mapping := map[string]string{}
	for _, w := range workflows {
		mapping[w] = w + "/" + w + ".dot"
		files["team.config"] = append(files["team.config"], []byte("  "+w+": "+mapping[w]+"\n")...)
	}
	files["GENERATED.md"] = []byte("# Generated team release\n\nGenerated by `go run ./cmd/teamrelease` from the selected canonical source. Do not edit distribution files. Use `go run ./cmd/teamrelease -check` to detect drift. Historical releases and saved captures are immutable evidence.\n")
	guidanceFiles := map[string][]byte{}
	if len(profile) != 0 {
		decoded, err := config.DecodeStackProfile(profile)
		if err != nil {
			return nil, release, err
		}
		guidanceFiles, err = teamsource.CaptureStackGuidance(ctx, teamsource.RetainedFiles(files), decoded)
		if err != nil {
			return nil, release, err
		}
	}
	if err := turn.ValidateTeamWorkflows(ctx, teamsource.RetainedFiles(files), mapping); err != nil {
		return nil, release, fmt.Errorf("candidate workflows: %w", err)
	}
	builders := map[string]func() (graphexec.Registry, error){"research": turn.ResearchSpecs, "plan": turn.PlanSpecs, "implement": turn.ImplementSpecs, "validate": turn.ValidateFlowSpecs, "spike": turn.SpikeSpecs, "bug": turn.SimplifiedBugSpecs}
	for _, w := range workflows {
		registry, err := builders[w]()
		if err != nil {
			return nil, release, err
		}
		source, err := fs.Sub(teamsource.RetainedFiles(files), w)
		if err != nil {
			return nil, release, err
		}
		definition, err := teamsource.CaptureWorkflowIdentity(ctx, source, w+".dot", release.Slug, agent.Workflow(w), registry)
		if err != nil {
			return nil, release, err
		}
		for name := range files {
			if strings.HasPrefix(name, w+"/") {
				if _, ok := definition.Files[strings.TrimPrefix(name, w+"/")]; !ok {
					if _, selected := guidanceFiles[name]; selected {
						continue
					}
					return nil, release, fmt.Errorf("%s: resource is outside the captured role closure", name)
				}
			}
		}
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	h := sha256.New()
	for _, name := range names {
		sum := sha256.Sum256(files[name])
		hash := hex.EncodeToString(sum[:])
		release.Files = append(release.Files, teampackage.File{Path: name, SHA256: hash})
		_, _ = io.WriteString(h, name+"\x00"+hash+"\n")
	}
	release.Checksum = hex.EncodeToString(h.Sum(nil))
	return files, release, nil
}

func safe(name string) bool {
	return name != "." && fs.ValidPath(name) && path.Clean(name) == name && !strings.ContainsAny(name, "\\:\x00\r\n%?#")
}

func safeAncestors(root *os.Root, name string) error {
	if !safe(name) {
		return fmt.Errorf("%s: unsafe path", name)
	}
	parts := strings.Split(name, "/")
	for i := range parts {
		p := strings.Join(parts[:i+1], "/")
		info, err := root.Lstat(p)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 || (i < len(parts)-1 && !info.IsDir()) {
			return fmt.Errorf("%s: symlink or invalid directory", p)
		}
	}
	return nil
}

func read(root *os.Root, name string) ([]byte, error) {
	if err := safeAncestors(root, name); err != nil {
		return nil, err
	}
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > teamsource.MaxSourceBytes {
		return nil, fmt.Errorf("%s: nonregular or oversized source", name)
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	after, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, after) {
		return nil, fmt.Errorf("%s: source changed while opening", name)
	}
	raw, err := io.ReadAll(io.LimitReader(f, teamsource.MaxSourceBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > teamsource.MaxSourceBytes {
		return nil, fmt.Errorf("%s: oversized source", name)
	}
	return raw, nil
}

func tree(root *os.Root, dir string) (map[string][]byte, error) {
	if err := safeAncestors(root, dir); err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	err := fs.WalkDir(root.FS(), dir, func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(name, dir+"/")
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s: symlink source/output", name)
		}
		if d.IsDir() {
			return nil
		}
		if !safe(rel) {
			return fmt.Errorf("%s: unsafe source/output path", name)
		}
		raw, err := read(root, name)
		if err != nil {
			return err
		}
		files[rel] = raw
		return nil
	})
	return files, err
}

func validatePackage(ctx context.Context, files map[string][]byte, release teampackage.Release, catalogue []byte) error {
	stage, err := os.MkdirTemp("", "lf-team-release-validate-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	stage, err = filepath.EvalSymlinks(stage)
	if err != nil {
		return err
	}
	if err := writeTree(filepath.Join(stage, "catalogue", filepath.FromSlash(release.Download)), files); err != nil {
		return err
	}
	cataloguePath := filepath.Join(stage, "catalogue", "teams.json")
	if err := os.WriteFile(cataloguePath, catalogue, 0o600); err != nil {
		return err
	}
	manager, err := teampackage.New(teampackage.Options{Paths: config.NewPaths(filepath.Join(stage, "installed")), Validate: turn.ValidateTeamWorkflows})
	if err != nil {
		return err
	}
	if _, _, err := manager.Install(ctx, cataloguePath, release.Slug, release.Version); err != nil {
		return fmt.Errorf("internal/teamsource/release.json candidate catalogue/release: %w", err)
	}
	return nil
}

func writeTree(dir string, files map[string][]byte) error {
	for name, raw := range files {
		if !safe(name) {
			return fmt.Errorf("%s: unsafe generated path", name)
		}
		dest := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(dest, raw, 0o600); err != nil {
			return err
		}
	}
	return nil
}

type replacement struct {
	target, staged, backup string
	hadOld, installed      bool
}

func publish(ctx context.Context, root *os.Root, repo string, outputs map[string]map[string][]byte, catalogue []byte, rename func(string, string) error) error {
	stage, err := os.MkdirTemp(repo, ".lf-release-")
	if err != nil {
		return err
	}
	keepStage := false
	defer func() {
		if !keepStage {
			_ = os.RemoveAll(stage)
		}
	}()
	stageName := filepath.Base(stage)
	targets := make([]string, 0, len(outputs))
	for target := range outputs {
		targets = append(targets, target)
	}
	slices.Sort(targets)
	changes := make([]replacement, 0, len(targets)+1)
	for i, target := range targets {
		staged := fmt.Sprintf("%s/new-%d", stageName, i)
		if err := writeTree(filepath.Join(repo, staged), outputs[target]); err != nil {
			return err
		}
		changes = append(changes, replacement{target: target, staged: staged, backup: fmt.Sprintf("%s/old-%d", stageName, i)})
	}
	staged := stageName + "/catalogue"
	if err := root.WriteFile(staged, catalogue, 0o600); err != nil {
		return err
	}
	changes = append(changes, replacement{target: "teams/teams.json", staged: staged, backup: stageName + "/old-catalogue"})
	rollback := func(cause error) error {
		faults := []error{cause}
		var restoreFaults []error
		for i := len(changes) - 1; i >= 0; i-- {
			c := changes[i]
			if c.installed {
				restoreFaults = append(restoreFaults, root.RemoveAll(c.target))
			}
			if c.hadOld {
				restoreFaults = append(restoreFaults, rename(c.backup, c.target))
			}
		}
		if err := errors.Join(restoreFaults...); err != nil {
			keepStage = true
			faults = append(faults, fmt.Errorf("restore prior output (backups retained in %s): %w", stage, err))
		}
		return errors.Join(faults...)
	}
	for i := range changes {
		c := &changes[i]
		if err := ctx.Err(); err != nil {
			return rollback(err)
		}
		if err := safeAncestors(root, c.target); err != nil {
			return rollback(err)
		}
		if err := root.MkdirAll(path.Dir(c.target), 0o700); err != nil {
			return rollback(err)
		}
		if _, err := root.Lstat(c.target); err == nil {
			if err := rename(c.target, c.backup); err != nil {
				return rollback(err)
			}
			c.hadOld = true
		} else if !errors.Is(err, fs.ErrNotExist) {
			return rollback(err)
		}
		if err := rename(c.staged, c.target); err != nil {
			return rollback(err)
		}
		c.installed = true
	}
	return nil
}
