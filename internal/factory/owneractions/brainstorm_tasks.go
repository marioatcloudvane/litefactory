package owneractions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

// Brainstorm task command names.
const (
	BrainstormCreateTasks  = "brainstorm_create_tasks"
	BrainstormTaskRecreate = "brainstorm_task_recreate"
)

const brainstormFresh = "the reviewed brainstorm drafts changed; request a fresh proposal"

// BrainstormCommand reports whether the command belongs to the brainstorm task domain.
func BrainstormCommand(command string) bool {
	return command == BrainstormCreateTasks || command == BrainstormTaskRecreate
}

// BrainstormIssueDraft is one exact issue rendered from a kept draft.
type BrainstormIssueDraft struct {
	DraftID, Key, Title, Body, Marker string
	Generation                        int
	Ledger                            json.RawMessage
	LedgerDigest                      string
}

// BrainstormDrafts renders the exact issue title and body of kept drafts after
// checking the owner, the repository:brainstorm consent and the size bounds.
type BrainstormDrafts interface {
	IssueDrafts(ctx context.Context, p owneraction.Principal, brainstormID, draftSetID string, keys []string, generation int) (store.BrainstormDraftSet, []BrainstormIssueDraft, error)
}

// BrainstormTasks owns the two brainstorm task owner actions. Execute only
// admits durable rows; the brainstorm task processor performs every forge write.
type BrainstormTasks struct {
	DB           *store.DB
	Drafts       BrainstormDrafts
	Protect      func(context.Context, string) (string, error)
	Availability Availability
}

type brainstormFrozen struct {
	Kind         string                       `json:"kind"`
	BrainstormID string                       `json:"brainstorm_id"`
	DraftSetID   string                       `json:"draft_set_id"`
	BatchID      string                       `json:"batch_id,omitempty"`
	Issues       []BrainstormIssueDraft       `json:"issues,omitempty"`
	Items        []store.BrainstormFrozenItem `json:"items,omitempty"`
	FrozenDigest string                       `json:"frozen_digest,omitempty"`
	RecreateItem string                       `json:"recreate_item,omitempty"`
}

func storePrincipal(p owneraction.Principal) store.OwnerActionPrincipal {
	return store.OwnerActionPrincipal{OwnerID: p.OwnerID, OwnerGeneration: p.OwnerGeneration, Transport: p.Transport, ClientID: p.ClientID, GrantID: p.GrantID, ChannelID: p.ChannelID, DiscordOwnerID: p.DiscordOwnerID}
}

func (b BrainstormTasks) repository(ctx context.Context, ref string) (store.Repo, error) {
	slug, ok := strings.CutPrefix(ref, store.ForgeGitHub+":")
	owner, name, split := strings.Cut(slug, "/")
	if !ok || !split {
		return store.Repo{}, errors.New("brainstorm tasks need a GitHub repository")
	}
	repo, err := b.DB.GetRepoBySlug(ctx, store.ForgeGitHub, owner, name)
	if errors.Is(err, store.ErrNotFound) {
		return repo, errors.New("brainstorm tasks need a registered GitHub repository")
	}
	return repo, err
}

func (b BrainstormTasks) owned(p owneraction.Principal, batch store.BrainstormTaskBatch) error {
	if batch.OwnerID != p.OwnerID || batch.OwnerGeneration != p.OwnerGeneration {
		return store.ErrNotFound
	}
	return nil
}

// unfinished freezes every item of the batch that has not started Plan.
func unfinished(items []store.BrainstormTaskItem) []store.BrainstormFrozenItem {
	var out []store.BrainstormFrozenItem
	for _, it := range items {
		if it.State != store.BrainstormItemPlanStarted {
			out = append(out, store.BrainstormFrozenItem{ItemID: it.ID, State: it.State, Generation: it.Generation})
		}
	}
	return out
}

