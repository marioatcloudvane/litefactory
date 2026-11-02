package turn

import "github.com/cloudvane/litefactory/internal/config"

// claudeEveryTier is a tier table that resolves model for every Claude tier.
// Workflow fixtures that run the live team sources use it so a role's tier
// choice in its frontmatter (an owner decision, e.g. #1293) never decides
// whether a test's agent can start.
func claudeEveryTier(model string) map[config.Kind]map[config.Tier]string {
	return map[config.Kind]map[config.Tier]string{config.KindClaude: {
		config.TierFlagship: model,
		config.TierBalanced: model,
		config.TierFast:     model,
	}}
}
