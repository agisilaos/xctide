package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func normalizeArgs(raw []string) ([]string, string, error) {
	if len(raw) == 0 {
		return raw, "build", nil
	}
	switch raw[0] {
	case "help":
		printUsage(os.Stdout)
		os.Exit(exitOK)
	}
	if mode, ok := resolveCommandMode(raw[0]); ok {
		return raw[1:], mode, nil
	}
	return raw, "build", nil
}

func printUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "xctide - wrapper around xcodebuild with TUI and machine-friendly modes")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "USAGE:")
	for _, line := range usageCommandLines() {
		_, _ = fmt.Fprintln(w, line)
	}
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "FLAGS:")
	for _, line := range usageFlagLines() {
		_, _ = fmt.Fprintln(w, line)
	}
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "ENV:")
	_, _ = fmt.Fprintln(w, "  XCTIDE_SCHEME, XCTIDE_WORKSPACE, XCTIDE_PROJECT, XCTIDE_CONFIGURATION, XCTIDE_DESTINATION, XCTIDE_PROGRESS")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "EXAMPLES:")
	_, _ = fmt.Fprintln(w, "  xctide")
	_, _ = fmt.Fprintln(w, "  xctide build --scheme Subsmind --destination \"platform=iOS Simulator,name=iPhone 16\"")
	_, _ = fmt.Fprintln(w, "  xctide run --scheme Subsmind --destination \"platform=iOS Simulator,id=<UDID>\"")
	_, _ = fmt.Fprintln(w, "  xctide plan --scheme Subsmind -- test")
	_, _ = fmt.Fprintln(w, "  xctide doctor")
	_, _ = fmt.Fprintln(w, "  xctide destinations --scheme Subsmind")
	_, _ = fmt.Fprintln(w, "  xctide xcrun simctl list devices available")
	_, _ = fmt.Fprintln(w, "  xctide xcrun xctrace list templates")
	_, _ = fmt.Fprintln(w, "  xctide xctest -h")
	_, _ = fmt.Fprintln(w, "  xctide completion zsh > ~/.zsh/completions/_xctide")
	_, _ = fmt.Fprintln(w, "  xctide --plain -- test")
	_, _ = fmt.Fprintln(w, "  xctide --progress plain -- test")
	_, _ = fmt.Fprintln(w, "  xctide --progress json -- test")
	_, _ = fmt.Fprintln(w, "  xctide --progress ndjson -- test")
}

func printCompletionUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "xctide completion - generate shell completion scripts")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "USAGE:")
	_, _ = fmt.Fprintln(w, "  xctide completion <bash|zsh|fish>")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "EXAMPLES:")
	_, _ = fmt.Fprintln(w, "  xctide completion bash > /usr/local/etc/bash_completion.d/xctide")
	_, _ = fmt.Fprintln(w, "  xctide completion zsh > ~/.zsh/completions/_xctide")
	_, _ = fmt.Fprintln(w, "  xctide completion fish > ~/.config/fish/completions/xctide.fish")
}

func runCompletion(args []string) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		printCompletionUsage(os.Stdout)
		return exitOK
	}
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "xctide: completion accepts exactly one shell argument: bash|zsh|fish")
		return exitInvalidUsage
	}
	script, err := completionScript(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "xctide:", err)
		return exitInvalidUsage
	}
	fmt.Fprint(os.Stdout, script)
	return exitOK
}

