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
	hint := destinationErrorHint(cfg, topErrors)
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
