package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/drift"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

type driftConfigurator func(context.Context, *drift.Service, *repoRuntime, *Env, drift.Request) error

type driftRepeated []string

func (v *driftRepeated) String() string         { return strings.Join(*v, ",") }
func (v *driftRepeated) Set(value string) error { *v = append(*v, value); return nil }

func newStandardsDriftCommand(configure driftConfigurator) *Command {
	c := &Command{Name: "standards analyze drift", Usage: "standards analyze drift --repo owner/name --base REF --head REF [--path PREFIX] [--issue N] [--key KEY] [--json]", Short: "Analyze bounded pinned changes and selected discussion", Long: driftUsage}
	c.New = func(fs *flag.FlagSet) Handler {
		request := drift.Request{Limits: drift.DefaultLimits()}
		var paths, issues driftRepeated
		var asJSON bool
		fs.StringVar(&request.Repository, "repo", "", "registered owner/name")
		fs.StringVar(&request.Base, "base", "", "explicit local base ref")
		fs.StringVar(&request.Head, "head", "", "explicit local head ref")
		fs.StringVar(&request.Key, "key", "", "caller idempotency key")
		fs.Var(&paths, "path", "literal path prefix (repeatable)")
		fs.Var(&issues, "issue", "same-repository issue or PR number (repeatable)")
		fs.BoolVar(&asJSON, "json", false, "emit JSON")
		fs.IntVar(&request.Limits.Paths, "max-paths", request.Limits.Paths, "lower changed-path ceiling")
		fs.IntVar(&request.Limits.Issues, "max-issues", request.Limits.Issues, "lower selected-issue ceiling")
		fs.IntVar(&request.Limits.Comments, "max-comments", request.Limits.Comments, "lower comment ceiling")
		fs.IntVar(&request.Limits.Requests, "max-requests", request.Limits.Requests, "lower remote-request ceiling")
		fs.Int64Var(&request.Limits.SourceBytes, "max-source-bytes", request.Limits.SourceBytes, "lower source-byte ceiling")
		fs.IntVar(&request.Limits.ItemBytes, "max-item-bytes", request.Limits.ItemBytes, "lower per-item byte ceiling")
		fs.IntVar(&request.Limits.AcquisitionSeconds, "acquisition-seconds", request.Limits.AcquisitionSeconds, "lower acquisition time ceiling")
		fs.IntVar(&request.Limits.AgentSeconds, "agent-seconds", request.Limits.AgentSeconds, "lower analyst time ceiling")
		fs.IntVar(&request.Limits.Calls, "max-calls", request.Limits.Calls, "lower broker-call ceiling")
		fs.Int64Var(&request.Limits.OutboundBytes, "max-outbound-bytes", request.Limits.OutboundBytes, "lower encoded tool-byte ceiling")
		fs.IntVar(&request.Limits.ResultBytes, "max-result-bytes", request.Limits.ResultBytes, "lower final-result byte ceiling")
		fs.IntVar(&request.Limits.Proposals, "max-proposals", request.Limits.Proposals, "lower proposal ceiling")

		return func(ctx context.Context, env *Env, _ []string) (err error) {
			if request.Repository == "" || request.Base == "" || request.Head == "" {
				return Usagef(c, "--repo, --base and --head are required")
			}
			if _, _, err = parseRepoFlag(c, request.Repository); err != nil {
				return err
			}
			request.Paths = append([]string(nil), paths...)
			for _, raw := range issues {
				n, e := strconv.Atoi(raw)
				if e != nil || n < 1 {
					return Usagef(c, "--issue requires a positive issue number")
				}
				request.Issues = append(request.Issues, n)
			}
			rt, err := openRepoRuntime(ctx)
			if err != nil {
				return err
			}
			defer func() { err = errors.Join(err, rt.close()) }()
			service := &drift.Service{Store: rt.db, OnCreated: func(a store.DriftAnalysis) error {
				out := env.Stdout
				if asJSON {
					out = env.Stderr
				}
				_, e := fmt.Fprintf(out, "Analysis: %s\n", a.ID)
				return e
			}}
			if configure != nil {
				if err = configure(ctx, service, rt, env, request); err != nil {
					return err
				}
			}
			analysis, err := service.Start(ctx, request)
			if analysis.ID != "" {
				if asJSON {
					err = errors.Join(err, writeJSON(env, analysis))
				} else {
					err = errors.Join(err, writeDriftAnalysisText(env.Stdout, analysis))
				}
			}
			return err
		}
	}
	return c
}