func sameFrozen(a, b []store.BrainstormFrozenItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (b BrainstormTasks) checkProtected(ctx context.Context, issues []BrainstormIssueDraft) error {
	if b.Protect == nil {
		return errors.New("action protection unavailable")
	}
	for _, d := range issues {
		for _, v := range []string{d.Title, d.Body} {
			protected, err := b.Protect(ctx, v)
			if err != nil {
				return err
			}
			if protected != v {
				return errors.New("protection changed a drafted issue; revise the draft before proposing")
			}
		}
	}
	return nil
}

func (b BrainstormTasks) brainstormRef(ctx context.Context, p owneraction.Principal, id string, at time.Time) (string, error) {
	owner, err := strconv.ParseInt(p.OwnerID, 10, 64)
	if err != nil {
		return "", store.ErrNotFound
	}
	record, err := b.DB.ReadBrainstorm(ctx, store.BrainstormOwner{OwnerID: owner, OwnerGeneration: p.OwnerGeneration}, id, at)
	if err != nil {
		return "", err
	}
	return record.RepositoryRef, nil
}

// Resolve freezes the exact issues to create, or the exact unfinished items to continue.
func (b BrainstormTasks) Resolve(ctx context.Context, p owneraction.Principal, in owneraction.Invocation, at time.Time) (owneraction.Facts, error) {
	inv, err := b.Availability.ValidateChange(owneraction.Invocation{Command: in.Command, Arguments: maps.Clone(in.Arguments)})
	if err != nil {
		return owneraction.Facts{}, err
	}
	if b.DB == nil || b.Drafts == nil {
		return owneraction.Facts{}, owneraction.ErrUnavailable
	}
	var frozen brainstormFrozen
	var summary string
	switch inv.Command {
	case BrainstormCreateTasks:
		frozen, summary, err = b.resolveCreate(ctx, p, inv.Arguments)
	case BrainstormTaskRecreate:
		frozen, summary, err = b.resolveRecreate(ctx, p, inv.Arguments, at)
	default:
		return owneraction.Facts{}, ErrInvalidSelection
	}
	if err != nil {
		return owneraction.Facts{}, err
	}
	ref, err := b.brainstormRef(ctx, p, frozen.BrainstormID, at)
	if err != nil {
		return owneraction.Facts{}, err
	}
	repo, err := b.repository(ctx, ref)
	if err != nil {
		return owneraction.Facts{}, err
	}
	if err = b.DB.CheckBrainstormConsent(ctx, storePrincipal(p), ref, at); err != nil {
		return owneraction.Facts{}, err
	}
	if err = b.checkProtected(ctx, frozen.Issues); err != nil {
		return owneraction.Facts{}, err
	}
	raw, err := json.Marshal(frozen)
	if err != nil {
		return owneraction.Facts{}, err
	}
	sum := sha256.Sum256(raw)
	return owneraction.Facts{Invocation: inv, RepositoryID: repo.ID, RepositoryRef: ref, TargetKind: "brainstorm_draft_set", TargetID: frozen.DraftSetID, TargetRevision: hex.EncodeToString(sum[:]), Summary: summary, Estimate: "unknown", Payload: string(raw)}, nil
}

func splitKeys(raw string) []string {
	var keys []string
	seen := map[string]bool{}
	for _, k := range strings.Split(raw, ",") {
		k = strings.TrimSpace(k)
		if k != "" && !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	return keys
}

func (b BrainstormTasks) resolveCreate(ctx context.Context, p owneraction.Principal, args map[string]string) (brainstormFrozen, string, error) {
	f := brainstormFrozen{BrainstormID: args["brainstorm"], DraftSetID: args["draft_set"]}
	batch, err := b.DB.BrainstormTaskBatchForDraftSet(ctx, f.DraftSetID)
	if err == nil {
		// A set that already has a batch is continued, whether or not it is still latest or used.
		if err = b.owned(p, batch); err != nil || batch.BrainstormID != f.BrainstormID {
			return f, "", store.ErrNotFound
		}
		return b.freezeContinue(ctx, batch, f)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return f, "", err
	}
	keys := splitKeys(args["drafts"])
	if len(keys) == 0 {
		return f, "", errors.New("name at least one kept draft")
	}
	latest, err := b.DB.LatestBrainstormDraftSet(ctx, f.BrainstormID)
	if err != nil {
		return f, "", err
	}
	if latest.ID != f.DraftSetID || latest.State != "latest" {
		return f, "", errors.New("only the latest unused draft set can start new tasks")
	}
	set, issues, err := b.Drafts.IssueDrafts(ctx, p, f.BrainstormID, f.DraftSetID, keys, 1)
	if err != nil {
		return f, "", err
	}
	if set.ID != f.DraftSetID || set.BrainstormID != f.BrainstormID || len(issues) != len(keys) {
		return f, "", errors.New(brainstormFresh)
	}
	f.Kind, f.Issues = "create", issues
	lines := []string{fmt.Sprintf("Create %d GitHub issues from brainstorm drafts and start Plan for each:", len(issues))}
	for _, d := range issues {
		lines = append(lines, "- "+d.Key+": "+d.Title)
	}
	return f, strings.Join(lines, "\n"), nil
}

func (b BrainstormTasks) freezeContinue(ctx context.Context, batch store.BrainstormTaskBatch, f brainstormFrozen) (brainstormFrozen, string, error) {
	items, err := b.DB.BrainstormTaskItems(ctx, batch.ID)
	if err != nil {
		return f, "", err
	}
	f.Kind, f.BatchID, f.Items = "continue", batch.ID, unfinished(items)
	if len(f.Items) == 0 {
		return f, "", fmt.Errorf("batch %s has no unfinished drafts; every issue already started Plan", batch.ID)
	}
	f.FrozenDigest = store.BrainstormFrozenDigest(f.Items)
	lines := []string{"continue batch " + batch.ID + " from its saved progress; issues already created are kept:"}
	for _, it := range items {
		if it.State != store.BrainstormItemPlanStarted {
			lines = append(lines, fmt.Sprintf("- %s (%s)", it.Title, it.State))
		}
	}
	return f, strings.Join(lines, "\n"), nil
}

func (b BrainstormTasks) itemForKey(ctx context.Context, batchID, key string) (store.BrainstormTaskItem, string, error) {
	items, err := b.DB.BrainstormTaskItems(ctx, batchID)
	if err != nil {
		return store.BrainstormTaskItem{}, "", err
	}
	for _, it := range items {
		d, err := b.DB.ReadBrainstormDraft(ctx, it.DraftID)
		if err != nil {
			return it, "", err
		}
		if d.Key == key || d.ID == key {
			return it, d.Key, nil
		}
	}
	return store.BrainstormTaskItem{}, "", errors.New("the batch has no such draft")
}

func (b BrainstormTasks) resolveRecreate(ctx context.Context, p owneraction.Principal, args map[string]string, _ time.Time) (brainstormFrozen, string, error) {
	batch, err := b.DB.ReadBrainstormTaskBatch(ctx, args["batch"])
	if err != nil {
		return brainstormFrozen{}, "", err
	}
	if err = b.owned(p, batch); err != nil {
		return brainstormFrozen{}, "", err
	}
	item, key, err := b.itemForKey(ctx, batch.ID, args["draft"])
	if err != nil {
		return brainstormFrozen{}, "", err
	}
	if item.State != store.BrainstormItemUncertainExhausted {
		return brainstormFrozen{}, "", errors.New("only a draft whose create stayed uncertain after every check can be created again")
	}
	_, issues, err := b.Drafts.IssueDrafts(ctx, p, batch.BrainstormID, batch.DraftSetID, []string{key}, item.Generation+1)
	if err != nil {
		return brainstormFrozen{}, "", err
	}
	if len(issues) != 1 || issues[0].DraftID != item.DraftID || issues[0].Generation != item.Generation+1 {
		return brainstormFrozen{}, "", errors.New(brainstormFresh)
	}
	f := brainstormFrozen{Kind: "recreate", BrainstormID: batch.BrainstormID, DraftSetID: batch.DraftSetID, BatchID: batch.ID, Issues: issues, RecreateItem: item.ID,
		Items: []store.BrainstormFrozenItem{{ItemID: item.ID, State: item.State, Generation: item.Generation}}}
	f.FrozenDigest = store.BrainstormFrozenDigest(f.Items)
	summary := "Create a second GitHub issue for draft " + key + ": " + issues[0].Title + ", then start Plan for it.\n" +
		"The first create could not be confirmed. The first issue may still appear, and two issues for this draft may then exist."
	return f, summary, nil
}

func unpackBrainstorm(f owneraction.Facts) (brainstormFrozen, error) {
	var v brainstormFrozen
	if err := json.Unmarshal([]byte(f.Payload), &v); err != nil || v.Kind == "" {
		return v, owneraction.ErrIntegrity
	}
	sum := sha256.Sum256([]byte(f.Payload))
	if hex.EncodeToString(sum[:]) != f.TargetRevision || v.DraftSetID != f.TargetID {
		return v, owneraction.ErrIntegrity
	}
	return v, nil
}

func sameIssues(a, b []BrainstormIssueDraft) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if x.DraftID != y.DraftID || x.Key != y.Key || x.Title != y.Title || x.Body != y.Body || x.Marker != y.Marker || x.Generation != y.Generation || x.LedgerDigest != y.LedgerDigest {
			return false
		}
	}
	return true
}

