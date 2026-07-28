package main

import (
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/ericlitman/threadbear/internal/app"
)

type commandArgument struct {
	name        string
	description string
}

type commandSpec struct {
	command       app.Command
	synopsis      string
	arguments     []commandArgument
	registerFlags func(*flag.FlagSet, *app.Request)
}

var commandSpecs = []commandSpec{
	{
		command:  app.CommandInstall,
		synopsis: "Install ThreadBear and set up its managed runtime.",
		registerFlags: func(flags *flag.FlagSet, request *app.Request) {
			registerConfigureFlags(flags, &request.Configure)
			registerLifecycleFlags(flags, request)
			flags.StringVar(&request.ControlTaskID, "control-task-id", "", "adopt existing readable unarchived Codex task `TASK_ID`")
			flags.BoolVar(&request.DryRun, "dry-run", false, "validate and print the complete install preview without mutation")
			flags.StringVar(&request.Version, "version", "", "install exact release `N.N.N` without a leading v instead of the latest")
		},
	},
	{
		command:  app.CommandHeartbeat,
		synopsis: "Classify changed tasks and apply lifecycle work.",
		registerFlags: func(flags *flag.FlagSet, request *app.Request) {
			flags.BoolVar(&request.DryRun, "dry-run", false, "inventory and analyze without models or mutations")
		},
	},
	{
		command:       app.CommandStatus,
		synopsis:      "Show ThreadBear health, preferences, and recent activity.",
		registerFlags: noCommandFlags,
	},
	{
		command:  app.CommandInspect,
		synopsis: "Explain ThreadBear's saved state for one task.",
		arguments: []commandArgument{
			{name: "TASK_ID", description: "Codex task ID to inspect"},
		},
		registerFlags: noCommandFlags,
	},
	{
		command:  app.CommandConfigure,
		synopsis: "Preview or change ThreadBear preferences.",
		registerFlags: func(flags *flag.FlagSet, request *app.Request) {
			registerConfigureFlags(flags, &request.Configure)
			registerNonInteractiveFlags(flags, request)
			flags.BoolVar(&request.DryRun, "dry-run", false, "preview configuration effects")
			flags.BoolVar(&request.Confirm, "confirm", false, "confirm the previewed changes")
		},
	},
	{
		command:       app.CommandEnable,
		synopsis:      "Enable the ThreadBear LaunchAgent.",
		registerFlags: noCommandFlags,
	},
	{
		command:       app.CommandDisable,
		synopsis:      "Disable the ThreadBear LaunchAgent.",
		registerFlags: noCommandFlags,
	},
	{
		command:  app.CommandRestore,
		synopsis: "Restore one task archived by ThreadBear.",
		arguments: []commandArgument{
			{name: "TASK_ID", description: "Codex task ID to restore"},
		},
		registerFlags: noCommandFlags,
	},
	{
		command:  app.CommandSelfTest,
		synopsis: "Check that ThreadBear's runtime surfaces are ready.",
		registerFlags: func(flags *flag.FlagSet, request *app.Request) {
			flags.BoolVar(&request.Candidate, "candidate", false, "validate this candidate before installation")
		},
	},
	{
		command:  app.CommandUpdate,
		synopsis: "Install the latest or an exact verified release.",
		registerFlags: func(flags *flag.FlagSet, request *app.Request) {
			flags.StringVar(&request.Version, "version", "", "install exact release `N.N.N` without a leading v instead of the latest")
		},
	},
	{
		command:  app.CommandUninstall,
		synopsis: "Preview and remove ThreadBear's managed runtime.",
		registerFlags: func(flags *flag.FlagSet, request *app.Request) {
			registerLifecycleFlags(flags, request)
			flags.BoolVar(&request.ArchiveControlTask, "archive-control-task", false, "archive the control task")
			flags.Bool("delete-state", false, "deprecated no-op; persistent state is always deleted")
		},
	},
	{
		command:       app.CommandVersion,
		synopsis:      "Show the installed ThreadBear version and website.",
		registerFlags: noCommandFlags,
	},
}

func noCommandFlags(*flag.FlagSet, *app.Request) {}

func commandSpecFor(command app.Command) (commandSpec, bool) {
	for _, spec := range commandSpecs {
		if spec.command == command {
			return spec, true
		}
	}
	return commandSpec{}, false
}

