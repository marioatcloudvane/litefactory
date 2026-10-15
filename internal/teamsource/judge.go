package teamsource

import (
	"embed"
	"errors"
	"slices"
)

//go:embed judge/challenger.md
var judgeAssets embed.FS

// JudgeInstructions returns shipped advisory role bytes for captured native delivery.
func JudgeInstructions() ([]byte, error) { return judgeAssets.ReadFile("judge/challenger.md") }

// WorkflowJudgeInstructions reads the challenger pinned by workflow capture.
func WorkflowJudgeInstructions(definition *WorkflowDefinition) ([]byte, error) {
	if definition == nil {
		return nil, errors.New("judge workflow definition is missing")
	}
	role, ok := definition.Files["judge/challenger.md"]
	if !ok || len(role) == 0 {
		return nil, errors.New("captured judge instructions are missing")
	}
	return slices.Clone(role), nil
}
