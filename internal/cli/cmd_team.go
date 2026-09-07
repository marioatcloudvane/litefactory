package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/team"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

type catalogAgentJSON struct {
	OverrideOrigin string   `json:"override_origin,omitempty"`
	ID             string   `json:"id"`
	Description    string   `json:"description"`
	Tier           string   `json:"tier"`
	Path           string   `json:"path"`
	Bytes          int      `json:"bytes"`
	Hash           string   `json:"hash"`
	Skills         []string `json:"skills"`
}
type catalogModeJSON struct {
	Mode   string             `json:"mode"`
	Agents []catalogAgentJSON `json:"agents"`
	Bytes  int                `json:"bytes"`
	Digest string             `json:"digest"`
}
type catalogTeamJSON struct {
	Origin      string            `json:"origin"`
	ID          string            `json:"id"`
	Description string            `json:"description"`
	Modes       []catalogModeJSON `json:"modes"`
}
type catalogJSON struct {
	Root  string            `json:"root"`
	Teams []catalogTeamJSON `json:"teams"`
}

func newTeamListCommand() *Command     { return newCatalogReadCommand("list", false) }
func newTeamShowCommand() *Command     { return newCatalogReadCommand("show", true) }
func newTeamValidateCommand() *Command { return newCatalogReadCommand("validate", true) }

func newCatalogReadCommand(verb string, named bool) *Command {
	cmd := &Command{Name: "team " + verb, Usage: "team " + verb + " [--json]", Short: "Inspect centrally installed team definitions"}
	if named {
		cmd.Args = ArgSpec{Names: []string{"team"}, Min: 1}
		cmd.Usage = "team " + verb + " <team> [--mode mode] [--json]"
	}
	cmd.New = func(flags *flag.FlagSet) Handler {
		asJSON := flags.Bool("json", false, "emit deterministic JSON")
		mode := flags.String("mode", "", "inspect only this workflow mode")
		return func(ctx context.Context, env *Env, args []string) error {
			paths, err := config.Locate()
			if err != nil {
				return err
			}
			resolved, err := teamsource.Resolve(ctx, paths)
			if err != nil {
				return Precondition(err)
			}
			names := make([]string, 0, len(resolved.Index.Teams))
			for name := range resolved.Index.Teams {
				names = append(names, name)
			}
			sort.Strings(names)
			if named {
				if _, ok := resolved.Index.Teams[args[0]]; !ok {
					return Usagef(cmd, "unknown installed team %q", args[0])
				}
				names = []string{args[0]}
			}
			modes := team.Modes()
			if *mode != "" {
				if _, ok := (team.TeamEntry{}).Mode(*mode); !ok {
					return Usagef(cmd, "unknown mode %q", *mode)
				}
				modes = []string{*mode}
			}
			out := catalogJSON{Root: paths.TeamsDir(), Teams: []catalogTeamJSON{}}
			metadata, err := teamsource.Metadata(ctx, paths.TeamsDir())
			if err != nil {
				return Precondition(err)
			}
			for _, name := range names {
				t := catalogTeamJSON{ID: name, Origin: metadata.Teams[name].Origin, Description: resolved.Index.Teams[name].Desc, Modes: []catalogModeJSON{}}
				for _, mode := range modes {
					s, e := teamsource.Capture(ctx, paths, name, mode)
					if e != nil {
						return Precondition(e)
					}
					m := catalogModeJSON{Mode: mode, Agents: []catalogAgentJSON{}, Digest: s.Digest}
					for _, b := range s.Files {
						m.Bytes += len(b)
					}
					for _, a := range s.Agents {
						d := a.Definition
						overrideOrigin := ""
						for _, o := range metadata.Overrides {
							if o.Path == d.Path {
								overrideOrigin = o.Origin
							}
						}
						m.Agents = append(m.Agents, catalogAgentJSON{ID: d.ID, OverrideOrigin: overrideOrigin, Description: d.Desc, Tier: d.Tier, Path: d.Path, Bytes: len(s.Files[d.Path]), Hash: s.Hashes[d.Path], Skills: d.Skills})
					}
					t.Modes = append(t.Modes, m)
				}
				out.Teams = append(out.Teams, t)
			}
			if *asJSON {
				return writeCatalogJSON(env, out)
			}
			fmt.Fprintf(env.Stdout, "catalog: %s\n", out.Root)
			for _, t := range out.Teams {
				fmt.Fprintf(env.Stdout, "%s: %s\n", t.ID, t.Description)
				for _, m := range t.Modes {
					fmt.Fprintf(env.Stdout, "  %s: %d agents, %d source bytes\n", m.Mode, len(m.Agents), m.Bytes)
					for _, a := range m.Agents {
						fmt.Fprintf(env.Stdout, "    %s [%s] %s (%d bytes)\n", a.ID, a.Tier, a.Path, a.Bytes)
					}
				}
			}
			return nil
		}
	}
	return cmd
}

func newTeamAddCommand() *Command {
	cmd := &Command{Name: "team add", Usage: "team add <team> [--source package-root] [--force]", Short: "Install one complete team into the central catalog", Args: ArgSpec{Names: []string{"team"}, Min: 1}}
	cmd.New = func(flags *flag.FlagSet) Handler {
		source := flags.String("source", "", "external version-2 package root; omitted uses embedded distribution")
		force := flags.Bool("force", false, "replace locally modified installed sources")
		return func(ctx context.Context, env *Env, args []string) error {
			paths, err := config.Locate()
			if err != nil {
				return err
			}
			if *source != "" {
				if err := validateExternalOrigin(ctx, paths, *source); err != nil {
					return Precondition(err)
				}
			}
			if err := teamsource.Install(ctx, paths.TeamsDir(), args[0], *source, *force); err != nil {
				return Precondition(err)
			}
			fmt.Fprintf(env.Stdout, "installed %s in %s\n", args[0], paths.TeamsDir())
			return nil
		}
	}
	return cmd
}

