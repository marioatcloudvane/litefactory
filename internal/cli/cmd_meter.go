package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/meterview"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

type meterOptions struct {
	mode, codingAgent, model string
	json, details            bool
	issue                    int64
	from, to                 string
}

func newMeterCommand() *Command {
	return &Command{Name: "meter", Usage: "meter [<repository> [<task-id>]] [--mode MODE] [--ca AGENT] [--model MODEL] [--json] [--details]", Short: "Show measured tokens, frozen estimates and missing coverage", Args: ArgSpec{Names: []string{"repository", "task-id"}}, New: func(fs *flag.FlagSet) Handler {
		var o meterOptions
		fs.StringVar(&o.mode, "mode", "", "filter research, plan, implement or spike")
		fs.StringVar(&o.codingAgent, "ca", "", "filter claude or codex")
		fs.StringVar(&o.model, "model", "", "filter actual model; unresolved attribution remains visible")
		fs.BoolVar(&o.json, "json", false, "emit JSON")
		fs.BoolVar(&o.details, "details", false, "include run, attempt and invocation rows")
		return func(ctx context.Context, env *Env, args []string) error { return runMeter(ctx, env, args, o, false) }
	}}
}
func newMeterExportCommand() *Command {
	return &Command{Name: "meter export", Usage: "meter export <project> [--coding_agent AGENT] [--issue NUMBER] [--mode MODE] [--time_from RFC3339] [--time_to RFC3339]", Short: "Export one registered GitHub project's measurements as CSV", Long: "CSV is written to stdout. Filters combine with AND; time bounds select entire attempts by start time, from inclusive and to exclusive. Unknown values are blank. Snapshot limits: 1,000,000 rows, 256 MiB encoded CSV, 30 seconds preparation. Daemon helper overhead excluded. See docs/meter.md for columns and reversible spreadsheet text escaping.", Args: ArgSpec{Names: []string{"project"}, Min: 1}, New: func(fs *flag.FlagSet) Handler {
		var o meterOptions
		fs.StringVar(&o.codingAgent, "coding_agent", "", "filter claude or codex")
		fs.Func("issue", "positive project-local issue number", func(raw string) error {
			n, e := strconv.ParseInt(raw, 10, 64)
			if e != nil || n <= 0 {
				return fmt.Errorf("issue must be positive")
			}
			o.issue = n
			return nil
		})
		fs.StringVar(&o.mode, "mode", "", "filter research, plan, implement or spike")
		fs.StringVar(&o.from, "time_from", "", "inclusive attempt start instant, RFC3339")
		fs.StringVar(&o.to, "time_to", "", "exclusive attempt start instant, RFC3339")
		return func(ctx context.Context, env *Env, args []string) error {

			return runMeter(ctx, env, args, o, true)
		}
	}}
}

func meterFilter(args []string, o meterOptions, export bool) (store.MeterFilter, string, error) {
	f := store.MeterFilter{Mode: o.mode, CodingAgent: o.codingAgent, Model: o.model, Issue: o.issue}
	if export && len(args) != 1 || !export && len(args) > 2 {
		return f, "", Usagef(nil, "invalid metering scope")
	}
	project := ""
	if len(args) > 0 {
		project = args[0]
	}
	if export && !strings.HasPrefix(project, "github:") {
		return f, "", Usagef(nil, "export requires github:OWNER/REPO")
	}
	if len(args) == 2 {
		n, e := strconv.ParseInt(args[1], 10, 64)
		if e != nil || n <= 0 {
			return f, "", Usagef(nil, "task-id must be a positive issue number")
		}
		f.Issue = n
	}
	validated, e := meterview.Validate(meterview.Request{Project: project, Task: f.Issue, Mode: o.mode, Agent: o.codingAgent, Model: o.model, From: o.from, To: o.to}, false)
	if e != nil {
		return f, "", Usagef(nil, "%s", e)
	}
	f = validated
	return f, project, nil
}

func resolveMeterProject(ctx context.Context, db *store.DB, selector string) (int64, error) {
	id, err := meterview.ResolveProject(ctx, db, selector)
	var viewErr *meterview.Error
	if errors.As(err, &viewErr) {
		return 0, Usagef(nil, "%s", viewErr)
	}
	return id, err
}

func runMeter(ctx context.Context, env *Env, args []string, o meterOptions, export bool) (err error) {
	f, project, err := meterFilter(args, o, export)
	if err != nil {
		return err
	}
	if export {
		ref, e := naming.ParseRef(project + "#1")
		if e != nil || ref.Forge != "github" {
			return Usagef(nil, "export requires github:OWNER/REPO")
		}
	}
	rt, err := openRepoRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, rt.close()) }()
	f.RepoID, err = resolveMeterProject(ctx, rt.db, project)
	if err != nil {
		return err
	}
	return writeMeter(ctx, env.Stdout, rt.db, f, o, export)
}

func writeMeter(ctx context.Context, out io.Writer, db *store.DB, f store.MeterFilter, o meterOptions, export bool) (err error) {
	if !export {
		view, e := meterview.Build(ctx, db, f, o.details)
		if e != nil {
			return e
		}
		if o.json {
			return json.NewEncoder(out).Encode(view)
		}
		return view.WriteText(out)
	}
	options := store.MeterSnapshotOptions{HeaderBytes: len(meterCSVHeaderLine), RowBytes: func(r store.MeterRow) (int, error) { b, e := meterCSVBytes(r); return len(b), e }}
	snapshot, err := db.NewMeterSnapshot(ctx, f, options)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, snapshot.Close(ctx)) }()
	return writeMeterCSV(ctx, out, snapshot)
}
