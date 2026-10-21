package instructionpackage

const (
	// JudgeRole names the advisory challenger catalogue role.
	JudgeRole = "challenger"
	// ChallengerRole is the challenger catalogue role.
	ChallengerRole = JudgeRole
	// JudgeRolePath is the package-relative challenger role source.
	JudgeRolePath = "agents/challenger.md"
)

// legacyJudgeResourcePaths were part of the challenger closure while the judge
// read a factory evidence snapshot. Captures retained then (in judge requests
// and pinned workflow definitions) still restore; nothing reads these files.
var legacyJudgeResourcePaths = []string{"resources/judge/evidence-suffix.md", "resources/judge/pin-prefix.md"}

func capturePaths(role string) []string {
	return []string{CataloguePath, rolePath(role)}
}

// retainedCapturePaths is the closure a retained capture may carry: today's,
// or the historical challenger closure with its two resource files.
func retainedCapturePaths(role string, files int) []string {
	paths := capturePaths(role)
	if role == JudgeRole && files == len(paths)+len(legacyJudgeResourcePaths) {
		paths = append(paths, legacyJudgeResourcePaths...)
	}
	return paths
}
