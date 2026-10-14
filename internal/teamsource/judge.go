package teamsource

import "embed"

//go:embed judge/challenger.md
var judgeAssets embed.FS

// JudgeInstructions returns shipped advisory role bytes for captured native delivery.
func JudgeInstructions() ([]byte, error) { return judgeAssets.ReadFile("judge/challenger.md") }
