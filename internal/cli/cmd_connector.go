package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/connector"
	"github.com/cloudvane/litefactory/internal/store"
)

// Connector administration uses local filesystem authority and deliberately does
// not load remote configuration or resolve any network credentials.
func openConnectorRuntime(ctx context.Context) (config.Paths, *store.DB, error) {
	paths, err := config.Locate()
	if err != nil {
		return config.Paths{}, nil, errors.New("cannot resolve the selected factory home")
	}
	info, err := os.Stat(paths.DBPath())
	if errors.Is(err, fs.ErrNotExist) {
		return paths, nil, Preconditionf("no litefactory database at %q: run `litefactory init` first", paths.DBPath())
	}
	if err != nil {
		return paths, nil, errors.New("cannot read the selected factory database")
	}
	if !info.Mode().IsRegular() {
		return paths, nil, Preconditionf("the selected factory database is not a regular file")
	}
	db, err := store.Open(ctx, paths.DBPath())
	if err != nil {
		return paths, nil, errors.New("cannot open the selected factory database")
	}
	if err = db.Migrate(ctx); err != nil {
		_ = db.Close()
		return paths, nil, errors.New("cannot migrate the selected factory database")
	}
	return paths, db, nil
}

func newConnectorGrantsCommand(action string) *Command {
	cmd := &Command{Name: "connector grants " + action, Usage: "connector grants " + action + " [--json]"}
	switch action {
	case "list":
		cmd.Short = "List retained connector grants without credentials"
	case "inspect":
		cmd.Short = "Inspect one connector grant without credentials"
	case "revoke":
		cmd.Short = "Revoke one connector grant locally"
	case "revoke-all":
		cmd.Short = "Revoke every existing connector grant locally"
	}
	if action == "inspect" || action == "revoke" {
		cmd.Args = ArgSpec{Names: []string{"grant-id"}, Min: 1}
		cmd.Usage = "connector grants " + action + " <grant-id> [--json]"
	}
	cmd.Long = "Operates on the existing selected LF_HOME without a daemon or remote configuration. Grant IDs must match exactly. Revocation is idempotent and takes effect on the next protected request after commit."
	cmd.New = func(flags *flag.FlagSet) Handler {
		var jsonOutput bool
		flags.BoolVar(&jsonOutput, "json", false, "emit JSON")
		return func(ctx context.Context, env *Env, args []string) (err error) {
			if (action == "inspect" || action == "revoke") && (strings.TrimSpace(args[0]) != args[0] || args[0] == "" || strings.ContainsFunc(args[0], unicode.IsControl)) {
				return Usagef(cmd, "grant-id must be an exact nonempty identifier without surrounding whitespace or control characters")
			}
			paths, db, err := openConnectorRuntime(ctx)
			if err != nil {
				return err
			}
			defer func() {
				if closeErr := db.Close(); closeErr != nil {
					err = errors.Join(err, errors.New("cannot close the selected factory database; inspect or retry the operation to confirm its result"))
				}
			}()
			now := time.Now().UTC()
			if env.Now != nil {
				now = env.Now().UTC()
			}
			return runConnectorGrantCommand(ctx, env, connector.NewGrantService(db), paths.Home(), action, args, jsonOutput, now)
		}
	}
	return cmd
}

func runConnectorGrantCommand(ctx context.Context, env *Env, svc *connector.GrantService, home, action string, args []string, jsonOutput bool, now time.Time) error {
	switch action {
	case "list", "inspect":
		grants := []connector.GrantSummary{}
		if action == "list" {
			rows, err := svc.ListGrantSummaries(ctx, now)
			if err != nil {
				return connectorGrantError(err, false)
			}
			grants = nonNil(rows)
		} else {
			grant, err := svc.InspectGrant(ctx, args[0], now)
			if err != nil {
				return connectorGrantError(err, false)
			}
			grants = append(grants, grant)
		}
		result := struct {
			FactoryHome string                   `json:"factory_home"`
			SnapshotAt  time.Time                `json:"snapshot_at"`
			Grants      []connector.GrantSummary `json:"grants"`
		}{home, now, grants}
		var outputErr error
		if jsonOutput {
			outputErr = writeJSON(env, result)
		} else {
			outputErr = writeConnectorGrantSummaries(env, home, now, grants)
		}
		if outputErr != nil {
			return errors.New("cannot write connector grant inspection result")
		}
		return nil
	case "revoke", "revoke-all":
		var mutation connector.GrantMutation
		var err error
		if action == "revoke" {
			mutation, err = svc.RevokeGrant(ctx, args[0], now)
		} else {
			mutation, err = svc.RevokeAllGrants(ctx, now)
		}
		if err != nil {
			return connectorGrantError(err, true)
		}
		result := struct {
			FactoryHome string `json:"factory_home"`
			connector.GrantMutation
		}{home, mutation}
		if jsonOutput {
			err = writeJSON(env, result)
		} else {
			if action == "revoke" {
				_, err = fmt.Fprintf(env.Stdout, "Factory: %q\nGrant %q: %s; newly revoked: %d\nCommitted at: %s\n", home, mutation.GrantID, mutation.Outcome, mutation.ChangedCount, connectorGrantTime(mutation.CommittedAt))
			} else {
				_, err = fmt.Fprintf(env.Stdout, "Factory: %q\nConnector grants newly revoked: %d\nCommitted at: %s\n", home, mutation.ChangedCount, connectorGrantTime(mutation.CommittedAt))
			}
		}
		if err != nil {
			return errors.New("connector revocation committed, but writing the result failed; inspect or retry the idempotent operation")
		}
		return nil
	}
	return Usagef(nil, "unknown connector grant operation")
}

