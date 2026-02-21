package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEventTrackerSuccessFlow(t *testing.T) {
	tracker := newEventTracker()
	t0 := time.Unix(100, 0)
	t1 := t0.Add(2 * time.Second)
	t2 := t1.Add(3 * time.Second)

	var events []buildEvent
	events = append(events, tracker.processLine("CompileC SomeFile.swift", t0)...)
	events = append(events, tracker.processLine("Ld App", t1)...)
	events = append(events, tracker.finish(nil, t2)...)

	if len(events) == 0 {
		t.Fatal("expected emitted events")
	}

	if events[0].Type != eventRunStarted {
		t.Fatalf("first event type = %s, want %s", events[0].Type, eventRunStarted)
	}

	last := events[len(events)-1]
	if last.Type != eventRunFinished {
		t.Fatalf("last event type = %s, want %s", last.Type, eventRunFinished)
	}
	if !last.Success {
		t.Fatal("run finished should be successful")
	}

	statusByStep := map[string]string{}
	for _, event := range events {
		if event.Type == eventStepDone {
			statusByStep[event.StepName] = event.StepStatus
		}
	}

	if statusByStep["Prepare"] != "done" {
		t.Fatalf("Prepare status = %q, want done", statusByStep["Prepare"])
	}
	if statusByStep["Compile"] != "done" {
		t.Fatalf("Compile status = %q, want done", statusByStep["Compile"])
	}
	if statusByStep["Link"] != "done" {
		t.Fatalf("Link status = %q, want done", statusByStep["Link"])
	}
	if statusByStep["Sign"] != "skipped" {
		t.Fatalf("Sign status = %q, want skipped", statusByStep["Sign"])
	}
	if statusByStep["Test"] != "skipped" {
		t.Fatalf("Test status = %q, want skipped", statusByStep["Test"])
	}
}

func TestEventTrackerFailureFlow(t *testing.T) {
	tracker := newEventTracker()
	t0 := time.Unix(200, 0)
	t1 := t0.Add(1 * time.Second)

	cmd := exec.Command("sh", "-c", "exit 1")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero command error")
	}

	var events []buildEvent
	events = append(events, tracker.processLine("Ld App", t0)...)
	events = append(events, tracker.finish(err, t1)...)

	var runFinished *buildEvent
	statusByStep := map[string]string{}
	for i := range events {
		event := events[i]
		if event.Type == eventStepDone {
			statusByStep[event.StepName] = event.StepStatus
		}
		if event.Type == eventRunFinished {
			runFinished = &events[i]
		}
	}

	if runFinished == nil {
		t.Fatal("missing run_finished event")
	}
	if runFinished.Success {
		t.Fatal("run_finished should be unsuccessful")
	}
	if runFinished.ExitCode != exitBuildFailure {
		t.Fatalf("exit_code = %d, want %d", runFinished.ExitCode, exitBuildFailure)
	}
	if statusByStep["Link"] != "failed" {
		t.Fatalf("Link status = %q, want failed", statusByStep["Link"])
	}
	if statusByStep["Sign"] != "skipped" {
		t.Fatalf("Sign status = %q, want skipped", statusByStep["Sign"])
	}
	if statusByStep["Test"] != "skipped" {
		t.Fatalf("Test status = %q, want skipped", statusByStep["Test"])
	}
}

func TestResolveProgressMode(t *testing.T) {
	cfg := buildConfig{progress: "auto"}
	mode, err := resolveProgressMode(cfg, map[string]bool{}, false)
	if err != nil {
		t.Fatalf("resolveProgressMode auto returned error: %v", err)
	}
	if mode != "plain" {
		t.Fatalf("mode = %q, want plain", mode)
	}

	cfg = buildConfig{progress: "tui"}
	_, err = resolveProgressMode(cfg, map[string]bool{"progress": true}, false)
	if err == nil {
		t.Fatal("expected error for --progress=tui without TTY")
	}

	cfg = buildConfig{progress: "plain", plain: true}
	_, err = resolveProgressMode(cfg, map[string]bool{"progress": true, "plain": true}, true)
	if err == nil {
		t.Fatal("expected conflict error for --progress with --plain")
	}

	cfg = buildConfig{progress: "ndjson"}
	mode, err = resolveProgressMode(cfg, map[string]bool{"progress": true}, false)
	if err != nil {
		t.Fatalf("resolveProgressMode ndjson returned error: %v", err)
	}
	if mode != "ndjson" {
		t.Fatalf("mode = %q, want ndjson", mode)
	}
}

