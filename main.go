package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

const version = "0.1.0"

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
	progress      string
	extraArgs     []string
	resultBundle  string
	useQuiet      bool
	verbose       bool
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
	// success/exit_code even when values are false/0 so consumers can rely on keys.
	if e.Type != eventRunFinished {
		return encoded, nil
	}

	var raw map[string]any
	if err := json.Unmarshal(encoded, &raw); err != nil {
		return nil, err
	}
	raw["success"] = e.Success
	raw["exit_code"] = e.ExitCode
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

type simDeviceInfo struct {
	Name string
	OS   string
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

const maxLines = 240

var (
	warningRe       = regexp.MustCompile(`(?i)\bwarning:`)
	errorRe         = regexp.MustCompile(`(?i)\berror:`)
	testRe          = regexp.MustCompile(`^Test Case\b|^Test Suite\b`)
	failRe          = regexp.MustCompile(`(?i)\b(failed|failures?)\b`)
	phaseRe         = regexp.MustCompile(`^(CompileC|SwiftCompile|SwiftCompileSources|Ld|LinkStoryboards|CompileStoryboard|ProcessInfoPlistFile|ProcessPCH|CopyBundleResources|CodeSign|Test Suite)\b`)
	targetStartRe   = regexp.MustCompile(`^=== BUILD TARGET (.+) OF PROJECT (.+?)(?: WITH CONFIGURATION .+)? ===$`)
	targetContextRe = regexp.MustCompile(`\(in target '(.+)' from project '(.+)'\)`)
	testCaseRe      = regexp.MustCompile(`^Test Case '-\[(.+)\]' (passed|failed) \((\d+\.?\d*) seconds\)`)
	compileFileRe   = regexp.MustCompile(`(?i)\bCompile\w*\b.*\s(/[^\s]+\.swift)\b.*\((\d+\.?\d*)\s*s\)`)
	timingSummaryRe = regexp.MustCompile(`^([A-Za-z0-9_]+ \([0-9]+ tasks?\)) \| ([0-9]+(?:\.[0-9]+)?) seconds$`)
	errInterrupted  = errors.New("interrupted")
)

type eventTracker struct {
	stepNames      []string
	currentStepIdx int
	currentStart   time.Time
	started        bool
	finished       bool
	events         []buildEvent
	stats          buildStats
}

func main() {
	cfg := buildConfig{}
	var showVersion bool
	var noColor bool
	args, commandMode, err := normalizeArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "xctide:", err)
		printUsage(os.Stderr)
		os.Exit(exitInvalidUsage)
	}
	if commandMode == "run" {
		cfg.runAfterBuild = true
	}
	if commandMode == "xcrun" || commandMode == "xctest" {
		if commandMode == "xctest" && wantsXctestHelp(args) {
			printXctestPassthroughHelp(os.Stdout)
			os.Exit(exitOK)
		}
		execName, execArgs, err := passthroughSpec(commandMode, args)
		if err != nil {
			fmt.Fprintln(os.Stderr, "xctide:", err)
			os.Exit(exitInvalidUsage)
		}
		os.Exit(runPassthrough(execName, execArgs))
	}

	flagSet := flag.NewFlagSet("xctide", flag.ContinueOnError)
	flagSet.SetOutput(os.Stderr)
	flagSet.Usage = func() {
		printUsage(flagSet.Output())
	}
	flagSet.StringVar(&cfg.scheme, "scheme", "", "Build scheme name")
	flagSet.StringVar(&cfg.projectPath, "project", "", "Path to .xcodeproj")
	flagSet.StringVar(&cfg.workspacePath, "workspace", "", "Path to .xcworkspace")
	flagSet.StringVar(&cfg.configuration, "configuration", "", "Build configuration (default: Debug)")
	flagSet.StringVar(&cfg.destination, "destination", "", "Destination (e.g. 'platform=iOS Simulator,name=iPhone 16')")
	flagSet.StringVar(&cfg.platform, "platform", "", "Destination filter for `destinations` (e.g. 'iOS Simulator' or 'iOS')")
	flagSet.BoolVar(&cfg.simulatorOnly, "simulator-only", false, "Only simulator destinations (for `destinations`)")
	flagSet.BoolVar(&cfg.deviceOnly, "device-only", false, "Only physical device destinations (for `destinations`)")
	flagSet.StringVar(&cfg.progress, "progress", "auto", "Progress mode: auto|tui|plain|json|ndjson")
	flagSet.StringVar(&cfg.resultBundle, "result-bundle", "", "Path to write result bundle")
	flagSet.BoolVar(&cfg.useQuiet, "quiet", false, "Pass -quiet to xcodebuild")
	flagSet.BoolVar(&cfg.verbose, "verbose", false, "Print wrapper diagnostics to stderr")
	flagSet.BoolVar(&cfg.plain, "plain", false, "Disable TUI and stream raw build output")
	flagSet.BoolVar(&cfg.jsonOutput, "json", false, "Print structured JSON summary to stdout")
	flagSet.BoolVar(&cfg.noInput, "no-input", false, "Disable prompts; fail on ambiguous selection")
	flagSet.BoolVar(&noColor, "no-color", false, "Disable color output")
	flagSet.BoolVar(&showVersion, "version", false, "Print version and exit")
	if err := flagSet.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(exitOK)
		}
		os.Exit(exitInvalidUsage)
	}
	cfg.extraArgs = flagSet.Args()
	if cfg.runAfterBuild && !hasBuildAction(cfg.extraArgs) {
		cfg.extraArgs = append(cfg.extraArgs, "build")
	}
	if cfg.simulatorOnly && cfg.deviceOnly {
		fmt.Fprintln(os.Stderr, "xctide: --simulator-only and --device-only cannot be used together")
		os.Exit(exitInvalidUsage)
	}
	seen := visitedFlags(flagSet)
	applyEnvDefaults(&cfg, seen)
	if cfg.configuration == "" {
		cfg.configuration = "Debug"
	}

	if showVersion {
		fmt.Println(version)
		return
	}

	if commandMode == "doctor" {
		result := runDoctor(cfg)
		if cfg.jsonOutput {
			if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
				fmt.Fprintln(os.Stderr, "xctide:", err)
				os.Exit(exitRuntimeFailure)
			}
		} else {
			renderDoctorResult(os.Stdout, result)
		}
		if result.Success {
			os.Exit(exitOK)
		}
		os.Exit(exitRuntimeFailure)
	}

	if commandMode == "plan" {
		if err := autoDetectConfig(&cfg); err != nil {
			fmt.Fprintln(os.Stderr, "xctide:", err)
			os.Exit(exitConfigFailure)
		}
		result := buildPlanResult(cfg, commandMode)
		if cfg.jsonOutput {
			if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
				fmt.Fprintln(os.Stderr, "xctide:", err)
				os.Exit(exitRuntimeFailure)
			}
		} else {
			renderPlanResult(os.Stdout, result)
		}
		os.Exit(exitOK)
	}

	if commandMode == "destinations" {
		if err := autoDetectConfig(&cfg); err != nil {
			fmt.Fprintln(os.Stderr, "xctide:", err)
			os.Exit(exitConfigFailure)
		}
		options, err := listDestinations(cfg)
		if err != nil {
			fmt.Fprintln(os.Stderr, "xctide:", err)
			os.Exit(exitRuntimeFailure)
		}
		result := destinationsResult{
			Project:      cfg.projectPath,
			Workspace:    cfg.workspacePath,
			Scheme:       cfg.scheme,
			Destinations: options,
		}
		if cfg.jsonOutput {
			if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
				fmt.Fprintln(os.Stderr, "xctide:", err)
				os.Exit(exitRuntimeFailure)
			}
		} else {
			renderDestinationsResult(os.Stdout, result)
		}
		os.Exit(exitOK)
	}

	mode, err := resolveProgressMode(cfg, seen, isTerminal())
	if err != nil {
		fmt.Fprintln(os.Stderr, "xctide:", err)
		os.Exit(exitInvalidUsage)
	}

	if err := autoDetectConfig(&cfg); err != nil {
		fmt.Fprintln(os.Stderr, "xctide:", err)
		os.Exit(exitConfigFailure)
	}
	if cfg.verbose {
		fmt.Fprintf(os.Stderr, "xctide: running xcodebuild %s\n", strings.Join(buildArgs(cfg), " "))
	}

	// Enable xcodebuild timing summary for richer completed reporting.
	if mode != "raw" {
		cfg.timingSummary = true
	}

	if mode == "json" {
		result, err := runJSONBuild(cfg)
		if err != nil {
			fmt.Fprintln(os.Stderr, "xctide:", err)
			os.Exit(exitRuntimeFailure)
		}
		if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
			fmt.Fprintln(os.Stderr, "xctide:", err)
			os.Exit(exitRuntimeFailure)
		}
		os.Exit(result.ExitCode)
	}
	if mode == "ndjson" {
		exitCode, err := runNDJSONBuild(cfg)
		if err != nil {
			fmt.Fprintln(os.Stderr, "xctide:", err)
			os.Exit(exitRuntimeFailure)
		}
		os.Exit(exitCode)
	}

	if mode == "raw" {
		if err := runPlainBuild(cfg); err != nil {
			fmt.Fprintln(os.Stderr, "xctide:", err)
			os.Exit(classifyBuildErr(err))
		}
		os.Exit(exitOK)
		return
	}
	if mode == "plain" {
		if err := runProgressPlainBuild(cfg); err != nil {
			if shouldPrintWrapperError("plain", err) {
				fmt.Fprintln(os.Stderr, "xctide:", err)
			}
			os.Exit(classifyBuildErr(err))
		}
		os.Exit(exitOK)
		return
	}

	if noColor || os.Getenv("NO_COLOR") != "" || strings.EqualFold(os.Getenv("TERM"), "dumb") {
		lipgloss.SetColorProfile(termenv.Ascii)
	}

	session, err := startBuild(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "xctide:", err)
		os.Exit(exitRuntimeFailure)
	}

	phases := defaultPhases()
	phases[0].status = "active"
	phases[0].startedAt = time.Now()

	m := model{
		config:     cfg,
		startTime:  time.Now(),
		phase:      phases[0].name,
		lines:      []string{},
		resultPath: cfg.resultBundle,
		session:    session,
		phases:     phases,
		phaseLogs:  make(map[string][]string),
		phaseStats: make(map[string]buildStats),
		tracker:    newEventTracker(),
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "xctide:", err)
		os.Exit(exitRuntimeFailure)
	}
	if final, ok := finalModel.(model); ok {
		if final.err != nil {
			fmt.Fprintln(os.Stderr, "xctide:", final.err)
			os.Exit(classifyBuildErr(final.err))
		}
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(listenLines(m.session), listenDone(m.session), tick())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tickMsg:
		return m, tick()
	case lineMsg:
		line := string(msg)
		m.lastLine = line
		if m.tracker != nil {
			events := m.tracker.processLine(line, time.Now())
			m.applyBuildEvents(events)
		}
		m.trackTarget(line)
		m.captureTestCase(line)
		m.captureCompileFile(line)
		m.lines = append(m.lines, line)
		if len(m.lines) > maxLines {
			m.lines = m.lines[len(m.lines)-maxLines:]
		}
		m.phaseLogs[m.phases[m.currentPhase].name] = append(m.phaseLogs[m.phases[m.currentPhase].name], line)
		return m, listenLines(m.session)
	case doneMsg:
		m.finished = true
		if m.finishedAt.IsZero() {
			m.finishedAt = time.Now()
		}
		m.err = msg.err
		if m.tracker != nil {
			events := m.tracker.finish(msg.err, time.Now())
			m.applyBuildEvents(events)
		} else {
			m.completeCurrentPhase()
		}
		m.finishTarget()
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.session != nil {
				m.session.interrupt()
			}
			m.err = errInterrupted
			return m, tea.Quit
		case "d":
			m.showDetails = !m.showDetails
			return m, nil
		}
	}
	return m, nil
}