func newCommandFlagSet(spec commandSpec, request *app.Request) *flag.FlagSet {
	flags := flag.NewFlagSet(string(spec.command), flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.BoolVar(&request.JSON, "json", request.JSON, "write stable JSON command results; help stays human-readable")
	spec.registerFlags(flags, request)
	return flags
}

func requestedHelp(args []string) (string, int, bool) {
	if len(args) == 0 {
		return renderTopLevelHelp(), 2, true
	}
	if len(args) == 1 && isHelpFlag(args[0]) {
		return renderTopLevelHelp(), 0, true
	}
	if args[0] == "help" {
		if len(args) == 1 {
			return renderTopLevelHelp(), 0, true
		}
		if len(args) == 2 {
			if spec, ok := commandSpecFor(app.Command(args[1])); ok {
				return renderCommandHelp(spec), 0, true
			}
			return unknownCommandMessage(app.Command(args[1])), 2, true
		}
		return "Usage: threadbear help [command]\n", 2, true
	}
	return "", 0, false
}

func isHelpFlag(arg string) bool {
	return arg == "-h" || arg == "--help"
}

func renderTopLevelHelp() string {
	var output strings.Builder
	output.WriteString("ThreadBear keeps Codex tasks tidy without wasting model tokens.\n\n")
	output.WriteString("Usage:\n")
	output.WriteString("  threadbear <command> [flags]\n")
	output.WriteString("  threadbear help [command]\n\n")
	output.WriteString("Commands:\n")
	width := 0
	for _, spec := range commandSpecs {
		if len(spec.command) > width {
			width = len(spec.command)
		}
	}
	for _, spec := range commandSpecs {
		fmt.Fprintf(&output, "  %-*s  %s\n", width, spec.command, spec.synopsis)
	}
	output.WriteString("\nRun `threadbear help <command>` for command details.\n")
	return output.String()
}

func renderCommandHelp(spec commandSpec) string {
	var output strings.Builder
	fmt.Fprintf(&output, "ThreadBear %s — %s\n\n", spec.command, spec.synopsis)
	output.WriteString("Usage:\n  threadbear ")
	output.WriteString(string(spec.command))
	output.WriteString(" [flags]")
	for _, argument := range spec.arguments {
		output.WriteByte(' ')
		output.WriteString(argument.name)
	}
	output.WriteString("\n")
	if len(spec.arguments) > 0 {
		output.WriteString("\nArguments:\n")
		rows := make([]helpRow, 0, len(spec.arguments))
		for _, argument := range spec.arguments {
			rows = append(rows, helpRow{label: argument.name, description: argument.description})
		}
		writeHelpRows(&output, rows)
	}
	output.WriteString("\nFlags:\n")
	rows := []helpRow{{label: "-h, --help", description: "show this help and let the bear rest"}}
	request := app.Request{Command: spec.command}
	flags := newCommandFlagSet(spec, &request)
	flags.VisitAll(func(current *flag.Flag) {
		rows = append(rows, commandFlagHelp(current))
	})
	writeHelpRows(&output, rows)
	return output.String()
}

type helpRow struct {
	label       string
	description string
}

func commandFlagHelp(current *flag.Flag) helpRow {
	valueHint, description := flag.UnquoteUsage(current)
	label := "--" + current.Name
	if _, ok := current.Value.(optionalBool); ok {
		label += "[=true|false]"
	} else if valueHint != "" {
		label += " " + valueHint
	}
	defaultValue := current.DefValue
	if _, ok := current.Value.(optionalBool); ok {
		defaultValue = "unchanged"
	} else if defaultValue == "" {
		defaultValue = `""`
	} else if _, err := strconv.ParseBool(defaultValue); err != nil {
		defaultValue = strconv.Quote(defaultValue)
	}
	description += " (default: " + defaultValue + ")"
	return helpRow{label: label, description: description}
}

func writeHelpRows(output *strings.Builder, rows []helpRow) {
	width := 0
	for _, row := range rows {
		if len(row.label) > width {
			width = len(row.label)
		}
	}
	for _, row := range rows {
		fmt.Fprintf(output, "  %-*s  %s\n", width, row.label, row.description)
	}
}

func unknownCommandMessage(command app.Command) string {
	return fmt.Sprintf("ThreadBear doesn't know command %q. Try `threadbear help`.\n", command)
}