func newStandardsAnalysisCommand(action string) *Command {
	name := "standards analysis " + action
	c := &Command{Name: name, Usage: name + " [--json]", Short: "Inspect retained drift analyses", Long: driftUsage}
	if action == "expire-evidence" {
		c.Short = "Expire retained excerpt bodies for a finished analysis"
	}
	if action != "list" {
		c.Args = ArgSpec{Names: []string{"analysis"}, Min: 1}
		c.Usage = name + " <analysis> [--json]"
		if action == "evidence" {
			c.Usage = name + " <analysis> [--evidence ID] [--offset N] [--limit N] [--json]"
		}
	}
	c.New = func(fs *flag.FlagSet) Handler {
		var asJSON bool
		var repo, evidenceID string
		var offset, limit int
		fs.BoolVar(&asJSON, "json", false, "emit JSON")
		fs.IntVar(&offset, "offset", 0, "retained page offset")
		fs.IntVar(&limit, "limit", 25, "page size (1..100)")
		if action == "list" {
			fs.StringVar(&repo, "repo", "", "filter registered owner/name")
		}
		if action == "evidence" {
			fs.StringVar(&evidenceID, "evidence", "", "read one retained evidence ID")
		}
		return func(ctx context.Context, env *Env, args []string) (err error) {
			if offset < 0 || limit < 1 || limit > 100 {
				return Usagef(c, "--offset must be nonnegative and --limit must be 1..100")
			}
			rt, err := openRepoRuntime(ctx)
			if err != nil {
				return err
			}
			defer func() { err = errors.Join(err, rt.close()) }()
			switch action {
			case "list":
				rows, e := rt.db.ListDriftAnalyses(ctx, repo, offset, limit)
				if e != nil {
					return e
				}
				if asJSON {
					return writeJSON(env, rows)
				}
				for _, row := range rows {
					if _, e = fmt.Fprintf(env.Stdout, "%s %s %s %s\n", row.ID, row.Repository, row.Execution, row.Disposition); e != nil {
						return e
					}
				}
				return nil
			case "show", "expire-evidence":
				if action == "expire-evidence" {
					if e := (&drift.Service{Store: rt.db}).ExpireEvidence(ctx, args[0]); e != nil {
						return e
					}
				}
				view, e := (&drift.Service{Store: rt.db}).Inspect(ctx, args[0])
				if e != nil {
					return e
				}
				if asJSON {
					return writeJSON(env, view)
				}
				if e = writeDriftAnalysisText(env.Stdout, view.Analysis); e != nil {
					return e
				}
				return writeRunStandardsText(env.Stdout, view.Standards)
			case "evidence":
				if _, e := rt.db.GetDriftAnalysis(ctx, args[0]); e != nil {
					return e
				}
				if evidenceID != "" {
					row, e := rt.db.GetDriftEvidence(ctx, args[0], evidenceID)
					if e != nil {
						return e
					}
					if asJSON {
						return writeJSON(env, row)
					}
					return writeDriftEvidenceText(env.Stdout, row)
				}
				rows, e := rt.db.ListDriftEvidence(ctx, args[0], offset, limit)
				if e != nil {
					return e
				}
				if asJSON {
					return writeJSON(env, rows)
				}
				for _, row := range rows {
					if e = writeDriftEvidenceText(env.Stdout, row); e != nil {
						return e
					}
				}
				return nil
			}
			return Usagef(c, "unknown analysis action")
		}
	}
	return c
}

