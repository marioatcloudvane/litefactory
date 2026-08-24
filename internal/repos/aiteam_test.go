package repos_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/repos"
)

// TestDetectAiteam pins the one deterministic rule (AC-1.4, AC-1.5).
//
// The table is written as much for the cases that must *not* match as for the
// ones that must. "Go Backend Team" as a heading, a roster table, a sentence
// naming a team — every one of those is the sort of thing a cleverer rule
// would read, and every one of them has to leave the team empty here. Nothing
// at M0 consumes the team, so a nearly-right answer is strictly worse than
// none: it would be believed by whatever eventually does consume it.
func TestDetectAiteam(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		files       map[string]string
		dirs        []string
		wantPresent bool
		wantTeam    string
		wantMarkers []repos.Marker
	}{
		{
			name:        "neither marker is present",
			files:       map[string]string{"README.md": "# acme-api\n"},
			wantPresent: false,
		},
		{
			name:        "CLAUDE.md that never mentions aiteam is not a marker",
			files:       map[string]string{"CLAUDE.md": "# acme-api\n\nRun the tests before you push.\n"},
			wantPresent: false,
		},
		{
			name:        "CLAUDE.md mentioning aiteam establishes presence without a team",
			files:       map[string]string{"CLAUDE.md": "# acme-api\n\nThis project is managed by aiteam.\n"},
			wantPresent: true,
			wantTeam:    "",
			wantMarkers: []repos.Marker{repos.MarkerClaudeMD},
		},
		{
			name: "CLAUDE.md marker supplies the team",
			files: map[string]string{
				"CLAUDE.md": "# acme-api\n\n<!-- aiteam:team=go-backend -->\n",
			},
			wantPresent: true,
			wantTeam:    "go-backend",
			wantMarkers: []repos.Marker{repos.MarkerClaudeMD},
		},
		{
			name: "the CLAUDE.md marker is matched case-insensitively and tolerates spacing",
			files: map[string]string{
				"CLAUDE.md": "<!--   AiTeam:Team = swift-apple   -->\n",
			},
			wantPresent: true,
			wantTeam:    "swift-apple",
			wantMarkers: []repos.Marker{repos.MarkerClaudeMD},
		},
		{
			name:        "an empty .aiteam directory establishes presence on its own",
			dirs:        []string{".aiteam"},
			wantPresent: true,
			wantTeam:    "",
			wantMarkers: []repos.Marker{repos.MarkerAiteamDir},
		},
		{
			name:        ".aiteam/team supplies the team",
			files:       map[string]string{".aiteam/team": "python-saas\n"},
			wantPresent: true,
			wantTeam:    "python-saas",
			wantMarkers: []repos.Marker{repos.MarkerAiteamDir},
		},
		{
			name:        ".aiteam/team is read to the first line only, trimmed",
			files:       map[string]string{".aiteam/team": "  go-backend  \nand some prose below\n"},
			wantPresent: true,
			wantTeam:    "go-backend",
			wantMarkers: []repos.Marker{repos.MarkerAiteamDir},
		},
		{
			name: "both markers present, the CLAUDE.md marker wins",
			files: map[string]string{
				"CLAUDE.md":    "<!-- aiteam:team=go-backend -->\n",
				".aiteam/team": "swift-apple\n",
			},
			wantPresent: true,
			wantTeam:    "go-backend",
			wantMarkers: []repos.Marker{repos.MarkerClaudeMD, repos.MarkerAiteamDir},
		},
		{
			name: "a team file below the first level is not read",
			files: map[string]string{
				".aiteam/main/team": "go-backend\n",
			},
			wantPresent: true,
			wantTeam:    "",
			wantMarkers: []repos.Marker{repos.MarkerAiteamDir},
		},
		{
			name:        "a team file that is not a legal team name yields no team",
			files:       map[string]string{".aiteam/team": "the Go Backend Team!\n"},
			wantPresent: true,
			wantTeam:    "",
			wantMarkers: []repos.Marker{repos.MarkerAiteamDir},
		},
		{
			name:        "an empty team file yields no team",
			files:       map[string]string{".aiteam/team": "\n\n"},
			wantPresent: true,
			wantTeam:    "",
			wantMarkers: []repos.Marker{repos.MarkerAiteamDir},
		},
		{
			name: "prose describing a team is not a team",
			files: map[string]string{
				"CLAUDE.md": "# acme-api\n\n## Go Backend Team\n\n" +
					"This repository is managed by aiteam. The team is go-backend.\n\n" +
					"| ID | Role |\n|---|---|\n| `go-architect` | architect |\n",
			},
			wantPresent: true,
			wantTeam:    "",
			wantMarkers: []repos.Marker{repos.MarkerClaudeMD},
		},
		{
			name:        "a directory named CLAUDE.md is not a marker",
			dirs:        []string{"CLAUDE.md"},
			wantPresent: false,
		},
		{
			name:        "a regular file named .aiteam is not a marker",
			files:       map[string]string{".aiteam": "aiteam\n"},
			wantPresent: false,
		},
		{
			name:        "a directory named .aiteam/team is not a team file",
			dirs:        []string{".aiteam/team"},
			wantPresent: true,
			wantTeam:    "",
			wantMarkers: []repos.Marker{repos.MarkerAiteamDir},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := writeTree(t, tt.files, tt.dirs)

			got, err := repos.DetectAiteam(root)
			if err != nil {
				t.Fatalf("DetectAiteam(%s) error = %v, want nil", root, err)
			}
			if got.Present != tt.wantPresent {
				t.Errorf("Present = %t, want %t", got.Present, tt.wantPresent)
			}
			if got.Team != tt.wantTeam {
				t.Errorf("Team = %q, want %q", got.Team, tt.wantTeam)
			}
			if !markersEqual(got.Markers, tt.wantMarkers) {
				t.Errorf("Markers = %v, want %v", got.Markers, tt.wantMarkers)
			}
			if got.Team == "" && got.TeamMarker != "" {
				t.Errorf("TeamMarker = %q with no team", got.TeamMarker)
			}
			if got.Team != "" && got.TeamMarker == "" {
				t.Errorf("TeamMarker is empty for team %q", got.Team)
			}
		})
	}
}

// TestDetectAiteamOnMissingRoot checks that a root that does not exist is
// reported as "no aiteam here" rather than as an error. The path check has
// already run by the time detection happens, so a missing directory can only
// mean the markers are missing too.
func TestDetectAiteamOnMissingRoot(t *testing.T) {
	t.Parallel()

	got, err := repos.DetectAiteam(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("DetectAiteam error = %v, want nil", err)
	}
	if got.Present {
		t.Errorf("Present = true for a missing root, want false")
	}
}

// writeTree materialises a table entry's files and directories under a fresh
// temporary root.
func writeTree(t *testing.T, files map[string]string, dirs []string) string {
	t.Helper()
	root := t.TempDir()

	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatalf("create directory for %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

func markersEqual(got, want []repos.Marker) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestAiteamMissingMessageNamesTheFix covers the second half of AC-1.4: the
// message a user gets has to tell them what to run.
func TestAiteamMissingMessageNamesTheFix(t *testing.T) {
	t.Parallel()

	if msg := repos.ErrAiteamMissing.Error(); !strings.Contains(msg, "npx aiteam") {
		t.Errorf("ErrAiteamMissing = %q, want it to name `npx aiteam`", msg)
	}
}
