package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/factory/localprojects"
)

// The access commands are local and owner-only. Grant text is stored exactly
// as written and is never interpreted here.

func newRepoAccessCommand(action string) *Command {
	cmd := &Command{Name: "repo access " + action, Short: "Manage plain-text access grants", Args: ArgSpec{Names: []string{"project-id"}, Min: 1}}
	switch action {
	case "grant":
		cmd.Usage = "repo access grant <project-id> <login> (--text TEXT | --file PATH)"
		cmd.Short = "Grant a person plain-text rights, or replace their text with a new revision"
		cmd.Args = ArgSpec{Names: []string{"project-id", "login"}, Min: 2}
	case "revoke":
		cmd.Usage = "repo access revoke <project-id> <grant-id>"
		cmd.Short = "Revoke an active grant by the id `repo access list` shows"
		cmd.Args = ArgSpec{Names: []string{"project-id", "grant-id"}, Min: 2}
	case "list":
		cmd.Usage = "repo access list <project-id>"
		cmd.Short = "List the active grants of a project"
	default:
		cmd.Usage = "repo access history <project-id>"
		cmd.Short = "Show every grant revision and revocation, oldest first"
	}
	cmd.New = func(fs *flag.FlagSet) Handler {
		var text, file string
		if action == "grant" {
			fs.StringVar(&text, "text", "", "grant text, stored exactly as written")
			fs.StringVar(&file, "file", "", "read the grant text from `PATH`")
		}
		return func(ctx context.Context, env *Env, args []string) error {
			return runRepoAccess(ctx, env, cmd, action, text, file, fileSet(fs, "text"), args)
		}
	}
	return cmd
}

func fileSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) { set = set || f.Name == name })
	return set
}

func runRepoAccess(ctx context.Context, env *Env, cmd *Command, action, text, file string, textSet bool, args []string) (err error) {
	if len(args) < 1 {
		return Usagef(cmd, "project-id is required")
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id <= 0 {
		return Usagef(cmd, "project-id must be a positive registered repository ID")
	}
	var grantID int64
	switch action {
	case "grant":
		if len(args) < 2 {
			return Usagef(cmd, "login is required")
		}
		if textSet == (file != "") {
			return Usagef(cmd, "give exactly one of --text or --file")
		}
		if file != "" {
			raw, e := os.ReadFile(file)
			if e != nil {
				return Usagef(cmd, "cannot read --file: %v", e)
			}
			text = string(raw)
		}
	case "revoke":
		if len(args) < 2 {
			return Usagef(cmd, "grant-id is required")
		}
		if grantID, err = strconv.ParseInt(args[1], 10, 64); err != nil || grantID <= 0 {
			return Usagef(cmd, "grant-id must be a positive number shown by `repo access list`")
		}
	}
	rt, err := openRepoRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, rt.close()) }()
	service, err := localprojects.Open(ctx, rt.db, id, localprojects.Options{Logger: env.Logger, Keychain: env.Keychain})
	if err != nil {
		return projectCommandError(cmd, err)
	}
	switch action {
	case "grant":
		g, e := service.GrantAccess(ctx, args[1], text)
		if e != nil {
			return projectCommandError(cmd, e)
		}
		fmt.Fprintf(env.Stdout, "granted %s\n", describeGrant(g))
	case "revoke":
		g, e := service.RevokeAccess(ctx, grantID)
		if e != nil {
			return projectCommandError(cmd, e)
		}
		fmt.Fprintf(env.Stdout, "revoked %s\n", describeGrant(g))
	case "list":
		rows, e := service.ListAccess(ctx)
		if e != nil {
			return projectCommandError(cmd, e)
		}
		if len(rows) == 0 {
			fmt.Fprintf(env.Stdout, "project %d has no active grants\n", id)
		}
		for _, g := range rows {
			writeGrant(env, g, false)
		}
	default:
		rows, e := service.AccessHistory(ctx)
		if e != nil {
			return projectCommandError(cmd, e)
		}
		if len(rows) == 0 {
			fmt.Fprintf(env.Stdout, "project %d has no grant history\n", id)
		}
		for _, g := range rows {
			writeGrant(env, g, true)
		}
	}
	return nil
}

func describeGrant(g contracts.ProjectAccessGrantView) string {
	return fmt.Sprintf("grant %d: %s %s id %s (@%s), revision %d", g.ID, g.Actor.Provider, g.Actor.Origin, g.Actor.RemoteID, g.Handle, g.Revision)
}

// writeGrant prints the header line, then the text indented and unchanged.
func writeGrant(env *Env, g contracts.ProjectAccessGrantView, withClose bool) {
	line := describeGrant(g) + "; created " + g.CreatedAt.UTC().Format(time.RFC3339)
	if withClose {
		if g.ClosedAt.IsZero() {
			line += "; active"
		} else {
			line += "; " + g.CloseReason + " " + g.ClosedAt.UTC().Format(time.RFC3339)
		}
	}
	fmt.Fprintln(env.Stdout, line)
	fmt.Fprintf(env.Stdout, "  text: %q\n", g.Text)
}
