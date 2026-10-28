package owneraction

import (
	"context"
	"testing"
	"time"
)

// The wrapped domain represents target reads as well as effects. Authentication
// must precede Resolve and Recheck because those methods may contact a provider.
type authorityCountingDomain struct {
	Domain
	resolves int
	rechecks int
	executes int
}

func (d *authorityCountingDomain) Resolve(ctx context.Context, p Principal, in Invocation, at time.Time) (Facts, error) {
	d.resolves++
	return d.Domain.Resolve(ctx, p, in, at)
}
func (d *authorityCountingDomain) Recheck(ctx context.Context, p Principal, f Facts, at time.Time) error {
	d.rechecks++
	return d.Domain.Recheck(ctx, p, f, at)
}
func (d *authorityCountingDomain) Execute(ctx context.Context, p Principal, f Facts, key string, at time.Time) (Result, error) {
	d.executes++
	return d.Domain.Execute(ctx, p, f, key, at)
}

func TestActionProposeChecksAuthorityBeforeResolvingTargets(t *testing.T) {
	for _, name := range []string{"wrong owner", "stale owner generation", "unknown grant", "missing action scope", "revoked grant", "expired grant"} {
		t.Run(name, func(t *testing.T) {
			service, domain, discord, at := actionServiceFixture(t)
			counted := &authorityCountingDomain{Domain: domain}
			service.domain = counted
			scope := "repository:actions"
			if name == "missing action scope" {
				scope = "factory:overview"
			}
			caller := actionMCPPrincipal(t, service, discord, "authority-check", scope)
			switch name {
			case "wrong owner":
				caller.OwnerID = "99"
			case "stale owner generation":
				caller.OwnerGeneration++
			case "unknown grant":
				caller.GrantID = "missing"
			case "revoked grant":
				if err := service.db.RevokeConnectorGrant(t.Context(), 42, caller.GrantID, *at); err != nil {
					t.Fatal(err)
				}
			case "expired grant":
				*at = at.Add(25 * time.Hour)
			}
			if _, err := service.Propose(t.Context(), caller, Invocation{Command: "diagnose"}, "request"); err == nil {
				t.Fatal("unauthorized proposal accepted")
			}
			if counted.resolves != 0 || counted.rechecks != 0 || counted.executes != 0 {
				t.Fatalf("authority refusal contacted domain: resolve=%d recheck=%d execute=%d", counted.resolves, counted.rechecks, counted.executes)
			}
		})
	}
}

func TestActionRevokedAdmissionFailsBeforeDomainRecheck(t *testing.T) {
	service, domain, discord, at := actionServiceFixture(t)
	counted := &authorityCountingDomain{Domain: domain}
	service.domain = counted
	caller := actionMCPPrincipal(t, service, discord, "admitted-origin", "repository:actions")
	proposal, err := service.Propose(t.Context(), caller, Invocation{Command: "diagnose"}, "request")
	if err != nil {
		t.Fatal(err)
	}
	if counted.resolves != 1 {
		t.Fatalf("authorized proposal did not resolve exactly once: %d", counted.resolves)
	}
	*at = at.Add(5 * time.Second)
	admitted, err := service.Confirm(t.Context(), caller, proposal.ID)
	if err != nil || admitted.State != "admitted" {
		t.Fatalf("admission: %+v %v", admitted, err)
	}
	counted.rechecks = 0
	if err = service.db.RevokeConnectorGrant(t.Context(), 42, caller.GrantID, *at); err != nil {
		t.Fatal(err)
	}
	failed, err := service.Step(t.Context(), proposal.ID)
	if err != nil || failed.State != "failed" || failed.Result.State != "failed" {
		t.Fatalf("revoked admitted work was not saved as failed: %+v %v", failed, err)
	}
	if counted.rechecks != 0 || counted.executes != 0 || domain.calls != 0 {
		t.Fatalf("revoked admission contacted domain: recheck=%d execute=%d effects=%d", counted.rechecks, counted.executes, domain.calls)
	}
	recorded, err := service.db.ReadOwnerAction(t.Context(), proposal.ID)
	if err != nil || recorded.State != "failed" {
		t.Fatalf("failure not durable: %+v %v", recorded, err)
	}
	repeated, err := service.Step(t.Context(), proposal.ID)
	if err != nil || repeated.State != "failed" || counted.rechecks != 0 || counted.executes != 0 {
		t.Fatalf("recorded refusal was retried: %+v %v", repeated, err)
	}
}
