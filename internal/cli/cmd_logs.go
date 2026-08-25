package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// followInterval is how often "logs -f" re-reads the run's JSONL for events
// that have appeared since the last read.
//
// Polling is the whole mechanism, deliberately. The writer is the daemon — a
// separate process appending to the file — so a filesystem watcher would buy
// nothing over a ticker while adding platform surface, which is why fsnotify
// is a rejected dependency.
const followInterval = 250 * time.Millisecond

// followReadSize is how many bytes are taken from the file per read. It is
// comfortably larger than a typical stream-json line and far smaller than a
// whole run, so a busy run drains in a handful of reads without holding the
// entire log in memory.
const followReadSize = 64 << 10

// terminalEventType is the "type" of the last event claude writes for a run.
// Seeing it is what ends a follow: the daemon will append nothing more.
//
// Note that this matches on the event type only. Whether the run succeeded is
// carried by is_error, never by subtype, which reports "success" even on failed
// runs (SD-3) — but `logs` only has to know that the stream ended, not how.
const terminalEventType = "result"

// logsOptions holds the parsed flags of "litefactory logs".
type logsOptions struct {
	// run selects a single run by ordinal; 0 means the newest run.
	run int
	// runs lists the issue's runs instead of showing one run's log.
	runs bool
	// raw writes the run's JSONL to stdout byte for byte.
	raw bool
	// follow streams events as they are appended.
	follow bool
	// only is the comma-separated list of event kinds to show.
	only string
	// grep is a regexp each output line must match.
	grep string
	// ignoreCase folds case in the --grep pattern.
	ignoreCase bool
	// invert shows the lines --grep does not match.
	invert bool
	// full lifts the length caps on tool arguments and tool output.
	full bool
	// repo disambiguates an issue number registered in more than one repo.
	repo string
}

// bind registers the logs flags on fs.
func (o *logsOptions) bind(fs *flag.FlagSet) {
	fs.IntVar(&o.run, "run", 0, "show run `N` (default: the newest run)")
	fs.BoolVar(&o.runs, "runs", false, "list the issue's runs instead of showing one")
	fs.BoolVar(&o.raw, "raw", false, "write the run's JSONL to stdout byte for byte")
	fs.BoolVar(&o.follow, "f", false, "follow the run as events are written")
	fs.StringVar(&o.only, "only", "", "show only these event `kinds`, comma-separated "+
		"(system, text, think, tool, output, result, user, other; or agent, all)")
	fs.StringVar(&o.grep, "grep", "", "show only lines matching `regexp`")
	fs.BoolVar(&o.ignoreCase, "i", false, "make --grep case-insensitive")
	fs.BoolVar(&o.invert, "invert", false, "show the lines --grep does not match")
	fs.BoolVar(&o.full, "full", false, "do not shorten tool arguments or tool output")
	fs.StringVar(&o.repo, "repo", "", "disambiguate the issue with `owner/name`")
}

// filtering reports whether the caller asked for a subset of the log. It is
// what decides whether the run's metadata summary is printed: a filtered view
// is a view someone intends to read or pipe line by line, and a header block
// on top of it is noise.
func (o logsOptions) filtering() bool { return o.grep != "" || o.only != "" }

// newSink builds the pipeline the run's lines are written through.
func (o logsOptions) newSink(out io.Writer, kinds kindSet, grep *lineFilter) logSink {
	if o.raw {
		return &rawSink{out: out, grep: grep}
	}
	return newRenderSink(out, kinds, grep, o.full)
}

