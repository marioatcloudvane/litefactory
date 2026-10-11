package ticket

// DiscoveryCursor is an adapter-versioned continuation bound to one source.
// Value is opaque; consumers persist it without interpreting remote ordering.
type DiscoveryCursor struct {
	Provider  Provider `json:"provider"`
	Origin    Origin   `json:"origin"`
	Container string   `json:"container"`
	Kind      string   `json:"kind"`
	Value     string   `json:"value"`
}

// ChangePage is one bounded discovery observation. Locators from reply scans
// require an authoritative resolve before work can be admitted.
type ChangePage struct {
	Tickets  []Snapshot
	Locators []Locator
	Next     DiscoveryCursor
	Complete bool
}

// Subscription carries observed nonsecret remote subscription configuration.
// Providers cannot return a signing secret, so this cannot verify secret equality.
type Subscription struct {
	ID          Identity
	Container   string
	CallbackURL string
	Events      []string
	Enabled     bool
	ContentType string
}

// SubscriptionSpec is desired nonsecret configuration. Credentials are supplied
// separately through the existing secret boundary and must never be persisted here.
type SubscriptionSpec struct {
	CallbackURL string
	Events      []string
	Enabled     bool
}
