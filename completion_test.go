package main

import (
	"strings"
	"testing"
)

func TestNormalizeArgsCompletionMode(t *testing.T) {
	args, mode, err := normalizeArgs([]string{"completion", "zsh"})
	if err != nil {
		t.Fatalf("normalizeArgs returned error: %v", err)
	}
	if mode != "completion" {
		t.Fatalf("mode = %q, want completion", mode)
	}
	if len(args) != 1 || args[0] != "zsh" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestCompletionScriptBash(t *testing.T) {
	script, err := completionScript("bash")
	if err != nil {
		t.Fatalf("completionScript returned error: %v", err)
	}
	if !strings.Contains(script, "complete -F _xctide_completions xctide") {
		t.Fatalf("unexpected bash script: %q", script)
	}
}

func TestCompletionScriptZsh(t *testing.T) {
	script, err := completionScript("zsh")
	if err != nil {
		t.Fatalf("completionScript returned error: %v", err)
	}
	if !strings.Contains(script, "#compdef xctide") {
		t.Fatalf("unexpected zsh script: %q", script)
	}
}

func TestCompletionScriptFish(t *testing.T) {
	script, err := completionScript("fish")
	if err != nil {
		t.Fatalf("completionScript returned error: %v", err)
	}
	if !strings.Contains(script, "complete -c xctide") {
		t.Fatalf("unexpected fish script: %q", script)
	}
}

func TestCompletionScriptInvalidShell(t *testing.T) {
	_, err := completionScript("powershell")
	if err == nil {
		t.Fatal("expected error for unsupported shell")
	}
}
