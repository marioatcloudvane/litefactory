package turn

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/naming"
)

const parentPointerPrefix = "Factory supplied parent session directory: "

// parentContextFixture is upstreamFixture with the parent's Research session
// folder, holding files, committed and pushed on the parent's branch before
// any story task checks out from it. It ends with story-001's technical stage
// owed.
func parentContextFixture(t *testing.T, files ...string) (*fixture, Request, *upstreamAgent, string) {
	t.Helper()
	u := &upstreamAgent{}
	var fx *fixture
	inner := u.behave(t, &fx)
	f, _, base, _, _ := graphPlanAgentFixture(t, func(s planAgentStep) (*agent.RunResult, error) {
		if u.instead != nil {
			if out, ok := u.instead(s); ok {
				return out, nil
			}
		}
		return inner(s)
	})
	fx = f
	parentBranch := fmt.Sprintf("lf/%d-feature", f.session.IssueNumber)
	_, err := f.db.FreezeSessionSlug(t.Context(), f.session.ID, "feature", parentBranch)
	mustProgress(t, err)
	revision := saveProposedSet(t, f, promoteEntries())
	if _, err := Execute(t.Context(), f.opts, promoteTrigger(base, 900, 7, promoteCommand(revision))); err != nil {
		t.Fatalf("promotion: %v logs=%s", err, f.logs.String())
	}
	parent, err := f.db.GetSessionByTaskRef(t.Context(), f.ref().String())
	mustProgress(t, err)
	if parent.Branch != parentBranch {
		t.Fatalf("the parent's branch is %q, want %q", parent.Branch, parentBranch)
	}
	parentDir, err := naming.AITeamSessionDir(parent.Branch, time.Now(), parent.FeatureSlug)
	mustProgress(t, err)
	f.repo.Git("checkout", "--quiet", "-B", parent.Branch)
	f.repo.WriteFile(parentDir+"/"+naming.ManifestName, "{}\n")
	for _, name := range files {
		f.repo.WriteFile(parentDir+"/"+name, "# "+name+"\n")
	}
	f.repo.Commit("parent research")
	f.repo.Git("push", "--quiet", "--force", "origin", parent.Branch)
	f.repo.Git("checkout", "--quiet", "-")
	draftAllStories(t, f, base)
	indepApprove(t, f, base, "story-002", 940)
	state := graphPlanState(t, f)
	if stage, _, ok := state.StoryNextStep("story-001"); stage != "technical" || !ok {
		t.Fatalf("story-001 technical stage is not owed: %q %v", stage, ok)
	}
	return f, base, u, parentDir
}

// captureTechnicalPrompts records each story-001 technical-stage prompt and the
// story's checkout, and lets the stage run as usual.
func captureTechnicalPrompts(u *upstreamAgent) (*[]string, *string) {
	prompts := []string{}
	worktree := ""
	u.instead = func(s planAgentStep) (*agent.RunResult, bool) {
		if s.story == "story-001" && s.stage == "technical" {
			prompts = append(prompts, s.req.Prompt)
			worktree = s.req.WorktreePath
		}
		return nil, false
	}
	return &prompts, &worktree
}

func parentPointerLines(prompt string) []string {
	var lines []string
	for _, line := range strings.Split(prompt, "\n") {
		if strings.Contains(line, parentPointerPrefix) {
			lines = append(lines, line)
		}
	}
	return lines
}

// requireParentPointer checks that prompt names the parent directory once, in
// one line that names the brief and the technical findings.
func requireParentPointer(t *testing.T, prompt, parentDir string) {
	t.Helper()
	lines := parentPointerLines(prompt)
	if len(lines) != 1 {
		t.Fatalf("want one parent pointer, got %d:\n%s", len(lines), prompt)
	}
	for _, want := range []string{
		parentPointerPrefix + parentDir + ".",
		naming.ResearchTechnicalName + " there holds the Research findings",
		naming.ResearchBriefName + " there holds the Research decisions and assumptions",
	} {
		if strings.Count(lines[0], want) != 1 {
			t.Fatalf("the parent pointer does not name %q once:\n%s", want, lines[0])
		}
	}
	if n := strings.Count(prompt, parentDir); n != 1 {
		t.Fatalf("the parent directory is named %d times:\n%s", n, prompt)
	}
}

