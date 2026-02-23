package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

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
	if commandMode == "completion" {
		os.Exit(runCompletion(args))
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
	flagSet.StringVar(&cfg.destName, "name", "", "Destination name contains filter (for `destinations`)")
	flagSet.StringVar(&cfg.destOS, "os", "", "Destination OS contains filter (for `destinations`)")
	flagSet.IntVar(&cfg.destLimit, "limit", 0, "Max destinations to return (for `destinations`)")
	flagSet.BoolVar(&cfg.destLatest, "latest", false, "Keep latest OS per destination name (for `destinations`)")
	flagSet.BoolVar(&cfg.simulatorOnly, "simulator-only", false, "Only simulator destinations (for `destinations`)")
	flagSet.BoolVar(&cfg.deviceOnly, "device-only", false, "Only physical device destinations (for `destinations`)")
	flagSet.StringVar(&cfg.progress, "progress", "auto", "Progress mode: auto|tui|plain|json|ndjson")
	flagSet.StringVar(&cfg.resultBundle, "result-bundle", "", "Path to write result bundle")
	flagSet.BoolVar(&cfg.details, "details", false, "Expanded plain output sections")
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
	if cfg.destLimit < 0 {
		fmt.Fprintln(os.Stderr, "xctide: --limit cannot be negative")
		os.Exit(exitInvalidUsage)
	}
	seen := visitedFlags(flagSet)
	applyEnvDefaults(&cfg, seen)
	if cfg.configuration == "" {
		cfg.configuration = "Debug"
	}

	if showVersion {
		fmt.Println(printableVersion())
		return
	}

	if handled, code := handleCommandMode(commandMode, &cfg); handled {
		os.Exit(code)
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

func defaultPhases() []phase {
	return []phase{
		{name: "Prepare", status: "pending"},
		{name: "Compile", status: "pending"},
		{name: "Link", status: "pending"},
		{name: "Sign", status: "pending"},
		{name: "Test", status: "pending"},
	}
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
