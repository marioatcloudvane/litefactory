package owneractions

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// Bug filing limits and fixed issue content.
const (
	BugTitleLimit       = 256
	BugDescriptionLimit = 8192
	BugLabel            = "bug"
	bugProvenance       = "Filed for the owner from a chat client."
	bugFresh            = "the reviewed bug filing target changed; request a fresh proposal"
	bugRevoked          = "authority was revoked before filing; nothing was filed"
)

// BugLabels reads whether a repository has a label, without writing.
type BugLabels interface {
	RepoLabelNodeID(ctx context.Context, repo github.Repo, name string) (nodeID string, found bool, err error)
}

// BugFiles owns bug_file. Execute only admits the durable filing record; the
// bug filing processor performs every forge write.
type BugFiles struct {
	DB           *store.DB
	Labels       BugLabels
	Protect      func(context.Context, string) (string, error)
	Availability Availability
}

// refuseBug returns a known pre-effect refusal with an owner-readable reason.
func refuseBug(why string) error { return owneraction.Refusal{Reason: why} }

type bugFrozen struct {
	Kind          string `json:"kind"`
	RepositoryID  int64  `json:"repository_id"`
	RepositoryRef string `json:"repository_ref"`
	Title         string `json:"title"`
	Body          string `json:"body"`
	Marker        string `json:"marker"`
	// The Linear fields are frozen at proposal and absent from GitHub payloads.
	Tracker     string `json:"tracker,omitempty"`
	LabelID     string `json:"label_id,omitempty"`
	Destination string `json:"destination,omitempty"`
	Nonce       string `json:"nonce,omitempty"`
}

// bugLinearTargetID is the stable target of a Linear filing: it has no marker,
// so it derives from the frozen payload hash.
func bugLinearTargetID(sumHex string) string { return "linear:" + sumHex[:32] }

// BugMarkerLine renders the hidden marker line for a 32-hex marker.
func BugMarkerLine(hexMarker string) string {
	return "<!-- lf:bug-filing v=1 marker=" + hexMarker + " -->"
}

// BugIssueBody composes the exact issue body filed for the owner.
func BugIssueBody(description, markerLine string) string {
	return description + "\n\n" + bugProvenance + "\n\n" + markerLine
}

func newBugMarker() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return BugMarkerLine(hex.EncodeToString(raw[:])), nil
}

// target selects the registered GitHub repository named by the owner.
func (b BugFiles) target(ctx context.Context, name string) (store.Repo, error) {
	repos, err := b.DB.ListRepos(ctx)
	if err != nil {
		return store.Repo{}, err
	}
	refs := make([]Repository, len(repos))
	for i, r := range repos {
		refs[i] = repository(r)
	}
	selected := SelectProject(refs, name, nil)
	if selected.Refusal != "" {
		return store.Repo{}, refuseBug(selected.Refusal)
	}
	for _, r := range repos {
		if r.ID == selected.Repository.ID {
			return r, nil
		}
	}
	return store.Repo{}, refuseBug("Unknown project.")
}

const bugNoLinearLabel = "This Linear project has no bug label set. Ask the factory operator to set the connection's --bug-label to your Linear bug label ID, then propose again."

// fences checks consent, ticket connection and the bug label for one
// repository. For a Linear project it returns the connection and true.
func (b BugFiles) fences(ctx context.Context, p owneraction.Principal, repo store.Repo, ref string, at time.Time) (store.ConnectionVersion, bool, error) {
	actor := storePrincipal(p)
	if err := b.DB.CheckOwnerActionAuthority(ctx, actor, store.OwnerAction{Principal: actor, Scope: "repository:actions", RepositoryID: repo.ID, RepositoryRef: ref}, at); err != nil {
		return store.ConnectionVersion{}, false, err
	}
	conn, err := b.DB.CurrentConnection(ctx, repo.ID, store.TicketConnection)
	switch {
	case err == nil && conn.Provider == ticket.Linear:
		if !conn.Enabled {
			return conn, true, refuseBug("the Linear connection is disabled")
		}
		if conn.Linear.BugLabelID == "" {
			return conn, true, refuseBug(bugNoLinearLabel)
		}
		return conn, true, nil
	case err != nil && !errors.Is(err, store.ErrNotFound):
		return store.ConnectionVersion{}, false, err
	}
	_, found, err := b.Labels.RepoLabelNodeID(ctx, github.Repo{Owner: repo.Owner, Name: repo.Name}, BugLabel)
	if err != nil {
		return store.ConnectionVersion{}, false, err
	}
	if !found {
		return store.ConnectionVersion{}, false, refuseBug("the repository " + repo.Slug() + " has no label named " + BugLabel + "; add it before filing")
	}
	return store.ConnectionVersion{}, false, nil
}