func completionScript(shell string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(shell)) {
	case "bash":
		return fmt.Sprintf(`# bash completion for xctide
_xctide_completions() {
  local cur prev
  COMPREPLY=()
  cur="${COMP_WORDS[COMP_CWORD]}"
  prev="${COMP_WORDS[COMP_CWORD-1]}"

  if [[ $COMP_CWORD -eq 1 ]]; then
    COMPREPLY=( $(compgen -W "%s" -- "$cur") )
    return 0
  fi

  if [[ "${COMP_WORDS[1]}" == "completion" ]]; then
    COMPREPLY=( $(compgen -W "%s" -- "$cur") )
    return 0
  fi

  COMPREPLY=( $(compgen -W "%s" -- "$cur") )
}
complete -F _xctide_completions xctide
`, completionCommandWords(), completionShellWords(), completionFlagWords()), nil
	case "zsh":
		return fmt.Sprintf(`#compdef xctide
_xctide() {
  local -a commands
  commands=(
%s
  )

  if (( CURRENT == 2 )); then
    _describe 'command' commands
    return
  fi

  if [[ ${words[2]} == completion ]]; then
    _values 'shell' %s
    return
  fi

  _arguments \
    '--scheme[Build scheme name]:scheme:' \
    '--workspace[Path to .xcworkspace]:workspace:_files' \
    '--project[Path to .xcodeproj]:project:_files' \
    '--configuration[Build configuration]:configuration:' \
    '--destination[Build destination]:destination:' \
    '--platform[Destination filter for destinations command]:platform:' \
    '--name[Destination name contains filter for destinations]:name:' \
    '--os[Destination OS contains filter for destinations]:os:' \
    '--limit[Max destinations to return]:limit:' \
    '--latest[Keep latest OS per destination name]' \
    '--simulator-only[Only simulator destinations]' \
    '--device-only[Only physical device destinations]' \
    '--progress[Progress mode]:progress:(auto tui plain json ndjson)' \
    '--result-bundle[Path to write result bundle]:result-bundle:_files' \
    '--details[Expanded plain output sections]' \
    '--plain[Disable TUI and stream raw output]' \
    '--json[Emit JSON summary]' \
    '--quiet[Pass -quiet to xcodebuild]' \
    '--verbose[Print wrapper diagnostics]' \
    '--no-input[Never prompt for selection]' \
    '--no-color[Disable color output]' \
    '--version[Print version and exit]'
}
_xctide "$@"
`, zshCommandSpecs(), completionShellWords()), nil
	case "fish":
		return fmt.Sprintf(`# fish completion for xctide
%s
%s
`, fishCommandCompletionLines(), fishFlagCompletionLines()), nil
	default:
		return "", fmt.Errorf("unsupported shell %q (expected bash|zsh|fish)", shell)
	}
}

func listDestinations(cfg buildConfig) ([]destinationOption, error) {
	args := []string{}
	if cfg.workspacePath != "" {
		args = append(args, "-workspace", cfg.workspacePath)
	} else if cfg.projectPath != "" {
		args = append(args, "-project", cfg.projectPath)
	} else {
		return nil, errors.New("missing project or workspace for destinations")
	}
	args = append(args, "-scheme", cfg.scheme, "-showdestinations")
	cmd := exec.Command("xcodebuild", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("xcodebuild -showdestinations failed: %w", err)
	}
	options := parseShowDestinationsOutput(out)
	options = filterDestinations(options, cfg.platform, cfg.destName, cfg.destOS, cfg.simulatorOnly, cfg.deviceOnly, cfg.destLatest)
	options = limitDestinations(options, cfg.destLimit)
	if len(options) == 0 {
		return nil, errors.New("no destinations found")
	}
	return options, nil
}

func filterDestinations(options []destinationOption, platform string, nameFilter string, osFilter string, simulatorOnly bool, deviceOnly bool, latest bool) []destinationOption {
	normalizedPlatform := strings.TrimSpace(strings.ToLower(platform))
	normalizedName := strings.TrimSpace(strings.ToLower(nameFilter))
	normalizedOS := strings.TrimSpace(strings.ToLower(osFilter))
	out := make([]destinationOption, 0, len(options))
	for _, option := range options {
		lowerPlatform := strings.ToLower(strings.TrimSpace(option.Platform))
		if normalizedPlatform != "" && lowerPlatform != normalizedPlatform {
			continue
		}
		lowerName := strings.ToLower(strings.TrimSpace(option.Name))
		if normalizedName != "" && !strings.Contains(lowerName, normalizedName) {
			continue
		}
		lowerOS := strings.ToLower(strings.TrimSpace(option.OS))
		if normalizedOS != "" && !strings.Contains(lowerOS, normalizedOS) {
			continue
		}
		isSimulator := strings.Contains(lowerPlatform, "simulator")
		if simulatorOnly && !isSimulator {
			continue
		}
		if deviceOnly && isSimulator {
			continue
		}
		out = append(out, option)
	}
	if latest {
		out = latestDestinationsByName(out)
	}
	return out
}

