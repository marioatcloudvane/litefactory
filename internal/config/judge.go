package config

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Thinking is a native provider effort request. Empty leaves the native default unset.
type Thinking string

// JudgeOptions contains owner overrides; empty fields inherit configured defaults.
type JudgeOptions struct {
	Agent    string   `json:"agent,omitempty" yaml:"agent"`
	Model    string   `json:"model,omitempty" yaml:"model"`
	Thinking Thinking `json:"thinking,omitempty" yaml:"thinking"`
}

// JudgeSettings configures shared advisory judging, independent of workflow modes.
type JudgeSettings struct {
	JudgeOptions `yaml:",inline"`
	Providers    map[Kind]JudgeProviderSettings `yaml:"providers"`
}

// JudgeProviderSettings supplies native thinking defaults and provider-scoped model aliases.
type JudgeProviderSettings struct {
	Thinking Thinking          `yaml:"thinking"`
	Aliases  map[string]string `yaml:"aliases"`
}

// JudgeInvocation records the resolved native request and any availability fallback.
type JudgeInvocation struct {
	Provider       Kind     `json:"provider"`
	Model          string   `json:"model"`
	Thinking       Thinking `json:"thinking,omitempty"`
	Selection      string   `json:"selection"`
	FallbackReason string   `json:"fallback_reason,omitempty"`
}

// ValidateThinking rejects unsupported native effort without silently lowering it.
func ValidateThinking(provider Kind, model string, thinking Thinking) error {
	if provider == KindClaude && (strings.HasPrefix(model, "gpt-") || strings.HasPrefix(model, "o3") || strings.HasPrefix(model, "o4")) || provider == KindCodex && (strings.HasPrefix(model, "claude-") || model == "opus" || model == "sonnet" || model == "haiku") {
		return fmt.Errorf("model %q is incompatible with %s", model, provider)
	}
	if thinking == "" {
		return nil
	}
	// Capabilities were checked on 2026-10-14: Claude's official model effort
	// table and the installed Codex 0.160.0 model catalog. Unknown literals remain
	// usable with native defaults, but do not inherit effort from a name prefix.
	var values string
	switch provider {
	case KindClaude:
		switch model {
		case "claude-opus-4-6", "claude-sonnet-4-6":
			values = " low medium high max "
		case "opus", "sonnet", "haiku", "claude-opus-4-7", "claude-opus-4-8", "claude-opus-5", "claude-opus-5-5", "claude-sonnet-5", "claude-sonnet-5-5", "claude-haiku-5-5", "claude-fable-5", "claude-fable-5-1":
			values = " low medium high xhigh max "
		}
	case KindCodex:
		switch model {
		case "gpt-6.1-sol", "gpt-6-astra", "gpt-6-sol", "gpt-5.6-sol", "gpt-5.6-terra":
			values = " low medium high xhigh max ultra "
		case "gpt-6-luna", "gpt-reserve", "gpt-5.6-luna", "codex-auto-review":
			values = " low medium high xhigh max "
		}
	default:
		return fmt.Errorf("unsupported judge provider %q", provider)
	}
	if values == "" {
		return fmt.Errorf("thinking capability is not verified for %s model %q; leave thinking unset", provider, model)
	}
	if !strings.Contains(values, " "+string(thinking)+" ") {
		return fmt.Errorf("unsupported thinking %q for %s model %q", thinking, provider, model)
	}
	return nil
}

// ResolveJudge applies request, shared judge and provider defaults in that order.
func (c *Config) ResolveJudge(request JudgeOptions, taskProvider, sourceProvider Kind) (JudgeInvocation, error) {
	selection := request.Agent
	if selection == "" {
		selection = c.Judge.Agent
	}
	if selection == "" {
		selection = string(taskProvider)
	}
	provider := Kind(selection)
	if selection == "other" {
		switch sourceProvider {
		case KindClaude:
			provider = KindCodex
		case KindCodex:
			provider = KindClaude
		default:
			return JudgeInvocation{}, errors.New("other requires a known saved source provider")
		}
	}
	return c.resolveJudgeProvider(request, provider, selection)
}