func TestHasBuildAction(t *testing.T) {
	if hasBuildAction([]string{"-showBuildSettings"}) {
		t.Fatal("expected false for non-action args")
	}
	if !hasBuildAction([]string{"clean", "build"}) {
		t.Fatal("expected true for clean/build action")
	}
	if !hasBuildAction([]string{"test"}) {
		t.Fatal("expected true for test action")
	}
}

func TestShouldPrintWrapperError(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 1")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero command error")
	}
	if shouldPrintWrapperError("plain", err) {
		t.Fatal("expected plain mode to suppress wrapper error line")
	}
	if !shouldPrintWrapperError("raw", err) {
		t.Fatal("expected raw mode to print wrapper error line")
	}
	if shouldPrintWrapperError("raw", nil) {
		t.Fatal("expected nil error to suppress wrapper error line")
	}
}

func TestProgressCounts(t *testing.T) {
	phases := []phase{
		{name: "Prepare", status: "done"},
		{name: "Compile", status: "done"},
		{name: "Link", status: "failed"},
		{name: "Sign", status: "done"},
		{name: "Test", status: "skipped"},
	}
	completed, total, skipped := progressCounts(phases)
	if completed != 4 {
		t.Fatalf("completed = %d, want 4", completed)
	}
	if total != 4 {
		t.Fatalf("total = %d, want 4 (non-skipped phases)", total)
	}
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1", skipped)
	}
}

func TestModelElapsed(t *testing.T) {
	start := time.Unix(100, 0)
	finished := start.Add(5 * time.Second)

	m := model{startTime: start, finished: true, finishedAt: finished}
	if got := modelElapsed(m); got != 5*time.Second {
		t.Fatalf("modelElapsed(finished) = %s, want 5s", got)
	}

	m = model{startTime: start, finished: true}
	if got := modelElapsed(m); got < 0 {
		t.Fatalf("modelElapsed(without finishedAt) = %s, want >= 0", got)
	}
}

func TestChooseOneIndexNoInputProvidesExample(t *testing.T) {
	_, err := chooseOneIndex("scheme", []string{"Subsmind", "Subsmind - dev"}, true)
	if err == nil {
		t.Fatal("expected chooseOneIndex to fail in no-input mode with multiple options")
	}
	msg := err.Error()
	if !strings.Contains(msg, "--scheme <value>") || !strings.Contains(msg, "--scheme \"Subsmind\"") {
		t.Fatalf("unexpected error guidance: %q", msg)
	}
}

func TestDestinationUDID(t *testing.T) {
	udid := destinationUDID("platform=iOS Simulator,id=ABC-123,name=iPhone 16 Pro")
	if udid != "ABC-123" {
		t.Fatalf("udid = %q, want %q", udid, "ABC-123")
	}
}

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

func TestSplitRunFinishedEvent(t *testing.T) {
	input := []buildEvent{
		{Type: eventStepStarted, StepName: "Prepare"},
		{Type: eventRunFinished, Success: true},
		{Type: eventCompleted, Message: "Ld"},
	}
	rest, final := splitRunFinishedEvent(input)
	if final == nil {
		t.Fatal("expected run_finished event")
	}
	if final.Type != eventRunFinished {
		t.Fatalf("final type = %s, want %s", final.Type, eventRunFinished)
	}
	if len(rest) != 2 {
		t.Fatalf("len(rest) = %d, want 2", len(rest))
	}
	if rest[0].Type != eventStepStarted || rest[1].Type != eventCompleted {
		t.Fatalf("unexpected rest events: %#v", rest)
	}
}