// Recheck refuses with "request a fresh proposal" when the reviewed set or frozen items moved.
func (b BrainstormTasks) Recheck(ctx context.Context, p owneraction.Principal, f owneraction.Facts, at time.Time) error {
	v, err := unpackBrainstorm(f)
	if err != nil {
		return err
	}
	if b.DB == nil || b.Drafts == nil {
		return owneraction.ErrUnavailable
	}
	ref, err := b.brainstormRef(ctx, p, v.BrainstormID, at)
	if err != nil {
		return err
	}
	if ref != f.RepositoryRef {
		return errors.New(brainstormFresh)
	}
	if err = b.DB.CheckBrainstormConsent(ctx, storePrincipal(p), ref, at); err != nil {
		return err
	}
	switch v.Kind {
	case "create":
		if _, err = b.DB.BrainstormTaskBatchForDraftSet(ctx, v.DraftSetID); !errors.Is(err, store.ErrNotFound) {
			return errors.New(brainstormFresh)
		}
		latest, err := b.DB.LatestBrainstormDraftSet(ctx, v.BrainstormID)
		if err != nil {
			return err
		}
		if latest.ID != v.DraftSetID || latest.State != "latest" {
			return errors.New(brainstormFresh)
		}
		keys := make([]string, len(v.Issues))
		for i, d := range v.Issues {
			keys[i] = d.Key
		}
		_, issues, err := b.Drafts.IssueDrafts(ctx, p, v.BrainstormID, v.DraftSetID, keys, 1)
		if err != nil {
			return err
		}
		if !sameIssues(issues, v.Issues) {
			return errors.New(brainstormFresh)
		}
		return nil
	case "continue", "recreate":
		batch, err := b.DB.ReadBrainstormTaskBatch(ctx, v.BatchID)
		if err != nil {
			return err
		}
		if err = b.owned(p, batch); err != nil {
			return err
		}
		items, err := b.DB.BrainstormTaskItems(ctx, v.BatchID)
		if err != nil {
			return err
		}
		current := unfinished(items)
		if v.Kind == "recreate" {
			current = nil
			for _, it := range items {
				if it.ID == v.RecreateItem {
					current = []store.BrainstormFrozenItem{{ItemID: it.ID, State: it.State, Generation: it.Generation}}
				}
			}
		}
		if !sameFrozen(current, v.Items) || store.BrainstormFrozenDigest(v.Items) != v.FrozenDigest {
			return errors.New(brainstormFresh)
		}
		return nil
	}
	return owneraction.ErrIntegrity
}

