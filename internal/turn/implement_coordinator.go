package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// bind supplies the coordinator's trusted authority from factory state: the
// parent, the generation, the unfinished stories in plan order with the branch of
// each story's own task, and the parent's branch they merge into. Nothing here
// comes from agent text. A story already complete counts as merged for the
// stories that depend on it, so it is left out of the set and out of their
// prerequisites.
func (rt *implementRuntime) bind(ctx context.Context, opts Options, _ ActivityRequest) (ImplementAuthority, error) {
	if rt.worker != nil {
		return ImplementAuthority{}, errors.New("a story worker has no coordinator authority")
	}
	parent := rt.parentRef.String()
	feature, err := opts.DB.GetManagedFeature(ctx, parent)
	if err != nil {
		return ImplementAuthority{}, fmt.Errorf("read the managed feature: %w", err)
	}
	if feature.State != "implementing" {
		return ImplementAuthority{}, errors.New("managed stories are not approved for implementation")
	}
	revisions, err := opts.DB.ListApprovedStoryRevisions(ctx, parent, feature.Generation)
	if err != nil {
		return ImplementAuthority{}, fmt.Errorf("read approved stories: %w", err)
	}
	session, err := opts.DB.GetSession(ctx, rt.req.SessionID)
	if err != nil {
		return ImplementAuthority{}, err
	}
	if strings.TrimSpace(session.Branch) == "" {
		return ImplementAuthority{}, errors.New("the feature branch of this task does not exist yet, so stories have nothing to merge into")
	}
	repo, err := executionRepository(ctx, opts.DB, session)
	if err != nil {
		return ImplementAuthority{}, err
	}
	returned, returnErr := opts.DB.LatestValidateReturn(ctx, parent, int64(feature.Generation))
	if returnErr != nil && !errors.Is(returnErr, store.ErrNotFound) {
		return ImplementAuthority{}, returnErr
	}
	selected := map[string]bool{}
	if returnErr == nil {
		if !returned.Applied {
			for _, r := range revisions {
				selected[r.Key] = true
			}
		} else {
			var allocation []agentresult.ReturnAllocation
			if err := json.Unmarshal([]byte(returned.Selection), &allocation); err != nil {
				return ImplementAuthority{}, err
			}
			rows, e := opts.DB.ListImplementStories(ctx, parent, int64(feature.Generation))
			if e != nil {
				return ImplementAuthority{}, e
			}
			for _, s := range allocation {
				for _, r := range rows {
					if s.Story == r.Key && r.State != store.ImplementMerged {
						selected[s.Story] = true
					}
				}
			}
		}
	}
	unfinished := map[string]bool{}
	for _, r := range revisions {
		if selected[r.Key] || r.State != "completed" && !r.EngineeringComplete {
			unfinished[r.Key] = true
		}
	}
	authority := ImplementAuthority{Parent: rt.parentRef, Generation: int64(feature.Generation), IntegrationBranch: session.Branch, BaseBranch: repo.BaseBranch}
	for _, r := range revisions {
		if !unfinished[r.Key] {
			continue
		}
		storySession, err := opts.DB.GetOrCreateSession(ctx, repo, r.IssueNumber)
		if err != nil {
			return ImplementAuthority{}, fmt.Errorf("find the task of story %s: %w", r.Key, err)
		}
		branch := storySession.Branch
		if branch == "" {
			slug, err := naming.Slug(r.Title)
			if err != nil {
				return ImplementAuthority{}, fmt.Errorf("the slug of story %s: %w", r.Key, err)
			}
			if branch, err = naming.Branch(repo.FeaturePrefix, r.IssueNumber, slug); err != nil {
				return ImplementAuthority{}, fmt.Errorf("the branch of story %s: %w", r.Key, err)
			}
		}
		var dependencies []string
		for _, key := range r.Dependencies {
			if unfinished[key] {
				dependencies = append(dependencies, key)
			}
		}
		authority.Stories = append(authority.Stories, ImplementStory{Key: r.Key, Name: r.Title, Branch: branch, IssueNumber: r.IssueNumber, Dependencies: dependencies})
	}
	return authority, nil
}

// implementQuestionCandidate builds the one question the owner gets after the
// hand-off: every parked story, by key, with its reason in plain language, and
// how to answer.
func implementQuestionCandidate(parked []store.ImplementStory) agentresult.Result {
	noun := "stories are stopped and need"
	if len(parked) == 1 {
		noun = "story is stopped and needs"
	}
	candidate := agentresult.Result{
		Outcome: "incomplete",
		Comment: fmt.Sprintf("The work that could be done is done. %d %s your decision. Each one keeps its branch and everything built so far.", len(parked), noun),
		Reason:  "Reply on this issue to restart the stopped stories. " + store.ImplementResumeGuidance,
		Payload: agentresult.Payload{Implement: &agentresult.Implement{}},
	}
	for i, row := range parked {
		where := "its worker could not finish it"
		if row.ParkOrigin == store.ImplementOriginMerge {
			where = "it could not be merged into the feature branch"
		}
		candidate.Questions = append(candidate.Questions, agentresult.Question{
			ID: fmt.Sprintf("q%d", i+1), Audience: "business",
			Text: fmt.Sprintf("**%s**: %s. %s", row.Key, where, sentence(row.Reason)),
		})
	}
	return candidate
}

