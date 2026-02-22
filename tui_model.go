package main

import (
	"path/filepath"
	"strings"
	"time"
)

func detectPhase(line string, current string) string {
	match := phaseRe.FindString(line)
	if match == "" {
		return current
	}
	if strings.HasPrefix(match, "Test Suite") {
		return "Testing"
	}
	return match
}

func (m *model) advancePhase(line string) {
	phaseName := phaseNameForLine(line)
	if phaseName == "" {
		return
	}
	idx := phaseIndex(m.phases, phaseName)
	if idx == -1 || idx == m.currentPhase || idx < m.currentPhase {
		return
	}
	m.completeCurrentPhase()
	m.currentPhase = idx
	m.phase = phaseName
	m.phases[m.currentPhase].status = "active"
	m.phases[m.currentPhase].startedAt = time.Now()
}

func (m *model) completeCurrentPhase() {
	if m.currentPhase < 0 || m.currentPhase >= len(m.phases) {
		return
	}
	if m.phases[m.currentPhase].status == "done" {
		return
	}
	m.phases[m.currentPhase].status = "done"
	m.phases[m.currentPhase].endedAt = time.Now()
}

func (m *model) markRemainingPhasesSkipped() {
	for i := range m.phases {
		if m.phases[i].status == "pending" {
			m.phases[i].status = "skipped"
		}
	}
}

func (m *model) applyBuildEvents(events []buildEvent) {
	for _, event := range events {
		switch event.Type {
		case eventStepStarted:
			idx := phaseIndex(m.phases, event.StepName)
			if idx < 0 {
				continue
			}
			m.currentPhase = idx
			m.phase = event.StepName
			m.phases[idx].status = "active"
			m.phases[idx].startedAt = event.At
		case eventStepDone:
			idx := phaseIndex(m.phases, event.StepName)
			if idx < 0 {
				continue
			}
			m.currentPhase = idx
			switch event.StepStatus {
			case "done":
				m.phases[idx].status = "done"
			case "failed":
				m.phases[idx].status = "failed"
			case "skipped":
				m.phases[idx].status = "skipped"
			}
			if m.phases[idx].startedAt.IsZero() {
				m.phases[idx].startedAt = event.At
			}
			m.phases[idx].endedAt = event.At
		case eventDiagnostic:
			switch event.Level {
			case "warning":
				m.stats.warnings++
			case "error":
				m.stats.errors++
			}
		case eventRunFinished:
			if event.Stats != nil {
				m.stats = *event.Stats
			}
		}
	}
}

func phaseNameForLine(line string) string {
	switch {
	case strings.Contains(line, "Test Suite") || strings.HasPrefix(line, "Test Case"):
		return "Test"
	case strings.HasPrefix(line, "CodeSign"):
		return "Sign"
	case strings.HasPrefix(line, "Ld"):
		return "Link"
	case strings.HasPrefix(line, "Compile") || strings.HasPrefix(line, "SwiftCompile") || strings.HasPrefix(line, "ProcessInfoPlistFile") || strings.HasPrefix(line, "CopyBundleResources"):
		return "Compile"
	case strings.Contains(line, "Build preparation"):
		return "Prepare"
	default:
		return ""
	}
}

func (m *model) trackTarget(line string) {
	match := targetStartRe.FindStringSubmatch(line)
	if len(match) < 2 {
		return
	}
	m.finishTarget()
	m.targetName = strings.TrimSpace(match[1])
	m.targetStart = time.Now()
}

func (m *model) finishTarget() {
	if m.targetName == "" || m.targetStart.IsZero() {
		return
	}
	duration := time.Since(m.targetStart).Truncate(time.Second)
	m.targets = addTimedItem(m.targets, timedItem{name: m.targetName, duration: duration}, 5)
	m.targetName = ""
	m.targetStart = time.Time{}
}

func (m *model) updatePhaseStats(line string) {
	name := m.phases[m.currentPhase].name
	stats := m.phaseStats[name]
	if warningRe.MatchString(line) {
		stats.warnings++
	}
	if errorRe.MatchString(line) {
		stats.errors++
	}
	m.phaseStats[name] = stats
}

func (m *model) captureTestCase(line string) {
	match := testCaseRe.FindStringSubmatch(line)
	if len(match) < 4 {
		return
	}
	duration := parseDurationSeconds(match[3])
	if duration == 0 {
		return
	}
	item := timedItem{name: match[1], duration: duration}
	m.slowTests = addTimedItem(m.slowTests, item, 5)
}

func (m *model) captureCompileFile(line string) {
	match := compileFileRe.FindStringSubmatch(line)
	if len(match) < 3 {
		return
	}
	duration := parseDurationSeconds(match[2])
	if duration == 0 {
		return
	}
	item := timedItem{name: filepath.Base(match[1]), duration: duration}
	m.slowFiles = addTimedItem(m.slowFiles, item, 5)
}

func phaseIndex(phases []phase, name string) int {
	for i, phase := range phases {
		if phase.name == name {
			return i
		}
	}
	return -1
}

func updateStats(line string, stats buildStats) buildStats {
	if warningRe.MatchString(line) {
		stats.warnings++
	}
	if errorRe.MatchString(line) {
		stats.errors++
	}
	if testRe.MatchString(line) {
		stats.tests++
	}
	if failRe.MatchString(line) {
		stats.failures++
	}
	return stats
}