// Execute admits the batch or continuation in one transaction keyed by the execution id.
func (b BrainstormTasks) Execute(ctx context.Context, p owneraction.Principal, f owneraction.Facts, key string, at time.Time) (owneraction.Result, error) {
	v, err := unpackBrainstorm(f)
	if err != nil {
		return owneraction.Result{}, err
	}
	if c, e := b.DB.BrainstormTaskContinuationByExecution(ctx, key); e == nil {
		return owneraction.Result{State: "executing", Summary: "Brainstorm tasks are in progress.", Reference: c.BatchID}, nil
	} else if !errors.Is(e, store.ErrNotFound) {
		return owneraction.Result{}, e
	}
	if err = b.Recheck(ctx, p, f, at); err != nil {
		return owneraction.Result{State: "failed", Summary: err.Error() + ". Nothing was created."}, nil //nolint:nilerr // A pre-effect refusal is a factual failed result.
	}
	switch v.Kind {
	case "create":
		batch := store.BrainstormTaskBatch{ExecutionID: key, OwnerID: p.OwnerID, OwnerGeneration: p.OwnerGeneration, RepositoryID: f.RepositoryID, RepositoryRef: f.RepositoryRef, BrainstormID: v.BrainstormID, DraftSetID: v.DraftSetID}
		items := make([]store.BrainstormTaskItem, len(v.Issues))
		for i, d := range v.Issues {
			items[i] = store.BrainstormTaskItem{DraftID: d.DraftID, Ordinal: i + 1, Generation: d.Generation, Title: d.Title, Body: d.Body, Marker: d.Marker, Ledger: d.Ledger, LedgerDigest: d.LedgerDigest}
		}
		batch, _, _, err = b.DB.AdmitBrainstormTaskBatch(ctx, batch, items, store.BrainstormTaskContinuation{}, at)
		if err != nil {
			return owneraction.Result{State: "failed", Summary: "The brainstorm tasks could not be admitted. Nothing was created; request a fresh proposal."}, nil //nolint:nilerr // The admission is atomic; nothing was written.
		}
		return owneraction.Result{State: "executing", Summary: fmt.Sprintf("Creating %d issues one at a time, then starting Plan for each.", len(items)), Reference: batch.ID}, nil
	case "continue", "recreate":
		c := store.BrainstormTaskContinuation{BatchID: v.BatchID, ExecutionID: key, Kind: v.Kind, Items: v.Items, FrozenDigest: v.FrozenDigest}
		var recreate *store.BrainstormTaskItem
		if v.Kind == "recreate" && len(v.Issues) == 1 {
			d := v.Issues[0]
			recreate = &store.BrainstormTaskItem{ID: v.RecreateItem, Generation: d.Generation, Title: d.Title, Body: d.Body, Marker: d.Marker}
		}
		if _, _, err = b.DB.AdmitBrainstormTaskContinuation(ctx, c, recreate, at); err != nil {
			if errors.Is(err, store.ErrBrainstormFrozenChanged) {
				return owneraction.Result{State: "failed", Summary: brainstormFresh + ". Nothing was created."}, nil
			}
			return owneraction.Result{State: "failed", Summary: "The brainstorm batch could not be continued. Nothing was created; request a fresh proposal."}, nil //nolint:nilerr // The admission is atomic; nothing was written.
		}
		return owneraction.Result{State: "executing", Summary: "Continuing batch " + v.BatchID + " from its saved progress.", Reference: v.BatchID}, nil
	}
	return owneraction.Result{}, owneraction.ErrIntegrity
}