// ask publishes the one batched owner question on the parent, listing every
// parked story, and opens the owner wait. The wait rests on the coordinator base
// sync (the merge step in older graphs), so the owner's reply resumes the
// coordinator where it brings main into the feature branch, then merges and
// starts stories.
func (rt *implementRuntime) ask(ctx context.Context, opts Options, a ActivityRequest) (ActivityResult, error) {
	rows, err := opts.DB.ListImplementStories(ctx, rt.parentRef.String(), rt.generation)
	if err != nil {
		return ActivityResult{}, err
	}
	var parked []store.ImplementStory
	for _, row := range rows {
		if row.State == store.ImplementParked {
			parked = append(parked, row)
		}
	}
	if len(parked) == 0 {
		return ActivityResult{}, errors.New("no story is parked, so there is nothing to ask")
	}
	source, err := opts.DB.LatestGraphNodeAttempt(ctx, a.InstanceID, a.Attrs["lf_source"])
	if err != nil {
		return ActivityResult{}, fmt.Errorf("find the step the owner's reply resumes: %w", err)
	}
	if source.Status != store.GraphSucceeded {
		return ActivityResult{}, errors.New("the step the owner's reply resumes has not finished")
	}
	policy := ImplementWorkflowPolicy()
	scope := policy.baseContext(true)
	posted, key, err := opts.ResearchReporter.ReportWorkflowCandidate(ctx, source, a.Node, "question", implementQuestionCandidate(parked), scope, "", policy)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	payload, err := json.Marshal(struct {
		DeliveryKey string `json:"delivery_key"`
		CommentID   int64  `json:"comment_id"`
		Stories     int    `json:"stories"`
	}{key, posted.CommentID, len(parked)})
	if err != nil {
		return ActivityResult{}, err
	}
	return ActivityResult{Outcome: "published", Kind: store.GraphOutputJSON, Output: payload}, nil
}

// implementCompletionCandidate builds the one reply the parent gets when every
// story of the generation is merged: the feature branch and its head commit, one
// line per story with its merge commit in ledger order, and the next step. The
// head commit is freshly verified against all listed story heads.
func implementCompletionCandidate(rows []store.ImplementStory, branch, head string, autoStart bool) agentresult.Result {
	noun := "stories are"
	if len(rows) == 1 {
		noun = "story is"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Implement is finished: all %d %s merged into `%s`, now at commit `%s`.\n", len(rows), noun, branch, head)
	for _, row := range rows {
		fmt.Fprintf(&b, "\n- **%s**: merged as `%s`", row.Key, row.MergeCommit)
	}
	next := "**Next step:** start Validate by commenting `" + ValidateStartCommand + "`"
	if autoStart {
		next = "**Next step:** Validate will start automatically."
	}
	return agentresult.Result{
		Outcome: "completed",
		Comment: b.String(),
		Reason:  next,
		Payload: agentresult.Payload{Implement: &agentresult.Implement{}},
	}
}

// reportDone posts the completion reply on the parent once every story of the
// generation is merged. The reply is identified by the merge step's attempt and
// its exact bytes, so a retried or restarted step posts nothing new. When a
// story is not merged it posts nothing.
func (rt *implementRuntime) reportDone(ctx context.Context, opts Options, a ActivityRequest) (ActivityResult, error) {
	rows, err := opts.DB.ListImplementStories(ctx, rt.parentRef.String(), rt.generation)
	if err != nil {
		return ActivityResult{}, err
	}
	merged := len(rows) > 0 && !slices.ContainsFunc(rows, func(r store.ImplementStory) bool { return r.State != store.ImplementMerged })
	if !merged {
		return implementResult("published", map[string]any{"posted": false, "reason": "not every story is merged, so there is no completion to report"})
	}
	session, err := opts.DB.GetSession(ctx, rt.req.SessionID)
	if err != nil {
		return ActivityResult{}, err
	}
	source, err := opts.DB.LatestGraphNodeAttempt(ctx, a.InstanceID, a.Attrs["lf_source"])
	if err != nil {
		return ActivityResult{}, fmt.Errorf("find the merge step the completion reply belongs to: %w", err)
	}
	if source.Status != store.GraphSucceeded {
		return ActivityResult{}, errors.New("the merge step the completion reply belongs to has not finished")
	}
	policy := ImplementWorkflowPolicy()
	head, stale, err := inspectImplementMerged(ctx, implementRepoLoader{rt: rt}, session.Branch, rows)
	if err != nil {
		return ActivityResult{}, fmt.Errorf("verify implement completion: %w", err)
	}
	if len(stale) > 0 {
		return ActivityResult{}, fmt.Errorf("story %s has published work absent from feature head %s", stale[0].Key, head)
	}
	candidate := implementCompletionCandidate(rows, session.Branch, head, opts.ValidateAutoStart)
	posted, key, err := opts.ResearchReporter.ReportWorkflowCandidate(ctx, source, a.Node, "final", candidate, policy.baseContext(true), "", policy)
	if err != nil {
		return ActivityResult{Diagnostic: err.Error()}, err
	}
	return implementResult("published", struct {
		Posted      bool   `json:"posted"`
		DeliveryKey string `json:"delivery_key"`
		CommentID   int64  `json:"comment_id"`
		Stories     int    `json:"stories"`
	}{true, key, posted.CommentID, len(rows)})
}

// sentence makes a reason read as a sentence: it starts with a capital letter and
// ends with a full stop.
func sentence(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return "No reason was recorded."
	}
	runes := []rune(text)
	runes[0] = unicode.ToUpper(runes[0])
	text = string(runes)
	if !strings.ContainsAny(text[len(text)-1:], ".!?") {
		text += "."
	}
	return text
}
