package factory

import (
	"context"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/store"
	"strings"
	"time"
)

// TaskStatus owns interpretation of factory activity, owner waits and task progress.
type TaskStatus struct{ db *store.DB }

// NewTaskStatus binds the existing local factory state for trusted composition.
func NewTaskStatus(db *store.DB) *TaskStatus { return &TaskStatus{db: db} }

// Task returns existing status for one known session without exposing its rows.
func (s *TaskStatus) Task(ctx context.Context, sessionID int64, at time.Time) (string, error) {
	session, err := s.db.GetSession(ctx, sessionID)
	if err != nil {
		return "", err
	}
	facts, err := NewTaskFacts(s.db).readTaskSessions(ctx, []store.Session{session})
	if err != nil {
		return "", err
	}
	return s.taskStatus(ctx, session, facts[0], at)
}

// inFlightSource words the dispatch source of an in-flight worker that has no
// run or graph step. Ordinary candidate sources are the worker checking the
// task for input; the factory's own dispatches are named as they are.
func inFlightSource(label string) string {
	switch label {
	case "due_schedule", "graph_step", "automatic_continuation", "stopped_result":
		return label
	case "":
		return "checking for input"
	}
	return "checking for input (" + strings.ReplaceAll(label, "+", ", ") + ")"
}