func TestBuildRunFinishedEvent(t *testing.T) {
	start := time.Unix(100, 0)
	end := start.Add(2500 * time.Millisecond)
	errs := []string{"first", "second"}
	event := buildRunFinishedEvent(start, end, buildStats{warnings: 1, errors: 2}, nil, errs)

	if event.Type != eventRunFinished {
		t.Fatalf("event type = %s, want %s", event.Type, eventRunFinished)
	}
	if !event.Success {
		t.Fatal("expected success=true")
	}
	if event.ExitCode != exitOK {
		t.Fatalf("exit code = %d, want %d", event.ExitCode, exitOK)
	}
	if event.DurationMS != 2500 {
		t.Fatalf("duration_ms = %d, want 2500", event.DurationMS)
	}
	if event.Stats == nil || event.Stats.warnings != 1 || event.Stats.errors != 2 {
		t.Fatalf("unexpected stats payload: %#v", event.Stats)
	}
	if !reflect.DeepEqual(event.TopErrors, errs) {
		t.Fatalf("top_errors = %#v, want %#v", event.TopErrors, errs)
	}
}

func TestBuildEventMarshalRunFinishedIncludesContractFields(t *testing.T) {
	event := buildEvent{
		Type:     eventRunFinished,
		At:       time.Unix(10, 0),
		Success:  false,
		ExitCode: exitBuildFailure,
	}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if _, ok := payload["success"]; !ok {
		t.Fatal("run_finished must include success")
	}
	if _, ok := payload["exit_code"]; !ok {
		t.Fatal("run_finished must include exit_code")
	}
}

func TestNDJSONContractRunFinishedIsLastAndUnique(t *testing.T) {
	start := time.Unix(100, 0)
	tracker := newEventTracker()
	_ = tracker.processLine("CompileC SomeFile.swift", start)
	finishEvents, _ := splitRunFinishedEvent(tracker.finish(nil, start.Add(2*time.Second)))

	topErrors := []string{"sample error"}
	stream := append([]buildEvent{}, finishEvents...)
	stream = append(stream, buildEvent{
		Type:       eventCompleted,
		At:         start.Add(2100 * time.Millisecond),
		Message:    "Ld",
		TaskCount:  2,
		DurationMS: 308,
	})
	stream = append(stream, buildEvent{
		Type:  eventDiagSummary,
		At:    start.Add(2200 * time.Millisecond),
		Stats: &tracker.stats,
	})
	stream = append(stream, buildEvent{
		Type:       eventActionDone,
		At:         start.Add(2300 * time.Millisecond),
		Message:    "Launch simulator",
		DurationMS: 400,
	})
	stream = append(stream, buildRunFinishedEvent(start, start.Add(3*time.Second), tracker.stats, nil, topErrors))

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, event := range stream {
		if err := enc.Encode(event); err != nil {
			t.Fatalf("encode failed: %v", err)
		}
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) == 0 {
		t.Fatal("expected ndjson lines")
	}

	runFinishedCount := 0
	for i, line := range lines {
		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			t.Fatalf("line %d: invalid json: %v", i, err)
		}
		validateMachineContractEvent(t, i, payload)
		typ, _ := payload["type"].(string)
		if typ == string(eventRunFinished) {
			runFinishedCount++
			if i != len(lines)-1 {
				t.Fatalf("run_finished must be last event, got at line %d of %d", i+1, len(lines))
			}
		}
	}
	if runFinishedCount != 1 {
		t.Fatalf("run_finished count = %d, want 1", runFinishedCount)
	}
}

func validateMachineContractEvent(t *testing.T, index int, payload map[string]any) {
	t.Helper()
	requireField := func(name string) {
		t.Helper()
		if _, ok := payload[name]; !ok {
			t.Fatalf("line %d missing required %q for type %v", index+1, name, payload["type"])
		}
	}

	requireField("type")
	requireField("at")
	typ, _ := payload["type"].(string)
	switch typ {
	case string(eventStepStarted):
		requireField("step_name")
		requireField("step_index")
		requireField("step_total")
	case string(eventStepDone):
		requireField("step_name")
		requireField("step_index")
		requireField("step_total")
		requireField("step_status")
	case string(eventDiagnostic):
		requireField("level")
		requireField("message")
	case string(eventCompleted):
		requireField("message")
		requireField("duration_ms")
	case string(eventActionDone):
		requireField("message")
		requireField("duration_ms")
	case string(eventActionFail):
		requireField("level")
		requireField("message")
	case string(eventDiagSummary):
		requireField("stats")
	case string(eventRunFinished):
		requireField("success")
		requireField("exit_code")
		requireField("duration_ms")
		requireField("stats")
	}
}

