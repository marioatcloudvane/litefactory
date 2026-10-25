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
	paths := []string{CataloguePath, rolePath(role)}
	if role == ConversationInboxOrganizerRole {
		paths = append(paths, InboxOrganizerSchemaPath)
	}
	if role == StandardsAuthorRole {
		paths = append(paths, StandardsAuthorSchemaPath)
	}
	return paths
}

// retainedCapturePaths accepts current closures and historical judge resources.
func retainedCapturePaths(role string, files int) []string {
	paths := capturePaths(role)
	if role == JudgeRole && files == len(paths)+len(legacyJudgeResourcePaths) {
		paths = append(paths, legacyJudgeResourcePaths...)
	}
	return paths
}
