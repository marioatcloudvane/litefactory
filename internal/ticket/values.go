package ticket

import "time"

// Actor carries descriptive account data alongside its authority identity.
type Actor struct {
	Identity Identity `json:"identity"`
	Handle   string   `json:"handle"`
	IsBot    bool     `json:"is_bot"`
}

// Snapshot is one observed ticket. Revision is an adapter-owned content token;
// it does not promise atomic compare-and-swap support at the remote service.
type Snapshot struct {
	Ref        Ref    `json:"ref"`
	DisplayKey string `json:"display_key"`
	BrowserURL string `json:"browser_url"`
	Title      string `json:"title"`
	// ProviderBody and Representation retain the provider bytes separately from canonical managed content.
	ProviderBody   string    `json:"provider_body,omitempty"`
	Representation string    `json:"representation,omitempty"`
	Body           string    `json:"body"`
	State          string    `json:"state"`
	StateReason    string    `json:"state_reason"`
	Author         Actor     `json:"author"`
	Assignees      []Actor   `json:"assignees"`
	Labels         []string  `json:"labels"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	Revision       string    `json:"revision"`
}

// AssignedTo compares qualified immutable account identities, never handles.
func (s Snapshot) AssignedTo(identity Identity) bool {
	for _, actor := range s.Assignees {
		if actor.Identity.Equal(identity) {
			return true
		}
	}
	return false
}

// Reply identity is scoped to its exact ticket and qualified author.
type Reply struct {
	ProviderBody   string    `json:"provider_body,omitempty"`
	Representation string    `json:"representation,omitempty"`
	Ticket         Ref       `json:"ticket"`
	Identity       Identity  `json:"identity"`
	Author         Actor     `json:"author"`
	Body           string    `json:"body"`
	BrowserURL     string    `json:"browser_url"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Cursor is an adapter-owned read position. Value must never be compared as a
// generic remote reply ID. Kind versions the provider's compatibility format.
type Cursor struct {
	Ticket Identity `json:"ticket"`
	Kind   string   `json:"kind"`
	Value  string   `json:"value"`
}

// ReplyPage is a bounded observation. Complete describes traversal completion,
// not proof that a possibly-sent remote mutation did not happen.
type ReplyPage struct {
	Replies  []Reply
	Next     Cursor
	Complete bool
}

// SnapshotPage retains a bounded provider observation, including entries
// excluded from the ticket projection (such as GitHub pull requests).
type SnapshotPage struct {
	Tickets  []Snapshot
	NextPage int
	More     bool
}
