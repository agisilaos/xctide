package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func renderLines(lines []string, width int, maxLines int) string {
	if len(lines) == 0 {
		return "(waiting for output...)"
	}
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	var b strings.Builder
	for i, line := range lines {
		b.WriteString(truncateLine(line, width))
		if i < len(lines)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func truncateLine(line string, width int) string {
	if width <= 4 {
		return line
	}
	max := width - 4
	if len(line) <= max {
		return line
	}
	return line[:max] + "..."
}

func renderPhasePanel(m model, labelStyle, accentStyle, warnStyle, errorStyle lipgloss.Style, width int, height int, panelStyle lipgloss.Style) string {
	var b strings.Builder
	b.WriteString(labelStyle.Render("phases"))
	b.WriteString("\n")
	total := totalPhaseDuration(m.phases)
	barWidth := width - 16
	if barWidth < 6 {
		barWidth = 6
	}
	for _, phase := range m.phases {
		status := phase.status
		tag := "[ ]"
		style := labelStyle
		switch status {
		case "active":
			tag = "[*]"
			style = accentStyle
		case "done":
			tag = "[x]"
			style = labelStyle
		}
		duration := formatPhaseDuration(phase)
		bar := renderBar(phaseDurationValue(phase), total, barWidth)
		line := fmt.Sprintf("%s %-7s %s %s", tag, phase.name, bar, duration)
		b.WriteString(style.Render(line))
		b.WriteString("\n")
	}
	return panelStyle.Width(width).Height(height).Render(strings.TrimSpace(b.String()))
}

func renderInsightsPanel(m model, labelStyle, accentStyle, warnStyle, errorStyle lipgloss.Style, width int, height int, panelStyle lipgloss.Style) string {
	var b strings.Builder
	b.WriteString(labelStyle.Render("insights"))
	b.WriteString("\n")
	b.WriteString(labelStyle.Render("slow files"))
	b.WriteString("\n")
	b.WriteString(renderTimedList(m.slowFiles, width, 3))
	b.WriteString("\n")
	b.WriteString(labelStyle.Render("slow tests"))
	b.WriteString("\n")
	b.WriteString(renderTimedList(m.slowTests, width, 3))
	b.WriteString("\n")
	b.WriteString(labelStyle.Render("slow targets"))
	b.WriteString("\n")
	b.WriteString(renderTimedList(m.targets, width, 3))
	return panelStyle.Width(width).Height(height).Render(strings.TrimSpace(b.String()))
}

func renderLogPanel(m model, labelStyle lipgloss.Style, width int, height int, panelStyle lipgloss.Style) string {
	title := labelStyle.Render("log")
	contentHeight := height - 2
	if contentHeight < 3 {
		contentHeight = 3
	}
	output := renderLines(m.lines, width, contentHeight-1)
	content := fmt.Sprintf("%s\n%s", title, output)
	return panelStyle.Width(width).Height(height).Render(content)
}

func formatPhaseDuration(phase phase) string {
	if phase.startedAt.IsZero() {
		return ""
	}
	end := phase.endedAt
	if end.IsZero() {
		end = time.Now()
	}
	return end.Sub(phase.startedAt).Truncate(time.Second).String()
}

func clamp(min, value, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func renderTimedList(items []timedItem, width int, limit int) string {
	if len(items) == 0 {
		return "  -"
	}
	var b strings.Builder
	count := limit
	if len(items) < count {
		count = len(items)
	}
	for i := 0; i < count; i++ {
		item := items[i]
		line := fmt.Sprintf("• %-28s %s", item.name, item.duration.String())
		b.WriteString(truncateLine(line, width))
		if i < count-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func renderBar(value time.Duration, total time.Duration, width int) string {
	if total <= 0 {
		total = time.Second
	}
	filled := int(float64(width) * (float64(value) / float64(total)))
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func phaseDurationValue(phase phase) time.Duration {
	if phase.startedAt.IsZero() {
		return 0
	}
	end := phase.endedAt
	if end.IsZero() {
		end = time.Now()
	}
	return end.Sub(phase.startedAt)
}

func totalPhaseDuration(phases []phase) time.Duration {
	var total time.Duration
	for _, phase := range phases {
		total += phaseDurationValue(phase)
	}
	if total <= 0 {
		total = time.Second
	}
	return total
}

func addTimedItem(items []timedItem, item timedItem, limit int) []timedItem {
	items = append(items, item)
	sort.Slice(items, func(i, j int) bool {
		return items[i].duration > items[j].duration
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

func parseDurationSeconds(raw string) time.Duration {
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	return time.Duration(value * float64(time.Second))
}

func renderHeader(m model, headerStyle, labelStyle, accentStyle, warnStyle, errorStyle lipgloss.Style, projectLabel, projectValue string, elapsed time.Duration) string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("xctide"))
	b.WriteString("  ")
	b.WriteString(labelStyle.Render(projectLabel + ": "))
	b.WriteString(accentStyle.Render(filepath.Base(projectValue)))
	b.WriteString("  ")
	b.WriteString(labelStyle.Render("scheme: "))
	b.WriteString(accentStyle.Render(m.config.scheme))
	b.WriteString("  ")
	b.WriteString(labelStyle.Render("config: "))
	b.WriteString(accentStyle.Render(m.config.configuration))
	if m.config.destination != "" {
		b.WriteString("  ")
		b.WriteString(labelStyle.Render("dest: "))
		b.WriteString(accentStyle.Render(m.config.destination))
	}
	b.WriteString("  ")
	b.WriteString(labelStyle.Render("elapsed: "))
	b.WriteString(accentStyle.Render(elapsed.String()))
	b.WriteString("\n")
	statsLine := fmt.Sprintf("warnings: %d  errors: %d  tests: %d  failures: %d", m.stats.warnings, m.stats.errors, m.stats.tests, m.stats.failures)
	statsLine = warnStyle.Render(statsLine)
	if m.stats.errors > 0 || m.stats.failures > 0 {
		statsLine = errorStyle.Render(statsLine)
	}
	b.WriteString(statsLine)
	return b.String()
}

func renderFooter(m model, labelStyle, accentStyle, warnStyle, errorStyle lipgloss.Style) string {
	var b strings.Builder
	if m.finished {
		statusLabel := "build succeeded"
		statusStyle := accentStyle
		if m.err != nil {
			statusLabel = "build failed"
			statusStyle = errorStyle
		}
		total := modelElapsed(m)
		b.WriteString(statusStyle.Render(fmt.Sprintf("%s · total %s", statusLabel, total)))
		b.WriteString("\n")
		b.WriteString(labelStyle.Render("controls: q quit"))
		return strings.TrimSpace(b.String())
	}
	if m.showDetails {
		b.WriteString(labelStyle.Render("last line"))
		b.WriteString("\n")
		b.WriteString(truncateLine(m.lastLine, m.width))
		b.WriteString("\n")
	}
	b.WriteString(labelStyle.Render("controls: q quit · d toggle details"))
	return strings.TrimSpace(b.String())
}

func renderClassicView(m model) string {
	headerStyle := lipgloss.NewStyle().Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	accentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true)
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("204")).Bold(true)

	elapsed := modelElapsed(m)
	projectValue := m.config.projectPath
	if m.config.workspacePath != "" {
		projectValue = m.config.workspacePath
	}

	completed, totalPhases, skipped := progressCounts(m.phases)
	progressPercent := 0
	if totalPhases > 0 {
		progressPercent = (completed * 100) / totalPhases
	}
	currentStep := m.phases[m.currentPhase].name
	if m.finished {
		currentStep = "Completed"
	}
	if m.targetName != "" {
		currentStep = fmt.Sprintf("%s (%s)", currentStep, m.targetName)
	}

	progressWidth := clamp(10, m.width-38, 40)
	filled := (progressWidth * progressPercent) / 100
	if filled < 0 {
		filled = 0
	}
	if filled > progressWidth {
		filled = progressWidth
	}
	progressBar := strings.Repeat("█", filled) + strings.Repeat("░", progressWidth-filled)

	var b strings.Builder
	b.WriteString(headerStyle.Render("xctide build"))
	b.WriteString(" ")
	b.WriteString(labelStyle.Render(filepath.Base(projectValue)))
	b.WriteString(" ")
	b.WriteString(labelStyle.Render(m.config.scheme))
	b.WriteString(" ")
	b.WriteString(labelStyle.Render(m.config.configuration))
	b.WriteString("\n\n")

	b.WriteString(labelStyle.Render("• Build Context"))
	b.WriteString("\n")
	b.WriteString("  ")
	b.WriteString(accentStyle.Render("Project"))
	b.WriteString(dimStyle.Render("  "))
	b.WriteString(labelStyle.Render(filepath.Base(projectValue)))
	b.WriteString("\n")
	b.WriteString("  ")
	b.WriteString(accentStyle.Render("Scheme"))
	b.WriteString(dimStyle.Render("   "))
	b.WriteString(labelStyle.Render(m.config.scheme))
	b.WriteString("\n")
	b.WriteString("  ")
	b.WriteString(accentStyle.Render("Config"))
	b.WriteString(dimStyle.Render("   "))
	b.WriteString(labelStyle.Render(m.config.configuration))
	if m.config.destination != "" {
		b.WriteString("\n")
		b.WriteString("  ")
		b.WriteString(accentStyle.Render("Destination"))
		b.WriteString(dimStyle.Render("  "))
		b.WriteString(labelStyle.Render(m.config.destination))
	}
	b.WriteString("\n\n")

	b.WriteString(labelStyle.Render("• Progress"))
	b.WriteString("\n")
	b.WriteString("  ")
	b.WriteString(labelStyle.Render(progressBar))
	b.WriteString(" ")
	b.WriteString(labelStyle.Render(fmt.Sprintf("%3d%%", progressPercent)))
	b.WriteString(dimStyle.Render(fmt.Sprintf(" (%d/%d)", completed, totalPhases)))
	if skipped > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf(" skipped:%d", skipped)))
	}
	b.WriteString("\n")
	if !m.finished {
		b.WriteString("  ")
		b.WriteString(accentStyle.Render("Active"))
		b.WriteString(dimStyle.Render("  "))
		b.WriteString(labelStyle.Render(currentStep))
	} else {
		b.WriteString("  ")
		b.WriteString(accentStyle.Render("State"))
		b.WriteString(dimStyle.Render("   "))
		b.WriteString(labelStyle.Render("Completed"))
	}
	b.WriteString("\n\n")

	b.WriteString(labelStyle.Render("• Steps"))
	b.WriteString("\n")
	for _, phase := range m.phases {
		statusSymbol := "·"
		statusStyle := dimStyle
		switch phase.status {
		case "active":
			statusSymbol = "▶"
			statusStyle = accentStyle
		case "done":
			statusSymbol = "✓"
			statusStyle = labelStyle
		case "failed":
			statusSymbol = "✗"
			statusStyle = errorStyle
		case "skipped":
			statusSymbol = "○"
			statusStyle = dimStyle
		}
		duration := formatPhaseDuration(phase)
		if phase.status == "skipped" {
			duration = "skipped"
		} else if phase.status == "failed" {
			if duration == "" {
				duration = "failed"
			}
		} else if duration == "" {
			duration = "-"
		}
		b.WriteString("  ")
		b.WriteString(statusStyle.Render(statusSymbol))
		b.WriteString(labelStyle.Render(" "))
		b.WriteString(statusStyle.Render(phase.name))
		b.WriteString(dimStyle.Render(" "))
		b.WriteString(dimStyle.Render(duration))
		b.WriteString("\n")
	}

	if len(m.slowFiles) > 0 {
		b.WriteString("\n")
		b.WriteString(labelStyle.Render("• Slow Files"))
		b.WriteString("\n")
		for _, file := range m.slowFiles {
			b.WriteString("  ")
			b.WriteString(labelStyle.Render("├─ "))
			b.WriteString(accentStyle.Render("Build"))
			b.WriteString(labelStyle.Render(" "))
			b.WriteString(labelStyle.Render(file.name))
			b.WriteString(dimStyle.Render(fmt.Sprintf(" %s", file.duration.String())))
			b.WriteString("\n")
		}
	}

	if len(m.slowTests) > 0 {
		b.WriteString("\n")
		b.WriteString(labelStyle.Render("• Slow Tests"))
		b.WriteString("\n")
		for _, test := range m.slowTests {
			b.WriteString("  ")
			b.WriteString(labelStyle.Render("├─ "))
			b.WriteString(accentStyle.Render("Test"))
			b.WriteString(labelStyle.Render(" "))
			b.WriteString(labelStyle.Render(test.name))
			b.WriteString(dimStyle.Render(fmt.Sprintf(" %s", test.duration.String())))
			b.WriteString("\n")
		}
	}

	if len(m.targets) > 0 {
		b.WriteString("\n")
		b.WriteString(labelStyle.Render("• Slow Targets"))
		b.WriteString("\n")
		for _, target := range m.targets {
			b.WriteString("   ")
			b.WriteString(labelStyle.Render("├─ "))
			b.WriteString(accentStyle.Render("Build"))
			b.WriteString(labelStyle.Render(" "))
			b.WriteString(labelStyle.Render(target.name))
			b.WriteString(dimStyle.Render(fmt.Sprintf(" %s", target.duration.String())))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	if m.finished {
		status := "Build Succeeded"
		style := accentStyle
		if m.err != nil {
			status = "Build Failed"
			style = errorStyle
		}
		b.WriteString(style.Render(fmt.Sprintf("• %s %s", status, elapsed.String())))
	} else {
		b.WriteString(labelStyle.Render(fmt.Sprintf("• Building %s", elapsed.String())))
		b.WriteString(" ")
		b.WriteString(dimStyle.Render(fmt.Sprintf("warnings:%d errors:%d tests:%d failures:%d", m.stats.warnings, m.stats.errors, m.stats.tests, m.stats.failures)))
	}
	b.WriteString("\n\n")

	return strings.TrimSpace(b.String())
}

func modelElapsed(m model) time.Duration {
	if m.startTime.IsZero() {
		return 0
	}
	if m.finished && !m.finishedAt.IsZero() {
		return m.finishedAt.Sub(m.startTime).Truncate(time.Second)
	}
	return time.Since(m.startTime).Truncate(time.Second)
}

func progressCounts(phases []phase) (completed int, total int, skipped int) {
	total = len(phases)
	for _, p := range phases {
		switch p.status {
		case "done", "failed":
			completed++
		case "skipped":
			skipped++
		}
	}
	total = total - skipped
	if total <= 0 {
		total = len(phases)
	}
	return completed, total, skipped
}