func (m model) View() string {
	if m.width == 0 {
		return ""
	}

	return renderClassicView(m)
}

type buildSession struct {
	lineCh chan string
	doneCh chan error
	cmd    *exec.Cmd
	once   sync.Once
}

func (s *buildSession) interrupt() {
	s.once.Do(func() {
		if s == nil || s.cmd == nil || s.cmd.Process == nil {
			return
		}
		_ = s.cmd.Process.Signal(os.Interrupt)
		go func(p *os.Process) {
			time.Sleep(2 * time.Second)
			_ = p.Kill()
		}(s.cmd.Process)
	})
}

func startBuild(cfg buildConfig) (*buildSession, error) {
	cmd := exec.Command("xcodebuild", buildArgs(cfg)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	session := &buildSession{
		lineCh: make(chan string, 256),
		doneCh: make(chan error, 1),
		cmd:    cmd,
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go streamLines(stdout, session.lineCh, &wg)
	go streamLines(stderr, session.lineCh, &wg)
	go func() {
		wg.Wait()
		close(session.lineCh)
	}()

	go func() {
		session.doneCh <- cmd.Wait()
	}()

	return session, nil
}

func listenLines(session *buildSession) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-session.lineCh
		if !ok {
			return nil
		}
		return lineMsg(line)
	}
}