func TestContractGoldenJSON(t *testing.T) {
	result := sampleContractJSONResult()
	got, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	assertGoldenBytes(t, filepath.Join("testdata", "contracts", "sample.json.golden"), got)
}

func TestContractGoldenNDJSON(t *testing.T) {
	events := sampleContractEvents()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, event := range events {
		if err := enc.Encode(event); err != nil {
			t.Fatalf("encode failed: %v", err)
		}
	}
	assertGoldenBytes(t, filepath.Join("testdata", "contracts", "sample.ndjson.golden"), []byte(strings.TrimSpace(buf.String())))
}

func TestContractFixtureLock(t *testing.T) {
	files := []string{
		filepath.Join("testdata", "contracts", "sample.json.golden"),
		filepath.Join("testdata", "contracts", "sample.ndjson.golden"),
	}
	hash, err := computeFilesHash(files)
	if err != nil {
		t.Fatalf("computeFilesHash returned error: %v", err)
	}
	lockPath := filepath.Join("testdata", "contracts", "LOCK")
	if os.Getenv("UPDATE_CONTRACT_LOCK") == "1" {
		if err := os.WriteFile(lockPath, []byte(hash+"\n"), 0o644); err != nil {
			t.Fatalf("write lock failed: %v", err)
		}
	}
	lockRaw, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lock failed: %v", err)
	}
	lock := strings.TrimSpace(string(lockRaw))
	if lock == "" {
		t.Fatalf("empty lock file: %s", lockPath)
	}
	if hash != lock {
		t.Fatalf("contract fixture lock mismatch\nlock: %s\nhash: %s\nrun: UPDATE_CONTRACT_LOCK=1 go test ./... -run TestContractFixtureLock", lock, hash)
	}
}

func TestPlainReportGoldenBuildSuccess(t *testing.T) {
	cfg := buildConfig{
		destination: "platform=iOS Simulator,name=iPhone 17 Pro",
	}
	events := []buildEvent{
		{Type: eventRunStarted, At: time.Date(2026, 2, 21, 10, 0, 0, 0, time.UTC)},
		{Type: eventStepDone, StepName: "Prepare", StepStatus: "done", DurationMS: 1200},
		{Type: eventRunFinished, Success: true},
	}
	completedRows := []completedItem{
		{Name: "Ld", TaskCount: 2, DurationMS: 308},
		{Name: "CodeSign", TaskCount: 1, DurationMS: 100},
	}
	stats := buildStats{warnings: 1, errors: 0, tests: 0, failures: 0}
	var buf bytes.Buffer
	renderPlainBuildReport(&buf, cfg, events, completedRows, nil, nil, stats, 14*time.Second, nil)
	assertGoldenBytes(t, filepath.Join("testdata", "plain", "build-success.golden"), []byte(strings.TrimSpace(buf.String())))
}

func TestPlainReportGoldenBuildFailure(t *testing.T) {
	cfg := buildConfig{
		projectPath: "Subsmind.xcodeproj",
		scheme:      "Subsmind",
		destination: "platform=iOS Simulator,name=iPhone 17 Pro",
	}
	events := []buildEvent{
		{Type: eventDiagnostic, Level: "error", Message: "xcodebuild: error: Unable to find a destination matching the provided destination specifier:"},
		{Type: eventRunFinished, Success: false},
	}
	stats := buildStats{warnings: 0, errors: 1, tests: 0, failures: 0}
	var buf bytes.Buffer
	cmd := exec.Command("sh", "-c", "exit 70")
	err := cmd.Run()
	renderPlainBuildReport(&buf, cfg, events, nil, nil, nil, stats, 1*time.Second, err)
	assertGoldenBytes(t, filepath.Join("testdata", "plain", "build-failure.golden"), []byte(strings.TrimSpace(buf.String())))
}