func writeDriftAnalysisText(out io.Writer, a store.DriftAnalysis) error {
	_, err := fmt.Fprintf(out, "Analysis: %s\nRepository: %s\nExecution: %s\nFinding disposition: %s\nReason: %s\nRequested scope: %s\nPinned sources: %s\nStandards context: %s\nCoverage: %s\nLimits: %+v\nUsage: %+v\nLaunch provenance: %s\nOffered evidence ranges: %+v\nReport: %s\n", a.ID, a.Repository, a.Execution, a.Disposition, a.Reason, a.Scope, a.Pinned, a.Standards, a.Coverage, a.Limits, a.Usage, a.Provenance, a.Offers, a.Report)
	if err != nil {
		return err
	}
	for _, p := range a.ProposalLinks {
		if _, err = fmt.Fprintf(out, "Unapproved proposal: %s version %d digest %s candidate %s\n", p.ProposalID, p.Version, p.Digest, p.CandidateKey); err != nil {
			return err
		}
	}
	return nil
}
func writeDriftEvidenceText(out io.Writer, e store.DriftEvidence) error {
	_, err := fmt.Fprintf(out, "Evidence: %s (%s)\nLocator: %s\nDigest: %s\nRedaction: %s\nOmission: %s\nMetadata: %s\n%s\n", e.ID, e.Kind, e.Locator, e.Digest, e.RedactionPolicy, e.Omission, e.Metadata, e.Text)
	return err
}

const driftUsage = `Analyze the net changes between two explicit local commits and selected same-repository issue bodies/comments. No fetch, checkout, linked-source expansion or automatic restart occurs. Scope, retained standards, sanitized evidence, offered ranges, gaps, budgets and exact unapproved proposal versions remain inspectable. Discussion is an observation interval, not an atomic snapshot or exhaustive history. Unsupported provider isolation refuses launch. Reuse the same key and unchanged request to recover; after interruption inspect the retained identity before starting anew. Disabled MCP and unsupported configured providers refuse launch. Provider gaps and retry timing remain visible; no remote read is automatically retried. Proposals require separate human review, approval and publication.`

func configureDriftCLI(ctx context.Context, service *drift.Service, rt *repoRuntime, env *Env, request drift.Request) error {
	owner, name, err := parseRepoFlag(nil, request.Repository)
	if err != nil {
		return err
	}
	repo, err := rt.db.GetRepoBySlug(ctx, store.ForgeGitHub, owner, name)
	if err != nil {
		return Preconditionf("registered repository required: %v", err)
	}
	binary, err := git.Resolve(ctx)
	if err != nil {
		return err
	}
	redactor := redact.New(redact.Options{Logger: env.Logger, Environ: os.Environ()})
	service.Acquisition = &drift.Acquirer{Store: rt.db, Git: binary, ClonePath: repo.Path, Repo: github.Repo{Owner: owner, Name: name}, Redactor: redactor}
	cfg := rt.cfg
	if !cfg.Settings.MCPAutoStart {
		service.LaunchRefusal = "MCP automatic startup is disabled; drift analyst launch refused"
	}
	ref, configured := secrets.ReferenceFrom(cfg.Providers.GitHub.TokenKeychain.Service, cfg.Providers.GitHub.TokenKeychain.Account, cfg.Providers.GitHub.TokenEnv)
	var token secrets.Token
	if configured {
		token, _ = (secrets.Resolver{Keychain: env.Keychain}).Resolve(ctx, ref)
		redactor.AddToken(token)
	}
	factory := projectconnection.NewFactory(projectconnection.Options{DB: rt.db, Resolver: secrets.Resolver{Keychain: env.Keychain}, Logger: env.Logger, Redactor: redactor, LegacyReference: ref, LegacyToken: token})
	if version, e := rt.db.CurrentConnection(ctx, repo.ID, store.TicketConnection); e == nil {
		if clients, e := factory.Clients(ctx, version, 0); e == nil {
			service.Acquisition.Discussion = clients.Code
		}
	} else if errors.Is(e, store.ErrNotFound) && !token.Empty() {
		client, e := github.New(github.Options{Token: token, APIBase: cfg.Providers.GitHub.APIBase, Redactor: redactor, Logger: env.Logger})
		if e == nil {
			service.Acquisition.Discussion = client
		}
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	kind, configuredAgent := cfg.DefaultAgent()
	if cfg.Settings.MCPAutoStart && configuredAgent && kind == config.Kind("claude") && cfg.Agents.Claude != nil {
		model, _ := cfg.Tiers.ResolveModel(config.Kind("claude"), config.TierFlagship)
		runner, e := harness.New(harness.Options{Paths: rt.paths, Claude: *cfg.Agents.Claude, Model: model, Environ: os.Environ(), Logger: env.Logger})
		if e == nil {
			service.Runner = &drift.Provider{Runner: runner, Model: model, Directory: os.TempDir(), Executable: executable}
		}
	}
	return nil
}
