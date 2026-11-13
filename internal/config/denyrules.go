package config

import (
	"errors"
	"slices"

	"github.com/cloudvane/litefactory/internal/denypolicy"
)

// Catalogue diagnostics include original syntax for correction. Remove
// credential-shaped values before parsing while scanSecrets checks the original
// configuration, so a syntax failure cannot disclose a rejected credential.
func validateDenyCatalogue(catalogue denypolicy.Catalogue) error {
	safe := make(denypolicy.Catalogue, len(catalogue))
	var faults []error
	names := make([]string, 0, len(catalogue))
	for name := range catalogue {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if class, ok := ClassifySecret(name); ok {
			faults = append(faults, &SecretError{Key: "settings.denyrules[group name]", Class: class})
			continue
		}
		group := catalogue[name]
		entries := slices.Clone(group.Deny)
		for i, entry := range entries {
			if _, ok := ClassifySecret(entry); ok {
				entries[i] = "Agent"
			}
		}
		safe[name] = denypolicy.Group{Deny: entries}
	}
	faults = append(faults, denypolicy.ValidateCatalogue(safe))
	return errors.Join(faults...)
}