// newLogsCommand declares "litefactory logs <issue> [--run N] [--only kinds]
// [--grep regexp] [--raw] [-f] [--repo owner/name]".
//
// The positional issue number is sliced off before the flag set sees the
// arguments, so "logs 123 --run 2" parses; see splitPositionals.
func newLogsCommand() *Command {
	cmd := &Command{
		Name:  "logs",
		Usage: "logs <issue> [--run N] [--only kinds] [--grep regexp] [--raw] [-f]",
		Short: "Show the local, unredacted log for an issue",
		Long: "Show what the forge comment left out. Reads the database and the run logs\n" +
			"under $LF_HOME directly, so it works with the daemon stopped. Output is\n" +
			"never redacted.\n\n" +
			"The task is named by its issue number or by its full ref, as in\n" +
			"github:acme/api#123.\n\n" +
			"With no flags it prints the newest run: a summary, then the run's events\n" +
			"rendered one labelled line at a time. --run N selects another run and\n" +
			"--runs lists them, newest last.\n\n" +
			"Each rendered line starts with its kind, so the output is greppable:\n" +
			"  system  the session's init line\n" +
			"  text    what claude said\n" +
			"  think   thinking blocks (hidden unless asked for)\n" +
			"  tool    a tool call and its main argument\n" +
			"  output  what the tool returned\n" +
			"  result  how the run ended, and its final text\n" +
			"  user    a user-role message that is not a tool result\n" +
			"  other   anything else, including lines that are not JSON\n\n" +
			"Examples:\n" +
			"  litefactory logs 123 -f --only agent   watch what the agents are doing\n" +
			"  litefactory logs 123 --only tool       every tool call the run made\n" +
			"  litefactory logs 123 --grep 'go test'  only the lines mentioning it\n" +
			"  litefactory logs 123 --raw             the JSONL, byte for byte",
		Args: ArgSpec{Names: []string{"issue"}, Min: 1},
	}
	cmd.New = func(fs *flag.FlagSet) Handler {
		var opts logsOptions
		opts.bind(fs)
		return func(ctx context.Context, env *Env, args []string) error {
			return runLogs(ctx, env, cmd, opts, args[0])
		}
	}
	return cmd
}

// runLogs resolves the issue to a task and prints one of its views: the run
// list, one run's rendered log, one run's raw JSONL, or a live follow of
// either.
//
// Nothing here is redacted, and this file must never import internal/redact.
// Redaction is outbound-only by design (S-7): the point of `logs` is that the
// local copy is the full truth, so a token-shaped string in the JSONL appears
// here verbatim (AC-5.7). TestLogsNeverImportsRedact fails the build if that
// is ever "fixed".
//
// There is also no daemon IPC of any kind (AC-5.8) — no socket, no signal, no
// lock. The database and the files under $LF_HOME/logs/runs are read directly,
// so every view works with the daemon stopped.
func runLogs(ctx context.Context, env *Env, cmd *Command, opts logsOptions, issueArg string) error {
	issue, owner, name, err := parseTaskArg(cmd, issueArg)
	if err != nil {
		return err
	}
	if owner == "" {
		if owner, name, err = parseRepoFlag(cmd, opts.repo); err != nil {
			return err
		}
	} else if opts.repo != "" {
		return Usagef(cmd, "a task ref already names its repository; drop --repo %s", opts.repo)
	}
	if opts.run < 0 {
		return Usagef(cmd, "--run takes a run ordinal of 1 or more, got %d", opts.run)
	}
	kinds := newKindSet(defaultKinds)
	if opts.only != "" {
		if kinds, err = parseKinds(opts.only); err != nil {
			return Usagef(cmd, "%s", err)
		}
	}
	grep, err := newLineFilter(opts.grep, opts.ignoreCase, opts.invert)
	if err != nil {
		return Usagef(cmd, "%s", err)
	}

	paths, err := config.Locate()
	if err != nil {
		return err
	}
	if _, err := os.Stat(paths.DBPath()); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Preconditionf("no litefactory database at %s: run `litefactory init` first", paths.DBPath())
		}
		return fmt.Errorf("read %s: %w", paths.DBPath(), err)
	}

	target, err := loadLogTarget(ctx, paths, issue, owner, name)
	if err != nil {
		return lookupError(err, issue, firstNonEmpty(opts.repo, joinRepo(owner, name)))
	}

	if opts.runs {
		return writeRunList(env.Stdout, target)
	}
	if len(target.runs) == 0 && !opts.raw && !opts.follow {
		// A task that exists but has never run is a state, not a failure. The
		// byte-oriented views still fail on it: there is no file to stand in
		// for, and a caller piping --raw wants a non-zero exit, not a sentence.
		fmt.Fprintf(env.Stdout, "%s has no runs yet\n", target.session.TaskRef)
		return nil
	}

	run, err := selectRun(target.runs, opts.run, issue)
	if err != nil {
		return err
	}
	path, err := target.logPath(run)
	if err != nil {
		return err
	}
	sink := opts.newSink(env.Stdout, kinds, grep)
	switch {
	case opts.follow:
		// A run that has already finished stops at end of file instead of
		// waiting for events that will never come (AC-5.6).
		return tailLog(ctx, sink, path, followInterval, run.Status != store.RunRunning)
	case opts.raw && grep == nil:
		// The unfiltered raw view is a byte-for-byte copy and nothing else
		// (AC-5.5); filtering it necessarily goes line by line instead.
		return copyLog(env.Stdout, path)
	default:
		return writeRun(env.Stdout, target, run, path, sink, opts)
	}
}

