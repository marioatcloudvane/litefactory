package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Host is the only forge host litefactory supports at M0 (AC-0.8). It is passed
// to gh explicitly rather than left to gh's default, so that neither an ambient
// GH_HOST nor a working directory can redirect owner discovery.
const Host = "github.com"

// The gh commands this package runs. There are two, they are the whole
// vocabulary, and both are read-only.
var (
	authStatusArgs = []string{"auth", "status"}
	apiUserArgs    = []string{"api", "--hostname", Host, "user"}
)

// Owner is the human whose comments litefactory acts on.
//
// UserID is the identity; Login is a label. GitHub lets an account be renamed,
// and a fine-grained PAT on a machine *user* account reports itself as a User
// rather than a Bot — so the self-filter, the comment classification and the
// bot != owner check all compare the numeric id and nothing else (D-I).
type Owner struct {
	// Login is the handle, for display: marioatcloudvane.
	Login string
	// UserID is the numeric GitHub account id, stable for the life of the
	// account. Never zero in a value returned by [ResolveOwner].
	UserID int64
}

// Options configures [ResolveOwner]. The zero value is the intended one.
type Options struct {
	// Timeout bounds one gh invocation. Zero means [DefaultTimeout].
	Timeout time.Duration
}

// ResolveOwner asks the locally logged-in gh who it is.
//
// It runs exactly two commands — `gh auth status`, to establish that gh is
// logged in and that it is logged in to github.com, and `gh api user`, for the
// login and the numeric id — with the credential variables stripped from their
// environment (see the package documentation).
//
// Every failure is terminal and none of them is partial: the caller gets a
// login *and* an id, or an error naming the command that failed and what it
// wrote to stderr, and writes nothing (AC-0.4).
func ResolveOwner(ctx context.Context, opts Options) (Owner, error) {
	gh, err := newRunner(opts.Timeout)
	if err != nil {
		return Owner{}, err
	}
	if err := gh.checkHost(ctx); err != nil {
		return Owner{}, err
	}
	return gh.owner(ctx)
}

// checkHost establishes that gh is logged in, and logged in to github.com.
//
// It is a separate command from the user lookup because the two failures need
// different messages: "you are not logged in" and "you are logged in to the
// wrong forge" are fixed in completely different ways, and `gh api user` would
// report both as an unhelpful HTTP error.
func (r runner) checkHost(ctx context.Context) error {
	out, err := r.run(ctx, authStatusArgs...)
	if err != nil {
		return classifyAuthFailure(err, out)
	}

	hosts := parseHosts(out.stdout + "\n" + out.stderr)
	switch {
	case len(hosts) == 0:
		return fmt.Errorf("%w: `%s` named no host", ErrNotAuthenticated, r.commandLineFor(authStatusArgs...))
	case !slices.Contains(hosts, Host):
		return fmt.Errorf("%w: gh is logged in to %s; run `gh auth login --hostname %s`",
			ErrUnsupportedHost, strings.Join(hosts, ", "), Host)
	}
	return nil
}

// owner reads the authenticated account from the GitHub API through gh.
func (r runner) owner(ctx context.Context) (Owner, error) {
	out, err := r.run(ctx, apiUserArgs...)
	if err != nil {
		return Owner{}, classifyAuthFailure(err, out)
	}

	var payload struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	}
	if err := json.Unmarshal([]byte(out.stdout), &payload); err != nil {
		return Owner{}, fmt.Errorf("parse the answer of `%s`: %w", r.commandLineFor(apiUserArgs...), err)
	}
	if payload.ID == 0 || payload.Login == "" {
		return Owner{}, fmt.Errorf("%w: `%s` answered %q",
			ErrNoIdentity, r.commandLineFor(apiUserArgs...), truncate(out.stdout))
	}
	return Owner{Login: payload.Login, UserID: payload.ID}, nil
}

// notLoggedInRE matches gh's own phrasing for an unauthenticated CLI, across
// the versions that have phrased it differently. Matching the message is the
// only signal available: gh exits 1 for this and for several other conditions.
var notLoggedInRE = regexp.MustCompile(`(?i)not logged in|no accounts|gh auth login`)

// classifyAuthFailure restates a plain non-zero exit as [ErrNotAuthenticated]
// when gh said that is what it was, keeping the command line, the exit code and
// stderr intact for AC-0.4.
//
// A timeout is never reclassified: "gh says you are logged out" and "gh never
// answered" are different problems and must stay distinguishable — the second
// one, on this machine, usually means a keychain dialog nobody can see.
func classifyAuthFailure(err error, out output) error {
	var cmdErr *CommandError
	if !errors.As(err, &cmdErr) || cmdErr.TimedOut || cmdErr.ExitCode <= 0 {
		return err
	}
	if !notLoggedInRE.MatchString(out.stdout + "\n" + out.stderr) {
		return err
	}
	classified := *cmdErr
	classified.Err = ErrNotAuthenticated
	return &classified
}

// hostRE matches a bare hostname, which is how `gh auth status` heads the
// block it prints for each host it holds credentials for.
var hostRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*\.[A-Za-z]{2,}$`)

// parseHosts extracts the hosts `gh auth status` reported.
//
// gh's output is a block per host: an unindented host line followed by
// indented detail lines. That shape has survived every gh version this has been
// run against, while the wording of the detail lines has not, so the parser
// keys on the shape and ignores the prose. Anything unindented that is not a
// hostname is skipped rather than guessed at.
//
// The detail lines can include a token, which gh masks (gho_****) unless it is
// asked for with --show-token. Nothing here passes that flag, so this text —
// which ends up in error messages — carries no credential.
func parseHosts(text string) []string {
	var hosts []string
	for line := range strings.SplitSeq(text, "\n") {
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		candidate := strings.TrimSuffix(strings.TrimRight(line, " \t\r"), ":")
		if !hostRE.MatchString(candidate) || slices.Contains(hosts, candidate) {
			continue
		}
		hosts = append(hosts, candidate)
	}
	return hosts
}

// maxQuotedAnswer bounds how much of an unexpected gh answer is quoted back in
// an error message. Enough to recognise it, not enough to fill a terminal.
const maxQuotedAnswer = 120

func truncate(s string) string {
	if len(s) <= maxQuotedAnswer {
		return s
	}
	return s[:maxQuotedAnswer] + "…"
}