func TestPlainReportGoldenRunSuccess(t *testing.T) {
	cfg := buildConfig{
		runAfterBuild: true,
		destination:   "platform=iOS Simulator,name=iPhone 17 Pro",
	}
	events := []buildEvent{
		{Type: eventRunStarted, At: time.Date(2026, 2, 21, 10, 0, 0, 0, time.UTC)},
		{Type: eventRunFinished, Success: true},
	}
	completedRows := []completedItem{
		{Name: "Ld", TaskCount: 2, DurationMS: 308},
	}
	executedRows := []timedItem{
		{name: "Launch simulator", duration: 400 * time.Millisecond},
		{name: "Install iOS app", duration: 6 * time.Second},
		{name: "Launch iOS app", duration: 1500 * time.Millisecond},
	}
	stats := buildStats{warnings: 0, errors: 0, tests: 0, failures: 0}
	var buf bytes.Buffer
	renderPlainBuildReport(&buf, cfg, events, completedRows, nil, executedRows, stats, 33*time.Second, nil)
	assertGoldenBytes(t, filepath.Join("testdata", "plain", "run-success.golden"), []byte(strings.TrimSpace(buf.String())))
}

func TestDestinationErrorHint(t *testing.T) {
	cfg := buildConfig{
		projectPath: "Subsmind.xcodeproj",
		scheme:      "Subsmind",
	}
	errors := []string{"xcodebuild: error: Unable to find a destination matching the provided destination specifier:"}
	hint := destinationErrorHint(cfg, errors)
	want := "xctide destinations --scheme Subsmind --project Subsmind.xcodeproj"
	if hint != want {
		t.Fatalf("hint = %q, want %q", hint, want)
	}
}

func sampleContractJSONResult() jsonBuildResult {
	events := sampleContractEvents()
	stats := buildStats{warnings: 1, errors: 0, tests: 2, failures: 0}
	return jsonBuildResult{
		Success:       true,
		ExitCode:      exitOK,
		DurationMS:    3000,
		Command:       []string{"xcodebuild", "-scheme", "Subsmind", "build"},
		Workspace:     "Subsmind.xcworkspace",
		Scheme:        "Subsmind",
		Configuration: "Debug",
		Destination:   "platform=iOS Simulator,id=ABC",
		Stats:         stats,
		PhaseTimeline: []string{"Prepare", "Compile"},
		Completed: []completedItem{
			{Name: "Ld", TaskCount: 2, DurationMS: 308},
		},
		Events: events,
		Executed: []jsonAction{
			{Name: "Launch simulator", DurationMS: 400},
		},
	}
}

func sampleContractEvents() []buildEvent {
	t0 := time.Date(2026, 2, 21, 10, 0, 0, 0, time.UTC)
	stats := buildStats{warnings: 1, errors: 0, tests: 2, failures: 0}
	statsCopy := stats
	return []buildEvent{
		{Type: eventRunStarted, At: t0},
		{Type: eventStepStarted, At: t0, StepName: "Prepare", StepIndex: 1, StepTotal: 5},
		{Type: eventStepDone, At: t0.Add(500 * time.Millisecond), StepName: "Prepare", StepIndex: 1, StepTotal: 5, StepStatus: "done", DurationMS: 500},
		{Type: eventStepStarted, At: t0.Add(500 * time.Millisecond), StepName: "Compile", StepIndex: 2, StepTotal: 5},
		{Type: eventDiagnostic, At: t0.Add(1500 * time.Millisecond), Level: "warning", Message: "warning: sample warning"},
		{Type: eventCompleted, At: t0.Add(2 * time.Second), Message: "Ld", TaskCount: 2, DurationMS: 308},
		{Type: eventDiagSummary, At: t0.Add(2500 * time.Millisecond), Stats: &statsCopy},
		{Type: eventActionDone, At: t0.Add(2800 * time.Millisecond), Message: "Launch simulator", DurationMS: 400},
		buildRunFinishedEvent(t0, t0.Add(3*time.Second), stats, nil, nil),
	}
}

func assertGoldenBytes(t *testing.T, path string, got []byte) {
	t.Helper()
	got = bytes.TrimSpace(got)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir failed: %v", err)
		}
		if err := os.WriteFile(path, append(got, '\n'), 0o644); err != nil {
			t.Fatalf("write golden failed: %v", err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden failed: %v", err)
	}
	want = bytes.TrimSpace(want)
	if !bytes.Equal(got, want) {
		t.Fatalf("golden mismatch for %s\nwant: %s\ngot:  %s", path, string(want), string(got))
	}
}

func computeFilesHash(paths []string) (string, error) {
	h := sha256.New()
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		_, _ = h.Write([]byte(path))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(content)
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
