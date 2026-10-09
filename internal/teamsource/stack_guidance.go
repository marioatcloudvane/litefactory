package teamsource

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/cloudvane/litefactory/internal/config"
)

// Match the release inventory ceiling without coupling source capture to packaging.
const maxStackGuidanceFiles = 4096

// CaptureStackGuidance captures profile-selected skills and their complete
// declared resource/dependency closure using the existing bounded skill reader.
// Paths in the result remain relative to the selected package root.
func CaptureStackGuidance(ctx context.Context, source fs.FS, profile config.StackProfile) (map[string][]byte, error) {
	if err := profile.Validate(); err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	total := 0
	for i, guidance := range profile.Guidance {
		prefix, skill, ok := strings.Cut(guidance.Skill, "/skills/")
		if strings.HasPrefix(guidance.Skill, "skills/") {
			prefix, skill, ok = ".", strings.TrimPrefix(guidance.Skill, "skills/"), true
		}
		if !ok || !strings.HasSuffix(skill, "/SKILL.md") {
			return nil, fmt.Errorf("stack.guidance[%d].skill: requires a workflow-relative skills path", i)
		}
		root, err := fs.Sub(source, prefix)
		if err != nil {
			return nil, fmt.Errorf("stack.guidance[%d]: invalid package root", i)
		}
		reader := researchReader{ctx: ctx, source: root, files: map[string][]byte{}, states: map[string]int{}, native: map[string]string{}, maxFiles: maxStackGuidanceFiles}
		if err := reader.skill(strings.TrimSuffix(skill, "/SKILL.md")); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			// A resource or malformed header may contain authored sensitive values.
			// Report its selected field and cause category, not the parser's input.
			return nil, fmt.Errorf("stack.guidance[%d]: required skill or resource is missing, unsafe or invalid", i)
		}
		for name, raw := range reader.files {
			name = path.Join(prefix, name)
			if _, exists := files[name]; exists {
				continue
			}
			if len(files) >= maxStackGuidanceFiles {
				return nil, errors.New("stack.guidance: captured resource count exceeds bounds")
			}
			if len(raw) > MaxSnapshotBytes-total {
				return nil, errors.New("stack.guidance: captured closure exceeds bounds")
			}
			total += len(raw)
			files[name] = raw
		}
	}
	return files, nil
}
