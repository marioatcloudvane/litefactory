package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/cloudvane/litefactory/internal/standardsexceptions"
	"github.com/cloudvane/litefactory/internal/store"
)

func newStandardsExceptionCommand(action string) *Command {
	c := &Command{Name: "standards exception " + action, Usage: "standards exception " + action, Short: "Record and inspect temporary exact-task standards exceptions", Long: "Exceptions grant no factory authority. Review immutable terms, then approve the exact ID and digest separately. Missing policy denies exceptions. Retry uncertain mutations with the same operation key or ID. See docs/cli/standards-exceptions.md for recovery and SQLite durability limits."}
	if action != "propose" && action != "list" {
		c.Args = ArgSpec{Names: []string{"id"}, Min: 1}
		c.Usage += " <id>"
	}
	switch action {
	case "propose":
		c.Usage += " --operation-key KEY --file FILE|-"
	case "approve":
		c.Usage += " --digest HASH"
	case "revoke":
		c.Usage += " [--reason TEXT]"
	case "list":
		c.Usage += " [--limit 50 --offset 0]"
	}
	c.Usage += " [--json]"
	c.New = func(fs *flag.FlagSet) Handler {
		var asJSON bool
		var file, key, digest, reason string
		var limit, offset int
		fs.BoolVar(&asJSON, "json", false, "emit structured JSON")
		if action == "propose" {
			fs.StringVar(&file, "file", "", "JSON terms file or - for stdin")
			fs.StringVar(&key, "operation-key", "", "stable caller retry key")
		}
		if action == "approve" {
			fs.StringVar(&digest, "digest", "", "exact proposal digest from review")
		}
		if action == "revoke" {
			fs.StringVar(&reason, "reason", "", "retained revocation reason")
		}
		if action == "list" {
			fs.IntVar(&limit, "limit", 50, "page size, 1..100")
			fs.IntVar(&offset, "offset", 0, "number of records to skip")
		}
		return func(ctx context.Context, env *Env, args []string) (err error) {
			rt, err := openRepoRuntime(ctx)
			if err != nil {
				return err
			}
			defer func() { err = errors.Join(err, rt.close()) }()
			svc := &standardsexceptions.Service{Store: rt.db}
			mutation := action == "propose" || action == "approve" || action == "revoke"
			if mutation {
				owner, e := rt.db.GetIdentity(ctx, store.ForgeGitHub, store.RoleOwner)
				if e != nil || owner.UserID <= 0 {
					if e != nil && !errors.Is(e, store.ErrNotFound) {
						return e
					}
					return Preconditionf("configured numeric owner identity required")
				}
				svc.OwnerID = owner.UserID
			}
			var result any
			switch action {
			case "propose":
				if key == "" {
					return Usagef(nil, "--operation-key is required")
				}
				var request standardsexceptions.Request
				if err = readExceptionRequest(env, file, &request); err != nil {
					return err
				}
				result, err = svc.Propose(ctx, key, request)
			case "list":
				result, err = svc.List(ctx, limit, offset)
			case "show", "review":
				result, err = svc.Show(ctx, args[0])
			case "approve":
				if digest == "" {
					return Usagef(nil, "--digest is required; review this exact exception first")
				}
				result, err = svc.Approve(ctx, args[0], digest)
			case "revoke":
				result, err = svc.Revoke(ctx, args[0], reason)
			}
			if err != nil {
				return err
			}
			if asJSON {
				err = writeJSON(env, result)
			} else {
				err = writeExceptionText(env.Stdout, result)
			}
			if err != nil && mutation {
				return fmt.Errorf("mutation committed; inspect the exception ID or retry the identical operation key/ID to recover: %w", err)
			}
			return err
		}
	}
	return c
}
func readExceptionRequest(env *Env, path string, dst *standardsexceptions.Request) error {
	if path == "" {
		return Usagef(nil, "--file is required; use - for stdin")
	}
	r := env.Stdin
	if path != "-" {
		f, err := os.Open(path) // #nosec G304 -- local operator selects the input file.
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		r = f
	}
	if r == nil {
		return Usagef(nil, "stdin unavailable")
	}
	const maximum = 1 << 20
	raw, err := io.ReadAll(io.LimitReader(r, maximum+1))
	if err != nil {
		return err
	}
	if len(raw) > maximum {
		return Usagef(nil, "exception terms exceed 1 MiB")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err = dec.Decode(dst); err != nil {
		return Usagef(nil, "exception terms: %v", err)
	}
	var extra any
	if err = dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return Usagef(nil, "exception terms require exactly one JSON object")
	}
	return nil
}
func writeExceptionText(w io.Writer, result any) error {
	switch v := result.(type) {
	case standardsexceptions.View:
		return renderException(w, v)
	case []standardsexceptions.View:
		for _, item := range v {
			if err := renderException(w, item); err != nil {
				return err
			}
		}
		return nil
	default:
		return errors.New("unexpected exception result")
	}
}
func renderException(w io.Writer, v standardsexceptions.View) error {
	// The complete structured agreement remains visible in text mode as indented terms and events.
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "Exception %s — %s; effective=%t\n%s\n", v.Proposal.ID, v.Status, v.Effective, raw)
	return err
}
