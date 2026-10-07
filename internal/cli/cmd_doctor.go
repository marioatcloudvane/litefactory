package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

func newDoctorCommand(operation string) *Command {
	name := "doctor"
	names := []string{"repository", "issue", "instructions"}
	short := "Diagnose one issue independently of its workflow (unverified providers refuse launch)"
	if operation != "" {
		name += " " + operation
		names = names[:2]
		short = "Read or stop the saved doctor conversation"
	}
	return &Command{Name: name, Usage: name + " <repository> <issue>" + doctorInstructionUsage(operation), Short: short, Args: ArgSpec{Names: names, Min: len(names)}, New: func(*flag.FlagSet) Handler {
		return func(ctx context.Context, env *Env, args []string) error { return runDoctor(ctx, env, operation, args) }
	}}
}
func doctorInstructionUsage(operation string) string {
	if operation == "" {
		return " <instructions>"
	}
	return ""
}

func runDoctor(ctx context.Context, env *Env, operation string, args []string) (err error) {
	issue, err := strconv.Atoi(args[1])
	if err != nil {
		return fmt.Errorf("doctor issue must be a positive integer: %w", err)
	}
	ref, err := doctor.Target(args[0], issue)
	if err != nil {
		return err
	}
	paths, err := config.Locate()
	if err != nil {
		return err
	}
	cfg, err := config.Load(paths)
	if err != nil {
		return err
	}
	// Open never migrates, but SQLite can create a missing file. Refuse it first.
	info, err := os.Lstat(paths.DBPath())
	if err != nil {
		return fmt.Errorf("doctor requires an existing initialized database: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("doctor database must be a regular file")
	}
	db, err := store.Open(ctx, paths.DBPath())
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	service := doctor.Service{DB: db, Paths: paths, Config: cfg, Redactor: redact.New(redact.Options{Logger: env.Logger})}
	if operation == "apply" || operation == "" {
		if err = configureDoctorNative(&service, env); err != nil {
			return err
		}
	}
	if operation == "apply" {
		return applyDoctorCLI(ctx, &service, env, doctor.ApplyRequest{Target: ref, SourceID: "cli-apply:" + rand.Text(), Principal: "local-owner"})
	}

	if operation == "stop" {
		stop, err := service.StopIssue(ctx, ref)
		if err != nil {
			return err
		}
		if stop.RunID != 0 {
			run, err := db.GetRun(ctx, stop.RunID)
			if err != nil {
				return err
			}
			if run.Status == store.RunRunning {
				_, err = fmt.Fprintf(env.Stdout, "Stop requested for workflow run %d; cancellation and saving are pending daemon settlement. Doctor conversation stopped: %t. No issue comment will be posted.\n", stop.RunID, stop.Conversation)
				return err
			}
			_, err = fmt.Fprintln(env.Stdout, doctor.StopSummary(stop)+" "+doctor.StopRunSummary(run))
			return err
		}
		_, err = fmt.Fprintln(env.Stdout, doctor.StopSummary(stop))
		return err
	}

	var result doctor.Conversation
	if operation == "status" {
		result, err = service.ReadConversation(ctx, ref)
	} else {
		result, err = service.Diagnose(ctx, doctor.DiagnoseRequest{Target: ref, Instructions: args[2]})
	}
	if result.Conversation.ID != 0 {
		displayErr := renderDoctor(env.Stdout, result)
		err = errors.Join(err, displayErr)
		if displayErr == nil {
			_, presentationErr := service.RecordPresentation(ctx, ref, result)
			err = errors.Join(err, presentationErr)
		}
	}
	return err
}

func renderDoctor(w io.Writer, result doctor.Conversation) error {
	var text bytes.Buffer
	fmt.Fprintf(&text, "Doctor conversation %d · %s · generation %d\n", result.Conversation.ID, result.Conversation.State, result.Conversation.Generation)
	if len(result.Attempts) > 0 {
		last := result.Attempts[len(result.Attempts)-1]
		fmt.Fprintf(&text, "Last attempt: %s\n", last.State)
		if last.Failure != "" {
			fmt.Fprintf(&text, "%s\n", last.Failure)
		}
	}
	if result.Diagnosis != nil {
		for _, f := range result.Diagnosis.Findings {
			fmt.Fprintf(&text, "\n%s\nEvidence: %s\nUncertainty: %s\n", f.Explanation, strings.Join(f.Evidence, ", "), f.Uncertainty)
		}
		for _, gap := range result.Diagnosis.CoverageGaps {
			fmt.Fprintf(&text, "Coverage gap: %s\n", gap)
		}
		for _, question := range result.Diagnosis.Questions {
			fmt.Fprintf(&text, "Question: %s\n", question)
		}
	} else {
		text.WriteString("No accepted diagnosis is saved yet.\n")
	}
	for _, step := range result.CurrentProposal {
		fmt.Fprintf(&text, "\n%d. %s\nPrecondition: %s\nChange: %s\nReason: %s\nReversibility: %s\nRollback: %s\n", step.Number, step.Target, step.Precondition, step.Change, step.Reason, step.Reversibility, step.Rollback)
		if step.Action != nil {
			fmt.Fprintf(&text, "Exact operation: %s\nBefore: %s\nAfter: %s\n", step.Action.Kind, step.Action.Before, step.Action.After)
		}
	}
	if result.Repair != nil {
		fmt.Fprintf(&text, "\nRepair %s: %s\n", result.Repair.ID, result.Repair.State)
		if result.Repair.Failure != "" {
			fmt.Fprintf(&text, "%s\n", result.Repair.Failure)
		}
		for _, step := range result.RepairSteps {
			fmt.Fprintf(&text, "Step %d: %s; backup: %s\n", step.Number, step.State, step.BackupReference)
		}
		if result.Restart != nil {
			fmt.Fprintf(&text, "Restart: %s\n%s\n", result.Restart.State, result.Restart.Report)
		}
	}
	text.WriteString("\nApply is unavailable. Repeat doctor with this target and new instructions to continue.\n")
	_, err := w.Write(text.Bytes())
	return err
}
