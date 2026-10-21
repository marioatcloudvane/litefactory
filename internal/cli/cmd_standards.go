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

	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

type standardsFlags struct {
	file, digest, proposal, name, description string
	version, expected                         int64
	asJSON                                    bool
}

type standardsInput struct {
	BaseRevision int64                 `json:"base_revision"`
	Operations   []standards.Operation `json:"operations"`
}

func newStandardsCommand(group, action string) *Command {
	name := "standards"
	if group != "" {
		name += " " + group
	}
	if action != "" {
		name += " " + action
	}
	c := &Command{Name: name, Usage: name + " [--json]", Short: "Manage proposals and inspect the immutable standards book", Long: standardsUsage}
	switch group + " " + action {
	case "proposal create", "proposal edit", "proposal show", "proposal validate", "proposal review", "proposal approve", "proposal publish", "category add", "category replace", "category remove", "book show":
		c.Args = ArgSpec{Names: []string{"id"}, Min: 1}
	case "book diff":
		c.Args = ArgSpec{Names: []string{"from", "to"}, Min: 2}
	}
	c.Usage = name
	for _, argument := range c.Args.Names {
		c.Usage += " <" + argument + ">"
	}
	switch group + " " + action {
	case "proposal create":
		c.Usage += " --file FILE|-"
	case "proposal edit":
		c.Usage += " --expected-version N --file FILE|-"
	case "proposal show":
		c.Usage += " [--version N]"
	case "proposal validate", "proposal review":
		c.Usage += " --version N"
	case "proposal approve", "proposal publish":
		c.Usage += " --version N --digest HASH"
	case "category add", "category replace":
		c.Usage += " --proposal ID --expected-version N --name NAME [--description TEXT]"
	case "category remove":
		c.Usage += " --proposal ID --expected-version N"
	case "category list":
		c.Usage += " [--proposal ID --version N]"
	}
	c.Usage += " [--json]"
	c.New = func(fs *flag.FlagSet) Handler {
		var o standardsFlags
		fs.BoolVar(&o.asJSON, "json", false, "emit structured JSON (also the default)")
		key := group + " " + action
		switch key {
		case "proposal create", "proposal edit":
			fs.StringVar(&o.file, "file", "", "JSON input file, or - for stdin")
		}
		switch key {
		case "proposal show", "proposal validate", "proposal review", "proposal approve", "proposal publish", "category list":
			fs.Int64Var(&o.version, "version", 0, "exact retained proposal version")
		}
		if action == "approve" || action == "publish" {
			fs.StringVar(&o.digest, "digest", "", "exact change digest from review")
		}
		if key == "proposal edit" || (group == "category" && action != "list") {
			fs.Int64Var(&o.expected, "expected-version", 0, "expected current proposal version")
		}
		if group == "category" {
			fs.StringVar(&o.proposal, "proposal", "", "proposal targeted by category operation")
			if action == "add" || action == "replace" {
				fs.StringVar(&o.name, "name", "", "category name")
				fs.StringVar(&o.description, "description", "", "category description")
			}
		}
		return func(ctx context.Context, env *Env, args []string) (err error) {
			if group == "" {
				_, err = fmt.Fprintln(env.Stdout, standardsUsage)
				return err
			}
			rt, err := openRepoRuntime(ctx)
			if err != nil {
				return err
			}
			defer func() { err = errors.Join(err, rt.close()) }()
			svc := &standards.Service{Store: rt.db}
			owner, ownerErr := rt.db.GetIdentity(ctx, store.ForgeGitHub, store.RoleOwner)
			if ownerErr != nil && !errors.Is(ownerErr, store.ErrNotFound) {
				return ownerErr
			}
			if group == "proposal" && (action == "approve" || action == "publish") && (ownerErr != nil || owner.UserID <= 0) {
				return Preconditionf("configured numeric owner identity required")
			}
			svc.OwnerID = owner.UserID
			result, e := runStandards(ctx, env, svc, group, action, args, o)
			if e != nil {
				return standardsCommandError(e)
			}
			if err := writeJSON(env, result); err != nil {
				if group == "proposal" && action == "publish" {
					return fmt.Errorf("publication committed; inspect proposal %q version %d to recover its result: %w", args[0], o.version, err)
				}
				return err
			}
			return nil
		}
	}
	return c
}

