package instructionpackage

const (
	// JudgeRole names the advisory challenger catalogue role.
	JudgeRole = "challenger"
	// ChallengerRole is the challenger catalogue role.
	ChallengerRole = JudgeRole
	// JudgeRolePath is the package-relative challenger role source.
	JudgeRolePath = "agents/challenger.md"
	// JudgePinPrefixPath is the fixed text before factory invocation JSON.
	JudgePinPrefixPath = "resources/judge/pin-prefix.md"
	// JudgeEvidenceSuffixPath is the fixed text after factory invocation JSON.
	JudgeEvidenceSuffixPath = "resources/judge/evidence-suffix.md"
)

func capturePaths(role string) []string {
	paths := []string{CataloguePath, rolePath(role)}
	if role == JudgeRole {
		paths = append(paths, JudgePinPrefixPath, JudgeEvidenceSuffixPath)
	}
	return paths
}
