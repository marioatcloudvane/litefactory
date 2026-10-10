package teamsource

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/config"
)

const stackCapturePath = ".stack-capture/admission.json"
const stackBasePath = ".stack-capture/base.json"
const stackOverridePath = ".stack-capture/override.json"
const stackGuidancePrefix = ".stack-capture/guidance/"

type stackCaptureManifest struct {
	Version         int    `json:"version"`
	Revision        string `json:"revision"`
	BaseDigest      string `json:"base_digest"`
	OverrideDigest  string `json:"override_digest"`
	EffectiveDigest string `json:"effective_digest"`
}

// AttachStackProfile retains exact profile, override, revision and selected
// guidance bytes in the existing workflow file inventory and content digest.
func AttachStackProfile(ctx context.Context, definition *WorkflowDefinition, resolved config.ResolvedStackProfile, revision string, guidance map[string][]byte) error {
	if definition == nil {
		return errors.New("stack.capture: missing workflow definition")
	}
	if err := resolved.ValidateRetained(); err != nil {
		return err
	}
	if !stackRevisionValid(revision) {
		return errors.New("stack.revision: requires full commit identity")
	}
	closure, err := CaptureStackGuidance(ctx, RetainedFiles(guidance), resolved.Effective)
	if err != nil {
		return err
	}
	if len(closure) != len(guidance) {
		return errors.New("stack.guidance: undeclared captured resource")
	}
	manifest, err := json.Marshal(stackCaptureManifest{Version: 1, Revision: revision, BaseDigest: resolved.BaseDigest, OverrideDigest: resolved.OverrideDigest, EffectiveDigest: resolved.EffectiveDigest})
	if err != nil {
		return err
	}
	files := make(map[string][]byte, len(definition.Files)+len(guidance)+3)
	for name, raw := range definition.Files {
		if strings.HasPrefix(name, ".stack-capture/") {
			return errors.New("stack.capture: reserved resource path collision")
		}
		files[name] = slices.Clone(raw)
	}
	files[stackCapturePath] = manifest
	files[stackBasePath] = slices.Clone(resolved.BaseBytes)
	if resolved.OverrideBytes != nil {
		files[stackOverridePath] = slices.Clone(resolved.OverrideBytes)
	}
	for name, raw := range guidance {
		if !safeResearchPath(name) || int64(len(raw)) > MaxSourceBytes {
			return errors.New("stack.guidance: invalid captured path or size")
		}
		files[stackGuidancePrefix+name] = slices.Clone(raw)
	}
	// Reconstruct from owned bytes, validating declared closure and all digests
	// before replacing the caller's existing admission.
	fresh := &WorkflowDefinition{Files: files, Nodes: definition.Nodes, StackProfile: nil}
	if err := restoreStackProfile(ctx, RetainedFiles(files), fresh); err != nil {
		return err
	}
	total := 0
	for _, raw := range files {
		total += len(raw)
	}
	if total > MaxSnapshotBytes {
		return errors.New("stack.capture: workflow capture exceeds bounds")
	}
	definition.Files = files
	definition.Nodes = fresh.Nodes
	definition.StackProfile = fresh.StackProfile
	definition.StackRevision = fresh.StackRevision
	definition.Hashes = map[string]string{}
	keys := make([]string, 0, len(files))
	for name, raw := range files {
		definition.Hashes[name] = hashBytes(raw)
		keys = append(keys, name)
	}
	slices.Sort(keys)
	var inventory strings.Builder
	for _, name := range keys {
		inventory.WriteString(name + "\x00" + definition.Hashes[name] + "\n")
	}
	definition.Digest = hashBytes([]byte(inventory.String()))
	return nil
}