// writeRun prints one run: its metadata summary, when the view is not a
// filtered or raw one, followed by the run's own log.
//
// This is the answer to "show me the logs". The summary is four lines of
// context — how it went, how long it took, what it cost — and then the events
// themselves, rather than a path to a JSON file to go and read elsewhere.
func writeRun(out io.Writer, target logTarget, run store.Run, path string, sink logSink, opts logsOptions) error {
	summarise := !opts.raw && !opts.filtering()
	if summarise {
		if err := writeRunSummary(out, target, run); err != nil {
			return err
		}
		fmt.Fprintln(out)
	}

	err := streamLog(path, sink)
	switch {
	case errors.Is(err, fs.ErrNotExist) && !opts.raw:
		// A run can be allocated and killed before claude writes a byte. The
		// summary above is still worth having, so this is reported rather than
		// returned.
		fmt.Fprintf(out, "no event log at %s\n", path)
		return nil
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("no event log at %s", path)
	case err != nil:
		return err
	}

	if rendered, ok := sink.(*renderSink); summarise && ok && !rendered.sawResult {
		// A run can end without a terminal result event; that is a real
		// outcome, not a corrupt file (SD-3).
		fmt.Fprintf(out, "\nthe run wrote no result event\n")
	}
	return nil
}

// logTarget is everything `logs` needs from the database: the task, its runs,
// and the ref its files are stored under.
type logTarget struct {
	session store.Session
	runs    []store.Run
	ref     naming.Ref
	home    string
}

// loadLogTarget reads the task and its runs, and closes the database before
// returning.
//
// Reading everything up front is what lets the handle be released this early,
// and that matters for `-f`: a follow can run for an hour, and holding a SQLite
// connection open across it for no reason would be the one way this read-only
// command could interfere with the daemon.
func loadLogTarget(ctx context.Context, paths config.Paths, issue int, owner, name string) (_ logTarget, err error) {
	db, err := store.Open(ctx, paths.DBPath())
	if err != nil {
		return logTarget{}, err
	}
	defer func() {
		if cerr := db.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	session, err := db.FindSessionByIssue(ctx, issue, owner, name)
	if err != nil {
		return logTarget{}, err
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		return logTarget{}, fmt.Errorf("task %s: %w", session.TaskRef, err)
	}
	// ListRuns orders by ordinal, so the slice is oldest first and newest last
	// — the order the listing prints and the order the newest run is picked
	// from (AC-5.1).
	runs, err := db.ListRuns(ctx, session.ID)
	if err != nil {
		return logTarget{}, err
	}
	return logTarget{session: session, runs: runs, ref: ref, home: paths.Home()}, nil
}

// logPath is where a run's event stream lives. The path recorded on the row
// wins, because that is the file the run actually wrote; deriving it is the
// fallback for a row that carries none.
func (t logTarget) logPath(run store.Run) (string, error) {
	if run.LogPath != "" {
		return run.LogPath, nil
	}
	return naming.RunLogPath(t.home, t.ref, run.Ordinal)
}

// metaPath is where a run's NNNN.meta.json lives. It is derived rather than
// stored: it is the sibling of the event stream under the same ordinal stem.
func (t logTarget) metaPath(run store.Run) (string, error) {
	return naming.RunMetaPath(t.home, t.ref, run.Ordinal)
}

// selectRun picks the run to show: the one named by --run, or the newest when
// the flag was omitted.
func selectRun(runs []store.Run, ordinal, issue int) (store.Run, error) {
	if len(runs) == 0 {
		return store.Run{}, fmt.Errorf("issue %d has no runs yet", issue)
	}
	if ordinal == 0 {
		return runs[len(runs)-1], nil
	}
	for _, run := range runs {
		if run.Ordinal == ordinal {
			return run, nil
		}
	}
	return store.Run{}, fmt.Errorf("issue %d has no run %d: it has runs %d to %d",
		issue, ordinal, runs[0].Ordinal, runs[len(runs)-1].Ordinal)
}

// writeRunList prints one line per run, oldest first and newest last (AC-5.1).
func writeRunList(out io.Writer, target logTarget) error {
	session := target.session
	if len(target.runs) == 0 {
		fmt.Fprintf(out, "%s has no runs yet\n", session.TaskRef)
		return nil
	}

	fmt.Fprintf(out, "%s  state %s", session.TaskRef, session.State)
	if session.Branch != "" {
		fmt.Fprintf(out, "  branch %s", session.Branch)
	}
	fmt.Fprintln(out)

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "RUN\tSTARTED\tDURATION\tEXIT\tMODE\tSTATUS\tTRIGGER\tRESULT\tLOG")
	for _, run := range target.runs {
		stem, err := naming.FormatRunOrdinal(run.Ordinal)
		if err != nil {
			return err
		}
		path, err := target.logPath(run)
		if err != nil {
			return err
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			stem, logsTime(run.StartedAt), logsDuration(run), logsExitCode(run),
			run.Mode, run.Status, logsID(run.TriggerCommentID), logsID(run.ResultCommentID), path)
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("write run list: %w", err)
	}
	return nil
}

