package main

import (
	"strings"
	"testing"
)

func TestCLIRegistryUsageAndCompletionStayInSync(t *testing.T) {
	commandWords := strings.Fields(completionCommandWords())
	for _, cmd := range cliCommands {
		if !containsWordSlice(commandWords, cmd.Name) {
			t.Fatalf("completion commands missing %q", cmd.Name)
		}
	}
	if !containsWordSlice(commandWords, "help") {
		t.Fatal("completion commands missing help")
	}

	usage := strings.Join(usageCommandLines(), "\n")
	for _, cmd := range cliCommands {
		want := "xctide " + cmd.Name + cmd.UsageSuffix
		if !strings.Contains(usage, want) {
			t.Fatalf("usage output missing command line: %q", want)
		}
	}

	zshSpecs := zshCommandSpecs()
	for _, cmd := range cliCommands {
		want := "'" + cmd.Name + ":" + cmd.Summary + "'"
		if !strings.Contains(zshSpecs, want) {
			t.Fatalf("zsh specs missing command summary: %q", want)
		}
	}

	fishLines := fishCommandCompletionLines()
	for _, cmd := range cliCommands {
		if !strings.Contains(fishLines, "-a "+cmd.Name) {
			t.Fatalf("fish completion missing command: %q", cmd.Name)
		}
	}
}

func TestCLIRegistryFlagsStayInSync(t *testing.T) {
	flagWords := strings.Fields(completionFlagWords())
	usage := strings.Join(usageFlagLines(), "\n")
	for _, flag := range cliFlags {
		wantWord := "--" + flag.Long
		if !containsWordSlice(flagWords, wantWord) {
			t.Fatalf("completion flags missing %q", wantWord)
		}
		if !strings.Contains(usage, wantWord) {
			t.Fatalf("usage flags missing %q", wantWord)
		}
	}
}

func containsWordSlice(words []string, target string) bool {
	for _, word := range words {
		if word == target {
			return true
		}
	}
	return false
}
