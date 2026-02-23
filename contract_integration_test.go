package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestCLIJSONContractSuccessIntegration(t *testing.T) {
	bin := buildCLIBinaryForIntegration(t)
	toolBin := writeStubToolchain(t)
	stdout, stderr, exitCode := runCLIIntegration(
		t,
		bin,
		toolBin,
		"success",
		"--progress", "json",
		"--project", "Stub.xcodeproj",
		"--scheme", "Subsmind",
		"--configuration", "Debug",
		"--destination", "platform=iOS Simulator,name=iPhone 17 Pro",
		"--", "build",
	)
	if stderr != "" {
		t.Fatalf("expected empty stderr, got: %s", stderr)
	}
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%s stderr=%s", exitCode, stdout, stderr)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("invalid json output: %v\n%s", err, stdout)
	}
	if schema, _ := payload["schema_version"].(string); schema != machineSchemaVersion {
		t.Fatalf("schema_version = %#v, want %q", payload["schema_version"], machineSchemaVersion)
	}
	if success, _ := payload["success"].(bool); !success {
		t.Fatalf("success = %#v, want true", payload["success"])
	}
	if exit, ok := payload["exit_code"].(float64); !ok || int(exit) != 0 {
		t.Fatalf("exit_code = %#v, want 0", payload["exit_code"])
	}
	completed, ok := payload["completed"].([]any)
	if !ok || len(completed) == 0 {
		t.Fatalf("completed = %#v, want non-empty", payload["completed"])
	}

	events, ok := payload["events"].([]any)
	if !ok || len(events) == 0 {
		t.Fatalf("events = %#v, want non-empty", payload["events"])
	}
	runFinishedCount := 0
	lastSeq := 0
	for _, item := range events {
		event, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("event not object: %#v", item)
		}
		typ, _ := event["type"].(string)
		seq, ok := event["seq"].(float64)
		if !ok {
			t.Fatalf("event missing seq: %#v", event)
		}
		if int(seq) <= lastSeq {
			t.Fatalf("seq not increasing: prev=%d current=%d event=%#v", lastSeq, int(seq), event)
		}
		lastSeq = int(seq)
		if schema, _ := event["schema_version"].(string); schema != machineSchemaVersion {
			t.Fatalf("event schema_version = %#v, want %q", event["schema_version"], machineSchemaVersion)
		}
		if typ == string(eventRunFinished) {
			runFinishedCount++
			if _, ok := event["success"]; !ok {
				t.Fatalf("run_finished missing success: %#v", event)
			}
			if _, ok := event["exit_code"]; !ok {
				t.Fatalf("run_finished missing exit_code: %#v", event)
			}
		}
	}
	if runFinishedCount != 1 {
		t.Fatalf("run_finished count = %d, want 1", runFinishedCount)
	}
}

func TestCLIJSONContractFailureIntegration(t *testing.T) {
	bin := buildCLIBinaryForIntegration(t)
	toolBin := writeStubToolchain(t)
	stdout, _, exitCode := runCLIIntegration(
		t,
		bin,
		toolBin,
		"failure",
		"--progress", "json",
		"--project", "Stub.xcodeproj",
		"--scheme", "Subsmind",
		"--configuration", "Debug",
		"--destination", "platform=iOS Simulator,name=iPhone 17 Pro",
		"--", "build",
	)
	if exitCode != exitBuildFailure {
		t.Fatalf("exit code = %d, want %d; stdout=%s", exitCode, exitBuildFailure, stdout)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("invalid json output: %v\n%s", err, stdout)
	}
	if success, _ := payload["success"].(bool); success {
		t.Fatalf("success = %#v, want false", payload["success"])
	}
	if exit, ok := payload["exit_code"].(float64); !ok || int(exit) != exitBuildFailure {
		t.Fatalf("exit_code = %#v, want %d", payload["exit_code"], exitBuildFailure)
	}
}