func readStandardsInput(env *Env, path string) (standardsInput, error) {
	var in standardsInput
	if path == "" {
		return in, Usagef(nil, "--file is required; use - for stdin")
	}
	r := env.Stdin
	if path != "-" {
		f, err := os.Open(path) // #nosec G304 -- the local operator explicitly selects this input file.
		if err != nil {
			return in, err
		}
		defer func() { _ = f.Close() }()
		r = f
	}
	if r == nil {
		return in, Usagef(nil, "stdin is unavailable")
	}
	const maxInput = 8 << 20
	raw, err := io.ReadAll(io.LimitReader(r, maxInput+1))
	if err != nil {
		return in, err
	}
	if len(raw) > maxInput {
		return in, Usagef(nil, "proposal input exceeds 8 MiB")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return in, Usagef(nil, "proposal input: %v", err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return in, Usagef(nil, "proposal input must contain exactly one JSON object")
	}
	return in, nil
}

func runStandards(ctx context.Context, env *Env, svc *standards.Service, group, action string, args []string, o standardsFlags) (any, error) {
	switch group + " " + action {
	case "proposal create", "proposal edit":
		in, err := readStandardsInput(env, o.file)
		if err != nil {
			return nil, err
		}
		if action == "create" {
			return svc.Create(ctx, args[0], in.BaseRevision, in.Operations)
		}
		if o.expected < 1 {
			return nil, Usagef(nil, "--expected-version is required")
		}
		return svc.Edit(ctx, args[0], o.expected, in.BaseRevision, in.Operations)
	case "proposal list":
		return svc.List(ctx)
	case "proposal show":
		return svc.Inspect(ctx, args[0], o.version)
	case "proposal validate", "proposal review":
		if o.version < 1 {
			return nil, Usagef(nil, "--version is required")
		}
		return svc.Review(ctx, args[0], o.version)
	case "proposal approve", "proposal publish":
		if o.version < 1 || o.digest == "" {
			return nil, Usagef(nil, "--version and --digest from review are required")
		}
		if action == "approve" {
			return svc.Approve(ctx, args[0], o.version, o.digest)
		}
		revision, err := svc.Publish(ctx, args[0], o.version, o.digest)
		if err != nil {
			return nil, fmt.Errorf("publication not confirmed; inspect proposal %q version %d before retrying: %w", args[0], o.version, err)
		}
		return revision, nil
	case "book current":
		return svc.Current(ctx)
	case "book history":
		return svc.History(ctx)
	case "book show":
		id, err := parseID(args[0])
		if err != nil {
			return nil, err
		}
		return svc.Revision(ctx, id)
	case "book diff":
		from, err := standardsRevisionID(args[0])
		if err != nil {
			return nil, err
		}
		to, err := standardsRevisionID(args[1])
		if err != nil {
			return nil, err
		}
		return svc.Diff(ctx, from, to)
	case "category list":
		if o.proposal != "" {
			r, err := svc.Review(ctx, o.proposal, o.version)
			if err != nil {
				return nil, err
			}
			return r.After.Categories, nil
		}
		r, err := svc.Current(ctx)
		if err != nil {
			return nil, err
		}
		return r.Snapshot.Categories, nil
	case "category add", "category replace", "category remove":
		if o.proposal == "" || o.expected < 1 {
			return nil, Usagef(nil, "--proposal and --expected-version are required")
		}
		p, err := svc.Proposal(ctx, o.proposal, o.expected)
		if err != nil {
			return nil, err
		}
		op := standards.Operation{Kind: "category." + action, ID: args[0]}
		if action != "remove" {
			op.ID = ""
			op.Category = &standards.Category{ID: args[0], Name: o.name, Description: o.description}
		}
		ops, err := amendStandardsCategory(p.Operations, op, action, args[0])
		if err != nil {
			return nil, err
		}
		return svc.Edit(ctx, o.proposal, o.expected, p.BaseRevision, ops)
	}
	return nil, Usagef(nil, "unknown standards operation")
}

func amendStandardsCategory(original []standards.Operation, op standards.Operation, action, categoryID string) ([]standards.Operation, error) {
	ops := append([]standards.Operation{}, original...)
	found := false
	for i, existing := range ops {
		id := existing.ID
		if existing.Category != nil {
			id = existing.Category.ID
		}
		if id != categoryID || (existing.Kind != "category.add" && existing.Kind != "category.replace" && existing.Kind != "category.remove") {
			continue
		}
		found = true
		if action == "add" {
			return nil, fmt.Errorf("%w: category %s already has an operation; use replace or edit the complete change set", standards.ErrInvalid, categoryID)
		}
		if action == "remove" && existing.Kind == "category.add" {
			ops = append(ops[:i:i], ops[i+1:]...)
		} else {
			if action == "replace" && existing.Kind == "category.add" {
				op.Kind = "category.add"
			}
			ops[i] = op
		}
		break
	}
	if !found {
		ops = append(ops, op)
	}
	return ops, nil
}

func standardsRevisionID(raw string) (int64, error) {
	if raw == "0" {
		return 0, nil
	}
	return parseID(raw)
}

func standardsCommandError(err error) error {
	if errors.Is(err, standards.ErrOwnerRequired) {
		return Precondition(err)
	}
	return err
}

const standardsUsage = `Manage one standards book per initialized factory database.
proposal create ID --file FILE|-; proposal edit ID --expected-version N --file FILE|-
proposal list; proposal show ID [--version N]; proposal validate|review ID --version N
proposal approve|publish ID --version N --digest HASH
category add|replace ID --proposal ID --expected-version N --name NAME [--description TEXT]
category remove ID --proposal ID --expected-version N; category list [--proposal ID --version N]
book current; book history; book show REVISION; book diff FROM TO
exception propose --operation-key KEY --file FILE|-; exception list [--limit N --offset N]
exception show|review ID; exception approve ID --digest HASH; exception revoke ID [--reason TEXT]
run TASK [--run N] [--repo owner/name] [--json] (read-only retained standards inspection)
Book and proposal results are JSON; run and exception inspection default to text (use --json for JSON). Proposal input: {"base_revision":0,"operations":[...]}; edits replace the full change set.
Review shows complete before/after changes. Approve and publish require the exact reviewed version and digest.
Local machine access implies the configured owner. Standards text grants no factory permissions.
After a lost publish response, inspect or retry the same proposal version and digest.
A busy conflict requires retry; a stale base requires an explicit edited version, review and fresh approval.
SQLite WAL synchronous=NORMAL preserves consistency but recent commits can be lost on power failure.
History is protected against later edits, not disk loss or database administrator tampering.`
