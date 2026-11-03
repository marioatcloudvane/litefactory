package contracts

// Project is registered project identity and read-only presentation metadata.
// It carries neither workspace paths nor retained connection authority.
type Project struct {
	ID                             int64
	Forge, Owner, Name, BaseBranch string
}

// Slug returns the registered owner/name locator.
func (p Project) Slug() string { return p.Owner + "/" + p.Name }
