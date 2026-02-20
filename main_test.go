package main

import (
	"os/exec"
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
}