func (b BugFiles) checkProtected(ctx context.Context, values ...string) error {
	if b.Protect == nil {
		return errors.New("action protection unavailable")
	}
	for _, v := range values {
		protected, err := b.Protect(ctx, v)
		if err != nil {
			return err
		}
		if protected != v {
			return refuseBug("protection changed the bug title or description; remove the secret-like text before proposing")
		}
	}
	return nil
}

// Resolve checks the inputs and the repository, then freezes the exact issue
// content with one new random marker.
func (b BugFiles) Resolve(ctx context.Context, p owneraction.Principal, in owneraction.Invocation, at time.Time) (owneraction.Facts, error) {
	if in.Command != BugFileCommand {
		return owneraction.Facts{}, ErrInvalidSelection
	}
	inv, err := b.Availability.ValidateChange(owneraction.Invocation{Command: in.Command, Arguments: maps.Clone(in.Arguments)})
	if err != nil {
		return owneraction.Facts{}, err
	}
	if b.DB == nil || b.Labels == nil {
		return owneraction.Facts{}, owneraction.ErrUnavailable
	}
	args := inv.Arguments
	for _, name := range []string{"repository", "title", "description"} {
		if strings.TrimSpace(args[name]) == "" {
			return owneraction.Facts{}, refuseBug("the bug " + name + " is blank")
		}
	}
	title, description := args["title"], args["description"]
	if utf8.RuneCountInString(title) > BugTitleLimit {
		return owneraction.Facts{}, refuseBug("the bug title is longer than 256 characters")
	}
	if len(description) > BugDescriptionLimit {
		return owneraction.Facts{}, refuseBug("the bug description is longer than 8192 bytes")
	}
	repo, err := b.target(ctx, args["repository"])
	if err != nil {
		return owneraction.Facts{}, err
	}
	ref := repo.Forge + ":" + repo.Slug()
	conn, isLinear, err := b.fences(ctx, p, repo, ref, at)
	if err != nil {
		return owneraction.Facts{}, err
	}
	var frozen bugFrozen
	if isLinear {
		var nonce [16]byte
		if _, err = rand.Read(nonce[:]); err != nil {
			return owneraction.Facts{}, err
		}
		frozen = bugFrozen{Kind: BugFileCommand, RepositoryID: repo.ID, RepositoryRef: ref, Title: title, Body: description + "\n\n" + bugProvenance, Tracker: store.BugTrackerLinear, LabelID: conn.Linear.BugLabelID, Destination: conn.Destination, Nonce: hex.EncodeToString(nonce[:])}
	} else {
		marker, markerErr := newBugMarker()
		if markerErr != nil {
			return owneraction.Facts{}, markerErr
		}
		frozen = bugFrozen{Kind: BugFileCommand, RepositoryID: repo.ID, RepositoryRef: ref, Title: title, Body: BugIssueBody(description, marker), Marker: marker}
	}
	if err = b.checkProtected(ctx, frozen.Title, frozen.Body); err != nil {
		return owneraction.Facts{}, err
	}
	raw, err := json.Marshal(frozen)
	if err != nil {
		return owneraction.Facts{}, err
	}
	sum := sha256.Sum256(raw)
	summary := "File a bug on " + repo.Slug() + " titled \"" + title + "\", label it " + BugLabel + " and start Bug analysis on it."
	targetID := frozen.Marker
	if isLinear {
		summary = "File a bug on the Linear project of " + repo.Slug() + " titled \"" + title + "\", label it " + BugLabel + " and start Bug analysis on it."
		targetID = bugLinearTargetID(hex.EncodeToString(sum[:]))
	}
	return owneraction.Facts{Invocation: inv, RepositoryID: repo.ID, RepositoryRef: ref, TargetKind: "bug_filing", TargetID: targetID, TargetRevision: hex.EncodeToString(sum[:]), Summary: summary, Estimate: "unknown", Payload: string(raw)}, nil
}

