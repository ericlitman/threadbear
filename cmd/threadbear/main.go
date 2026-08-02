package main

import (
	"context"
	"encoding/json"
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
		flags.String("version", "", "installer-selected release version")
		action = func() (any, error) { return install(*controlTaskID, *dry, *noninteractive && *confirm) }
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
		action = func() (any, error) { return transitionMigration(ctx, *phase, *controllerTaskID) }
	case "status":
		action = status
	case "self-test":
		flags.Bool("candidate", false, "validate this binary before installation")
		action = selfTest
	case "uninstall":
		noninteractive := flags.Bool("noninteractive", false, "run without prompts")
		confirm := flags.Bool("confirm", false, "confirm the previewed uninstall")
		action = func() (any, error) { return uninstall(ctx, *noninteractive && *confirm) }
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
		_ = json.NewEncoder(stdout).Encode(map[string]any{"ready": false, "error": err.Error()})
		return 1
	}
	if json.NewEncoder(stdout).Encode(result) != nil {
		return 1
	}
	return 0
}
