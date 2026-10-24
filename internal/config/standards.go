package config

import (
	"fmt"
	"regexp"
	"strings"
)

// StandardsSettings groups owner-started standards analysis settings.
type StandardsSettings struct {
	LogAnalysis LogAnalysisSettings `yaml:"log_analysis"`
}

// LogAnalysisSettings configures where log analysis reports litefactory's own
// defects. Upstream is "github:owner/name"; empty shows upstream reports
// without filing them.
type LogAnalysisSettings struct {
	Upstream string `yaml:"upstream"`
}

var upstreamPattern = regexp.MustCompile(`^github:([A-Za-z0-9][A-Za-z0-9-]{0,38})/([A-Za-z0-9._-]{1,100})$`)

// UpstreamRepository returns the configured upstream GitHub owner and name.
// ok is false when no upstream is configured.
func (s LogAnalysisSettings) UpstreamRepository() (owner, name string, ok bool) {
	m := upstreamPattern.FindStringSubmatch(s.Upstream)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

func (s StandardsSettings) validate() []error {
	upstream := s.LogAnalysis.Upstream
	if upstream != "" && (!upstreamPattern.MatchString(upstream) || strings.HasSuffix(upstream, ".git")) {
		return []error{fmt.Errorf("standards.log_analysis.upstream: %q must be github:owner/name or empty", upstream)}
	}
	return nil
}
