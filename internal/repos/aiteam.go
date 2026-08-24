package repos

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Marker names one of the two places [DetectAiteam] looks.
type Marker string

const (
	// MarkerClaudeMD is the CLAUDE.md at the repository root.
	MarkerClaudeMD Marker = "CLAUDE.md"
	// MarkerAiteamDir is the .aiteam directory at the repository root.
	MarkerAiteamDir Marker = ".aiteam/"
)

// File and directory names the rule reads. They are constants so that the two
// things litefactory knows about an aiteam installation have one definition.
const (
	claudeMDName  = "CLAUDE.md"
	aiteamDirName = ".aiteam"
	teamFileName  = "team"
)

// Read limits. CLAUDE.md is a document a human wrote and the team file holds
// one word; anything larger is not a marker file and there is no reason to
// pull it into memory.
const (
	claudeMDLimit = 1 << 20 // 1 MiB
	teamFileLimit = 4 << 10 // 4 KiB
)

// presenceMarker is the literal a CLAUDE.md must contain for aiteam to count
// as present, compared case-insensitively. This is the spec's rule verbatim: "a
// CLAUDE.md at the repo root that mentions aiteam".
const presenceMarker = "aiteam"

// teamMarkerRE is the one team marker litefactory reads out of CLAUDE.md:
//
//	<!-- aiteam:team=go-backend -->
//
// An HTML comment because CLAUDE.md is Markdown and this must not show up when
// the file is rendered. The first match in the file wins.
var teamMarkerRE = regexp.MustCompile(`(?i)<!--\s*aiteam:team\s*=\s*([A-Za-z0-9][A-Za-z0-9._-]{0,63})\s*-->`)

// teamNameRE is the shape of a team name: `swift-apple`, `python-saas`,
// `go-backend`. It bounds the length so a stray file cannot put a megabyte in
// the database.
var teamNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// Aiteam is what [DetectAiteam] found.
type Aiteam struct {
	// Present reports that aiteam is installed. False means the repository is
	// not registered at all (AC-1.4).
	Present bool
	// Markers lists the markers that were found, in inspection order. It is
	// what the log line and the test assertions talk about.
	Markers []Marker
	// Team is the detected team, empty when the rule matched nothing. Empty is
	// a warning and a NULL column, never a failure (AC-1.5).
	Team string
	// TeamMarker names where Team came from, empty when Team is empty.
	TeamMarker Marker
}

// DetectAiteam reports whether aiteam is installed in the repository rooted at
// root, and which team it is configured with.
//
// # The rule
//
// There is exactly one rule and it has two halves, both explicit, both
// deterministic. Nothing else is consulted and nothing is inferred:
//
//  1. CLAUDE.md at the repository root. Present when the file contains the
//     case-insensitive literal "aiteam". The team is the capture of the first
//     `<!-- aiteam:team=NAME -->` marker in the file.
//
//  2. .aiteam at the repository root. Present when it is a directory; the
//     directory is read one level deep and no further. The team is the first
//     line of a regular file named `team` inside it, when that line is a legal
//     team name.
//
// Either half alone establishes presence. The CLAUDE.md marker wins over the
// team file when both name a team.
//
// # What this rule deliberately does not do
//
// It does not read prose. A heading that says "Go Backend Team", a roster
// table, a sentence describing the team — none of them are consulted, and the
// nearly-right answer they would produce is worse than no answer, because
// nothing at M0 consumes the team and a wrong value would be discovered much
// later by something that does. No fuzzy matching, no scoring, no "smart"
// inference. If the team is not stated in one of the two explicit markers, the
// repository is registered with no team and the caller warns.
//
// An error is returned only when a marker could not be inspected — a
// permission failure, say. "The marker is not there" is an answer, not an
// error, and so is a `team` file whose contents are not a legal team name.
func DetectAiteam(root string) (Aiteam, error) {
	var found Aiteam

	claudeMD, err := detectClaudeMD(filepath.Join(root, claudeMDName))
	if err != nil {
		return Aiteam{}, err
	}
	if claudeMD.present {
		found.Present = true
		found.Markers = append(found.Markers, MarkerClaudeMD)
		if claudeMD.team != "" {
			found.Team, found.TeamMarker = claudeMD.team, MarkerClaudeMD
		}
	}

	dir, err := detectAiteamDir(filepath.Join(root, aiteamDirName))
	if err != nil {
		return Aiteam{}, err
	}
	if dir.present {
		found.Present = true
		found.Markers = append(found.Markers, MarkerAiteamDir)
		if found.Team == "" && dir.team != "" {
			found.Team, found.TeamMarker = dir.team, MarkerAiteamDir
		}
	}

	return found, nil
}

// markerResult is what one half of the rule found.
type markerResult struct {
	present bool
	team    string
}

// detectClaudeMD applies half one of the rule to the file at path.
func detectClaudeMD(path string) (markerResult, error) {
	content, err := readLimited(path, claudeMDLimit)
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, errNotRegularFile):
		return markerResult{}, nil
	case err != nil:
		return markerResult{}, fmt.Errorf("inspect %s: %w", path, err)
	}

	if !strings.Contains(strings.ToLower(content), presenceMarker) {
		return markerResult{}, nil
	}
	result := markerResult{present: true}
	if m := teamMarkerRE.FindStringSubmatch(content); m != nil {
		result.team = m[1]
	}
	return result, nil
}

// detectAiteamDir applies half two of the rule to the directory at path. It is
// one os.ReadDir — a single level, never a walk.
func detectAiteamDir(path string) (markerResult, error) {
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return markerResult{}, nil
	case err != nil:
		return markerResult{}, fmt.Errorf("inspect %s: %w", path, err)
	case !info.IsDir():
		// A regular file named .aiteam is not an aiteam installation.
		return markerResult{}, nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return markerResult{}, fmt.Errorf("read %s: %w", path, err)
	}
	result := markerResult{present: true}
	for _, entry := range entries {
		if entry.Name() != teamFileName || !entry.Type().IsRegular() {
			continue
		}
		team, err := readTeamFile(filepath.Join(path, teamFileName))
		if err != nil {
			return markerResult{}, err
		}
		result.team = team
		break
	}
	return result, nil
}

// readTeamFile returns the team named by .aiteam/team, or "" when the file
// does not name a legal one. A malformed file is not an error: it is a marker
// that did not match, which the caller reports as "no team".
func readTeamFile(path string) (string, error) {
	content, err := readLimited(path, teamFileLimit)
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, errNotRegularFile):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("read %s: %w", path, err)
	}

	first, _, _ := strings.Cut(content, "\n")
	team := strings.TrimSpace(first)
	if !teamNameRE.MatchString(team) {
		return "", nil
	}
	return team, nil
}

// errNotRegularFile reports a marker path that exists but is a directory, a
// device or a socket. It is a sentinel rather than an error to report, because
// "CLAUDE.md is a directory" means the marker is absent, not that registration
// should fail.
var errNotRegularFile = errors.New("not a regular file")

// readLimited reads at most limit bytes of a regular file.
func readLimited(path string, limit int64) (string, error) {
	//nolint:gosec // G304: path is <clone>/CLAUDE.md or <clone>/.aiteam/team, built
	// from the repository root git itself resolved.
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errNotRegularFile
	}

	content, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return "", err
	}
	return string(content), nil
}
