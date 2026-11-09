package teamsource

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/factorylib"
)

// ErrHistoricalJudgeCapture identifies a role-only capture usable for saved reports.
var ErrHistoricalJudgeCapture = errors.New("historical judge capture is incomplete; new provider launch refused")

// The judge is a factory library role, not team content. A workflow with a
// judge step retains its capture under these keys of the workflow capture.
const judgeCapturePath = "judge/instructions.json"
const judgeLegacyRolePath = "judge/challenger.md"

// WorkflowJudgeInstructions reads the role pinned by workflow capture.
func WorkflowJudgeInstructions(definition *WorkflowDefinition) ([]byte, error) {
	if definition == nil {
		return nil, errors.New("judge workflow definition is missing")
	}
	role := definition.Files[judgeLegacyRolePath]
	if len(role) == 0 {
		return nil, errors.New("captured judge instructions are missing")
	}
	return slices.Clone(role), nil
}

// WorkflowJudgeCapture restores the retained judge capture without consulting live files.
func WorkflowJudgeCapture(definition *WorkflowDefinition) (*factorylib.Capture, error) {
	if definition == nil {
		return nil, errors.New("judge workflow definition is missing")
	}
	raw := definition.Files[judgeCapturePath]
	if len(raw) == 0 {
		return nil, ErrHistoricalJudgeCapture
	}
	c, err := factorylib.Restore(raw)
	if err != nil {
		return nil, fmt.Errorf("captured judge instructions: %w", err)
	}
	if c.Role() != factorylib.Judge {
		return nil, errors.New("captured judge role differs")
	}
	if !bytes.Equal(c.Instructions(), definition.Files[judgeLegacyRolePath]) {
		return nil, errors.New("captured judge role differs from retained resource")
	}
	return c, nil
}

func captureWorkflowJudge(ctx context.Context, source fs.FS, result *WorkflowDefinition, total *int, libraries []factorylib.Source) error {
	if retained, ok := source.(RetainedFiles); ok {
		if _, exists := retained[judgeCapturePath]; !exists {
			role := retained[judgeLegacyRolePath]
			if len(role) == 0 {
				return errors.New("retained judge instructions are missing")
			}
			// Historical captures keep their original identity for report-only recovery.
			result.Files[judgeLegacyRolePath] = slices.Clone(role)
			*total += len(role)
			if int64(len(role)) > MaxSourceBytes || *total > MaxSnapshotBytes {
				return errors.New("historical judge resource exceeds capture bounds")
			}
			return nil
		}
		for name, raw := range retained {
			if name == judgeCapturePath || name == judgeLegacyRolePath || strings.HasPrefix(name, "judge/") {
				if int64(len(raw)) > MaxSourceBytes || len(raw) > MaxSnapshotBytes-*total {
					return errors.New("judge resource exceeds capture bounds")
				}
				result.Files[name] = slices.Clone(raw)
				*total += len(raw)
			}
		}
		_, err := WorkflowJudgeCapture(result)
		return err
	}
	if len(libraries) == 0 || libraries[0] == nil {
		// Validation-only callers (team releases, team checks) capture no
		// factory role; admission always passes the installed library.
		return nil
	}
	capture, err := factorylib.CaptureRole(ctx, libraries[0], factorylib.Judge)
	if err != nil {
		return fmt.Errorf("capture judge instructions: %w", err)
	}
	raw, err := capture.Encode()
	if err != nil {
		return err
	}
	files := map[string][]byte{judgeCapturePath: raw, judgeLegacyRolePath: capture.Instructions()}
	for _, name := range []string{judgeCapturePath, judgeLegacyRolePath} {
		content := files[name]
		if _, err := fs.Stat(source, name); err == nil || !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("workflow package collides with factory judge resource %s", name)
		}
		if _, exists := result.Files[name]; exists {
			return fmt.Errorf("workflow capture collides with factory judge resource %s", name)
		}
		if int64(len(content)) > MaxSourceBytes || len(content) > MaxSnapshotBytes-*total {
			return errors.New("judge resource exceeds capture bounds")
		}
		result.Files[name] = slices.Clone(content)
		*total += len(content)
	}
	return nil
}
