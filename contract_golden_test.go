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