func listenDone(session *buildSession) tea.Cmd {
	return func() tea.Msg {
		err := <-session.doneCh
		return doneMsg{err: err}
	}
}

func streamLines(r io.Reader, out chan<- string, wg *sync.WaitGroup) {
	defer wg.Done()
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		out <- scanner.Text()
	}
}

func tick() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

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

type xcodebuildList struct {
	Project struct {
		Schemes []string `json:"schemes"`
	} `json:"project"`
	Workspace struct {
		Schemes []string `json:"schemes"`
	} `json:"workspace"`
}

func autoDetectConfig(cfg *buildConfig) error {
	if cfg.projectPath == "" && cfg.workspacePath == "" {
		workspaces, projects := findXcodeContainers(".")
		if len(workspaces) > 0 {
			selected, err := chooseOnePath("workspace", workspaces, cfg.noInput)
			if err != nil {
				return err
			}
			cfg.workspacePath = selected
		} else if len(projects) > 0 {
			selected, err := chooseOnePath("project", projects, cfg.noInput)
			if err != nil {
				return err
			}
			cfg.projectPath = selected
		} else {
			return errors.New("no .xcworkspace or .xcodeproj found")
		}
	}

	if cfg.scheme == "" {
		schemes, err := detectSchemes(*cfg)
		if err != nil {
			return err
		}
		scheme, err := chooseOneValue("scheme", schemes, cfg.noInput)
		if err != nil {
			return err
		}
		cfg.scheme = scheme
	}

	return nil
}

