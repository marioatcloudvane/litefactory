package config

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// DefaultTeamsIndexURL names the independently published first-party catalog.
const DefaultTeamsIndexURL = "https://github.com/marioatcloudvane/litefactory/teams/teams.json"

// Teams configures operator-owned distribution sources, not repository inputs.
type Teams struct {
	IndexURL      string                `yaml:"index_url"`
	DefaultSource string                `yaml:"default_source"`
	Sources       map[string]TeamSource `yaml:"sources"`
}

// TeamSource holds credential references only. Consumers resolve them at fetch time.
type TeamSource struct {
	IndexURL      string   `yaml:"index_url"`
	TokenEnv      string   `yaml:"token_env"`
	TokenKeychain Keychain `yaml:"token_keychain"`
}

// Source selects an explicit alias/HTTPS URL, then the default alias, then index_url.
// Loading applies Defaults before decoding; an explicitly empty index_url is invalid.
func (t Teams) Source(explicit string) (string, TeamSource, error) {
	name := explicit
	if name == "" {
		name = t.DefaultSource
	}
	if name != "" {
		if source, ok := t.Sources[name]; ok {
			return name, source, nil
		}
		if err := validateTeamURL(name); err != nil {
			return "", TeamSource{}, fmt.Errorf("unknown team source or invalid HTTPS URL")
		}
		return "", TeamSource{IndexURL: name}, nil
	}
	if err := validateTeamURL(t.IndexURL); err != nil {
		return "", TeamSource{}, err
	}
	return "", TeamSource{IndexURL: t.IndexURL}, nil
}

func validateTeamURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.Opaque != "" || strings.ContainsAny(raw, "\\\r\n\t ") {
		return fmt.Errorf("must be an HTTPS file URL without credentials, query or fragment")
	}
	return nil
}

func (t Teams) validate() []error {
	var errs []error
	if err := validateTeamURL(t.IndexURL); err != nil {
		errs = append(errs, fmt.Errorf("teams.index_url: %w", err))
	}
	if t.DefaultSource != "" {
		if _, ok := t.Sources[t.DefaultSource]; !ok {
			errs = append(errs, fmt.Errorf("teams.default_source: must name a configured source"))
		}
	}
	keys := make([]string, 0, len(t.Sources))
	for key := range t.Sources {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		s := t.Sources[key]
		if !regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`).MatchString(key) {
			errs = append(errs, fmt.Errorf("teams.sources: invalid alias"))
		}
		if err := validateTeamURL(s.IndexURL); err != nil {
			errs = append(errs, fmt.Errorf("teams.sources.%s.index_url: %w", key, err))
		}
		keychain := s.TokenKeychain.Service != "" || s.TokenKeychain.Account != ""
		if keychain && (s.TokenKeychain.Service == "" || s.TokenKeychain.Account == "" || s.TokenEnv != "") {
			errs = append(errs, fmt.Errorf("teams.sources.%s: specify a complete keychain reference or token_env, not both", key))
		}
		if s.TokenEnv != "" && !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(s.TokenEnv) {
			errs = append(errs, fmt.Errorf("teams.sources.%s.token_env: invalid environment variable name", key))
		}
	}
	return errs
}
