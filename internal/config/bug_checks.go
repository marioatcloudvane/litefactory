package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// BugRecipePath locates the trusted recipe within the recorded target commit.
const BugRecipePath = ".litefactory/bug-checks.yaml"

// BugRecipeLimit bounds raw recipe input to one MiB before YAML decoding.
const BugRecipeLimit = 1 << 20

// BugCheckCommand declares one bounded argv invocation and environment-name references.
type BugCheckCommand struct {
	// Observe is a factory-only result-capture protocol. Recipe decoding cannot
	// select observers or supply artifact paths.
	Observe string `yaml:"-" json:"-"`

	Argv           []string `yaml:"argv" json:"argv"`
	CWD            string   `yaml:"cwd,omitempty" json:"cwd"`
	Env            []string `yaml:"env,omitempty" json:"env"`
	TimeoutSeconds int      `yaml:"timeout_seconds,omitempty" json:"timeout_seconds"`
	// Rerun optionally lets the factory attribute a failure to named tests by
	// rerunning only those tests; absent, a failure counts as it always did.
	Rerun *BugCheckRerun `yaml:"rerun,omitempty" json:"rerun,omitempty"`
}

// BugRerunFailuresGoTest names the only supported failure parser: go test output.
const BugRerunFailuresGoTest = "go-test"

// BugRerunTests and BugRerunPackages are the whole-element rerun argv
// placeholders for the failing tests and their packages.
const (
	BugRerunTests    = "{tests}"
	BugRerunPackages = "{packages}"
)

// BugCheckRerun declares how a failed command's named failing tests are rerun.
// TimeoutSeconds defaults to the declaring command's timeout.
type BugCheckRerun struct {
	Failures       string   `yaml:"failures" json:"failures"`
	Argv           []string `yaml:"argv" json:"argv"`
	TimeoutSeconds int      `yaml:"timeout_seconds,omitempty" json:"timeout_seconds"`
	inheritTimeout bool
}

// BugCheckPhase orders the commands required to establish one phase outcome.
type BugCheckPhase struct {
	Commands []BugCheckCommand `yaml:"commands" json:"commands"`
}

// BugCheckPhases defines the three mandatory sequential evidence phases.
type BugCheckPhases struct {
	Regression BugCheckPhase `yaml:"regression" json:"regression"`
	Focused    BugCheckPhase `yaml:"focused" json:"focused"`
	Full       BugCheckPhase `yaml:"full" json:"full"`
}

// BugCheckRecipe is the versioned target-owned command authority for Bug checks.
type BugCheckRecipe struct {
	Version int            `yaml:"version" json:"version"`
	Phases  BugCheckPhases `yaml:"phases" json:"phases"`
}

// Phase selects a mandatory phase by its closed factory-owned name.
func (r BugCheckRecipe) Phase(name string) (BugCheckPhase, error) {
	switch name {
	case "regression":
		return r.Phases.Regression, nil
	case "focused":
		return r.Phases.Focused, nil
	case "full":
		return r.Phases.Full, nil
	}
	return BugCheckPhase{}, errors.New("unknown Bug check phase")
}

// BugRecipeDigest identifies the complete original recipe bytes, including formatting.
func BugRecipeDigest(data []byte) string { d := sha256.Sum256(data); return hex.EncodeToString(d[:]) }

var bugEnvironmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// bugArgvFaults applies the command argv bounds to any declared argv.
func bugArgvFaults(what string, argv []string) []error {
	var faults []error
	if len(argv) == 0 || len(argv) > 128 || strings.TrimSpace(argv[0]) == "" {
		faults = append(faults, fmt.Errorf("%s invalid argv", what))
	}
	size := 0
	for _, v := range argv {
		size += len(v)
		if strings.ContainsRune(v, 0) {
			faults = append(faults, errors.New("bug argv contains NUL"))
		}
	}
	if size > 64<<10 {
		faults = append(faults, errors.New("bug argv exceeds 64 KiB"))
	}
	return faults
}

// bugRerunFaults validates a rerun block: a known parser, the argv bounds and
// each placeholder exactly once as a whole element, never inside another one.
func bugRerunFaults(what string, r *BugCheckRerun) []error {
	if r == nil {
		return nil
	}
	var faults []error
	if r.Failures != BugRerunFailuresGoTest {
		faults = append(faults, fmt.Errorf("%s rerun failures parser unsupported", what))
	}
	faults = append(faults, bugArgvFaults(what+" rerun", r.Argv)...)
	tests, packages := 0, 0
	for i, v := range r.Argv {
		switch {
		case v == BugRerunTests && i > 0:
			tests++
		case v == BugRerunPackages && i > 0:
			packages++
		case strings.Contains(v, BugRerunTests) || strings.Contains(v, BugRerunPackages):
			faults = append(faults, fmt.Errorf("%s rerun placeholder must be a whole argument", what))
		}
	}
	if tests != 1 || packages != 1 {
		faults = append(faults, fmt.Errorf("%s rerun argv needs one %s and one %s", what, BugRerunTests, BugRerunPackages))
	}
	if r.TimeoutSeconds < 1 || r.TimeoutSeconds > 86400 {
		faults = append(faults, errors.New("bug rerun timeout outside 1..86400 seconds"))
	}
	return faults
}

// ParseBugCheckRecipe validates the whole pinned recipe before any phase runs.
func ParseBugCheckRecipe(data []byte) (BugCheckRecipe, error) {
	var r BugCheckRecipe
	if len(data) == 0 || len(data) > BugRecipeLimit {
		return r, errors.New("bug recipe size outside 1..1048576 bytes")
	}
	var err error
	r, err = decodeBugCheckRecipe(data)
	if err != nil {
		return r, err
	}
	var faults []error
	if r.Version != 1 {
		faults = append(faults, errors.New("unsupported Bug recipe version"))
	}
	for _, name := range []string{"regression", "focused", "full"} {
		p, _ := r.Phase(name)
		if len(p.Commands) == 0 || len(p.Commands) > 64 {
			faults = append(faults, fmt.Errorf("%s requires 1..64 commands", name))
		}
		for i, c := range p.Commands {
			faults = append(faults, bugArgvFaults(fmt.Sprintf("%s command %d", name, i), c.Argv)...)
			faults = append(faults, bugRerunFaults(fmt.Sprintf("%s command %d", name, i), c.Rerun)...)
			if c.TimeoutSeconds < 1 || c.TimeoutSeconds > 86400 {
				faults = append(faults, errors.New("bug timeout outside 1..86400 seconds"))
			}
			if c.CWD == "" || filepath.IsAbs(c.CWD) || filepath.Clean(c.CWD) == ".." || strings.HasPrefix(filepath.Clean(c.CWD), ".."+string(filepath.Separator)) || strings.ContainsRune(c.CWD, 0) {
				faults = append(faults, errors.New("bug cwd must stay inside worktree"))
			}
			if len(c.Env) > 64 {
				faults = append(faults, errors.New("bug command exceeds 64 environment names"))
			}
			seen := map[string]bool{}
			for _, v := range c.Env {
				if !bugEnvironmentName.MatchString(v) || seen[v] {
					faults = append(faults, errors.New("invalid or duplicate Bug environment name"))
				}
				seen[v] = true
			}
		}
	}
	return r, errors.Join(faults...)
}