func latestDestinationsByName(options []destinationOption) []destinationOption {
	type keyedOption struct {
		option destinationOption
		key    string
	}
	byName := make(map[string]keyedOption, len(options))
	order := make([]string, 0, len(options))
	for _, option := range options {
		key := strings.ToLower(strings.TrimSpace(option.Platform + "|" + option.Name))
		current := keyedOption{option: option, key: key}
		prev, exists := byName[key]
		if !exists {
			byName[key] = current
			order = append(order, key)
			continue
		}
		if destinationOSSortValue(option.OS) > destinationOSSortValue(prev.option.OS) {
			byName[key] = current
		}
	}
	out := make([]destinationOption, 0, len(byName))
	for _, key := range order {
		out = append(out, byName[key].option)
	}
	return out
}

func destinationOSSortValue(raw string) int {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0
	}
	parts := strings.Split(value, ".")
	sum := 0
	factor := 1000000
	for i := 0; i < len(parts) && i < 3; i++ {
		num, err := strconv.Atoi(parts[i])
		if err != nil {
			return 0
		}
		sum += num * factor
		factor /= 100
	}
	return sum
}

func limitDestinations(options []destinationOption, limit int) []destinationOption {
	if limit <= 0 || len(options) <= limit {
		return options
	}
	out := make([]destinationOption, 0, limit)
	out = append(out, options[:limit]...)
	return out
}

func parseShowDestinationsOutput(data []byte) []destinationOption {
	lines := strings.Split(string(data), "\n")
	out := make([]destinationOption, 0, len(lines))
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "{") || !strings.HasSuffix(line, "}") {
			continue
		}
		option, ok := parseDestinationDictLine(line)
		if !ok {
			continue
		}
		out = append(out, option)
	}
	return out
}

func parseDestinationDictLine(line string) (destinationOption, bool) {
	body := strings.TrimSpace(line)
	body = strings.TrimPrefix(body, "{")
	body = strings.TrimSuffix(body, "}")
	body = strings.TrimSpace(body)
	if body == "" {
		return destinationOption{}, false
	}
	option := destinationOption{}
	for _, part := range strings.Split(body, ",") {
		part = strings.TrimSpace(part)
		kv := strings.SplitN(part, ":", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])
		switch key {
		case "platform":
			option.Platform = value
		case "arch":
			option.Arch = value
		case "id":
			option.ID = value
		case "OS":
			option.OS = value
		case "name":
			option.Name = value
		}
	}
	specParts := make([]string, 0, 3)
	if option.Platform != "" {
		specParts = append(specParts, "platform="+option.Platform)
	}
	if option.ID != "" {
		specParts = append(specParts, "id="+option.ID)
	}
	if option.Name != "" {
		specParts = append(specParts, "name="+option.Name)
	}
	option.Spec = strings.Join(specParts, ",")
	return option, option.Spec != ""
}

func renderDestinationsResult(w io.Writer, result destinationsResult, cfg buildConfig) {
	fmt.Fprintln(w, "• Destinations")
	if result.Workspace != "" {
		fmt.Fprintf(w, "  Workspace %s\n", result.Workspace)
	} else if result.Project != "" {
		fmt.Fprintf(w, "  Project %s\n", result.Project)
	}
	fmt.Fprintf(w, "  Scheme %s\n", result.Scheme)
	if cfg.platform != "" || cfg.destName != "" || cfg.destOS != "" || cfg.simulatorOnly || cfg.deviceOnly || cfg.destLatest {
		parts := make([]string, 0, 6)
		if cfg.platform != "" {
			parts = append(parts, "platform="+cfg.platform)
		}
		if cfg.destName != "" {
			parts = append(parts, "name~"+cfg.destName)
		}
		if cfg.destOS != "" {
			parts = append(parts, "os~"+cfg.destOS)
		}
		if cfg.simulatorOnly {
			parts = append(parts, "simulator-only")
		}
		if cfg.deviceOnly {
			parts = append(parts, "device-only")
		}
		if cfg.destLatest {
			parts = append(parts, "latest")
		}
		fmt.Fprintf(w, "  Filters %s\n", strings.Join(parts, ", "))
	}
	fmt.Fprintln(w, "")
	items := result.Destinations
	displayLimit := cfg.destLimit
	if displayLimit <= 0 {
		displayLimit = 25
	}
	truncated := false
	if len(items) > displayLimit {
		items = items[:displayLimit]
		truncated = true
	}
	for _, item := range items {
		label := strings.TrimSpace(strings.Join([]string{item.Platform, item.Name}, " "))
		if item.OS != "" {
			fmt.Fprintf(w, "  - %s (iOS %s)\n", label, item.OS)
		} else {
			fmt.Fprintf(w, "  - %s\n", label)
		}
		if item.Spec != "" {
			fmt.Fprintf(w, "    %s\n", item.Spec)
		}
	}
	if truncated {
		fmt.Fprintln(w, "")
		fmt.Fprintf(w, "  ... %d more destination(s). Use --limit to control output or --json for full list.\n", len(result.Destinations)-len(items))
	}
}

