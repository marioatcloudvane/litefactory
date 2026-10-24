package ask

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

// Facts limits bound live issue reads, excerpts and the complete encoded bundle.
const (
	MaxIssues    = 50
	MaxIssueBody = 2 << 10
	MaxFactsSize = 64 << 10
)

// Fact identifies one fresh observation. Text is untrusted model input.
type Fact struct {
	Handle string    `json:"handle"`
	Kind   string    `json:"kind"`
	Link   string    `json:"link,omitempty"`
	ReadAt time.Time `json:"read_at"`
	Text   string    `json:"text"`
}

// Bundle is the factory-owned citation catalog for one answer.
type Bundle struct {
	CapturedAt  time.Time `json:"captured_at"`
	Project     string    `json:"project"`
	TargetIssue int       `json:"target_issue,omitempty"`
	Facts       []Fact    `json:"facts"`
	Gaps        []string  `json:"gaps"`
}

// Task is a read-only projection; it contains no clone paths or credentials.
type Task struct {
	IssueNumber  int
	Reference    string
	State        string
	LatestRun    *store.Run
	PlanStages   []string
	OwnerWait    *OwnerWait
	OwnerBlocked string
}

// OwnerWait is a published owner question or review still waiting for a reply.
type OwnerWait struct {
	Workflow, Kind, Node, Asked string
	CommentID                   int64
	Since                       time.Time
}

// FactoryReader builds a fresh bounded project projection. Incomplete reports
// omitted task data; source errors are exposed as gaps rather than fabricated
// task state. The implementation must not hold a database cursor across I/O.
type FactoryReader interface {
	ReadProjectTasks(context.Context, int64) (tasks []Task, incomplete bool, err error)
	ReadDiscoveryPause(context.Context) (paused bool, known bool, err error)
}

// IssueReader reads open issues newest-updated first, returning at most limit
// issues and whether more exist. The bounded API belongs to the forge adapter;
// this service never receives a client that can create or edit an issue.
type IssueReader interface {
	ReadOpenIssues(context.Context, github.Repo, int) (issues []github.Issue, incomplete bool, err error)
}

// Collector has only read capabilities. Every collection is fresh and ephemeral.
type Collector struct {
	Factory FactoryReader
	Issues  IssueReader
}

// Collect records omissions explicitly and caps the complete encoded bundle,
// including fact metadata and gaps, rather than only counting issue body bytes.
func (c Collector) Collect(ctx context.Context, repo store.Repo, at time.Time) (Bundle, error) {
	return c.collect(ctx, repo, at, 0)
}

