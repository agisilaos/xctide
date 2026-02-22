package main

import "time"

func newEventTracker() *eventTracker {
	phases := defaultPhases()
	names := make([]string, 0, len(phases))
	for _, p := range phases {
		names = append(names, p.name)
	}
	return &eventTracker{
		stepNames:      names,
		currentStepIdx: 0,
	}
}

func (t *eventTracker) runStarted(at time.Time) []buildEvent {
	if t.started {
		return nil
	}
	t.started = true
	t.currentStart = at
	out := []buildEvent{
		{Type: eventRunStarted, At: at},
		{
			Type:      eventStepStarted,
			At:        at,
			StepName:  t.stepNames[t.currentStepIdx],
			StepIndex: t.currentStepIdx + 1,
			StepTotal: len(t.stepNames),
		},
	}
	t.events = append(t.events, out...)
	return out
}

func (t *eventTracker) processLine(line string, at time.Time) []buildEvent {
	var out []buildEvent
	if !t.started {
		out = append(out, t.runStarted(at)...)
	}
	if warningRe.MatchString(line) {
		t.stats.warnings++
		event := buildEvent{Type: eventDiagnostic, At: at, Level: "warning", Message: line}
		out = append(out, event)
		t.events = append(t.events, event)
	}
	if errorRe.MatchString(line) {
		t.stats.errors++
		event := buildEvent{Type: eventDiagnostic, At: at, Level: "error", Message: line}
		out = append(out, event)
		t.events = append(t.events, event)
	}
	if testRe.MatchString(line) {
		t.stats.tests++
	}
	if failRe.MatchString(line) {
		t.stats.failures++
	}

	nextPhase := phaseNameForLine(line)
	nextIdx := phaseIndexByName(t.stepNames, nextPhase)
	if nextIdx <= t.currentStepIdx {
		return out
	}
	for i := t.currentStepIdx; i < nextIdx; i++ {
		finish := buildEvent{
			Type:       eventStepDone,
			At:         at,
			StepName:   t.stepNames[i],
			StepIndex:  i + 1,
			StepTotal:  len(t.stepNames),
			StepStatus: "done",
			DurationMS: at.Sub(t.currentStart).Milliseconds(),
		}
		start := buildEvent{
			Type:      eventStepStarted,
			At:        at,
			StepName:  t.stepNames[i+1],
			StepIndex: i + 2,
			StepTotal: len(t.stepNames),
		}
		out = append(out, finish, start)
		t.events = append(t.events, finish, start)
		t.currentStepIdx = i + 1
		t.currentStart = at
	}
	return out
}

func (t *eventTracker) finish(err error, at time.Time) []buildEvent {
	if t.finished {
		return nil
	}
	if !t.started {
		t.runStarted(at)
	}
	t.finished = true
	var out []buildEvent

	currentStatus := "done"
	if err != nil {
		currentStatus = "failed"
	}
	currentFinished := buildEvent{
		Type:       eventStepDone,
		At:         at,
		StepName:   t.stepNames[t.currentStepIdx],
		StepIndex:  t.currentStepIdx + 1,
		StepTotal:  len(t.stepNames),
		StepStatus: currentStatus,
		DurationMS: at.Sub(t.currentStart).Milliseconds(),
	}
	out = append(out, currentFinished)
	t.events = append(t.events, currentFinished)

	for i := t.currentStepIdx + 1; i < len(t.stepNames); i++ {
		skipped := buildEvent{
			Type:       eventStepDone,
			At:         at,
			StepName:   t.stepNames[i],
			StepIndex:  i + 1,
			StepTotal:  len(t.stepNames),
			StepStatus: "skipped",
		}
		out = append(out, skipped)
		t.events = append(t.events, skipped)
	}

	stats := t.stats
	done := buildEvent{
		Type:       eventRunFinished,
		At:         at,
		ExitCode:   classifyBuildErr(err),
		Success:    err == nil,
		DurationMS: at.Sub(t.events[0].At).Milliseconds(),
		Stats:      &stats,
	}
	out = append(out, done)
	t.events = append(t.events, done)
	return out
}

func phaseIndexByName(stepNames []string, name string) int {
	for i, step := range stepNames {
		if step == name {
			return i
		}
	}
	return -1
}
