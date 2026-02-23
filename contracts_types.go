package main

import (
	"encoding/json"
	"time"
)

const version = "0.1.0"
const machineSchemaVersion = "v1"

const (
	exitOK             = 0
	exitRuntimeFailure = 1
	exitInvalidUsage   = 2
	exitConfigFailure  = 3
	exitBuildFailure   = 4
	exitInterrupted    = 130
)

type buildConfig struct {
	projectPath   string
	workspacePath string
	scheme        string
	configuration string
	destination   string
	platform      string
	simulatorOnly bool
	deviceOnly    bool
	destName      string
	destOS        string
	destLimit     int
	destLatest    bool
	progress      string
	extraArgs     []string
	resultBundle  string
	useQuiet      bool
	verbose       bool
	details       bool
	plain         bool
	jsonOutput    bool
	noInput       bool
	runAfterBuild bool
	timingSummary bool
}

type buildStats struct {
	warnings int
	errors   int
	tests    int
	failures int
}

type doctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // pass|warn|fail
	Details string `json:"details"`
	Hint    string `json:"hint,omitempty"`
}

type doctorResult struct {
	Success bool          `json:"success"`
	Checks  []doctorCheck `json:"checks"`
}

type planResult struct {
	Mode          string   `json:"mode"`
	Project       string   `json:"project,omitempty"`
	Workspace     string   `json:"workspace,omitempty"`
	Scheme        string   `json:"scheme"`
	Configuration string   `json:"configuration"`
	Destination   string   `json:"destination,omitempty"`
	RunAfterBuild bool     `json:"run_after_build"`
	XcodebuildCmd []string `json:"xcodebuild_command"`
}

type diagnoseBuildResult struct {
	Ready     bool         `json:"ready"`
	Doctor    doctorResult `json:"doctor"`
	Plan      *planResult  `json:"plan,omitempty"`
	Issues    []string     `json:"issues,omitempty"`
	NextSteps []string     `json:"next_steps,omitempty"`
}

type destinationOption struct {
	Platform string `json:"platform,omitempty"`
	Arch     string `json:"arch,omitempty"`
	ID       string `json:"id,omitempty"`
	OS       string `json:"os,omitempty"`
	Name     string `json:"name,omitempty"`
	Spec     string `json:"spec"`
}

type destinationsResult struct {
	Project      string              `json:"project,omitempty"`
	Workspace    string              `json:"workspace,omitempty"`
	Scheme       string              `json:"scheme"`
	Destinations []destinationOption `json:"destinations"`
}

func (s buildStats) MarshalJSON() ([]byte, error) {
	type payload struct {
		Warnings int `json:"warnings"`
		Errors   int `json:"errors"`
		Tests    int `json:"tests"`
		Failures int `json:"failures"`
	}
	return json.Marshal(payload{
		Warnings: s.warnings,
		Errors:   s.errors,
		Tests:    s.tests,
		Failures: s.failures,
	})
}

type eventType string

const (
	eventRunStarted  eventType = "run_started"
	eventStepStarted eventType = "step_started"
	eventStepDone    eventType = "step_finished"
	eventDiagnostic  eventType = "diagnostic"
	eventCompleted   eventType = "completed_item"
	eventActionDone  eventType = "action_finished"
	eventActionFail  eventType = "action_failed"
	eventDiagSummary eventType = "diagnostic_summary"
	eventRunFinished eventType = "run_finished"
)

type buildEvent struct {
	Type       eventType   `json:"type"`
	At         time.Time   `json:"at"`
	Schema     string      `json:"schema_version,omitempty"`
	Seq        int         `json:"seq,omitempty"`
	StepName   string      `json:"step_name,omitempty"`
	StepIndex  int         `json:"step_index,omitempty"`
	StepTotal  int         `json:"step_total,omitempty"`
	StepStatus string      `json:"step_status,omitempty"`
	DurationMS int64       `json:"duration_ms,omitempty"`
	Level      string      `json:"level,omitempty"`
	Message    string      `json:"message,omitempty"`
	TaskCount  int         `json:"task_count,omitempty"`
	TopErrors  []string    `json:"top_errors,omitempty"`
	ExitCode   int         `json:"exit_code,omitempty"`
	Success    bool        `json:"success,omitempty"`
	Stats      *buildStats `json:"stats,omitempty"`
}

func (e buildEvent) MarshalJSON() ([]byte, error) {
	type alias buildEvent
	payload := alias(e)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	// run_finished is the contract anchor event for machines; always include
	// success/exit_code/duration_ms so consumers can rely on keys.
	if e.Type != eventRunFinished {
		return encoded, nil
	}

	var raw map[string]any
	if err := json.Unmarshal(encoded, &raw); err != nil {
		return nil, err
	}
	raw["success"] = e.Success
	raw["exit_code"] = e.ExitCode
	raw["duration_ms"] = e.DurationMS
	return json.Marshal(raw)
}

type timedItem struct {
	name     string
	duration time.Duration
}

type completedItem struct {
	Name       string `json:"name"`
	TaskCount  int    `json:"task_count"`
	DurationMS int64  `json:"duration_ms"`
}

type phase struct {
	name      string
	status    string
	startedAt time.Time
	endedAt   time.Time
}

type model struct {
	config       buildConfig
	startTime    time.Time
	finishedAt   time.Time
	phase        string
	lines        []string
	phaseLogs    map[string][]string
	phases       []phase
	currentPhase int
	stats        buildStats
	phaseStats   map[string]buildStats
	targetName   string
	targetStart  time.Time
	targets      []timedItem
	slowFiles    []timedItem
	slowTests    []timedItem
	finished     bool
	err          error
	width        int
	height       int
	lastLine     string
	resultPath   string
	showDetails  bool
	session      *buildSession
	tracker      *eventTracker
}

type lineMsg string

type doneMsg struct {
	err error
}

type tickMsg time.Time

type eventTracker struct {
	stepNames      []string
	currentStepIdx int
	currentStart   time.Time
	started        bool
	finished       bool
	events         []buildEvent
	stats          buildStats
}
