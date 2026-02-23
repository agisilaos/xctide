package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type jsonBuildResult struct {
	Success           bool               `json:"success"`
	ExitCode          int                `json:"exit_code"`
	DurationMS        int64              `json:"duration_ms"`
	Command           []string           `json:"command"`
	Project           string             `json:"project,omitempty"`
	Workspace         string             `json:"workspace,omitempty"`
	Scheme            string             `json:"scheme"`
	Configuration     string             `json:"configuration"`
	Destination       string             `json:"destination,omitempty"`
	Stats             buildStats         `json:"stats"`
	PhaseTimeline     []string           `json:"phase_timeline,omitempty"`
	Completed         []completedItem    `json:"completed,omitempty"`
	Events            []buildEvent       `json:"events,omitempty"`
	Executed          []jsonAction       `json:"executed,omitempty"`
	DependencyTargets []jsonTargetTiming `json:"dependency_targets,omitempty"`
	TopErrors         []string           `json:"top_errors,omitempty"`
	Error             string             `json:"error,omitempty"`
}

type jsonAction struct {
	Name       string `json:"name"`
	DurationMS int64  `json:"duration_ms"`
}

type jsonTargetTiming struct {
	Name       string `json:"name"`
	Project    string `json:"project,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

type ndjsonCompletedEvent struct {
	Type       eventType   `json:"type"`
	At         time.Time   `json:"at"`
	Message    string      `json:"message,omitempty"`
	TaskCount  int         `json:"task_count,omitempty"`
	DurationMS int64       `json:"duration_ms"`
	StepName   string      `json:"step_name,omitempty"`
	StepIndex  int         `json:"step_index,omitempty"`
	StepTotal  int         `json:"step_total,omitempty"`
	StepStatus string      `json:"step_status,omitempty"`
	Level      string      `json:"level,omitempty"`
	TopErrors  []string    `json:"top_errors,omitempty"`
	ExitCode   int         `json:"exit_code,omitempty"`
	Success    bool        `json:"success,omitempty"`
	Stats      *buildStats `json:"stats,omitempty"`
}

func runJSONBuild(cfg buildConfig) (jsonBuildResult, error) {
	start := time.Now()
	args := buildArgs(cfg)
	cmd := exec.Command("xcodebuild", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return jsonBuildResult{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return jsonBuildResult{}, err
	}
	if err := cmd.Start(); err != nil {
		return jsonBuildResult{}, err
	}

	lines := make(chan string, 256)
	var wg sync.WaitGroup
	wg.Add(2)
	go streamLines(stdout, lines, &wg)
	go streamLines(stderr, lines, &wg)
	go func() {
		wg.Wait()
		close(lines)
	}()

	tracker := newEventTracker()
	targetTracker := newTargetTimingTracker()
	timingRows := make([]completedItem, 0)
	inTimingSummary := false
	for line := range lines {
		now := time.Now()
		_ = tracker.processLine(line, now)
		targetTracker.processLine(line, now)
		trimmed := strings.TrimSpace(line)
		if trimmed == "Build Timing Summary" {
			inTimingSummary = true
			continue
		}
		if inTimingSummary {
			if strings.HasPrefix(trimmed, "** BUILD ") {
				inTimingSummary = false
				continue
			}
			if row, ok := parseTimingSummaryLine(trimmed); ok {
				timingRows = append(timingRows, row)
			}
		}
		if cfg.verbose {
			fmt.Fprintln(os.Stderr, line)
		}
	}
	waitErr := cmd.Wait()
	targetTracker.finish(time.Now())
	_ = tracker.finish(waitErr, time.Now())
	var executedRows []timedItem
	if waitErr == nil && cfg.runAfterBuild {
		executedRows, waitErr = runAppOnSimulator(cfg)
	}

	phaseTimeline := phaseTimelineFromEvents(tracker.events)
	result := jsonBuildResult{
		Success:       waitErr == nil,
		ExitCode:      classifyBuildErr(waitErr),
		DurationMS:    time.Since(start).Milliseconds(),
		Command:       append([]string{"xcodebuild"}, args...),
		Project:       cfg.projectPath,
		Workspace:     cfg.workspacePath,
		Scheme:        cfg.scheme,
		Configuration: cfg.configuration,
		Destination:   cfg.destination,
		Stats:         tracker.stats,
		PhaseTimeline: phaseTimeline,
		Completed:     completedFromTimingRows(timingRows),
		Events:        append([]buildEvent(nil), tracker.events...),
		TopErrors:     topErrorsFromEvents(tracker.events, 5),
	}
	dependencyRows := dependencyTargetRows(cfg, targetTracker.rows)
	if len(dependencyRows) > 0 {
		result.DependencyTargets = make([]jsonTargetTiming, 0, len(dependencyRows))
		for _, row := range dependencyRows {
			result.DependencyTargets = append(result.DependencyTargets, jsonTargetTiming{
				Name:       row.name,
				Project:    row.project,
				DurationMS: row.duration.Milliseconds(),
			})
		}
	}
	if len(executedRows) > 0 {
		result.Executed = make([]jsonAction, 0, len(executedRows))
		for _, row := range executedRows {
			result.Executed = append(result.Executed, jsonAction{
				Name:       row.name,
				DurationMS: row.duration.Milliseconds(),
			})
		}
	}
	if waitErr != nil {
		result.Error = waitErr.Error()
	}
	return result, nil
}

func encodeNDJSONEvent(encoder *json.Encoder, event buildEvent) error {
	if event.Type != eventCompleted {
		return encoder.Encode(event)
	}
	payload := ndjsonCompletedEvent{
		Type:       event.Type,
		At:         event.At,
		Message:    event.Message,
		TaskCount:  event.TaskCount,
		DurationMS: event.DurationMS,
		StepName:   event.StepName,
		StepIndex:  event.StepIndex,
		StepTotal:  event.StepTotal,
		StepStatus: event.StepStatus,
		Level:      event.Level,
		TopErrors:  event.TopErrors,
		ExitCode:   event.ExitCode,
		Success:    event.Success,
		Stats:      event.Stats,
	}
	return encoder.Encode(payload)
}

func runNDJSONBuild(cfg buildConfig) (int, error) {
	streamStart := time.Now()
	args := buildArgs(cfg)
	cmd := exec.Command("xcodebuild", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return exitRuntimeFailure, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return exitRuntimeFailure, err
	}
	if err := cmd.Start(); err != nil {
		return exitRuntimeFailure, err
	}

	lines := make(chan string, 256)
	var wg sync.WaitGroup
	wg.Add(2)
	go streamLines(stdout, lines, &wg)
	go streamLines(stderr, lines, &wg)
	go func() {
		wg.Wait()
		close(lines)
	}()

	encoder := json.NewEncoder(os.Stdout)
	tracker := newEventTracker()
	timingRows := make([]completedItem, 0)
	inTimingSummary := false
	for line := range lines {
		events := tracker.processLine(line, time.Now())
		for _, event := range events {
			if err := encodeNDJSONEvent(encoder, event); err != nil {
				return exitRuntimeFailure, err
			}
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "Build Timing Summary" {
			inTimingSummary = true
			continue
		}
		if inTimingSummary {
			if strings.HasPrefix(trimmed, "** BUILD ") {
				inTimingSummary = false
				continue
			}
			if row, ok := parseTimingSummaryLine(trimmed); ok {
				timingRows = append(timingRows, row)
			}
		}
		if cfg.verbose {
			fmt.Fprintln(os.Stderr, line)
		}
	}

	waitErr := cmd.Wait()
	finishEvents, provisionalRunFinished := splitRunFinishedEvent(tracker.finish(waitErr, time.Now()))
	for _, event := range finishEvents {
		if err := encodeNDJSONEvent(encoder, event); err != nil {
			return exitRuntimeFailure, err
		}
	}
	for _, row := range timingRows {
		event := buildEvent{
			Type:       eventCompleted,
			At:         time.Now(),
			Message:    row.Name,
			TaskCount:  row.TaskCount,
			DurationMS: row.DurationMS,
		}
		if err := encoder.Encode(event); err != nil {
			return exitRuntimeFailure, err
		}
	}
	summary := buildEvent{
		Type:      eventDiagSummary,
		At:        time.Now(),
		Stats:     &tracker.stats,
		TopErrors: topErrorsFromEvents(tracker.events, 5),
	}
	if err := encodeNDJSONEvent(encoder, summary); err != nil {
		return exitRuntimeFailure, err
	}

	if waitErr == nil && cfg.runAfterBuild {
		executedRows, runErr := runAppOnSimulator(cfg)
		for _, row := range executedRows {
			event := buildEvent{
				Type:       eventActionDone,
				At:         time.Now(),
				Message:    row.name,
				DurationMS: row.duration.Milliseconds(),
			}
			if err := encodeNDJSONEvent(encoder, event); err != nil {
				return exitRuntimeFailure, err
			}
		}
		if runErr != nil {
			if err := encodeNDJSONEvent(encoder, buildEvent{
				Type:    eventActionFail,
				At:      time.Now(),
				Level:   "error",
				Message: runErr.Error(),
			}); err != nil {
				return exitRuntimeFailure, err
			}
		}
		waitErr = runErr
	}
	final := buildRunFinishedEvent(streamStart, time.Now(), tracker.stats, waitErr, topErrorsFromEvents(tracker.events, 5))
	if provisionalRunFinished != nil && waitErr == nil && !cfg.runAfterBuild {
		// Preserve tracker-calculated completion timestamp semantics when no post-build actions run.
		final.At = provisionalRunFinished.At
		final.DurationMS = provisionalRunFinished.DurationMS
	}
	if err := encodeNDJSONEvent(encoder, final); err != nil {
		return exitRuntimeFailure, err
	}
	return classifyBuildErr(waitErr), nil
}

func splitRunFinishedEvent(events []buildEvent) ([]buildEvent, *buildEvent) {
	rest := make([]buildEvent, 0, len(events))
	var runFinished *buildEvent
	for _, event := range events {
		if event.Type == eventRunFinished {
			copyEvent := event
			runFinished = &copyEvent
			continue
		}
		rest = append(rest, event)
	}
	return rest, runFinished
}

func buildRunFinishedEvent(start time.Time, at time.Time, stats buildStats, err error, topErrors []string) buildEvent {
	statsCopy := stats
	event := buildEvent{
		Type:       eventRunFinished,
		At:         at,
		DurationMS: at.Sub(start).Milliseconds(),
		ExitCode:   classifyBuildErr(err),
		Success:    err == nil,
		Stats:      &statsCopy,
	}
	if len(topErrors) > 0 {
		event.TopErrors = append([]string(nil), topErrors...)
	}
	return event
}

func runProgressPlainBuild(cfg buildConfig) error {
	start := time.Now()
	args := buildArgs(cfg)
	cmd := exec.Command("xcodebuild", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	lines := make(chan string, 256)
	var wg sync.WaitGroup
	wg.Add(2)
	go streamLines(stdout, lines, &wg)
	go streamLines(stderr, lines, &wg)
	go func() {
		wg.Wait()
		close(lines)
	}()

	tracker := newEventTracker()
	targetTracker := newTargetTimingTracker()
	timingRows := make([]completedItem, 0)
	inTimingSummary := false
	commandLabel := "build"
	if cfg.runAfterBuild {
		commandLabel = "run"
	}
	resolved := append([]string{"xcodebuild"}, args...)
	fmt.Fprintln(os.Stdout, "• Invocation")
	fmt.Fprintf(os.Stdout, "  Mode %s\n", commandLabel)
	fmt.Fprintf(os.Stdout, "  Resolved %s\n\n", strings.Join(resolved, " "))

	for line := range lines {
		now := time.Now()
		_ = tracker.processLine(line, now)
		targetTracker.processLine(line, now)
		trimmed := strings.TrimSpace(line)
		if trimmed == "Build Timing Summary" {
			inTimingSummary = true
			continue
		}
		if inTimingSummary {
			if strings.HasPrefix(trimmed, "** BUILD ") {
				inTimingSummary = false
				continue
			}
			if row, ok := parseTimingSummaryLine(trimmed); ok {
				timingRows = append(timingRows, row)
			}
		}
		if cfg.verbose {
			fmt.Fprintln(os.Stdout, line)
		}
	}

	err = cmd.Wait()
	targetTracker.finish(time.Now())
	_ = tracker.finish(err, time.Now())
	var executedRows []timedItem
	if err == nil && cfg.runAfterBuild {
		executedRows, err = runAppOnSimulator(cfg)
	}
	completedRows := completedFromTargetRows(targetTracker.rows)
	if len(timingRows) > 0 {
		completedRows = timingRows
	}
	dependencyRows := dependencyTargetRows(cfg, targetTracker.rows)
	stats := tracker.stats
	renderPlainBuildReport(os.Stdout, cfg, tracker.events, completedRows, dependencyRows, executedRows, stats, time.Since(start), err)
	return err
}

func runPlainBuild(cfg buildConfig) error {
	cmd := exec.Command("xcodebuild", buildArgs(cfg)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
