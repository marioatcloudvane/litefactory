// Package connector provides a separate, owner-authorized MCP boundary.
// It never exposes the factory's agent or submission backends.
package connector

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io/fs"
	"log/slog"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/durableworkflow"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

// Scope is the read authority behind the factory_overview tool.
const Scope = "factory:overview"

// DiscussionScope authorizes task evidence for tasks in covered repositories.
const DiscussionScope = "task:discussion"

// CodeReviewScope independently authorizes committed code and published rules.
const CodeReviewScope = "task:code-review"

// Usage scopes grant independent recorded-usage access.
const (
	FactoryUsageScope    = "factory:usage"
	RepositoryUsageScope = "repository:usage"
	TaskUsageScope       = "task:usage"
)

// RepositoryActionsScope authorizes existing changes in explicitly selected repositories.
const RepositoryActionsScope = "repository:actions"

// StandardsManagementScope authorizes changes to the shared standards book.
const StandardsManagementScope = "standards:management"

// ScopeInfo names one scope the connector can issue and says in one plain line
// what it allows. The consent page shows the description next to the scope.
type ScopeInfo struct{ Name, Description string }

// KnownScopes lists every scope the connector can issue, in display order. A
// client may request any non-empty subset. The consent page offers all of
// them, pre-ticks the requested ones, and the owner ticks which to grant.
// store.ConnectorKnownScopes must list the same names.
var KnownScopes = []ScopeInfo{
	{Name: DiscussionScope, Description: "Read recorded status, results, questions, research, Plan and approval evidence for any task in the repositories selected below; the client names the task on each call. Read-only; it cannot change work or grant workflow approval."},
	{Name: CodeReviewScope, Description: "Read committed task/base code, reachable history and changes, and published standards and role counts for any task in the selected repositories, from that task's own retained code repository. Requires task:discussion; read-only."},
	{Name: Scope, Description: "Read the factory overview: health, current activity, open owner questions and reviews, and last week's publications. Read-only; it cannot change factory work."},
	{Name: FactoryUsageScope, Description: "Read all recorded factory usage and historical estimates, including work without a repository. Read-only."},
	{Name: RepositoryUsageScope, Description: "Read recorded usage and historical estimates for the repositories selected below, one repository per call. Read-only."},
	{Name: TaskUsageScope, Description: "Read recorded usage and historical estimates for any task in the selected repositories and its linked report family, including retired history and later persisted links. Read-only."},
	{Name: RepositoryActionsScope, Description: "Propose, confirm and inspect existing actions only for explicitly selected repositories. Binding actions also require the configured owner’s Discord approval."},
	{Name: StandardsManagementScope, Description: "Manage the installation-wide standards book, including drafting and publication. Repository context also requires separate read coverage. Publication requires Discord approval."},
}

func knownScope(name string) bool {
	return slices.ContainsFunc(KnownScopes, func(k ScopeInfo) bool { return k.Name == name })
}

func scopeNames() []string {
	names := make([]string, len(KnownScopes))
	for i, k := range KnownScopes {
		names[i] = k.Name
	}
	return names
}

// normalizeScope validates an RFC 6749 scope string against KnownScopes and
// returns it in catalogue order. Empty, repeated or unknown scopes are refused.
func normalizeScope(raw string) (string, bool) {
	tokens, ok := store.ConnectorScopes(raw)
	if !ok {
		return "", false
	}
	for _, token := range tokens {
		if !knownScope(token) {
			return "", false
		}
	}
	var out []string
	for _, k := range KnownScopes {
		if slices.Contains(tokens, k.Name) {
			out = append(out, k.Name)
		}
	}
	return strings.Join(out, " "), true
}

// Options supplies dependencies resolved by trusted daemon composition.
type Options struct {
	Workflows    *durableworkflow.Service
	Actions      *owneraction.Service
	Config       config.RemoteConnector
	DB           *store.DB
	Logger       *slog.Logger
	Now          func() time.Time
	LocalControl *LocalControl
	Overview     OverviewProvider
	TaskEvidence TaskEvidenceProvider
	Usage        UsageProvider
	CodeEvidence CodeEvidenceProvider
	GitHub       GitHubIdentityProvider
	CookieKey    []byte
	GitHubSecret string
}

// Server owns remote authentication independently of local factory authority.
type Server struct {
	actions      atomic.Pointer[owneraction.Service]
	workflows    atomic.Pointer[durableworkflow.Service]
	cfg          config.RemoteConnector
	db           *store.DB
	logger       *slog.Logger
	now          func() time.Time
	localControl *LocalControl
	overview     OverviewProvider
	taskEvidence TaskEvidenceProvider
	usage        UsageProvider
	codeEvidence CodeEvidenceProvider
	usageViews   usageCache
	guidance     fs.FS
	github       GitHubIdentityProvider
	cookie       cipher.AEAD
	transport    *transportState
}

// New refuses construction until security prerequisites and owner binding work.
func New(ctx context.Context, opts Options) (*Server, error) {
	if err := opts.Config.Validate(); err != nil {
		return nil, err
	}
	if !opts.Config.Enabled {
		return nil, errors.New("connector: remote access disabled")
	}
	if opts.DB == nil || opts.Logger == nil || opts.LocalControl == nil {
		return nil, errors.New("connector: required dependency unavailable")
	}
	if len(opts.CookieKey) != 32 {
		return nil, errors.New("connector: cookie key must be 32 bytes")
	}
	if opts.GitHub == nil && opts.GitHubSecret == "" {
		return nil, errors.New("connector: GitHub credential unavailable")
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	if _, err := opts.DB.ReconcileConnectorOwner(ctx, opts.Config.OwnerID, now()); err != nil {
		return nil, err
	}
	if err := opts.LocalControl.check(ctx, opts.DB, opts.Config.OwnerID); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(opts.CookieKey)
	if err != nil {
		return nil, errors.New("connector: invalid cookie key")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errors.New("connector: cookie encryption unavailable")
	}
	github := opts.GitHub
	if github == nil {
		github = &GitHubProvider{ClientID: opts.Config.GitHubClientID, ClientSecret: opts.GitHubSecret}
	}
	opts.Config.AllowedOrigins = slices.Clone(opts.Config.AllowedOrigins)
	server := &Server{cfg: opts.Config, db: opts.DB, logger: opts.Logger.With("component", "connector"), now: now, localControl: opts.LocalControl, overview: opts.Overview, taskEvidence: opts.TaskEvidence, usage: opts.Usage, codeEvidence: opts.CodeEvidence, guidance: guidanceFiles, github: github, cookie: aead, transport: newTransportState()}
	server.actions.Store(opts.Actions)
	server.workflows.Store(opts.Workflows)
	return server, nil
}

func randomCredential() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", errors.New("connector: secure randomness unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func digest(value string) string {
	b := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// SetActions safely publishes trusted composition when lazy setup finishes.
func (s *Server) SetActions(actions *owneraction.Service) { s.actions.Store(actions) }

// SetWorkflows publishes the installed coordinator before serving.
func (s *Server) SetWorkflows(v *durableworkflow.Service) { s.workflows.Store(v) }
