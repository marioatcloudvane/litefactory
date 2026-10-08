package daemon

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/store/creationfixture"
	"github.com/cloudvane/litefactory/internal/turn"
)

// The embedded runner has no execution implementation: guidance must only observe
// the native capability and must never execute work to discover it.
type guidanceNativeRunner struct{ turn.Runner }

func (guidanceNativeRunner) SupportsMCP() bool { return true }

func TestCreationGuidanceIOPreparation(t *testing.T) {
	for _, path := range []string{"legacy", "graph", "unavailable"} {
		for _, recovered := range []bool{false, true} {
			name := path + "/fresh"
			if recovered {
				name = path + "/recovered"
			}
			t.Run(name, func(t *testing.T) {
				f := newPublicationIO(t)
				task := rejectionTask(t, f)
				selected := path
				if selected == "unavailable" {
					selected = "legacy"
				}
				_, err := f.db.AdmitResearchPath(t.Context(), f.session.ID, selected)
				publicationMust(t, err)
				f.d.defaultAgent = agent.Kind("claude")
				executor, ok := f.d.turn.(turnExecutor)
				if !ok {
					t.Fatal("fixture does not use the real turn executor")
				}
				executor.opts.Runners = turn.RunnerSet{agent.Kind("claude"): guidanceNativeRunner{}}
				f.d.turn = executor
				if path == "unavailable" {
					f.runtime.DisableMCP = true
				}
				control := successfulPreparationControl(t, f, 70, "Exact guidance proposal")
				if recovered {
					_, err = f.runtime.PrepareCreationProposal(t.Context(), control)
					publicationMust(t, err)
					rejectionRestart(t, f)
					task.tickAt = f.now
				}
				body := control.Body
				if recovered {
					body = "edited command must not replace retained proposal"
				}
				publicationMust(t, f.d.handleComment(t.Context(), task, rejectionSource(f, body)))
				original, err := f.db.GetCreationPreparation(t.Context(), store.CreationSource{Repository: control.Repository, SourceTask: control.SourceTask, CommentID: 70, OwnerID: 10})
				publicationMust(t, err)
				if f.posts != 1 || f.creationPosts != 0 {
					t.Fatalf("posts=%d publications=%d", f.posts, f.creationPosts)
				}
				reply := f.bodies[0]
				// Factory publishing needs no agent MCP launch, so even a
				// disabled MCP runtime publishes on approval, on either path.
				expected := []string{string(original.Envelope), "`/mcp-create " + original.Hash + " linked`", "Approving publishes these exact issues right away", "no further comment or run is needed"}
				for _, want := range expected {
					if !strings.Contains(reply, want) {
						t.Errorf("missing %q in %s", want, reply)
					}
				}
				runs, err := f.db.ListRuns(t.Context(), f.session.ID)
				publicationMust(t, err)
				if len(runs) != 0 {
					t.Fatalf("guidance started runs: %+v", runs)
				}
				creationfixture.NoCreationIntents(t, store.DSN(f.db.Path()), f.session.TaskRef)
				rejectionWatermark(t, f, 70)
			})
		}
	}
}
