package main

import (
	"os/exec"
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
