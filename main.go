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
	ExitCode   int         `json:"exit_code,omitempty"`
	Success    bool        `json:"success,omitempty"`
	Stats      *buildStats `json:"stats,omitempty"`
}

type timedItem struct {
	name     string
	duration time.Duration
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
	warningRe      = regexp.MustCompile(`(?i)\bwarning:`)
	errorRe        = regexp.MustCompile(`(?i)\berror:`)
	testRe         = regexp.MustCompile(`^Test Case\b|^Test Suite\b`)
	failRe         = regexp.MustCompile(`(?i)\b(failed|failures?)\b`)
	phaseRe        = regexp.MustCompile(`^(CompileC|SwiftCompile|SwiftCompileSources|Ld|LinkStoryboards|CompileStoryboard|ProcessInfoPlistFile|ProcessPCH|CopyBundleResources|CodeSign|Test Suite)\b`)
	targetStartRe  = regexp.MustCompile(`^=== BUILD TARGET (.+) OF PROJECT`)
	testCaseRe     = regexp.MustCompile(`^Test Case '-\[(.+)\]' (passed|failed) \((\d+\.?\d*) seconds\)`)
	compileFileRe  = regexp.MustCompile(`(?i)\bCompile\w*\b.*\s(/[^\s]+\.swift)\b.*\((\d+\.?\d*)\s*s\)`)
	errInterrupted = errors.New("interrupted")
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
	flagSet.StringVar(&cfg.progress, "progress", "auto", "Progress mode: auto|tui|plain|json")
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
	seen := visitedFlags(flagSet)
	applyEnvDefaults(&cfg, seen)
	if cfg.configuration == "" {
		cfg.configuration = "Debug"
	}
	mode, err := resolveProgressMode(cfg, seen, isTerminal())
	if err != nil {
		fmt.Fprintln(os.Stderr, "xctide:", err)
		os.Exit(exitInvalidUsage)
	}

	if showVersion {
		fmt.Println(version)
		return
	}

	if err := autoDetectConfig(&cfg); err != nil {
		fmt.Fprintln(os.Stderr, "xctide:", err)
		os.Exit(exitConfigFailure)
	}
	if cfg.verbose {
		fmt.Fprintf(os.Stderr, "xctide: running xcodebuild %s\n", strings.Join(buildArgs(cfg), " "))
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
			fmt.Fprintln(os.Stderr, "xctide:", err)
			os.Exit(classifyBuildErr(err))
		}
		os.Exit(exitOK)
		return
	}

	cfg.timingSummary = true

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

func normalizeArgs(raw []string) ([]string, string, error) {
	if len(raw) == 0 {
		return raw, "build", nil
	}
	switch raw[0] {
	case "help":
		printUsage(os.Stdout)
		os.Exit(exitOK)
	case "build":
		return raw[1:], "build", nil
	case "run":
		return raw[1:], "run", nil
	}
	return raw, "build", nil
}

func printUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "xctide - wrapper around xcodebuild with TUI and machine-friendly modes")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "USAGE:")
	_, _ = fmt.Fprintln(w, "  xctide [flags] [-- <xcodebuild args>]")
	_, _ = fmt.Fprintln(w, "  xctide build [flags] [-- <xcodebuild args>]")
	_, _ = fmt.Fprintln(w, "  xctide run [flags] [-- <xcodebuild args>]")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "FLAGS:")
	_, _ = fmt.Fprintln(w, "  -h, --help            Show this help")
	_, _ = fmt.Fprintln(w, "      --version         Print version and exit")
	_, _ = fmt.Fprintln(w, "      --scheme string")
	_, _ = fmt.Fprintln(w, "      --workspace string")
	_, _ = fmt.Fprintln(w, "      --project string")
	_, _ = fmt.Fprintln(w, "      --configuration string")
	_, _ = fmt.Fprintln(w, "      --destination string")
	_, _ = fmt.Fprintln(w, "      --progress string  Progress mode: auto|tui|plain|json")
	_, _ = fmt.Fprintln(w, "      --result-bundle string")
	_, _ = fmt.Fprintln(w, "      --plain           Disable TUI and stream raw output")
	_, _ = fmt.Fprintln(w, "      --json            Emit JSON summary to stdout")
	_, _ = fmt.Fprintln(w, "      --quiet           Pass -quiet to xcodebuild")
	_, _ = fmt.Fprintln(w, "      --verbose         Print wrapper diagnostics to stderr")
	_, _ = fmt.Fprintln(w, "      --no-input        Never prompt for selection")
	_, _ = fmt.Fprintln(w, "      --no-color        Disable color output")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "ENV:")
	_, _ = fmt.Fprintln(w, "  XCTIDE_SCHEME, XCTIDE_WORKSPACE, XCTIDE_PROJECT, XCTIDE_CONFIGURATION, XCTIDE_DESTINATION, XCTIDE_PROGRESS")
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "EXAMPLES:")
	_, _ = fmt.Fprintln(w, "  xctide")
	_, _ = fmt.Fprintln(w, "  xctide build --scheme Subsmind --destination \"platform=iOS Simulator,name=iPhone 16\"")
	_, _ = fmt.Fprintln(w, "  xctide run --scheme Subsmind --destination \"platform=iOS Simulator,id=<UDID>\"")
	_, _ = fmt.Fprintln(w, "  xctide --plain -- test")
	_, _ = fmt.Fprintln(w, "  xctide --progress plain -- test")
	_, _ = fmt.Fprintln(w, "  xctide --progress json -- test")
}

func visitedFlags(flagSet *flag.FlagSet) map[string]bool {
	seen := make(map[string]bool)
	flagSet.Visit(func(f *flag.Flag) {
		seen[f.Name] = true
	})
	return seen
}

