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
		dry := flags.Bool("dry-run", false, "preview without mutation")
		noninteractive := flags.Bool("noninteractive", false, "run without prompts")
		confirm := flags.Bool("confirm", false, "confirm the previewed installation")
		flags.String("version", "", "installer-selected release version")
		action = func() (any, error) { return install(ctx, *dry, *noninteractive && *confirm) }
	case "inventory":
		action = func() (any, error) {
			items, err := migrationInventory(ctx)
			deterministic := 0
			for _, item := range items {
				if item.Deterministic {
					deterministic++
				}
			}
			return map[string]any{"ready": err == nil, "count": len(items), "deterministic": deterministic, "ambiguous": len(items) - deterministic, "tasks": items}, err
		}
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
