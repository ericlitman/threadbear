package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ericlitman/threadbear/internal/app"
	"github.com/ericlitman/threadbear/internal/output"
)

var version = "dev"

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	request, err := parseRequest(args)
	format := output.FormatHuman
	if request.JSON {
		format = output.FormatJSON
	}
	if err != nil {
		result := output.ErrorResult{Operation: "parse", ErrorCode: "invalid_arguments"}
		if errors.Is(err, app.ErrUnknownCommand) {
			result.Operation = "dispatch"
			result.ErrorCode = "unknown_command"
		}
		if writeErr := output.Write(stdout, format, result); writeErr != nil {
			fmt.Fprintf(stderr, "ThreadBear couldn't write its result: %v\n", writeErr)
			return 1
		}
		return 2
	}
	result, dispatchErr := app.New(version).Dispatch(ctx, request)
	if err := output.Write(stdout, format, result); err != nil {
		fmt.Fprintf(stderr, "ThreadBear couldn't write its result: %v\n", err)
		return 1
	}
	if dispatchErr != nil {
		return 1
	}
	return 0
}

func parseRequest(args []string) (app.Request, error) {
	if len(args) == 0 {
		return app.Request{}, fmt.Errorf("choose a command: install, heartbeat, status, inspect, configure, enable, disable, restore, self-test, update, uninstall, or version")
	}
	request := app.Request{Command: app.Command(args[0]), JSON: containsFlag(args[1:], "--json")}
	if !request.Command.Valid() {
		return request, fmt.Errorf("%w: %q", app.ErrUnknownCommand, request.Command)
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.BoolVar(&request.JSON, "json", request.JSON, "write stable JSON output")
	if request.Command == app.CommandHeartbeat {
		flags.BoolVar(&request.DryRun, "dry-run", false, "inventory and analyze without models or mutations")
	}
	commandArgs := args[1:]
	if request.Command == app.CommandInspect || request.Command == app.CommandRestore {
		commandArgs = flagsBeforePositionals(commandArgs, "--json")
	}
	if err := flags.Parse(commandArgs); err != nil {
		return request, err
	}
	remaining := flags.Args()
	switch request.Command {
	case app.CommandInspect, app.CommandRestore:
		if len(remaining) != 1 {
			return request, fmt.Errorf("%s requires exactly one task ID", request.Command)
		}
		request.TaskID = remaining[0]
	default:
		if len(remaining) != 0 {
			return request, fmt.Errorf("%s does not accept positional arguments", request.Command)
		}
	}
	if err := request.Validate(); err != nil {
		return request, err
	}
	return request, nil
}

func containsFlag(args []string, target string) bool {
	for _, arg := range args {
		if arg == target {
			return true
		}
	}
	return false
}

func flagsBeforePositionals(args []string, known ...string) []string {
	isKnown := func(value string) bool {
		for _, flagName := range known {
			if value == flagName {
				return true
			}
		}
		return false
	}
	ordered := make([]string, 0, len(args))
	for _, arg := range args {
		if isKnown(arg) {
			ordered = append(ordered, arg)
		}
	}
	for _, arg := range args {
		if !isKnown(arg) {
			ordered = append(ordered, arg)
		}
	}
	return ordered
}
