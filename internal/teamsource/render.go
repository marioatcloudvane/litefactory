package teamsource

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/cloudvane/litefactory/internal/render"
	"github.com/cloudvane/litefactory/internal/team"
)

// RosterEntry is one agent to render for a team add/update invocation: the
// key it is (or will be) recorded under in the team index, and the
// AgentEntry describing where its source markdown is read from. Callers
// build a roster by merging a team's own agents with the index's shared
// agents (internal/cli's teamRoster does this); this package does not need
// to know which half of that merge an entry came from, only where to read
// it from and what key to render it under.
type RosterEntry struct {
	Key   string
	Agent team.AgentEntry
}

// parseAgentSource splits raw agent markdown into a render.ResolvedSource:
// a "---"-delimited YAML frontmatter block decoded into render.Frontmatter,
// plus the remaining body. internal/render deliberately does not do this
// (internal/render/doc.go: "building a real ResolvedSource by reading and
// splitting an actual markdown file is not this package's job"), and
// internal/team never imports internal/render at all (internal/team/doc.go),
// so this lives here, in the package that sits above both.
func parseAgentSource(raw []byte) (render.ResolvedSource, error) {
	const delim = "---"
	text := string(raw)
	if !strings.HasPrefix(text, delim+"\n") {
		return render.ResolvedSource{}, fmt.Errorf("missing leading %q frontmatter delimiter", delim)
	}
	rest := text[len(delim+"\n"):]
	end := strings.Index(rest, "\n"+delim)
	if end < 0 {
		return render.ResolvedSource{}, fmt.Errorf("unterminated frontmatter block (no closing %q)", delim)
	}
	header := rest[:end]
	body := strings.TrimPrefix(rest[end+len("\n"+delim):], "\n")

	var fm render.Frontmatter
	if err := yaml.Unmarshal([]byte(header), &fm); err != nil {
		return render.ResolvedSource{}, fmt.Errorf("decode frontmatter: %w", err)
	}
	return render.ResolvedSource{Frontmatter: fm, Body: body}, nil
}

// RenderTeam renders every entry in roster for every named harness, reading
// each agent's source from fsys, and returns one team.AgentRender per
// (harness, agent) pair, ready to pass to team.Add.
//
// For each (harness, agent key) pair, root's provenance lockfile is
// consulted via team.ExistingTeams before Teams is set: teamName is unioned
// into whatever teams already contribute that agent there, rather than
// replacing them outright. This is the collection step internal/team/doc.go
// and write_types.go's AgentRender doc comment both assign to this package —
// upsertMarkerSection (internal/team/marker.go) locates a shared agent's
// root-file section by agent key alone and replaces it wholesale using only
// the Teams this function hands it, so a second team adding an already
// shared agent would otherwise silently drop the first team's attribution.
// team.ExistingTeams itself returns (nil, nil) both when root has no
// lockfile yet and when this agent/harness pair has no prior record, so the
// first-ever add for an agent is simply teamName on its own.
//
// Every read, parse, render, or lockfile-read failure across the whole
// roster is collected and returned together via errors.Join instead of
// stopping at the first one, matching Add's own aggregate-then-report
// validation — and, since a non-nil error here means the caller never calls
// team.Add, preserving the same render-all-then-write-all-or-nothing
// behavior the original inline loop had.
func RenderTeam(root string, fsys fs.FS, teamName string, roster []RosterEntry, harnessNames []string) ([]team.AgentRender, error) {
	var renders []team.AgentRender
	var errs []error
	for _, harnessName := range harnessNames {
		renderer, lookupErr := render.Lookup(harnessName)
		if lookupErr != nil {
			errs = append(errs, lookupErr)
			continue
		}
		for _, entry := range roster {
			raw, readErr := fs.ReadFile(fsys, entry.Agent.Source)
			if readErr != nil {
				errs = append(errs, fmt.Errorf("read %s: %w", entry.Agent.Source, readErr))
				continue
			}
			source, parseErr := parseAgentSource(raw)
			if parseErr != nil {
				errs = append(errs, fmt.Errorf("%s: %w", entry.Agent.Source, parseErr))
				continue
			}
			// A renderer error means every RenderedFile it produced (if any)
			// must be discarded (internal/render/doc.go): nothing from this
			// call is appended to renders below.
			files, renderErr := renderer.Render(source)
			if renderErr != nil {
				errs = append(errs, fmt.Errorf("render %s for %s: %w", entry.Key, harnessName, renderErr))
				continue
			}
			teamFiles := make([]team.RenderedFile, len(files))
			for i, f := range files {
				teamFiles[i] = team.RenderedFile{Path: f.Path, Bytes: f.Bytes}
			}

			existingTeams, existingErr := team.ExistingTeams(root, harnessName, entry.Key)
			if existingErr != nil {
				errs = append(errs, fmt.Errorf("existing teams for %s (%s): %w", entry.Key, harnessName, existingErr))
				continue
			}
			teams := make([]string, 0, len(existingTeams)+1)
			teams = append(teams, existingTeams...)
			teams = append(teams, teamName)

			renders = append(renders, team.AgentRender{
				AgentKey:   entry.Key,
				Harness:    harnessName,
				Teams:      teams,
				SourcePath: entry.Agent.Source,
				SourceHash: team.HashBytes(raw),
				Files:      teamFiles,
			})
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return renders, nil
}

// RenderCurrent re-renders exactly one agent's current source markdown for
// one harness, independent of team membership and without touching the
// provenance lockfile at all. It answers "what would this exact agent
// produce today" — the fresh-render half of team status's drift
// classification (internal/team.Classify) — as opposed to RenderTeam, whose
// team-membership bookkeeping (team.ExistingTeams unioned with the team being
// added) has no meaning for a status check: there is no team being added.
//
// It returns HashBytes of the raw source bytes read from sourcePath, plus the
// rendered files that source produces for harnessName, converted to
// team.RenderedFile the same way RenderTeam does.
func RenderCurrent(fsys fs.FS, harnessName, sourcePath string) (sourceHash string, files []team.RenderedFile, err error) {
	renderer, err := render.Lookup(harnessName)
	if err != nil {
		return "", nil, err
	}
	raw, err := fs.ReadFile(fsys, sourcePath)
	if err != nil {
		return "", nil, fmt.Errorf("read %s: %w", sourcePath, err)
	}
	source, err := parseAgentSource(raw)
	if err != nil {
		return "", nil, fmt.Errorf("%s: %w", sourcePath, err)
	}
	renderedFiles, err := renderer.Render(source)
	if err != nil {
		return "", nil, fmt.Errorf("render %s for %s: %w", sourcePath, harnessName, err)
	}
	teamFiles := make([]team.RenderedFile, len(renderedFiles))
	for i, f := range renderedFiles {
		teamFiles[i] = team.RenderedFile{Path: f.Path, Bytes: f.Bytes}
	}
	return team.HashBytes(raw), teamFiles, nil
}
