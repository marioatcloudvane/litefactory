package owneraction

// ActionDefinition exposes only classified operations enabled by the domain.
type ActionDefinition struct {
	Command   string               `json:"command"`
	Subaction string               `json:"subaction,omitempty"`
	Tier      string               `json:"tier"`
	Scope     string               `json:"scope"`
	Arguments []ArgumentDefinition `json:"arguments"`
}

// ArgumentDefinition describes scalar input accepted by existing validation.
type ArgumentDefinition struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
	MaxBytes int    `json:"max_bytes"`
}

// Catalog is optional discovery; unavailable domains publish no actions.
func (s *Service) Catalog() []ActionDefinition {
	if catalog, ok := s.domain.(interface{ ActionCatalog() []ActionDefinition }); ok {
		return catalog.ActionCatalog()
	}
	return nil
}