func TestCLINDJSONContractIntegration(t *testing.T) {
	bin := buildCLIBinaryForIntegration(t)
	toolBin := writeStubToolchain(t)
	stdout, stderr, exitCode := runCLIIntegration(
		t,
		bin,
		toolBin,
		"success",
		"--progress", "ndjson",
		"--project", "Stub.xcodeproj",
		"--scheme", "Subsmind",
		"--configuration", "Debug",
		"--destination", "platform=iOS Simulator,name=iPhone 17 Pro",
		"--", "build",
	)
	if stderr != "" {
		t.Fatalf("expected empty stderr, got: %s", stderr)
	}
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%s stderr=%s", exitCode, stdout, stderr)
	}

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) == 0 {
		t.Fatal("expected ndjson lines")
	}
	runFinishedCount := 0
	diagnosticSummaryCount := 0
	for i, line := range lines {
		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			t.Fatalf("line %d invalid json: %v", i+1, err)
		}
		validateMachineContractEvent(t, i, payload)
		typ, _ := payload["type"].(string)
		if typ == string(eventDiagSummary) {
			diagnosticSummaryCount++
		}
		if typ == string(eventRunFinished) {
			runFinishedCount++
			if i != len(lines)-1 {
				t.Fatalf("run_finished must be final line, got index %d of %d", i+1, len(lines))
			}
		}
	}
	if runFinishedCount != 1 {
		t.Fatalf("run_finished count = %d, want 1", runFinishedCount)
	}
	if diagnosticSummaryCount != 1 {
		t.Fatalf("diagnostic_summary count = %d, want 1", diagnosticSummaryCount)
	}
}

func TestCLIPlainOutputIntegration(t *testing.T) {
	bin := buildCLIBinaryForIntegration(t)
	toolBin := writeStubToolchain(t)
	stdout, stderr, exitCode := runCLIIntegration(
		t,
		bin,
		toolBin,
		"success",
		"--progress", "plain",
		"--project", "Stub.xcodeproj",
		"--scheme", "Subsmind",
		"--configuration", "Debug",
		"--destination", "platform=iOS Simulator,name=iPhone 17 Pro",
		"--", "build",
	)
	if stderr != "" {
		t.Fatalf("expected empty stderr, got: %s", stderr)
	}
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%s stderr=%s", exitCode, stdout, stderr)
	}
	normalized := normalizePlainIntegrationOutput(stdout)
	assertGoldenBytes(t, filepath.Join("testdata", "integration", "plain-success.golden"), []byte(normalized))
}

func TestCLIDiagnoseBuildJSONReadyIntegration(t *testing.T) {
	bin := buildCLIBinaryForIntegration(t)
	toolBin := writeStubToolchain(t)
	stdout, stderr, exitCode := runCLIIntegration(
		t,
		bin,
		toolBin,
		"success",
		"diagnose", "build", "--json",
		"--project", "Stub.xcodeproj",
		"--scheme", "Subsmind",
		"--configuration", "Debug",
		"--destination", "platform=iOS Simulator,name=iPhone 17 Pro",
	)
	if stderr != "" {
		t.Fatalf("expected empty stderr, got: %s", stderr)
	}
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%s stderr=%s", exitCode, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("invalid json output: %v\n%s", err, stdout)
	}
	if ready, _ := payload["ready"].(bool); !ready {
		t.Fatalf("ready = %#v, want true", payload["ready"])
	}
	if _, ok := payload["plan"].(map[string]any); !ok {
		t.Fatalf("expected plan object, got %#v", payload["plan"])
	}
}

func TestCLIDiagnoseBuildJSONFailIntegration(t *testing.T) {
	bin := buildCLIBinaryForIntegration(t)
	toolBin := writeBrokenXcrunToolchain(t)
	stdout, stderr, exitCode := runCLIIntegration(
		t,
		bin,
		toolBin,
		"success",
		"diagnose", "build", "--json",
		"--project", "Stub.xcodeproj",
		"--scheme", "Subsmind",
	)
	if stderr != "" {
		t.Fatalf("expected empty stderr, got: %s", stderr)
	}
	if exitCode != exitRuntimeFailure {
		t.Fatalf("exit code = %d, want %d; stdout=%s stderr=%s", exitCode, exitRuntimeFailure, stdout, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("invalid json output: %v\n%s", err, stdout)
	}
	if ready, _ := payload["ready"].(bool); ready {
		t.Fatalf("ready = %#v, want false", payload["ready"])
	}
}

func TestCLICompletionIntegrationZsh(t *testing.T) {
	bin := buildCLIBinaryForIntegration(t)
	stdout, stderr, exitCode := runCLIIntegration(
		t,
		bin,
		"",
		"success",
		"completion", "zsh",
	)
	if stderr != "" {
		t.Fatalf("expected empty stderr, got: %s", stderr)
	}
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%s stderr=%s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stdout, "#compdef xctide") {
		t.Fatalf("missing zsh compdef header: %q", stdout)
	}
	if !strings.Contains(stdout, "xcrun:passthrough to xcrun") {
		t.Fatalf("missing xcrun command completion entry: %q", stdout)
	}
}

