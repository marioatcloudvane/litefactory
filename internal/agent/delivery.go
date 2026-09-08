package agent

import (
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
)

// Delivery names a retained immutable instruction bundle. It contains no
// credentials and no vendor-specific representation crossing this boundary.
type Delivery struct {
	Root     string           `json:"root"`
	Identity string           `json:"identity"`
	Roles    []string         `json:"roles"`
	Skills   []string         `json:"skills"`
	Evidence DeliveryEvidence `json:"evidence"`
}

// DeliveryEvidence stays in per-run metadata even if an operator later removes
// a retained bundle. All URLs here are validated credential-free source names.
type DeliveryEvidence struct {
	Team           string               `json:"team"`
	Mode           string               `json:"mode"`
	SourceURL      string               `json:"source_url,omitempty"`
	SourceAlias    string               `json:"source_alias,omitempty"`
	Version        string               `json:"version,omitempty"`
	ManifestDigest string               `json:"manifest_digest,omitempty"`
	SnapshotDigest string               `json:"snapshot_digest"`
	SourceBytes    int                  `json:"source_bytes"`
	Specialists    []SpecialistEvidence `json:"specialists"`
	ResourceHashes map[string]string    `json:"resource_hashes"`
}

// SpecialistEvidence records the resolved source and local model for one role.
type SpecialistEvidence struct {
	ID         string `json:"id"`
	Tier       string `json:"tier"`
	Model      string `json:"model"`
	SourcePath string `json:"source_path"`
	SourceHash string `json:"source_hash"`
	Override   bool   `json:"override"`
}

// Clone prevents request and metadata copies from sharing mutable containers.
func (d Delivery) Clone() Delivery {
	d.Roles = slices.Clone(d.Roles)
	d.Skills = slices.Clone(d.Skills)
	d.Evidence.Specialists = slices.Clone(d.Evidence.Specialists)
	d.Evidence.ResourceHashes = maps.Clone(d.Evidence.ResourceHashes)
	return d
}

var deliveryRoleName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func (d Delivery) validate() error {
	if !filepath.IsAbs(d.Root) || d.Identity == "" {
		return fmt.Errorf("delivery requires an absolute retained bundle and identity")
	}
	for _, id := range d.Roles {
		if !deliveryRoleName.MatchString(id) {
			return fmt.Errorf("delivery role has unsafe name")
		}
	}
	for _, skill := range d.Skills {
		if !filepath.IsLocal(skill) {
			return fmt.Errorf("delivery skill path escapes bundle")
		}
	}
	return nil
}
