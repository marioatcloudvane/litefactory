package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/prevention"
	"github.com/cloudvane/litefactory/internal/secrets"
)

const preventionUsage = `Analyze explicitly selected bugs using retained, bounded evidence.
start --repo owner/name --issues 123,124 --request-key KEY [--json]
list [--repo owner/name] [--limit 20] [--json]
show|evidence|result ANALYSIS_ID [--json]
An analysis creates advisory unapproved proposals only. Review, edit, approve and publish separately.
Keep the analysis ID on failure. Reuse the same request key and scope to inspect retained work without reanalysis.
Missing coverage does not establish no change. Remote observations are not an atomic repository snapshot.
Redaction removes known tokens and cannot detect every unknown secret.
SQLite WAL synchronous=NORMAL can lose recent commits after power failure.`

func newPreventionCommand(action string) *Command {
	c := &Command{Name: "standards analyze bugs " + action, Short: "Analyze selected bugs and inspect retained prevention evidence", Long: preventionUsage}
	c.Usage = c.Name
	if action != "start" && action != "list" {
		c.Args = ArgSpec{Names: []string{"analysis-id"}, Min: 1}
		c.Usage += " <analysis-id>"
	}
	if action == "start" {
		c.Usage += " --repo owner/name --issues 123,124 --request-key KEY"
	}
	c.Usage += " [--json]"
	c.New = func(fs *flag.FlagSet) Handler {
		var repo, issues, key string
		var asJSON bool
		var limit int
		limits := prevention.DefaultLimits()
		fs.BoolVar(&asJSON, "json", false, "emit structured JSON")
		if action == "start" || action == "list" {
			fs.StringVar(&repo, "repo", "", "registered GitHub owner/name")
		}
		if action == "list" {
			fs.IntVar(&limit, "limit", 20, "maximum retained analyses (1..100)")
		}
		if action == "start" {
			fs.IntVar(&limits.Issues, "max-issues", limits.Issues, "issue ceiling (lower only)")
			fs.IntVar(&limits.Fixes, "max-fixes", limits.Fixes, "direct fix reference ceiling (lower only)")
			fs.IntVar(&limits.Requests, "max-requests", limits.Requests, "remote request ceiling including failures (lower only)")
			fs.IntVar(&limits.PageSize, "page-size", limits.PageSize, "remote page size (lower only)")
			fs.Int64Var(&limits.ResponseBytes, "max-response-bytes", limits.ResponseBytes, "decoded response byte ceiling (lower only)")
			fs.Int64Var(&limits.SourceBytes, "max-source-bytes", limits.SourceBytes, "total source byte ceiling (lower only)")
			fs.IntVar(&limits.EvidenceBytes, "max-evidence-bytes", limits.EvidenceBytes, "retained evidence byte ceiling (lower only)")
			fs.IntVar(&limits.DurationSeconds, "timeout-seconds", limits.DurationSeconds, "whole invocation deadline in seconds (lower only)")
			fs.IntVar(&limits.Turns, "max-turns", limits.Turns, "analyst turn ceiling (lower only)")
			fs.IntVar(&limits.Corrections, "max-corrections", limits.Corrections, "result correction ceiling (lower only)")
			fs.IntVar(&limits.Proposals, "max-proposals", limits.Proposals, "proposal ceiling (lower only)")
			fs.StringVar(&issues, "issues", "", "explicit comma-separated issue numbers")
			fs.StringVar(&key, "request-key", "", "stable idempotency key for this scope")
		}
		return func(ctx context.Context, env *Env, args []string) (err error) {
			var scope prevention.Scope
			if action == "start" {
				if repo == "" || issues == "" || strings.TrimSpace(key) == "" {
					return Usagef(c, "--repo, --issues and --request-key are required")
				}
				if _, _, err := parseRepoFlag(c, repo); err != nil {
					return err
				}
				selected, e := preventionIssueNumbers(issues)
				if e != nil {
					return Usagef(c, "%v", e)
				}
				scope = prevention.Scope{Repository: repo, Issues: selected, RequestKey: key, Limits: limits}
				if e := scope.Validate(); e != nil {
					return Usagef(c, "%v", e)
				}
			}
			if action == "list" && repo != "" {
				if _, _, e := parseRepoFlag(c, repo); e != nil {
					return e
				}
			}
			if action == "list" && (limit < 1 || limit > 100) {
				return Usagef(c, "--limit must be 1..100")
			}
			rt, err := openRepoRuntime(ctx)
			if err != nil {
				return err
			}
			defer func() { err = errors.Join(err, rt.close()) }()
			svc := &prevention.Service{DB: rt.db}
			if action == "start" {
				if env.PreventionService != nil {
					svc, err = env.PreventionService(ctx, rt.db, rt.cfg, rt.paths)
				} else {
					svc, err = prevention.NewRuntime(ctx, rt.db, rt.cfg, rt.paths, env.Logger, secrets.Resolver{Keychain: env.Keychain})
				}
				if err != nil {
					return err
				}
			}
			if action == "list" {
				result, e := svc.List(ctx, repo, limit)
				if e != nil {
					return e
				}
				if asJSON {
					return writeJSON(env, result)
				}
				for _, a := range result {
					if _, e = fmt.Fprintf(env.Stdout, "%s  %s  %s\n", a.ID, a.Repository, a.Status); e != nil {
						return e
					}
				}
				return nil
			}
			var view prevention.Inspection
			if action == "start" {
				view, err = svc.Start(ctx, scope)
			} else {
				view, err = svc.Inspect(ctx, args[0])
			}
			if err != nil && view.Analysis.ID == "" {
				return err
			}
			var value any = view
			if action == "evidence" {
				value = view.Manifest
			}
			if action == "result" {
				value = struct {
					Result      any    `json:"result"`
					Draft       any    `json:"draft,omitempty"`
					Proposals   any    `json:"proposals"`
					Status      string `json:"status"`
					Diagnostics any    `json:"diagnostics,omitempty"`
					Coverage    any    `json:"coverage"`
					Limits      any    `json:"limits"`
					Standards   any    `json:"standards"`
				}{view.Result, view.Analysis.Draft, view.Proposals, view.Analysis.Status, view.Analysis.Diagnostics, view.Manifest.Coverage, view.Analysis.Limits, view.Standards}
			}
			if asJSON {
				return errors.Join(err, writeJSON(env, value))
			}
			if _, e := fmt.Fprintf(env.Stdout, "Analysis %s\nRepository: %s\nExecution: %s\nCreated: %s\nUpdated: %s\n", view.Analysis.ID, view.Analysis.Repository, view.Analysis.Status, logsTime(view.Analysis.CreatedAt), logsTime(view.Analysis.UpdatedAt)); e != nil {
				return errors.Join(err, e)
			}
			raw, e := json.MarshalIndent(value, "", "  ")
			if e != nil {
				return errors.Join(err, e)
			}
			_, e = fmt.Fprintf(env.Stdout, "%s\nReview unapproved proposals separately with standards proposal show/review/edit/approve/publish.\nRecover this analysis by its ID; the same request key does not rerun it.\n", raw)
			return errors.Join(err, e)
		}
	}
	return c
}

func preventionIssueNumbers(raw string) ([]int, error) {
	result := []int{}
	for _, piece := range strings.Split(raw, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(piece))
		if err != nil || n < 1 {
			return nil, fmt.Errorf("--issues requires positive issue numbers")
		}
		result = append(result, n)
	}
	return result, nil
}
