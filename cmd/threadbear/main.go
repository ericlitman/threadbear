package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/ericlitman/threadbear/assets"
	"io"
	"os"
)

var version = "dev"

func main() { os.Exit(run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }
func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, assets.HelpText)
		return 2
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(stdout, assets.HelpText)
		return 0
	}
	command := args[0]
	if command == "hook" {
		if len(args) != 1 {
			return 2
		}
		if err := hook(ctx, stdin, stdout); err != nil {
			fmt.Fprintln(stderr, "ThreadBear hook:", err)
			return 1
		}
		return 0
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Bool("json", false, "write JSON output")
	var action func() (any, error)
	switch command {
	case "install":
		controlTaskID := flags.String("control-task-id", "", "active task that becomes ThreadBear's persistent home")
		dry := flags.Bool("dry-run", false, "preview without mutation")
		noninteractive := flags.Bool("noninteractive", false, "run without prompts")
		confirm := flags.Bool("confirm", false, "confirm the previewed installation")
		debugCanaries := flags.Bool("debug-canaries", false, "run guided Desktop canaries after installation")
		flags.String("version", "", "installer-selected release version")
		action = func() (any, error) { return install(*controlTaskID, *dry, *noninteractive && *confirm, *debugCanaries) }
	case "inventory":
		action = func() (any, error) {
			items, remaining, value, err := migrationInventory(ctx)
			deterministic := 0
			for _, item := range items {
				if item.Deterministic {
					deterministic++
				}
			}
			return map[string]any{"ready": err == nil && remaining == 0 && value.Phase == phaseMigrationComplete, "count": len(items), "deterministic": deterministic, "ambiguous": len(items) - deterministic, "applied": len(items) - remaining, "remaining": remaining, "phase": value.Phase, "main_task_id": value.MainTaskID, "controller_task_id": value.ControllerTaskID, "tasks": items}, err
		}
	case "migration":
		phase := flags.String("phase", "", "migration phase: migration_running, migration_complete, or migration_failed")
		controllerTaskID := flags.String("controller-task-id", "", "ephemeral migration controller task")
		settled := flags.Bool("settled", false, "confirm every admitted native call returned a terminal result")
		action = func() (any, error) { return transitionMigration(ctx, *phase, *controllerTaskID, *settled) }
	case "maintenance":
		archive := flags.String("archive", "", "stage or reconcile one eligible task archive")
		restore := flags.String("restore", "", "stage or reconcile one ThreadBear-owned restore")
		cancel := flags.String("cancel", "", "cancel one known-unapplied native archive operation")
		days := flags.Int("archive-after-days", 14, "quiet days before a completed task is eligible")
		action = func() (any, error) { return maintenance(ctx, *archive, *restore, *cancel, *days) }
	case "update":
		action = func() (any, error) { return update(ctx) }
	case "status":
		action = func() (any, error) { return status(ctx) }
	case "self-test":
		flags.Bool("candidate", false, "validate this binary before installation")
		action = selfTest
	case "uninstall":
		prepare := flags.Bool("prepare", false, "persist the initiating task and original control-task state")
		abort := flags.Bool("abort", false, "abandon the prepared uninstall after restoring the control-task archive state")
		initiatorTaskID := flags.String("initiator-task-id", "", "active task that owns this uninstall operation")
		noninteractive := flags.Bool("noninteractive", false, "run without prompts")
		confirm := flags.Bool("confirm", false, "confirm the previewed uninstall")
		action = func() (any, error) {
			switch {
			case *prepare && *abort:
				return nil, errors.New("uninstall accepts only one of --prepare or --abort")
			case *prepare:
				return prepareUninstall(ctx, *initiatorTaskID)
			case *abort:
				return completeUninstall(ctx, *initiatorTaskID, false, true)
			default:
				return completeUninstall(ctx, *initiatorTaskID, *noninteractive && *confirm, false)
			}
		}
	case "version":
		action = func() (any, error) { return map[string]any{"version": version}, nil }
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", command, assets.HelpText)
		return 2
	}
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 {
		return 2
	}
	result, err := action()
	if err != nil {
		failure := map[string]any{"ready": false, "error": err.Error()}
		var updateErr *updateError
		if errors.As(err, &updateErr) {
			failure["stage"] = updateErr.Stage
		}
		_ = json.NewEncoder(stdout).Encode(failure)
		return 1
	}
	if json.NewEncoder(stdout).Encode(result) != nil {
		return 1
	}
	return 0
}