func (c Collector) collect(ctx context.Context, repo store.Repo, at time.Time, target int) (Bundle, error) {
	b := Bundle{TargetIssue: target, CapturedAt: at.UTC(), Project: repo.Slug(), Facts: []Fact{}, Gaps: []string{}}
	if c.Factory == nil || repo.ID < 1 || repo.Forge != "github" || at.IsZero() {
		return b, errors.New("ask: invalid facts collection context")
	}
	if err := (github.Repo{Owner: repo.Owner, Name: repo.Name}).Validate(); err != nil {
		return b, err
	}
	base := "https://github.com/" + repo.Slug()
	omitted := false
	add := func(f Fact) {
		if omitted {
			return
		}
		b.Facts = append(b.Facts, f)
		if encodedSize(b) > MaxFactsSize-1024 {
			b.Facts = b.Facts[:len(b.Facts)-1]
			omitted = true
		}
	}
	fact := func(handle, kind, link, text string) Fact {
		return Fact{Handle: handle, Kind: kind, Link: link, ReadAt: at.UTC(), Text: text}
	}
	add(fact("repository", "repository", base, fmt.Sprintf("Registered project %s; base branch %s.", repo.Slug(), byteExcerpt(repo.BaseBranch, 256))))
	paused, known, err := c.Factory.ReadDiscoveryPause(ctx)
	if err != nil || !known {
		b.Gaps = append(b.Gaps, "Factory discovery pause state could not be read.")
	} else {
		add(fact("discovery", "discovery", "", fmt.Sprintf("Discovery paused: %t.", paused)))
	}
	tasks, tasksCut, taskErr := c.Factory.ReadProjectTasks(ctx, repo.ID)
	if taskErr != nil {
		b.Gaps = append(b.Gaps, "Factory task data could not be read.")
	}
	if tasksCut {
		b.Gaps = append(b.Gaps, "Additional factory tasks were left out.")
	}
	var issues []github.Issue
	var openIssues map[int]bool
	if c.Issues == nil {
		b.Gaps = append(b.Gaps, "GitHub could not be read; using factory data only.")
	} else {
		var cut bool
		issues, cut, err = c.Issues.ReadOpenIssues(ctx, github.Repo{Owner: repo.Owner, Name: repo.Name}, MaxIssues)
		switch {
		case err != nil:
			issues = nil
			b.Gaps = append(b.Gaps, "GitHub could not be read; using factory data only.")
		case cut || len(issues) > MaxIssues:
			b.Gaps = append(b.Gaps, "Additional open issues were left out (50 issue limit).")
		default:
			openIssues = map[int]bool{}
			for _, issue := range issues {
				if issue.State == github.StateOpen {
					openIssues[issue.Number] = true
				}
			}
		}
	}
	// A task is closed only when a complete open-issue read omits its issue.
	closed := func(task Task) bool { return target == 0 && openIssues != nil && !openIssues[task.IssueNumber] }
	if target > 0 {
		// The bounded project list may omit an older named task. Resolve only
		// that exact registered repository/issue through an optional reader.
		if reader, ok := c.Factory.(interface {
			ReadIssueTasks(context.Context, store.Repo, int) ([]Task, error)
		}); ok {
			selected, readErr := reader.ReadIssueTasks(ctx, repo, target)
			if readErr != nil {
				b.Gaps = append(b.Gaps, "Named issue factory evidence could not be read.")
			} else {
				tasks, taskErr = selected, nil
			}
		}
		tasks = slices.DeleteFunc(slices.Clone(tasks), func(t Task) bool { return t.IssueNumber != target })
		issues = slices.DeleteFunc(slices.Clone(issues), func(i github.Issue) bool { return i.Number != target })
		b.Facts = nil // Project-wide facts cannot support an issue-specific answer.
	}
	if err := ctx.Err(); err != nil {
		return b, err
	}
	if taskErr == nil {
		tasks = slices.Clone(tasks)
		sort.SliceStable(tasks, func(i, j int) bool { return tasks[i].IssueNumber < tasks[j].IssueNumber })
		// Owner waits are workflow state and go before issue text so the
		// bundle budget cannot crowd them out.
		seenWaits := map[int]bool{}
		for _, task := range tasks {
			w := task.OwnerWait
			if w == nil || task.IssueNumber < 1 || w.CommentID < 1 || seenWaits[task.IssueNumber] {
				continue
			}
			seenWaits[task.IssueNumber] = true
			add(fact(fmt.Sprintf("wait:%d", task.IssueNumber), "owner_wait", fmt.Sprintf("%s/issues/%d#issuecomment-%d", base, task.IssueNumber, w.CommentID), waitText(task.IssueNumber, *w, at)))
		}
		// Running work is live run state and also goes before issue text.
		seenActive := map[int]bool{}
		for _, task := range tasks {
			run := task.LatestRun
			if !runActive(run) || task.IssueNumber < 1 || seenActive[task.IssueNumber] || closed(task) {
				continue
			}
			seenActive[task.IssueNumber] = true
			add(fact(fmt.Sprintf("active:%d", task.IssueNumber), "active", fmt.Sprintf("%s/issues/%d", base, task.IssueNumber), fmt.Sprintf("#%d has a factory run in progress: run %d; mode: %s; status: %s; started: %s.", task.IssueNumber, run.Ordinal, byteExcerpt(run.Mode, 128), run.Status, run.StartedAt.UTC().Format(time.RFC3339))))
		}
		if len(seenActive) == 0 && !tasksCut && target == 0 {
			add(fact("active:none", "active", "", "No factory run is currently running."))
		}
	}
	issues = slices.Clone(issues)
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].UpdatedAt.Equal(issues[j].UpdatedAt) {
			return issues[i].Number < issues[j].Number
		}
		return issues[i].UpdatedAt.After(issues[j].UpdatedAt)
	})
	seen := map[int]bool{}
	bodyCut := false
	for _, issue := range issues[:min(len(issues), MaxIssues)] {
		if issue.State != github.StateOpen || issue.Number < 1 || seen[issue.Number] {
			continue
		}
		seen[issue.Number] = true
		body := byteExcerpt(issue.Body, MaxIssueBody)
		bodyCut = bodyCut || len(body) < len(issue.Body)
		link := fmt.Sprintf("%s/issues/%d", base, issue.Number)
		add(fact(fmt.Sprintf("issue:%d", issue.Number), "issue", link, fmt.Sprintf("#%d %s\nUpdated: %s\n%s", issue.Number, byteExcerpt(issue.Title, 512), issue.UpdatedAt.UTC().Format(time.RFC3339), body)))
	}
	if target > 0 && !seen[target] {
		b.Gaps = append(b.Gaps, "The named issue text was not captured in the bounded open-issue read; this does not establish its current state.")
	}
	if bodyCut {
		b.Gaps = append(b.Gaps, "Issue bodies were shortened (2 KiB body limit).")
	}
	if taskErr == nil {
		seenTasks := map[int]bool{}
		for _, task := range tasks {
			if task.IssueNumber < 1 || seenTasks[task.IssueNumber] || closed(task) {
				continue
			}
			seenTasks[task.IssueNumber] = true
			link := fmt.Sprintf("%s/issues/%d", base, task.IssueNumber)
			add(fact(fmt.Sprintf("task:%d", task.IssueNumber), "task", link, fmt.Sprintf("Task %s; state: %s.", byteExcerpt(task.Reference, 512), byteExcerpt(task.State, 128))))
			if run := task.LatestRun; run != nil {
				add(fact(fmt.Sprintf("run:%d/%d", task.IssueNumber, run.Ordinal), "run", link, fmt.Sprintf("Latest run %d; mode: %s; status: %s; outcome: %s; started: %s.", run.Ordinal, byteExcerpt(run.Mode, 128), run.Status, byteExcerpt(run.SemanticOutcome, 256), run.StartedAt.UTC().Format(time.RFC3339))))
			}
			for i, stage := range task.PlanStages {
				add(fact(fmt.Sprintf("plan:%d/%d", task.IssueNumber, i+1), "plan", link, byteExcerpt(stage, 512)))
			}
		}
	}
	if omitted {
		b.Gaps = append(b.Gaps, "Additional project facts were left out (64 KiB bundle limit).")
	}
	if encodedSize(b) > MaxFactsSize {
		return Bundle{}, errors.New("ask: project metadata exceeds facts bundle limit")
	}
	return b, nil
}

