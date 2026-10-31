package instructionpackage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/skillclosure"
)

// selectedSkills is factory-owned selection; package bytes cannot add entry points.
func selectedSkills(role string) []string {
	switch role {
	case "task-advisory":
		return []string{"task-advisory"}
	case "doctor-diagnosis":
		return []string{"doctor-field-guide"}
	case "doctor-repair":
		return []string{"doctor-field-guide", "doctor-repair-authority"}
	case "conversation-factory":
		return []string{StandardsAuthoringSkill}
	default:
		return nil
	}
}

// StandardsAuthoringSkill guides the conversation agent through authoring a
// standard, from the confirmed start to the one publish step.
const StandardsAuthoringSkill = "standards-authoring"

// optionalSkills joined a role after captures of it were retained, and after
// the pinned legacy baseline: a package or capture without one selects it not.
var optionalSkills = map[string]bool{StandardsAuthoringSkill: true}

// presentSkills is the selection for role among the files has holds.
func presentSkills(role string, has func(string) bool) []string {
	var out []string
	for _, name := range selectedSkills(role) {
		if !optionalSkills[name] || has("skills/"+name+"/SKILL.md") {
			out = append(out, name)
		}
	}
	return out
}

func (p *Package) skillsFor(role string) []string {
	return presentSkills(role, func(name string) bool { _, ok := p.files[name]; return ok })
}

func roleClosure(role string, selected []string, read func(string) ([]byte, error)) (map[string][]byte, error) {
	files := map[string][]byte{}
	bounded := map[string][]byte{}
	totalRead := 0
	originalRead := read
	read = func(name string) ([]byte, error) {
		if raw, ok := bounded[name]; ok {
			return raw, nil
		}
		if len(bounded) >= MaxFiles {
			return nil, ownerError("bounds", nil)
		}
		raw, err := originalRead(name)
		if err != nil {
			return nil, err
		}
		totalRead += len(raw)
		if len(raw) > MaxFileBytes || totalRead > MaxCaptureBytes {
			return nil, ownerError("bounds", nil)
		}
		bounded[name] = raw
		return raw, nil
	}
	for _, name := range []string{CataloguePath, rolePath(role)} {
		raw, err := read(name)
		if err != nil {
			return nil, err
		}
		files[name] = raw
	}
	if err := validateText(files[rolePath(role)]); err != nil {
		return nil, err
	}
	_, skills, err := skillclosure.Load(selected, read)
	if err != nil {
		return nil, err
	}
	for name, raw := range skills {
		files[name] = raw
	}
	total := 0
	for _, raw := range files {
		total += len(raw)
		if len(raw) > MaxFileBytes || total > MaxCaptureBytes {
			return nil, fmt.Errorf("instruction closure exceeds content bounds")
		}
	}
	if len(files) > MaxFiles {
		return nil, fmt.Errorf("instruction closure exceeds file bound")
	}
	return files, nil
}

// SkillInstructions returns the exact selected skill body followed by its
// dependency bodies and resource bytes in stable path order, all from capture.
// Shipped Doctor skills have no appended resources or dependencies.
func (c *RoleCapture) SkillInstructions(name string) []byte {
	files := map[string][]byte{}
	for _, file := range c.data.Files {
		files[file.Path] = file.Content
	}
	if !slices.Contains(selectedSkills(c.data.Role), name) {
		return nil
	}
	_, closure, err := skillclosure.Load([]string{name}, func(path string) ([]byte, error) {
		raw, ok := files[path]
		if !ok {
			return nil, fmt.Errorf("missing captured skill")
		}
		return raw, nil
	})
	if err != nil {
		return nil
	}
	root := "skills/" + name + "/SKILL.md"
	_, body, err := skillclosure.Parse(closure[root], name)
	if err != nil {
		return nil
	}
	result := bytes.Clone(body)
	names := make([]string, 0, len(closure))
	for path := range closure {
		if path != root {
			names = append(names, path)
		}
	}
	slices.Sort(names)
	for _, path := range names {
		content := closure[path]
		if strings.HasSuffix(path, "/SKILL.md") {
			id := strings.TrimSuffix(strings.TrimPrefix(path, "skills/"), "/SKILL.md")
			_, content, err = skillclosure.Parse(content, id)
			if err != nil {
				return nil
			}
		}
		result = append(result, []byte("\n\nCaptured skill resource "+path+":\n")...)
		result = append(result, content...)
	}
	return result
}

func (p *Package) selectSkillFiles(ctx context.Context, role string) (map[string][]byte, map[string]Source, []Diagnostic, error) {
	shipped, err := roleClosure(role, p.skillsFor(role), func(name string) ([]byte, error) {
		raw, ok := p.files[name]
		if !ok {
			return nil, fmt.Errorf("missing shipped instruction %s", name)
		}
		return bytes.Clone(raw), nil
	})
	if err != nil {
		return nil, nil, nil, err
	}
	sources := map[string]Source{}
	for name := range shipped {
		sources[name] = SourceShipped
	}
	fallback := func(name string, err error) (map[string][]byte, map[string]Source, []Diagnostic, error) {
		if ctx.Err() != nil {
			return nil, nil, nil, ctx.Err()
		}
		if _, ok := shipped[name]; !ok {
			name = rolePath(role)
		}
		return shipped, sources, []Diagnostic{{Role: role, File: name, Category: failureCategory(err), SelectedSource: SourceShipped}}, nil
	}
	if p.ownerDir == "" {
		return shipped, sources, nil, ctx.Err()
	}
	owner, err := openOwner(p.ownerDir)
	if err != nil {
		if errors.Is(err, errOwnerAbsent) {
			return shipped, sources, nil, ctx.Err()
		}
		return fallback(CataloguePath, err)
	}
	defer owner.root.Close()
	failedName := rolePath(role)
	read := func(name string) ([]byte, error) {
		raw, err := owner.read(ctx, name)
		if err != nil {
			failedName = name
			return nil, ownerError(failureCategory(err), err)
		}
		if name == CataloguePath {
			roles, err := parseCatalogue(raw)
			if err != nil {
				failedName = name
				return nil, ownerError("invalid", err)
			}
			if !sameRoles(roles, p.roles) {
				failedName = name
				return nil, ownerError("catalogue-mismatch", nil)
			}
		}
		return raw, nil
	}
	files, err := roleClosure(role, p.skillsFor(role), read)
	if err != nil {
		return fallback(failedName, ownerErrorUnlessCategorized(err))
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		raw, err := owner.read(ctx, name)
		if err != nil {
			return fallback(name, err)
		}
		if !bytes.Equal(raw, files[name]) {
			return fallback(name, ownerError("changed", nil))
		}
	}
	sources = map[string]Source{}
	for name := range files {
		sources[name] = SourceOwner
	}
	return files, sources, nil, ctx.Err()
}

func ownerErrorUnlessCategorized(err error) error {
	var failure *ownerFailure
	if errors.As(err, &failure) {
		return err
	}
	return ownerError("invalid", err)
}