// Recover reports live per-draft progress and settles when the continuation settles.
func (b BrainstormTasks) Recover(ctx context.Context, _ owneraction.Principal, f owneraction.Facts, key string, _ time.Time) (owneraction.Result, bool, error) {
	if _, err := unpackBrainstorm(f); err != nil {
		return owneraction.Result{}, false, err
	}
	c, err := b.DB.BrainstormTaskContinuationByExecution(ctx, key)
	if errors.Is(err, store.ErrNotFound) {
		return owneraction.Result{}, false, nil
	}
	if err != nil {
		return owneraction.Result{}, false, err
	}
	items, err := b.DB.BrainstormTaskItems(ctx, c.BatchID)
	if err != nil {
		return owneraction.Result{}, false, err
	}
	frozen := map[string]bool{}
	for _, it := range c.Items {
		frozen[it.ItemID] = true
	}
	var lines []string
	for _, it := range items {
		if !frozen[it.ID] {
			continue
		}
		line := "- " + it.Title + ": " + strings.ReplaceAll(it.State, "_", " ")
		if it.IssueLink != "" {
			line += " " + it.IssueLink
		}
		if it.Reason != "" {
			line += " (" + it.Reason + ")"
		}
		lines = append(lines, line)
	}
	state := "executing"
	if c.State == "settled" {
		state = "succeeded"
	}
	summary := "Batch " + c.BatchID + " progress:\n" + strings.Join(lines, "\n")
	if b.Protect != nil {
		if summary, err = b.Protect(ctx, summary); err != nil {
			return owneraction.Result{}, false, err
		}
	}
	return owneraction.Result{State: state, Summary: summary, Reference: c.BatchID}, true, nil
}
