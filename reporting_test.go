package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPhaseTimelineFromEvents(t *testing.T) {
	events := []buildEvent{
		{Type: eventStepDone, StepName: "Prepare", StepStatus: "done"},
		{Type: eventStepDone, StepName: "Compile", StepStatus: "done"},
		{Type: eventStepDone, StepName: "Link", StepStatus: "failed"},
		{Type: eventStepDone, StepName: "Sign", StepStatus: "skipped"},
	}
	timeline := phaseTimelineFromEvents(events)
	if len(timeline) != 2 {
		t.Fatalf("len(timeline) = %d, want 2", len(timeline))
	}
	if timeline[0] != "Prepare" || timeline[1] != "Compile" {
		t.Fatalf("unexpected timeline: %#v", timeline)
	}
}

func TestDestinationErrorHintIncludesProject(t *testing.T) {
	cfg := buildConfig{scheme: "Subsmind", projectPath: "Subsmind.xcodeproj"}
	topErrors := []string{"Unable to find a destination matching the provided destination specifier"}
	hint := buildFailureHint(cfg, topErrors)
	if !strings.Contains(hint, "xctide destinations --scheme Subsmind") {
		t.Fatalf("unexpected hint: %q", hint)
	}
	if !strings.Contains(hint, "--project Subsmind.xcodeproj") {
		t.Fatalf("expected project hint, got: %q", hint)
	}
}

func TestFormatDuration(t *testing.T) {
	if got := formatDuration(0); got != "0.0s" {
		t.Fatalf("formatDuration(0) = %q, want 0.0s", got)
	}
	if got := formatDuration(1530); got != "1.5s" {
		t.Fatalf("formatDuration(1530) = %q, want 1.5s", got)
	}
}

func TestRenderPlainBuildReportShowsCoreSections(t *testing.T) {
	var buf bytes.Buffer
	cfg := buildConfig{
		scheme:      "Subsmind",
		destination: "platform=iOS Simulator,name=iPhone 17 Pro",
		details:     true,
	}
	events := []buildEvent{{Type: eventDiagnostic, Level: "error", Message: "sample error"}}
	completed := []completedItem{{Name: "Subsmind", DurationMS: 4000}}
	dependencies := []buildTargetTiming{{name: "Markdown", project: "swift-markdown", duration: 2 * time.Second}}
	executed := []timedItem{{name: "Launch simulator", duration: 400 * time.Millisecond}}
	stats := buildStats{warnings: 1, errors: 1}
	renderPlainBuildReport(&buf, cfg, events, completed, dependencies, executed, stats, 5*time.Second, errors.New("boom"))

	out := buf.String()
	checks := []string{
		"• Run Destination",
		"• Completed",
		"• Dependencies",
		"• Executed",
		"• Build Failed",
		"top errors:",
		"sample error",
	}
	for _, token := range checks {
		if !strings.Contains(out, token) {
			t.Fatalf("expected %q in report output:\n%s", token, out)
		}
	}
}

func TestRenderPlainBuildReportCompactDefault(t *testing.T) {
	var buf bytes.Buffer
	cfg := buildConfig{
		scheme:      "Subsmind",
		destination: "platform=iOS Simulator,name=iPhone 17 Pro",
	}
	events := []buildEvent{{Type: eventDiagnostic, Level: "error", Message: "sample error"}}
	completed := []completedItem{{Name: "Subsmind", DurationMS: 4000}}
	dependencies := []buildTargetTiming{{name: "Markdown", project: "swift-markdown", duration: 2 * time.Second}}
	stats := buildStats{warnings: 1, errors: 1}
	renderPlainBuildReport(&buf, cfg, events, completed, dependencies, nil, stats, 5*time.Second, errors.New("boom"))

	out := buf.String()
	if !strings.Contains(out, "• Summary") {
		t.Fatalf("expected compact summary section, got:\n%s", out)
	}
	if strings.Contains(out, "• Completed") {
		t.Fatalf("did not expect detailed completed section in compact mode:\n%s", out)
	}
	if !strings.Contains(out, "slow dependencies:1") || !strings.Contains(out, "Markdown (swift-markdown)") {
		t.Fatalf("expected compact dependency preview, got:\n%s", out)
	}
}

func TestBuildFailureHintCompileError(t *testing.T) {
	cfg := buildConfig{scheme: "Subsmind"}
	topErrors := []string{"/tmp/File.swift:12:5: error: expected expression"}
	hint := buildFailureHint(cfg, topErrors)
	if !strings.Contains(hint, "fix the first compiler error") {
		t.Fatalf("unexpected hint: %q", hint)
	}
}

func TestBuildFailureHintMissingProjectPath(t *testing.T) {
	cfg := buildConfig{scheme: "Subsmind"}
	topErrors := []string{"xcodebuild: error: 'Subsmind.xcodeproj' does not exist."}
	hint := buildFailureHint(cfg, topErrors)
	if !strings.Contains(hint, "project root") {
		t.Fatalf("unexpected hint: %q", hint)
	}
}

func TestTopErrorsFromEventsNormalizesDuplicateMessages(t *testing.T) {
	events := []buildEvent{
		{Type: eventDiagnostic, Level: "error", Message: "  Foo   BAR  "},
		{Type: eventDiagnostic, Level: "error", Message: "foo bar"},
		{Type: eventDiagnostic, Level: "error", Message: "FOO BAR"},
		{Type: eventDiagnostic, Level: "error", Message: "another error"},
	}
	top := topErrorsFromEvents(events, 5)
	if len(top) != 2 {
		t.Fatalf("len(top) = %d, want 2; top=%#v", len(top), top)
	}
	if top[0] != "Foo   BAR" {
		t.Fatalf("top[0] = %q, want %q", top[0], "Foo   BAR")
	}
	if top[1] != "another error" {
		t.Fatalf("top[1] = %q, want %q", top[1], "another error")
	}
}
