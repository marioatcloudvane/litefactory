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
	"strings"

	"github.com/cloudvane/litefactory/internal/instructionpackage"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

const standardsAuthorUsage = `Author one standard without an interactive terminal.
start ID --intent TEXT [--target STANDARD] [--categories ID,ID]
list; show ID [--version N]; questions ID [--version N]
answer ID --expected-version N --question ID --answer TEXT [--replace]
answer ID --expected-version N --file FILE|- (JSON array of answers)
edit ID --expected-version N --file FILE|- (JSON category_id, rule, description, evidence, scope, check, applies_to, context_category_ids fields)
continue|validate|complete ID --expected-version N
rebase ID --expected-version N --revision REVISION
All commands accept --json. Start also accepts --file FILE|- for intent text.
Inspect the saved version before completion. Completion creates an unapproved proposal;
review, approval and publication remain separate commands.
Continuation uses at most two isolated calls, a 120-second deadline, 256 KiB prompts
and 32 KiB responses. Retained book capture is limited to 16 MiB; each serialized
saved session version is limited to 64 MiB, including its base, original target and draft.
Use --categories at start or edit context_category_ids to narrow oversized context.
After interruption, show the session and explicitly retry continuation when its lease expires.
Retries may charge again and return different text. Stale bases need deliberate rebase.
SQLite NORMAL protects ordinary process interruption; power loss can lose recent writes.
Disk loss and database administrator changes are outside these guarantees.`

func newStandardsAuthorCommand(action string) *Command {
	return newStandardsAuthorCommandWithRunner(action, nil)
}

func newStandardsAuthorCommandWithRunner(action string, runner standards.AuthoringRunner) *Command {
	name := "standards author"
	if action != "" {
		name += " " + action
	}
	c := &Command{Name: name, Usage: name + " [--json]", Short: "Save and resume assisted standards proposals", Long: standardsAuthorUsage}
	if action != "" && action != "list" {
		c.Args = ArgSpec{Names: []string{"id"}, Min: 1}
		c.Usage = name + " <id> [--json]"
	}
	c.New = func(fs *flag.FlagSet) Handler {
		var asJSON, replace bool
		var version, expected, revision int64
		var intent, target, categories, file, question, answer string
		fs.BoolVar(&asJSON, "json", false, "emit structured JSON")
		switch action {
		case "start":
			fs.StringVar(&intent, "intent", "", "intended guidance")
			fs.StringVar(&target, "target", "", "existing standard ID (omit for creation)")
			fs.StringVar(&categories, "categories", "", "comma-separated retained category filter")
			fs.StringVar(&file, "file", "", "intent text file or - for stdin")
		case "show", "questions":
			fs.Int64Var(&version, "version", 0, "retained version (default current)")
		case "answer":
			fs.StringVar(&question, "question", "", "stable question ID")
			fs.StringVar(&answer, "answer", "", "explicit answer text")
			fs.BoolVar(&replace, "replace", false, "explicitly replace an existing answer")
			fs.StringVar(&file, "file", "", "JSON answer array file or - for stdin")
		case "rebase":
			fs.Int64Var(&revision, "revision", -1, "explicit new published head from book current (zero for no book)")
		case "edit":
			fs.StringVar(&file, "file", "", "JSON draft edits file or - for stdin")
		}
		switch action {
		case "answer", "edit", "continue", "validate", "complete", "rebase":
			fs.Int64Var(&expected, "expected-version", 0, "exact inspected current version")
		}
		return func(ctx context.Context, env *Env, args []string) (err error) {
			if action == "" {
				_, err = fmt.Fprintln(env.Stdout, standardsAuthorUsage)
				return err
			}
			switch action {
			case "answer", "edit", "continue", "validate", "complete", "rebase":
				if expected < 1 {
					return Usagef(c, "--expected-version is required")
				}
			}
			rt, err := openRepoRuntime(ctx)
			if err != nil {
				return err
			}
			defer func() { err = errors.Join(err, rt.close()) }()
			owner, ownerErr := rt.db.GetIdentity(ctx, store.ForgeGitHub, store.RoleOwner)
			if ownerErr != nil && !errors.Is(ownerErr, store.ErrNotFound) {
				return ownerErr
			}
			switch action {
			case "start", "answer", "edit", "continue", "validate", "complete", "rebase":
				if ownerErr != nil || owner.UserID <= 0 {
					return Preconditionf("configured numeric owner identity required")
				}
			}
			instructions, instructionErr := instructionpackage.New(ctx, rt.paths)
			if instructionErr != nil {
				return instructionErr
			}
			svc := &standards.Authoring{Store: rt.db, Book: &standards.Service{Store: rt.db, OwnerID: owner.UserID}, Instructions: instructions}
			if action == "continue" {
				svc.Runner = runner
				if svc.Runner == nil {
					svc.Runner = newStandardsAuthoringRunner(rt)
				}
			}
			var result any
			switch action {
			case "start":
				if file != "" {
					if intent != "" {
						return Usagef(c, "use --intent or --file, not both")
					}
					var raw []byte
					raw, err = readAuthorInput(env, file)
					intent = string(raw)
				}
				if err == nil {
					mode := "create"
					if target != "" {
						mode = "edit"
					}
					result, err = svc.Start(ctx, standards.AuthoringStart{ID: args[0], Mode: mode, TargetID: target, Intent: intent, CategoryIDs: authorCategoryIDs(categories)})
				}
			case "list":
				result, err = svc.List(ctx, 100)
			case "show", "questions":
				result, err = svc.Show(ctx, args[0], version)
			case "answer":
				var answers []standards.AuthoringAnswer
				if file != "" {
					if question != "" || answer != "" || replace {
						return Usagef(c, "--file cannot be combined with question flags")
					}
					err = decodeAuthorInput(env, file, &answers)
				} else {
					if question == "" || strings.TrimSpace(answer) == "" {
						return Usagef(c, "--question and --answer, or --file, are required")
					}
					answers = []standards.AuthoringAnswer{{QuestionID: question, Text: answer, Replace: replace}}
				}
				if err == nil {
					result, err = svc.Answer(ctx, args[0], expected, answers)
				}
			case "edit":
				var edit standards.AuthoringEdit
				err = decodeAuthorInput(env, file, &edit)
				if err == nil {
					result, err = svc.Edit(ctx, args[0], expected, edit)
				}
			case "continue":
				result, err = svc.Continue(ctx, args[0], expected)
			case "validate":
				result, err = svc.Validate(ctx, args[0], expected)
			case "complete":
				result, err = svc.Complete(ctx, args[0], expected)
			case "rebase":
				if revision < 0 {
					return Usagef(c, "--revision from book current is required")
				}
				result, err = svc.Rebase(ctx, args[0], expected, revision)
			default:
				return Usagef(c, "unknown authoring operation")
			}
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(env, result)
			}
			return writeAuthorText(env.Stdout, result)
		}
	}
	return c
}

