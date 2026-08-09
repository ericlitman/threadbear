package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ericlitman/threadbear/assets"
)

var version = "dev"

func main() { os.Exit(run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(ctx context.Context, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, assets.HelpText)
		return 2
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(stdout, assets.HelpText)
		return 0
	}
	command := args[0]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Bool("json", false, "write JSON output")
	var action func() (any, error)

	switch command {
	case "install":
		dry := flags.Bool("dry-run", false, "preview without mutation")
		noninteractive := flags.Bool("noninteractive", false, "run without prompts")
		confirm := flags.Bool("confirm", false, "confirm the previewed installation")
		reset := flags.Bool("reset", false, "replace an exact legacy 2.2.1 installation")
		automatic := flags.Bool("automatic", false, "internal verified-update installation")
		selectedVersion := flags.String("version", "", "installer-selected release version")
		action = func() (any, error) {
			return install(ctx, installOptions{
				DryRun: *dry, Confirmed: *noninteractive && *confirm, Reset: *reset,
				Automatic: *automatic, SelectedVersion: *selectedVersion,
			})
		}
	case "title":
		selectedStatus := flags.String("status", "", "plan a title for complete, next_steps, needs_input, blocked, or automation")
		action = func() (any, error) {
			return runCurrentTitle(ctx, os.Getenv("CODEX_THREAD_ID"), *selectedStatus)
		}
	case "status":
		action = func() (any, error) { return status(ctx) }
	case "self-test":
		flags.Bool("candidate", false, "validate this binary before installation")
		action = func() (any, error) { return selfTest(ctx) }
	case "update":
		automatic := flags.Bool("automatic", false, "run from the update-only LaunchAgent")
		action = func() (any, error) { return update(ctx, *automatic) }
	case "uninstall":
		dry := flags.Bool("dry-run", false, "preview without mutation")
		prepare := flags.Bool("prepare", false, "prepare exact unarchived title cleanup")
		noninteractive := flags.Bool("noninteractive", false, "run without prompts")
		confirm := flags.Bool("confirm", false, "confirm removal")
		action = func() (any, error) {
			return uninstall(ctx, uninstallOptions{DryRun: *dry, Prepare: *prepare, Confirmed: *noninteractive && *confirm})
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
		var details map[string]any
		if encoded, encodeErr := json.Marshal(result); result != nil && encodeErr == nil {
			_ = json.Unmarshal(encoded, &details)
		}
		if details != nil {
			for key, value := range details {
				failure[key] = value
			}
			failure["ready"] = false
			failure["error"] = err.Error()
		}
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