func passthroughSpec(mode string, args []string) (string, []string, error) {
	cleanArgs := args
	if len(cleanArgs) > 0 && cleanArgs[0] == "--" {
		cleanArgs = cleanArgs[1:]
	}
	switch mode {
	case "xcrun":
		if len(cleanArgs) == 0 {
			return "", nil, errors.New("xcrun requires arguments (example: xctide xcrun simctl list devices)")
		}
		return "xcrun", cleanArgs, nil
	case "xctest":
		if len(cleanArgs) == 0 {
			return "", nil, errors.New("xctest requires a test bundle path (use `xctide xctest --help` for examples)")
		}
		return "xcrun", append([]string{"xctest"}, cleanArgs...), nil
	default:
		return "", nil, fmt.Errorf("unsupported passthrough mode %q", mode)
	}
}

func wantsXctestHelp(args []string) bool {
	cleanArgs := args
	if len(cleanArgs) > 0 && cleanArgs[0] == "--" {
		cleanArgs = cleanArgs[1:]
	}
	if len(cleanArgs) == 0 {
		return true
	}
	if cleanArgs[0] == "-h" || cleanArgs[0] == "--help" || cleanArgs[0] == "help" {
		return true
	}
	return false
}

func printXctestPassthroughHelp(w io.Writer) {
	_, _ = fmt.Fprintln(w, "xctide xctest - passthrough to `xcrun xctest`")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "USAGE:")
	_, _ = fmt.Fprintln(w, "  xctide xctest [xctest args...] <path/to/Tests.xctest>")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "EXAMPLES:")
	_, _ = fmt.Fprintln(w, "  xctide xctest /path/to/YourTests.xctest")
	_, _ = fmt.Fprintln(w, "  xctide xctest -XCTest MySuite/testExample /path/to/YourTests.xctest")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "NOTE:")
	_, _ = fmt.Fprintln(w, "  This command forwards arguments to `xcrun xctest` unchanged.")
}

func runPassthrough(name string, args []string) int {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				return status.ExitStatus()
			}
		}
		return exitRuntimeFailure
	}
	return exitOK
}

func shouldPrintWrapperError(mode string, err error) bool {
	if err == nil {
		return false
	}
	if mode == "plain" {
		return false
	}
	return true
}

func buildPlanResult(cfg buildConfig, mode string) planResult {
	args := buildArgs(cfg)
	return planResult{
		Mode:          mode,
		Project:       cfg.projectPath,
		Workspace:     cfg.workspacePath,
		Scheme:        cfg.scheme,
		Configuration: cfg.configuration,
		Destination:   cfg.destination,
		RunAfterBuild: cfg.runAfterBuild,
		XcodebuildCmd: append([]string{"xcodebuild"}, args...),
	}
}

func renderPlanResult(w io.Writer, result planResult) {
	fmt.Fprintln(w, "• Resolved Configuration")
	if result.Workspace != "" {
		fmt.Fprintf(w, "  Workspace %s\n", result.Workspace)
	} else if result.Project != "" {
		fmt.Fprintf(w, "  Project %s\n", result.Project)
	}
	fmt.Fprintf(w, "  Scheme %s\n", result.Scheme)
	fmt.Fprintf(w, "  Configuration %s\n", result.Configuration)
	if result.Destination != "" {
		fmt.Fprintf(w, "  Destination %s\n", result.Destination)
	}
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "• Command Preview")
	fmt.Fprintf(w, "  %s\n", strings.Join(result.XcodebuildCmd, " "))
	if result.RunAfterBuild {
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "• Post Build")
		fmt.Fprintln(w, "  run_after_build enabled (simulator boot/install/launch)")
	}
}

func runDoctor(cfg buildConfig) doctorResult {
	checks := []doctorCheck{
		checkXcodebuild(),
		checkXcrun(),
		checkSimulators(),
		checkProjectContext(cfg),
	}
	success := true
	for _, check := range checks {
		if check.Status == "fail" {
			success = false
			break
		}
	}
	return doctorResult{Success: success, Checks: checks}
}

