package teamsource

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/instructionpackage"
)

// ErrHistoricalJudgeCapture identifies a role-only capture usable for saved reports.
var ErrHistoricalJudgeCapture = errors.New("historical judge capture is incomplete; new provider launch refused")

const judgeCapturePath = "judge/instructions.json"
const judgeLegacyRolePath = "judge/challenger.md"

// JudgeInstructions returns shipped advisory role bytes for compatibility.
func JudgeInstructions() ([]byte, error) {
	p, err := instructionpackage.LoadShipped(context.TODO(), "")
	if err != nil {
		return nil, err
	}
	c, _, err := p.Capture(context.TODO(), instructionpackage.ChallengerRole)
	if err != nil {
		return nil, err
	}
	return c.Instructions(), nil
}

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

// WorkflowJudgeCapture restores the complete closure without consulting live files.
func WorkflowJudgeCapture(definition *WorkflowDefinition) (*instructionpackage.RoleCapture, error) {
	if definition == nil {
		return nil, errors.New("judge workflow definition is missing")
	}
	raw := definition.Files[judgeCapturePath]
	if len(raw) == 0 {
		return nil, ErrHistoricalJudgeCapture
	}
	c, err := instructionpackage.Restore(raw)
	if err != nil {
		return nil, fmt.Errorf("captured judge instructions: %w", err)
	}
	if c.Role() != instructionpackage.ChallengerRole {
		return nil, errors.New("captured judge role differs")
	}
	if !bytes.Equal(c.Instructions(), definition.Files[judgeLegacyRolePath]) {
		return nil, errors.New("captured judge role differs from retained resource")
	}
	expected := map[string]bool{judgeCapturePath: true, judgeLegacyRolePath: true}
	for _, f := range c.Snapshot().Files {
		expected["judge/"+f.Path] = true
	}
	names := make([]string, 0, len(definition.Files))
	for name := range definition.Files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if strings.HasPrefix(name, "judge/") && !expected[name] {
			return nil, fmt.Errorf("unexpected captured judge resource %s", name)
		}
	}
	for _, f := range c.Snapshot().Files {
		if !bytes.Equal(f.Content, definition.Files["judge/"+f.Path]) {
			return nil, fmt.Errorf("captured judge resource %s differs", f.Path)
		}
	}
	return c, nil
}

func captureWorkflowJudge(ctx context.Context, source fs.FS, result *WorkflowDefinition, total *int, paths []config.Paths) error {
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
	var p *instructionpackage.Package
	var err error
	if len(paths) > 0 {
		p, err = instructionpackage.New(ctx, paths[0])
	} else {
		p, err = instructionpackage.LoadShipped(ctx, "")
	}
	if err != nil {
		return fmt.Errorf("load judge package: %w", err)
	}
	capture, _, err := p.Capture(ctx, instructionpackage.ChallengerRole)
	if err != nil {
		return fmt.Errorf("capture judge instructions: %w", err)
	}
	raw, err := capture.Encode()
	if err != nil {
		return err
	}
	files := map[string][]byte{judgeCapturePath: raw, judgeLegacyRolePath: capture.Instructions()}
	for _, file := range capture.Snapshot().Files {
		files["judge/"+file.Path] = file.Content
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
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