func findXcodeContainers(root string) ([]string, []string) {
	workspaces, _ := filepath.Glob(filepath.Join(root, "*.xcworkspace"))
	projects, _ := filepath.Glob(filepath.Join(root, "*.xcodeproj"))
	sort.Strings(workspaces)
	sort.Strings(projects)
	return workspaces, projects
}

func detectSchemes(cfg buildConfig) ([]string, error) {
	args := []string{"-list", "-json"}
	if cfg.workspacePath != "" {
		args = append(args, "-workspace", cfg.workspacePath)
	} else if cfg.projectPath != "" {
		args = append(args, "-project", cfg.projectPath)
	} else {
		return nil, errors.New("missing project or workspace for scheme detection")
	}

	cmd := exec.Command("xcodebuild", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("xcodebuild -list failed: %w", err)
	}

	result, err := decodeXcodebuildListOutput(out.Bytes())
	if err != nil {
		return nil, fmt.Errorf("parse xcodebuild -list output failed: %w", err)
	}

	var schemes []string
	if cfg.workspacePath != "" {
		schemes = result.Workspace.Schemes
	} else {
		schemes = result.Project.Schemes
	}
	if len(schemes) == 0 {
		return nil, errors.New("no schemes found")
	}
	sort.Strings(schemes)
	return schemes, nil
}

func decodeXcodebuildListOutput(data []byte) (xcodebuildList, error) {
	var result xcodebuildList
	if err := json.Unmarshal(data, &result); err == nil {
		return result, nil
	}
	payload := extractJSONObject(data)
	if len(payload) == 0 {
		return xcodebuildList{}, errors.New("no JSON object found in xcodebuild output")
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return xcodebuildList{}, err
	}
	return result, nil
}

func extractJSONObject(data []byte) []byte {
	start := bytes.IndexByte(data, '{')
	end := bytes.LastIndexByte(data, '}')
	if start == -1 || end == -1 || end < start {
		return nil
	}
	return bytes.TrimSpace(data[start : end+1])
}

func defaultPhases() []phase {
	return []phase{
		{name: "Prepare", status: "pending"},
		{name: "Compile", status: "pending"},
		{name: "Link", status: "pending"},
		{name: "Sign", status: "pending"},
		{name: "Test", status: "pending"},
	}
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

func buildArgs(cfg buildConfig) []string {
	args := []string{}
	if cfg.workspacePath != "" {
		args = append(args, "-workspace", cfg.workspacePath)
	} else if cfg.projectPath != "" {
		args = append(args, "-project", cfg.projectPath)
	}
	args = append(args, "-scheme", cfg.scheme)
	if cfg.configuration != "" {
		args = append(args, "-configuration", cfg.configuration)
	}
	if cfg.destination != "" {
		args = append(args, "-destination", cfg.destination)
	}
	if cfg.useQuiet {
		args = append(args, "-quiet")
	}
	if cfg.resultBundle != "" {
		args = append(args, "-resultBundlePath", cfg.resultBundle)
	}
	args = append(args, cfg.extraArgs...)
	if cfg.timingSummary && !hasArg(args, "-showBuildTimingSummary") {
		args = append(args, "-showBuildTimingSummary")
	}
	return args
}

func hasArg(args []string, value string) bool {
	for _, arg := range args {
		if arg == value {
			return true
		}
	}
	return false
}

func hasBuildAction(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "build", "clean", "test", "archive", "analyze":
			return true
		}
	}
	return false
}

