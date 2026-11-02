package contracts

// BootstrapResult is the local initialization summary. Completed remains true
// if initialization finished but closing its database subsequently failed.
type BootstrapResult struct {
	Home, ConfigPath, DBPath string
	WrittenConfig            bool
	OwnerLogin               string
	OwnerID                  int64
	BotLogin                 string
	BotID                    int64
	HasBot, Completed        bool
}

// BootstrapWarningKind identifies the existing nonfatal bootstrap warnings.
type BootstrapWarningKind string

// The following constants retain the existing local operation states.
const (
	BootstrapNoToken         BootstrapWarningKind = "no-token"
	BootstrapTokenFailed     BootstrapWarningKind = "token-failed"
	BootstrapKeychainMissing BootstrapWarningKind = "keychain-missing"
)

// BootstrapWarning contains safe facts for a local bootstrap warning.
type BootstrapWarning struct {
	Kind                         BootstrapWarningKind
	ConfigPath, Service, Account string
	Err                          error
}

// BootstrapFailureKind identifies the bootstrap setup failure to map at the CLI.
type BootstrapFailureKind string

// The following constants retain the existing local operation states.
const (
	BootstrapConfigFailure BootstrapFailureKind = "config"
	BootstrapOwnerFailure  BootstrapFailureKind = "owner"
	BootstrapSameAccount   BootstrapFailureKind = "same-account"
)

// BootstrapFailure separates existing setup errors from infrastructure errors.
type BootstrapFailure struct {
	Kind        BootstrapFailureKind
	Err         error
	Login       string
	UserID      int64
	Interrupted bool
}

func (e *BootstrapFailure) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return "bot and owner share the same numeric identity"
}
func (e *BootstrapFailure) Unwrap() error { return e.Err }

// AgentCredentialView contains only credential source labels, never secrets.
type AgentCredentialView struct{ Description, EnvName string }

// AgentStartView omits the agent environment because it may contain a token.
type AgentStartView struct {
	Label, PlistPath, Domain                       string
	ProgramArguments                               []string
	Path, Home, StdoutPath, StderrPath, ConfigPath string
	Replaced                                       bool
	Credential                                     AgentCredentialView
}

// AgentStopView describes the local service unload outcome.
type AgentStopView struct {
	Unloaded                  bool
	Target, Domain, PlistPath string
}

// AgentStatusView describes the installed and running local service.
type AgentStatusView struct {
	Label, Domain, PlistPath   string
	Installed, Loaded, Running bool
	PID                        int
	State                      string
}

// LocalAdminUnavailable represents an existing local setup refusal.
type LocalAdminUnavailable struct{ Err error }

func (e *LocalAdminUnavailable) Error() string { return e.Err.Error() }
func (e *LocalAdminUnavailable) Unwrap() error { return e.Err }