func connectorGrantError(err error, mutation bool) error {
	if errors.Is(err, connector.ErrGrantNotFound) {
		return Preconditionf("connector grant not found")
	}
	if mutation {
		return errors.New("connector revocation was not confirmed; inspect or retry the idempotent operation")
	}
	return errors.New("cannot inspect connector grants")
}

func connectorGrantTime(at time.Time) string { return at.UTC().Format(time.RFC3339Nano) }

func writeConnectorGrantSummaries(env *Env, home string, snapshot time.Time, grants []connector.GrantSummary) error {
	var out strings.Builder
	fmt.Fprintf(&out, "Factory: %q\nSnapshot at: %s\n", home, connectorGrantTime(snapshot))
	if len(grants) == 0 {
		out.WriteString("No connector grants.\n")
	}
	for _, grant := range grants {
		fmt.Fprintf(&out, "\nGrant: %q\nClient: %q (%q)\nScope: %q\nResource: %q\n", grant.ID, grant.ClientID, grant.ClientName, grant.Scope, grant.Resource)
		if grant.Coverage.TaskRef != "" {
			fmt.Fprintf(&out, "Discussion task: %q\n", grant.Coverage.TaskRef)
		}
		if grant.Coverage.FactoryUsage {
			out.WriteString("Usage coverage: all recorded factory work, including work without a repository\n")
		}
		if grant.Coverage.UsageRepositoryRef != "" {
			fmt.Fprintf(&out, "Usage repository: %q\n", grant.Coverage.UsageRepositoryRef)
		}
		if grant.Coverage.UsageTaskRef != "" {
			fmt.Fprintf(&out, "Usage task: %q; linked report family=%t (includes retired history and later persisted links)\n", grant.Coverage.UsageTaskRef, grant.Coverage.UsageTaskFamily)
		}
		fmt.Fprintf(&out, "Repository coverage: all registered repositories=%t; private repositories=%t; future registrations=%t\n", grant.Coverage.AllRegisteredRepositories, grant.Coverage.PrivateRepositories, grant.Coverage.FutureRepositories)
		fmt.Fprintf(&out, "Created at: %s\nGrant expires at: %s\nRenewable: %t\nEffective status: %s (does not indicate daemon availability)\n", connectorGrantTime(grant.CreatedAt), connectorGrantTime(grant.ExpiresAt), grant.Renewable, grant.Status)
		if grant.RevokedAt != nil {
			fmt.Fprintf(&out, "Revoked at: %s\nRevocation reason: %q\n", connectorGrantTime(*grant.RevokedAt), grant.RevocationReason)
		}
		if grant.AccessTokens.Count == 0 {
			out.WriteString("Currently unexpired issued access tokens: none\n")
		} else {
			fmt.Fprintf(&out, "Currently unexpired issued access tokens: %d; usable: %d; unusable: %d\n", grant.AccessTokens.Count, grant.AccessTokens.UsableCount, grant.AccessTokens.Count-grant.AccessTokens.UsableCount)
			if grant.AccessTokens.EarliestExpiry != nil {
				fmt.Fprintf(&out, "Earliest access-token expiry: %s\n", connectorGrantTime(*grant.AccessTokens.EarliestExpiry))
			}
			if grant.AccessTokens.LatestExpiry != nil {
				fmt.Fprintf(&out, "Latest access-token expiry: %s\n", connectorGrantTime(*grant.AccessTokens.LatestExpiry))
			}
		}
	}
	if len(grants) > 0 {
		out.WriteString("Access-token expiry does not end a renewable grant.\n")
	}
	_, err := io.WriteString(env.Stdout, out.String())
	return err
}
