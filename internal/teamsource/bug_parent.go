package teamsource

import (
	"context"
	"errors"
	"github.com/cloudvane/litefactory/internal/team"
	"io/fs"
	"slices"
	"sort"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

// CaptureBugParent pins the complete parent, roles and separate child closure.
func CaptureBugParent(ctx context.Context, registry graphexec.Registry) (*WorkflowDefinition, error) {
	source, err := fs.Sub(bugAssets, "bug")
	if err != nil {
		return nil, err
	}
	out, err := CaptureWorkflowIdentity(ctx, source, "bug.dot", "builtin", agent.WorkflowBug, registry)
	if err != nil {
		return nil, err
	}
	if err = ValidateBugParent(out.Profile); err != nil {
		return nil, err
	}
	child, err := CaptureBugTests(ctx, registry)
	if err != nil {
		return nil, err
	}
	for path, raw := range child.Files {
		out.Files[path] = slices.Clone(raw)
		out.Hashes[path] = child.Hashes[path]
	}
	keys := make([]string, 0, len(out.Files))
	for path := range out.Files {
		keys = append(keys, path)
	}
	sort.Strings(keys)
	var manifest strings.Builder
	for _, path := range keys {
		manifest.WriteString(path + "\x00" + out.Hashes[path] + "\n")
	}
	out.Digest = team.HashBytes([]byte(manifest.String()))
	return out, nil
}

// ValidateBugParent admits exactly the shipped topology, role selection and
// finite bounds. User package text cannot select a new entry or capability.
func ValidateBugParent(p *graphexec.Profile) error {
	if p == nil || p.Workflow != agent.WorkflowBug || p.Mode != "" || p.Start != "record" || p.Team != "builtin" {
		return errors.New("invalid Bug parent identity")
	}
	raw, err := fs.ReadFile(bugAssets, "bug/bug.dot")
	if err != nil {
		return err
	}
	expected, err := graphexec.ParseProfile(raw)
	if err != nil {
		return err
	}
	if p.Digest != expected.Digest {
		return errors.New("bug parent differs from factory-pinned topology and bounds")
	}
	return nil
}
