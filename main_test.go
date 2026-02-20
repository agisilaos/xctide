package main

import (
	"bytes"
	"encoding/json"
	"os/exec"
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
