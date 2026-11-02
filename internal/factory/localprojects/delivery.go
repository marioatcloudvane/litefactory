package localprojects

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

// DeliveryRequestOptions contains an external owner credential reference, never
// a token argument. The publisher credential cannot stand in for the requester.
type DeliveryRequestOptions struct {
	DB                *store.DB
	Resolver          secrets.Resolver
	HTTPClient        *http.Client
	Credential        secrets.Reference
	Parent, RequestID string
	Generation        int
}

// RequestCodeDelivery verifies a human against retained GitHub code authority
// and journals a typed request. Daemon recovery owns executing the request.
func RequestCodeDelivery(ctx context.Context, o DeliveryRequestOptions) (contracts.ProjectDeliveryReceipt, error) {
	var empty contracts.ProjectDeliveryReceipt
	if o.DB == nil || o.RequestID == "" || len(o.RequestID) > 128 || strings.ContainsAny(o.RequestID, " \r\n\t\x00") || o.Generation <= 0 {
		return empty, errors.New("feature, generation and stable request ID are required")
	}
	session, err := o.DB.GetSessionByTaskRef(ctx, o.Parent)
	if err != nil {
		return empty, err
	}
	feature, err := o.DB.GetManagedFeature(ctx, o.Parent)
	if err != nil {
		return empty, err
	}
	if feature.Generation != o.Generation || feature.WorkflowVersion != store.WorkflowSeparated {
		return empty, errors.New("delivery request does not name the current approved separated generation")
	}
	authority, err := o.DB.EnsureDeliveryCodeAuthority(ctx, session.ID)
	if err != nil {
		return empty, err
	}
	token, err := o.Resolver.Resolve(ctx, o.Credential)
	if err != nil {
		return empty, err
	}
	redactor := redact.New(redact.Options{Token: token, Environ: []string{}})
	client, err := github.New(github.Options{APIBase: string(authority.Origin), Token: token, HTTPClient: o.HTTPClient, Redactor: redactor})
	if err != nil {
		return empty, errors.New("owner GitHub transport unavailable")
	}
	actor, err := client.AuthenticatedUser(ctx)
	if err != nil {
		return empty, err
	}
	if actor.ID != authority.OwnerID || actor.ID == authority.PublisherID || actor.IsBot || !actor.IsHuman {
		return empty, errors.New("delivery request credential is not the retained human GitHub code owner")
	}
	repo, err := deliveryRepository(authority.Repository)
	if err != nil {
		return empty, err
	}
	observed, err := client.InspectRepository(ctx, repo)
	if err != nil {
		return empty, err
	}
	if strconv.FormatInt(observed.ID, 10) != authority.RepositoryID || !strings.EqualFold(observed.FullName, authority.Repository) {
		return empty, errors.New("original code repository identity changed")
	}
	sourceID := "github-cli:" + o.RequestID
	old, err := o.DB.GetDeliveryControlSource(ctx, sourceID)
	if err == nil {
		if old.SessionID != session.ID || old.Kind != "github-cli-request" || old.Generation != o.Generation || old.ActorID != actor.ID {
			return empty, store.ErrBindingConflict
		}
		return contracts.ProjectDeliveryReceipt(old), nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return empty, err
	}
	retained, err := o.DB.RetainDeliveryControlSource(ctx, store.DeliveryControlSource{ID: sourceID, SessionID: session.ID, Kind: "github-cli-request", Generation: o.Generation, ActorID: actor.ID, Body: fmt.Sprintf("/deliver %d", o.Generation), CreatedAt: time.Now().UTC()})
	return contracts.ProjectDeliveryReceipt(retained), err
}

func deliveryRepository(destination string) (github.Repo, error) {
	owner, name, found := strings.Cut(destination, "/")
	repo := github.Repo{Owner: owner, Name: name}
	if !found {
		return repo, errors.New("code repository must name owner/repository")
	}
	return repo, repo.Validate()
}