// runMeta is the S-3 run metadata document, NNNN.meta.json. It is unredacted
// and holds the prompt exactly as it was sent, which is why the summary reports
// the prompt's size and where to read it rather than printing it inline.
//
// This is the reading half of the on-disk contract; internal/turn writes it.
// Every field is optional here: a run that was killed before its metadata was
// written still has a database row and an event stream, and `--run N` must
// stay useful for it.
type runMeta struct {
	RunOrdinal      int      `json:"run_ordinal"`
	TaskRef         string   `json:"task_ref"`
	SessionID       int64    `json:"session_id"`
	SegmentOrdinal  int      `json:"segment_ordinal"`
	ClaudeSessionID string   `json:"claude_session_id"`
	Mode            string   `json:"mode"`
	Model           string   `json:"model"`
	Prompt          string   `json:"prompt"`
	WorktreePath    string   `json:"worktree_path"`
	Branch          string   `json:"branch"`
	SHABefore       string   `json:"sha_before"`
	SHAAfter        string   `json:"sha_after"`
	ExitCode        *int64   `json:"exit_code"`
	StartedAt       string   `json:"started_at"`
	FinishedAt      string   `json:"finished_at"`
	DurationMS      *int64   `json:"duration_ms"`
	CostUSD         *float64 `json:"cost_usd"`
	FilesTouched    []string `json:"files_touched"`
	ResultCommentID int64    `json:"result_comment_id"`
	Error           string   `json:"error"`
}

// field is one label/value pair in a summary block.
type field struct {
	label string
	value string
}

// writeRunSummary prints a run's S-3 metadata summary (AC-5.4). The run's
// events are printed after it by [writeRun]; the final result text is the last
// thing they carry, so it is not repeated here.
//
// The database row and the metadata file overlap. The row wins wherever both
// speak, because the row is written transactionally and the metadata file is
// best-effort on abnormal termination (S-3); the file supplies what the row
// does not carry.
func writeRunSummary(out io.Writer, target logTarget, run store.Run) error {
	stem, err := naming.FormatRunOrdinal(run.Ordinal)
	if err != nil {
		return err
	}
	logPath, err := target.logPath(run)
	if err != nil {
		return err
	}
	metaPath, err := target.metaPath(run)
	if err != nil {
		return err
	}

	meta, metaNote, err := readRunMeta(metaPath)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "run %s of %s\n\n", stem, target.session.TaskRef)

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, f := range []field{
		{"status", string(run.Status)},
		{"exit code", logsExitCode(run)},
		{"error", firstNonEmpty(run.Error, meta.Error)},
		{"mode", firstNonEmpty(run.Mode, meta.Mode)},
		{"model", meta.Model},
		{"started", logsTime(run.StartedAt)},
		{"finished", logsTime(run.FinishedAt)},
		{"duration", logsDuration(run)},
		{"cost", logsCost(run.CostUSD, meta.CostUSD)},
		{"segment", logsSegment(meta.SegmentOrdinal)},
		{"claude session", meta.ClaudeSessionID},
		{"branch", firstNonEmpty(meta.Branch, target.session.Branch)},
		{"worktree", firstNonEmpty(meta.WorktreePath, target.session.WorktreePath)},
		{"sha before", meta.SHABefore},
		{"sha after", firstNonEmpty(run.CommitSHA, meta.SHAAfter)},
		{"files touched", strings.Join(meta.FilesTouched, " ")},
		{"trigger comment", logsID(run.TriggerCommentID)},
		{"result comment", logsID(firstNonZero(run.ResultCommentID, meta.ResultCommentID))},
		{"prompt", logsPrompt(meta.Prompt)},
		{"log", logPath},
		{"metadata", metaNote},
	} {
		if f.value == "" {
			continue
		}
		fmt.Fprintf(tw, "  %s\t%s\n", f.label, f.value)
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("write run summary: %w", err)
	}
	return nil
}