func (c *Config) resolveJudgeProvider(request JudgeOptions, provider Kind, selection string) (JudgeInvocation, error) {
	if provider != KindClaude && provider != KindCodex {
		return JudgeInvocation{}, fmt.Errorf("unsupported judge provider %q", provider)
	}

	model := request.Model
	if model == "" {
		model = c.Judge.Model
	}
	if model == "" {
		model = string(TierFlagship)
	}
	if Tier(model).Valid() {
		resolved, ok := c.Tiers.ResolveModel(provider, Tier(model))
		if !ok || resolved == "" {
			return JudgeInvocation{}, fmt.Errorf("judge model tier %q is not configured for %s", model, provider)
		}
		model = resolved
	} else if alias, ok := c.Judge.Providers[provider].Aliases[model]; ok {
		model = alias
	}
	if strings.TrimSpace(model) == "" || strings.ContainsAny(model, "\r\n\x00") {
		return JudgeInvocation{}, errors.New("invalid judge model")
	}
	thinking := request.Thinking
	if thinking == "" {
		thinking = c.Judge.Thinking
	}
	if thinking == "" {
		thinking = c.Judge.Providers[provider].Thinking
	}
	if err := ValidateThinking(provider, model, thinking); err != nil {
		return JudgeInvocation{}, err
	}
	return JudgeInvocation{Provider: provider, Model: model, Thinking: thinking, Selection: selection}, nil
}

// JudgeFallback resolves the sole availability fallback. The caller must classify
// the launch as unavailable, never malformed output, refusal or budget exhaustion.
func (c *Config) JudgeFallback(request JudgeOptions, sourceProvider Kind, previous JudgeInvocation, reason string) (JudgeInvocation, error) {
	if previous.Selection != "other" || previous.FallbackReason != "" || strings.TrimSpace(reason) == "" || previous.Provider == sourceProvider {
		return JudgeInvocation{}, errors.New("judge availability fallback is not permitted")
	}
	result, err := c.resolveJudgeProvider(request, sourceProvider, "other")
	result.FallbackReason = reason
	return result, err
}

func (c *Config) validateJudge() []error {
	var errs []error
	if c.Judge.Agent != "" && c.Judge.Agent != "claude" && c.Judge.Agent != "codex" && c.Judge.Agent != "other" {
		errs = append(errs, errors.New("judge.agent must be claude, codex or other"))
	}
	providers := make([]Kind, 0, len(c.Judge.Providers))
	for provider := range c.Judge.Providers {
		providers = append(providers, provider)
	}
	sort.Slice(providers, func(i, j int) bool { return providers[i] < providers[j] })
	for _, provider := range providers {
		settings := c.Judge.Providers[provider]
		if provider != KindClaude && provider != KindCodex {
			errs = append(errs, fmt.Errorf("judge.providers: unsupported provider %q", provider))
			continue
		}
		model, _ := c.Tiers.ResolveModel(provider, TierFlagship)
		if err := ValidateThinking(provider, model, settings.Thinking); err != nil {
			errs = append(errs, err)
		}
		aliases := make([]string, 0, len(settings.Aliases))
		for alias := range settings.Aliases {
			aliases = append(aliases, alias)
		}
		sort.Strings(aliases)
		for _, alias := range aliases {
			value := settings.Aliases[alias]
			if strings.TrimSpace(alias) == "" || strings.TrimSpace(value) == "" {
				errs = append(errs, errors.New("judge provider aliases require a name and model"))
			}
		}
	}
	if c.Judge.Thinking != "" {
		switch c.Judge.Thinking {
		case "low", "medium", "high", "max", "xhigh", "ultra":
		default:
			errs = append(errs, fmt.Errorf("judge.thinking: unsupported effort %q", c.Judge.Thinking))
		}
	}
	return errs
}