func TestCLIXcrunPassthroughArgumentFidelityIntegration(t *testing.T) {
	bin := buildCLIBinaryForIntegration(t)
	toolBin := writeStubToolchain(t)
	stdout, stderr, exitCode := runCLIIntegrationWithOptions(
		t,
		bin,
		toolBin,
		"success",
		"",
		[]string{"XCTIDE_XCRUN_STUB_ECHO_ARGS=1"},
		"xcrun", "--", "simctl", "list", "devices", "available",
	)
	if stderr != "" {
		t.Fatalf("expected empty stderr, got: %s", stderr)
	}
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%s stderr=%s", exitCode, stdout, stderr)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	want := []string{"simctl", "list", "devices", "available"}
	if len(lines) != len(want) {
		t.Fatalf("stdout lines = %#v, want %#v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestCLIDoctorWarnIntegration(t *testing.T) {
	bin := buildCLIBinaryForIntegration(t)
	toolBin := writeStubToolchain(t)
	cwd := t.TempDir()
	stdout, stderr, exitCode := runCLIIntegrationWithOptions(
		t,
		bin,
		toolBin,
		"success",
		cwd,
		nil,
		"doctor", "--json",
	)
	if stderr != "" {
		t.Fatalf("expected empty stderr, got: %s", stderr)
	}
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%s stderr=%s", exitCode, stdout, stderr)
	}

	var result doctorResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("invalid doctor json output: %v\n%s", err, stdout)
	}
	if !result.Success {
		t.Fatalf("doctor success = false, want true; payload=%s", stdout)
	}
	warnCount := 0
	for _, check := range result.Checks {
		if check.Status == "warn" {
			warnCount++
		}
	}
	if warnCount == 0 {
		t.Fatalf("expected at least one warn check, got %#v", result.Checks)
	}
}

func TestCLIDoctorFailIntegration(t *testing.T) {
	bin := buildCLIBinaryForIntegration(t)
	toolBin := writeBrokenXcrunToolchain(t)
	stdout, stderr, exitCode := runCLIIntegration(
		t,
		bin,
		toolBin,
		"success",
		"doctor", "--json",
	)
	if stderr != "" {
		t.Fatalf("expected empty stderr, got: %s", stderr)
	}
	if exitCode != exitRuntimeFailure {
		t.Fatalf("exit code = %d, want %d; stdout=%s stderr=%s", exitCode, exitRuntimeFailure, stdout, stderr)
	}

	var result doctorResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("invalid doctor json output: %v\n%s", err, stdout)
	}
	if result.Success {
		t.Fatalf("doctor success = true, want false; payload=%s", stdout)
	}
	hasFailedCheck := false
	for _, check := range result.Checks {
		if check.Status == "fail" {
			hasFailedCheck = true
			break
		}
	}
	if !hasFailedCheck {
		t.Fatalf("expected at least one failing check, got %#v", result.Checks)
	}
}

func normalizePlainIntegrationOutput(raw string) string {
	out := strings.TrimSpace(raw)
	reSuccess := regexp.MustCompile(`• Build Succeeded [^\n]+`)
	out = reSuccess.ReplaceAllString(out, "• Build Succeeded <elapsed>")
	reFail := regexp.MustCompile(`• Build Failed [^\n]+`)
	out = reFail.ReplaceAllString(out, "• Build Failed <elapsed>")
	return out
}

func buildCLIBinaryForIntegration(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "xctide")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build failed: %v\n%s", err, string(out))
	}
	return bin
}