func authorCategoryIDs(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	ids := strings.Split(raw, ",")
	for i := range ids {
		ids[i] = strings.TrimSpace(ids[i])
	}
	return ids
}

func readAuthorInput(env *Env, path string) ([]byte, error) {
	if path == "" {
		return nil, Usagef(nil, "--file is required; use - for stdin")
	}
	r := env.Stdin
	if path != "-" {
		f, err := os.Open(path) // #nosec G304 -- the local operator explicitly selects this input file.
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()
		r = f
	}
	if r == nil {
		return nil, Usagef(nil, "stdin is unavailable")
	}
	const limit = 256 << 10
	raw, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > limit {
		return nil, Usagef(nil, "authoring input exceeds 256 KiB")
	}
	return raw, nil
}
func decodeAuthorInput(env *Env, path string, dst any) error {
	raw, err := readAuthorInput(env, path)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err = dec.Decode(dst); err != nil {
		return Usagef(nil, "authoring input: %v", err)
	}
	var extra any
	if !errors.Is(dec.Decode(&extra), io.EOF) {
		return Usagef(nil, "authoring input must contain exactly one JSON value")
	}
	return nil
}

func writeAuthorText(out io.Writer, result any) error {
	switch v := result.(type) {
	case standards.AuthoringSession:
		if _, err := fmt.Fprintf(out, "Session %s version %d (%s, %s)\nTarget: %s\nBase: revision %d, schema %d, digest %s\n%s\nOperator fields: %v\nContext categories: %v\nContext reason: %s\nCoverage: %v\n", v.ID, v.Version, v.Mode, v.Status, v.TargetID, v.BaseRevision, v.BaseSchema, v.BaseDigest, standards.Render(v.Base.Categories, []standards.Standard{v.Draft}), v.Overrides, v.Context.CategoryIDs, v.Context.Reason, v.Context.Coverage); err != nil {
			return err
		}
		for _, q := range v.Questions {
			if _, err := fmt.Fprintf(out, "Question %s (%s): %s\n  Options: %v\n  Answer: %s\n  Previous answers: %v\n  Follow-up to: %s\n", q.ID, q.State, q.Text, q.Options, q.Answer, q.AnswerHistory, q.FollowUpTo); err != nil {
				return err
			}
		}
		for _, d := range v.Diagnostics {
			if _, err := fmt.Fprintf(out, "Problem %s: %s\n", d.Field, d.Message); err != nil {
				return err
			}
		}
		for _, attempt := range v.Attempts {
			if _, err := fmt.Fprintf(out, "Attempt %s (%s), input version %d, result version %d\n  Provider: %s; model: %s; usage: %s\n  Lease until: %s\n  Diagnostic: %s\n", attempt.ID, attempt.Outcome, attempt.InputVersion, attempt.ResultVersion, attempt.Provider, attempt.Model, attempt.UsageReference, logsTime(attempt.LeaseUntil), attempt.Diagnostic); err != nil {
				return err
			}
		}
		if len(v.RebaseChanges) > 0 {
			raw, err := json.MarshalIndent(v.RebaseChanges, "", "  ")
			if err != nil {
				return err
			}
			if _, err = fmt.Fprintf(out, "Rebase changes:\n%s\n", raw); err != nil {
				return err
			}
		}
		if v.Proposal != nil {
			return writeAuthorText(out, *v.Proposal)
		}
		_, err := fmt.Fprintf(out, "Inspect this saved version before using --expected-version %d. Complete creates a proposal for separate human review.\n", v.Version)
		return err
	case []standards.AuthoringSession:
		for _, session := range v {
			if _, err := fmt.Fprintf(out, "%s version %d %s %s target=%s\n", session.ID, session.Version, session.Mode, session.Status, session.TargetID); err != nil {
				return err
			}
		}
		return nil
	case store.StandardsProposalVersion:
		_, err := fmt.Fprintf(out, "Proposal %s version %d\nBase revision: %d\nChange digest: %s\nResult digest: %s\nReview: litefactory standards proposal review %s --version %d\nHuman approval and publication remain separate.\n", v.ProposalID, v.Version, v.BaseRevision, v.ChangeDigest, v.ResultDigest, v.ProposalID, v.Version)
		return err
	default:
		return fmt.Errorf("unsupported authoring result %T", result)
	}
}
