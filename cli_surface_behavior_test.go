package main

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeArgsRunMode(t *testing.T) {
	args, mode, err := normalizeArgs([]string{"run", "--scheme", "Subsmind"})
	if err != nil {
		t.Fatalf("normalizeArgs returned error: %v", err)
	}
	if mode != "run" {
		t.Fatalf("mode = %q, want run", mode)
	}
	if len(args) != 2 || args[0] != "--scheme" || args[1] != "Subsmind" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestNormalizeArgsPlanMode(t *testing.T) {
	args, mode, err := normalizeArgs([]string{"plan", "--scheme", "Subsmind"})
	if err != nil {
		t.Fatalf("normalizeArgs returned error: %v", err)
	}
	if mode != "plan" {
		t.Fatalf("mode = %q, want plan", mode)
	}
	if len(args) != 2 || args[0] != "--scheme" || args[1] != "Subsmind" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestNormalizeArgsDoctorMode(t *testing.T) {
	args, mode, err := normalizeArgs([]string{"doctor"})
	if err != nil {
		t.Fatalf("normalizeArgs returned error: %v", err)
	}
	if mode != "doctor" {
		t.Fatalf("mode = %q, want doctor", mode)
	}
	if len(args) != 0 {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestNormalizeArgsDestinationsMode(t *testing.T) {
	args, mode, err := normalizeArgs([]string{"destinations", "--scheme", "Subsmind"})
	if err != nil {
		t.Fatalf("normalizeArgs returned error: %v", err)
	}
	if mode != "destinations" {
		t.Fatalf("mode = %q, want destinations", mode)
	}
	if len(args) != 2 || args[0] != "--scheme" || args[1] != "Subsmind" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestNormalizeArgsXcrunMode(t *testing.T) {
	args, mode, err := normalizeArgs([]string{"xcrun", "simctl", "list"})
	if err != nil {
		t.Fatalf("normalizeArgs returned error: %v", err)
	}
	if mode != "xcrun" {
		t.Fatalf("mode = %q, want xcrun", mode)
	}
	if len(args) != 2 || args[0] != "simctl" || args[1] != "list" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestNormalizeArgsXctestMode(t *testing.T) {
	args, mode, err := normalizeArgs([]string{"xctest", "-h"})
	if err != nil {
		t.Fatalf("normalizeArgs returned error: %v", err)
	}
	if mode != "xctest" {
		t.Fatalf("mode = %q, want xctest", mode)
	}
	if len(args) != 1 || args[0] != "-h" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestParseTimingSummaryLine(t *testing.T) {
	item, ok := parseTimingSummaryLine("Ld (2 tasks) | 0.308 seconds")
	if !ok {
		t.Fatal("expected timing summary line to parse")
	}
	if item.Name != "Ld" {
		t.Fatalf("name = %q, want %q", item.Name, "Ld")
	}
	if item.TaskCount != 2 {
		t.Fatalf("task_count = %d, want %d", item.TaskCount, 2)
	}
	if item.DurationMS <= 0 {
		t.Fatalf("duration_ms = %d, want > 0", item.DurationMS)
	}

	if _, ok := parseTimingSummaryLine("not a summary line"); ok {
		t.Fatal("expected invalid summary line to fail parsing")
	}
}

func TestTopErrorsFromEvents(t *testing.T) {
	events := []buildEvent{
		{Type: eventDiagnostic, Level: "error", Message: "first"},
		{Type: eventDiagnostic, Level: "error", Message: "first"},
		{Type: eventDiagnostic, Level: "warning", Message: "ignore"},
		{Type: eventDiagnostic, Level: "error", Message: "second"},
	}
	top := topErrorsFromEvents(events, 5)
	if len(top) != 2 {
		t.Fatalf("len(top) = %d, want 2", len(top))
	}
	if top[0] != "first" || top[1] != "second" {
		t.Fatalf("unexpected top errors: %#v", top)
	}
}

func TestBuildPlanResult(t *testing.T) {
	cfg := buildConfig{
		workspacePath: "Subsmind.xcworkspace",
		scheme:        "Subsmind",
		configuration: "Debug",
		destination:   "platform=iOS Simulator,id=ABC",
		extraArgs:     []string{"build"},
	}
	result := buildPlanResult(cfg, "plan")
	if result.Mode != "plan" {
		t.Fatalf("mode = %q, want plan", result.Mode)
	}
	if result.Scheme != "Subsmind" {
		t.Fatalf("scheme = %q, want Subsmind", result.Scheme)
	}
	if len(result.XcodebuildCmd) == 0 || result.XcodebuildCmd[0] != "xcodebuild" {
		t.Fatalf("unexpected command: %#v", result.XcodebuildCmd)
	}
	foundScheme := false
	foundAction := false
	for i := 0; i < len(result.XcodebuildCmd); i++ {
		if result.XcodebuildCmd[i] == "-scheme" && i+1 < len(result.XcodebuildCmd) && result.XcodebuildCmd[i+1] == "Subsmind" {
			foundScheme = true
		}
		if result.XcodebuildCmd[i] == "build" {
			foundAction = true
		}
	}
	if !foundScheme {
		t.Fatal("expected -scheme Subsmind in command preview")
	}
	if !foundAction {
		t.Fatal("expected build action in command preview")
	}
}

func TestParseAvailableSimulatorCount(t *testing.T) {
	input := []byte(`{
		"devices": {
			"com.apple.CoreSimulator.SimRuntime.iOS-26-0": [
				{"isAvailable": true},
				{"isAvailable": false}
			],
			"com.apple.CoreSimulator.SimRuntime.iOS-25-0": [
				{"isAvailable": true}
			]
		}
	}`)
	count, err := parseAvailableSimulatorCount(input)
	if err != nil {
		t.Fatalf("parseAvailableSimulatorCount returned error: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
}

func TestDecodeXcodebuildListOutput(t *testing.T) {
	raw := []byte(`{
		"project": { "schemes": ["A", "B"] }
	}`)
	result, err := decodeXcodebuildListOutput(raw)
	if err != nil {
		t.Fatalf("decodeXcodebuildListOutput returned error: %v", err)
	}
	if len(result.Project.Schemes) != 2 || result.Project.Schemes[0] != "A" || result.Project.Schemes[1] != "B" {
		t.Fatalf("unexpected schemes: %#v", result.Project.Schemes)
	}
}

func TestDecodeXcodebuildListOutputWithPrefixNoise(t *testing.T) {
	raw := []byte(`xcodebuild warning: using first of multiple matching destinations
{
	"workspace": { "schemes": ["Subsmind"] }
}
`)
	result, err := decodeXcodebuildListOutput(raw)
	if err != nil {
		t.Fatalf("decodeXcodebuildListOutput returned error: %v", err)
	}
	if len(result.Workspace.Schemes) != 1 || result.Workspace.Schemes[0] != "Subsmind" {
		t.Fatalf("unexpected schemes: %#v", result.Workspace.Schemes)
	}
}

func TestDecodeXcodebuildListOutputWithoutJSON(t *testing.T) {
	_, err := decodeXcodebuildListOutput([]byte("no json here"))
	if err == nil {
		t.Fatal("expected decode failure when output has no JSON object")
	}
}

func TestParseDestinationDictLine(t *testing.T) {
	line := "{ platform:iOS Simulator, arch:arm64, id:973281EF-824E-43BB-915F-DBD755A1291A, OS:26.2, name:iPhone 17 Pro }"
	option, ok := parseDestinationDictLine(line)
	if !ok {
		t.Fatal("expected destination line to parse")
	}
	if option.Platform != "iOS Simulator" {
		t.Fatalf("platform = %q, want %q", option.Platform, "iOS Simulator")
	}
	if option.Name != "iPhone 17 Pro" {
		t.Fatalf("name = %q, want %q", option.Name, "iPhone 17 Pro")
	}
	if option.OS != "26.2" {
		t.Fatalf("os = %q, want %q", option.OS, "26.2")
	}
	if option.ID == "" {
		t.Fatal("expected non-empty id")
	}
	if !strings.Contains(option.Spec, "platform=iOS Simulator") || !strings.Contains(option.Spec, "id=973281EF-824E-43BB-915F-DBD755A1291A") {
		t.Fatalf("unexpected spec: %q", option.Spec)
	}
}

func TestParseTargetStartLine(t *testing.T) {
	line := "=== BUILD TARGET Markdown OF PROJECT swift-markdown WITH CONFIGURATION Debug ==="
	target, project, ok := parseTargetStartLine(line)
	if !ok {
		t.Fatal("expected target start line to parse")
	}
	if target != "Markdown" {
		t.Fatalf("target = %q, want %q", target, "Markdown")
	}
	if project != "swift-markdown" {
		t.Fatalf("project = %q, want %q", project, "swift-markdown")
	}
}

func TestParseTargetContextLine(t *testing.T) {
	line := "SwiftCompile normal arm64 /tmp/File.swift (in target 'Markdown' from project 'swift-markdown')"
	target, project, ok := parseTargetContextLine(line)
	if !ok {
		t.Fatal("expected target context line to parse")
	}
	if target != "Markdown" {
		t.Fatalf("target = %q, want Markdown", target)
	}
	if project != "swift-markdown" {
		t.Fatalf("project = %q, want swift-markdown", project)
	}
}

func TestTargetTimingTrackerFallsBackToContextSpans(t *testing.T) {
	tracker := newTargetTimingTracker()
	start := time.Date(2026, 2, 21, 10, 0, 0, 0, time.UTC)
	tracker.processLine("SwiftCompile ... (in target 'Markdown' from project 'swift-markdown')", start)
	tracker.processLine("Ld ... (in target 'Markdown' from project 'swift-markdown')", start.Add(2*time.Second))
	tracker.finish(start.Add(3 * time.Second))
	if len(tracker.rows) == 0 {
		t.Fatal("expected target rows from context fallback")
	}
	if tracker.rows[0].name != "Markdown" {
		t.Fatalf("row name = %q, want Markdown", tracker.rows[0].name)
	}
	if tracker.rows[0].project != "swift-markdown" {
		t.Fatalf("row project = %q, want swift-markdown", tracker.rows[0].project)
	}
	if tracker.rows[0].duration < 2*time.Second {
		t.Fatalf("row duration = %s, want >= 2s", tracker.rows[0].duration)
	}
}

func TestDependencyTargetRows(t *testing.T) {
	cfg := buildConfig{
		projectPath: "/tmp/Subsmind.xcodeproj",
		scheme:      "Subsmind",
	}
	rows := []buildTargetTiming{
		{name: "Subsmind", project: "Subsmind", duration: 2 * time.Second},
		{name: "Markdown", project: "swift-markdown", duration: 4 * time.Second},
		{name: "NIO", project: "swift-nio", duration: 3 * time.Second},
	}
	deps := dependencyTargetRows(cfg, rows)
	if len(deps) != 2 {
		t.Fatalf("len(deps) = %d, want 2", len(deps))
	}
	if deps[0].name != "Markdown" || deps[1].name != "NIO" {
		t.Fatalf("unexpected dependency ordering: %#v", deps)
	}
}

func TestParseShowDestinationsOutput(t *testing.T) {
	raw := []byte(`Command line invocation:
    /Applications/Xcode.app/Contents/Developer/usr/bin/xcodebuild -showdestinations

	Available destinations for the "Subsmind" scheme:
		{ platform:iOS Simulator, arch:arm64, id:973281EF-824E-43BB-915F-DBD755A1291A, OS:26.2, name:iPhone 17 Pro }
		{ platform:iOS, arch:arm64, id:00008150-0001686E0C38401C, name:Agis iPhone }
`)
	options := parseShowDestinationsOutput(raw)
	if len(options) != 2 {
		t.Fatalf("len(options) = %d, want 2", len(options))
	}
	if options[0].Name != "iPhone 17 Pro" {
		t.Fatalf("first destination name = %q, want iPhone 17 Pro", options[0].Name)
	}
	if options[1].Platform != "iOS" {
		t.Fatalf("second destination platform = %q, want iOS", options[1].Platform)
	}
}

func TestFilterDestinations(t *testing.T) {
	options := []destinationOption{
		{Platform: "iOS Simulator", Name: "iPhone 17 Pro"},
		{Platform: "iOS", Name: "Agis iPhone"},
		{Platform: "tvOS Simulator", Name: "Apple TV"},
	}

	simOnly := filterDestinations(options, "", true, false)
	if len(simOnly) != 2 {
		t.Fatalf("len(simOnly) = %d, want 2", len(simOnly))
	}
	for _, option := range simOnly {
		if !strings.Contains(strings.ToLower(option.Platform), "simulator") {
			t.Fatalf("unexpected non-simulator platform in simOnly: %q", option.Platform)
		}
	}

	deviceOnly := filterDestinations(options, "", false, true)
	if len(deviceOnly) != 1 || deviceOnly[0].Platform != "iOS" {
		t.Fatalf("unexpected deviceOnly result: %#v", deviceOnly)
	}

	platformOnly := filterDestinations(options, "iOS Simulator", false, false)
	if len(platformOnly) != 1 || platformOnly[0].Name != "iPhone 17 Pro" {
		t.Fatalf("unexpected platformOnly result: %#v", platformOnly)
	}
}

func TestPassthroughSpecXcrun(t *testing.T) {
	name, args, err := passthroughSpec("xcrun", []string{"simctl", "list", "devices"})
	if err != nil {
		t.Fatalf("passthroughSpec returned error: %v", err)
	}
	if name != "xcrun" {
		t.Fatalf("name = %q, want xcrun", name)
	}
	if len(args) != 3 || args[0] != "simctl" || args[1] != "list" || args[2] != "devices" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestPassthroughSpecXcrunRequiresArgs(t *testing.T) {
	_, _, err := passthroughSpec("xcrun", nil)
	if err == nil {
		t.Fatal("expected error when xcrun is missing arguments")
	}
}

func TestPassthroughSpecXctest(t *testing.T) {
	name, args, err := passthroughSpec("xctest", []string{"-h"})
	if err != nil {
		t.Fatalf("passthroughSpec returned error: %v", err)
	}
	if name != "xcrun" {
		t.Fatalf("name = %q, want xcrun", name)
	}
	if len(args) != 2 || args[0] != "xctest" || args[1] != "-h" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestPassthroughSpecXctestRequiresArgs(t *testing.T) {
	_, _, err := passthroughSpec("xctest", nil)
	if err == nil {
		t.Fatal("expected error when xctest is missing arguments")
	}
}

func TestPassthroughSpecStripsLeadingDoubleDash(t *testing.T) {
	name, args, err := passthroughSpec("xcrun", []string{"--", "simctl", "list"})
	if err != nil {
		t.Fatalf("passthroughSpec returned error: %v", err)
	}
	if name != "xcrun" {
		t.Fatalf("name = %q, want xcrun", name)
	}
	if len(args) != 2 || args[0] != "simctl" || args[1] != "list" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestWantsXctestHelp(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{args: nil, want: true},
		{args: []string{"-h"}, want: true},
		{args: []string{"--help"}, want: true},
		{args: []string{"help"}, want: true},
		{args: []string{"--", "--help"}, want: true},
		{args: []string{"/tmp/Tests.xctest"}, want: false},
		{args: []string{"-XCTest", "Suite/test", "/tmp/Tests.xctest"}, want: false},
	}

	for _, tc := range cases {
		if got := wantsXctestHelp(tc.args); got != tc.want {
			t.Fatalf("wantsXctestHelp(%v) = %v, want %v", tc.args, got, tc.want)
		}
	}
}