// runActive reports whether the factory is still executing the run.
func runActive(run *store.Run) bool {
	return run != nil && (run.Status == store.RunPrelaunch || run.Status == store.RunRunning)
}

func waitText(issue int, w OwnerWait, at time.Time) string {
	text := fmt.Sprintf("#%d is waiting for the owner: %s %s at node %s.", issue, byteExcerpt(w.Workflow, 128), byteExcerpt(w.Kind, 64), byteExcerpt(w.Node, 128))
	if w.Asked != "" {
		text += "\nAsked: " + byteExcerpt(w.Asked, 1024)
	}
	text += "\nReply on the linked issue comment."
	if !w.Since.IsZero() {
		age := max(at.Sub(w.Since), 0)
		ago := fmt.Sprintf("%d hours", int(age/time.Hour))
		if age >= 48*time.Hour {
			ago = fmt.Sprintf("%d days", int(age/(24*time.Hour)))
		}
		text += fmt.Sprintf("\nWaiting since %s (%s before capture).", w.Since.UTC().Format(time.RFC3339), ago)
	}
	return text
}

func encodedSize(b Bundle) int {
	data, _ := json.Marshal(b)
	return len(data)
}

func byteExcerpt(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	s = s[:limit]
	for !utf8.ValidString(s) && len(s) > 0 {
		s = s[:len(s)-1]
	}
	return s
}

var issueReference = regexp.MustCompile(`(?i)(?:\b(?:issue|bug)\s*#?\s*|#)([1-9][0-9]*)\b`)

// ExplicitIssues returns distinct issue numbers in encounter order. It does not
// infer an issue from a bare number, run number or PR number.
func ExplicitIssues(text string) []int {
	var out []int
	for _, m := range issueReference.FindAllStringSubmatch(text, -1) {
		n, err := strconv.Atoi(m[1])
		if err == nil && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}
