package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	case "build":
		return raw[1:], "build", nil
	case "run":
		return raw[1:], "run", nil
	case "plan":
		return raw[1:], "plan", nil
	case "doctor":
		return raw[1:], "doctor", nil
	case "destinations":
		return raw[1:], "destinations", nil
	case "xcrun":
		return raw[1:], "xcrun", nil
	case "xctest":
		return raw[1:], "xctest", nil
	}
	return raw, "build", nil
}

func printUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "xctide - wrapper around xcodebuild with TUI and machine-friendly modes")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "USAGE:")
	_, _ = fmt.Fprintln(w, "  xctide [flags] [-- <xcodebuild args>]")
	_, _ = fmt.Fprintln(w, "  xctide build [flags] [-- <xcodebuild args>]")
	_, _ = fmt.Fprintln(w, "  xctide run [flags] [-- <xcodebuild args>]")
	_, _ = fmt.Fprintln(w, "  xctide plan [flags] [-- <xcodebuild args>]")
	_, _ = fmt.Fprintln(w, "  xctide doctor [--json]")
	_, _ = fmt.Fprintln(w, "  xctide destinations [flags] [--json]")
	_, _ = fmt.Fprintln(w, "  xctide xcrun <args...>")
	_, _ = fmt.Fprintln(w, "  xctide xctest <args...>")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "FLAGS:")
	_, _ = fmt.Fprintln(w, "  -h, --help            Show this help")
	_, _ = fmt.Fprintln(w, "      --version         Print version and exit")
	_, _ = fmt.Fprintln(w, "      --scheme string")
	_, _ = fmt.Fprintln(w, "      --workspace string")
	_, _ = fmt.Fprintln(w, "      --project string")
	_, _ = fmt.Fprintln(w, "      --configuration string")
	_, _ = fmt.Fprintln(w, "      --destination string")
	_, _ = fmt.Fprintln(w, "      --platform string  Destination filter for `destinations`")
	_, _ = fmt.Fprintln(w, "      --simulator-only   Filter to simulator destinations (`destinations`)")
	_, _ = fmt.Fprintln(w, "      --device-only      Filter to physical device destinations (`destinations`)")
	_, _ = fmt.Fprintln(w, "      --progress string  Progress mode: auto|tui|plain|json|ndjson")
	_, _ = fmt.Fprintln(w, "      --result-bundle string")
	_, _ = fmt.Fprintln(w, "      --plain           Disable TUI and stream raw output")
	_, _ = fmt.Fprintln(w, "      --json            Emit JSON summary to stdout")
	_, _ = fmt.Fprintln(w, "      --quiet           Pass -quiet to xcodebuild")
	_, _ = fmt.Fprintln(w, "      --verbose         Print wrapper diagnostics to stderr")
	_, _ = fmt.Fprintln(w, "      --no-input        Never prompt for selection")
	_, _ = fmt.Fprintln(w, "      --no-color        Disable color output")
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
	_, _ = fmt.Fprintln(w, "  xctide xctest -h")
	_, _ = fmt.Fprintln(w, "  xctide --plain -- test")
	_, _ = fmt.Fprintln(w, "  xctide --progress plain -- test")
	_, _ = fmt.Fprintln(w, "  xctide --progress json -- test")
	_, _ = fmt.Fprintln(w, "  xctide --progress ndjson -- test")
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
	options = filterDestinations(options, cfg.platform, cfg.simulatorOnly, cfg.deviceOnly)
	if len(options) == 0 {
		return nil, errors.New("no destinations found")
	}
	return options, nil
}

func filterDestinations(options []destinationOption, platform string, simulatorOnly bool, deviceOnly bool) []destinationOption {
	normalizedPlatform := strings.TrimSpace(strings.ToLower(platform))
	out := make([]destinationOption, 0, len(options))
	for _, option := range options {
		lowerPlatform := strings.ToLower(strings.TrimSpace(option.Platform))
		if normalizedPlatform != "" && lowerPlatform != normalizedPlatform {
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

func renderDestinationsResult(w io.Writer, result destinationsResult) {
	fmt.Fprintln(w, "• Destinations")
	if result.Workspace != "" {
		fmt.Fprintf(w, "  Workspace %s\n", result.Workspace)
	} else if result.Project != "" {
		fmt.Fprintf(w, "  Project %s\n", result.Project)
	}
	fmt.Fprintf(w, "  Scheme %s\n", result.Scheme)
	fmt.Fprintln(w, "")
	for _, item := range result.Destinations {
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
