package teamsource

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/render"
)

// NativeResearch returns only one selected node's native delivery resources.
// The caller writes these through the existing confined temporary bundle writer;
// no installed instruction tree or catalog is changed. Tools are intersected
// with the invocation ceiling rather than granted by shipped role content.
func NativeResearch(kind string, node ResearchNode, model string, allowedTools []string, sandbox string) ([]render.RenderedFile, error) {
	var faults []error
	native := map[string]bool{}
	for _, skill := range node.Skills {
		if !graphexec.ResearchName(skill, true) || native[path.Base(skill)] {
			faults = append(faults, fmt.Errorf("invalid or colliding selected skill %q", skill))
		}
		native[path.Base(skill)] = true
	}
	total := 0
	for name, raw := range node.Files {
		if !safeResearchPath(name) || int64(len(raw)) > MaxSourceBytes {
			faults = append(faults, fmt.Errorf("invalid Research delivery resource %q", name))
		}
		total += len(raw)
	}
	if total > MaxSnapshotBytes {
		faults = append(faults, errors.New("research delivery exceeds aggregate bound"))
	}
	if err := errors.Join(faults...); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(node.Skills))
	for _, skill := range node.Skills {
		names = append(names, path.Base(skill))
	}
	sort.Strings(names)
	tools := []string{}
	for _, tool := range node.Role.Frontmatter.Tools {
		if slices.Contains(allowedTools, tool) {
			tools = append(tools, tool)
		}
	}
	role, err := render.Native(kind, render.NativeRole{Source: node.Role, Model: model, Tools: tools, Skills: names}, sandbox)
	if err != nil {
		return nil, err
	}
	result := []render.RenderedFile{role}
	for _, skill := range node.Skills {
		prefix := "skills/" + skill + "/"
		if _, exists := node.Files[prefix+"SKILL.md"]; !exists {
			return nil, fmt.Errorf("selected Research skill %s is missing", skill)
		}
		for name, raw := range node.Files {
			if !strings.HasPrefix(name, prefix) {
				continue
			}
			result = append(result, render.RenderedFile{Path: "plugin/skills/" + path.Base(skill) + "/" + strings.TrimPrefix(name, prefix), Bytes: slices.Clone(raw)})
		}
	}
	result = append(result, render.RenderedFile{Path: "plugin/.claude-plugin/plugin.json", Bytes: []byte("{\"name\":\"litefactory-research\",\"version\":\"1.0.0\"}\n")})
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}