func newTeamStatusCommand() *Command {
	cmd := &Command{Name: "team status", Usage: "team status [--json]", Short: "Inspect central definitions for source drift"}
	cmd.New = func(flags *flag.FlagSet) Handler {
		asJSON := flags.Bool("json", false, "emit deterministic JSON")
		return func(ctx context.Context, env *Env, _ []string) error {
			paths, err := config.Locate()
			if err != nil {
				return err
			}
			records, err := teamsource.Status(ctx, paths.TeamsDir())
			if err != nil {
				return Precondition(err)
			}
			if *asJSON {
				if err := writeCatalogJSON(env, records); err != nil {
					return err
				}
			} else {
				for _, r := range records {
					fmt.Fprintf(env.Stdout, "%s %s %s %s\n", r.Team, r.Path, r.State, r.Error)
				}
			}
			for _, r := range records {
				if r.State != "in-sync" || r.Error != "" {
					return Preconditionf("central team catalog has drift; inspect team status before updating")
				}
			}
			return nil
		}
	}
	return cmd
}

func newTeamUpdateCommand() *Command {
	cmd := &Command{Name: "team update", Usage: "team update [--force] [--json]", Short: "Update centrally installed definitions from recorded origins"}
	cmd.New = func(flags *flag.FlagSet) Handler {
		force := flags.Bool("force", false, "replace locally modified or conflicting sources")
		asJSON := flags.Bool("json", false, "emit deterministic JSON")
		return func(ctx context.Context, env *Env, _ []string) error {
			paths, err := config.Locate()
			if err != nil {
				return err
			}
			before, err := teamsource.Status(ctx, paths.TeamsDir())
			if err != nil {
				return Precondition(err)
			}
			if err := teamsource.Update(ctx, paths.TeamsDir(), *force); err != nil {
				return Precondition(err)
			}
			if *asJSON {
				return writeCatalogJSON(env, struct {
					Force  bool                       `json:"force"`
					Before []teamsource.CatalogStatus `json:"before"`
				}{*force, before})
			}
			for _, r := range before {
				action := "updated"
				if *force && (r.State == "locally-modified" || r.State == "conflict") {
					action = "overwritten"
				}
				if r.State != "in-sync" {
					fmt.Fprintf(env.Stdout, "%s %s %s\n", action, r.Team, r.Path)
				}
			}
			return nil
		}
	}
	return cmd
}

func newTeamManageCommand() *Command {
	cmd := &Command{Name: "team manage", Usage: "team manage <scope> <action> [<source>] (shared:id | team:team:agent; set source=path | unset | show)", Short: "Manage an operator-global agent source override", Args: ArgSpec{Names: []string{"scope", "action", "source"}, Min: 2}}
	cmd.New = func(flags *flag.FlagSet) Handler {
		asJSON := flags.Bool("json", false, "emit deterministic JSON")
		return func(ctx context.Context, env *Env, args []string) error {
			paths, err := config.Locate()
			if err != nil {
				return err
			}
			source := ""
			if len(args) > 2 {
				var ok bool
				source, ok = strings.CutPrefix(args[2], "source=")
				if !ok || source == "" {
					return Usagef(cmd, "expected source=<path>")
				}
			}
			if args[1] == "set" {
				if source == "" {
					return Usagef(cmd, "set requires source=<path>")
				}
				if err := validateExternalOrigin(ctx, paths, source); err != nil {
					return Precondition(err)
				}
			} else if source != "" {
				return Usagef(cmd, "only set accepts source=<path>")
			}
			result, err := teamsource.Manage(ctx, paths.TeamsDir(), args[0], args[1], source)
			if err != nil {
				return Precondition(err)
			}
			if *asJSON {
				return writeCatalogJSON(env, result)
			}
			fmt.Fprintf(env.Stdout, "%s %s: %s (%s)\n", args[0], args[1], result.Path, result.Origin)
			return nil
		}
	}
	return cmd
}

func writeCatalogJSON(env *Env, value any) error {
	e := json.NewEncoder(env.Stdout)
	e.SetIndent("", "  ")
	return e.Encode(value)
}

func validateExternalOrigin(ctx context.Context, paths config.Paths, source string) (err error) {
	absolute, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return err
	}
	if _, err := os.Stat(paths.DBPath()); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	db, err := store.Open(ctx, paths.DBPath())
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	if err := db.Migrate(ctx); err != nil {
		return err
	}
	repos, err := db.ListRepos(ctx)
	if err != nil {
		return err
	}
	for _, repo := range repos {
		for _, root := range []string{repo.Path, repo.WorktreeRoot} {
			if root == "" {
				continue
			}
			canonical, e := filepath.EvalSymlinks(root)
			if errors.Is(e, fs.ErrNotExist) {
				canonical = filepath.Clean(root)
			} else if e != nil {
				return e
			}
			rel, e := filepath.Rel(canonical, absolute)
			if e != nil {
				return e
			}
			if rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return fmt.Errorf("source %s is inside registered repository/worktree storage %s; use an independent central package", absolute, canonical)
			}
		}
	}
	return nil
}