// readRunMeta loads NNNN.meta.json. The second result is what the summary
// should print for the metadata file: its path, or the path plus why it could
// not be read.
//
// A missing or malformed metadata file is reported, not fatal. It is written
// after the run and only best-effort when the daemon is killed, so refusing to
// show the run because of it would hide the log exactly when it is wanted.
func readRunMeta(path string) (runMeta, string, error) {
	// #nosec G304 -- path is litefactory's own artefact path, not user input:
	// naming.RunMetaPath derives it from $LF_HOME and the validated task ref of
	// a row this daemon wrote. The command reads run artefacts by design.
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return runMeta{}, path + " (not written)", nil
	case err != nil:
		return runMeta{}, "", fmt.Errorf("read %s: %w", path, err)
	}

	var meta runMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return runMeta{}, fmt.Sprintf("%s (unreadable: %v)", path, err), nil
	}
	return meta, path, nil
}

// isTerminalEvent reports whether line is the run's terminal result event: the
// last line a run's log ever gets, and the one that ends a follow.
//
// The substring pre-check is what keeps this cheap: it runs on every line of
// every run, and stream-json lines have been measured past 70 KB, so parsing
// each one to discover it is an assistant message would be wasteful.
func isTerminalEvent(line []byte) bool {
	if !bytes.Contains(line, []byte(`"`+terminalEventType+`"`)) {
		return false
	}
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(line, &envelope); err != nil {
		// A partially written or non-JSON line is not the terminal event.
		// Following a live file must not fail on one.
		return false
	}
	return envelope.Type == terminalEventType
}

// streamLog feeds every line of a finished run's log to sink.
//
// Lines are read with bufio.Reader.ReadBytes rather than bufio.Scanner:
// Scanner aborts the read with ErrTooLong past 64 KB per token instead of
// truncating, and real stream-json lines exceed that (SD-6) — which would
// silently drop the very event you were looking for.
//
// The sink's "done" is ignored here. A follower stops at the terminal event
// because nothing can come after it; a file reader has the whole file in front
// of it and prints all of it, so a log with anything after its result event
// shows that rather than hiding it.
func streamLog(path string, sink logSink) (err error) {
	// #nosec G304 -- path comes from the runs row litefactory wrote, or is
	// derived by naming from $LF_HOME and the validated task ref. Reading that
	// file is the whole point of the command.
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	reader := bufio.NewReaderSize(f, followReadSize)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			if _, writeErr := sink.write(line); writeErr != nil {
				return writeErr
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return fmt.Errorf("read %s: %w", path, readErr)
		}
	}
}

// copyLog writes the run's JSONL to out byte for byte (AC-5.5).
//
// io.Copy is the whole implementation on purpose. Decoding and re-encoding the
// events, or normalising line endings, would make the output a rendering of the
// log rather than the log — and this is the command people reach for when they
// need to know exactly what claude emitted.
func copyLog(out io.Writer, path string) (err error) {
	// #nosec G304 -- path comes from the runs row litefactory wrote, or is
	// derived by naming from $LF_HOME and the validated task ref. Reading that
	// file is the whole point of the command.
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("no event log at %s", path)
		}
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if _, err := io.Copy(out, f); err != nil {
		return fmt.Errorf("copy %s: %w", path, err)
	}
	return nil
}

