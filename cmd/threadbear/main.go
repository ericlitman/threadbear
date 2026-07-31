package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

var version = "dev"

const heartbeatLimit = 4*time.Minute + 30*time.Second

const helpText = `ThreadBear keeps Codex task titles useful with a deterministic, low-token heartbeat.

Usage:
  threadbear <command> [flags]

Commands:
  install      Preview or install the managed runtime
  heartbeat    Scan changed tasks and stage guarded title decisions
  status       Show runtime health and the latest scan
  self-test    Validate a release candidate
  uninstall    Remove ThreadBear while preserving current titles
  version      Show the installed version

Run threadbear <command> --help for command flags.
`

var commandHelp = map[string]string{
	"install":   "Usage: threadbear install [--control-task-id ID] [--dry-run] [--noninteractive --confirm] [--json]\n",
	"heartbeat": "Usage: threadbear heartbeat [--dry-run] [--json]\n",
	"status":    "Usage: threadbear status [--json]\n",
	"self-test": "Usage: threadbear self-test [--candidate] [--json]\n",
	"uninstall": "Usage: threadbear uninstall --noninteractive --confirm [--json]\n",
	"version":   "Usage: threadbear version [--json]\n",
}

func main() { os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr)) }

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		io.WriteString(stdout, helpText)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	if args[0] == "help" {
		if len(args) == 2 {
			io.WriteString(stdout, commandHelp[args[1]])
		} else {
			io.WriteString(stdout, helpText)
		}
		return 0
	}
	command := args[0]
	if len(args) == 2 && (args[1] == "--help" || args[1] == "-h") {
		io.WriteString(stdout, commandHelp[command])
		return 0
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	asJSON := flags.Bool("json", false, "write JSON output")
	var result any
	var err error
	switch command {
	case "install":
		control := flags.String("control-task-id", "", "adopt active Codex task ID")
		dry := flags.Bool("dry-run", false, "validate and print effects without mutation")
		noninteractive := flags.Bool("noninteractive", false, "run without prompts")
		confirm := flags.Bool("confirm", false, "confirm the previewed installation")
		flags.String("version", "", "installer-selected release version")
		if flags.Parse(args[1:]) == nil {
			result, err = install(ctx, *control, *dry, *noninteractive && *confirm)
		} else {
			return 2
		}
	case "heartbeat":
		dry := flags.Bool("dry-run", false, "scan without models or state writes")
		if flags.Parse(args[1:]) == nil {
			heartbeatCtx, cancel := context.WithTimeout(ctx, heartbeatLimit)
			defer cancel()
			result, err = heartbeat(heartbeatCtx, *dry)
		} else {
			return 2
		}
	case "status":
		if flags.Parse(args[1:]) == nil {
			result, err = status()
		} else {
			return 2
		}
	case "self-test":
		flags.Bool("candidate", false, "validate this binary before installation")
		if flags.Parse(args[1:]) == nil {
			result, err = selfTest()
		} else {
			return 2
		}
	case "uninstall":
		noninteractive := flags.Bool("noninteractive", false, "run without prompts")
		confirm := flags.Bool("confirm", false, "confirm the previewed uninstall")
		if flags.Parse(args[1:]) == nil {
			result, err = uninstall(ctx, *noninteractive && *confirm)
		} else {
			return 2
		}
	case "version":
		if flags.Parse(args[1:]) == nil {
			if *asJSON {
				result = map[string]any{"version": version}
			} else {
				fmt.Fprintf(stdout, "ThreadBear %s\n", version)
				return 0
			}
		} else {
			return 2
		}
	case "title-plan":
		stage := flags.Bool("stage", false, "stage a guarded retained-task footer")
		batch := flags.Bool("batch", false, "list the next guarded operations")
		operation := flags.String("operation", "", "revalidate an operation")
		report := flags.Bool("report", false, "commit verified native outcomes")
		if flags.Parse(args[1:]) != nil {
			return 2
		}
		mode := ""
		switch {
		case *stage:
			mode = "stage"
		case *batch:
			mode = "batch"
		case *operation != "":
			mode = "operation"
		case *report:
			mode = "report"
		}
		result, err = titlePlan(ctx, mode, *operation, os.Stdin)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", command, helpText)
		return 2
	}
	if err != nil {
		if *asJSON {
			json.NewEncoder(stdout).Encode(map[string]any{"ready": false, "error": err.Error()})
		} else {
			fmt.Fprintln(stderr, "ThreadBear:", err)
		}
		return 1
	}
	if *asJSON {
		if json.NewEncoder(stdout).Encode(result) != nil {
			return 1
		}
	} else {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Fprintln(stdout, string(data))
	}
	return 0
}
