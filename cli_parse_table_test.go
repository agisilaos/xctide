package main

import (
	"strings"
	"testing"
)

func TestNormalizeArgsTableDriven(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		in        []string
		wantMode  string
		wantArgs  []string
		wantError bool
	}{
		{name: "default build", in: []string{}, wantMode: "build", wantArgs: []string{}},
		{name: "run mode", in: []string{"run", "--scheme", "Subsmind"}, wantMode: "run", wantArgs: []string{"--scheme", "Subsmind"}},
		{name: "plan mode", in: []string{"plan", "--scheme", "Subsmind"}, wantMode: "plan", wantArgs: []string{"--scheme", "Subsmind"}},
		{name: "doctor mode", in: []string{"doctor"}, wantMode: "doctor", wantArgs: []string{}},
		{name: "destinations mode", in: []string{"destinations", "--scheme", "Subsmind"}, wantMode: "destinations", wantArgs: []string{"--scheme", "Subsmind"}},
		{name: "xcrun mode", in: []string{"xcrun", "simctl", "list"}, wantMode: "xcrun", wantArgs: []string{"simctl", "list"}},
		{name: "xctest mode", in: []string{"xctest", "-h"}, wantMode: "xctest", wantArgs: []string{"-h"}},
		{name: "completion mode", in: []string{"completion", "zsh"}, wantMode: "completion", wantArgs: []string{"zsh"}},
		{name: "diagnose build", in: []string{"diagnose", "build", "--scheme", "Subsmind"}, wantMode: "diagnose_build", wantArgs: []string{"--scheme", "Subsmind"}},
		{name: "diagnose missing subcommand", in: []string{"diagnose"}, wantError: true},
		{name: "diagnose unsupported subcommand", in: []string{"diagnose", "test"}, wantError: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotArgs, gotMode, err := normalizeArgs(tc.in)
			if tc.wantError {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeArgs returned error: %v", err)
			}
			if gotMode != tc.wantMode {
				t.Fatalf("mode = %q, want %q", gotMode, tc.wantMode)
			}
			if len(gotArgs) != len(tc.wantArgs) {
				t.Fatalf("args len = %d, want %d (%#v)", len(gotArgs), len(tc.wantArgs), gotArgs)
			}
			for i := range tc.wantArgs {
				if gotArgs[i] != tc.wantArgs[i] {
					t.Fatalf("arg[%d] = %q, want %q", i, gotArgs[i], tc.wantArgs[i])
				}
			}
		})
	}
}

func TestResolveProgressModeTableDriven(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		cfg           buildConfig
		seen          map[string]bool
		isTTY         bool
		wantMode      string
		wantError     bool
		errorContains string
	}{
		{
			name:     "auto non tty -> plain",
			cfg:      buildConfig{outputOptions: outputOptions{progress: "auto"}},
			seen:     map[string]bool{},
			isTTY:    false,
			wantMode: "plain",
		},
		{
			name:          "tui non tty fails",
			cfg:           buildConfig{outputOptions: outputOptions{progress: "tui"}},
			seen:          map[string]bool{"progress": true},
			isTTY:         false,
			wantError:     true,
			errorContains: "requires a tty",
		},
		{
			name:          "progress with plain flag fails",
			cfg:           buildConfig{outputOptions: outputOptions{progress: "plain", plain: true}},
			seen:          map[string]bool{"progress": true, "plain": true},
			isTTY:         true,
			wantError:     true,
			errorContains: "use either --progress or --plain/--json",
		},
		{
			name:     "ndjson explicit",
			cfg:      buildConfig{outputOptions: outputOptions{progress: "ndjson"}},
			seen:     map[string]bool{"progress": true},
			isTTY:    false,
			wantMode: "ndjson",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotMode, err := resolveProgressMode(tc.cfg, tc.seen, tc.isTTY)
			if tc.wantError {
				if err == nil {
					t.Fatal("expected error")
				}
				if tc.errorContains != "" && !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.errorContains)) {
					t.Fatalf("error = %q, want contains %q", err.Error(), tc.errorContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveProgressMode returned error: %v", err)
			}
			if gotMode != tc.wantMode {
				t.Fatalf("mode = %q, want %q", gotMode, tc.wantMode)
			}
		})
	}
}