func restoreStackProfile(ctx context.Context, source fs.FS, definition *WorkflowDefinition) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	reader := researchReader{ctx: ctx, source: source, files: map[string][]byte{}}
	raw, err := reader.read(stackCapturePath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("stack.capture: invalid manifest resource")
	}
	var manifest stackCaptureManifest
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&manifest); err != nil {
		return errors.New("stack.capture: invalid manifest schema")
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("stack.capture: trailing manifest data")
	}
	if manifest.Version != 1 || !stackRevisionValid(manifest.Revision) {
		return errors.New("stack.capture: invalid version or revision")
	}
	base, err := reader.read(stackBasePath)
	if err != nil {
		return errors.New("stack.capture: missing base profile")
	}
	override, err := reader.read(stackOverridePath)
	if errors.Is(err, fs.ErrNotExist) {
		override = nil
	} else if err != nil {
		return errors.New("stack.capture: invalid override resource")
	}
	resolved, err := config.ResolveStackProfile(base, override)
	if err != nil {
		return err
	}
	if manifest.BaseDigest != resolved.BaseDigest || manifest.OverrideDigest != resolved.OverrideDigest || manifest.EffectiveDigest != resolved.EffectiveDigest {
		return errors.New("stack.capture: retained profile digest mismatch")
	}
	guidanceFS, err := fs.Sub(source, strings.TrimSuffix(stackGuidancePrefix, "/"))
	if err != nil {
		return errors.New("stack.guidance: invalid captured root")
	}
	closure, err := CaptureStackGuidance(ctx, guidanceFS, resolved.Effective)
	if err != nil {
		return err
	}
	for name, content := range closure {
		reader.files[stackGuidancePrefix+name] = content
	}
	for name, content := range reader.files {
		if _, exists := definition.Files[name]; !exists {
			definition.Files[name] = content
		}
	}
	nodes, err := stackDeliveryNodes(definition.Nodes, closure)
	if err != nil {
		return err
	}
	definition.Nodes = nodes
	definition.StackProfile = &resolved
	definition.StackRevision = manifest.Revision
	total := 0
	for _, content := range definition.Files {
		total += len(content)
	}
	if total > MaxSnapshotBytes {
		return errors.New("stack.capture: workflow capture exceeds bounds")
	}
	return nil
}

func stackRevisionValid(revision string) bool {
	if len(revision) != 40 && len(revision) != 64 {
		return false
	}
	for _, c := range revision {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// stackDeliveryNodes maps captured package skills into the native skill namespace
// for every admitted node. Dependencies and resource bytes travel together.
func stackDeliveryNodes(nodes map[string]WorkflowNode, closure map[string][]byte) (map[string]WorkflowNode, error) {
	delivery := map[string][]byte{}
	skills := []string{}
	for name, raw := range closure {
		_, relative, ok := strings.Cut(name, "/skills/")
		if strings.HasPrefix(name, "skills/") {
			relative, ok = strings.TrimPrefix(name, "skills/"), true
		}
		if !ok {
			return nil, errors.New("stack.guidance: invalid native resource path")
		}
		target := "skills/" + relative
		if previous, exists := delivery[target]; exists && !bytes.Equal(previous, raw) {
			return nil, errors.New("stack.guidance: selected native resource collision")
		}
		delivery[target] = raw
		if strings.HasSuffix(relative, "/SKILL.md") {
			skill := strings.TrimSuffix(relative, "/SKILL.md")
			if !slices.Contains(skills, skill) {
				skills = append(skills, skill)
			}
		}
	}
	result := make(map[string]WorkflowNode, len(nodes))
	for id, node := range nodes {
		merged := node
		merged.Skills = slices.Clone(node.Skills)
		merged.Files = make(map[string][]byte, len(node.Files)+len(delivery))
		for name, raw := range node.Files {
			merged.Files[name] = slices.Clone(raw)
		}
		for name, raw := range delivery {
			if previous, exists := merged.Files[name]; exists && !bytes.Equal(previous, raw) {
				return nil, errors.New("stack.guidance: conflicts with captured node resource")
			}
			merged.Files[name] = slices.Clone(raw)
		}
		for _, skill := range skills {
			if !slices.Contains(merged.Skills, skill) {
				merged.Skills = append(merged.Skills, skill)
			}
		}
		native := map[string]string{}
		for _, skill := range merged.Skills {
			if previous, exists := native[path.Base(skill)]; exists && previous != skill {
				return nil, errors.New("stack.guidance: colliding native skill identity")
			}
			native[path.Base(skill)] = skill
		}
		total := 0
		for _, raw := range merged.Files {
			total += len(raw)
		}
		if total > MaxSnapshotBytes {
			return nil, errors.New("stack.guidance: node delivery exceeds bounds")
		}
		slices.Sort(merged.Skills)
		result[id] = merged
	}
	return result, nil
}