func writeStubToolchain(t *testing.T) string {
	t.Helper()
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin dir failed: %v", err)
	}

	xcodebuild := `#!/usr/bin/env bash
set -euo pipefail

for arg in "$@"; do
  if [[ "$arg" == "-showBuildSettings" ]]; then
    cat <<'OUT'
TARGET_BUILD_DIR = /tmp/build
WRAPPER_NAME = Subsmind.app
PRODUCT_BUNDLE_IDENTIFIER = com.example.subsmind
OUT
    exit 0
  fi
  if [[ "$arg" == "-list" ]]; then
    cat <<'OUT'
{"project":{"schemes":["Subsmind"]}}
OUT
    exit 0
  fi
  if [[ "$arg" == "-version" ]]; then
    echo "Xcode 26.0"
    exit 0
  fi
done

scenario="${XCTIDE_TEST_SCENARIO:-success}"
log_file="${XCTIDE_TESTDATA_DIR}/build-${scenario}.log"
cat "${log_file}"

if [[ "${scenario}" == "failure" ]]; then
  exit 65
fi
exit 0
`
	if err := os.WriteFile(filepath.Join(binDir, "xcodebuild"), []byte(xcodebuild), 0o755); err != nil {
		t.Fatalf("write xcodebuild stub failed: %v", err)
	}

	xcrun := `#!/usr/bin/env bash
set -euo pipefail
if [[ "${XCTIDE_XCRUN_STUB_ECHO_ARGS:-}" == "1" ]]; then
  printf '%s\n' "$@"
  exit 0
fi
if [[ "${1:-}" == "--version" ]]; then
  echo "xcrun version 1"
  exit 0
fi
if [[ "${1:-}" == "simctl" ]]; then
  if [[ "${2:-}" == "list" ]]; then
    cat <<'OUT'
{"devices":{}}
OUT
    exit 0
  fi
fi
exit 0
`
	if err := os.WriteFile(filepath.Join(binDir, "xcrun"), []byte(xcrun), 0o755); err != nil {
		t.Fatalf("write xcrun stub failed: %v", err)
	}

	return binDir
}

func runCLIIntegration(t *testing.T, bin string, toolBin string, scenario string, args ...string) (stdout string, stderr string, exitCode int) {
	t.Helper()
	return runCLIIntegrationWithOptions(t, bin, toolBin, scenario, "", nil, args...)
}

func runCLIIntegrationWithOptions(
	t *testing.T,
	bin string,
	toolBin string,
	scenario string,
	workingDir string,
	extraEnv []string,
	args ...string,
) (stdout string, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	var outBuf strings.Builder
	var errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	if workingDir != "" {
		cmd.Dir = workingDir
	}

	env := append([]string{}, os.Environ()...)
	if toolBin != "" {
		env = append(env, fmt.Sprintf("PATH=%s:%s", toolBin, os.Getenv("PATH")))
	}
	if scenario != "" {
		env = append(env, fmt.Sprintf("XCTIDE_TEST_SCENARIO=%s", scenario))
	}
	env = append(env, fmt.Sprintf("XCTIDE_TESTDATA_DIR=%s", filepath.Join("testdata", "integration")))
	env = append(env, extraEnv...)
	cmd.Env = env

	err := cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return outBuf.String(), errBuf.String(), exitErr.ExitCode()
		}
		t.Fatalf("run command failed: %v", err)
	}
	return outBuf.String(), errBuf.String(), 0
}

func writeBrokenXcrunToolchain(t *testing.T) string {
	t.Helper()
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin dir failed: %v", err)
	}

	xcodebuild := `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "-version" ]]; then
  echo "Xcode 26.0"
  exit 0
fi
if [[ "${1:-}" == "-list" ]]; then
  cat <<'OUT'
{"project":{"schemes":["Subsmind"]}}
OUT
  exit 0
fi
exit 0
`
	if err := os.WriteFile(filepath.Join(binDir, "xcodebuild"), []byte(xcodebuild), 0o755); err != nil {
		t.Fatalf("write xcodebuild stub failed: %v", err)
	}

	xcrun := `#!/usr/bin/env bash
set -euo pipefail
echo "xcrun unavailable in test stub" >&2
exit 2
`
	if err := os.WriteFile(filepath.Join(binDir, "xcrun"), []byte(xcrun), 0o755); err != nil {
		t.Fatalf("write xcrun stub failed: %v", err)
	}

	return binDir
}
