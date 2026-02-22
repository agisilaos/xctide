package main

import (
	"strings"
	"testing"
)

func TestResolveCommandMode(t *testing.T) {
	mode, ok := resolveCommandMode("completion")
	if !ok {
		t.Fatal("expected completion to resolve")
	}
	if mode != "completion" {
		t.Fatalf("mode = %q, want completion", mode)
	}

	if _, ok := resolveCommandMode("unknown"); ok {
		t.Fatal("expected unknown command not to resolve")
	}
}

func TestUsageCommandLinesIncludesCompletion(t *testing.T) {
	lines := usageCommandLines()
	found := false
	for _, line := range lines {
		if line == "  xctide completion <bash|zsh|fish>" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing completion usage line in %#v", lines)
	}
}

func TestCompletionWordLists(t *testing.T) {
	commands := completionCommandWords()
	if commands == "" || !containsWord(commands, "completion") || !containsWord(commands, "build") {
		t.Fatalf("unexpected command words: %q", commands)
	}
	flags := completionFlagWords()
	if flags == "" || !containsWord(flags, "--progress") || !containsWord(flags, "--scheme") {
		t.Fatalf("unexpected flag words: %q", flags)
	}
}

func containsWord(words string, want string) bool {
	for _, word := range strings.Fields(words) {
		if word == want {
			return true
		}
	}
	return false
}
