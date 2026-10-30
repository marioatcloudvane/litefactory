package store

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestStandardsReviewedHeadFenceIsAtomicAndKeepsReceiptRecovery(t *testing.T) {
	db := newTestDB(t)
	operation := conversationTestOperation(t, db, "reviewed-operation")
	ctx := WithStandardsConversationOperation(t.Context(), operation.Key, operation.RequestDigest)
	ctx = WithStandardsExpectedHead(ctx, 1)
	draft := StandardsAuthoringSession{ID: "fixed", Mode: "create", TargetID: "standard", Status: "draft", Data: json.RawMessage(`{}`)}
	if _, err := db.SaveStandardsAuthoring(ctx, draft, 0); !errors.Is(err, ErrStandardsConflict) {
		t.Fatalf("changed book accepted: %v", err)
	}
	if _, err := db.GetStandardsAuthoring(t.Context(), draft.ID, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("fenced write leaked effect: %v", err)
	}
	ctx = WithStandardsExpectedHead(ctx, 0)
	if _, err := db.SaveStandardsAuthoring(ctx, draft, 0); err != nil {
		t.Fatal(err)
	}
	// Receipt recovery wins over a now-stale head fence, without another effect.
	ctx = WithStandardsExpectedHead(ctx, 1)
	if _, err := db.SaveStandardsAuthoring(ctx, draft, 0); !errors.Is(err, ErrStandardsOperationCommitted) {
		t.Fatalf("recovery lost to head fence: %v", err)
	}
}

func TestStandardsActionProvenanceDoesNotAuthorizeUnclaimedWrites(t *testing.T) {
	db := newTestDB(t)
	actor := "owner-action:opaque-id"
	if !StandardsProvenanceValid(actor) || StandardsProvenanceValid("owner-action:bad/id") {
		t.Fatal("invalid action attribution format")
	}
	ctx := WithStandardsProvenance(t.Context(), actor)
	if _, err := db.SaveStandardsAuthoring(ctx, StandardsAuthoringSession{ID: "fixed", Mode: "create", TargetID: "standard", Status: "draft", Data: json.RawMessage(`{}`)}, 0); err == nil {
		t.Fatal("provenance text authorized an unclaimed action")
	}
}

func TestStandardsLateWriteUsesCurrentGrantClock(t *testing.T) {
	db, p := ownerActionStoreFixture(t)
	connectorTestClient(t, db, "client")
	if _, err := db.sql.ExecContext(t.Context(), `INSERT INTO connector_grants(id,client_id,resource,scope,owner_id,generation,created_at,expires_at) VALUES(?,?,?,?,?,?,?,?)`, "short-grant", "client", connectorTestResource, "standards:management", 42, p.Principal.OwnerGeneration, FormatTime(p.CreatedAt), FormatTime(p.CreatedAt.Add(10*time.Second))); err != nil {
		t.Fatal(err)
	}
	p.ID = "late-write"
	p.ExecutionID = "owner-action:late-write"
	p.RequestKey = "late-key"
	p.Principal.Transport = "mcp"
	p.Principal.ClientID = "client"
	p.Principal.GrantID = "short-grant"
	if err := db.CreateOwnerAction(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	if _, err := db.MutateOwnerAction(t.Context(), p.Principal, p.ID, "confirm", "", p.ConfirmNotBefore); err != nil {
		t.Fatal(err)
	}
	if claimed, err := db.ClaimOwnerActionExecution(t.Context(), p.ID, p.ConfirmNotBefore); err != nil || !claimed {
		t.Fatalf("claim: %v %v", claimed, err)
	}
	ctx := WithStandardsProvenance(t.Context(), p.ExecutionID)
	ctx = WithStandardsOwnerActionTime(ctx, p.ConfirmNotBefore)
	ctx = WithStandardsOwnerActionClock(ctx, func() time.Time { return p.CreatedAt.Add(11 * time.Second) })
	if _, err := db.SaveStandardsAuthoring(ctx, StandardsAuthoringSession{ID: "late", Mode: "create", TargetID: "standard", Status: "draft", Data: json.RawMessage(`{}`)}, 0); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("expired grant wrote using old execution time: %v", err)
	}
	if _, err := db.GetStandardsAuthoring(t.Context(), "late", 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("late effect leaked: %v", err)
	}
}