func unpackBug(f owneraction.Facts) (bugFrozen, error) {
	var v bugFrozen
	if err := json.Unmarshal([]byte(f.Payload), &v); err != nil || v.Kind != BugFileCommand {
		return v, owneraction.ErrIntegrity
	}
	sum := sha256.Sum256([]byte(f.Payload))
	sumHex := hex.EncodeToString(sum[:])
	if sumHex != f.TargetRevision || v.RepositoryID != f.RepositoryID || v.RepositoryRef != f.RepositoryRef || v.Title == "" {
		return v, owneraction.ErrIntegrity
	}
	switch v.Tracker {
	case "":
		if v.LabelID != "" || v.Destination != "" || v.Nonce != "" || v.Marker != f.TargetID || !strings.HasSuffix(v.Body, "\n\n"+v.Marker) {
			return v, owneraction.ErrIntegrity
		}
	case store.BugTrackerLinear:
		if v.Marker != "" || v.LabelID == "" || v.Destination == "" || f.TargetID != bugLinearTargetID(sumHex) || strings.TrimSpace(v.Body) == "" {
			return v, owneraction.ErrIntegrity
		}
	default:
		return v, owneraction.ErrIntegrity
	}
	return v, nil
}

// Recheck repeats the repository, consent, Linear and label fences.
func (b BugFiles) Recheck(ctx context.Context, p owneraction.Principal, f owneraction.Facts, at time.Time) error {
	v, err := unpackBug(f)
	if err != nil {
		return err
	}
	if b.DB == nil || b.Labels == nil {
		return owneraction.ErrUnavailable
	}
	slug, ok := strings.CutPrefix(v.RepositoryRef, store.ForgeGitHub+":")
	owner, name, split := strings.Cut(slug, "/")
	if !ok || !split {
		return owneraction.ErrIntegrity
	}
	repo, err := b.DB.GetRepoBySlug(ctx, store.ForgeGitHub, owner, name)
	if errors.Is(err, store.ErrNotFound) || err == nil && repo.ID != v.RepositoryID {
		return refuseBug(bugFresh)
	}
	if err != nil {
		return err
	}
	conn, isLinear, err := b.fences(ctx, p, repo, v.RepositoryRef, at)
	if err == nil && isLinear != (v.Tracker == store.BugTrackerLinear) {
		return refuseBug(bugFresh)
	}
	if err == nil && isLinear && (conn.Linear.BugLabelID != v.LabelID || conn.Destination != v.Destination) {
		return refuseBug(bugFresh)
	}
	if err != nil {
		var refusal owneraction.Refusal
		if errors.As(err, &refusal) {
			return refuseBug(refusal.Reason + "; " + bugFresh)
		}
		return err
	}
	return nil
}

func (b BugFiles) admit(ctx context.Context, p owneraction.Principal, v bugFrozen, key string, at time.Time) (store.BugFiling, error) {
	filing := store.BugFiling{ExecutionID: key, OwnerActionID: strings.TrimPrefix(key, "owner-action:"), OwnerID: p.OwnerID, OwnerGeneration: p.OwnerGeneration, RepositoryID: v.RepositoryID, RepositoryRef: v.RepositoryRef, Title: v.Title, Body: v.Body, Marker: v.Marker}
	if v.Tracker == store.BugTrackerLinear {
		filing.Tracker, filing.LabelID, filing.Destination, filing.EffectKey = v.Tracker, v.LabelID, v.Destination, "bug-filing:"+key
	}
	f, _, err := b.DB.AdmitBugFiling(ctx, filing, at)
	return f, err
}