// Summary lists, from the store, what is running now, what is queued for a
// slot or a retry, and what waits on the owner. The Running now count is the
// number of listed lines. An in-flight task with no run or graph step is named
// by its task and the dispatch source that started its worker; an in-flight
// session that cannot be resolved is counted in the header.
func (s *TaskStatus) Summary(ctx context.Context, v contracts.Health, at time.Time) contracts.StatusListing {
	var shown []contracts.ShownTask
	repos, err := s.db.ListRepos(ctx)
	if err != nil {
		return contracts.StatusListing{Text: "\nTask listing unavailable: registered projects could not be read."}
	}
	githubRepo := map[int64]store.Repo{}
	for _, repo := range repos {
		if repo.Forge == "github" {
			githubRepo[repo.ID] = repo
		}
	}
	// task records a listed task so the owner can act on "that one" later.
	task := func(repoID int64, issue int, text string) statusEntry {
		return statusEntry{text: text, item: contracts.ShownTask{RepoID: repoID, Project: githubRepo[repoID].Slug(), Issue: issue}}
	}

	var running, queued []statusEntry
	busy := map[int64]bool{}
	active, err := s.db.ListActiveTaskWork(ctx)
	if err != nil {
		running = append(running, statusEntry{text: "Running task data could not be read."})
	}
	for _, w := range active {
		busy[w.SessionID] = true
		if _, ok := githubRepo[w.RepoID]; !ok {
			continue
		}
		mode := w.Mode
		if mode == "" {
			mode = "unavailable"
		}
		line := w.TaskRef + " · " + mode
		if w.Node != "" {
			line += " · step " + w.Node
		}
		running = append(running, task(w.RepoID, w.IssueNumber, line+" · since "+statusClock(w.Since, at)))
	}
	background, err := s.db.ListBackgroundWork(ctx, at)
	if err != nil {
		running = append(running, statusEntry{text: "Background work data could not be read."})
	}
	for _, w := range background {
		line := w.Kind + " · " + w.What
		if w.Queued {
			queued = append(queued, statusEntry{text: line + " · waiting since " + statusClock(w.Since, at)})
		} else {
			running = append(running, statusEntry{text: line + " · since " + statusClock(w.Since, at)})
		}
	}
	// Only a live daemon's slots count. A slot with no task work is named by
	// its task and real dispatch source; one that cannot be resolved is counted.
	unidentified := 0
	inFlight := map[int64]bool{}
	if v.Running {
		if len(v.InFlightSessions) > 0 {
			for _, id := range v.InFlightSessions {
				if busy[id] {
					continue
				}
				session, err := s.db.GetSession(ctx, id)
				if _, ok := githubRepo[session.RepoID]; err != nil || !ok {
					unidentified++
					continue
				}
				inFlight[id] = true
				running = append(running, task(session.RepoID, session.IssueNumber, session.TaskRef+" · "+inFlightSource(v.InFlightLabels[id])))
			}
		} else {
			unidentified = max(0, v.RunsInFlight-len(busy))
		}
	}

	slot := "waiting for the next heartbeat"
	if v.SlotsTotal > 0 && v.SlotsUsed >= v.SlotsTotal {
		slot = fmt.Sprintf("waiting for a free slot (%d/%d busy)", v.SlotsUsed, v.SlotsTotal)
	}
	waits, err := s.db.ListQueuedTaskWork(ctx, at)
	if err != nil {
		queued = append(queued, statusEntry{text: "Queued task data could not be read."})
	}
	listed := map[int64]bool{}
	for _, w := range waits {
		if _, ok := githubRepo[w.RepoID]; busy[w.SessionID] || inFlight[w.SessionID] || listed[w.SessionID] || !ok {
			continue
		}
		listed[w.SessionID] = true
		line := w.TaskRef + " · "
		if w.Node != "" {
			line += "step " + w.Node + " · "
		}
		if w.DueAt.After(at) {
			line += "retry at " + statusClock(w.DueAt, at)
		} else {
			line += slot
		}
		queued = append(queued, task(w.RepoID, w.IssueNumber, line))
	}

	waiting, complete := s.ownerWaits(ctx, repos, busy, at)
	if complete && len(waiting) == 0 {
		waiting = append(waiting, statusEntry{text: "none."})
	}

	header := fmt.Sprintf("Running now: %d", len(running))
	if unidentified > 0 {
		header += fmt.Sprintf(" (+%d unidentified)", unidentified)
	}
	sections := []struct {
		header string
		lines  []statusEntry
	}{{header + ".", running}, {fmt.Sprintf("Queued: %d.", len(queued)), queued}, {"Waiting on you:", waiting}}
	// Keep the factual source bounded; presentation retains it as details when needed.
	text := "\nScope: registered GitHub code projects and chat background work; running, queued and owner-waiting work, including parked work. Idle tasks require a task-specific status lookup.\n"
	for _, section := range sections {
		text += section.header + "\n"
		for i, line := range section.lines {
			if len(text)+len(line.text) > 16000 {
				text += fmt.Sprintf("Task listing: %d additional entries left out.\n", len(section.lines)-i)
				break
			}
			text += "- " + line.text + "\n"
			// Only shown tasks are recorded.
			if line.item.RepoID > 0 {
				shown = append(shown, line.item)
			}
		}
	}
	return contracts.StatusListing{Text: strings.TrimRight(text, "\n"), Shown: shown}
}

// statusEntry is one listed line and, for a task, the issue it shows.
type statusEntry struct {
	text string
	item contracts.ShownTask
}

// ownerWaits lists tasks that wait on the owner: a published question or
// review, or a parked story. Busy tasks are listed as running instead.
func (s *TaskStatus) ownerWaits(ctx context.Context, repos []store.Repo, busy map[int64]bool, at time.Time) ([]statusEntry, bool) {
	reader := NewTaskFacts(s.db)
	var lines []statusEntry
	complete := true
	for _, repo := range repos {
		if repo.Forge != "github" {
			continue
		}
		var after int64
		for {
			sessions, err := s.db.ListTaskSessionsPage(ctx, repo.ID, after, 100)
			if err != nil {
				complete = false
				lines = append(lines, statusEntry{text: repo.Slug() + ": task data could not be read."})
				break
			}
			if len(sessions) == 0 {
				break
			}
			after = sessions[len(sessions)-1].ID
			tasks, err := reader.readTaskSessions(ctx, sessions)
			if err != nil {
				complete = false
				lines = append(lines, statusEntry{text: repo.Slug() + ": task data could not be read."})
				break
			}
			for index, task := range tasks {
				if busy[sessions[index].ID] || task.OwnerWait == nil && task.OwnerBlocked == "" {
					continue
				}
				text, err := s.taskStatus(ctx, sessions[index], task, at)
				if err != nil {
					complete = false
					lines = append(lines, statusEntry{text: task.Reference + ": task data could not be read."})
				} else {
					lines = append(lines, statusEntry{text: text, item: contracts.ShownTask{RepoID: repo.ID, Project: repo.Slug(), Issue: sessions[index].IssueNumber}})
				}
			}
		}
	}
	return lines, complete
}

