package contracts

import (
	"time"

	"github.com/cloudvane/litefactory/internal/ticket"
)

// ProjectConnectionView contains safe retained connection identity and availability.
type ProjectConnectionView struct {
	ID, RepoID, Revision int64
	Role                 string
	Provider             ticket.Provider
	Origin               ticket.Origin
	Destination          string
	Enabled              bool
	Provenance           string
}

// ProjectObservationView contains bounded provider connection health evidence.
type ProjectObservationView struct {
	VersionID              int64
	CheckedAt              time.Time
	Status, Reason, Remedy string
	Capabilities           []string
}

// ProjectConnectionHealth distinguishes unchecked state from retained provider observations.
type ProjectConnectionHealth struct {
	Enabled, Checked bool
	Observation      ProjectObservationView
}

// ProjectBindingConflict describes a retained binding that cannot inherit a connection.
type ProjectBindingConflict struct {
	BindingID, PriorVersionID, CurrentVersionID int64
	Reason                                      string
}

// ProjectImportView describes retained legacy import status.
type ProjectImportView struct{ Status, Reason string }

// ConnectionPresentation preserves the existing ordered local command output.
type ConnectionPresentation struct {
	Connection      func(ProjectConnectionView) error
	BindingConflict func(ProjectBindingConflict) error
	Health          func(ProjectConnectionHealth) error
	Import          func(ProjectImportView) error
}

// SetProjectConnectionRequest describes one explicit versioned connection change.
type SetProjectConnectionRequest struct {
	Role                                                             string
	Provider, Workspace, Team, Project, AuthKind, ModeLabels, States string
	// CodingAgentLabels is JSON (agent name to label ID); BugLabel is the
	// optional existing Linear label ID that means bug. Linear only.
	CodingAgentLabels, BugLabel               string
	Revision                                  int64
	Origin, Destination, Credential, RemoteID string
}

// ProjectBindingRequest describes one qualified ticket binding change.
type ProjectBindingRequest struct {
	ID, Revision             int64
	Key, RemoteID, Container string
	Number                   int
}

// ProjectBindingView contains a retained provider-qualified ticket binding.
type ProjectBindingView struct {
	ID, RepoID, ConnectionVersionID, Revision int64
	Ref                                       ticket.Ref
	Enabled                                   bool
	Provenance                                string
}

// ProjectApproverView contains one verified provider-qualified owner appointment.
type ProjectApproverView struct {
	ID, RepoID, Revision    int64
	Actor                   ticket.Identity
	Handle                  string
	VerifiedAt, ActivatedAt time.Time
	Provenance              string
	RequireFresh            bool
}

// ProjectApproverBlocker identifies work that prevents an owner appointment change.
type ProjectApproverBlocker struct{ Kind, Key string }

// ProjectApproverInspection contains current appointment, connection and blocker evidence.
type ProjectApproverInspection struct {
	Appointment    ProjectApproverView
	Connection     ProjectConnectionView
	Present        bool
	Blockers       []ProjectApproverBlocker
	Initialization ProjectImportView
}

// ProjectUsageError marks invalid local project command input.
type ProjectUsageError struct{ Err error }

func (e *ProjectUsageError) Error() string { return e.Err.Error() }
func (e *ProjectUsageError) Unwrap() error { return e.Err }

// ProjectUnavailable marks an existing project operation precondition refusal.
type ProjectUnavailable struct{ Err error }

func (e *ProjectUnavailable) Error() string { return e.Err.Error() }
func (e *ProjectUnavailable) Unwrap() error { return e.Err }

// RegisterProjectRequest contains explicit local clone registration overrides.
type RegisterProjectRequest struct{ Path, BaseBranch, WorktreeRoot, FeaturePrefix string }

// ProjectRegistrationView contains resolved registration values and their sources.
type ProjectRegistrationView struct {
	Path, Ref, RemoteURL, BaseBranch, BaseBranchSource, FeaturePrefix, WorktreeRoot, WorktreeRootSource string
}

// RepositoryView contains the repository facts needed for local list output.
type RepositoryView struct {
	ID                                          int64
	Path, Forge, Slug, BaseBranch, WorktreeRoot string
	LastFetchAt                                 time.Time
}

// RepositoryBaseChangeView reports one `repo base` outcome. Stale names the
// side that disagreed and was reconciled: "record", "connection" or both.
type RepositoryBaseChangeView struct {
	Repository         RepositoryView
	OldBase            string
	NewBase            string
	NoChange           bool
	Stale              []string
	KeptByStartedIssue int
}

// ProjectWebhookView contains safe webhook registration outcome and warnings.
type ProjectWebhookView struct {
	Description, Reason           string
	Failed, Skipped, Insecure, OK bool
}

// ProjectRegistrationPresentation preserves registration output before best-effort webhook work.
type ProjectRegistrationPresentation struct {
	Registration func(ProjectRegistrationView, bool)
	Webhook      func(RepositoryView, ProjectWebhookView)
}

// ProjectWebhookPresentation renders each bounded webhook repair outcome.
type ProjectWebhookPresentation struct {
	Empty  func()
	Result func(RepositoryView, ProjectWebhookView)
}

// ProjectSubscriptionRequest selects one versioned subscription operation.
type ProjectSubscriptionRequest struct {
	HasID                bool
	ProjectID, Revision  int64
	ID, Callback, Events string
	Enabled              bool
}

// ProjectSubscriptionView contains subscription health. It names the callback
// and the Keychain item of the signing secret, never the secret value.
type ProjectSubscriptionView struct {
	ID                                        string
	RepoID, Revision, ConnectionVersionID     int64
	Provider                                  ticket.Provider
	Origin                                    ticket.Origin
	Container                                 string
	DesiredEnabled                            bool
	ObservedState, RemoteHookID, Failure      string
	CallbackURL, SecretService, SecretAccount string
	CheckedAt, DeliveredAt                    time.Time
}

// ProjectRecoveryView contains observed subscription recovery progress.
type ProjectRecoveryView struct {
	Known                                                bool
	CompletedThrough, EnrollmentAt, ReconcileCompletedAt time.Time
}

// ProjectSubscriptionPresentation preserves subscription output before recovery lookup.
type ProjectSubscriptionPresentation struct {
	Subscription func(ProjectSubscriptionView) error
	Recovery     func(ProjectRecoveryView) error
	Empty        func(int64) error
}

// ProjectContainerClaim contains only the destination identity needed for verification.
type ProjectContainerClaim struct{ Destination, RemoteID string }

// ProjectDeliveryView contains the local delivery request acknowledgment.
type ProjectDeliveryView struct {
	ID         string
	Generation int
	Completed  bool
}

// ProjectDeliveryReceipt is the retained request receipt, not delivery authority.
type ProjectDeliveryReceipt struct {
	ID, Kind, ProposalKey, Body   string
	SessionID, ActorID, CommentID int64
	Generation                    int
	CreatedAt                     time.Time
	Completed                     bool
}

// ProjectRegistrationFailure marks clone validation errors for existing CLI exit mapping.
type ProjectRegistrationFailure struct{ Err error }

func (e *ProjectRegistrationFailure) Error() string { return e.Err.Error() }
func (e *ProjectRegistrationFailure) Unwrap() error { return e.Err }

// ProjectAccessGrantView is one revision of a plain-text grant. ClosedAt is the
// zero time while the revision is in force; CloseReason is replaced or revoked.
type ProjectAccessGrantView struct {
	ID, RepoID, Revision int64
	Actor                ticket.Identity
	Handle, Text         string
	CreatedAt, ClosedAt  time.Time
	CloseReason          string
}