func chooseOnePath(kind string, options []string, noInput bool) (string, error) {
	labels := make([]string, 0, len(options))
	for _, option := range options {
		labels = append(labels, filepath.Base(option))
	}
	index, err := chooseOneIndex(kind, labels, noInput)
	if err != nil {
		return "", err
	}
	return options[index], nil
}

func chooseOneValue(kind string, options []string, noInput bool) (string, error) {
	index, err := chooseOneIndex(kind, options, noInput)
	if err != nil {
		return "", err
	}
	return options[index], nil
}

func chooseOneIndex(kind string, options []string, noInput bool) (int, error) {
	if len(options) == 0 {
		return -1, fmt.Errorf("no %ss found", kind)
	}
	if len(options) == 1 {
		return 0, nil
	}
	if noInput || !isInteractiveTerminal() {
		return -1, fmt.Errorf(
			"multiple %ss found (%s); rerun with --%s <value> (example: --%s %q) or use interactive mode",
			kind,
			strings.Join(options, ", "),
			kind,
			kind,
			options[0],
		)
	}
	fmt.Fprintf(os.Stderr, "xctide: multiple %ss found:\n", kind)
	for i, option := range options {
		fmt.Fprintf(os.Stderr, "  %d) %s\n", i+1, option)
	}
	reader := bufio.NewReader(os.Stdin)
	for attempts := 0; attempts < 3; attempts++ {
		fmt.Fprintf(os.Stderr, "Select %s [1-%d]: ", kind, len(options))
		line, err := reader.ReadString('\n')
		if err != nil {
			return -1, fmt.Errorf("failed to read %s selection: %w", kind, err)
		}
		value, err := strconv.Atoi(strings.TrimSpace(line))
		if err == nil && value >= 1 && value <= len(options) {
			return value - 1, nil
		}
		fmt.Fprintln(os.Stderr, "Invalid selection.")
	}
	return -1, fmt.Errorf("unable to resolve %s from multiple choices", kind)
}

