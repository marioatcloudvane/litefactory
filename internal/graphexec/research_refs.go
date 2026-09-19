package graphexec

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/cloudvane/litefactory/internal/config"
)

var researchName = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

// ResearchName accepts bounded bare names. Shared skills remain inside the
// built-in package; role references deliberately have no alternate search path.
func ResearchName(value string, skill bool) bool {
	if skill {
		value = strings.TrimPrefix(value, "shared/")
	}
	return len(value) <= 64 && researchName.MatchString(value)
}

// ResearchReferences validates all independent node reference fields. Defaults
// are resolved by the owner of shipped content before checking effective values.
func ResearchReferences(attrs map[string]string) (string, []string, error) {
	role := attrs["lf_agent_markdown"]
	var faults []error
	if !ResearchName(role, false) {
		faults = append(faults, fmt.Errorf("lf_agent_markdown %q: require a kebab-case bare name without .md", role))
	}
	var skills []string
	seen := map[string]bool{}
	if raw, ok := attrs["lf_skills"]; ok && raw != "" {
		for _, value := range strings.Split(raw, ",") {
			value = strings.TrimSpace(value)
			if !ResearchName(value, true) || seen[value] {
				faults = append(faults, fmt.Errorf("lf_skills: invalid or duplicate skill %q", value))
				continue
			}
			seen[value] = true
			skills = append(skills, value)
		}
	}
	return role, skills, errors.Join(faults...)
}

// ValidateResearchEffective checks the launch values after inheritance.
func ValidateResearchEffective(task string, tier config.Tier, limit int) error {
	var faults []error
	if strings.TrimSpace(task) == "" || len(task) > 65536 {
		faults = append(faults, errors.New("effective Research task must contain 1–65536 bytes"))
	}
	if !tier.Valid() {
		faults = append(faults, fmt.Errorf("invalid effective Research model tier %q", tier))
	}
	if limit <= 0 {
		faults = append(faults, errors.New("research agent requires a finite positive lf_limit"))
	}
	return errors.Join(faults...)
}