// Execute admits the filing record for this execution id, at most once.
func (b BugFiles) Execute(ctx context.Context, p owneraction.Principal, f owneraction.Facts, key string, at time.Time) (owneraction.Result, error) {
	v, err := unpackBug(f)
	if err != nil {
		return owneraction.Result{}, err
	}
	if b.DB == nil {
		return owneraction.Result{}, owneraction.ErrUnavailable
	}
	filing, err := b.admit(ctx, p, v, key, at)
	if err != nil {
		return owneraction.Result{}, err
	}
	return b.result(ctx, filing)
}

// authorityRevoked reports errors that prove the owner's authority is gone.
func authorityRevoked(err error) bool {
	return errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrConnectorOwner) || errors.Is(err, store.ErrConnectorInvalid) || errors.Is(err, store.ErrConnectorScope) || errors.Is(err, store.ErrDoctorFenced)
}

// recoverAuthority rechecks the owner action's current authority, the
// selected-repository consent and the handler fences before a late admission.
func (b BugFiles) recoverAuthority(ctx context.Context, p owneraction.Principal, f owneraction.Facts, key string, at time.Time) error {
	record, err := b.DB.ReadOwnerAction(ctx, strings.TrimPrefix(key, "owner-action:"))
	if err != nil {
		return err
	}
	if record.ExecutionID != key || record.RepositoryID != f.RepositoryID || record.RepositoryRef != f.RepositoryRef {
		return store.ErrNotFound
	}
	if record.State == "executing" {
		err = b.DB.CheckOwnerActionEffect(store.WithOwnerActionEffect(ctx, key, func() time.Time { return at }))
	} else {
		err = b.DB.CheckOwnerActionAuthority(ctx, record.Principal, record, at)
	}
	if err != nil {
		return err
	}
	return b.Recheck(ctx, p, f, at)
}

// Recover admits a filing that a crash or a failed Execute left missing, after
// rechecking authority, then reports the recorded filing state.
func (b BugFiles) Recover(ctx context.Context, p owneraction.Principal, f owneraction.Facts, key string, at time.Time) (owneraction.Result, bool, error) {
	v, err := unpackBug(f)
	if err != nil {
		return owneraction.Result{}, false, err
	}
	if b.DB == nil {
		return owneraction.Result{}, false, owneraction.ErrUnavailable
	}
	filing, err := b.DB.BugFilingByExecution(ctx, key)
	if errors.Is(err, store.ErrNotFound) {
		if err = b.recoverAuthority(ctx, p, f, key, at); err != nil {
			var refusal owneraction.Refusal
			switch {
			case authorityRevoked(err):
				return owneraction.Result{State: "failed", Summary: bugRevoked}, true, nil
			case errors.As(err, &refusal):
				return owneraction.Result{State: "failed", Summary: refusal.Reason + "; nothing was filed"}, true, nil
			}
			return owneraction.Result{}, false, err
		}
		filing, err = b.admit(ctx, p, v, key, at)
	}
	if err != nil {
		return owneraction.Result{}, false, err
	}
	res, err := b.result(ctx, filing)
	return res, err == nil, err
}

// BugFilingResult maps a recorded filing to the owner-action result.
func BugFilingResult(f store.BugFiling) owneraction.Result {
	res := owneraction.Result{State: "executing", Reference: f.IssueLink}
	switch f.State {
	case store.BugFilingAdmitted, store.BugFilingCreating:
		res.Summary = "filing"
	case store.BugFilingUncertain:
		res.Summary = "filing; checking whether the issue was created"
	case store.BugFilingCreated:
		res.Summary = "filed but not yet labelled"
	case store.BugFilingLabelled, store.BugFilingStartAdmitted:
		res.Summary = orDefault(f.Reason, "labelled but not yet started")
	case store.BugFilingDispatched:
		res.State, res.Summary = "succeeded", "Bug started"
	default:
		res.State, res.Summary = "failed", orDefault(f.Reason, "the bug filing failed")
	}
	return res
}

func orDefault(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}

func (b BugFiles) result(ctx context.Context, f store.BugFiling) (owneraction.Result, error) {
	res := BugFilingResult(f)
	if b.Protect != nil {
		summary, err := b.Protect(ctx, res.Summary)
		if err != nil {
			return owneraction.Result{}, err
		}
		res.Summary = summary
	}
	return res, nil
}
