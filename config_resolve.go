package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/term"
)

type xcodebuildList struct {
	Project struct {
		Schemes []string `json:"schemes"`
	} `json:"project"`
	Workspace struct {
		Schemes []string `json:"schemes"`
	} `json:"workspace"`
}

func visitedFlags(flagSet *flag.FlagSet) map[string]bool {
	seen := make(map[string]bool)
	flagSet.Visit(func(f *flag.Flag) {
		seen[f.Name] = true
	})
	return seen
}

func applyEnvDefaults(cfg *buildConfig, seen map[string]bool) {
	if !seen["scheme"] {
		cfg.scheme = firstNonEmpty(cfg.scheme, os.Getenv("XCTIDE_SCHEME"))
	}
	if !seen["workspace"] {
		cfg.workspacePath = firstNonEmpty(cfg.workspacePath, os.Getenv("XCTIDE_WORKSPACE"))
	}
	if !seen["project"] {
		cfg.projectPath = firstNonEmpty(cfg.projectPath, os.Getenv("XCTIDE_PROJECT"))
	}
	if !seen["configuration"] {
		cfg.configuration = firstNonEmpty(cfg.configuration, os.Getenv("XCTIDE_CONFIGURATION"))
	}
	if !seen["destination"] {
		cfg.destination = firstNonEmpty(cfg.destination, os.Getenv("XCTIDE_DESTINATION"))
	}
	if !seen["progress"] {
		cfg.progress = firstNonEmpty(cfg.progress, os.Getenv("XCTIDE_PROGRESS"))
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func resolveProgressMode(cfg buildConfig, seen map[string]bool, hasTTY bool) (string, error) {
	if seen["progress"] && (seen["plain"] || seen["json"]) {
		return "", errors.New("use either --progress or --plain/--json, not both")
	}
	if seen["progress"] {
		switch cfg.progress {
		case "auto":
			if hasTTY {
				return "tui", nil
			}
			return "plain", nil
		case "tui":
			if !hasTTY {
				return "", errors.New("--progress=tui requires a TTY")
			}
			return "tui", nil
		case "plain":
			return "plain", nil
		case "json":
			return "json", nil
		case "ndjson":
			return "ndjson", nil
		default:
			return "", fmt.Errorf("invalid --progress value %q (expected auto|tui|plain|json|ndjson)", cfg.progress)
		}
	}
	if cfg.jsonOutput {
		return "json", nil
	}
	if cfg.plain {
		return "raw", nil
	}
	if hasTTY {
		return "tui", nil
	}
	return "plain", nil
}

func autoDetectConfig(cfg *buildConfig) error {
	if cfg.projectPath == "" && cfg.workspacePath == "" {
		workspaces, projects := findXcodeContainers(".")
		if len(workspaces) > 0 {
			selected, err := chooseOnePath("workspace", workspaces, cfg.noInput)
			if err != nil {
				return err
			}
			cfg.workspacePath = selected
		} else if len(projects) > 0 {
			selected, err := chooseOnePath("project", projects, cfg.noInput)
			if err != nil {
				return err
			}
			cfg.projectPath = selected
		} else {
			return errors.New("no .xcworkspace or .xcodeproj found")
		}
	}

	if cfg.scheme == "" {
		schemes, err := detectSchemes(*cfg)
		if err != nil {
			return err
		}
		scheme, err := chooseOneValue("scheme", schemes, cfg.noInput)
		if err != nil {
			return err
		}
		cfg.scheme = scheme
	}

	return nil
}

func findXcodeContainers(root string) ([]string, []string) {
	workspaces, _ := filepath.Glob(filepath.Join(root, "*.xcworkspace"))
	projects, _ := filepath.Glob(filepath.Join(root, "*.xcodeproj"))
	sort.Strings(workspaces)
	sort.Strings(projects)
	return workspaces, projects
}

func detectSchemes(cfg buildConfig) ([]string, error) {
	args := []string{"-json"}
	if cfg.workspacePath != "" {
		args = append(args, "-workspace", cfg.workspacePath)
	} else if cfg.projectPath != "" {
		args = append(args, "-project", cfg.projectPath)
	}
	args = append(args, "-list")

	cmd := exec.Command("xcodebuild", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("xcodebuild -list failed: %w", err)
	}

	parsed, err := decodeXcodebuildListOutput(out)
	if err != nil {
		return nil, fmt.Errorf("failed to parse xcodebuild -list output: %w", err)
	}

	schemes := parsed.Project.Schemes
	if len(parsed.Workspace.Schemes) > len(schemes) {
		schemes = parsed.Workspace.Schemes
	}
	if len(schemes) == 0 {
		return nil, errors.New("no schemes found")
	}
	sort.Strings(schemes)
	return schemes, nil
}

func decodeXcodebuildListOutput(data []byte) (xcodebuildList, error) {
	var result xcodebuildList
	if err := json.Unmarshal(data, &result); err == nil {
		return result, nil
	}
	payload := extractJSONObject(data)
	if len(payload) == 0 {
		return xcodebuildList{}, errors.New("no JSON object found in xcodebuild output")
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return xcodebuildList{}, err
	}
	return result, nil
}

func extractJSONObject(data []byte) []byte {
	start := bytes.IndexByte(data, '{')
	end := bytes.LastIndexByte(data, '}')
	if start == -1 || end == -1 || end < start {
		return nil
	}
	return bytes.TrimSpace(data[start : end+1])
}

func chooseOnePath(kind string, options []string, noInput bool) (string, error) {
	labels := make([]string, 0, len(options))
	for _, option := range options {
		labels = append(labels, filepath.Base(option))
	}
	index, err := chooseOneIndex(kind, labels, noInput)
	if err != nil {
		return "", err
	}
	return options[index], nil
}

func chooseOneValue(kind string, options []string, noInput bool) (string, error) {
	index, err := chooseOneIndex(kind, options, noInput)
	if err != nil {
		return "", err
	}
	return options[index], nil
}

func chooseOneIndex(kind string, options []string, noInput bool) (int, error) {
	if len(options) == 0 {
		return -1, fmt.Errorf("no %ss found", kind)
	}
	if len(options) == 1 {
		return 0, nil
	}
	if noInput || !isInteractiveTerminal() {
		return -1, fmt.Errorf(
			"multiple %ss found (%s); rerun with --%s <value> (example: --%s %q) or use interactive mode",
			kind,
			strings.Join(options, ", "),
			kind,
			kind,
			options[0],
		)
	}
	fmt.Fprintf(os.Stderr, "xctide: multiple %ss found:\n", kind)
	for i, option := range options {
		fmt.Fprintf(os.Stderr, "  %d) %s\n", i+1, option)
	}
	reader := bufio.NewReader(os.Stdin)
	for attempts := 0; attempts < 3; attempts++ {
		fmt.Fprintf(os.Stderr, "Select %s [1-%d]: ", kind, len(options))
		line, err := reader.ReadString('\n')
		if err != nil {
			return -1, fmt.Errorf("failed to read %s selection: %w", kind, err)
		}
		value, err := strconv.Atoi(strings.TrimSpace(line))
		if err == nil && value >= 1 && value <= len(options) {
			return value - 1, nil
		}
		fmt.Fprintln(os.Stderr, "Invalid selection.")
	}
	return -1, fmt.Errorf("unable to resolve %s from multiple choices", kind)
}

func isTerminal() bool {
	return term.IsTerminal(int(os.Stdout.Fd())) && term.IsTerminal(int(os.Stderr.Fd()))
}

func isInteractiveTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}
