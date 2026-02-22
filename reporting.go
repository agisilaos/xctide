package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

func topErrorsFromEvents(events []buildEvent, limit int) []string {
	if limit <= 0 {
		return nil
	}
	seen := make(map[string]bool)
	out := make([]string, 0, limit)
	for _, event := range events {
		if event.Type != eventDiagnostic || event.Level != "error" {
			continue
		}
		msg := strings.TrimSpace(event.Message)
		if msg == "" || seen[msg] {
			continue
		}
		seen[msg] = true
		out = append(out, msg)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func completedFromTimingRows(rows []completedItem) []completedItem {
	if len(rows) == 0 {
		return nil
	}
	out := make([]completedItem, 0, len(rows))
	out = append(out, rows...)
	return out
}

func phaseTimelineFromEvents(events []buildEvent) []string {
	seen := make(map[string]bool)
	var timeline []string
	for _, phase := range defaultPhases() {
		for _, event := range events {
			if event.Type == eventStepDone && event.StepStatus == "done" && event.StepName == phase.name {
				seen[phase.name] = true
				break
			}
		}
		if seen[phase.name] {
			timeline = append(timeline, phase.name)
		}
	}
	return timeline
}

func printPlainEvent(event buildEvent) {
	switch event.Type {
	case eventStepStarted:
		fmt.Fprintf(os.Stdout, "step %d/%d: %s (started)\n", event.StepIndex, event.StepTotal, event.StepName)
	case eventStepDone:
		switch event.StepStatus {
		case "done", "failed":
			fmt.Fprintf(
				os.Stdout,
				"step %d/%d: %s (%s %s)\n",
				event.StepIndex,
				event.StepTotal,
				event.StepName,
				event.StepStatus,
				(time.Duration(event.DurationMS) * time.Millisecond).Truncate(time.Second),
			)
		case "skipped":
			fmt.Fprintf(os.Stdout, "step %d/%d: %s (skipped)\n", event.StepIndex, event.StepTotal, event.StepName)
		}
	}
}

func renderPlainBuildReport(w io.Writer, cfg buildConfig, events []buildEvent, completedRows []completedItem, dependencyRows []buildTargetTiming, executedRows []timedItem, stats buildStats, elapsed time.Duration, err error) {
	fmt.Fprintln(w, "• Run Destination")
	destinationKind, destinationName, osVersion := destinationSummary(cfg.destination)
	if destinationName != "" {
		fmt.Fprintf(w, "  %s %s\n", destinationKind, destinationName)
	} else if cfg.destination != "" {
		fmt.Fprintf(w, "  %s\n", cfg.destination)
	}
	if osVersion != "" {
		fmt.Fprintf(w, "  iOS %s\n", osVersion)
	}
	fmt.Fprintln(w, "")

	fmt.Fprintln(w, "• Completed")
	if len(completedRows) > 0 {
		for _, row := range completedRows {
			if row.TaskCount > 0 {
				fmt.Fprintf(w, "  └ Build %s (%d tasks) %s\n", row.Name, row.TaskCount, formatDuration(row.DurationMS))
			} else {
				fmt.Fprintf(w, "  └ Build %-24s %s\n", row.Name, formatDuration(row.DurationMS))
			}
		}
	} else {
		for _, event := range events {
			if event.Type != eventStepDone || event.StepStatus != "done" {
				continue
			}
			fmt.Fprintf(w, "  └ Build %-8s %s\n", event.StepName, formatDuration(event.DurationMS))
		}
	}
	fmt.Fprintln(w, "")

	if len(dependencyRows) > 0 {
		fmt.Fprintln(w, "• Dependencies")
		limit := len(dependencyRows)
		if limit > 12 {
			limit = 12
		}
		for _, row := range dependencyRows[:limit] {
			label := row.name
			if row.project != "" {
				label = fmt.Sprintf("%s (%s)", row.name, row.project)
			}
			fmt.Fprintf(w, "  └ Build %-24s %s\n", label, formatDurationDur(row.duration))
		}
		if len(dependencyRows) > limit {
			fmt.Fprintf(w, "  ... and %d more\n", len(dependencyRows)-limit)
		}
		fmt.Fprintln(w, "")
	}

	if len(executedRows) > 0 {
		fmt.Fprintln(w, "• Executed")
		for _, row := range executedRows {
			fmt.Fprintf(w, "  └ %-24s %s\n", row.name, formatDurationDur(row.duration))
		}
		fmt.Fprintln(w, "")
	}

	if err == nil {
		fmt.Fprintf(w, "• Build Succeeded %s\n", elapsed.Truncate(time.Second))
	} else {
		fmt.Fprintf(w, "• Build Failed %s\n", elapsed.Truncate(time.Second))
	}
	if stats.warnings > 0 || stats.errors > 0 || stats.tests > 0 || stats.failures > 0 {
		fmt.Fprintf(w, "  warnings:%d errors:%d tests:%d failures:%d\n", stats.warnings, stats.errors, stats.tests, stats.failures)
	}
	topErrors := topErrorsFromEvents(events, 3)
	if len(topErrors) > 0 {
		fmt.Fprintln(w, "  top errors:")
		for _, message := range topErrors {
			fmt.Fprintf(w, "  - %s\n", message)
		}
	}
	if hint := destinationErrorHint(cfg, topErrors); hint != "" {
		fmt.Fprintf(w, "  hint: %s\n", hint)
	}
}

func destinationErrorHint(cfg buildConfig, topErrors []string) string {
	for _, item := range topErrors {
		if !strings.Contains(item, "Unable to find a destination matching the provided destination specifier") {
			continue
		}
		args := []string{"xctide", "destinations", "--scheme", cfg.scheme}
		if cfg.workspacePath != "" {
			args = append(args, "--workspace", cfg.workspacePath)
		} else if cfg.projectPath != "" {
			args = append(args, "--project", cfg.projectPath)
		}
		return strings.Join(args, " ")
	}
	return ""
}

func formatDuration(durationMS int64) string {
	if durationMS <= 0 {
		return "0.0s"
	}
	return fmt.Sprintf("%.1fs", float64(durationMS)/1000.0)
}

func formatDurationDur(value time.Duration) string {
	return fmt.Sprintf("%.1fs", value.Seconds())
}

func parseTimingSummaryLine(line string) (completedItem, bool) {
	match := timingSummaryRe.FindStringSubmatch(strings.TrimSpace(line))
	if len(match) != 3 {
		return completedItem{}, false
	}
	nameAndCount := match[1]
	seconds, err := strconv.ParseFloat(match[2], 64)
	if err != nil {
		return completedItem{}, false
	}
	taskCount := 0
	name := nameAndCount
	if open := strings.LastIndex(nameAndCount, " ("); open > 0 && strings.HasSuffix(nameAndCount, ")") {
		name = nameAndCount[:open]
		suffix := strings.TrimSuffix(strings.TrimPrefix(nameAndCount[open:], " ("), ")")
		suffix = strings.TrimSuffix(strings.TrimSuffix(suffix, " tasks"), " task")
		if parsed, err := strconv.Atoi(strings.TrimSpace(suffix)); err == nil {
			taskCount = parsed
		}
	}
	return completedItem{
		Name:       strings.TrimSpace(name),
		TaskCount:  taskCount,
		DurationMS: time.Duration(seconds * float64(time.Second)).Milliseconds(),
	}, true
}