func applyEnvDefaults(cfg *buildConfig, seen map[string]bool) {
	if !seen["scheme"] {
		cfg.scheme = firstNonEmpty(cfg.scheme, os.Getenv("XCTIDE_SCHEME"))
	}
	if !seen["workspace"] {
		cfg.workspacePath = firstNonEmpty(cfg.workspacePath, os.Getenv("XCTIDE_WORKSPACE"))
	}
	if !seen["project"] {
		cfg.projectPath = firstNonEmpty(cfg.projectPath, os.Getenv("XCTIDE_PROJECT"))
	}
	if !seen["configuration"] {
		cfg.configuration = firstNonEmpty(cfg.configuration, os.Getenv("XCTIDE_CONFIGURATION"))
	}
	if !seen["destination"] {
		cfg.destination = firstNonEmpty(cfg.destination, os.Getenv("XCTIDE_DESTINATION"))
	}
	if !seen["progress"] {
		cfg.progress = firstNonEmpty(cfg.progress, os.Getenv("XCTIDE_PROGRESS"))
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

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

func resolveProgressMode(cfg buildConfig, seen map[string]bool, hasTTY bool) (string, error) {
	if seen["progress"] && (seen["plain"] || seen["json"]) {
		return "", errors.New("use either --progress or --plain/--json, not both")
	}
	if seen["progress"] {
		switch cfg.progress {
		case "auto":
			if hasTTY {
				return "tui", nil
			}
			return "plain", nil
		case "tui":
			if !hasTTY {
				return "", errors.New("--progress=tui requires a TTY")
			}
			return "tui", nil
		case "plain":
			return "plain", nil
		case "json":
			return "json", nil
		default:
			return "", fmt.Errorf("invalid --progress value %q (expected auto|tui|plain|json)", cfg.progress)
		}
	}
	if cfg.jsonOutput {
		return "json", nil
	}
	if cfg.plain {
		return "raw", nil
	}
	if hasTTY {
		return "tui", nil
	}
	return "plain", nil
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

	var result xcodebuildList
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
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
		total := time.Since(m.startTime).Truncate(time.Second)
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

	elapsed := time.Since(m.startTime).Truncate(time.Second)
	projectValue := m.config.projectPath
	if m.config.workspacePath != "" {
		projectValue = m.config.workspacePath
	}

	completed := 0
	skipped := 0
	for _, phase := range m.phases {
		if phase.status == "done" || phase.status == "skipped" || phase.status == "failed" {
			completed++
		}
		if phase.status == "skipped" {
			skipped++
		}
	}
	totalPhases := len(m.phases)
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

type jsonBuildResult struct {
	Success       bool         `json:"success"`
	ExitCode      int          `json:"exit_code"`
	DurationMS    int64        `json:"duration_ms"`
	Command       []string     `json:"command"`
	Project       string       `json:"project,omitempty"`
	Workspace     string       `json:"workspace,omitempty"`
	Scheme        string       `json:"scheme"`
	Configuration string       `json:"configuration"`
	Destination   string       `json:"destination,omitempty"`
	Stats         buildStats   `json:"stats"`
	PhaseTimeline []string     `json:"phase_timeline,omitempty"`
	Events        []buildEvent `json:"events,omitempty"`
	Executed      []jsonAction `json:"executed,omitempty"`
	Error         string       `json:"error,omitempty"`
}

type jsonAction struct {
	Name       string `json:"name"`
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
		return -1, fmt.Errorf("multiple %ss found (%s); rerun with --%s or use interactive mode", kind, strings.Join(options, ", "), kind)
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
	for line := range lines {
		_ = tracker.processLine(line, time.Now())
		if cfg.verbose {
			fmt.Fprintln(os.Stderr, line)
		}
	}
	waitErr := cmd.Wait()
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
		Events:        append([]buildEvent(nil), tracker.events...),
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
	var targetRows []timedItem
	currentTarget := ""
	currentTargetStart := time.Time{}
	commandLabel := "build"
	if cfg.runAfterBuild {
		commandLabel = "run"
	}
	fmt.Fprintf(os.Stdout, "xctide $ xctide %s %s %s %s\n\n", commandLabel, filepath.Base(firstNonEmpty(cfg.workspacePath, cfg.projectPath)), cfg.scheme, cfg.configuration)

	for line := range lines {
		_ = tracker.processLine(line, time.Now())
		if match := targetStartRe.FindStringSubmatch(line); len(match) > 1 {
			if currentTarget != "" && !currentTargetStart.IsZero() {
				targetRows = append(targetRows, timedItem{name: currentTarget, duration: time.Since(currentTargetStart)})
			}
			currentTarget = strings.TrimSpace(match[1])
			currentTargetStart = time.Now()
		}
		if cfg.verbose {
			fmt.Fprintln(os.Stdout, line)
		}
	}

	err = cmd.Wait()
	if currentTarget != "" && !currentTargetStart.IsZero() {
		targetRows = append(targetRows, timedItem{name: currentTarget, duration: time.Since(currentTargetStart)})
	}
	_ = tracker.finish(err, time.Now())
	var executedRows []timedItem
	if err == nil && cfg.runAfterBuild {
		executedRows, err = runAppOnSimulator(cfg)
	}
	stats := tracker.stats
	renderPlainBuildReport(os.Stdout, cfg, tracker.events, targetRows, executedRows, stats, time.Since(start), err)
	return err
}

func renderPlainBuildReport(w io.Writer, cfg buildConfig, events []buildEvent, targetRows []timedItem, executedRows []timedItem, stats buildStats, elapsed time.Duration, err error) {
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
	if len(targetRows) > 0 {
		for _, row := range targetRows {
			fmt.Fprintf(w, "  └ Build %-24s %s\n", row.name, formatDurationDur(row.duration))
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
