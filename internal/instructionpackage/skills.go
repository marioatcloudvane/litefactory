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
	case LogAnalystRole:
		return []string{"go-guidance", "agent-setup-guidance"}
	case "doctor-diagnosis":
		return []string{"doctor-field-guide"}
	case "doctor-repair":
		return []string{"doctor-field-guide", "doctor-repair-authority"}
	default:
		return nil
	}
}

func roleClosure(role string, read func(string) ([]byte, error)) (map[string][]byte, error) {
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
	_, skills, err := skillclosure.Load(selectedSkills(role), read)
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
	shipped, err := roleClosure(role, func(name string) ([]byte, error) {
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
	files, err := roleClosure(role, read)
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
