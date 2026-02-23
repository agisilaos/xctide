package main

import (
	"fmt"
	"strings"
)

type cliCommand struct {
	Name        string
	Mode        string
	UsageSuffix string
	Summary     string
}

type cliFlag struct {
	Long        string
	Value       string
	Description string
}

var cliCommands = []cliCommand{
	{Name: "build", Mode: "build", UsageSuffix: " [flags] [-- <xcodebuild args>]", Summary: "run xcodebuild wrapper"},
	{Name: "run", Mode: "run", UsageSuffix: " [flags] [-- <xcodebuild args>]", Summary: "build and launch app on simulator"},
	{Name: "diagnose", Mode: "diagnose_build", UsageSuffix: " build [flags] [-- <xcodebuild args>]", Summary: "preflight doctor + plan for build"},
	{Name: "plan", Mode: "plan", UsageSuffix: " [flags] [-- <xcodebuild args>]", Summary: "show resolved xcodebuild command"},
	{Name: "doctor", Mode: "doctor", UsageSuffix: " [--json]", Summary: "validate local environment"},
	{Name: "destinations", Mode: "destinations", UsageSuffix: " [flags] [--json]", Summary: "list available destinations"},
	{Name: "xcrun", Mode: "xcrun", UsageSuffix: " <args...>", Summary: "passthrough to xcrun"},
	{Name: "xctest", Mode: "xctest", UsageSuffix: " <args...>", Summary: "passthrough to xcrun xctest"},
	{Name: "completion", Mode: "completion", UsageSuffix: " <bash|zsh|fish>", Summary: "generate shell completions"},
}

var cliFlags = []cliFlag{
	{Long: "scheme", Value: "string", Description: ""},
	{Long: "workspace", Value: "string", Description: ""},
	{Long: "project", Value: "string", Description: ""},
	{Long: "configuration", Value: "string", Description: ""},
	{Long: "destination", Value: "string", Description: ""},
	{Long: "platform", Value: "string", Description: "Destination filter for `destinations`"},
	{Long: "name", Value: "string", Description: "Destination name contains filter for `destinations`"},
	{Long: "os", Value: "string", Description: "Destination OS contains filter for `destinations`"},
	{Long: "limit", Value: "int", Description: "Max destinations to return (`destinations`)"},
	{Long: "latest", Description: "Keep latest OS per destination name (`destinations`)"},
	{Long: "simulator-only", Description: "Filter to simulator destinations (`destinations`)"},
	{Long: "device-only", Description: "Filter to physical device destinations (`destinations`)"},
	{Long: "progress", Value: "string", Description: "Progress mode: auto|tui|plain|json|ndjson"},
	{Long: "result-bundle", Value: "string", Description: ""},
	{Long: "details", Description: "Expanded plain output sections"},
	{Long: "plain", Description: "Disable TUI and stream raw output"},
	{Long: "json", Description: "Emit JSON summary to stdout"},
	{Long: "quiet", Description: "Pass -quiet to xcodebuild"},
	{Long: "verbose", Description: "Print wrapper diagnostics to stderr"},
	{Long: "no-input", Description: "Never prompt for selection"},
	{Long: "no-color", Description: "Disable color output"},
	{Long: "version", Description: "Print version and exit"},
}

var completionShells = []string{"bash", "zsh", "fish"}

func resolveCommandMode(token string) (string, bool) {
	normalized := strings.TrimSpace(strings.ToLower(token))
	for _, cmd := range cliCommands {
		if cmd.Name == normalized {
			return cmd.Mode, true
		}
	}
	return "", false
}

func usageCommandLines() []string {
	lines := []string{"  xctide [flags] [-- <xcodebuild args>]"}
	for _, cmd := range cliCommands {
		lines = append(lines, fmt.Sprintf("  xctide %s%s", cmd.Name, cmd.UsageSuffix))
	}
	return lines
}

func usageFlagLines() []string {
	lines := []string{
		"  -h, --help            Show this help",
	}
	for _, flag := range cliFlags {
		head := fmt.Sprintf("      --%s", flag.Long)
		if flag.Value != "" {
			head = fmt.Sprintf("%s %s", head, flag.Value)
		}
		if flag.Description == "" {
			lines = append(lines, head)
			continue
		}
		lines = append(lines, padRight(head, 24)+flag.Description)
	}
	return lines
}

func completionCommandWords() string {
	words := make([]string, 0, len(cliCommands)+1)
	for _, cmd := range cliCommands {
		words = append(words, cmd.Name)
	}
	words = append(words, "help")
	return strings.Join(words, " ")
}

func completionFlagWords() string {
	words := make([]string, 0, len(cliFlags))
	for _, flag := range cliFlags {
		words = append(words, "--"+flag.Long)
	}
	return strings.Join(words, " ")
}

func zshCommandSpecs() string {
	parts := make([]string, 0, len(cliCommands)+1)
	for _, cmd := range cliCommands {
		parts = append(parts, fmt.Sprintf("    '%s:%s'", cmd.Name, cmd.Summary))
	}
	parts = append(parts, "    'help:show usage'")
	return strings.Join(parts, "\n")
}

func completionShellWords() string {
	return strings.Join(completionShells, " ")
}

func fishCommandCompletionLines() string {
	lines := make([]string, 0, len(cliCommands)+1)
	for _, cmd := range cliCommands {
		lines = append(lines, fmt.Sprintf("complete -c xctide -f -n '__fish_use_subcommand' -a %s -d '%s'", cmd.Name, cmd.Summary))
	}
	lines = append(lines, fmt.Sprintf("complete -c xctide -f -n '__fish_seen_subcommand_from completion' -a '%s'", completionShellWords()))
	return strings.Join(lines, "\n")
}

func fishFlagCompletionLines() string {
	lines := make([]string, 0, len(cliFlags))
	for _, flag := range cliFlags {
		parts := []string{fmt.Sprintf("complete -c xctide -l %s", flag.Long)}
		if flag.Value != "" {
			parts = append(parts, "-r")
		}
		if flag.Long == "progress" {
			parts = append(parts, "-a 'auto tui plain json ndjson'")
		}
		if flag.Description != "" {
			parts = append(parts, fmt.Sprintf("-d '%s'", flag.Description))
		}
		lines = append(lines, strings.Join(parts, " "))
	}
	return strings.Join(lines, "\n")
}

func padRight(value string, width int) string {
	if len(value) >= width {
		return value + " "
	}
	return value + strings.Repeat(" ", width-len(value))
}