func classifyBuildErr(err error) int {
	if err == nil {
		return exitOK
	}
	if errors.Is(err, errInterrupted) {
		return exitInterrupted
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			if status.Signaled() && status.Signal() == syscall.SIGINT {
				return exitInterrupted
			}
		}
		return exitBuildFailure
	}
	return exitRuntimeFailure
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
			if err := encoder.Encode(event); err != nil {
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
		if err := encoder.Encode(event); err != nil {
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
	if err := encoder.Encode(summary); err != nil {
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
			if err := encoder.Encode(event); err != nil {
				return exitRuntimeFailure, err
			}
		}
		if runErr != nil {
			if err := encoder.Encode(buildEvent{
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
	if err := encoder.Encode(final); err != nil {
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
	fmt.Fprintf(os.Stdout, "xctide $ xctide %s %s %s %s\n\n", commandLabel, filepath.Base(firstNonEmpty(cfg.workspacePath, cfg.projectPath)), cfg.scheme, cfg.configuration)

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

func destinationSummary(destination string) (kind string, name string, osVersion string) {
	kind = "Destination"
	if strings.Contains(destination, "iOS Simulator") {
		kind = "Simulator"
	}
	if strings.Contains(destination, "platform=iOS") && !strings.Contains(destination, "Simulator") {
		kind = "Device"
	}

	for _, part := range strings.Split(destination, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "name=") {
			name = strings.TrimPrefix(part, "name=")
		}
		if strings.HasPrefix(part, "id=") {
			udid := strings.TrimPrefix(part, "id=")
			info := simulatorInfoForUDID(udid)
			if info.Name != "" {
				name = info.Name
			}
			if info.OS != "" {
				osVersion = info.OS
			}
		}
	}
	return kind, name, osVersion
}

func simulatorInfoForUDID(udid string) simDeviceInfo {
	if udid == "" {
		return simDeviceInfo{}
	}
	cmd := exec.Command("xcrun", "simctl", "list", "devices", "--json")
	out, err := cmd.Output()
	if err != nil {
		return simDeviceInfo{}
	}
	var payload struct {
		Devices map[string][]struct {
			UDID string `json:"udid"`
			Name string `json:"name"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return simDeviceInfo{}
	}
	for runtime, devices := range payload.Devices {
		for _, device := range devices {
			if strings.EqualFold(device.UDID, udid) {
				os := strings.TrimPrefix(runtime, "com.apple.CoreSimulator.SimRuntime.iOS-")
				os = strings.ReplaceAll(os, "-", ".")
				return simDeviceInfo{Name: device.Name, OS: os}
			}
		}
	}
	return simDeviceInfo{}
}

func destinationUDID(destination string) string {
	for _, part := range strings.Split(destination, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "id=") {
			return strings.TrimPrefix(part, "id=")
		}
	}
	return ""
}

func runAppOnSimulator(cfg buildConfig) ([]timedItem, error) {
	udid := destinationUDID(cfg.destination)
	if udid == "" {
		return nil, errors.New("run mode requires simulator destination with id=<UDID>")
	}
	info := simulatorInfoForUDID(udid)
	if info.Name == "" {
		return nil, fmt.Errorf("destination id %s is not a simulator device", udid)
	}
	settings, err := readBuildSettings(cfg)
	if err != nil {
		return nil, err
	}
	appPath := filepath.Join(settings.TargetBuildDir, settings.WrapperName)
	rows := make([]timedItem, 0, 3)
	duration, err := runTimedCommand("xcrun", "simctl", "boot", udid)
	if err == nil {
		rows = append(rows, timedItem{name: "Launch simulator", duration: duration})
	}
	if _, err := runTimedCommand("xcrun", "simctl", "bootstatus", udid, "-b"); err != nil {
		return rows, err
	}
	duration, err = runTimedCommand("xcrun", "simctl", "install", udid, appPath)
	if err != nil {
		return rows, err
	}
	rows = append(rows, timedItem{name: "Install iOS app", duration: duration})
	duration, err = runTimedCommand("xcrun", "simctl", "launch", udid, settings.BundleID)
	if err != nil {
		return rows, err
	}
	rows = append(rows, timedItem{name: "Launch iOS app", duration: duration})
	return rows, nil
}

func runTimedCommand(name string, args ...string) (time.Duration, error) {
	start := time.Now()
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if bytes.Contains(out, []byte("Unable to boot device in current state: Booted")) {
			return time.Since(start), nil
		}
		return time.Since(start), fmt.Errorf("%s %s failed: %w", name, strings.Join(args, " "), err)
	}
	return time.Since(start), nil
}

type buildSettings struct {
	TargetBuildDir string
	WrapperName    string
	BundleID       string
}

func readBuildSettings(cfg buildConfig) (buildSettings, error) {
	args := buildArgs(cfg)
	filtered := make([]string, 0, len(args)+1)
	for _, arg := range args {
		switch arg {
		case "build", "clean", "test", "archive", "analyze":
			continue
		default:
			filtered = append(filtered, arg)
		}
	}
	filtered = append(filtered, "-showBuildSettings")
	cmd := exec.Command("xcodebuild", filtered...)
	out, err := cmd.Output()
	if err != nil {
		return buildSettings{}, err
	}
	settings := buildSettings{}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "TARGET_BUILD_DIR = ") {
			settings.TargetBuildDir = strings.TrimPrefix(line, "TARGET_BUILD_DIR = ")
		}
		if strings.HasPrefix(line, "WRAPPER_NAME = ") {
			settings.WrapperName = strings.TrimPrefix(line, "WRAPPER_NAME = ")
		}
		if strings.HasPrefix(line, "PRODUCT_BUNDLE_IDENTIFIER = ") {
			settings.BundleID = strings.TrimPrefix(line, "PRODUCT_BUNDLE_IDENTIFIER = ")
		}
	}
	if settings.TargetBuildDir == "" || settings.WrapperName == "" || settings.BundleID == "" {
		return buildSettings{}, errors.New("could not determine app settings from build settings")
	}
	return settings, nil
}

func runPlainBuild(cfg buildConfig) error {
	cmd := exec.Command("xcodebuild", buildArgs(cfg)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func isTerminal() bool {
	return term.IsTerminal(int(os.Stdout.Fd())) && term.IsTerminal(int(os.Stderr.Fd()))
}

func isInteractiveTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}