func renderDoctorResult(w io.Writer, result doctorResult) {
	fmt.Fprintln(w, "• Doctor")
	for _, check := range result.Checks {
		icon := "✓"
		if check.Status == "warn" {
			icon = "!"
		}
		if check.Status == "fail" {
			icon = "x"
		}
		fmt.Fprintf(w, "  %s %s: %s\n", icon, check.Name, check.Details)
		if check.Hint != "" {
			fmt.Fprintf(w, "    hint: %s\n", check.Hint)
		}
	}
	if result.Success {
		fmt.Fprintln(w, "\n• Doctor Passed")
	} else {
		fmt.Fprintln(w, "\n• Doctor Found Issues")
	}
}

func checkXcodebuild() doctorCheck {
	path, err := exec.LookPath("xcodebuild")
	if err != nil {
		return doctorCheck{Name: "xcodebuild", Status: "fail", Details: "xcodebuild not found in PATH", Hint: "Install Xcode and run xcode-select --switch /Applications/Xcode.app"}
	}
	out, err := exec.Command("xcodebuild", "-version").CombinedOutput()
	if err != nil {
		return doctorCheck{Name: "xcodebuild", Status: "fail", Details: fmt.Sprintf("xcodebuild present at %s but -version failed", path), Hint: strings.TrimSpace(string(out))}
	}
	first := strings.Split(strings.TrimSpace(string(out)), "\n")[0]
	return doctorCheck{Name: "xcodebuild", Status: "pass", Details: fmt.Sprintf("%s (%s)", first, path)}
}

func checkXcrun() doctorCheck {
	path, err := exec.LookPath("xcrun")
	if err != nil {
		return doctorCheck{Name: "xcrun", Status: "fail", Details: "xcrun not found in PATH", Hint: "Install Command Line Tools: xcode-select --install"}
	}
	out, err := exec.Command("xcrun", "--version").CombinedOutput()
	if err != nil {
		return doctorCheck{Name: "xcrun", Status: "fail", Details: fmt.Sprintf("xcrun present at %s but --version failed", path), Hint: strings.TrimSpace(string(out))}
	}
	first := strings.Split(strings.TrimSpace(string(out)), "\n")[0]
	return doctorCheck{Name: "xcrun", Status: "pass", Details: fmt.Sprintf("%s (%s)", first, path)}
}

func checkSimulators() doctorCheck {
	out, err := exec.Command("xcrun", "simctl", "list", "devices", "available", "-j").CombinedOutput()
	if err != nil {
		return doctorCheck{Name: "simulators", Status: "fail", Details: "unable to query simulators", Hint: strings.TrimSpace(string(out))}
	}
	count, err := parseAvailableSimulatorCount(out)
	if err != nil {
		return doctorCheck{Name: "simulators", Status: "fail", Details: "unable to parse simulator list", Hint: err.Error()}
	}
	if count == 0 {
		return doctorCheck{Name: "simulators", Status: "warn", Details: "no available simulators found", Hint: "Open Xcode > Settings > Platforms and install at least one iOS simulator runtime"}
	}
	return doctorCheck{Name: "simulators", Status: "pass", Details: fmt.Sprintf("%d available simulator(s)", count)}
}

func checkProjectContext(cfg buildConfig) doctorCheck {
	if cfg.workspacePath != "" || cfg.projectPath != "" {
		return doctorCheck{Name: "project_context", Status: "pass", Details: "project/workspace provided via flags or env"}
	}
	workspaces, projects := findXcodeContainers(".")
	if len(workspaces) == 0 && len(projects) == 0 {
		return doctorCheck{Name: "project_context", Status: "warn", Details: "no .xcworkspace or .xcodeproj found in current directory", Hint: "Run doctor from your project root or pass --workspace/--project when building"}
	}
	return doctorCheck{Name: "project_context", Status: "pass", Details: fmt.Sprintf("found %d workspace(s), %d project(s)", len(workspaces), len(projects))}
}

func parseAvailableSimulatorCount(data []byte) (int, error) {
	var payload struct {
		Devices map[string][]struct {
			IsAvailable bool `json:"isAvailable"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return 0, err
	}
	count := 0
	for _, devices := range payload.Devices {
		for _, device := range devices {
			if device.IsAvailable {
				count++
			}
		}
	}
	return count, nil
}
