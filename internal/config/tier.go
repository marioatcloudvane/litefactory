package config

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// The capability-tier vocabulary (US-6).
//
// A specialist's definition used to say `model: opus` or `model: sonnet` —
// values that mean something only if the tool running it is Claude. Tier
// replaces that with a vendor-neutral vocabulary, and [Tiers.ModelByTier] is
// the one litefactory-owned table that turns a (tool, tier) pair back into
// the literal --model value that tool takes.
//
// This file is stdlib-only, like everything in this package except the YAML
// decode itself (see purity_test.go).

// Tier is a vendor-neutral capability tier: how strong a model a run needs,
// not which vendor's model name spells that. It is a closed set — flagship,
// balanced, fast — validated wherever it appears in configuration; an
// unrecognised value is an [UnknownTierError], never a silent fallback
// (AC-6.1).
type Tier string

// The closed set of tiers.
const (
	// TierFlagship is a vendor's strongest available model.
	TierFlagship Tier = "flagship"
	// TierBalanced is a vendor's mid-weight model: capable enough for most
	// specialist work, cheaper than flagship. It is the default a session
	// runs on when nothing more specific is configured.
	TierBalanced Tier = "balanced"
	// TierFast is a vendor's cheapest, quickest model.
	TierFast Tier = "fast"
)

// allTiers lists the closed set in the order error text should name them:
// strongest first.
var allTiers = []Tier{TierFlagship, TierBalanced, TierFast}

// Valid reports whether t is one of the three tiers. The zero value is not
// valid — callers that treat "" as "unset, use the default" check for that
// themselves, before calling Valid, so this stays a strict membership test.
func (t Tier) Valid() bool {
	return slices.Contains(allTiers, t)
}

// String renders the tier for logs and error text.
func (t Tier) String() string { return string(t) }

// Kind names which coding-agent-tool implementation a [Tiers.ModelByTier]
// entry applies to — "claude", eventually "codex".
//
// It deliberately mirrors internal/agent.Kind's string values rather than
// importing that type: internal/config is a leaf package every other
// package may depend on (enforced by purity_test.go and stated in the
// package map in .aiteam/APP_CONTEXT.md), so it cannot import
// internal/agent without becoming non-leaf. Like agent.Kind, it carries no
// closed vocabulary of its own — a third tool is an additional map key here,
// never a new case to add.
type Kind string

const (
	// KindClaude is the discriminator internal/daemon uses to key both this
	// package's [Tiers.ModelByTier] and the internal/turn.RunnerSet the
	// Claude [agent.Runner] implementation is registered under. The two
	// spellings (this constant, and internal/store.AgentClaude) are kept as
	// separate string constants — config cannot import internal/store any
	// more than it can import internal/agent — and are asserted to agree
	// only where they actually meet: internal/daemon's wiring, not here.
	KindClaude Kind = "claude"
	// KindCodex is the discriminator for the Codex implementation, once one
	// exists. Its tier→model values are intentionally absent from
	// [defaultModelByTier] — see that function's doc.
	KindCodex Kind = "codex"
)

// ErrUnknownTier marks every "this string is not flagship, balanced or fast"
// failure. The command layer does not special-case it the way it does
// [ErrLiteralSecret]; it surfaces through the ordinary joined [Config.Validate]
// error like any other bad value.
var ErrUnknownTier = errors.New("unknown capability tier")

// UnknownTierError reports one configuration value that named a tier outside
// the closed set (AC-6.1). Key is the dotted path to the offending value,
// never the value itself embedded in a way that hides which key is wrong.
type UnknownTierError struct {
	Key   string
	Value string
}

func (e *UnknownTierError) Error() string {
	names := make([]string, len(allTiers))
	for i, t := range allTiers {
		names[i] = string(t)
	}
	return fmt.Sprintf("%s: %q is not a tier; must be one of %s", e.Key, e.Value, strings.Join(names, ", "))
}

func (e *UnknownTierError) Unwrap() error { return ErrUnknownTier }

// Tiers is the one litefactory-owned table answering "for this tool, at this
// tier, which concrete model runs" (T-6.1).
//
// It is deliberately not where a specialist's tier assignment lives —
// .aiteam/AGENT_TIERS.md is, because .claude/agents/*.md frontmatter is
// parsed by Claude Code itself, not by litefactory: an unrecognised tier:
// key there would be silently ignored and the specialist would silently
// fall back to the session default, which is exactly the failure mode a
// litefactory-owned table exists to avoid. A specialist's own
// .claude/agents/*.md file keeps a literal model: value; T-6.6's test is
// what keeps that value honest against this map.
type Tiers struct {
	// ModelByTier maps a tool ([Kind]) and a capability tier ([Tier]) to the
	// literal --model value that tool takes for it. Only "claude" is
	// populated by [Defaults] today: "codex" waits on a live measurement of
	// Codex's own tier/model names (T-6.5, AC-6.6), not on a web-researched
	// guess.
	ModelByTier map[Kind]map[Tier]string `yaml:"model_by_tier"`
}

// ResolveModel returns the model string configured for kind at tier, and
// whether one was found.
//
// A miss is not resolved to a fallback here: the caller — the selected
// [agent.Runner] implementation, at the point it resolves its own --model —
// decides what a miss means for it, because only it knows whether "fall back
// to some default" or "refuse to start" is the right answer for that tool.
func (t Tiers) ResolveModel(kind Kind, tier Tier) (string, bool) {
	byTier, ok := t.ModelByTier[kind]
	if !ok {
		return "", false
	}
	model, ok := byTier[tier]
	return model, ok
}

// defaultModelByTier is the tier table [Defaults] returns and
// [DefaultFileContents] writes.
//
// The Claude column is a pure relabelling of values that already existed:
// DefaultClaudeModel ("sonnet") is TierBalanced, and "opus"/"haiku" are the
// same two aliases claude.model's own documentation in template.go already
// names as valid --model values — no specialist's actual model assignment
// changes (AC-6.8). The Codex column is intentionally absent: T-6.5 is
// blocked on a live measurement of Codex's own tier/model names (AC-6.6);
// fabricating one from documentation or web knowledge is exactly the
// failure mode this whole feature exists to avoid.
func defaultModelByTier() map[Kind]map[Tier]string {
	return map[Kind]map[Tier]string{
		KindClaude: {
			TierFlagship: "opus",
			TierBalanced: DefaultClaudeModel,
			TierFast:     "haiku",
		},
	}
}

// sortedKinds orders a ModelByTier map's tool keys for deterministic
// iteration — Go randomises map order, and both validation error lists and
// the generated config.yaml need to be reproducible across runs.
func sortedKinds(m map[Kind]map[Tier]string) []Kind {
	keys := make([]Kind, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// sortedTiers orders one tool's tier keys for the same reason.
func sortedTiers(m map[Tier]string) []Tier {
	keys := make([]Tier, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