// statusClock shows a time in the reply's zone: the clock alone today, with
// the day otherwise.
func statusClock(t, at time.Time) string {
	t = t.In(at.Location())
	if y, m, d := t.Date(); y == at.Year() && m == at.Month() && d == at.Day() {
		return t.Format("15:04")
	}
	return t.Format("Mon 2 Jan 15:04")
}

// taskStatus uses retained state; absent facts stay explicitly unavailable.
func (s *TaskStatus) taskStatus(ctx context.Context, session store.Session, task contracts.Task, at time.Time) (string, error) {
	mode, stage, waiting, state := "unavailable", "unavailable", "none recorded", task.State
	runs, err := s.db.RecentTaskRuns(ctx, session.ID, 3)
	if err != nil {
		return "", err
	}
	if len(runs) > 0 {
		mode = runs[0].Mode
		if mode == "" {
			mode = runs[0].Workflow
		}
		if mode == "" {
			mode = "unavailable"
		}
	}
	instance, err := s.db.LatestSessionGraphInstance(ctx, session.ID)
	if err == nil {
		stage = instance.CurrentNode + " (" + instance.Status + ")"
		definition, err := s.db.GetGraphDefinition(ctx, instance.DefinitionID)
		if err != nil {
			return "", err
		}
		mode = definition.Mode
		if mode == "" {
			mode = definition.Workflow
		}
		if mode == "" {
			mode = "unavailable"
		}
		if instance.Status == store.GraphRunning {
			// A running graph supersedes the failed flag an earlier attempt left.
			if state == string(store.SessionFailed) {
				state = "in progress (an earlier attempt failed; work resumed)"
			}
			dispatch, err := s.db.LatestGraphDispatch(ctx, instance.ID)
			switch {
			case err == nil && dispatch.Status == "pending":
				stage += ", next step due " + dispatch.DueAt.UTC().Format(time.RFC3339)
			case err == nil && (dispatch.Status == "reserved" || dispatch.Status == "running"):
				stage += ", next step starting"
			case err != nil && !errors.Is(err, store.ErrNotFound):
				return "", err
			}
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	var stories []store.ImplementStory
	if mode == "implement" {
		if stories, err = s.db.LatestImplementStories(ctx, session.TaskRef); err != nil {
			return "", err
		}
	}
	progress := ""
	if len(stories) > 0 {
		var parked string
		progress, parked, err = s.implementProgress(ctx, stories, at)
		if err != nil {
			return "", err
		}
		if parked != "" {
			waiting = "owner — " + parked
		}
	} else if len(task.PlanStages) > 0 {
		stage += "; plan stories: " + strings.Join(task.PlanStages, "; ")
	}
	if task.OwnerWait != nil {
		waiting = "owner — " + task.OwnerWait.Workflow + " " + task.OwnerWait.Kind
	}
	if task.OwnerBlocked != "" {
		waiting = "owner — parked: " + task.OwnerBlocked
		story, err := s.db.ImplementStoryByWorkerSession(ctx, session.ID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return "", err
		}
		if err == nil && story.State == store.ImplementParked {
			waiting = "owner — " + parkedWait(story, "story "+story.Key, at)
		}
	}
	text := fmt.Sprintf("%s: %s. Mode: %s. Stage: %s.%s Waiting on: %s. Recent runs on this task (newest first, limit 3):", task.Reference, state, mode, stage, progress, waiting)
	if len(runs) == 0 {
		text += " unavailable."
	}
	for _, run := range runs {
		text += fmt.Sprintf(" %d: %s (%s);", run.Ordinal, run.Status, run.Mode)
	}
	return text, nil
}

// implementProgress reports a feature's Implement ledger from the local store:
// per-story state of the newest generation with story issue numbers, failed
// attempts and the latest failure, plus active runs on the story tasks. It
// also returns the first parked story, which waits on the owner.
func (s *TaskStatus) implementProgress(ctx context.Context, stories []store.ImplementStory, at time.Time) (string, string, error) {
	counts := map[string]int{}
	var lines, active []string
	parked := ""
	for _, story := range stories {
		counts[story.State]++
		label, latest := story.Key, ""
		if story.WorkerSession > 0 {
			worker, err := s.db.GetSession(ctx, story.WorkerSession)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return "", "", err
			}
			if err == nil {
				label += fmt.Sprintf(" (#%d)", worker.IssueNumber)
				run, err := s.db.LatestTaskRun(ctx, worker.ID)
				if err != nil && !errors.Is(err, store.ErrNotFound) {
					return "", "", err
				}
				switch {
				case err == nil && (run.Status == store.RunRunning || run.Status == store.RunPrelaunch):
					active = append(active, fmt.Sprintf("#%d run %d (%s) running since %s", worker.IssueNumber, run.Ordinal, run.Mode, run.StartedAt.UTC().Format(time.RFC3339)))
				case err == nil && story.State == store.ImplementRunning:
					latest = fmt.Sprintf(", no active run (latest run %d %s)", run.Ordinal, run.Status)
				}
			}
		}
		line := label + " " + story.State + latest
		if story.Attempts > 0 {
			line += fmt.Sprintf(", %d failed attempts", story.Attempts)
		}
		if story.State == store.ImplementParked {
			line += ", parked: " + statusExcerpt(story.Reason)
			if parked == "" {
				parked = parkedWait(story, label, at)
			}
		}
		if story.State != store.ImplementParked && story.State != store.ImplementMerged && story.GuidanceQuestion != "" && story.Guidance == "" {
			line += ", restarted after: " + statusExcerpt(story.GuidanceQuestion)
		}
		if story.LastFailure != "" && story.State != store.ImplementMerged {
			line += ", last failure: " + statusExcerpt(story.LastFailure)
		}
		lines = append(lines, line)
	}
	var tally []string
	for _, state := range []string{store.ImplementMerged, store.ImplementDone, store.ImplementRunning, store.ImplementWaiting, store.ImplementParked} {
		if counts[state] > 0 {
			tally = append(tally, fmt.Sprintf("%d %s", counts[state], state))
		}
	}
	text := fmt.Sprintf(" Implementation stories (generation %d, %d total): %s. Per story: %s.", stories[0].Generation, len(stories), strings.Join(tally, ", "), strings.Join(lines, "; "))
	if len(active) == 0 {
		text += " Active story runs: none."
	} else {
		text += " Active story runs: " + strings.Join(active, "; ") + "."
	}
	return text, parked, nil
}

// parkedWait is a parked story's owner wait: since when, why, and the reply on
// the parent issue that restarts it, in the owner question's own words.
func parkedWait(story store.ImplementStory, label string, at time.Time) string {
	since := story.StateAt
	if since.IsZero() {
		since = story.UpdatedAt
	}
	return fmt.Sprintf("parked since %s (%s): %s. To resume: reply on %s to restart the stopped stories. %s", statusClock(since, at), label, strings.TrimSuffix(statusExcerpt(story.Reason), "."), story.Parent, store.ImplementResumeGuidance)
}

// statusExcerpt keeps a ledger reason on one bounded line.
func statusExcerpt(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 200 {
		return factExcerpt(text, 200) + "…"
	}
	return text
}