func parentFolderFiles(t *testing.T, worktree, parentDir string) []string {
	t.Helper()
	var files []string
	root := filepath.Join(worktree, filepath.FromSlash(parentDir))
	mustProgress(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(root, path)
			files = append(files, rel)
		}
		return nil
	}))
	slices.Sort(files)
	return files
}

// C1, C4: a graph Plan story technical stage is pointed at its parent's
// Research brief and technical findings, and a second attempt of the stage is
// pointed once again without writing into the parent's folder.
func TestPlanStoryStagePromptNamesTheParentSessionDirectory(t *testing.T) {
	f, base, u, parentDir := parentContextFixture(t, naming.ResearchBriefName, naming.ResearchTechnicalName)
	prompts, worktree := captureTechnicalPrompts(u)
	if _, err := executePlanStoryWork(t, f, base, "story-001"); err != nil {
		t.Fatalf("technical stage: %v logs=%s", err, f.logs.String())
	}
	if len(*prompts) != 1 {
		t.Fatalf("story-001's technical stage ran %d times", len(*prompts))
	}
	requireParentPointer(t, (*prompts)[0], parentDir)
	before := parentFolderFiles(t, *worktree, parentDir)

	if _, err := Execute(t.Context(), f.opts, ownerReplyRun(t, f, base, "story-001", 5101, "Please keep the technical design small.")); err != nil {
		t.Fatalf("second technical attempt: %v logs=%s", err, f.logs.String())
	}
	if len(*prompts) != 2 {
		t.Fatalf("the owner's reply did not run a second technical attempt: %d prompts", len(*prompts))
	}
	requireParentPointer(t, (*prompts)[1], parentDir)
	if after := parentFolderFiles(t, *worktree, parentDir); !slices.Equal(before, after) {
		t.Fatalf("the parent's folder changed: before %v after %v", before, after)
	}
}

// C3: a parent without a Research brief gives the story stage no parent line,
// and the stage runs as before.
func TestPlanStoryStagePromptWithoutParentBriefHasNoPointer(t *testing.T) {
	f, base, u, _ := parentContextFixture(t)
	prompts, _ := captureTechnicalPrompts(u)
	out, err := executePlanStoryWork(t, f, base, "story-001")
	if err != nil || out.Err != nil {
		t.Fatalf("technical stage: %v %+v logs=%s", err, out, f.logs.String())
	}
	if len(*prompts) != 1 {
		t.Fatalf("story-001's technical stage ran %d times", len(*prompts))
	}
	if strings.Contains((*prompts)[0], "parent session directory") {
		t.Fatalf("a parent pointer was named without parent Research files:\n%s", (*prompts)[0])
	}
	if item := indepStory(t, f, "story-001"); item.Wait == nil || item.Wait.Stage != "technical" {
		t.Fatalf("the technical result was not accepted: %+v", item)
	}
}

// C5: the parent task's own runs, such as its Plan set, are not a story and get
// no parent pointer.
func TestPlanParentTaskHasNoParentSessionDirectory(t *testing.T) {
	f, _, _, _ := parentContextFixture(t, naming.ResearchBriefName, naming.ResearchTechnicalName)
	parent, err := f.db.GetSessionByTaskRef(t.Context(), f.ref().String())
	mustProgress(t, err)
	if dir, files := parentSessionArtifacts(t.Context(), f.opts, f.repo.Dir, parent, time.Now(), f.opts.Logger); dir != "" || files != nil {
		t.Fatalf("the parent task was given a parent directory %q %v", dir, files)
	}
}