// tailLog streams the run's event stream to sink as the daemon appends to it
// (AC-5.6). What reaches stdout is whatever the sink makes of each line:
// rendered lines by default, the raw bytes under --raw.
//
// It returns when the terminal event is written, when ctx is cancelled (SIGINT
// reaches the command as a cancelled context, and interrupting a follow is a
// normal way to end one, not a failure), or immediately after draining the file
// when finished says the run has already ended.
//
// The follow position is the offset of the open file handle — an
// [io.ReadSeeker] held across ticks — so each tick resumes exactly where the
// last one stopped without re-reading or re-seeking. Run logs are append-only
// and never rotated, which is what makes holding the offset sufficient.
//
// There is no goroutine here and no watcher: the loop reads, then waits on the
// ticker and ctx together. That is what keeps `logs -f` responsive to Ctrl-C
// while spending only one of the program's four goroutines — the one already
// running this command.
func tailLog(ctx context.Context, sink logSink, path string, interval time.Duration, finished bool) (err error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var (
		f io.ReadSeeker
		// closeFile closes f; it is nil until the file has been opened, which
		// for a still-running run may take several ticks.
		closeFile func() error
		// pending holds the bytes after the last newline seen: the daemon may
		// be mid-write, and half an event must not be printed as an event. It
		// is completed by a later read rather than dropped.
		pending []byte
	)
	defer func() {
		if closeFile == nil {
			return
		}
		if cerr := closeFile(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	buf := make([]byte, followReadSize)
	for {
		if f == nil {
			// #nosec G304 -- path comes from the runs row litefactory wrote, or
			// is derived by naming from $LF_HOME and the validated task ref.
			// Following that file is the whole point of the command.
			opened, openErr := os.Open(path)
			switch {
			case openErr == nil:
				f, closeFile = opened, opened.Close
			case errors.Is(openErr, fs.ErrNotExist) && !finished:
				// The run is allocated but claude has not written its first
				// event yet. Wait for the file rather than failing.
			case errors.Is(openErr, fs.ErrNotExist):
				return fmt.Errorf("no event log at %s", path)
			default:
				return openErr
			}
		}
		if f != nil {
			stop, drainErr := drainLog(ctx, f, sink, buf, &pending)
			if drainErr != nil {
				return drainErr
			}
			if stop {
				return nil
			}
		}
		if finished {
			// Nothing will ever be appended, so a trailing partial line is not
			// half of a future event — it is the file's real tail, left behind
			// by a writer that was killed mid-line. Printing it keeps a follow
			// of a finished run as complete as --raw.
			return flushPending(sink, &pending)
		}

		select {
		case <-ctx.Done():
			// Interrupted mid-line: the partial stays unprinted rather than
			// being emitted as a truncated event.
			return nil
		case <-ticker.C:
		}
	}
}

// drainLog feeds every complete line that has appeared in f since the last
// call to sink. It reports stop when the follow should end: either the
// terminal event was written, or ctx was cancelled.
func drainLog(ctx context.Context, f io.Reader, sink logSink, buf []byte, pending *[]byte) (stop bool, err error) {
	for {
		// A writer that keeps up with the reader would otherwise hold this
		// loop past a Ctrl-C, because the select on ctx is outside it.
		if ctx.Err() != nil {
			//nolint:nilerr // Deliberate: interrupting a follow is how one ends,
			// not a failure, so the cancellation is reported as stop-without-error
			// exactly as tailLog's own <-ctx.Done() branch does. Returning
			// ctx.Err() here would make Ctrl-C exit non-zero.
			return true, nil
		}
		n, readErr := f.Read(buf)
		if n > 0 {
			*pending = append(*pending, buf[:n]...)
			done, writeErr := emitLines(sink, pending)
			if writeErr != nil {
				return false, writeErr
			}
			if done {
				return true, nil
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return false, nil
			}
			return false, fmt.Errorf("read run log: %w", readErr)
		}
	}
}

// flushPending writes whatever is left of an unterminated final line. It is
// only ever called once no further bytes can arrive.
func flushPending(sink logSink, pending *[]byte) error {
	if len(*pending) == 0 {
		return nil
	}
	if _, err := sink.write(*pending); err != nil {
		return err
	}
	*pending = nil
	return nil
}

// emitLines feeds each newline-terminated line in pending to sink and leaves
// any trailing partial line behind for the next read. It stops at the terminal
// event, which is the last line a run's log ever gets.
func emitLines(sink logSink, pending *[]byte) (bool, error) {
	rest := *pending
	defer func() { *pending = rest }()

	for {
		i := bytes.IndexByte(rest, '\n')
		if i < 0 {
			return false, nil
		}
		line := rest[:i+1]
		rest = rest[i+1:]
		done, err := sink.write(line)
		if err != nil {
			return false, err
		}
		if done {
			return true, nil
		}
	}
}

// lookupError turns a failed issue lookup into something a person can act on:
// never a bare store error, never a stack trace (AC-5.3).
func lookupError(err error, issue int, repo string) error {
	var ambiguous *store.AmbiguousIssueError
	switch {
	case errors.As(err, &ambiguous):
		return ambiguousIssueError(ambiguous)
	case errors.Is(err, store.ErrNotFound):
		if repo != "" {
			return fmt.Errorf("no task for issue %d in %s: litefactory has never run it", issue, repo)
		}
		return fmt.Errorf("no task for issue %d: litefactory has never run it", issue)
	default:
		return err
	}
}

// ambiguousIssueError renders the candidate repositories the issue number
// matched, each as the --repo value that would pick it (AC-5.2). Printing them
// is the whole reason the store reports ambiguity as a typed error rather than
// a sentinel.
func ambiguousIssueError(ambiguous *store.AmbiguousIssueError) error {
	var b strings.Builder
	fmt.Fprint(&b, "Pass --repo owner/name to choose one:\n")
	for _, candidate := range ambiguous.Candidates {
		fmt.Fprintf(&b, "  --repo %s\n", candidate)
	}
	return &UsageError{
		Usage: b.String(),
		Err: fmt.Errorf("issue %d exists in more than one registered repository",
			ambiguous.IssueNumber),
	}
}

// parseTaskArg reads the positional argument, which names a task either by its
// issue number or by the full task ref.
//
// A leading "#" is accepted on the number because that is how issues are
// written everywhere else. The ref form — github:acme/api#123 — is accepted
// because it is what every comment marker, prompt header and log line calls a
// task, so it is what people have in their clipboard; it also carries the
// repository, which is what --repo would otherwise have to supply.
//
// owner and name are empty for the number form.
func parseTaskArg(cmd *Command, arg string) (issue int, owner, name string, err error) {
	if strings.ContainsAny(arg, ":/") {
		ref, refErr := naming.ParseRef(arg)
		if refErr != nil {
			return 0, "", "", Usagef(cmd, "%q is neither an issue number nor a task ref: %v", arg, refErr)
		}
		return ref.Issue, ref.Owner, ref.Repo, nil
	}
	issue, convErr := strconv.Atoi(strings.TrimPrefix(arg, "#"))
	if convErr != nil || issue <= 0 {
		return 0, "", "", Usagef(cmd, "issue must be a positive number, got %q", arg)
	}
	return issue, "", "", nil
}

// joinRepo renders owner/name, or nothing when the repository is not known.
func joinRepo(owner, name string) string {
	if owner == "" || name == "" {
		return ""
	}
	return owner + "/" + name
}

// parseRepoFlag splits --repo owner/name. Both halves are empty when the flag
// was omitted, which is what tells the store to search every repository.
func parseRepoFlag(cmd *Command, value string) (owner, name string, err error) {
	if value == "" {
		return "", "", nil
	}
	owner, name, ok := strings.Cut(value, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return "", "", Usagef(cmd, "--repo takes owner/name, got %q", value)
	}
	return owner, name, nil
}

// noValue is what every column prints when the value it would hold does not
// exist yet: a run that has not exited has no exit code, and a run that has not
// been reported has no result comment.
//
// These renderers are named for the command rather than for the type they
// format because they encode how `logs` chooses to show a value — an empty
// column here, a dash there — not a package-wide convention.
const noValue = "-"

func logsExitCode(run store.Run) string {
	if run.ExitCode == nil {
		return noValue
	}
	return strconv.FormatInt(*run.ExitCode, 10)
}

func logsDuration(run store.Run) string {
	d, ok := run.Duration()
	if !ok {
		return noValue
	}
	return d.Round(time.Millisecond).String()
}

func logsTime(t time.Time) string {
	if t.IsZero() {
		return noValue
	}
	return store.FormatTime(t)
}

func logsID(id int64) string {
	if id == 0 {
		return noValue
	}
	return strconv.FormatInt(id, 10)
}

func logsSegment(ordinal int) string {
	if ordinal == 0 {
		return ""
	}
	return strconv.Itoa(ordinal)
}

func logsCost(values ...*float64) string {
	for _, v := range values {
		if v != nil {
			return fmt.Sprintf("$%.4f", *v)
		}
	}
	return ""
}

// logsPrompt reports the prompt's size rather than its text. The prompt as sent
// is often thousands of lines and would bury the result underneath it; the
// summary says where to read it instead.
func logsPrompt(prompt string) string {
	if prompt == "" {
		return ""
	}
	return fmt.Sprintf("%d bytes (in the metadata file)", len(prompt))
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstNonZero(values ...int64) int64 {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}
	return 0
}
